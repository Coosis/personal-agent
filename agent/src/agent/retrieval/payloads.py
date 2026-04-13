"""Structured tool payload helpers for retrieved context and citations."""

from __future__ import annotations

import json
from typing import Any


def encode_tool_payload(context: str, citations: list[dict[str, Any]]) -> str:
    return json.dumps({
        "context": context,
        "citations": citations,
    })
