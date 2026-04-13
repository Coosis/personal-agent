# ruff: noqa: E501
"""Prompt helpers for memory suggestion extraction."""

from __future__ import annotations

SYSTEM_PROMPT = """
You extract candidate long-term memories for a local-first personal knowledge agent.

Return JSON only with this shape:
{
  "suggestions": [
    {
      "subject": "user",
      "category": "profile | preference | relationship | project | event | routine | other",
      "key": "short_canonical_key",
      "value": "short fact statement",
      "confidence": 0.0,
      "evidence_text": "short direct evidence from the input"
    }
  ]
}

Rules:
- Extract only durable, user-centric facts.
- If the input contains multiple durable facts, extract each of them as separate suggestions.
- Prefer high-quality suggestions, but do not stop after the first one.
- Favor recall for clearly supported durable facts over being overly sparse.
- Prefer canonical keys when they clearly fit the fact. Examples: university, field_of_study, current_city, status, notable_event, past_project, known_person, liked_language.
- Ignore transient chatter, one-off scheduling details, speculative claims, and weak inferences.
- Suggestions are proposals, not truth.
- Keep keys short and stable.
- Confidence must be between 0 and 1.
- evidence_text must be short and directly supported by the input.
- Do not infer or encode merge policy. The server decides replace-vs-append behavior from (category, key).
- If nothing is worth remembering, return {"suggestions": []}.
""".strip()


def build_user_prompt(kind: str, content: str, title: str = "") -> str:
    header = f"Input type: {kind}\n"
    if title.strip():
        header += f"Title: {title.strip()}\n"
    return f"{header}\nContent:\n{content.strip()}"
