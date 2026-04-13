"""Runtime configuration for the agent skeleton."""

import os
from dataclasses import dataclass

DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 8090

DEFAULT_EMBEDDING_MODEL = "text-embedding-v4"
DEFAULT_EMBEDDING_API_URL = "https://dashscope.aliyuncs.com/compatible-mode/v1/embeddings"


@dataclass(frozen=True)
class Config:
    agent_api_key: str = ""
    agent_model: str = ""
    agent_base_url: str = ""

    embedding_api_key: str = ""
    embedding_model: str = DEFAULT_EMBEDDING_MODEL
    embedding_api_url: str = DEFAULT_EMBEDDING_API_URL

    host: str = DEFAULT_HOST
    port: int = DEFAULT_PORT
    database_url: str = ""

    log_level: str = "info"


def validate_settings(settings: Config) -> None:
    if settings.agent_api_key == "":
        raise ValueError("AGENT_API_KEY environment variable is required")
    if settings.agent_model == "":
        raise ValueError("AGENT_MODEL environment variable cannot be empty")
    if settings.agent_base_url == "":
        raise ValueError("AGENT_BASE_URL environment variable cannot be empty")

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
        agent_api_key=os.getenv("AGENT_API_KEY", ""),
        agent_model=os.getenv("AGENT_MODEL", ""),
        agent_base_url=os.getenv("AGENT_BASE_URL", ""),
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
