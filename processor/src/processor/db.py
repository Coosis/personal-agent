"""Database operations for the processor."""
import logging
from contextlib import contextmanager
from typing import Generator, List, Optional
import psycopg
from psycopg.rows import dict_row
from psycopg_pool import ConnectionPool

from processor.config import get_settings

logger = logging.getLogger(__name__)

# Global connection pool
_pool: Optional[ConnectionPool] = None


def init_pool() -> ConnectionPool:
    """Initialize the database connection pool."""
    global _pool
    if _pool is None:
        settings = get_settings()
        _pool = ConnectionPool(
            conninfo=settings.database_url,
            min_size=1,
            max_size=settings.max_workers + 2,
            kwargs={"row_factory": dict_row},
        )
        logger.info("Database pool initialized")
    return _pool


def close_pool() -> None:
    """Close the database connection pool."""
    global _pool
    if _pool is not None:
        _pool.close()
        _pool = None
        logger.info("Database pool closed")


@contextmanager
def get_connection() -> Generator[psycopg.Connection, None, None]:
    """Get a database connection from the pool."""
    pool = init_pool()
    with pool.connection() as conn:
        yield conn


@contextmanager
def get_transaction() -> Generator[psycopg.Connection, None, None]:
    """Get a database connection with transaction."""
    with get_connection() as conn:
        with conn.transaction():
            yield conn


class DocumentDB:
    """Document database operations."""

    @staticmethod
    def get_pending_documents(limit: int = 10) -> List[dict]:
        """Get documents waiting for processing."""
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    SELECT id, path, filename, extension, mime_type, 
                           size_bytes, checksum, metadata
                    FROM documents
                    WHERE processing_status = 'pending'
                    ORDER BY created_at
                    LIMIT %s
                    FOR UPDATE SKIP LOCKED
                    """,
                    (limit,),
                )
                return cur.fetchall()

    @staticmethod
    def update_document_status(
        doc_id: int, status: str, error_message: Optional[str] = None
    ) -> None:
        """Update document processing status."""
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    UPDATE documents
                    SET processing_status = %s,
                        error_message = %s,
                        updated_at = NOW()
                    WHERE id = %s
                    """,
                    (status, error_message, doc_id),
                )

    @staticmethod
    def create_chunk(
        doc_id: int,
        chunk_index: int,
        content: str,
        content_vector: List[float],
        token_count: Optional[int] = None,
        char_count: Optional[int] = None,
        heading_path: Optional[List[str]] = None,
    ) -> int:
        """Create a document chunk with embedding."""
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    INSERT INTO chunks (
                        document_id, chunk_index, content, content_vector,
                        token_count, char_count, heading_path, metadata
                    ) VALUES (%s, %s, %s, %s, %s, %s, %s, '{}')
                    RETURNING id
                    """,
                    (
                        doc_id,
                        chunk_index,
                        content,
                        content_vector,
                        token_count,
                        char_count,
                        heading_path or [],
                    ),
                )
                result = cur.fetchone()
                return result["id"] if result else 0

    @staticmethod
    def delete_chunks_by_document(doc_id: int) -> None:
        """Delete all chunks for a document (for reprocessing)."""
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    "DELETE FROM chunks WHERE document_id = %s",
                    (doc_id,),
                )

    @staticmethod
    def update_document_content_hash(doc_id: int, content_hash: str) -> None:
        """Update the content hash of a processed document."""
        with get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    UPDATE documents
                    SET content_hash = %s,
                        indexed_at = NOW(),
                        updated_at = NOW()
                    WHERE id = %s
                    """,
                    (content_hash, doc_id),
                )
