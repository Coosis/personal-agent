"""LangGraph assembly point."""

from langgraph.constants import END, START
from langgraph.graph.state import CompiledStateGraph, StateGraph
from langgraph.prebuilt import ToolNode

from agent.config import Config
from agent.context import AppContext
from agent.nodes import build_nodes
from agent.state import AgentState, should_call_tool
from agent.tools import create_tools

def build_graph(cfg: Config, app_ctx: AppContext) -> CompiledStateGraph[AgentState, None, AgentState, AgentState]:
    """Build the agent graph with retrieval capabilities."""
    builder = StateGraph(AgentState)

    get_personal_information, search_knowledge_base = create_tools(app_ctx)
    summarize_input, plan_response, generate_response = build_nodes(
        cfg,
        app_ctx
    )

    builder.add_node("summarize_input", summarize_input)
    builder.add_node("plan_response", plan_response)
    builder.add_node("generate_response", generate_response)

    builder.add_node("tool", ToolNode([get_personal_information, search_knowledge_base]))

    builder.add_edge(START, "summarize_input")
    builder.add_edge("summarize_input", "plan_response")
    builder.add_conditional_edges("plan_response", should_call_tool, {True: "tool", False: "generate_response"})
    builder.add_edge("tool", "plan_response")
    builder.add_edge("generate_response", END)

    return builder.compile()
