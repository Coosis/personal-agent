"""Rolling conversation summary processor."""

from __future__ import annotations

import json
import logging
import re
from dataclasses import dataclass
from typing import Any

import httpx
from psycopg.types.json import Jsonb

from processor.config import Config
from processor.db import query, transaction
from processor.heartbeat import Heartbeat
from processor.conversation_summary_prompt import (
    RETRY_SYSTEM_PROMPT,
    SYSTEM_PROMPT,
    build_user_prompt,
)
from processor.runtime import PermanentJobError, coerce_int, ensure_mapping
from sqlc.pydb.models import Job
from sqlc.pydb.query import UpsertConversationSummaryParams

logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class SummaryState:
    conversation_id: int
    summary_text: str
    state_text: str
    keywords: list[str]
    metadata: dict[str, Any]
    last_message_id: int
    pass_index: int


def process_summarize_conversation(cfg: Config, heartbeat: Heartbeat, job: Job) -> None:
    payload = ensure_mapping(job.payload)
    conversation_id = coerce_int(payload.get("conversation_id"), "conversation_id")
    up_to_message_id = coerce_int(payload.get("up_to_message_id"), "up_to_message_id")
    pass_index = coerce_int(payload.get("pass_index"), "pass_index")

    heartbeat.ensure_active()
    summary_state = build_summary_state(
        cfg,
        heartbeat,
        conversation_id=conversation_id,
        up_to_message_id=up_to_message_id,
        pass_index=pass_index,
    )

    heartbeat.ensure_active()
    with transaction() as conn:
        q = query(conn)
        conversation = q.get_conversation_by_id(id=conversation_id)
        if conversation is None:
            raise PermanentJobError(f"conversation {conversation_id} not found")

        logger.info(
            "writing conversation summary for conversation %s up to message %s (pass %s)",
            conversation_id,
            summary_state.last_message_id,
            summary_state.pass_index,
        )
        q.upsert_conversation_summary(
            UpsertConversationSummaryParams(
                conversation_id=conversation_id,
                summary_text=summary_state.summary_text,
                state_text=summary_state.state_text,
                keywords=summary_state.keywords,
                keywords_text=build_keywords_text(summary_state.keywords),
                metadata_text=build_metadata_text(summary_state.metadata),
                metadata=Jsonb(summary_state.metadata),
                last_message_id=summary_state.last_message_id,
                pass_index=summary_state.pass_index,
            )
        )
        q.update_conversation_summary(
            id=conversation_id,
            summary=summary_state.summary_text,
        )
        logger.info("marking summarize_conversation job %s complete", job.id)
        q.complete_job(id=job.id)


def build_summary_state(
    cfg: Config,
    heartbeat: Heartbeat,
    conversation_id: int,
    up_to_message_id: int,
    pass_index: int,
) -> SummaryState:
    logger.info(
        "building summary state for conversation %s up to message %s (pass %s)",
        conversation_id,
        up_to_message_id,
        pass_index,
    )
    with transaction() as conn:
        q = query(conn)
        conversation = q.get_conversation_by_id(id=conversation_id)
        if conversation is None:
            raise PermanentJobError(f"conversation {conversation_id} not found")

        existing = q.get_conversation_summary_by_conversation_id(conversation_id=conversation_id)
        # previous
        previous_summary = ""
        previous_keywords: list[str] = []
        previous_metadata: dict[str, Any] = {}
        after_message_id = 0
        if existing is not None:
            # previous
            previous_summary = existing.summary_text
            previous_keywords = coerce_keywords(existing.keywords)
            if isinstance(existing.metadata, dict):
                previous_metadata = existing.metadata
            after_message_id = existing.last_message_id or 0

            if up_to_message_id <= after_message_id and pass_index <= existing.pass_index:
                return SummaryState(
                    conversation_id=conversation_id,
                    summary_text=existing.summary_text,
                    state_text=existing.state_text,
                    keywords=previous_keywords,
                    metadata=previous_metadata,
                    last_message_id=existing.last_message_id or up_to_message_id,
                    pass_index=existing.pass_index,
                )

        messages = list(
            q.list_completed_messages_by_conversation_range(
                conversation_id=conversation_id,
                after_message_id=after_message_id,
                up_to_message_id=up_to_message_id,
            )
        )
        logger.info(
            "loaded %s completed messages for conversation %s summary",
            len(messages),
            conversation_id,
        )

    transcript = format_summary_messages(messages)
    if not transcript:
        state_text = build_state_text(previous_summary, previous_metadata)
        return SummaryState(
            conversation_id=conversation_id,
            summary_text=previous_summary,
            state_text=state_text,
            keywords=previous_keywords,
            metadata=previous_metadata,
            last_message_id=up_to_message_id,
            pass_index=pass_index,
        )

    heartbeat.ensure_active()
    logger.info("requesting summary model for conversation %s", conversation_id)
    generated = generate_summary(
        cfg,
        heartbeat,
        previous_summary=previous_summary,
        previous_keywords=previous_keywords,
        previous_state=previous_metadata,
        transcript=transcript,
    )
    logger.info("received parsed summary payload for conversation %s", conversation_id)
    summary_text = str(generated.get("summary_text", "")).strip()
    if not summary_text:
        summary_text = previous_summary
    keywords = coerce_keywords(generated.get("keywords"))
    if not keywords:
        keywords = previous_keywords

    metadata = {
        "keywords": keywords,
        "active_topics": coerce_string_list(generated.get("active_topics")),
        "open_questions": coerce_string_list(generated.get("open_questions")),
        "entities": coerce_string_list(generated.get("entities")),
        "project_state": coerce_project_state(generated.get("project_state")),
        "episodic_notes": coerce_string_list(generated.get("episodic_notes")),
        "candidate_memories": coerce_candidate_memories(generated.get("candidate_memories")),
    }
    state_text = build_state_text(summary_text, metadata)
    return SummaryState(
        conversation_id=conversation_id,
        summary_text=summary_text,
        state_text=state_text,
        keywords=keywords,
        metadata=metadata,
        last_message_id=up_to_message_id,
        pass_index=pass_index,
    )


