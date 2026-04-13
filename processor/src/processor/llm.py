"""Helpers for OpenAI-compatible chat completion providers."""

from __future__ import annotations

from typing import Any

import httpx

from processor.runtime import PermanentJobError
from processor.config import Config


def ensure_extraction_api_key(cfg: Config, purpose: str) -> None:
    if not cfg.extraction_api_key:
        raise PermanentJobError(f"EXTRACTION_API_KEY is required for {purpose}")


def post_chat_completion(
    cfg: Config,
    *,
    model: str,
    messages: list[dict[str, str]],
    timeout: float = 60,
    **payload: Any,
) -> httpx.Response:
    return httpx.post(
        f"{cfg.extraction_base_url.rstrip('/')}/chat/completions",
        headers={
            "Authorization": f"Bearer {cfg.extraction_api_key}",
            "Content-Type": "application/json",
        },
        json={
            "model": model,
            "messages": messages,
            **payload,
        },
        timeout=timeout,
    )
