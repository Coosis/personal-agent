"""Memory suggestion extraction and persistence helpers."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

import httpx

from processor.config import Config
from processor.db import CreateMemorySuggestionParams, FindPendingMemorySuggestionMatchParams, query, transaction
from processor.heartbeat import Heartbeat
from processor.memory_prompt import SYSTEM_PROMPT, build_user_prompt
from processor.runtime import PermanentJobError, coerce_int, ensure_mapping
from sqlc.pydb.models import Job

EXTRACTOR_TYPE = "llm_v1"


@dataclass(frozen=True)
class SuggestionPayload:
    subject: str
    category: str
    key: str
    value: str
    confidence: float
    evidence_text: str


@dataclass(frozen=True)
class ExtractionTarget:
    kind: str
    title: str
    content: str
    source_id: int | None
    document_id: int | None
    message_id: int | None


def process_extract_memory_suggestions(cfg: Config, heartbeat: Heartbeat, job: Job) -> None:
    heartbeat.ensure_active()
    payload = ensure_mapping(job.payload)
    note_id = payload.get("note_id")
    message_id = payload.get("message_id")

    if note_id is not None:
        target = load_note_memory_target(coerce_int(note_id, "note_id"))
    elif message_id is not None:
        target = load_message_memory_target(coerce_int(message_id, "message_id"))
    else:
        raise PermanentJobError("extract_memory_suggestions requires note_id or message_id")

    heartbeat.ensure_active()
    suggestions = extract_memory_suggestions(
        cfg,
        heartbeat,
        kind=target.kind,
        title=target.title,
        content=target.content,
    )

    heartbeat.ensure_active()
    with transaction() as conn:
        q = query(conn)
        store_suggestions(
            q,
            heartbeat,
            suggestions,
            source_id=target.source_id,
            document_id=target.document_id,
            message_id=target.message_id,
        )
        heartbeat.ensure_active()
        q.complete_job(id=job.id)


def load_note_memory_target(note_id: int) -> ExtractionTarget:
    with transaction() as conn:
        q = query(conn)
        note = q.get_note_by_id(id=note_id)
        if note is None:
            raise PermanentJobError(f"note {note_id} not found")

        document = q.get_document_by_source_id(source_id=note.source_id)
        if document is None:
            raise PermanentJobError(f"document for note {note_id} not found")

        return ExtractionTarget(
            kind="note",
            title=note.title,
            content=note.body,
            source_id=note.source_id,
            document_id=document.id,
            message_id=None,
        )


def load_message_memory_target(message_id: int) -> ExtractionTarget:
    with transaction() as conn:
        q = query(conn)
        message = q.get_message_by_id(id=message_id)
        if message is None:
            raise PermanentJobError(f"message {message_id} not found")
        if message.role != "user":
            raise PermanentJobError(f"message {message_id} is not a user message")

        return ExtractionTarget(
            kind="user_message",
            title="",
            content=message.content,
            source_id=None,
            document_id=None,
            message_id=message_id,
        )


def extract_memory_suggestions(
    cfg: Config,
    heartbeat: Heartbeat,
    kind: str,
    content: str,
    title: str = "",
) -> list[SuggestionPayload]:
    if not cfg.openrouter_api_key:
        raise PermanentJobError("OPENROUTER_API_KEY is required for memory extraction")

    trimmed = content.strip()
    if not trimmed:
        return []

    heartbeat.ensure_active()
    response = httpx.post(
        f"{cfg.openrouter_api_url.rstrip('/')}/chat/completions",
        headers={
            "Authorization": f"Bearer {cfg.openrouter_api_key}",
            "Content-Type": "application/json",
        },
        json={
            "model": cfg.openrouter_model,
            "temperature": 0,
            "response_format": {"type": "json_object"},
            "messages": [
                {"role": "system", "content": SYSTEM_PROMPT},
                {"role": "user", "content": build_user_prompt(kind=kind, title=title, content=trimmed)},
            ],
        },
        timeout=60,
    )
    response.raise_for_status()

    heartbeat.ensure_active()
    payload = response.json()
    raw_content = payload["choices"][0]["message"]["content"]
    if not isinstance(raw_content, str):
        raise PermanentJobError("memory extraction response did not contain JSON text")

    parsed = json.loads(raw_content)
    suggestions = parsed.get("suggestions", [])
    if not isinstance(suggestions, list):
        raise PermanentJobError("memory extraction response has invalid suggestions payload")

    items: list[SuggestionPayload] = []
    for raw in suggestions:
        heartbeat.ensure_active()
        item = normalize_suggestion(raw)
        if item is not None:
            items.append(item)
    return items


def normalize_suggestion(raw: Any) -> SuggestionPayload | None:
    if not isinstance(raw, dict):
        return None

    subject = str(raw.get("subject", "")).strip() or "user"
    category = str(raw.get("category", "")).strip().lower()
    key = str(raw.get("key", "")).strip().lower().replace(" ", "_")
    value = str(raw.get("value", "")).strip()
    evidence_text = str(raw.get("evidence_text", "")).strip()

    try:
        confidence = float(raw.get("confidence", 0))
    except (TypeError, ValueError):
        confidence = 0

    confidence = max(0.0, min(1.0, confidence))
    if not category or not key or not value or not evidence_text:
        return None

    return SuggestionPayload(
        subject=subject[:120],
        category=category[:80],
        key=key[:120],
        value=value[:500],
        confidence=confidence,
        evidence_text=evidence_text[:500],
    )


def store_suggestions(
    q,
    heartbeat: Heartbeat,
    suggestions: list[SuggestionPayload],
    source_id: int | None,
    document_id: int | None,
    message_id: int | None,
) -> None:
    for suggestion in suggestions:
        heartbeat.ensure_active()
        if q.find_active_memory_match(
            subject=suggestion.subject,
            category=suggestion.category,
            key=suggestion.key,
            value=suggestion.value,
        ) is not None:
            continue

        existing = q.find_pending_memory_suggestion_match(
            FindPendingMemorySuggestionMatchParams(
                subject=suggestion.subject,
                category=suggestion.category,
                key=suggestion.key,
                value=suggestion.value,
                source_id=source_id,
                document_id=document_id,
                message_id=message_id,
            )
        )
        if existing is not None:
            continue

        q.create_memory_suggestion(
            CreateMemorySuggestionParams(
                subject=suggestion.subject,
                category=suggestion.category,
                key=suggestion.key,
                value=suggestion.value,
                confidence=suggestion.confidence,
                status="pending",
                extractor_type=EXTRACTOR_TYPE,
                source_id=source_id,
                document_id=document_id,
                message_id=message_id,
                evidence_text=suggestion.evidence_text,
            )
        )
