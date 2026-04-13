"""Retrieval search helpers and tool factory."""

from typing import Any

from langchain_core.tools import tool

from agent.context import AppContext
from agent.retrieval.payloads import encode_tool_payload


def format_search_results(results) -> tuple[str, list[dict[str, Any]]]:
    """Format retrieval results into compact grounding blocks plus citation metadata."""
    formatted_results: list[str] = []
    citations: list[dict[str, Any]] = []
    for index, result in enumerate(results, start=1):
        citation_id = f"D{index}"
        source_ref = (
            result.source_item_locator
            or result.source_locator
            or result.source_display_name
            or result.document_title
        )
        formatted_results.append(
            f"[{citation_id}]\n"
            f"Document: {result.document_title}\n"
            f"Source: {source_ref}\n"
            f"Content: {result.content}\n"
            f"Lexical score: {result.lexical_score:.2f}\n"
            f"Vector score: {result.vector_score:.2f}\n"
            f"Combined score: {result.combined_score:.2f}\n"
        )
        citations.append(
            {
                "id": citation_id,
                "kind": "document_chunk",
                "chunk_id": result.chunk_id,
                "document_id": result.document_id,
                "build_id": result.build_id,
                "chunk_index": result.chunk_index,
                "document_title": result.document_title,
                "source": source_ref,
                "snippet": result.content,
            }
        )
    return "\n".join(formatted_results), citations


def search_knowledge_base_text(app_ctx: AppContext, query: str) -> str:
    """Search the knowledge base and return formatted text."""
    try:
        query_vector = app_ctx.embd_svc.embed(query)
        results = app_ctx.db.search_similar_chunks(query, query_vector)
        if not results:
            return encode_tool_payload("No relevant information found.", [])
        context, citations = format_search_results(results)
        return encode_tool_payload(context, citations)
    except Exception as exc:
        return encode_tool_payload(f"Error searching knowledge base: {str(exc)}", [])


def create_search_knowledge_base_tool(app_ctx: AppContext):
    """Create the semantic knowledge-base search tool."""

    @tool
    def search_knowledge_base(query: str) -> str:
        """Search the knowledge base for relevant information."""
        return search_knowledge_base_text(app_ctx, query)

    return search_knowledge_base
