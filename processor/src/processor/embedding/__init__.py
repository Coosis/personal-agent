"""Embedding service using Alibaba DashScope."""
import logging
from typing import List
import httpx
from tenacity import retry, stop_after_attempt, wait_exponential

from processor.config import get_settings

logger = logging.getLogger(__name__)


class EmbeddingService:
    """Alibaba DashScope embedding service."""

    API_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/embeddings"

    def __init__(self):
        settings = get_settings()
        self.api_key = settings.alibaba_api_key
        # Use v4 by default for compatible-mode endpoint
        self.model = settings.alibaba_embedding_model or "text-embedding-v4"
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

        # Filter out empty/whitespace-only texts to avoid API errors
        # Track original positions to reconstruct output
        filtered_texts = []
        original_indices = []
        for i, text in enumerate(texts):
            if text and text.strip():
                filtered_texts.append(text)
                original_indices.append(i)

        if not filtered_texts:
            logger.warning("All texts are empty, returning zero embeddings")
            return [[0.0] * self.dimensions for _ in texts]

        if len(filtered_texts) < len(texts):
            logger.debug(f"Filtered {len(texts) - len(filtered_texts)} empty texts")

        # DashScope has a limit on batch size, process in chunks
        batch_size = 25
        filtered_embeddings = []

        for i in range(0, len(filtered_texts), batch_size):
            batch = filtered_texts[i : i + batch_size]
            batch_embeddings = self._embed_batch(batch)
            filtered_embeddings.extend(batch_embeddings)

        # Reconstruct output with empty embeddings for skipped texts
        all_embeddings = [[0.0] * self.dimensions for _ in texts]
        for orig_idx, embedding in zip(original_indices, filtered_embeddings):
            all_embeddings[orig_idx] = embedding

        return all_embeddings

    def _embed_batch(self, texts: List[str]) -> List[List[float]]:
        """Embed a single batch of texts using OpenAI-compatible format."""
        headers = {
            "Authorization": f"Bearer {self.api_key}",
            "Content-Type": "application/json",
        }

        # OpenAI compatible format: input is string or array of strings
        # For single text, use string; for batch, use array
        # Include dimensions parameter to control vector size
        if len(texts) == 1:
            payload = {
                "model": self.model,
                "input": texts[0],
                "dimensions": self.dimensions,
            }
        else:
            payload = {
                "model": self.model,
                "input": texts,
                "dimensions": self.dimensions,
            }

        logger.debug(f"Embedding request: model={self.model}, dimensions={self.dimensions}, texts={len(texts)}")

        try:
            with httpx.Client(timeout=60.0) as client:
                response = client.post(
                    self.API_URL,
                    headers=headers,
                    json=payload,
                )
                response.raise_for_status()
                data = response.json()

                # OpenAI format: data is array of {embedding, index, object}
                if "data" not in data:
                    raise ValueError(f"Unexpected response format: {data}")

                # Sort by index and extract embeddings
                embeddings = sorted(data["data"], key=lambda x: x["index"])
                result = [e["embedding"] for e in embeddings]
                
                # Verify dimensions
                if result and len(result[0]) != self.dimensions:
                    logger.warning(f"API returned {len(result[0])} dimensions, expected {self.dimensions}")
                
                return result

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
