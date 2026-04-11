"""Prompt placeholders for the agent skeleton."""

SYSTEM_PROMPT = """
You are a grounded assistant operating on local knowledge.
"""

def build_router_prompt() -> str:
    return "Decide whether to answer directly, retrieve context, or call a tool."
