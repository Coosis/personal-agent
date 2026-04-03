-- Documents queries
-- name: GetDocumentByID :one
SELECT * FROM documents WHERE id = $1;

-- name: GetDocumentByPath :one
SELECT * FROM documents WHERE path = $1;

-- name: GetDocumentByPathForUpdate :one
SELECT * FROM documents WHERE path = $1 FOR UPDATE;

-- name: UpsertDocument :one
INSERT INTO documents (
    path, filename, extension, mime_type, size_bytes, checksum, content_hash,
    last_modified, metadata, processing_status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
ON CONFLICT (path) DO UPDATE SET
    size_bytes = EXCLUDED.size_bytes,
    checksum = EXCLUDED.checksum,
    last_modified = EXCLUDED.last_modified,
    processing_status = EXCLUDED.processing_status,
    error_message = NULL,
    updated_at = NOW()
RETURNING *;

-- name: GetDocumentByChecksum :one
SELECT * FROM documents WHERE checksum = $1;

-- name: ListDocuments :many
SELECT * FROM documents ORDER BY updated_at DESC LIMIT $1 OFFSET $2;

-- name: CreateDocument :one
INSERT INTO documents (
    path, filename, extension, mime_type, size_bytes, checksum, content_hash,
    last_modified, metadata, processing_status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: UpdateDocument :one
UPDATE documents SET
    filename = $2,
    extension = $3,
    mime_type = $4,
    size_bytes = $5,
    checksum = $6,
    content_hash = $7,
    last_modified = $8,
    metadata = $9,
    processing_status = $10,
    error_message = $11,
    updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: UpdateDocumentStatus :one
UPDATE documents SET
    processing_status = $2,
    error_message = $3,
    updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: DeleteDocument :one
DELETE FROM documents WHERE id = $1 RETURNING *;

-- name: GetDocumentsByStatus :many
SELECT * FROM documents WHERE processing_status = $1 ORDER BY updated_at DESC;

-- Chunks queries
-- name: CreateChunk :one
INSERT INTO chunks (
    document_id, chunk_index, content, content_vector, token_count,
    char_count, start_offset, end_offset, heading_path, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: GetChunksByDocument :many
SELECT * FROM chunks WHERE document_id = $1 ORDER BY chunk_index;

-- name: DeleteChunksByDocument :exec
DELETE FROM chunks WHERE document_id = $1;

-- name: SearchSimilarChunks :many
SELECT 
    c.*,
    d.path as document_path,
    d.filename,
    d.metadata as document_metadata,
    1 - (c.content_vector <=> $1) as similarity
FROM chunks c
JOIN documents d ON c.document_id = d.id
WHERE c.content_vector IS NOT NULL
ORDER BY c.content_vector <=> $1
LIMIT $2;

-- name: SearchSimilarChunksByDocument :many
SELECT 
    c.*,
    1 - (c.content_vector <=> $1) as similarity
FROM chunks c
WHERE c.content_vector IS NOT NULL AND c.document_id = $2
ORDER BY c.content_vector <=> $1
LIMIT $3;

-- Conversations queries
-- name: CreateConversation :one
INSERT INTO conversations (title, summary, metadata) 
VALUES ($1, $2, $3) RETURNING *;

-- name: GetConversationByID :one
SELECT * FROM conversations WHERE id = $1;

-- name: ListConversations :many
SELECT * FROM conversations ORDER BY updated_at DESC LIMIT $1 OFFSET $2;

-- name: UpdateConversation :one
UPDATE conversations SET
    title = $2,
    summary = $3,
    message_count = $4,
    token_usage_total = $5,
    metadata = $6,
    updated_at = NOW()
WHERE id = $1 RETURNING *;

-- name: DeleteConversation :one
DELETE FROM conversations WHERE id = $1 RETURNING *;

-- Messages queries
-- name: CreateMessage :one
INSERT INTO messages (
    conversation_id, role, content, content_blocks, tool_calls,
    tool_results, token_count, latency_ms, model, parent_message_id,
    sequence_number, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
) RETURNING *;

-- name: GetMessagesByConversation :many
SELECT * FROM messages 
WHERE conversation_id = $1 
ORDER BY sequence_number 
LIMIT $2 OFFSET $3;

-- name: GetMessageByID :one
SELECT * FROM messages WHERE id = $1;

-- name: GetLatestMessageSequence :one
SELECT COALESCE(MAX(sequence_number), 0) FROM messages WHERE conversation_id = $1;

-- Agent runs queries
-- name: CreateAgentRun :one
INSERT INTO agent_runs (
    conversation_id, trigger_message_id, status, metadata
) VALUES ($1, $2, $3, $4) RETURNING *;

-- name: GetAgentRunByID :one
SELECT * FROM agent_runs WHERE id = $1;

-- name: UpdateAgentRun :one
UPDATE agent_runs SET
    status = $2,
    trajectory = $3,
    tools_used = $4,
    documents_accessed = $5,
    end_time = $6,
    total_tokens = $7,
    total_latency_ms = $8,
    step_count = $9,
    error_type = $10,
    error_message = $11,
    metadata = $12
WHERE id = $1 RETURNING *;

-- name: ListAgentRuns :many
SELECT * FROM agent_runs ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- Watch directories queries
-- name: CreateWatchDirectory :one
INSERT INTO watch_directories (path, pattern, recursive, enabled, priority, metadata)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING *;

-- name: GetWatchDirectoryByID :one
SELECT * FROM watch_directories WHERE id = $1;

-- name: GetWatchDirectoryByPath :one
SELECT * FROM watch_directories WHERE path = $1;

-- name: ListWatchDirectories :many
SELECT * FROM watch_directories WHERE enabled = true ORDER BY priority DESC;

-- name: UpdateWatchDirectory :one
UPDATE watch_directories SET
    pattern = $2,
    recursive = $3,
    enabled = $4,
    priority = $5,
    metadata = $6
WHERE id = $1 RETURNING *;

-- name: DeleteWatchDirectory :one
DELETE FROM watch_directories WHERE id = $1 RETURNING *;

-- File events queries
-- name: CreateFileEvent :one
INSERT INTO file_events (path, event_type, document_id, size_bytes, metadata)
VALUES ($1, $2, $3, $4, $5) RETURNING *;

-- name: GetFileEventsByPath :many
SELECT * FROM file_events WHERE path = $1 ORDER BY created_at DESC LIMIT $2;

-- name: GetUnprocessedFileEvents :many
SELECT * FROM file_events WHERE processed = false ORDER BY created_at LIMIT $1;

-- name: GetUnprocessedFileEventsForUpdate :many
SELECT * FROM file_events 
WHERE processed = false 
ORDER BY created_at 
LIMIT $1 
FOR UPDATE SKIP LOCKED;

-- name: MarkFileEventProcessed :one
UPDATE file_events SET processed = true, error = $2 WHERE id = $1 RETURNING *;

-- name: LinkFileEventToDocument :one
UPDATE file_events SET 
    document_id = $2, 
    processed = true, 
    error = $3 
WHERE id = $1 
RETURNING *;

-- name: GetUnprocessedFileEventsWithDocuments :many
SELECT 
    fe.*,
    d.id as doc_id,
    d.processing_status as doc_status
FROM file_events fe
LEFT JOIN documents d ON fe.path = d.path
WHERE fe.processed = false
ORDER BY fe.created_at 
LIMIT $1;

-- Configuration queries
-- name: GetConfig :one
SELECT * FROM config WHERE key = $1;

-- name: SetConfig :one
INSERT INTO config (key, value, description, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = NOW(),
    updated_by = EXCLUDED.updated_by
RETURNING *;

-- name: ListConfig :many
SELECT * FROM config ORDER BY key;
