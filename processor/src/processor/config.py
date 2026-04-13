"""Configuration management for the processor."""

import os
from dataclasses import dataclass
from functools import lru_cache

DEFAULT_DATABASE_URL = "postgres://postgres:postgres@localhost:5433/agentdb"
DEFAULT_LEASE_DURATION_SECONDS = 60
DEFAULT_HEARTBEAT_INTERVAL_SECONDS = 20
DEFAULT_MAX_RETRIES = 3
DEFAULT_ALIBABA_EMBEDDING_MODEL = "text-embedding-v4"
DEFAULT_EMBEDDING_DIMENSIONS = 1024
DEFAULT_POLL_INTERVAL_SECONDS = 5
DEFAULT_CHUNK_SIZE = 512
DEFAULT_CHUNK_OVERLAP = 50
DEFAULT_OPENROUTER_MODEL = "qwen/qwen-2.5-72b-instruct"
DEFAULT_OPENROUTER_API_URL = "https://openrouter.ai/api/v1"


@dataclass(frozen=True)
class Config:
    database_url: str = DEFAULT_DATABASE_URL
    lease_duration_seconds: int = DEFAULT_LEASE_DURATION_SECONDS
    heartbeat_interval_seconds: int = DEFAULT_HEARTBEAT_INTERVAL_SECONDS
    max_retries: int = DEFAULT_MAX_RETRIES
    alibaba_api_key: str = ""
    alibaba_embedding_model: str = DEFAULT_ALIBABA_EMBEDDING_MODEL
    embedding_dimensions: int = DEFAULT_EMBEDDING_DIMENSIONS
    openrouter_api_key: str = ""
    openrouter_model: str = DEFAULT_OPENROUTER_MODEL
    openrouter_api_url: str = DEFAULT_OPENROUTER_API_URL
    poll_interval_seconds: int = DEFAULT_POLL_INTERVAL_SECONDS
    chunk_size: int = DEFAULT_CHUNK_SIZE
    chunk_overlap: int = DEFAULT_CHUNK_OVERLAP


def validate_config(cfg: Config) -> None:
    if not cfg.database_url:
        raise ValueError("DATABASE_URL environment variable is required")
    if not cfg.alibaba_api_key:
        raise ValueError("ALIBABA_API_KEY environment variable is required")
    if cfg.lease_duration_seconds <= 0:
        raise ValueError("LEASE_DURATION_SECONDS must be positive")
    if cfg.heartbeat_interval_seconds <= 0:
        raise ValueError("HEARTBEAT_INTERVAL_SECONDS must be positive")
    if cfg.heartbeat_interval_seconds >= cfg.lease_duration_seconds:
        raise ValueError(
            "HEARTBEAT_INTERVAL_SECONDS must be smaller than LEASE_DURATION_SECONDS"  # noqa: E501
        )
    if cfg.max_retries <= 0:
        raise ValueError("MAX_RETRIES must be positive")
    if cfg.poll_interval_seconds <= 0:
        raise ValueError("POLL_INTERVAL_SECONDS must be positive")
    if cfg.chunk_size <= 0:
        raise ValueError("CHUNK_SIZE must be positive")
    if cfg.chunk_overlap < 0:
        raise ValueError("CHUNK_OVERLAP cannot be negative")


@lru_cache(maxsize=1)
def get_config() -> Config:
    cfg = Config(
        database_url=os.getenv("DATABASE_URL", DEFAULT_DATABASE_URL),
        lease_duration_seconds=int(
            os.getenv("LEASE_DURATION_SECONDS", DEFAULT_LEASE_DURATION_SECONDS)
        ),
        heartbeat_interval_seconds=int(
            os.getenv("HEARTBEAT_INTERVAL_SECONDS", DEFAULT_HEARTBEAT_INTERVAL_SECONDS)
        ),
        max_retries=int(os.getenv("MAX_RETRIES", DEFAULT_MAX_RETRIES)),
        alibaba_api_key=os.getenv("ALIBABA_API_KEY", ""),
        alibaba_embedding_model=os.getenv(
            "ALIBABA_EMBEDDING_MODEL", DEFAULT_ALIBABA_EMBEDDING_MODEL
        ),
        embedding_dimensions=int(os.getenv("EMBEDDING_DIMENSIONS", DEFAULT_EMBEDDING_DIMENSIONS)),
        openrouter_api_key=os.getenv("OPENROUTER_API_KEY", ""),
        openrouter_model=os.getenv("OPENROUTER_MODEL", DEFAULT_OPENROUTER_MODEL),
        openrouter_api_url=os.getenv("OPENROUTER_API_URL", DEFAULT_OPENROUTER_API_URL),
        poll_interval_seconds=int(
            os.getenv("POLL_INTERVAL_SECONDS", DEFAULT_POLL_INTERVAL_SECONDS)
        ),
        chunk_size=int(os.getenv("CHUNK_SIZE", DEFAULT_CHUNK_SIZE)),
        chunk_overlap=int(os.getenv("CHUNK_OVERLAP", DEFAULT_CHUNK_OVERLAP)),
    )
    validate_config(cfg)
    return cfg
