"""Memory retrieval helpers and tool factory."""

import re
from typing import Any

from langchain_core.tools import tool

from agent.context import AppContext
from agent.retrieval.payloads import encode_tool_payload

# TODO! change memory fetch to use pg's full text search
# instead of simple substring match


def search_memories_text(app_ctx: AppContext, query: str) -> str:
    try:
        # simple lexical search over active memories in db
        combined: dict[int, object] = {}
        for candidate in build_memory_queries(query):
            results = app_ctx.db.search_memories(candidate)
            for result in results:
                combined[result.id] = result
        results = list(combined.values())

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
            f"[{citation_id}]\n"
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


# normalize the input and generate candidate queries for memory search
def build_memory_queries(query: str) -> list[str]:
    # normalize whitespace
    # e.g. "  Hello   world \n test  " -> "Hello world test"
    normalized = " ".join(query.strip().split())
    if not normalized:
        return []

    candidates: list[str] = [normalized]

    # finds keywords
    # 1. not a stopword
    # 2. at least 3 characters
    tokens = [
        token
        for token in re.findall(r"[a-z0-9_]+", normalized.lower())
        if len(token) >= 3 and token not in STOPWORDS
    ]

    # 1. concatenate keywords to form a candidate
    # 2. take first 6 keywords individually
    if tokens:
        candidates.append(" ".join(tokens))
        candidates.extend(tokens[:6])

    # filter out duplicates and empty candidates while preserving order
    seen: set[str] = set()
    ordered: list[str] = []
    for candidate in candidates:
        value = candidate.strip()
        if value and value not in seen:
            seen.add(value)
            ordered.append(value)
    return ordered


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
    "is",
    "it",
    "know",
    "last",
    "me",
    "my",
    "of",
    "on",
    "or",
    "that",
    "the",
    "thing",
    "this",
    "to",
    "we",
    "what",
    "who",
    "you",
}
