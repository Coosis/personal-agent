"""Document parsing utilities."""
import logging
import mimetypes
from pathlib import Path
from typing import Optional

from unstructured.partition.auto import partition
from unstructured.partition.text import partition_text
from unstructured.chunking.title import chunk_by_title

logger = logging.getLogger(__name__)


def extract_text(file_path: str) -> str:
    """Extract text content from a file.

    Uses unstructured.io for most document types.
    Falls back to plain text reading for simple files.
    """
    path = Path(file_path)
    mime_type, _ = mimetypes.guess_type(file_path)

    logger.info(f"Extracting text from {path.name} (type: {mime_type})")

    try:
        # Use unstructured for rich document types
        if mime_type in (
            "application/pdf",
            "application/msword",
            "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
            "text/html",
            "application/vnd.ms-powerpoint",
            "application/vnd.openxmlformats-officedocument.presentationml.presentation",
        ):
            elements = partition(filename=file_path)
            return "\n\n".join(str(el) for el in elements)

        # Plain text files
        elif mime_type in ("text/plain", "text/markdown", "text/x-markdown"):
            return path.read_text(encoding="utf-8")

        # Code files - read as text
        elif mime_type and mime_type.startswith("text/"):
            return path.read_text(encoding="utf-8")

        # Try unstructured as fallback
        else:
            elements = partition(filename=file_path)
            return "\n\n".join(str(el) for el in elements)

    except Exception as e:
        logger.error(f"Failed to extract text from {file_path}: {e}")
        # Last resort - try reading as binary and decode
        try:
            return path.read_bytes().decode("utf-8", errors="ignore")
        except Exception:
            raise ValueError(f"Cannot extract text from {file_path}: {e}")


def get_document_metadata(file_path: str) -> dict:
    """Extract metadata from a document."""
    path = Path(file_path)
    mime_type, _ = mimetypes.guess_type(file_path)

    return {
        "filename": path.name,
        "extension": path.suffix.lower(),
        "mime_type": mime_type or "application/octet-stream",
        "size_bytes": path.stat().st_size,
    }
