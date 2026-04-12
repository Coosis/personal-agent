"""Flask entrypoint for the agent."""

from collections.abc import Iterator
from flask import Flask, Response, json, jsonify, request, stream_with_context
from langchain_core.messages import AIMessage, HumanMessage, SystemMessage, ToolMessage

from agent.config import get_config
from agent.context import AppContext
from agent.graph import build_graph
from langgraph.graph.state import CompiledStateGraph

from agent.context import AppContext
from agent.nodes import GENERATE_RESPONSE_STREAM_KEY
from agent.state import AgentState, pretty_print_state


type CompiledGraph = CompiledStateGraph[AgentState, None, AgentState, AgentState]

def decode_conversation_messages(payload) -> list:
    messages = []
    if not isinstance(payload, list):
        return messages

    for item in payload:
        if not isinstance(item, dict):
            continue
        role = item.get("role")
        content = item.get("content")
        if not isinstance(role, str) or not isinstance(content, str):
            continue

        if role == "user":
            messages.append(HumanMessage(content=content))
        elif role == "assistant":
            messages.append(AIMessage(content=content))
        elif role == "system":
            messages.append(SystemMessage(content=content))
        elif role == "tool":
            messages.append(ToolMessage(content=content, tool_call_id="history"))

    return messages

def run_once(graph: CompiledGraph, content: str, history: list) -> Iterator[str]:
    """Run the graph once for a single user input."""
    for chunk in graph.stream(
            {
                "user_input": content,
                "messages": [],
                "conversation_messages": history,
            }, # type: ignore[arg-type]
            version="v2",
            stream_mode=["custom", "values"],
            subgraphs=True,
            ):
        if chunk["type"] == "custom":
            yield chunk["data"][GENERATE_RESPONSE_STREAM_KEY]

        elif chunk["type"] == "values":
            pass
            # print("\n\nCurrent state:")
            # print(f"{json.dumps(pretty_print_state(chunk["data"]), indent=2)}")


def create_app(cfg, app_ctx: AppContext) -> Flask:
    """Create the Flask app and wire routes."""
    app = Flask(__name__)
    graph = build_graph(cfg, app_ctx)

    @app.get("/health")
    def health():
        return jsonify({"status": "ok"})

    # streaming response using sse
    def sse_event(data: dict, event = None) -> str:
        lines = []
        if event is not None:
            lines.append(f"event: {event}")

        lines.append(f"data: {json.dumps(data)}")
        lines.append("")
        lines.append("")
        return "\n".join(lines)

    @app.post("/v1/stream")
    def stream():
        def generate(graph, content, history) -> Iterator[str]:
            yield sse_event({"type": "start"}, event="signal")
            for token in run_once(graph, content, history):
                yield sse_event({"type": "token", "content": token}, event="message")
            yield sse_event({"type": "stop"}, event="signal")

        payload = request.get_json(silent=True)
        if not isinstance(payload, dict):
            return jsonify({"error": "request body must be a JSON object"}), 400

        content = payload.get("content")
        if not isinstance(content, str) or not content.strip():
            return jsonify({"error": "content must be a non-empty string"}), 400

        history = decode_conversation_messages(payload.get("messages"))

        try:
            return Response(stream_with_context(generate(graph, content, history)), mimetype="text/event-stream")
        except Exception as exc:
            app.logger.exception("request failed")
            return jsonify({"error": str(exc)}), 500

    return app


def main() -> None:
    """Start the Flask server."""
    cfg = get_config()
    app_ctx = AppContext(cfg)
    app = create_app(cfg, app_ctx)
    print(f"agent listening on http://{cfg.host}:{cfg.port}")
    app.run(host=cfg.host, port=cfg.port, debug=False, use_reloader=False)


if __name__ == "__main__":
    main()
