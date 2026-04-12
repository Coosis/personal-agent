"""Flask entrypoint for the agent."""

from collections.abc import Iterator
from flask import Flask, Response, json, jsonify, request, stream_with_context

from agent.config import get_config
from agent.context import AppContext
from agent.graph import build_graph
from langgraph.graph.state import CompiledStateGraph

from agent.context import AppContext
from agent.state import AgentState


type CompiledGraph = CompiledStateGraph[AgentState, None, AgentState, AgentState]

def run_once(graph: CompiledGraph, content: str) -> Iterator[str]:
    """Run the graph once for a single user input."""
    # output = graph.invoke({"user_input": content})  # type: ignore[arg-type]
    for chunk in graph.stream(
            {"user_input": content}, # type: ignore[arg-type]
            version="v2",
            stream_mode=["messages", "values"],
            subgraphs=True,
            ):
        if chunk["type"] == "messages":
            msg, metadata = chunk["data"]
            if msg.content is not None:
                if isinstance(msg.content, str) and msg.content.strip() != "":
                    print(msg.content, end="", flush=True)
                    yield msg.content
                elif isinstance(msg.content, list):
                    for part in msg.content:
                        if isinstance(part, str) and part.strip() != "":
                            print(part, end="", flush=True)
                            yield part
            elif msg.chunk_position == "last":
                print("\n\n")
                yield "\n\n"

        elif chunk["type"] == "values":
            print(f"\n\nFinal answer: {chunk['data']}\n\n")
            # print(f"Metadata: {metadata}\n\n\n\n")
    # return output["final_answer"]
    # return "ok!"


def create_app(cfg, app_ctx: AppContext) -> Flask:
    """Create the Flask app and wire routes."""
    app = Flask(__name__)
    graph = build_graph(cfg, app_ctx)

    @app.get("/health")
    def health():
        return jsonify({"status": "ok"})


    # non-streaming response
    @app.post("/v1/chat")
    def chat():
        payload = request.get_json(silent=True)
        if not isinstance(payload, dict):
            return jsonify({"error": "request body must be a JSON object"}), 400

        content = payload.get("content")
        if not isinstance(content, str) or not content.strip():
            return jsonify({"error": "content must be a non-empty string"}), 400

        try:
            output = run_once(graph, content)
            return jsonify({"content": output})
        except Exception as exc:
            app.logger.exception("request failed")
            return jsonify({"error": str(exc)}), 500

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
        def generate(graph, content) -> Iterator[str]:
            yield sse_event({"type": "start"}, event="signal")
            for token in run_once(graph, content):
                yield sse_event({"type": "token", "content": token}, event="message")
            yield sse_event({"type": "stop"}, event="signal")

        payload = request.get_json(silent=True)
        if not isinstance(payload, dict):
            return jsonify({"error": "request body must be a JSON object"}), 400

        content = payload.get("content")
        if not isinstance(content, str) or not content.strip():
            return jsonify({"error": "content must be a non-empty string"}), 400

        try:
            return Response(stream_with_context(generate(graph, content)), mimetype="text/event-stream")
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
