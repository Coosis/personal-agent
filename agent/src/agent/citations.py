"""Citation marker helpers shared across the agent."""

import re

CITATION_MARKER_TEMPLATE = "[[cite:{citation_id}]]"
_CITATION_ID_RE = r"[A-Z]\d+"
_CITATION_MARKER_RE = re.compile(r"\[\[cite:([A-Z]\d+)\]\]")


def format_citation_marker(citation_id: str) -> str:
    """Build the canonical inline citation marker for a citation id."""
    return CITATION_MARKER_TEMPLATE.format(citation_id=citation_id)


def extract_citation_ids(text: str) -> set[str]:
    """Extract cited ids from assistant output."""
    return set(_CITATION_MARKER_RE.findall(text))


def is_valid_citation_id(value: object) -> bool:
    """Validate the raw citation id before remapping it."""
    return isinstance(value, str) and re.fullmatch(_CITATION_ID_RE, value) is not None
