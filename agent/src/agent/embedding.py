"""Embedding service for the agent."""

import httpx

from agent.config import Config


class AgentEmbeddingService:
    def __init__(self, cfg: Config):
        self.api_key = cfg.embedding_api_key
        self.model = cfg.embedding_model
        self.api_url = cfg.embedding_api_url

        if not self.api_key:
            raise ValueError("ALIBABA_API_KEY environment variable is required")

    def embed(self, text: str) -> list[float]:
        """Get embedding for a text."""
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        payload = {"model": self.model, "input": text, "dimensions": 1024}

        response = httpx.post(self.api_url, headers=headers, json=payload)
        response.raise_for_status()
        data = response.json()

        return data["data"][0]["embedding"]
