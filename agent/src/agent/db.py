"""Database access helpers for the agent."""
from dataclasses import dataclass
from typing import List

import sqlalchemy
from pgvector import Vector
from pgvector.psycopg import register_vector
from sqlalchemy import event
from sqlc.pydb.query import Querier

from agent.config import Config


@dataclass(frozen=True)
class SearchChunkResult:
    chunk_id: int
    document_id: int
    build_id: int
    chunk_index: int
    content: str
    section_path: List[str]
    semantic_type: str | None
    token_count: int | None
    start_offset: int | None
    end_offset: int | None
    metadata: object
    document_title: str
    source_display_name: str | None
    source_locator: str | None
    source_item_locator: str | None
    lexical_score: float = 0.0
    vector_score: float = 0.0
    combined_score: float = 0.0


class AgentDB:
    def __init__(self, cfg: Config):
        self.engine = sqlalchemy.create_engine(_sqlalchemy_database_url(cfg.database_url))
        event.listen(self.engine, "connect", self._register_vector)

    @staticmethod
    def _register_vector(dbapi_connection, _connection_record) -> None:
        register_vector(dbapi_connection)

    def search_similar_chunks(
        self,
        query: str,
        query_vector: List[float],
        limit: int = 5,
    ) -> List[SearchChunkResult]:
        """Search chunks using lexical + vector retrieval and fuse the results."""
        with self.engine.begin() as conn:
            q = Querier(conn)
            q.set_local_hnsw_ef_search100()

            lexical_rows = list(q.search_lexical_chunks(websearch_to_tsquery=query, limit=limit, offset=0))
            combined: dict[int, SearchChunkResult] = {}

            for row in lexical_rows:
                item = SearchChunkResult(
                    chunk_id=row.chunk_id,
                    document_id=row.document_id,
                    build_id=row.build_id,
                    chunk_index=row.chunk_index,
                    content=row.content,
                    section_path=row.section_path,
                    semantic_type=row.semantic_type,
                    token_count=row.token_count,
                    start_offset=row.start_offset,
                    end_offset=row.end_offset,
                    metadata=row.metadata,
                    document_title=row.document_title,
                    source_display_name=row.source_display_name,
                    source_locator=row.source_locator,
                    source_item_locator=row.source_item_locator,
                    lexical_score=row.lexical_score,
                    combined_score=row.lexical_score,
                )
                combined[item.chunk_id] = item

            if query_vector:
                vector_rows = list(q.search_vector_chunks(embedding=Vector(query_vector), limit=limit, offset=0))
                for row in vector_rows:
                    if row.chunk_id in combined:
                        existing = combined[row.chunk_id]
                        combined[row.chunk_id] = SearchChunkResult(
                            chunk_id=existing.chunk_id,
                            document_id=existing.document_id,
                            build_id=existing.build_id,
                            chunk_index=existing.chunk_index,
                            content=existing.content,
                            section_path=existing.section_path,
                            semantic_type=existing.semantic_type,
                            token_count=existing.token_count,
                            start_offset=existing.start_offset,
                            end_offset=existing.end_offset,
                            metadata=existing.metadata,
                            document_title=existing.document_title,
                            source_display_name=existing.source_display_name,
                            source_locator=existing.source_locator,
                            source_item_locator=existing.source_item_locator,
                            lexical_score=existing.lexical_score,
                            vector_score=row.vector_score,
                            combined_score=existing.lexical_score + row.vector_score,
                        )
                        continue

                    combined[row.chunk_id] = SearchChunkResult(
                        chunk_id=row.chunk_id,
                        document_id=row.document_id,
                        build_id=row.build_id,
                        chunk_index=row.chunk_index,
                        content=row.content,
                        section_path=row.section_path,
                        semantic_type=row.semantic_type,
                        token_count=row.token_count,
                        start_offset=row.start_offset,
                        end_offset=row.end_offset,
                        metadata=row.metadata,
                        document_title=row.document_title,
                        source_display_name=row.source_display_name,
                        source_locator=row.source_locator,
                        source_item_locator=row.source_item_locator,
                        vector_score=row.vector_score,
                        combined_score=row.vector_score,
                    )

            results = list(combined.values())
            if not query_vector:
                results.sort(key=lambda item: (item.lexical_score, item.chunk_id), reverse=True)
                return results

            fused = [item for item in results if item.combined_score > 0]
            fused.sort(key=lambda item: (item.combined_score, item.chunk_id), reverse=True)
            if fused:
                return fused

            results.sort(key=lambda item: (item.vector_score, item.chunk_id), reverse=True)
            return results


def _sqlalchemy_database_url(database_url: str) -> str:
    if database_url.startswith("postgresql+psycopg://"):
        return database_url
    if database_url.startswith("postgresql://"):
        return "postgresql+psycopg://" + database_url[len("postgresql://"):]
    if database_url.startswith("postgres://"):
        return "postgresql+psycopg://" + database_url[len("postgres://"):]
    return database_url
