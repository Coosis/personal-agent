"""Main processor entry point with lease-based distributed locking."""
import logging
import signal
import sys
import threading
import time
import uuid
from datetime import UTC, datetime, timedelta

from processor.config import get_settings
from processor.db import (
    DocumentDB,
    init_pool,
    close_pool,
    transaction,
)
from processor.parsing import extract_text
from processor.chunking import chunk_document
from processor.embedding import get_embedding_service

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
)
logger = logging.getLogger(__name__)

# Global flag for graceful shutdown
shutdown_requested = False

# Unique worker ID for lease ownership
WORKER_ID = str(uuid.uuid4())[:8]

# Lease configuration
LEASE_DURATION_SECONDS = 60  # 1 minute lease
HEARTBEAT_INTERVAL_SECONDS = 30  # Renew every 30s
MAX_RETRIES = 3  # Max retry attempts for failed documents


def signal_handler(signum, frame):
    """Handle shutdown signals."""
    global shutdown_requested
    logger.info("Shutdown signal received, finishing current work...")
    shutdown_requested = True


def calculate_lease_expiry() -> str:
    """Calculate lease expiry timestamp."""
    expiry = datetime.now(UTC) + timedelta(seconds=LEASE_DURATION_SECONDS)
    return expiry.isoformat()


class Heartbeat:
    """Background thread to renew document lease."""
    
    def __init__(self, doc_id: int):
        self.doc_id = doc_id
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self._run, daemon=True)
        self.last_renewal_success = True
    
    def start(self):
        """Start the heartbeat thread."""
        self.thread.start()
        logger.debug(f"Heartbeat started for document {self.doc_id}")
    
    def stop(self):
        """Stop the heartbeat thread."""
        self.stop_event.set()
        self.thread.join(timeout=5)
        logger.debug(f"Heartbeat stopped for document {self.doc_id}")
    
    def _run(self):
        """Renew lease periodically until stopped."""
        while not self.stop_event.is_set():
            # Wait for heartbeat interval or stop signal
            if self.stop_event.wait(HEARTBEAT_INTERVAL_SECONDS):
                break
            
            try:
                with transaction() as conn:
                    success = DocumentDB.renew_lease(
                        conn,
                        self.doc_id,
                        calculate_lease_expiry(),
                        WORKER_ID,
                    )
                    self.last_renewal_success = success
                    if success:
                        logger.debug(f"Lease renewed for document {self.doc_id}")
                    else:
                        logger.warning(f"Failed to renew lease for document {self.doc_id}")
            except Exception as e:
                logger.error(f"Heartbeat error for document {self.doc_id}: {e}")
                self.last_renewal_success = False


def process_document(doc: dict) -> bool:
    """Process a single document through the pipeline with lease heartbeat.
    
    Lease is already acquired by poll_and_process(). This function does the
    heavy work (parsing, chunking, embeddings) and stores results.
    
    If crash happens during work, lease expires and another worker picks it up.
    """
    doc_id = doc["id"]
    file_path = doc["path"]
    extension = doc.get("extension", "")
    
    logger.info(f"[{WORKER_ID}] Processing document {doc_id}: {file_path}")
    
    # Start heartbeat to keep lease alive during processing
    heartbeat = Heartbeat(doc_id)
    heartbeat.start()
    
    try:
        # Step 1: Extract text (outside transaction - no DB lock held)
        logger.debug(f"Extracting text from {file_path}")
        content = extract_text(file_path)
        
        if not content or not content.strip():
            logger.warning(f"No content extracted from {file_path}")
            with transaction() as conn:
                DocumentDB.complete_processing(conn, doc_id, "")
            return True
        
        # Step 2: Chunk document
        logger.debug(f"Chunking document {doc_id}")
        settings = get_settings()
        chunks = chunk_document(
            content,
            extension,
            chunk_size=settings.chunk_size,
            chunk_overlap=settings.chunk_overlap,
        )
        
        if not chunks:
            logger.warning(f"No chunks generated for {file_path}")
            with transaction() as conn:
                DocumentDB.complete_processing(conn, doc_id, "")
            return True
        
        logger.info(f"Generated {len(chunks)} chunks for document {doc_id}")
        
        # Step 3: Get embeddings (API calls - lease kept alive by heartbeat)
        embedding_service = get_embedding_service()
        all_embeddings = []
        batch_size = 25  # Alibaba API limit
        
        for i in range(0, len(chunks), batch_size):
            if shutdown_requested:
                logger.info("Shutdown requested, stopping batch processing")
                raise InterruptedError("Shutdown requested")
            
            # Check if heartbeat is still working
            if not heartbeat.last_renewal_success:
                raise RuntimeError("Lost lease during processing")
            
            batch = chunks[i : i + batch_size]
            texts = [chunk.content for chunk in batch]
            
            # Skip empty batches
            if not texts or all(not t or not t.strip() for t in texts):
                logger.warning(f"Skipping empty batch {i//batch_size + 1}")
                continue
            
            # Debug: check for empty texts
            for j, text in enumerate(texts):
                if not text or not text.strip():
                    logger.warning(f"Empty text at batch position {j}, chunk index {batch[j].index}")
            
            logger.debug(f"Getting embeddings for batch {i//batch_size + 1} ({len(texts)} texts)")
            embeddings = embedding_service.embed(texts)
            all_embeddings.extend(embeddings)
        
        # Transaction 2: Store chunks and mark complete
        content_hash = doc.get("checksum", "")
        
        with transaction() as conn:
            # Verify we still hold the lease
            renewed = DocumentDB.renew_lease(
                conn, doc_id, calculate_lease_expiry(), WORKER_ID
            )
            if not renewed:
                raise RuntimeError("Lost lease before final commit")
            
            # Store all chunks
            for chunk, embedding in zip(chunks, all_embeddings):
                DocumentDB.create_chunk(
                    conn=conn,
                    doc_id=doc_id,
                    chunk_index=chunk.index,
                    content=chunk.content,
                    content_vector=embedding,
                    token_count=chunk.token_count,
                    char_count=chunk.char_count,
                    heading_path=chunk.heading_path,
                )
            
            # Mark as completed (releases lease)
            DocumentDB.complete_processing(conn, doc_id, content_hash)
        
        logger.info(f"Successfully processed document {doc_id}")
        return True
        
    except InterruptedError:
        # Clean shutdown - release lease so another worker can pick it up
        logger.info(f"Releasing lease for document {doc_id} due to shutdown")
        with transaction() as conn:
            DocumentDB.release_lease(conn, doc_id)
        raise
        
    except Exception as e:
        logger.exception(f"Failed to process document {doc_id}: {e}")
        # Release lease so another worker can retry
        try:
            with transaction() as conn:
                DocumentDB.mark_failed(conn, doc_id, str(e)[:500])
        except Exception as e2:
            logger.error(f"Failed to mark document as failed: {e2}")
        return False
        
    finally:
        # Stop heartbeat
        heartbeat.stop()


