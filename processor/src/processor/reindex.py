from logging import Logger
from psycopg.types.json import Jsonb
from sqlc.pydb.models import Job
from sqlc.pydb.query import CreateChunkParams, UpdateDocumentBasicsParams
from processor.runtime import LoadedDocument, PermanentJobError, RetryableJobError, coerce_int, ensure_mapping
from processor.chunking import Chunk, chunk_document
from processor.config import Config
from processor.db import query, transaction
from processor.document import load_document_content, mark_document_deleted
from processor.embedding import get_embedding_service
from processor.heartbeat import Heartbeat

# only side effect is at the end when activating the new build
def process_reindex_document(
        cfg: Config,
        worker_id: str,
        parser_version: str,
        chunker_version: str,
        heartbeat: Heartbeat,
        job: Job,
        logger: Logger
        ) -> None:
    payload = ensure_mapping(job.payload) # asserts payload is dict-like
    document_id = coerce_int(payload.get("document_id"), "document_id") # asserts document_id is int
    loaded = load_document_content(document_id) # db query to get the document
    if loaded is None:
        with transaction() as conn:
            mark_document_deleted(conn, document_id)
            query(conn).complete_job(id=job.id)
        logger.info("[%s] document %s no longer has source content, marked deleted", worker_id, document_id)
        return

    heartbeat.ensure_active()

    chunks = chunk_document(
        loaded.title,
        loaded.content,
        loaded.extension,
        chunk_size=cfg.chunk_size,
        chunk_overlap=cfg.chunk_overlap,
    )

    embeddings: list[list[float]] = []
    if chunks:
        embedding_service = get_embedding_service()
        batch_size = 25
        for start in range(0, len(chunks), batch_size):
            heartbeat.ensure_active()
            batch = chunks[start : start + batch_size]
            embeddings.extend(embedding_service.embed([chunk.content for chunk in batch]))

    heartbeat.ensure_active()
    activate_new_build(job.id, loaded, chunks, embeddings, parser_version, chunker_version)

def activate_new_build(job_id: int, loaded: LoadedDocument, chunks: list[Chunk], embeddings: list[list[float]], parser_version: str, chunker_version: str) -> None:
    with transaction() as conn:
        q = query(conn)
        document = q.get_document_by_id(id=loaded.document_id)
        if document is None:
            raise PermanentJobError(f"document {loaded.document_id} disappeared")

        latest_build_no = q.get_latest_build_no(document_id=loaded.document_id) or 0
        build = q.create_index_build(
            document_id=loaded.document_id,
            build_no=latest_build_no + 1,
            content_hash=loaded.content_hash,
            status="building",
        )
        if build is None:
            raise RetryableJobError(f"failed to create build for document {loaded.document_id}")

        for chunk, embedding in zip(chunks, embeddings):
            q.create_chunk(
                CreateChunkParams(
                    document_id=loaded.document_id,
                    build_id=build.id,
                    chunk_index=chunk.index,
                    content=chunk.content,
                    embedding=embedding,
                    section_path=chunk.section_path,
                    semantic_type=chunk.semantic_type,
                    token_count=chunk.token_count,
                    content_hash=chunk.content_hash,
                    start_offset=chunk.start_offset,
                    end_offset=chunk.end_offset,
                    metadata=Jsonb(chunk.metadata),
                )
            )

        old_build_id = document.active_build_id
        q.set_document_active_build(id=document.id, active_build_id=build.id)
        q.update_index_build_status(id=build.id, status="active")
        if old_build_id is not None and old_build_id != build.id:
            q.update_index_build_status(id=old_build_id, status="superseded")

        q.update_document_basics(
            UpdateDocumentBasicsParams(
                id=document.id,
                title=loaded.title,
                mime_type=loaded.mime_type,
                status="active",
                parser_version=parser_version,
                chunker_version=chunker_version,
                last_error=None,
                metadata=Jsonb(loaded.metadata),
            )
        )
        q.complete_job(id=job_id)
