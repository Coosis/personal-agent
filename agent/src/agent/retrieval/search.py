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
            f"Similarity: {result.vector_score:.2f}\n"
        )
    return "\n".join(formatted_results)


def create_search_knowledge_base_tool(app_ctx: AppContext):
    """Create the semantic knowledge-base search tool."""

    @tool
    def search_knowledge_base(query: str) -> str:
        """Search the knowledge base for relevant information."""
        try:
            query_vector = app_ctx.embd_svc.embed(query)
            results = app_ctx.db.search_similar_chunks(query_vector, limit=5)
            if not results:
                return "No relevant information found."
            return format_search_results(results)
        except Exception as exc:
            return f"Error searching knowledge base: {str(exc)}"

    return search_knowledge_base
