from langchain_core.tools import tool

from agent.context import AppContext
from agent.retrieval.memory import (
    get_profile_context_text,
    search_memories_text,
)


def create_search_memories_tool(app_ctx: AppContext):
    @tool
    def search_memories(query: str) -> str:
        """
        Search accepted user memories.
        Use this first for stable personal facts,
        preferences, relationships,
        and ongoing projects.
        """
        return search_memories_text(app_ctx, query)

    return search_memories


def create_get_profile_context_tool(app_ctx: AppContext):
    @tool
    def get_profile_context(subject: str = "user") -> str:
        """
        Get compact canonical memory context for a subject,
        usually the user.
        """
        return get_profile_context_text(app_ctx, subject)

    return get_profile_context
