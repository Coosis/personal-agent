"""Node package for the agent graph."""

from __future__ import annotations

import json
import re
from typing import Annotated, Literal, TypedDict, cast

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


QuestionType = Literal["knowledge", "chit_chat", "reasoning", "unknown"]
KnowledgeScope = Literal["personal", "project", "document", "general", "none", "unknown"]


def take_recent_history(messages: Annotated[list, "conversation_messages"]) -> list:
    """Take the most recent messages from the conversation history."""
    return messages[-4:] if messages else []


def extract_citation_ids(text: str) -> set[str]:
    return set(re.findall(r"\[([A-Z]\d+)\]", text))


def coerce_analysis_result(raw: object, question: str) -> AnalysisResult:
    if isinstance(raw, dict):
        return normalize_analysis_result(raw, question)

    if isinstance(raw, str):
        text = raw.strip()
        if text:
            try:
                parsed = json.loads(text)
                if isinstance(parsed, dict):
                    return normalize_analysis_result(parsed, question)
            except json.JSONDecodeError:
                start = text.find("{")
                end = text.rfind("}")
                if start >= 0 and end > start:
                    try:
                        parsed = json.loads(text[start : end + 1])
                        if isinstance(parsed, dict):
                            return normalize_analysis_result(parsed, question)
                    except json.JSONDecodeError:
                        pass

    return heuristic_analysis_result(question)


def normalize_analysis_result(raw: dict, question: str) -> AnalysisResult:
    raw_question_type = str(raw.get("question_type", "")).strip()
    question_type: QuestionType
    if raw_question_type in {"knowledge", "chit_chat", "reasoning"}:
        question_type = cast(QuestionType, raw_question_type)
    else:
        question_type = "unknown"

    raw_knowledge_scope = str(raw.get("knowledge_scope", "")).strip()
    knowledge_scope: KnowledgeScope
    if raw_knowledge_scope in {"personal", "project", "document", "general", "none"}:
        knowledge_scope = cast(KnowledgeScope, raw_knowledge_scope)
    else:
        knowledge_scope = "unknown"

    retrieval_query = str(raw.get("retrieval_query", "")).strip()
    if retrieval_query == "":
        retrieval_query = question.strip()

    return {
        "intent": str(raw.get("intent", "")).strip() or "unknown",
        "question_type": question_type,
        "knowledge_scope": knowledge_scope,
        "needs_retrieval": bool(raw.get("needs_retrieval", False)),
        "retrieval_query": retrieval_query,
        "missing_information": str(raw.get("missing_information", "")).strip(),
    }


def heuristic_analysis_result(question: str) -> AnalysisResult:
    lowered = question.lower()

    personal_markers = [
        "my ",
        "i am",
        "i'm ",
        "i like",
        "my friend",
        "my girlfriend",
        "my boyfriend",
        "my wife",
        "my husband",
        "do you remember",
        "what do you know about me",
    ]
    project_markers = [
        "this project",
        "this repo",
        "our codebase",
        "the codebase",
        "the repository",
    ]
    document_markers = [
        "document",
        "note",
        "notes",
        "file",
        "files",
        "pdf",
        "upload",
        "source",
    ]
    chit_chat_markers = ["hello", "hi", "hey", "how are you", "thanks", "thank you"]
    reasoning_markers = ["how do i", "how to", "write", "implement", "example", "examples", "explain"]

    knowledge_scope: KnowledgeScope = "general"
    if any(marker in lowered for marker in personal_markers):
        knowledge_scope = "personal"
    elif any(marker in lowered for marker in project_markers):
        knowledge_scope = "project"
    elif any(marker in lowered for marker in document_markers):
        knowledge_scope = "document"

    question_type: QuestionType = "knowledge"
    if any(marker in lowered for marker in chit_chat_markers) and "?" not in lowered:
        question_type = "chit_chat"
    elif any(marker in lowered for marker in reasoning_markers):
        question_type = "reasoning"

    needs_retrieval = knowledge_scope in {"personal", "project", "document"}

    return {
        "intent": "classify_request",
        "question_type": question_type,
        "knowledge_scope": knowledge_scope,
        "needs_retrieval": needs_retrieval,
        "retrieval_query": question.strip(),
        "missing_information": "",
    }


def build_nodes(cfg: Config, tools) -> dict:
    """Build node callables bound to runtime configuration."""
    model = ChatOpenAI(
        model=cfg.openrouter_model,
        api_key=lambda: cfg.openrouter_api_key,
        base_url=cfg.openrouter_api_url,
    )
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
        response = model.invoke(msgs)
        analysis = coerce_analysis_result(response.content, state["user_input"])

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
        contents: list[str] = []
        citations: list[dict[str, object]] = []
        for message in state["messages"]:
            if isinstance(message, ToolMessage):
                content = ""
                if isinstance(message.content, str):
                    content = message.content
                elif isinstance(message.content, list):
                    content = "\n".join([msg for msg in message.content if isinstance(msg, str)])
                if content.strip():
                    try:
                        payload = json.loads(content)
                    except json.JSONDecodeError:
                        contents.append(content)
                        continue

                    if isinstance(payload, dict):
                        context_value = payload.get("context")
                        if isinstance(context_value, str) and context_value.strip():
                            contents.append(context_value)

                        raw_citations = payload.get("citations")
                        if isinstance(raw_citations, list):
                            for item in raw_citations:
                                if isinstance(item, dict):
                                    citations.append(cast(dict[str, object], item))
                        continue

                    contents.append(content)
        return {
            "retrieved_context": "\n".join(contents) if contents else "No relevant information retrieved from tools.",
            "retrieved_citations": citations,
        }

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

        cited_ids = extract_citation_ids(final_answer)
        final_citations = [
            citation
            for citation in state.get("retrieved_citations", [])
            if isinstance(citation.get("id"), str) and citation["id"] in cited_ids
        ]

        return {
            "final_answer": final_answer,
            "final_citations": final_citations,
        }

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
