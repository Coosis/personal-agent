"""Semantic chunking strategies."""

from __future__ import annotations

import hashlib
import logging
from dataclasses import dataclass, field
from typing import Any, List, Optional

from langchain_text_splitters import (
    MarkdownHeaderTextSplitter,
    RecursiveCharacterTextSplitter,
    Language,
)

logger = logging.getLogger(__name__)


@dataclass
class Chunk:
    """Processor-side chunk object before DB insert."""

    content: str
    index: int
    section_path: List[str] = field(default_factory=list)
    semantic_type: Optional[str] = None
    token_count: Optional[int] = None
    start_offset: Optional[int] = None
    end_offset: Optional[int] = None
    content_hash: Optional[str] = None
    metadata: dict[str, Any] = field(default_factory=dict)


def chunk_document(content: str, extension: str, chunk_size: int = 512, chunk_overlap: int = 50) -> List[Chunk]:
    """Chunk a document based on its type."""
    
    # Skip empty content
    if not content or not content.strip():
        return []
    
    # Markdown files - use header-based chunking
    if extension in (".md", ".markdown"):
        chunks = chunk_markdown(content, chunk_size, chunk_overlap)
    
    # Code files - use language-aware chunking
    elif extension in (".py", ".js", ".ts", ".go", ".java", ".cpp", ".c", ".rs"):
        chunks = chunk_code(content, extension, chunk_size, chunk_overlap)
    
    # Default - recursive character chunking
    else:
        chunks = chunk_recursive(content, chunk_size, chunk_overlap)
    
    filtered: List[Chunk] = []
    for chunk in chunks:
        if not chunk.content or not chunk.content.strip():
            continue
        chunk.index = len(filtered)
        chunk.token_count = approximate_token_count(chunk.content)
        chunk.content_hash = hashlib.sha256(chunk.content.encode("utf-8")).hexdigest()
        filtered.append(chunk)

    if len(filtered) < len(chunks):
        logger.debug("Filtered %s empty chunks", len(chunks) - len(filtered))

    return filtered


def chunk_markdown(content: str, chunk_size: int, chunk_overlap: int) -> List[Chunk]:
    """Chunk markdown documents by headers."""
    # First split by headers
    headers_to_split_on = [
        ("#", "Header 1"),
        ("##", "Header 2"),
        ("###", "Header 3"),
    ]
    
    markdown_splitter = MarkdownHeaderTextSplitter(
        headers_to_split_on=headers_to_split_on,
        strip_headers=False,
    )
    
    try:
        md_chunks = markdown_splitter.split_text(content)
    except Exception:
        # Fallback to recursive if markdown parsing fails
        return chunk_recursive(content, chunk_size, chunk_overlap)
    
    # Further split large chunks
    text_splitter = RecursiveCharacterTextSplitter(
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
        length_function=len,
    )
    
    chunks = []
    for _, doc in enumerate(md_chunks):
        # Get header path from metadata
        heading_path = []
        if "Header 1" in doc.metadata:
            heading_path.append(doc.metadata["Header 1"])
        if "Header 2" in doc.metadata:
            heading_path.append(doc.metadata["Header 2"])
        if "Header 3" in doc.metadata:
            heading_path.append(doc.metadata["Header 3"])
        
        # If chunk is too large, split it further
        if len(doc.page_content) > chunk_size * 1.5:
            sub_chunks = text_splitter.split_text(doc.page_content)
            for _, sub_content in enumerate(sub_chunks):
                chunks.append(Chunk(
                    content=sub_content,
                    index=len(chunks),
                    section_path=heading_path.copy(),
                    semantic_type="section",
                    metadata={"char_count": len(sub_content)},
                ))
        else:
            chunks.append(Chunk(
                content=doc.page_content,
                index=len(chunks),
                section_path=heading_path.copy(),
                semantic_type="section",
                metadata={"char_count": len(doc.page_content)},
            ))
    
    return chunks


def chunk_code(content: str, extension: str, chunk_size: int, chunk_overlap: int) -> List[Chunk]:
    """Chunk code files using language-aware splitter."""
    language_map = {
        ".py": Language.PYTHON,
        ".js": Language.JS,
        ".ts": Language.TS,
        ".go": Language.GO,
        ".java": Language.JAVA,
        ".cpp": Language.CPP,
        ".c": Language.C,
        ".rs": Language.RUST,
    }
    
    language = language_map.get(extension)
    
    if language:
        splitter = RecursiveCharacterTextSplitter.from_language(
            language=language,
            chunk_size=chunk_size,
            chunk_overlap=chunk_overlap,
        )
    else:
        splitter = RecursiveCharacterTextSplitter(
            chunk_size=chunk_size,
            chunk_overlap=chunk_overlap,
        )
    
    texts = splitter.split_text(content)
    
    return [
        Chunk(
            content=text,
            index=i,
            semantic_type="code",
            metadata={"char_count": len(text)},
        )
        for i, text in enumerate(texts)
    ]


def chunk_recursive(content: str, chunk_size: int, chunk_overlap: int) -> List[Chunk]:
    """Default recursive character chunking."""
    splitter = RecursiveCharacterTextSplitter(
        chunk_size=chunk_size,
        chunk_overlap=chunk_overlap,
        separators=["\n\n", "\n", ". ", "! ", "? ", " ", ""],
    )
    
    texts = splitter.split_text(content)
    
    return [
        Chunk(
            content=text,
            index=i,
            metadata={"char_count": len(text)},
        )
        for i, text in enumerate(texts)
    ]


def approximate_token_count(text: str) -> int:
    return len(text.split())
