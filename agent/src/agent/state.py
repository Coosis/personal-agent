"""State definitions for the agent graph."""

from typing import TypedDict, Annotated, cast
from langchain_core.messages import AIMessage
from langgraph.graph.message import add_messages, AnyMessage

class AgentState(TypedDict):
    user_input: str
    messages: Annotated[list[AnyMessage], add_messages]
    summarized_input: AIMessage
    plan: AIMessage
    final_answer: str

class AgentStateUpdate(TypedDict, total=False):
    user_input: str
    messages: Annotated[list[AnyMessage], add_messages]
    summarized_input: AIMessage
    plan: AIMessage
    final_answer: str

def should_call_tool(state: AgentState) -> bool:
    msgs = state.get("messages")
    if msgs is None or len(msgs) == 0:
        return False
    last_msg = msgs[-1]
    if not isinstance(last_msg, AIMessage):
        return False
    last_msg = cast(AIMessage, last_msg)
    return last_msg.tool_calls is not None and len(last_msg.tool_calls) > 0
