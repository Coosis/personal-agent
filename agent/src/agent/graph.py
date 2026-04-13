"""LangGraph assembly point."""

from langgraph.constants import END, START
from langgraph.graph.state import CompiledStateGraph, StateGraph
from langgraph.prebuilt import ToolNode

from agent.config import Config
from agent.context import AppContext
from agent.nodes import build_nodes
from agent.state import AgentState, should_call_tool, should_continue_loop
from agent.tools import create_tools

# START
# |
# v
# analyze_request
# |
# v                   ----(doesn't need tool)-----> generate_response --> commit_agent_response --> END
# decide_next_action
# |                   <--------------+
# | (needs tool)                     |
# |                                  |
# v                                  | (continue)
# call_tool                          |
# |                                  |
# v                                  |
# observe_tool_result ----> decide_whether_continue



def build_graph(cfg: Config, app_ctx: AppContext) -> CompiledStateGraph[AgentState, None, AgentState, AgentState]:
    """Build the agent graph with retrieval capabilities."""
    builder = StateGraph(AgentState)

    tools = create_tools(app_ctx)
    nodes = build_nodes(cfg, tools)

    builder.add_node("analyze_request", nodes["analyze_request"])
    builder.add_node("decide_next_action", nodes["decide_next_action"])
    builder.add_node("observe_tool_result", nodes["observe_tool_result"])
    builder.add_node("decide_whether_continue", nodes["decide_whether_continue"])
    builder.add_node("generate_response", nodes["generate_response"])
    builder.add_node("commit_agent_response", nodes["commit_agent_response"])
    builder.add_node("call_tool", ToolNode(tools))

    builder.add_edge(START, "analyze_request")
    builder.add_edge("analyze_request", "decide_next_action")
    builder.add_conditional_edges("decide_next_action", should_call_tool, {True: "call_tool", False: "generate_response"})
    builder.add_edge("call_tool", "observe_tool_result")
    builder.add_edge("observe_tool_result", "decide_whether_continue")
    builder.add_conditional_edges("decide_whether_continue", should_continue_loop, {True: "decide_next_action", False: "generate_response"})

    builder.add_edge("generate_response", "commit_agent_response")
    builder.add_edge("commit_agent_response", END)

    return builder.compile()
