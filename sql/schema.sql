DROP TABLE IF EXISTS agent_runs CASCADE;
DROP TABLE IF EXISTS memory_suggestions CASCADE;
DROP TABLE IF EXISTS memories CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS conversations CASCADE;
DROP TABLE IF EXISTS jobs CASCADE;
DROP TABLE IF EXISTS chunks CASCADE;
DROP TABLE IF EXISTS index_builds CASCADE;
DROP TABLE IF EXISTS documents CASCADE;
DROP TABLE IF EXISTS notes CASCADE;
DROP TABLE IF EXISTS uploads CASCADE;
DROP TABLE IF EXISTS source_items CASCADE;
DROP TABLE IF EXISTS sources CASCADE;

CREATE EXTENSION IF NOT EXISTS vector;

-- for multi-item sources(directory):
-- source -> multiple source_items -> multiple documents -> multiple builds -> multiple chunks
-- for single-item sources(note, file, upload):
-- source -> document -> builds -> chunks
-- source -> note/upload

CREATE TABLE sources (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    type TEXT NOT NULL CHECK (type IN ('text', 'upload', 'file', 'directory')),
    sync_mode TEXT NOT NULL CHECK (sync_mode IN ('reconcile', 'replace', 'none')),
    locator TEXT, -- source identifier, e.g. file path, URL, etc.
    display_name TEXT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'deleted')),
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_scan_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_sources_unique_locator
    ON sources(type, locator)
    WHERE locator IS NOT NULL;

CREATE INDEX idx_sources_type_status ON sources(type, status);
CREATE INDEX idx_sources_created_at ON sources(created_at DESC);

CREATE TABLE source_items (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    source_id BIGINT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    item_key TEXT NOT NULL,
    locator TEXT NOT NULL,
    display_name TEXT,
    fingerprint TEXT,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_id, item_key)
);

CREATE INDEX idx_source_items_source_id ON source_items(source_id);
CREATE INDEX idx_source_items_is_deleted ON source_items(is_deleted);

