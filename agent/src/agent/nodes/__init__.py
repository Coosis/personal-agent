"""Node package for the agent graph."""

from typing import Annotated
from langchain_core.messages import AIMessage, HumanMessage, ToolMessage
from langchain_core.output_parsers import StrOutputParser
from langchain_openai import ChatOpenAI
from langgraph.config import get_stream_writer

from agent.config import Config
from agent.prompts import build_analysis_prompt, build_plan_prompt, build_response_prompt
from agent.state import AgentState, AgentStateUpdate

GENERATE_RESPONSE_STREAM_KEY = "generate_response_msg"

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
        analysis = (model | StrOutputParser()).invoke(msgs)

        return {"input_analysis": analysis}

    def plan_response(state: AgentState) -> AgentStateUpdate:
        msgs = plan_prompt.invoke({
            "question": state["user_input"],
            "input_analysis": state["input_analysis"],
        }).to_messages()
        msgs = msgs[:1] + take_recent_history(state["conversation_messages"]) + msgs[1:]
        plan = tool_model.invoke(msgs)
        content = ""
        if isinstance(plan.content, str):
            content = plan.content
        elif isinstance(plan.content, list):
            content = "\n".join([msg for msg in plan.content if isinstance(msg, str)])

        return {"plan_decision": content, "messages": [plan]}

    def normalize_tool_result(state: AgentState) -> AgentStateUpdate:
        for message in state["messages"]:
            if isinstance(message, ToolMessage):
                content = ""
                if isinstance(message.content, str):
                    content = message.content
                elif isinstance(message.content, list):
                    content = "\n".join([msg for msg in message.content if isinstance(msg, str)])
        return {"retrieved_context": content}

    def generate_response(state: AgentState) -> AgentStateUpdate:
        response_chain = model
        msgs = response_prompt.invoke({
            "question": state["user_input"],
            "input_analysis": state["input_analysis"],
            "plan_decision": state["plan_decision"],
            "retrieved_context": state.get("retrieved_context", "No relevant information retrieved from tools."),
        }).to_messages()

        writer = get_stream_writer()

        msgs = msgs[:1] + take_recent_history(state["conversation_messages"]) + msgs[1:]

        final_answer = ""
        for chunk in response_chain.stream(msgs):
            msg = chunk
            if msg.content is None:
                continue
            if isinstance(msg.content, str) and msg.content.strip() != "":
                # print(msg.content, end="", flush=True)
                writer({GENERATE_RESPONSE_STREAM_KEY: msg.content})
                final_answer += msg.content
            elif isinstance(msg.content, list):
                for part in msg.content:
                    if isinstance(part, str) and part.strip() != "":
                        # print(part, end="", flush=True)
                        writer({GENERATE_RESPONSE_STREAM_KEY: msg.content})
                        final_answer += part
            # if msg.chunk_position == "last":
            #     writer({GENERATE_RESPONSE_STREAM_KEY: "\n\n"})

        return {"final_answer": final_answer}

    def commit_agent_response(state: AgentState) -> AgentStateUpdate:
        user_msg = HumanMessage(content=state["user_input"])
        response_msg = AIMessage(content=state["final_answer"])
        return {"conversation_messages": state["conversation_messages"] + [user_msg, response_msg]}

    return {
        "analyze_request": analyze_request,
        "plan_response": plan_response,
        "generate_response": generate_response,
        "commit_agent_response": commit_agent_response,
        "normalize_tool_result": normalize_tool_result,
    }
