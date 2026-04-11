"""Database access helpers for the agent."""
from typing import List

import sqlalchemy
from pgvector.psycopg import register_vector
from sqlalchemy import event
from sqlc.pydb.query import Querier, SearchVectorChunksRow

from agent.config import Config

class AgentDB:
    def __init__(self, cfg: Config):
        self.engine = sqlalchemy.create_engine(_sqlalchemy_database_url(cfg.database_url))
        event.listen(self.engine, "connect", self._register_vector)

    @staticmethod
    def _register_vector(dbapi_connection, _connection_record) -> None:
        register_vector(dbapi_connection)

    def search_similar_chunks(
        self,
        query_vector: List[float],
        limit: int = 5,
    ) -> List[SearchVectorChunksRow]:
        """Search for similar chunks using the generated sqlc query layer."""
        with self.engine.begin() as conn:
            q = Querier(conn)
            q.set_local_hnsw_ef_search100()
            return list(q.search_vector_chunks(embedding=query_vector, limit=limit, offset=0))


def _sqlalchemy_database_url(database_url: str) -> str:
    if database_url.startswith("postgresql+psycopg://"):
        return database_url
    if database_url.startswith("postgresql://"):
        return "postgresql+psycopg://" + database_url[len("postgresql://"):]
    if database_url.startswith("postgres://"):
        return "postgresql+psycopg://" + database_url[len("postgres://"):]
    return database_url
