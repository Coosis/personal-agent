"""Shared helpers and constants for agent graph nodes."""

from __future__ import annotations

import json
from typing import Annotated

GENERATE_RESPONSE_STREAM_KEY = "generate_response_msg"
MAX_REACT_STEPS = 4


def coerce_json_dict(raw: object) -> dict | None:
    if isinstance(raw, dict):
        return raw

    if isinstance(raw, str):
        text = raw.strip()
        if not text:
            return None

        try:
            parsed = json.loads(text)
            if isinstance(parsed, dict):
                return parsed
        except json.JSONDecodeError:
            start = text.find("{")
            end = text.rfind("}")
            if start >= 0 and end > start:
                try:
                    parsed = json.loads(text[start : end + 1])
                    if isinstance(parsed, dict):
                        return parsed
                except json.JSONDecodeError:
                    pass

    return None


def take_recent_history(
    messages: Annotated[list, "conversation_messages"],
) -> list:
    """Take the most recent messages from the conversation history."""
    return messages[-4:] if messages else []