def generate_summary(
    cfg: Config,
    heartbeat: Heartbeat,
    previous_summary: str,
    previous_keywords: list[str],
    previous_state: dict[str, Any],
    transcript: str,
) -> dict[str, Any]:
    if not cfg.openrouter_api_key:
        raise PermanentJobError("OPENROUTER_API_KEY is required for conversation summaries")

    heartbeat.ensure_active()
    user_prompt = build_user_prompt(
        previous_summary=previous_summary,
        previous_keywords=previous_keywords,
        previous_state=previous_state,
        transcript=transcript,
    )
    return request_summary_json(
        cfg=cfg,
        heartbeat=heartbeat,
        system_prompt=SYSTEM_PROMPT,
        user_prompt=user_prompt,
        max_tokens=320,
        retry_system_prompt=RETRY_SYSTEM_PROMPT,
        retry_max_tokens=220,
    )


def request_summary_json(
    cfg: Config,
    heartbeat: Heartbeat,
    system_prompt: str,
    user_prompt: str,
    max_tokens: int,
    retry_system_prompt: str,
    retry_max_tokens: int,
) -> dict[str, Any]:
    raw_content = request_summary_content(
        cfg=cfg,
        heartbeat=heartbeat,
        system_prompt=system_prompt,
        user_prompt=user_prompt,
        max_tokens=max_tokens,
    )
    try:
        parsed = json.loads(raw_content)
    except json.JSONDecodeError:
        logger.warning("summary JSON parse failed, retrying with compact prompt")
        heartbeat.ensure_active()
        raw_content = request_summary_content(
            cfg=cfg,
            heartbeat=heartbeat,
            system_prompt=retry_system_prompt,
            user_prompt=user_prompt,
            max_tokens=retry_max_tokens,
        )
        parsed = json.loads(raw_content)

    if not isinstance(parsed, dict):
        raise PermanentJobError("conversation summary response has invalid payload")
    return parsed


def request_summary_content(
    cfg: Config,
    heartbeat: Heartbeat,
    system_prompt: str,
    user_prompt: str,
    max_tokens: int,
) -> str:
    response = httpx.post(
        f"{cfg.openrouter_api_url.rstrip('/')}/chat/completions",
        headers={
            "Authorization": f"Bearer {cfg.openrouter_api_key}",
            "Content-Type": "application/json",
        },
        json={
            "model": cfg.openrouter_model,
            "temperature": 0,
            "max_tokens": max_tokens,
            "response_format": {"type": "json_object"},
            "messages": [
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_prompt},
            ],
        },
        timeout=60,
    )
    response.raise_for_status()

    heartbeat.ensure_active()
    logger.info("summary model response received, parsing provider payload")
    payload = response.json()
    raw_content = payload["choices"][0]["message"]["content"]
    if not isinstance(raw_content, str):
        raise PermanentJobError("conversation summary response did not contain JSON text")
    if len(raw_content) > 12000:
        raise PermanentJobError("conversation summary response too large")
    logger.info("summary model returned %s chars of JSON content", len(raw_content))
    heartbeat.ensure_active()
    return raw_content


def format_summary_messages(messages: list[Any]) -> str:
    lines: list[str] = []
    for message in messages:
        role = getattr(message, "role", "")
        content = getattr(message, "content", "")
        if not isinstance(role, str) or not isinstance(content, str):
            continue
        if role not in {"user", "assistant", "system"}:
            continue
        label = role.capitalize()
        lines.append(f"{label}: {content.strip()}")
    return "\n".join(line for line in lines if line.strip())


def coerce_string_list(raw: Any) -> list[str]:
    if not isinstance(raw, list):
        return []
    values: list[str] = []
    seen: set[str] = set()
    for item in raw:
        if not isinstance(item, str):
            continue
        value = " ".join(item.strip().split())
        if not value or value in seen:
            continue
        seen.add(value)
        values.append(value[:200])
    return values


