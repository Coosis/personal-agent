from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from psycopg.types.json import Jsonb

from processor.db import (
    CreateDocumentParams,
    CreateJobParams,
    UpdateDocumentBasicsParams,
    UpsertSourceItemParams,
    models,
    query,
    transaction,
)
from processor.document import document_title_from_path
from processor.heartbeat import Heartbeat
from processor.metadata import detect_mime_type, file_fingerprint
from processor.runtime import (
    PermanentJobError,
    RetryableJobError,
    ScannedFile,
    coerce_int,
    ensure_mapping,
)


# scan a source for changes and update documents accordingly,
# then enqueue reindex jobs for changed documents for file
# and directory sources
def process_scan_source(reindex_job_type: str, job: models.Job, heartbeat: Heartbeat) -> None:
    payload = ensure_mapping(job.payload)
    source_id = coerce_int(payload.get("source_id"), "source_id")

    with transaction() as conn:
        source = query(conn).get_source_by_id(id=source_id)  # no side effect db query lookup
    if source is None:
        raise PermanentJobError(f"source {source_id} not found")

    if source.type == "file":
        scan_file_source(reindex_job_type, job.id, source, heartbeat)
        return
    if source.type == "directory":
        scan_directory_source(reindex_job_type, job.id, source, heartbeat)
        return

    raise PermanentJobError(f"scan_source does not support source type {source.type}")


def scan_file_source(
    reindex_job_type: str,
    job_id: int,
    source: models.Source,
    heartbeat: Heartbeat,
) -> None:
    if not source.locator:
        raise PermanentJobError(f"file source {source.id} has no locator")

    heartbeat.ensure_active()
    path = Path(source.locator)
    display_name = source.display_name or document_title_from_path(path)
    current_metadata = dict(source.metadata or {})

    with transaction() as conn:
        q = query(conn)
        document = q.get_document_by_source_id(source_id=source.id)

        if not path.exists() or not path.is_file():
            if document is not None:
                q.update_document_basics(
                    UpdateDocumentBasicsParams(
                        id=document.id,
                        title=document.title,
                        mime_type=document.mime_type,
                        status="deleted",
                        parser_version=document.parser_version,
                        chunker_version=document.chunker_version,
                        last_error=None,
                        metadata=document.metadata,
                    )
                )
            q.set_source_last_scan_at(id=source.id)
            q.complete_job(id=job_id)
            return

        current_fingerprint = file_fingerprint(path)
        changed = current_metadata.get("scan_fingerprint") != current_fingerprint
        current_metadata.update(
            {
                "scan_fingerprint": current_fingerprint,
                "size_bytes": path.stat().st_size,
                "mtime_ns": path.stat().st_mtime_ns,
            }
        )
        q.update_source_basics(
            id=source.id,
            locator=str(path),
            display_name=display_name,
            metadata=current_metadata,
        )

        if document is None:
            document = q.create_document(
                CreateDocumentParams(
                    source_id=source.id,
                    source_item_id=None,
                    title=display_name,
                    mime_type=detect_mime_type(path),
                    status="active",
                    parser_version=None,
                    chunker_version=None,
                    metadata={},
                )
            )
            changed = True
        else:
            q.update_document_basics(
                UpdateDocumentBasicsParams(
                    id=document.id,
                    title=display_name,
                    mime_type=detect_mime_type(path),
                    status="active",
                    parser_version=document.parser_version,
                    chunker_version=document.chunker_version,
                    last_error=None,
                    metadata=document.metadata,
                )
            )
            changed = changed or document.status != "active"

        if document is None:
            raise RetryableJobError(f"failed to create document for source {source.id}")

        if changed:
            enqueue_reindex_document(reindex_job_type, conn, document.id, source.id, "scan_source")

        q.set_source_last_scan_at(id=source.id)
        q.complete_job(id=job_id)


