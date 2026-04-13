"""Runtime configuration for the agent skeleton."""

import os
from dataclasses import dataclass

DEFAULT_MODEL = "minimax/minimax-m2.7"
OPENROUTER_BASE_URL = "https://openrouter.ai/api/v1"

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8090

DEFAULT_EMBEDDING_MODEL = "text-embedding-v4"
DEFAULT_EMBEDDING_API_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/embeddings"


@dataclass(frozen=True)
class Config:
    openrouter_api_key: str = ""
    openrouter_model: str = DEFAULT_MODEL
    openrouter_api_url: str = OPENROUTER_BASE_URL

    embedding_api_key: str = ""
    embedding_model: str = DEFAULT_EMBEDDING_MODEL
    embedding_api_url: str = DEFAULT_EMBEDDING_API_URL

    host: str = DEFAULT_HOST
    port: int = DEFAULT_PORT
    database_url: str = ""

    log_level: str = "info"


def validate_settings(settings: Config) -> None:
    if settings.openrouter_api_key == "":
        raise ValueError("OPENROUTER_API_KEY environment variable is required")
    if settings.openrouter_model == "":
        raise ValueError("OPENROUTER_MODEL environment variable cannot be empty")
    if settings.openrouter_api_url == "":
        raise ValueError("OPENROUTER_API_URL environment variable cannot be empty")

    if settings.embedding_api_key == "":
        raise ValueError("ALIBABA_API_KEY environment variable is required for embeddings")
    if settings.embedding_model == "":
        raise ValueError("ALIBABA_EMBEDDING_MODEL environment variable cannot be empty")
    if settings.embedding_api_url == "":
        raise ValueError("ALIBABA_EMBEDDING_API_URL environment variable cannot be empty")

    if settings.host == "":
        raise ValueError("AGENT_HOST environment variable cannot be empty")
    if settings.port == "":
        raise ValueError("AGENT_PORT environment variable cannot be empty")
    if settings.database_url == "":
        raise ValueError("DATABASE_URL environment variable is required")
    if settings.log_level not in {
        "debug",
        "info",
        "warning",
        "error",
        "critical",
    }:
        raise ValueError(f"Invalid LOG_LEVEL: {settings.log_level}")


def get_config() -> Config:
    s = Config(
        openrouter_api_key=os.getenv("OPENROUTER_API_KEY", ""),
        openrouter_model=os.getenv("OPENROUTER_MODEL", DEFAULT_MODEL),
        openrouter_api_url=os.getenv("OPENROUTER_API_URL", OPENROUTER_BASE_URL),
        embedding_api_key=os.getenv("ALIBABA_API_KEY", ""),
        embedding_model=os.getenv("ALIBABA_EMBEDDING_MODEL", DEFAULT_EMBEDDING_MODEL),
        embedding_api_url=os.getenv("ALIBABA_EMBEDDING_API_URL", DEFAULT_EMBEDDING_API_URL),
        host=os.getenv("AGENT_HOST", DEFAULT_HOST),
        port=int(os.getenv("AGENT_PORT", DEFAULT_PORT)),
        database_url=os.getenv("DATABASE_URL", ""),
        log_level=os.getenv("LOG_LEVEL", "info"),
    )
    try:
        validate_settings(s)
    except ValueError as exc:
        raise ValueError(f"Invalid configuration: {exc}") from exc
    return s
