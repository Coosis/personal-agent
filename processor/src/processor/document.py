from pathlib import Path

from psycopg.types.json import Jsonb

from processor.db import (
    UpdateDocumentBasicsParams,
    query,
    transaction,
)
from processor.metadata import detect_mime_type, sha256_file
from processor.parsing import extract_text
from processor.runtime import LoadedDocument, PermanentJobError


def load_document_content(document_id: int) -> LoadedDocument | None:
    with transaction() as conn:
        q = query(conn)
        document = q.get_document_by_id(id=document_id)
        if document is None:
            raise PermanentJobError(f"document {document_id} not found")

        source = q.get_source_by_id(id=document.source_id)
        if source is None:
            raise PermanentJobError(f"source {document.source_id} not found")

        metadata = dict(document.metadata or {})

        if source.type == "text":
            note = q.get_note_by_source_id(source_id=source.id)
            if note is None:
                raise PermanentJobError(f"note for source {source.id} not found")
            return LoadedDocument(
                document_id=document.id,
                source_id=source.id,
                source_type=source.type,
                title=note.title,
                mime_type=document.mime_type or "text/plain",
                metadata=metadata,
                content=note.body,
                content_hash=note.content_hash,
                extension=".txt",
            )

        if source.type == "upload":
            upload = q.get_upload_by_source_id(source_id=source.id)
            if upload is None:
                raise PermanentJobError(f"upload for source {source.id} not found")
            path = Path(upload.storage_path)
            if not path.exists():
                raise PermanentJobError(f"upload payload missing at {path}")
            return LoadedDocument(
                document_id=document.id,
                source_id=source.id,
                source_type=source.type,
                title=source.display_name or upload.original_filename,
                mime_type=upload.mime_type,
                metadata=metadata,
                content=extract_text(str(path)),
                content_hash=upload.content_hash or sha256_file(path),
                extension=path.suffix.lower(),
            )

        if source.type == "file":
            if not source.locator:
                raise PermanentJobError(f"file source {source.id} has no locator")
            path = Path(source.locator)
            if not path.exists():
                return None
            return LoadedDocument(
                document_id=document.id,
                source_id=source.id,
                source_type=source.type,
                title=source.display_name or document_title_from_path(path),
                mime_type=detect_mime_type(path),
                metadata=metadata,
                content=extract_text(str(path)),
                content_hash=sha256_file(path),
                extension=path.suffix.lower(),
            )

        if source.type == "directory":
            if document.source_item_id is None:
                raise PermanentJobError(f"directory document {document.id} has no source_item_id")
            item = q.get_source_item_by_id(id=document.source_item_id)
            if item is None or item.is_deleted:
                return None
            path = Path(item.locator)
            if not path.exists():
                return None
            return LoadedDocument(
                document_id=document.id,
                source_id=source.id,
                source_type=source.type,
                title=item.display_name or document_title_from_path(path),
                mime_type=detect_mime_type(path),
                metadata=metadata,
                content=extract_text(str(path)),
                content_hash=sha256_file(path),
                extension=path.suffix.lower(),
            )

        raise PermanentJobError(f"unsupported source type {source.type}")


def mark_document_deleted(conn, document_id: int) -> None:
    q = query(conn)
    document = q.get_document_by_id(id=document_id)
    if document is None:
        return
    q.update_document_basics(
        UpdateDocumentBasicsParams(
            id=document.id,
            title=document.title,
            mime_type=document.mime_type,
            status="deleted",
            parser_version=document.parser_version,
            chunker_version=document.chunker_version,
            last_error=None,
            metadata=Jsonb(document.metadata),
        )
    )


def document_title_from_path(path: Path) -> str:
    return path.name or str(path)