def scan_directory_source(
    reindex_job_type: str,
    job_id: int,
    source: models.Source,
    heartbeat: Heartbeat,
) -> None:
    if not source.locator:
        raise PermanentJobError(f"directory source {source.id} has no locator")

    heartbeat.ensure_active()
    root = Path(source.locator)
    discovered = discover_directory_files(root)
    discovered_by_key = {item.item_key: item for item in discovered}

    with transaction() as conn:
        q = query(conn)
        existing_items = {
            item.item_key: item for item in q.list_source_items_by_source_id(source_id=source.id)
        }

        for scanned in discovered:
            heartbeat.ensure_active()
            existing = existing_items.get(scanned.item_key)
            changed = (
                existing is None
                or existing.is_deleted
                or existing.fingerprint != scanned.fingerprint
            )

            item = q.upsert_source_item(
                UpsertSourceItemParams(
                    source_id=source.id,
                    item_key=scanned.item_key,
                    locator=scanned.locator,
                    display_name=scanned.display_name,
                    fingerprint=scanned.fingerprint,
                    is_deleted=False,
                    metadata=Jsonb(scanned.metadata),
                )
            )
            if item is None:
                raise RetryableJobError(f"failed to upsert source_item {scanned.item_key}")

            document = q.get_document_by_source_item_id(source_item_id=item.id)
            if document is None:
                document = q.create_document(
                    CreateDocumentParams(
                        source_id=source.id,
                        source_item_id=item.id,
                        title=scanned.display_name,
                        mime_type=scanned.mime_type,
                        status="active",
                        parser_version=None,
                        chunker_version=None,
                        metadata=Jsonb(scanned.metadata),
                    )
                )
                changed = True
            else:
                q.update_document_basics(
                    UpdateDocumentBasicsParams(
                        id=document.id,
                        title=scanned.display_name,
                        mime_type=scanned.mime_type,
                        status="active",
                        parser_version=document.parser_version,
                        chunker_version=document.chunker_version,
                        last_error=None,
                        metadata=document.metadata,
                    )
                )
                changed = changed or document.status != "active"

            if document is None:
                raise RetryableJobError(f"failed to create document for source_item {item.id}")

            if changed:
                enqueue_reindex_document(
                    reindex_job_type,
                    conn,
                    document.id,
                    source.id,
                    "scan_source",
                    source_item_id=item.id,
                )

        for item_key, existing in existing_items.items():
            heartbeat.ensure_active()
            if item_key in discovered_by_key or existing.is_deleted:
                continue
            q.mark_source_item_deleted_by_id(id=existing.id)
            q.mark_document_deleted_by_source_item_id(source_item_id=existing.id)

        q.set_source_last_scan_at(id=source.id)
        q.complete_job(id=job_id)


def discover_directory_files(root: Path) -> list[ScannedFile]:
    if not root.exists() or not root.is_dir():
        return []

    discovered: list[ScannedFile] = []
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        item_key = path.relative_to(root).as_posix()
        stat = path.stat()
        discovered.append(
            ScannedFile(
                item_key=item_key,
                locator=str(path),
                display_name=path.name,
                fingerprint=file_fingerprint(path),
                mime_type=detect_mime_type(path),
                metadata={
                    "relative_path": item_key,
                    "size_bytes": stat.st_size,
                    "mtime_ns": stat.st_mtime_ns,
                },
            )
        )
    return discovered


def enqueue_reindex_document(
    reindex_job_type: str,
    conn,
    document_id: int,
    source_id: int,
    trigger: str,
    source_item_id: int | None = None,
) -> None:
    q = query(conn)
    payload = build_reindex_payload(document_id, source_id, trigger, source_item_id)
    q.create_job(
        CreateJobParams(
            type=reindex_job_type,
            payload=Jsonb(payload),
            dedupe_key=f"{reindex_job_type}:{document_id}",
            status="pending",
            available_at=datetime.now(UTC),
        )
    )


def build_reindex_payload(
    document_id: int,
    source_id: int,
    trigger: str,
    source_item_id: int | None = None,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "document_id": document_id,
        "source_id": source_id,
        "trigger": trigger,
    }
    if source_item_id is not None:
        payload["source_item_id"] = source_item_id
    return payload
