# ruff: noqa: E501
"""Prompt helpers for rolling conversation summaries."""

from __future__ import annotations

import json

SYSTEM_PROMPT = """
You maintain a rolling cross-chat conversation summary for a personal knowledge agent.

Return only JSON with this shape, no other text or markdown:
{{
  "summary_text": "compact paragraph summary",
  "keywords": ["keyword"],
  "active_topics": ["topic"],
  "entities": ["entity"],
  "project_state": [
    {{
      "name": "project or context name",
      "decision": "current status or focus",
      "options": ["next option"],
    }}
  ],
  "candidate_memories": [
    {{
      "category": "profile | preference | relationship | project | event | routine | other",
      "key": "short_canonical_key",
      "value": "short fact statement"
    }}
  ]
}}

Rules:
- Maintain a concise but information-dense rolling summary.
- Preserve active topics, unresolved decisions, and notable context that may matter in future chats.
- Prefer facts directly supported by the conversation or related words.
- Keep `summary_text` under 80 words.
- `keywords` should include verbs, nouns, and adjectives together with some of their extrapolated related terms. Examples:
  - play football -> `kick`, `run`, `exercise`
  - finish essay -> `write`, `edit`, `research`
  - go to restaurant -> `eat`, `go`, `order`
- `keywords` should include short searchable terms, aliases, abbreviations, technologies, and closely related concepts.
- Include broader parent-category terms when clearly useful for future retrieval, not just narrow specifics.
- If you include a specific item, also include its general class when obvious. Examples:
  - `lunch` -> also include `food`, `meal`
  - `protein` -> also include `food`, `nutrition`
  - `postgres` -> also include `database`, `sql`
- Keep lists small:
  - `keywords`: at most 12
  - `active_topics`: at most 3
  - `entities`: at most 5
  - `project_state`: at most 1 item, with at most 3 options
  - `candidate_memories`: at most 2 items
- Keep each string short. Prefer noun phrases over full sentences except for `summary_text`.
- Do not restate the transcript verbatim.
- `active_topics`, `open_questions`, `entities` should be short lists.
- `candidate_memories` should contain only durable facts worth later promotion into long-term memory.
- If prior summary/state exists, update it rather than starting over.
""".strip()


RETRY_SYSTEM_PROMPT = """
Return one small valid JSON object only.

Use exactly this shape:
{
  "summary_text": "short summary",
  "keywords": ["keyword"],
  "active_topics": ["topic"],
  "entities": ["entity"],
  "project_state": [{"name": "name", "decision": "decision", "options": ["option"]}],
  "candidate_memories": [{"category": "category", "key": "key", "value": "value"}]
}

Rules:
- Output valid minified JSON only.
- Keep `summary_text` under 60 words.
- Keep every list very small.
- Keep strings short.
- Do not include explanations or markdown.
""".strip()


def build_user_prompt(
    previous_summary: str,
    previous_keywords: list[str],
    previous_state: object,
    transcript: str,
) -> str:
    previous_keywords = previous_keywords[:8]
    previous_state_json = json.dumps(previous_state, ensure_ascii=True, separators=(",", ":"))
    if len(previous_state_json) > 600:
        previous_state_json = previous_state_json[:600]
    transcript = transcript.strip()
    if len(transcript) > 1800:
        transcript = transcript[-1800:]
    return (
        "Previous summary:\n"
        f"{previous_summary.strip()[:400] or '(none)'}\n\n"
        "Previous keywords:\n"
        f"{previous_keywords}\n\n"
        "Previous structured state JSON:\n"
        f"{previous_state_json}\n\n"
        "New completed conversation messages:\n"
        f"{transcript}"
    )
