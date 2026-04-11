"""Flask entrypoint for the agent."""

from flask import Flask, jsonify, request

from agent.config import get_config
from agent.context import AppContext
from agent.graph import build_graph


def run_once(graph, content: str) -> str:
    """Run the graph once for a single user input."""
    output = graph.invoke({"user_input": content})  # type: ignore[arg-type]
    return output["final_answer"]


def create_app(cfg, app_ctx: AppContext) -> Flask:
    """Create the Flask app and wire routes."""
    app = Flask(__name__)
    graph = build_graph(cfg, app_ctx)

    @app.get("/health")
    def health():
        return jsonify({"status": "ok"})

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
