from typing import TypedDict

from agent.nodes.shared import (
    MAX_REACT_STEPS,
    coerce_json_dict,
    take_recent_history,
)
from agent.prompts import build_continue_prompt, build_plan_prompt
from agent.state import AgentState, AgentStateUpdate


class ContinueDecision(TypedDict):
    continue_loop: bool
    reason: str


def make_decide_nxt_action_node(tool_model):
    decide_nxt_action_prompt = build_plan_prompt()

    def decide_next_action(state: AgentState) -> AgentStateUpdate:
        msgs = decide_nxt_action_prompt.invoke(
            {
                "question": state["user_input"],
                "intent": state.get("intent", "unknown"),
                "question_type": state.get("question_type", "unknown"),
                "knowledge_scope": state.get("knowledge_scope", "unknown"),
                "needs_retrieval": "true" if state.get("needs_retrieval", False) else "false",
                "retrieval_query": state.get("retrieval_query", state["user_input"]),
                "missing_information": state.get("missing_information", "unknown"),
                "react_step_count": state.get("react_step_count", 0),
                "retrieved_context": state.get(
                    "retrieved_context",
                    "No relevant information retrieved from tools.",
                ),
            }
        ).to_messages()
        msgs = (
            msgs[:1]
            + take_recent_history(state["conversation_messages"])
            + state.get("messages", [])
            + msgs[1:]
        )
        plan = tool_model.invoke(msgs)

        return {"messages": [plan]}

    return decide_next_action


def make_decide_whether_continue_node(model):
    continue_prompt = build_continue_prompt()

    def decide_whether_continue(state: AgentState) -> AgentStateUpdate:
        step_count = state.get("react_step_count", 0)
        if step_count >= MAX_REACT_STEPS:
            return {"should_continue": False}

        msgs = continue_prompt.invoke(
            {
                "question": state["user_input"],
                "intent": state.get("intent", "unknown"),
                "question_type": state.get("question_type", "unknown"),
                "knowledge_scope": state.get("knowledge_scope", "unknown"),
                "needs_retrieval": "true" if state.get("needs_retrieval", False) else "false",
                "react_step_count": step_count,
                "latest_observation": state.get("latest_observation", ""),
                "retrieved_context": state.get(
                    "retrieved_context",
                    "No relevant information retrieved from tools.",
                ),
            }
        ).to_messages()
        msgs = (
            msgs[:1]
            + take_recent_history(state["conversation_messages"])
            + state.get("messages", [])
            + msgs[1:]
        )
        response = model.invoke(msgs)
        decision = coerce_continue_decision(response.content, step_count)
        return {"should_continue": decision["continue_loop"]}

    return decide_whether_continue


def coerce_continue_decision(raw: object, step_count: int) -> ContinueDecision:
    if step_count >= MAX_REACT_STEPS:
        return {
            "continue_loop": False,
            "reason": "max_react_steps_reached",
        }

    parsed = coerce_json_dict(raw)
    if parsed is not None:
        return {
            "continue_loop": bool(parsed.get("continue_loop", False)),
            "reason": str(parsed.get("reason", "")).strip(),
        }

    return {
        "continue_loop": True,
        "reason": "fallback_continue",
    }
