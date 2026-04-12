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
    question_type: str
    knowledge_scope: str
    needs_retrieval: bool
    retrieval_query: str
    missing_information: str

    final_answer: str

class AgentStateUpdate(TypedDict, total=False):
    user_input: str
    messages: Annotated[list[AnyMessage], add_messages]
    conversation_messages: Annotated[list[AnyMessage], add_messages]

    intent: str
    retrieved_context: str
    question_type: str
    knowledge_scope: str
    needs_retrieval: bool
    retrieval_query: str
    missing_information: str

    final_answer: str

def pretty_print_state(state: AgentState) -> dict:
    return {
        "user_input": state["user_input"],
        "messages": [str(msg) for msg in state["messages"]],
        "conversation_messages": [str(msg) for msg in state["conversation_messages"]],
        "intent": state.get("intent", ""),
        "retrieved_context": state.get("retrieved_context", ""),
        "question_type": state.get("question_type", ""),
        "knowledge_scope": state.get("knowledge_scope", ""),
        "needs_retrieval": state.get("needs_retrieval", False),
        "retrieval_query": state.get("retrieval_query", ""),
        "missing_information": state.get("missing_information", ""),
        "final_answer": state.get("final_answer", ""),
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
