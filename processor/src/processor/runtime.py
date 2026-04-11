from __future__ import annotations

from dataclasses import dataclass
from typing import Any


class RetryableJobError(Exception):
    """A temporary failure that should be retried."""


class PermanentJobError(Exception):
    """A terminal failure that should not be retried."""


@dataclass(frozen=True)
class LoadedDocument:
    document_id: int
    source_id: int
    source_type: str
    title: str
    mime_type: str
    metadata: dict[str, Any]
    content: str
    content_hash: str
    extension: str


@dataclass(frozen=True)
class ScannedFile:
    item_key: str
    locator: str
    display_name: str
    fingerprint: str
    mime_type: str
    metadata: dict[str, Any]


def ensure_mapping(payload: Any) -> dict[str, Any]:
    return payload if isinstance(payload, dict) else {}


def coerce_int(value: Any, key: str) -> int:
    if isinstance(value, bool):
        raise PermanentJobError(f"{key} must be an integer")
    if isinstance(value, int):
        return value
    raise PermanentJobError(f"{key} is required")
