"""Tool package for agent-side tool wrappers."""

from agent.context import AppContext
from agent.retrieval import create_search_knowledge_base_tool
from agent.tools.personal import create_personal_information_tool


def create_tools(app_ctx: AppContext):
    """Create tool instances bound to the current app context."""
    return [
        # create_personal_information_tool(),
        create_search_knowledge_base_tool(app_ctx),
    ]
