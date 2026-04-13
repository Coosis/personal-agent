import json
from typing import cast

from langchain_core.messages import ToolMessage

from agent.state import AgentState, AgentStateUpdate


def observe_tool_result(state: AgentState) -> AgentStateUpdate:
    # all tool messages in current state
    tool_messages = [
        message for message in state.get("messages", []) if type(message) is ToolMessage
    ]
    # previous tool messages count
    observed_tool_messages = state.get("observed_tool_messages", 0)
    # new tool messages since last observation
    new_tool_messages = tool_messages[observed_tool_messages:]

    contents: list[str] = []
    citations: list[dict[str, object]] = list(state.get("retrieved_citations", []))
    next_citation_number = state.get("next_citation_number", 1)

    for message in new_tool_messages:
        if type(message) is not ToolMessage:
            continue
        content = ""
        if isinstance(message.content, str):
            content = message.content
        elif isinstance(message.content, list):
            content = "\n".join([msg for msg in message.content if isinstance(msg, str)])

        if not content.strip():
            continue

        # contains non-whitespace characters
        try:
            payload = json.loads(content)
        except json.JSONDecodeError:
            contents.append(content)
            continue

        if isinstance(payload, dict):
            context_value = payload.get("context")
            raw_citations = payload.get("citations")
            citation_items: list[dict[str, object]] = []
            if isinstance(raw_citations, list):
                for item in raw_citations:
                    if isinstance(item, dict):
                        citation_items.append(cast(dict[str, object], item))

            if isinstance(context_value, str) and context_value.strip():
                remapped_context, remapped_citations, next_citation_number = remap_citations(
                    context_value,
                    citation_items,
                    next_citation_number,
                )
                contents.append(remapped_context)
                citations.extend(remapped_citations)
            continue

        contents.append(content)

    existing_context = state.get("retrieved_context", "")
    new_context = "\n".join(contents).strip()
    combined_context_parts = [
        part
        for part in [existing_context, new_context]
        if part and part != "No relevant information retrieved from tools."
    ]
    combined_context = (
        "\n\n".join(combined_context_parts)
        if combined_context_parts
        else "No relevant information retrieved from tools."
    )

    latest_observation = (
        new_context if new_context else "No new relevant information observed from tools."
    )
    step_increment = 1 if new_tool_messages else 0

    return {
        "retrieved_context": combined_context,
        "retrieved_citations": citations,
        "latest_observation": latest_observation,
        "react_step_count": state.get("react_step_count", 0) + step_increment,
        "observed_tool_messages": observed_tool_messages + len(new_tool_messages),
        "next_citation_number": next_citation_number,
    }


def remap_citations(
    context: str, citations: list[dict[str, object]], next_citation_number: int
) -> tuple[str, list[dict[str, object]], int]:
    remapped_context = context
    remapped_citations: list[dict[str, object]] = []
    current = next_citation_number

    for citation in citations:
        raw_id = citation.get("id")
        if not isinstance(raw_id, str):
            continue

        new_id = f"C{current}"
        remapped_context = remapped_context.replace(f"[{raw_id}]", f"[{new_id}]")
        remapped = dict(citation)
        remapped["id"] = new_id
        remapped_citations.append(remapped)
        current += 1

    return remapped_context, remapped_citations, current
