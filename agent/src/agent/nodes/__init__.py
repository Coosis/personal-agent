"""Node package for the agent graph."""

import sys

from langchain_core.output_parsers import StrOutputParser
from langchain_core.prompts import ChatPromptTemplate
from langchain_openai import ChatOpenAI

from agent.config import Config
from agent.state import AgentState, AgentStateUpdate

def build_nodes(cfg: Config, get_personal_information, search_knowledge_base):
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
    ).bind_tools([search_knowledge_base])

    summary_prompt = ChatPromptTemplate([
        ("system", "You are a personal assistant. Please analyze the user's question and provide a summary."),
        ("user", "{question}"),
    ])

    def summarize_input(state: AgentState) -> AgentStateUpdate:
        msgs = summary_prompt.invoke({"question": state["user_input"]}).to_messages() + state["messages"]
        summarized_input = model.invoke(msgs)
        print(f"Summarized input: {summarized_input}", file=sys.stderr)
        return {"summarized_input": summarized_input}

    plan_system_prompt = """
You are a personal assistant.
Please analyze the user's question and your summarized input,
and provide a plan to answer the question.
If the answer needs personal information, use the available tool to retrieve additional information.
"""
    plan_prompt = ChatPromptTemplate([
        ("system", plan_system_prompt),
        ("user", "User question: {question}"),
    ])

    def plan_response(state: AgentState) -> AgentStateUpdate:
        msgs = plan_prompt.invoke({
            "question": state["user_input"],
        }).to_messages() + state["messages"]
        plan = tool_model.invoke(msgs)
        print(f"Generated plan: {plan}", file=sys.stderr)
        return {"messages": [plan]}

    response_system_prompt = """
You are a personal assistant.
You have the user's question, your summarized input,
and you have made a plan to answer the question,
you may have also retrieved additional information if needed,
now you are going to answer the question based on the plan.
"""
    response_prompt = ChatPromptTemplate([
        ("system", response_system_prompt),
        ("user", "User question: {question}"),
    ])

    def generate_response(state: AgentState) -> AgentStateUpdate:
        response_chain = model | StrOutputParser()
        msgs = response_prompt.invoke({
            "question": state["user_input"],
        }).to_messages() + state["messages"]
        final_answer = response_chain.invoke(msgs)
        return {"final_answer": final_answer}

    force_tool_call_prompt = ChatPromptTemplate([
        ("system", "You are a personal assistant. You have access to a tool called {tool_name}. "),
        ("system", "Please call the tool with appropriate arguments to demonstrate that tool calls work"),
    ])

    def force_tool_call(state: AgentState) -> AgentStateUpdate:
        output = (force_tool_call_prompt | model).invoke({"tool_name": get_personal_information})
        return {"messages": [output]}

    return summarize_input, plan_response, generate_response, force_tool_call
