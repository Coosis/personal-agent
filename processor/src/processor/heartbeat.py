"""
Heartbeat mechanism for renewing job leases in a worker process.
"""

import logging
import threading

from processor.config import Config
from processor.db import query, transaction
from processor.runtime import RetryableJobError


class Heartbeat:
    """Renews the claimed job lease while work is in progress."""

    def __init__(
        self,
        cfg: Config,
        worker_id: str,
        job_id: int,
        shutdown_event: threading.Event,
        logger: logging.Logger,
    ):
        self.cfg = cfg
        self.worker_id = worker_id
        self.job_id = job_id
        self.shutdown_event = shutdown_event
        self.stop_event = threading.Event()
        self.lost_lease = False
        self.thread = threading.Thread(target=self._run, daemon=True)
        self.logger = logger

    def start(self) -> None:
        self.thread.start()

    def stop(self) -> None:
        self.stop_event.set()
        self.thread.join(timeout=5)

    def ensure_active(self) -> None:
        if self.shutdown_event.is_set():
            raise RetryableJobError("shutdown requested")
        if self.lost_lease:
            raise RetryableJobError("lost job lease")

    def _run(self) -> None:
        while not self.stop_event.wait(self.cfg.heartbeat_interval_seconds):
            try:
                with transaction() as conn:
                    job = query(conn).renew_job_lease(
                        id=self.job_id,
                        duration=self.cfg.lease_duration_seconds,
                    )
                if job is None:
                    self.lost_lease = True
                    self.logger.warning(
                        "[%s] lost lease for job %s",
                        self.worker_id,
                        self.job_id,
                    )
                    return
            except Exception as exc:
                self.lost_lease = True
                self.logger.exception(
                    "[%s] heartbeat failed for job %s: %s",
                    self.worker_id,
                    self.job_id,
                    exc,
                )
                return
