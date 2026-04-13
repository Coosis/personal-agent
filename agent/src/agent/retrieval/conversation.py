"""Conversation summary retrieval helpers and tool factory."""

from itertools import combinations
import re
from typing import Any

from langchain_core.tools import tool

from agent.citations import format_citation_marker
from agent.context import AppContext
from agent.retrieval.payloads import encode_tool_payload


def format_conversation_summary_results(
    results,
) -> tuple[str, list[dict[str, Any]]]:
    formatted: list[str] = []
    citations: list[dict[str, Any]] = []

    for index, result in enumerate(results, start=1):
        citation_id = f"H{index}"
        formatted.append(
            f"{format_citation_marker(citation_id)}\n"
            f"Conversation ID: {result.conversation_id}\n"
            f"Pass index: {result.pass_index}\n"
            f"Summary: {result.summary_text}\n"
            f"State: {result.state_text}\n"
            f"Keywords: {', '.join(result.keywords)}\n"
            f"Lexical score: {result.lexical_score:.2f}\n"
        )
        citations.append(
            {
                "id": citation_id,
                "kind": "conversation_summary",
                "conversation_summary_id": result.id,
                "conversation_id": result.conversation_id,
                "last_message_id": result.last_message_id,
                "pass_index": result.pass_index,
                "summary_text": result.summary_text,
                "keywords": result.keywords,
            }
        )

    return "\n".join(formatted), citations


def search_previous_chats_text(app_ctx: AppContext, query: str) -> str:
    try:
        search_queries = build_conversation_search_queries(query)
        if not search_queries:
            return encode_tool_payload("No previous-chat query provided.", [])

        results = []
        seen_summary_ids: set[int] = set()
        for candidate in search_queries:
            for result in app_ctx.db.search_conversation_summaries(candidate):
                if result.id in seen_summary_ids:
                    continue
                seen_summary_ids.add(result.id)
                results.append(result)
                if len(results) >= 5:
                    break
            if len(results) >= 5:
                break

        if not results:
            return encode_tool_payload("No relevant previous chat summaries found.", [])

        context, citations = format_conversation_summary_results(results)
        return encode_tool_payload(context, citations)
    except Exception as exc:
        return encode_tool_payload(f"Error searching previous chats: {str(exc)}", [])


def create_search_previous_chats_tool(app_ctx: AppContext):
    """Create the previous-chat search tool."""

    @tool
    def search_previous_chats(query: str) -> str:
        """Search previous chat summaries for prior discussions, active topics, and open questions. Prefer short noun phrases and broad topic/category terms over long natural-language questions."""
        return search_previous_chats_text(app_ctx, query)

    return search_previous_chats


def normalize_conversation_query(query: str) -> str:
    return " ".join(query.strip().split())


def build_conversation_search_queries(query: str) -> list[str]:
    normalized = normalize_conversation_query(query)
    if not normalized:
        return []

    terms = extract_conversation_terms(normalized)
    candidates: list[str] = []
    seen: set[str] = set()

    def add_candidate(candidate: str) -> None:
        value = normalize_conversation_query(candidate)
        if not value or value in seen:
            return
        seen.add(value)
        candidates.append(value)

    keyword_query = " ".join(terms)
    if keyword_query:
        add_candidate(keyword_query)

    prioritized_terms = prioritize_conversation_terms(terms)
    for size in range(min(3, len(prioritized_terms)), 1, -1):
        for combo in combinations(prioritized_terms[:5], size):
            add_candidate(" ".join(combo))

    for term in prioritized_terms:
        add_candidate(term)

    add_candidate(normalized)
    return candidates[:12]


def extract_conversation_terms(query: str) -> list[str]:
    values: list[str] = []
    seen: set[str] = set()
    for token in re.findall(r"[a-z0-9_]+", query.lower()):
        if len(token) < 2 or token in STOPWORDS:
            continue
        for variant in expand_conversation_term(token):
            if variant in seen:
                continue
            seen.add(variant)
            values.append(variant)
    return values


def prioritize_conversation_terms(terms: list[str]) -> list[str]:
    return sorted(
        terms,
        key=lambda term: (term in BROAD_TERMS, -len(term), term),
    )


def expand_conversation_term(term: str) -> list[str]:
    variants = TERM_ALIASES.get(term, [])
    return [term, *variants]


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


BROAD_TERMS = {
    "chat",
    "consider",
    "consideration",
    "conversation",
    "decision",
    "discussion",
    "project",
    "question",
    "talk",
    "topic",
}


TERM_ALIASES = {
    "db": ["database", "sql"],
    "database": ["db", "sql"],
    "postgres": ["postgresql", "sql", "database"],
    "postgresql": ["postgres", "sql", "database"],
    "mysql": ["sql", "database"],
    "dynamodb": ["nosql", "database"],
}
