"""Tool package for agent-side tool wrappers."""

from langchain_core.tools import tool

from agent.context import AppContext


def create_tools(app_ctx: AppContext):
    """Create tool instances bound to the current app context."""

    @tool
    def get_personal_information() -> str:
        """A tool that retrieves user's personal information."""
        return "User is a computer science student at HHU university junior year."

    @tool
    def search_knowledge_base(query: str) -> str:
        """Search the knowledge base for relevant information."""
        try:
            query_vector = app_ctx.embd_svc.embed(query)
            results = app_ctx.db.search_similar_chunks(query_vector, limit=5)

            formatted_results = []
            for result in results:
                source_ref = result.source_item_locator or result.source_locator or result.source_display_name or result.document_title
                formatted_results.append(
                    f"Document: {result.document_title}\n"
                    f"Source: {source_ref}\n"
                    f"Content: {result.content}\n"
                    f"Similarity: {result.vector_score:.2f}\n"
                )

            return "\n".join(formatted_results) if formatted_results else "No relevant information found."
        except Exception as e:
            return f"Error searching knowledge base: {str(e)}"

    return get_personal_information, search_knowledge_base
