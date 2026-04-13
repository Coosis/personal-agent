"""Database helpers for the processor job worker."""

from __future__ import annotations

from collections.abc import Generator
from contextlib import contextmanager

import sqlalchemy
from pgvector.psycopg import register_vector
from sqlalchemy import event

from processor.config import get_config
from sqlc.pydb import models
from sqlc.pydb.query import (
    CreateChunkParams,
    CreateDocumentParams,
    CreateJobParams,
    CreateMemorySuggestionParams,
    FindPendingMemorySuggestionMatchParams,
    Querier,
    UpdateDocumentBasicsParams,
    UpsertSourceItemParams,
)

_engine: sqlalchemy.Engine | None = None


def _sqlalchemy_database_url(database_url: str) -> str:
    if database_url.startswith("postgresql+psycopg://"):
        return database_url
    if database_url.startswith("postgresql://"):
        return "postgresql+psycopg://" + database_url[len("postgresql://") :]
    if database_url.startswith("postgres://"):
        return "postgresql+psycopg://" + database_url[len("postgres://") :]
    return database_url


def init_engine() -> sqlalchemy.Engine:
    global _engine
    if _engine is None:
        cfg = get_config()
        _engine = sqlalchemy.create_engine(_sqlalchemy_database_url(cfg.database_url))
        event.listen(_engine, "connect", _register_vector)
    return _engine


def close_engine() -> None:
    global _engine
    if _engine is not None:
        _engine.dispose()
        _engine = None


def _register_vector(dbapi_connection, _connection_record) -> None:
    register_vector(dbapi_connection)


@contextmanager
def transaction() -> Generator[sqlalchemy.engine.Connection]:
    engine = init_engine()
    with engine.begin() as conn:
        yield conn


def query(conn: sqlalchemy.engine.Connection) -> Querier:
    return Querier(conn)


__all__ = [
    "CreateChunkParams",
    "CreateDocumentParams",
    "CreateJobParams",
    "CreateMemorySuggestionParams",
    "FindPendingMemorySuggestionMatchParams",
    "Querier",
    "UpdateDocumentBasicsParams",
    "UpsertSourceItemParams",
    "close_engine",
    "init_engine",
    "models",
    "query",
    "transaction",
]
