-- Sources
-- name: CreateSource :one
INSERT INTO sources (
    type,
    sync_mode,
    locator,
    display_name,
    status,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
) RETURNING *;

-- name: ListSources :many
SELECT *
FROM sources
WHERE (@query::text = '' OR COALESCE(display_name, '') ILIKE '%' || @query || '%' OR COALESCE(locator, '') ILIKE '%' || @query || '%')
  AND (@type::text = '' OR type = @type)
  AND (
    (@status::text <> '' AND status = @status)
    OR (
      @status::text = ''
      AND (@include_archived::bool OR status <> 'archived')
      AND (@include_deleted::bool OR status <> 'deleted')
    )
  )
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetSourceByID :one
SELECT *
FROM sources
WHERE id = $1;

-- name: UpdateSourceBasics :one
UPDATE sources
SET locator = $2,
    display_name = $3,
    metadata = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateSourceStatus :one
UPDATE sources
SET status = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetSourceLastScanAt :one
UPDATE sources
SET last_scan_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- Source Items
-- name: CreateSourceItem :one
INSERT INTO source_items (
    source_id,
    item_key,
    locator,
    display_name,
    fingerprint,
    last_seen_at,
    is_deleted,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    NOW(),
    $6,
    $7
) RETURNING *;

-- name: UpsertSourceItem :one
INSERT INTO source_items (
    source_id,
    item_key,
    locator,
    display_name,
    fingerprint,
    last_seen_at,
    is_deleted,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    NOW(),
    $6,
    $7
)
ON CONFLICT (source_id, item_key) DO UPDATE
SET locator = EXCLUDED.locator,
    display_name = EXCLUDED.display_name,
    fingerprint = EXCLUDED.fingerprint,
    last_seen_at = NOW(),
    is_deleted = EXCLUDED.is_deleted,
    metadata = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetSourceItemByID :one
SELECT *
FROM source_items
WHERE id = $1;

-- name: MarkSourceItemDeletedByID :one
UPDATE source_items
SET is_deleted = TRUE,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: ListSourceItemsBySourceID :many
SELECT *
FROM source_items
WHERE source_id = $1
ORDER BY created_at DESC;

-- name: MarkSourceItemsDeletedBySourceID :exec
UPDATE source_items
SET is_deleted = TRUE,
    updated_at = NOW()
WHERE source_id = $1;

-- Notes
-- name: CreateNote :one
INSERT INTO notes (
    source_id,
    title,
    body,
    content_hash,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
) RETURNING *;

-- name: ListNotes :many
SELECT n.*
FROM notes n
JOIN sources s ON s.id = n.source_id
WHERE (@query::text = '' OR n.title ILIKE '%' || @query || '%' OR n.body ILIKE '%' || @query || '%')
  AND (@include_archived::bool OR s.status <> 'archived')
  AND (@include_deleted::bool OR s.status <> 'deleted')
ORDER BY n.created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetNoteByID :one
SELECT *
FROM notes
WHERE id = $1;

-- name: GetNoteBySourceID :one
SELECT *
FROM notes
WHERE source_id = $1;

-- name: UpdateNote :one
UPDATE notes
SET title = $2,
    body = $3,
    content_hash = $4,
    metadata = $5,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateNoteStatus :one
UPDATE sources s
SET status = $2,
    updated_at = NOW()
FROM notes n
WHERE n.id = $1
  AND s.id = n.source_id
RETURNING n.*;

-- Uploads
-- name: CreateUpload :one
INSERT INTO uploads (
    source_id,
    original_filename,
    storage_path,
    mime_type,
    size_bytes,
    content_hash,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
) RETURNING *;

-- name: ListUploads :many
SELECT u.*
FROM uploads u
JOIN sources s ON s.id = u.source_id
WHERE (@query::text = '' OR u.original_filename ILIKE '%' || @query || '%' OR u.storage_path ILIKE '%' || @query || '%')
  AND (@status::text = '' OR s.status = @status)
ORDER BY u.created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetUploadByID :one
SELECT *
FROM uploads
WHERE id = $1;

-- name: GetUploadBySourceID :one
SELECT *
FROM uploads
WHERE source_id = $1;

-- name: UpdateUploadStoredFile :one
UPDATE uploads
SET storage_path = $2,
    mime_type = $3,
    size_bytes = $4,
    content_hash = $5,
    metadata = $6,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateUploadStatus :one
UPDATE sources s
SET status = $2,
    updated_at = NOW()
FROM uploads u
WHERE u.id = $1
  AND s.id = u.source_id
RETURNING u.*;

