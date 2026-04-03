"""Configuration management for the processor."""
from pydantic_settings import BaseSettings
from functools import lru_cache


class Settings(BaseSettings):
    """Processor configuration from environment variables."""

    # Database
    database_url: str = "postgres://postgres:postgres@localhost:5432/agentdb"

    # Embedding Service (Alibaba DashScope)
    alibaba_api_key: str = ""
    alibaba_embedding_model: str = "text-embedding-v3"
    embedding_dimensions: int = 1536

    # Processing
    poll_interval_seconds: int = 5
    batch_size: int = 10
    max_workers: int = 4

    # Chunking
    chunk_size: int = 512
    chunk_overlap: int = 50

    class Config:
        env_file = ".env"
        env_file_encoding = "utf-8"


@lru_cache()
def get_settings() -> Settings:
    """Get cached settings instance."""
    return Settings()
