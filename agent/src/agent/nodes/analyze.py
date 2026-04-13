from typing import Literal, TypedDict, cast

from agent.nodes.shared import coerce_json_dict, take_recent_history
from agent.prompts import build_analysis_prompt
from agent.state import AgentState, AgentStateUpdate

QuestionType = Literal["knowledge", "chit_chat", "reasoning", "unknown"]
KnowledgeScope = Literal["personal", "project", "document", "general", "none", "unknown"]


class AnalysisResult(TypedDict):
    intent: str
    question_type: QuestionType
    knowledge_scope: KnowledgeScope
    needs_retrieval: bool
    retrieval_query: str
    missing_information: str


def make_analysis_node(model):
    analysis_prompt = build_analysis_prompt()

    def analyze_request(state: AgentState) -> AgentStateUpdate:
        msgs = analysis_prompt.invoke(
            {
                "question": state["user_input"],
            }
        ).to_messages()
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

    return analyze_request


def coerce_analysis_result(raw: object, question: str) -> AnalysisResult:
    coerced_dict = coerce_json_dict(raw)
    if coerced_dict is not None:
        return normalize_analysis_result(coerced_dict, question)

    return heuristic_analysis_result(question)


def normalize_analysis_result(raw: dict, question: str) -> AnalysisResult:
    # question type
    raw_question_type = str(raw.get("question_type", "")).strip()
    question_type: QuestionType
    if raw_question_type in {"knowledge", "chit_chat", "reasoning"}:
        question_type = cast(QuestionType, raw_question_type)
    else:
        question_type = "unknown"

    # knowledge scope
    raw_knowledge_scope = str(raw.get("knowledge_scope", "")).strip()
    knowledge_scope: KnowledgeScope
    if raw_knowledge_scope in {
        "personal",
        "project",
        "document",
        "general",
        "none",
    }:
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
        "documents",
        "note",
        "notes",
        "file",
        "files",
        "pdf",
        "pdfs",
        "upload",
        "uploads",
        "source",
        "sources",
    ]
    chit_chat_markers = [
        "hello",
        "hi",
        "hey",
        "how are you",
        "thanks",
        "thank you",
    ]
    reasoning_markers = [
        "how do i",
        "how to",
        "write",
        "implement",
        "example",
        "examples",
        "explain",
    ]

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
