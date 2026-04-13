"""Node package for the agent graph."""

from __future__ import annotations

from langchain_core.messages import AIMessage, HumanMessage
from langchain_openai import ChatOpenAI

from agent.config import Config
from agent.nodes.analyze import make_analysis_node
from agent.nodes.decide import (
    make_decide_nxt_action_node,
    make_decide_whether_continue_node,
)
from agent.nodes.observe_tool_result import observe_tool_result
from agent.nodes.response import make_generate_response_node
from agent.state import AgentState, AgentStateUpdate


def build_nodes(cfg: Config, tools) -> dict:
    """Build node callables bound to runtime configuration."""
    model = ChatOpenAI(
        model=cfg.agent_model,
        api_key=lambda: cfg.agent_api_key,
        base_url=cfg.agent_base_url,
    )
    tool_model = ChatOpenAI(
        model=cfg.agent_model,
        api_key=lambda: cfg.agent_api_key,
        base_url=cfg.agent_base_url,
    ).bind_tools(tools)

    def commit_agent_response(state: AgentState) -> AgentStateUpdate:
        user_msg = HumanMessage(content=state["user_input"])
        response_msg = AIMessage(content=state["final_answer"])
        return {"conversation_messages": state["conversation_messages"] + [user_msg, response_msg]}

    return {
        "analyze_request": make_analysis_node(model),
        "decide_next_action": make_decide_nxt_action_node(tool_model),
        "observe_tool_result": observe_tool_result,
        "decide_whether_continue": make_decide_whether_continue_node(model),
        "generate_response": make_generate_response_node(model),
        "commit_agent_response": commit_agent_response,
    }
