"""Job-driven processor entrypoint."""

from __future__ import annotations

import logging
import os
import signal
import threading
import time
import uuid

from processor.config import Config, get_config
from processor.conversation_summary import process_summarize_conversation
from processor.db import (
    close_engine,
    init_engine,
    models,
    query,
    transaction,
)
from processor.heartbeat import Heartbeat
from processor.memory import process_extract_memory_suggestions
from processor.reindex import process_reindex_document
from processor.runtime import PermanentJobError, coerce_int, ensure_mapping
from processor.scan import process_scan_source

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s [processor] %(message)s",
)
logger = logging.getLogger(__name__)

PARSER_VERSION = "processor:v1"
CHUNKER_VERSION = "chunker:v1"
REINDEX_JOB_TYPE = "reindex_document"
SCAN_JOB_TYPE = "scan_source"
PURGE_JOB_TYPE = "purge_source_content"
MEMORY_EXTRACTION_JOB_TYPE = "extract_memory_suggestions"
SUMMARIZE_CONVERSATION_JOB_TYPE = "summarize_conversation"

WORKER_ID = str(uuid.uuid4())[:8]


def process_purge_source_content(job: models.Job, heartbeat: Heartbeat) -> None:
    payload = ensure_mapping(job.payload)
    source_id = coerce_int(payload.get("source_id"), "source_id")

    heartbeat.ensure_active()
    with transaction() as conn:
        q = query(conn)
        source = q.get_source_by_id(id=source_id)
        if source is None:
            raise PermanentJobError(f"source {source_id} not found")

        q.mark_documents_deleted_by_source_id(source_id=source.id)
        if source.type == "directory":
            q.mark_source_items_deleted_by_source_id(source_id=source.id)
        q.complete_job(id=job.id)


def process_job(cfg: Config, shutdown_event: threading.Event, job: models.Job) -> None:
    heartbeat = Heartbeat(cfg, WORKER_ID, job.id, shutdown_event, logger)
    heartbeat.start()
    try:
        logger.info("[%s] processing job %s (%s)", WORKER_ID, job.id, job.type)
        if job.type == REINDEX_JOB_TYPE:
            process_reindex_document(
                cfg,
                WORKER_ID,
                PARSER_VERSION,
                CHUNKER_VERSION,
                heartbeat,
                job,
                logger,
            )
        elif job.type == SCAN_JOB_TYPE:
            process_scan_source(REINDEX_JOB_TYPE, job, heartbeat)
        elif job.type == PURGE_JOB_TYPE:
            process_purge_source_content(job, heartbeat)
        elif job.type == MEMORY_EXTRACTION_JOB_TYPE:
            heartbeat.ensure_active()
            process_extract_memory_suggestions(cfg, heartbeat, job)
        elif job.type == SUMMARIZE_CONVERSATION_JOB_TYPE:
            heartbeat.ensure_active()
            process_summarize_conversation(cfg, heartbeat, job)
        else:
            raise PermanentJobError(f"unsupported job type {job.type}")

        logger.info("[%s] completed job %s", WORKER_ID, job.id)
    except PermanentJobError as exc:
        fail_job(job, exc, permanent=True)
    except Exception as exc:
        fail_job(job, exc, permanent=False, max_retries=cfg.max_retries)
    finally:
        heartbeat.stop()


def fail_job(job: models.Job, exc: Exception, permanent: bool, max_retries: int = 0) -> None:
    message = str(exc).strip() or exc.__class__.__name__
    message = message[:2000]
    logger.exception("[%s] job %s failed: %s", WORKER_ID, job.id, message)

    with transaction() as conn:
        q = query(conn)
        if permanent or job.attempt_count >= max_retries:
            q.fail_job_permanent(id=job.id, last_error=message)
            return

        backoff_seconds = min(300, max(5, job.attempt_count * 15))
        q.fail_job_retryable(
            id=job.id,
            duration=backoff_seconds,
            last_error=message,
        )


def claim_next_job(cfg: Config) -> models.Job | None:
    with transaction() as conn:
        return query(conn).claim_next_runnable_job(duration=cfg.lease_duration_seconds)


def main() -> None:
    cfg = get_config()
    shutdown_event = threading.Event()
    signal_count = 0
    init_engine()

    def handle_signal(signum, _frame) -> None:
        nonlocal signal_count
        signal_count += 1
        if signal_count >= 2:
            logger.warning("received signal %s again, forcing immediate shutdown", signum)
            os._exit(128 + int(signum))
        logger.info("received signal %s, shutting down after current job", signum)
        shutdown_event.set()

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    logger.info("[%s] processor started", WORKER_ID)
    try:
        while not shutdown_event.is_set():
            job = claim_next_job(cfg)
            if job is None:
                time.sleep(cfg.poll_interval_seconds)
                continue
            process_job(cfg, shutdown_event, job)
    finally:
        close_engine()
        logger.info("[%s] processor stopped", WORKER_ID)


if __name__ == "__main__":
    main()