-- Documents
-- name: CreateDocument :one
INSERT INTO documents (
    source_id,
    source_item_id,
    title,
    mime_type,
    status,
    parser_version,
    chunker_version,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8
) RETURNING *;

-- name: ListDocuments :many
SELECT *
FROM documents
WHERE ($1::text = '' OR title ILIKE '%' || $1 || '%')
  AND ($2::text = '' OR status = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: GetDocumentByID :one
SELECT *
FROM documents
WHERE id = $1;

-- name: GetDocumentBySourceID :one
SELECT *
FROM documents
WHERE source_id = $1
  AND source_item_id IS NULL;

-- name: GetDocumentBySourceItemID :one
SELECT *
FROM documents
WHERE source_item_id = $1;

-- name: MarkDocumentsDeletedBySourceID :exec
UPDATE documents
SET status = 'deleted',
    updated_at = NOW()
WHERE source_id = $1;

-- name: MarkDocumentDeletedBySourceItemID :one
UPDATE documents
SET status = 'deleted',
    updated_at = NOW()
WHERE source_item_id = $1
RETURNING *;

-- name: ArchiveDocument :one
UPDATE documents
SET status = 'archived',
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: MarkDocumentDeleted :one
UPDATE documents
SET status = 'deleted',
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetDocumentActiveBuild :one
UPDATE documents
SET active_build_id = $2,
    indexed_at = NOW(),
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateDocumentError :one
UPDATE documents
SET last_error = $2,
    status = $3,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateDocumentBasics :one
UPDATE documents
SET title = $2,
    mime_type = $3,
    status = $4,
    parser_version = $5,
    chunker_version = $6,
    last_error = $7,
    metadata = $8,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- Index Builds
-- name: GetLatestBuildNo :one
SELECT COALESCE(MAX(build_no), 0)::int4
FROM index_builds
WHERE document_id = $1;

-- name: CreateIndexBuild :one
INSERT INTO index_builds (
    document_id,
    build_no,
    content_hash,
    status
) VALUES (
    $1,
    $2,
    $3,
    $4
) RETURNING *;

-- name: GetIndexBuildByID :one
SELECT *
FROM index_builds
WHERE id = $1;

-- name: UpdateIndexBuildStatus :one
UPDATE index_builds
SET status = $2,
    activated_at = CASE WHEN $2 = 'active' THEN NOW() ELSE activated_at END
WHERE id = $1
RETURNING *;

-- Chunks
-- name: CreateChunk :one
INSERT INTO chunks (
    document_id,
    build_id,
    chunk_index,
    content,
    embedding,
    section_path,
    semantic_type,
    token_count,
    content_hash,
    start_offset,
    end_offset,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11,
    $12
) RETURNING *;

-- name: GetChunksByBuildID :many
SELECT *
FROM chunks
WHERE build_id = $1
ORDER BY chunk_index;

-- name: DeleteChunksByBuildID :exec
DELETE FROM chunks
WHERE build_id = $1;

-- Jobs
-- name: CreateJob :one
INSERT INTO jobs (
    type,
    payload,
    dedupe_key,
    status,
    available_at
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5
)
ON CONFLICT (dedupe_key) WHERE (dedupe_key IS NOT NULL AND status = 'pending')
DO UPDATE SET
    updated_at = jobs.updated_at
RETURNING *;

-- name: ListJobs :many
SELECT *
FROM jobs
WHERE ($1::text = '' OR status = $1)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetPendingJobByDedupeKey :one
SELECT *
FROM jobs
WHERE dedupe_key = $1
  AND status = 'pending'
ORDER BY created_at DESC
LIMIT 1;

-- name: GetJobByID :one
SELECT *
FROM jobs
WHERE id = $1;

-- name: ClaimNextRunnableJob :one
WITH next_job AS (
    SELECT id
    FROM jobs
    WHERE (
        status IN ('pending', 'failed_retryable')
        AND available_at <= NOW()
    ) OR (
        status = 'running'
        AND lease_expires_at IS NOT NULL
        AND lease_expires_at <= NOW()
    )
    ORDER BY available_at ASC, created_at ASC, id ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE jobs
SET status = 'running',
    attempt_count = attempt_count + 1,
    lease_expires_at = NOW() + make_interval(secs => @duration::int),
    updated_at = NOW()
WHERE id = (SELECT id FROM next_job)
RETURNING *;

-- name: RenewJobLease :one
UPDATE jobs
SET lease_expires_at = NOW() + make_interval(secs => @duration::int),
    updated_at = NOW()
WHERE id = $1
  AND status = 'running'
RETURNING *;

-- name: CompleteJob :one
UPDATE jobs
SET status = 'completed',
    lease_expires_at = NULL,
    last_error = NULL,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: FailJobRetryable :one
UPDATE jobs
SET status = 'failed_retryable',
    lease_expires_at = NULL,
    available_at = NOW() + make_interval(secs => @duration::int),
    last_error = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: FailJobPermanent :one
UPDATE jobs
SET status = 'failed_permanent',
    lease_expires_at = NULL,
    last_error = $2,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: RetryJob :one
UPDATE jobs
SET status = 'pending',
    available_at = NOW(),
    last_error = NULL,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- Conversations
-- name: CreateConversation :one
INSERT INTO conversations (
    title,
    summary,
    metadata
) VALUES (
    $1,
    $2,
    $3
) RETURNING *;

-- name: ListConversations :many
SELECT *
FROM conversations
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetConversationByID :one
SELECT *
FROM conversations
WHERE id = $1;

-- name: UpdateConversation :one
UPDATE conversations
SET title = $2,
    summary = $3,
    metadata = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteConversation :one
DELETE FROM conversations
WHERE id = $1
RETURNING *;

-- Messages
-- name: CreateMessage :one
INSERT INTO messages (
    conversation_id,
    role,
    content,
    citations,
    tool_calls,
    tool_results,
    token_count,
    latency_ms,
    model,
    parent_message_id,
    sequence_number,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11,
    $12
) RETURNING *;

-- name: ListMessagesByConversation :many
SELECT *
FROM messages
WHERE conversation_id = $1
ORDER BY sequence_number
LIMIT $2 OFFSET $3;

-- name: GetLatestMessageSequence :one
SELECT COALESCE(MAX(sequence_number), 0)::int4
FROM messages
WHERE conversation_id = $1;

-- Agent Runs
-- name: CreateAgentRun :one
INSERT INTO agent_runs (
    conversation_id,
    trigger_message_id,
    status,
    metadata
) VALUES (
    $1,
    $2,
    $3,
    $4
) RETURNING *;

-- name: ListAgentRuns :many
SELECT *
FROM agent_runs
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: GetAgentRunByID :one
SELECT *
FROM agent_runs
WHERE id = $1;

-- name: UpdateAgentRun :one
UPDATE agent_runs
SET status = $2,
    trace = $3,
    tools_used = $4,
    documents_accessed = $5,
    end_time = $6,
    total_tokens = $7,
    total_latency_ms = $8,
    step_count = $9,
    error_type = $10,
    error_message = $11,
    metadata = $12
WHERE id = $1
RETURNING *;

-- Search
-- name: SetLocalHnswEfSearch100 :exec
SET LOCAL hnsw.ef_search = 100;

-- name: SearchLexicalChunks :many
SELECT
    c.id AS chunk_id,
    c.document_id,
    c.build_id,
    c.chunk_index,
    c.content,
    c.section_path,
    c.semantic_type,
    c.token_count,
    c.start_offset,
    c.end_offset,
    c.metadata,
    d.title AS document_title,
    s.display_name AS source_display_name,
    s.locator AS source_locator,
    si.locator AS source_item_locator,
    ts_rank_cd(to_tsvector('simple', c.content), websearch_to_tsquery('simple', $1))::float8 AS lexical_score
FROM chunks c
JOIN documents d ON d.id = c.document_id
JOIN sources s ON s.id = d.source_id
LEFT JOIN source_items si ON si.id = d.source_item_id
WHERE d.status = 'active'
  AND s.status = 'active'
  AND c.build_id = d.active_build_id
  AND to_tsvector('simple', c.content) @@ websearch_to_tsquery('simple', $1)
ORDER BY lexical_score DESC, c.id DESC
LIMIT $2 OFFSET $3;

-- name: SearchVectorChunks :many
SELECT
    c.id AS chunk_id,
    c.document_id,
    c.build_id,
    c.chunk_index,
    c.content,
    c.section_path,
    c.semantic_type,
    c.token_count,
    c.start_offset,
    c.end_offset,
    c.metadata,
    d.title AS document_title,
    s.display_name AS source_display_name,
    s.locator AS source_locator,
    si.locator AS source_item_locator,
    (1 - (c.embedding <=> $1))::float8 AS vector_score
FROM chunks c
JOIN documents d ON d.id = c.document_id
JOIN sources s ON s.id = d.source_id
LEFT JOIN source_items si ON si.id = d.source_item_id
WHERE d.status = 'active'
  AND s.status = 'active'
  AND c.build_id = d.active_build_id
  AND c.embedding IS NOT NULL
ORDER BY c.embedding <=> $1, c.id DESC
LIMIT $2 OFFSET $3;
