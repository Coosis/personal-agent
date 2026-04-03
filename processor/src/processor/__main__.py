"""Main processor entry point."""
import logging
import signal
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

from processor.config import get_settings
from processor.db import (
    DocumentDB,
    init_pool,
    close_pool,
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


def signal_handler(signum, frame):
    """Handle shutdown signals."""
    global shutdown_requested
    logger.info("Shutdown signal received, finishing current work...")
    shutdown_requested = True


def process_document(doc: dict) -> bool:
    """Process a single document through the pipeline."""
    doc_id = doc["id"]
    file_path = doc["path"]
    extension = doc.get("extension", "")

    logger.info(f"Processing document {doc_id}: {file_path}")

    try:
        # Update status to processing
        DocumentDB.update_document_status(doc_id, "processing")

        # Step 1: Delete existing chunks (for reprocessing)
        DocumentDB.delete_chunks_by_document(doc_id)

        # Step 2: Extract text
        logger.debug(f"Extracting text from {file_path}")
        content = extract_text(file_path)

        if not content or not content.strip():
            logger.warning(f"No content extracted from {file_path}")
            DocumentDB.update_document_status(doc_id, "completed")
            return True

        # Step 3: Chunk document
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
            DocumentDB.update_document_status(doc_id, "completed")
            return True

        logger.info(f"Generated {len(chunks)} chunks for document {doc_id}")

        # Step 4: Get embeddings and store chunks
        embedding_service = get_embedding_service()
        
        # Process chunks in batches for embedding
        batch_size = 25  # Alibaba API limit
        for i in range(0, len(chunks), batch_size):
            batch = chunks[i:i + batch_size]
            texts = [chunk.content for chunk in batch]
            
            logger.debug(f"Getting embeddings for batch {i//batch_size + 1}")
            embeddings = embedding_service.embed(texts)
            
            # Store chunks with embeddings
            for chunk, embedding in zip(batch, embeddings):
                DocumentDB.create_chunk(
                    doc_id=doc_id,
                    chunk_index=chunk.index,
                    content=chunk.content,
                    content_vector=embedding,
                    token_count=chunk.token_count,
                    char_count=chunk.char_count,
                    heading_path=chunk.heading_path,
                )

        # Step 5: Update document status to completed
        content_hash = doc.get("checksum", "")  # Use existing checksum
        DocumentDB.update_document_content_hash(doc_id, content_hash)
        DocumentDB.update_document_status(doc_id, "completed")

        logger.info(f"Successfully processed document {doc_id}")
        return True

    except Exception as e:
        logger.exception(f"Failed to process document {doc_id}: {e}")
        DocumentDB.update_document_status(doc_id, "failed", str(e)[:500])
        return False


def run_processor():
    """Main processor loop."""
    settings = get_settings()
    logger.info(f"Starting processor with {settings.max_workers} workers")
    logger.info(f"Poll interval: {settings.poll_interval_seconds}s")
    logger.info(f"Batch size: {settings.batch_size}")

    # Initialize database pool
    init_pool()

    try:
        with ThreadPoolExecutor(max_workers=settings.max_workers) as executor:
            while not shutdown_requested:
                # Get pending documents
                docs = DocumentDB.get_pending_documents(settings.batch_size)

                if not docs:
                    # No work, sleep and retry
                    time.sleep(settings.poll_interval_seconds)
                    continue

                logger.info(f"Found {len(docs)} pending documents")

                # Submit all documents to thread pool
                futures = {
                    executor.submit(process_document, doc): doc
                    for doc in docs
                }

                # Wait for completion
                for future in as_completed(futures):
                    doc = futures[future]
                    try:
                        future.result()
                    except Exception as e:
                        logger.exception(f"Document {doc['id']} failed: {e}")

                # Small delay before next poll
                if not shutdown_requested:
                    time.sleep(0.1)

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
