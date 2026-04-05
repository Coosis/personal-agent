-- Drop existing tables for fresh reset
DROP TABLE IF EXISTS file_events CASCADE;
DROP TABLE IF EXISTS watch_directories CASCADE;
DROP TABLE IF EXISTS config CASCADE;
DROP TABLE IF EXISTS agent_runs CASCADE;
DROP TABLE IF EXISTS messages CASCADE;
DROP TABLE IF EXISTS conversations CASCADE;
DROP TABLE IF EXISTS chunks CASCADE;
DROP TABLE IF EXISTS documents CASCADE;

-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Documents table stores metadata for ingested files
CREATE TABLE documents (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    path TEXT UNIQUE NOT NULL,
    filename TEXT NOT NULL,
    extension TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    checksum TEXT NOT NULL,
    content_hash TEXT,
    last_modified TIMESTAMPTZ NOT NULL,
    indexed_at TIMESTAMPTZ DEFAULT NOW(),
    metadata JSONB DEFAULT '{}',
    processing_status TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    -- Lease mechanism for distributed processing
    lease_expires_at TIMESTAMPTZ,
    leased_by TEXT,
    retry_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_documents_path ON documents(path);
CREATE INDEX idx_documents_status ON documents(processing_status);
CREATE INDEX idx_documents_checksum ON documents(checksum);
CREATE INDEX idx_documents_modified ON documents(last_modified);

-- Chunks table stores semantic chunks with vector embeddings
CREATE TABLE chunks (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    document_id BIGINT NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    content TEXT NOT NULL,
    content_vector VECTOR(1024),
    token_count INTEGER,
    char_count INTEGER,
    start_offset INTEGER,
    end_offset INTEGER,
    heading_path TEXT[],
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    
    UNIQUE(document_id, chunk_index)
);

CREATE INDEX idx_chunks_document ON chunks(document_id);
CREATE INDEX idx_chunks_vector ON chunks USING ivfflat (content_vector vector_cosine_ops);

-- Conversations table for chat sessions
CREATE TABLE conversations (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    title TEXT,
    summary TEXT,
    source_document_ids BIGINT[],
    message_count INTEGER DEFAULT 0,
    token_usage_total INTEGER DEFAULT 0,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_conversations_updated ON conversations(updated_at DESC);

-- Messages table for conversation history
CREATE TABLE messages (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    conversation_id BIGINT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    content_blocks JSONB,
    tool_calls JSONB,
    tool_results JSONB,
    token_count INTEGER,
    latency_ms INTEGER,
    model TEXT,
    parent_message_id BIGINT,
    sequence_number INTEGER NOT NULL,
    feedback_rating INTEGER,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_messages_conversation ON messages(conversation_id, sequence_number);

-- Agent runs table for tracking agent execution
CREATE TABLE agent_runs (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    conversation_id BIGINT REFERENCES conversations(id) ON DELETE SET NULL,
    trigger_message_id BIGINT REFERENCES messages(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'running',
    
    -- ReAct trajectory stored as JSON
    trajectory JSONB DEFAULT '[]',
    
    -- Tool executions summary
    tools_used TEXT[],
    documents_accessed BIGINT[],
    
    -- Performance metrics
    start_time TIMESTAMPTZ DEFAULT NOW(),
    end_time TIMESTAMPTZ,
    total_tokens INTEGER DEFAULT 0,
    total_latency_ms INTEGER DEFAULT 0,
    step_count INTEGER DEFAULT 0,
    
    -- Error tracking
    error_type TEXT,
    error_message TEXT,
    
    -- Observability
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_agent_runs_conversation ON agent_runs(conversation_id);
CREATE INDEX idx_agent_runs_status ON agent_runs(status);
CREATE INDEX idx_agent_runs_created ON agent_runs(created_at DESC);

-- Configuration table for runtime settings
CREATE TABLE config (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    description TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    updated_by TEXT
);

-- Watch directories configuration
CREATE TABLE watch_directories (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    path TEXT UNIQUE NOT NULL,
    pattern TEXT DEFAULT '*',
    recursive BOOLEAN DEFAULT true,
    priority INTEGER DEFAULT 0,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- File events log for tracking changes
-- Lightweight queue - checksum stored in documents table (computed by worker)
CREATE TABLE file_events (
    id BIGINT PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    path TEXT NOT NULL,
    event_type TEXT NOT NULL,
    document_id BIGINT REFERENCES documents(id) ON DELETE SET NULL,
    size_bytes BIGINT,
    processed BOOLEAN DEFAULT false,
    error TEXT,
    metadata JSONB DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_file_events_path ON file_events(path);
CREATE INDEX idx_file_events_created ON file_events(created_at DESC);
CREATE INDEX idx_file_events_processed ON file_events(processed);
