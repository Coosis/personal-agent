"""Retrieval search helpers and tool factory."""

from langchain_core.tools import tool

from agent.context import AppContext


def format_search_results(results) -> str:
    """Format retrieval results into a compact grounding block."""
    formatted_results: list[str] = []
    for result in results:
        source_ref = (
            result.source_item_locator
            or result.source_locator
            or result.source_display_name
            or result.document_title
        )
        formatted_results.append(
            f"Document: {result.document_title}\n"
            f"Source: {source_ref}\n"
            f"Content: {result.content}\n"
            f"Lexical score: {result.lexical_score:.2f}\n"
            f"Vector score: {result.vector_score:.2f}\n"
            f"Combined score: {result.combined_score:.2f}\n"
        )
    return "\n".join(formatted_results)


def search_knowledge_base_text(app_ctx: AppContext, query: str) -> str:
    """Search the knowledge base and return formatted text."""
    try:
        query_vector = app_ctx.embd_svc.embed(query)
        results = app_ctx.db.search_similar_chunks(query, query_vector)
        if not results:
            return "No relevant information found."
        return format_search_results(results)
    except Exception as exc:
        return f"Error searching knowledge base: {str(exc)}"


def create_search_knowledge_base_tool(app_ctx: AppContext):
    """Create the semantic knowledge-base search tool."""

    @tool
    def search_knowledge_base(query: str) -> str:
        """Search the knowledge base for relevant information."""
        return search_knowledge_base_text(app_ctx, query)

    return search_knowledge_base
