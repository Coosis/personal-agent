"""Memory retrieval helpers and tool factory."""

import re
from typing import Any

from langchain_core.tools import tool

from agent.citations import format_citation_marker
from agent.context import AppContext
from agent.retrieval.payloads import encode_tool_payload


def search_memories_text(app_ctx: AppContext, query: str) -> str:
    try:
        search_queries = build_memory_search_queries(query)
        if not search_queries:
            return get_profile_context_text(app_ctx)

        results = []
        seen_memory_ids: set[int] = set()
        for candidate in search_queries:
            for result in app_ctx.db.search_memories(candidate):
                if result.id in seen_memory_ids:
                    continue
                seen_memory_ids.add(result.id)
                results.append(result)
                if len(results) >= 8:
                    break
            if len(results) >= 8:
                break

        # no results from search, falling back
        # to searching "user" in profile memories
        if not results:
            fallback = app_ctx.db.get_profile_memories(subject="user")
            if not fallback:
                return encode_tool_payload("No relevant memories found.", [])
            context, citations = format_memory_results(fallback)
            return encode_tool_payload(
                "No exact memory match found. "
                + "Returning active user memory context:\n\n"
                + context,
                citations,
            )

        context, citations = format_memory_results(results)
        return encode_tool_payload(context, citations)

    except Exception as exc:
        return encode_tool_payload(f"Error searching memories: {str(exc)}", [])


def get_profile_context_text(app_ctx: AppContext, subject: str = "user") -> str:
    try:
        results = app_ctx.db.get_profile_memories(subject=subject)
        if not results:
            return encode_tool_payload("No active profile memories found.", [])
        context, citations = format_memory_results(results)
        return encode_tool_payload(context, citations)
    except Exception as exc:
        return encode_tool_payload(f"Error loading profile context: {str(exc)}", [])


def create_search_memories_tool(app_ctx: AppContext):
    """Create the memory search tool."""

    @tool
    def search_memories(query: str) -> str:
        """Search stored memories for relevant personal or project context."""
        return search_memories_text(app_ctx, query)

    return search_memories


def create_get_profile_context_tool(app_ctx: AppContext):
    """Create the profile context tool."""

    @tool
    def get_profile_context(subject: str = "user") -> str:
        """Load active profile memories for a subject, defaulting to the user."""
        return get_profile_context_text(app_ctx, subject)

    return get_profile_context


def format_memory_results(results) -> tuple[str, list[dict[str, Any]]]:
    formatted: list[str] = []
    citations: list[dict[str, Any]] = []
    for index, result in enumerate(results, start=1):
        citation_id = f"M{index}"
        provenance: list[str] = []
        if result.source_id is not None:
            provenance.append(f"source_id={result.source_id}")
        if result.document_id is not None:
            provenance.append(f"document_id={result.document_id}")
        if result.message_id is not None:
            provenance.append(f"message_id={result.message_id}")

        provenance_text = ", ".join(provenance) if provenance else "none"
        formatted.append(
            f"{format_citation_marker(citation_id)}\n"
            f"Memory ID: {result.id}\n"
            f"Subject: {result.subject}\n"
            f"Category: {result.category}\n"
            f"Key: {result.key}\n"
            f"Value: {result.value}\n"
            f"Confidence: {result.confidence:.2f}\n"
            f"Provenance: {provenance_text}\n"
        )
        citations.append(
            {
                "id": citation_id,
                "kind": "memory",
                "memory_id": result.id,
                "subject": result.subject,
                "category": result.category,
                "key": result.key,
                "value": result.value,
                "confidence": result.confidence,
                "source_id": result.source_id,
                "document_id": result.document_id,
                "message_id": result.message_id,
            }
        )
    return "\n".join(formatted), citations


def normalize_memory_query(query: str) -> str:
    return " ".join(query.strip().split())


def build_memory_search_queries(query: str) -> list[str]:
    normalized = normalize_memory_query(query)
    if not normalized:
        return []

    candidates: list[str] = []
    keyword_query = build_keyword_memory_query(normalized)
    if keyword_query:
        candidates.append(keyword_query)
    if normalized not in candidates:
        candidates.append(normalized)
    return candidates


def build_keyword_memory_query(query: str) -> str:
    tokens = [
        token
        for token in re.findall(r"[a-z0-9_]+", query.lower())
        if len(token) >= 2 and token not in STOPWORDS
    ]
    return " ".join(tokens)


STOPWORDS = {
    "a",
    "about",
    "am",
    "an",
    "and",
    "are",
    "as",
    "at",
    "be",
    "did",
    "do",
    "for",
    "from",
    "how",
    "i",
    "if",
    "in",
    "is",
    "it",
    "know",
    "last",
    "me",
    "my",
    "of",
    "on",
    "or",
    "our",
    "that",
    "the",
    "thing",
    "this",
    "to",
    "we",
    "what",
    "which",
    "who",
    "with",
    "you",
    "your",
}
