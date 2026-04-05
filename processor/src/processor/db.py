"""Database operations for the processor."""
import logging
from contextlib import contextmanager
from typing import Any, Generator, List, Optional, cast
import psycopg
from psycopg.rows import dict_row
from psycopg_pool import ConnectionPool

from processor.config import get_settings

logger = logging.getLogger(__name__)

# Type alias for database row
Row = Any

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
def transaction() -> Generator[psycopg.Connection, None, None]:
    """Get a database connection with transaction."""
    with get_connection() as conn:
        with conn.transaction():
            yield conn


class DocumentDB:
    """Document database operations with lease-based distributed locking."""

    @staticmethod
    def get_document_for_processing(conn: psycopg.Connection, max_retries: int = 3) -> Optional[Row]:
        """Get one document available for processing (pending, expired lease, or failed with retries left).
        
        Uses FOR UPDATE SKIP LOCKED to prevent race conditions between workers.
        """
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT id, path, filename, extension, mime_type, 
                       size_bytes, checksum, metadata, processing_status, retry_count
                FROM documents
                WHERE processing_status = 'pending'
                   OR (processing_status = 'processing' AND lease_expires_at < NOW())
                   OR (processing_status = 'failed' AND retry_count < %s)
                ORDER BY created_at
                LIMIT 1
                FOR UPDATE SKIP LOCKED
                """,
                (max_retries,),
            )
            return cur.fetchone()

    @staticmethod
    def acquire_lease(
        conn: psycopg.Connection,
        doc_id: int,
        expires_at: str,  # ISO format timestamp
        worker_id: str,
        max_retries: int = 3,
    ) -> Optional[Row]:
        """Acquire a lease on a document.
        
        Only succeeds if document is pending, lease expired, or failed with retries left.
        Increments retry_count if document was previously failed.
        Returns the document if lease acquired, None otherwise.
        """
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE documents SET
                    processing_status = 'processing',
                    lease_expires_at = %s,
                    leased_by = %s,
                    retry_count = CASE 
                        WHEN processing_status = 'failed' THEN retry_count + 1 
                        ELSE retry_count 
                    END,
                    updated_at = NOW()
                WHERE id = %s
                  AND (processing_status = 'pending' 
                       OR (processing_status = 'processing' AND lease_expires_at < NOW())
                       OR (processing_status = 'failed' AND retry_count < %s))
                RETURNING id, retry_count
                """,
                (expires_at, worker_id, doc_id, max_retries),
            )
            return cur.fetchone()

    @staticmethod
    def renew_lease(
        conn: psycopg.Connection,
        doc_id: int,
        expires_at: str,
        worker_id: str,
    ) -> bool:
        """Renew an existing lease. Returns True if renewed successfully."""
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE documents SET
                    lease_expires_at = %s,
                    updated_at = NOW()
                WHERE id = %s
                  AND processing_status = 'processing'
                  AND leased_by = %s
                RETURNING id
                """,
                (expires_at, doc_id, worker_id),
            )
            result = cur.fetchone()
            return result is not None

    @staticmethod
    def release_lease(conn: psycopg.Connection, doc_id: int) -> None:
        """Release the lease on a document (set to failed or for retry)."""
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE documents SET
                    lease_expires_at = NULL,
                    leased_by = NULL,
                    updated_at = NOW()
                WHERE id = %s
                  AND processing_status = 'processing'
                """,
                (doc_id,),
            )

    @staticmethod
    def complete_processing(
        conn: psycopg.Connection,
        doc_id: int,
        content_hash: str,
    ) -> Optional[Row]:
        """Mark document as completed and release lease."""
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE documents SET
                    processing_status = 'completed',
                    content_hash = %s,
                    lease_expires_at = NULL,
                    leased_by = NULL,
                    indexed_at = NOW(),
                    updated_at = NOW()
                WHERE id = %s
                  AND processing_status = 'processing'
                RETURNING id
                """,
                (content_hash, doc_id),
            )
            return cur.fetchone()

    @staticmethod
    def mark_failed(
        conn: psycopg.Connection,
        doc_id: int,
        error_message: str,
    ) -> None:
        """Mark document as failed and release lease."""
        with conn.cursor() as cur:
            cur.execute(
                """
                UPDATE documents SET
                    processing_status = 'failed',
                    error_message = %s,
                    lease_expires_at = NULL,
                    leased_by = NULL,
                    updated_at = NOW()
                WHERE id = %s
                """,
                (error_message, doc_id),
            )

    @staticmethod
    def create_chunk(
        conn: psycopg.Connection,
        doc_id: int,
        chunk_index: int,
        content: str,
        content_vector: List[float],
        token_count: Optional[int] = None,
        char_count: Optional[int] = None,
        heading_path: Optional[List[str]] = None,
    ) -> int:
        """Create a document chunk with embedding."""
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
            if result is None:
                return 0
            # Handle both dict and tuple results (dict_row factory vs default)
            if isinstance(result, dict):
                result = cast(dict, result)
                return int(result["id"])
            return int(result[0])

    @staticmethod
    def delete_chunks_by_document(conn: psycopg.Connection, doc_id: int) -> None:
        """Delete all chunks for a document (for reprocessing)."""
        with conn.cursor() as cur:
            cur.execute(
                "DELETE FROM chunks WHERE document_id = %s",
                (doc_id,),
            )
