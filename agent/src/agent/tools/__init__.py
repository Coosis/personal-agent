"""Tool package for agent-side tool wrappers."""

from agent.context import AppContext
from agent.retrieval import (
    create_get_profile_context_tool,
    create_search_knowledge_base_tool,
    create_search_memories_tool,
    create_search_previous_chats_tool,
)


def create_tools(app_ctx: AppContext):
    """Create tool instances bound to the current app context."""
    return [
        create_get_profile_context_tool(app_ctx),
        create_search_previous_chats_tool(app_ctx),
        create_search_memories_tool(app_ctx),
        create_search_knowledge_base_tool(app_ctx),
    ]
