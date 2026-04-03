"""Embedding service using Alibaba DashScope."""
import logging
from typing import List
import httpx
from tenacity import retry, stop_after_attempt, wait_exponential

from processor.config import get_settings

logger = logging.getLogger(__name__)


class EmbeddingService:
    """Alibaba DashScope embedding service."""

    API_URL = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/text-embedding/text-embedding"

    def __init__(self):
        settings = get_settings()
        self.api_key = settings.alibaba_api_key
        self.model = settings.alibaba_embedding_model
        self.dimensions = settings.embedding_dimensions

        if not self.api_key:
            raise ValueError("ALIBABA_API_KEY is required for embeddings")

    @retry(
        stop=stop_after_attempt(3),
        wait=wait_exponential(multiplier=1, min=2, max=10),
    )
    def embed(self, texts: List[str]) -> List[List[float]]:
        """Get embeddings for a list of texts."""
        if not texts:
            return []

        # DashScope has a limit on batch size, process in chunks
        batch_size = 25
        all_embeddings = []

        for i in range(0, len(texts), batch_size):
            batch = texts[i : i + batch_size]
            batch_embeddings = self._embed_batch(batch)
            all_embeddings.extend(batch_embeddings)

        return all_embeddings

    def _embed_batch(self, texts: List[str]) -> List[List[float]]:
        """Embed a single batch of texts."""
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        payload = {
            "model": self.model,
            "input": {"texts": texts},
            "parameters": {"text_type": "document"},
        }

        try:
            with httpx.Client(timeout=60.0) as client:
                response = client.post(
                    self.API_URL,
                    headers=headers,
                    json=payload,
                )
                response.raise_for_status()
                data = response.json()

                if "output" not in data or "embeddings" not in data["output"]:
                    raise ValueError(f"Unexpected response format: {data}")

                # Sort embeddings by index to maintain order
                embeddings = data["output"]["embeddings"]
                embeddings.sort(key=lambda x: x["text_index"])

                return [e["embedding"] for e in embeddings]

        except httpx.HTTPStatusError as e:
            logger.error(f"Embedding API error: {e.response.text}")
            raise
        except Exception as e:
            logger.error(f"Embedding request failed: {e}")
            raise

    def embed_single(self, text: str) -> List[float]:
        """Get embedding for a single text."""
        embeddings = self.embed([text])
        return embeddings[0] if embeddings else []


# Global embedding service instance
_embedding_service: EmbeddingService | None = None


def get_embedding_service() -> EmbeddingService:
    """Get or create the embedding service singleton."""
    global _embedding_service
    if _embedding_service is None:
        _embedding_service = EmbeddingService()
    return _embedding_service
