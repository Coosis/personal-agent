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

    tools = create_tools(app_ctx)
    nodes = build_nodes(cfg, tools)

    builder.add_node("analyze_request", nodes["analyze_request"])
    builder.add_node("plan_response", nodes["plan_response"])
    builder.add_node("normalize_tool_result", nodes["normalize_tool_result"])
    builder.add_node("generate_response", nodes["generate_response"])
    builder.add_node("commit_agent_response", nodes["commit_agent_response"])
    builder.add_node("tool", ToolNode(tools))

    builder.add_edge(START, "analyze_request")
    builder.add_edge("analyze_request", "plan_response")
    builder.add_conditional_edges("plan_response", should_call_tool, {True: "tool", False: "generate_response"})
    builder.add_edge("tool", "normalize_tool_result")
    builder.add_edge("normalize_tool_result", "generate_response")

    builder.add_edge("generate_response", "commit_agent_response")
    builder.add_edge("commit_agent_response", END)

    return builder.compile()
