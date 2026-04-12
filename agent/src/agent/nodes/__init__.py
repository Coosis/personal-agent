"""Node package for the agent graph."""

from __future__ import annotations

from typing import Annotated, Literal, TypedDict

from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langchain_openai import ChatOpenAI
from langgraph.config import get_stream_writer

from agent.config import Config
from agent.prompts import build_analysis_prompt, build_plan_prompt, build_response_prompt
from agent.state import AgentState, AgentStateUpdate

GENERATE_RESPONSE_STREAM_KEY = "generate_response_msg"


class AnalysisResult(TypedDict):
    intent: str
    question_type: Literal["knowledge", "chit_chat", "reasoning", "unknown"]
    knowledge_scope: Literal["personal", "project", "document", "general", "none", "unknown"]
    needs_retrieval: bool
    retrieval_query: str
    missing_information: str


def take_recent_history(messages: Annotated[list, "conversation_messages"]) -> list:
    """Take the most recent messages from the conversation history."""
    return messages[-4:] if messages else []


def build_nodes(cfg: Config, tools) -> dict:
    """Build node callables bound to runtime configuration."""
    model = ChatOpenAI(
        model=cfg.openrouter_model,
        api_key=lambda: cfg.openrouter_api_key,
        base_url=cfg.openrouter_api_url,
    )
    analysis_model = model.with_structured_output(AnalysisResult)
    tool_model = ChatOpenAI(
        model=cfg.openrouter_model,
        api_key=lambda: cfg.openrouter_api_key,
        base_url=cfg.openrouter_api_url,
    ).bind_tools(tools)

    analysis_prompt = build_analysis_prompt()
    plan_prompt = build_plan_prompt()
    response_prompt = build_response_prompt()

    def analyze_request(state: AgentState) -> AgentStateUpdate:
        msgs = analysis_prompt.invoke({
            "question": state["user_input"],
        }).to_messages()
        msgs = msgs[:1] + take_recent_history(state["conversation_messages"]) + msgs[1:]
        analysis = analysis_model.invoke(msgs)

        return {
            "intent": analysis["intent"],
            "question_type": analysis["question_type"],
            "knowledge_scope": analysis["knowledge_scope"],
            "needs_retrieval": analysis["needs_retrieval"],
            "retrieval_query": analysis["retrieval_query"],
            "missing_information": analysis["missing_information"],
        }

    def plan_response(state: AgentState) -> AgentStateUpdate:
        msgs = plan_prompt.invoke({
            "question": state["user_input"],
            "intent": state.get("intent", "unknown"),
            "question_type": state.get("question_type", "unknown"),
            "knowledge_scope": state.get("knowledge_scope", "unknown"),
            "needs_retrieval": "true" if state.get("needs_retrieval", False) else "false",
            "retrieval_query": state.get("retrieval_query", state["user_input"]),
            "missing_information": state.get("missing_information", "unknown"),
        }).to_messages()
        msgs = msgs[:1] + take_recent_history(state["conversation_messages"]) + msgs[1:]
        plan = tool_model.invoke(msgs)

        return {"messages": [plan]}

    def normalize_tool_result(state: AgentState) -> AgentStateUpdate:
        contents = []
        for message in state["messages"]:
            if isinstance(message, ToolMessage):
                content = ""
                if isinstance(message.content, str):
                    content = message.content
                elif isinstance(message.content, list):
                    content = "\n".join([msg for msg in message.content if isinstance(msg, str)])
                if content.strip():
                    contents.append(content)
        return {"retrieved_context": "\n".join(contents) if contents else "No relevant information retrieved from tools."}

    def generate_response(state: AgentState) -> AgentStateUpdate:
        msgs = response_prompt.invoke({
            "question": state["user_input"],
            "intent": state.get("intent", "unknown"),
            "question_type": state.get("question_type", "unknown"),
            "knowledge_scope": state.get("knowledge_scope", "unknown"),
            "missing_information": state.get("missing_information", "unknown"),
            "retrieved_context": state.get("retrieved_context", "No relevant information retrieved from tools."),
        }).to_messages()

        writer = get_stream_writer()
        msgs = msgs[:1] + take_recent_history(state["conversation_messages"]) + msgs[1:]

        final_answer = ""
        for chunk in model.stream(msgs):
            if chunk.content is None:
                continue
            if isinstance(chunk.content, str) and chunk.content.strip():
                writer({GENERATE_RESPONSE_STREAM_KEY: chunk.content})
                final_answer += chunk.content
            elif isinstance(chunk.content, list):
                for part in chunk.content:
                    if isinstance(part, str) and part.strip():
                        writer({GENERATE_RESPONSE_STREAM_KEY: part})
                        final_answer += part

        return {"final_answer": final_answer}

    def commit_agent_response(state: AgentState) -> AgentStateUpdate:
        user_msg = HumanMessage(content=state["user_input"])
        response_msg = AIMessage(content=state["final_answer"])
        return {"conversation_messages": state["conversation_messages"] + [user_msg, response_msg]}

    return {
        "analyze_request": analyze_request,
        "plan_response": plan_response,
        "normalize_tool_result": normalize_tool_result,
        "generate_response": generate_response,
        "commit_agent_response": commit_agent_response,
    }
