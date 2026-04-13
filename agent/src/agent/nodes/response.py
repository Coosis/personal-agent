import re

from langgraph.config import get_stream_writer

from agent.nodes.shared import GENERATE_RESPONSE_STREAM_KEY, take_recent_history
from agent.prompts import build_response_prompt
from agent.state import AgentState, AgentStateUpdate


def make_generate_response_node(model):
    response_prompt = build_response_prompt()

    def generate_response(state: AgentState) -> AgentStateUpdate:
        msgs = response_prompt.invoke(
            {
                "question": state["user_input"],
                "intent": state.get("intent", "unknown"),
                "question_type": state.get("question_type", "unknown"),
                "knowledge_scope": state.get("knowledge_scope", "unknown"),
                "missing_information": state.get("missing_information", "unknown"),
                "retrieved_context": state.get(
                    "retrieved_context",
                    "No relevant information retrieved from tools.",
                ),
            }
        ).to_messages()

        writer = get_stream_writer()
        msgs = msgs[:1] + take_recent_history(state["conversation_messages"]) + msgs[1:]

        # streaming
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

    return generate_response


def extract_citation_ids(text: str) -> set[str]:
    return set(re.findall(r"\[([A-Z]\d+)\]", text))
