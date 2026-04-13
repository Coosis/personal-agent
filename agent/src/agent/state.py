"""State definitions for the agent graph."""

from typing import Annotated, TypedDict, cast

from langchain_core.messages import AIMessage
from langgraph.graph.message import AnyMessage, add_messages

class AgentState(TypedDict):
    user_input: str
    messages: Annotated[list[AnyMessage], add_messages]
    conversation_messages: Annotated[list[AnyMessage], add_messages]

    intent: str
    retrieved_context: str
    retrieved_citations: list[dict[str, object]]
    latest_observation: str
    question_type: str
    knowledge_scope: str
    needs_retrieval: bool
    retrieval_query: str
    missing_information: str
    react_step_count: int
    observed_tool_messages: int
    next_citation_number: int
    should_continue: bool

    final_answer: str
    final_citations: list[dict[str, object]]

class AgentStateUpdate(TypedDict, total=False):
    user_input: str
    messages: Annotated[list[AnyMessage], add_messages]
    conversation_messages: Annotated[list[AnyMessage], add_messages]

    intent: str
    retrieved_context: str
    retrieved_citations: list[dict[str, object]]
    latest_observation: str
    question_type: str
    knowledge_scope: str
    needs_retrieval: bool
    retrieval_query: str
    missing_information: str
    react_step_count: int
    observed_tool_messages: int
    next_citation_number: int
    should_continue: bool

    final_answer: str
    final_citations: list[dict[str, object]]

def pretty_print_state(state: AgentState) -> dict:
    return {
        "user_input": state["user_input"],
        "messages": [str(msg) for msg in state["messages"]],
        "conversation_messages": [str(msg) for msg in state["conversation_messages"]],
        "intent": state.get("intent", ""),
        "retrieved_context": state.get("retrieved_context", ""),
        "retrieved_citations": state.get("retrieved_citations", []),
        "latest_observation": state.get("latest_observation", ""),
        "question_type": state.get("question_type", ""),
        "knowledge_scope": state.get("knowledge_scope", ""),
        "needs_retrieval": state.get("needs_retrieval", False),
        "retrieval_query": state.get("retrieval_query", ""),
        "missing_information": state.get("missing_information", ""),
        "react_step_count": state.get("react_step_count", 0),
        "observed_tool_messages": state.get("observed_tool_messages", 0),
        "next_citation_number": state.get("next_citation_number", 1),
        "should_continue": state.get("should_continue", False),
        "final_answer": state.get("final_answer", ""),
        "final_citations": state.get("final_citations", []),
    }

def should_call_tool(state: AgentState) -> bool:
    msgs = state.get("messages")
    if msgs is None or len(msgs) == 0:
        return False
    last_msg = msgs[-1]
    if not isinstance(last_msg, AIMessage):
        return False
    last_msg = cast(AIMessage, last_msg)
    return last_msg.tool_calls is not None and len(last_msg.tool_calls) > 0

def should_continue_loop(state: AgentState) -> bool:
    return bool(state.get("should_continue", False))