def coerce_keywords(raw: Any) -> list[str]:
    if not isinstance(raw, list):
        return []
    values: list[str] = []
    seen: set[str] = set()
    for item in raw:
        if not isinstance(item, str):
            continue
        for part in split_keyword_parts(item):
            for value in expand_keyword(normalize_keyword(part)):
                if not value or value in seen:
                    continue
                seen.add(value)
                values.append(value[:80])
    return values


def coerce_project_state(raw: Any) -> list[dict[str, Any]]:
    if not isinstance(raw, list):
        return []
    items: list[dict[str, Any]] = []
    for item in raw:
        if not isinstance(item, dict):
            continue
        name = " ".join(str(item.get("name", "")).strip().split())
        decision = " ".join(str(item.get("decision", "")).strip().split())
        options = coerce_string_list(item.get("options"))
        if not name and not decision and not options:
            continue
        items.append(
            {
                "name": name[:200],
                "decision": decision[:200],
                "options": options[:10],
            }
        )
    return items


def coerce_candidate_memories(raw: Any) -> list[dict[str, str]]:
    if not isinstance(raw, list):
        return []
    items: list[dict[str, str]] = []
    for item in raw:
        if not isinstance(item, dict):
            continue
        category = str(item.get("category", "")).strip().lower()
        key = str(item.get("key", "")).strip().lower().replace(" ", "_")
        value = " ".join(str(item.get("value", "")).strip().split())
        if not category or not key or not value:
            continue
        items.append(
            {
                "category": category[:80],
                "key": key[:120],
                "value": value[:300],
            }
        )
    return items


def build_state_text(summary_text: str, metadata: dict[str, Any]) -> str:
    parts: list[str] = []
    if summary_text.strip():
        parts.append(summary_text.strip())
    for keyword in coerce_keywords(metadata.get("keywords")):
        parts.append(keyword)
    for key in ["active_topics", "open_questions", "entities", "episodic_notes"]:
        for item in coerce_string_list(metadata.get(key)):
            parts.append(item)
    for item in coerce_project_state(metadata.get("project_state")):
        for value in [item.get("name", ""), item.get("decision", "")]:
            if isinstance(value, str) and value.strip():
                parts.append(value)
        for option in item.get("options", []):
            if isinstance(option, str) and option.strip():
                parts.append(option)
    for item in coerce_candidate_memories(metadata.get("candidate_memories")):
        parts.append(item["category"])
        parts.append(item["key"])
        parts.append(item["value"])
    return "\n".join(parts)


def build_keywords_text(keywords: list[str]) -> str:
    return " ".join(coerce_keywords(keywords))


def build_metadata_text(metadata: dict[str, Any]) -> str:
    parts: list[str] = []
    for key in ["active_topics", "open_questions", "entities", "episodic_notes"]:
        parts.extend(coerce_string_list(metadata.get(key)))
    for item in coerce_project_state(metadata.get("project_state")):
        for value in [item.get("name", ""), item.get("decision", "")]:
            if isinstance(value, str) and value.strip():
                parts.append(value)
        for option in item.get("options", []):
            if isinstance(option, str) and option.strip():
                parts.append(option)
    for item in coerce_candidate_memories(metadata.get("candidate_memories")):
        parts.append(item["category"])
        parts.append(item["key"])
        parts.append(item["value"])
    return "\n".join(parts)


def normalize_keyword(raw: str) -> str:
    value = " ".join(raw.strip().split()).lower()
    if not value:
        return ""

    value = value.strip("\"'`“”‘’")
    value = re.sub(r"^[\[\(\{]+|[\]\)\}]+$", "", value)
    value = re.sub(r"^[,;:.!?]+|[,;:.!?]+$", "", value)
    value = " ".join(value.split())
    return value[:80]


def split_keyword_parts(raw: str) -> list[str]:
    parts = re.split(r"[,;\n]+", raw)
    return [part for part in parts if part.strip()]


def expand_keyword(keyword: str) -> list[str]:
    if not keyword:
        return []

    values = [keyword]
    values.extend(KEYWORD_EXPANSIONS.get(keyword, []))
    return values


KEYWORD_EXPANSIONS = {
    "breakfast": ["meal", "food"],
    "brunch": ["meal", "food"],
    "carb": ["nutrition", "food"],
    "carbs": ["nutrition", "food"],
    "database": ["data", "storage"],
    "dinner": ["meal", "food"],
    "food": ["meal", "nutrition"],
    "lunch": ["meal", "food"],
    "meal": ["food", "nutrition"],
    "mysql": ["database", "sql"],
    "nutrition": ["food", "health"],
    "postgres": ["postgresql", "database", "sql"],
    "postgresql": ["postgres", "database", "sql"],
    "protein": ["nutrition", "food"],
    "snack": ["meal", "food"],
}