def poll_and_process() -> bool:
    """Poll for one document and process it. Returns True if work was done.
    
    Uses a single transaction to both select and acquire lease atomically.
    The FOR UPDATE SKIP LOCKED prevents race conditions between workers.
    """
    doc = None
    
    with transaction() as conn:
        # Select and lock the document (includes failed docs with retries left)
        row = DocumentDB.get_document_for_processing(conn, max_retries=MAX_RETRIES)
        
        if row is None:
            return False
        
        doc = dict(row)
        doc_id = doc["id"]
        
        # Acquire lease in the SAME transaction while holding the row lock
        # Increments retry_count if document was previously failed
        result = DocumentDB.acquire_lease(
            conn,
            doc_id,
            calculate_lease_expiry(),
            WORKER_ID,
            max_retries=MAX_RETRIES,
        )
        
        if result is None:
            logger.warning(f"Failed to acquire lease for document {doc_id}")
            return False
        
        # Delete existing chunks if any (still in the same tx)
        DocumentDB.delete_chunks_by_document(conn, doc_id)
        
        # Log retry info if this was a failed document
        prev_status = doc.get('processing_status')
        retry_info = ""
        if prev_status == 'failed':
            new_retry_count = result.get('retry_count', 0) if result else 0
            retry_info = f" (retry {new_retry_count}/{MAX_RETRIES})"
        
        logger.info(f"[{WORKER_ID}] Acquired lease on document {doc_id}: {doc['path']}{retry_info}")
    
    # Now process outside transaction - lease is held, row lock released
    return process_document(doc)


def run_processor():
    """Main processor loop."""
    settings = get_settings()
    logger.info(f"[{WORKER_ID}] Starting processor")
    logger.info(f"Lease duration: {LEASE_DURATION_SECONDS}s")
    logger.info(f"Heartbeat interval: {HEARTBEAT_INTERVAL_SECONDS}s")
    logger.info(f"Max retries for failed docs: {MAX_RETRIES}")
    
    # Initialize database pool
    init_pool()
    
    try:
        while not shutdown_requested:
            try:
                did_work = poll_and_process()
                
                if not did_work:
                    # No work, sleep and retry
                    time.sleep(settings.poll_interval_seconds)
                    
            except InterruptedError:
                # Clean shutdown
                break
            except Exception as e:
                logger.exception(f"Error in processor loop: {e}")
                time.sleep(1)
                
    finally:
        logger.info("Shutting down processor...")
        close_pool()


def main():
    """Entry point."""
    # Register signal handlers
    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)
    
    try:
        run_processor()
    except Exception as e:
        logger.exception(f"Processor crashed: {e}")
        sys.exit(1)


if __name__ == "__main__":
    main()