CREATE TABLE notes (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    source_id BIGINT NOT NULL UNIQUE REFERENCES sources(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_notes_created_at ON notes(created_at DESC);

CREATE TABLE uploads (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    source_id BIGINT NOT NULL UNIQUE REFERENCES sources(id) ON DELETE CASCADE,
    original_filename TEXT NOT NULL,
    storage_path TEXT NOT NULL, -- storage as in managed by system, e.g. S3 key or local file path
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    content_hash TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_uploads_created_at ON uploads(created_at DESC);

CREATE TABLE documents (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    source_id BIGINT NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    source_item_id BIGINT REFERENCES source_items(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'deleted', 'error')),
    active_build_id BIGINT,
    indexed_at TIMESTAMPTZ,
    parser_version TEXT,
    chunker_version TEXT,
    last_error TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_documents_unique_source
    ON documents(source_id)
    WHERE source_item_id IS NULL;

CREATE UNIQUE INDEX idx_documents_unique_source_item
    ON documents(source_item_id)
    WHERE source_item_id IS NOT NULL;

CREATE INDEX idx_documents_source_id ON documents(source_id);
CREATE INDEX idx_documents_status ON documents(status);
CREATE INDEX idx_documents_created_at ON documents(created_at DESC);

CREATE TABLE index_builds (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    build_no INTEGER NOT NULL,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('building', 'active', 'superseded', 'failed', 'deleted')),
    activated_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (document_id, build_no)
);

CREATE INDEX idx_index_builds_document_id ON index_builds(document_id);
CREATE INDEX idx_index_builds_status ON index_builds(status);

ALTER TABLE documents
    ADD CONSTRAINT documents_active_build_fkey
    FOREIGN KEY (active_build_id) REFERENCES index_builds(id)
    ON DELETE SET NULL
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE chunks (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    build_id BIGINT NOT NULL REFERENCES index_builds(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    content TEXT NOT NULL,
    embedding VECTOR(1024),
    section_path TEXT[] NOT NULL DEFAULT '{}'::text[],
    semantic_type TEXT,
    token_count INTEGER,
    content_hash TEXT,
    start_offset INTEGER,
    end_offset INTEGER,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (build_id, chunk_index)
);

CREATE INDEX idx_chunks_document_id ON chunks(document_id);
CREATE INDEX idx_chunks_build_id ON chunks(build_id);
CREATE INDEX idx_chunks_embedding ON chunks USING hnsw (embedding vector_cosine_ops);

-- agent related

-- go api puts tasks in db, python processor worker picks up tasks
CREATE TABLE jobs (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    dedupe_key TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (
        status IN ('pending', 'running', 'completed', 'failed_retryable', 'failed_permanent', 'cancelled')
    ),
    attempt_count INTEGER NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_jobs_status_available_at ON jobs(status, available_at);
CREATE INDEX idx_jobs_dedupe_key ON jobs(dedupe_key);
CREATE UNIQUE INDEX idx_jobs_pending_dedupe_key
    ON jobs(dedupe_key)
    WHERE dedupe_key IS NOT NULL AND status = 'pending';
CREATE INDEX idx_jobs_created_at ON jobs(created_at DESC);

CREATE TABLE conversations (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    title TEXT,
    summary TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_conversations_created_at ON conversations(created_at DESC);

CREATE TABLE messages (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    conversation_id BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    status TEXT NOT NULL DEFAULT 'completed' CHECK (status IN ('streaming', 'completed', 'failed')),
    content TEXT NOT NULL,
    citations JSONB NOT NULL DEFAULT '[]'::jsonb,
    tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb,
    tool_results JSONB NOT NULL DEFAULT '[]'::jsonb,
    token_count INTEGER,
    latency_ms INTEGER,
    model TEXT,
    parent_message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL,
    sequence_number INTEGER NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (conversation_id, sequence_number)
);

CREATE INDEX idx_messages_conversation_id ON messages(conversation_id, sequence_number);
CREATE INDEX idx_messages_status ON messages(status);

CREATE TABLE memories (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    subject TEXT NOT NULL,
    category TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived', 'deleted')),
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    source_id BIGINT REFERENCES sources(id) ON DELETE SET NULL,
    document_id BIGINT REFERENCES documents(id) ON DELETE SET NULL,
    message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memories_status ON memories(status);
CREATE INDEX idx_memories_subject_category ON memories(subject, category);
CREATE INDEX idx_memories_created_at ON memories(created_at DESC);

CREATE TABLE memory_suggestions (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    subject TEXT NOT NULL,
    category TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected', 'expired')),
    extractor_type TEXT NOT NULL,
    source_id BIGINT REFERENCES sources(id) ON DELETE SET NULL,
    document_id BIGINT REFERENCES documents(id) ON DELETE SET NULL,
    message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL,
    evidence_text TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_memory_suggestions_status ON memory_suggestions(status);
CREATE INDEX idx_memory_suggestions_subject_category ON memory_suggestions(subject, category);
CREATE INDEX idx_memory_suggestions_created_at ON memory_suggestions(created_at DESC);

CREATE TABLE agent_runs (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    conversation_id BIGINT REFERENCES conversations(id) ON DELETE SET NULL,
    trigger_message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    trace JSONB NOT NULL DEFAULT '[]'::jsonb,
    tools_used TEXT[] NOT NULL DEFAULT '{}'::text[],
    documents_accessed BIGINT[] NOT NULL DEFAULT '{}'::bigint[],
    start_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_time TIMESTAMPTZ,
    total_tokens INTEGER NOT NULL DEFAULT 0,
    total_latency_ms INTEGER NOT NULL DEFAULT 0,
    step_count INTEGER NOT NULL DEFAULT 0,
    error_type TEXT,
    error_message TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_runs_conversation_id ON agent_runs(conversation_id);
CREATE INDEX idx_agent_runs_created_at ON agent_runs(created_at DESC);
