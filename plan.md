# Personal Knowledge Base Agent - Project Plan

## 1. Project Overview

### 1.1 Vision

A local-first personal knowledge base agent that ingests heterogeneous content, builds a searchable and traceable knowledge index, and supports citation-grounded question answering and task-oriented knowledge workflows.

This system is not a file synchronization engine. Its primary goal is to make personal knowledge **importable, searchable, explainable, and maintainable**.

### 1.2 Design Principles

- **Local-first**: user-controlled sources and local indexing pipeline
- **Explicit ingestion**: sources are added intentionally, not inferred from fragile OS file events
- **Idempotent indexing**: rescanning and reindexing should be safe and repeatable
- **Source-aware lifecycle**: different source types have different update/delete semantics
- **Grounded retrieval**: answers must be backed by actual indexed content
- **Observability**: background jobs and agent decisions are inspectable

### 1.3 Core Capabilities

- **Knowledge ingestion**: import text, files, and directories
- **Incremental indexing**: scan sources and reindex only changed content
- **Structured retrieval**: hybrid lexical + vector retrieval with citations
- **Agentic workflows**: answer, summarize, compare, extract, and synthesize from indexed knowledge
- **Knowledge lifecycle management**: update, archive, delete, and reindex managed knowledge entities

---

## 2. Scope

### 2.1 In Scope

- Local files and directories as managed sources
- Pasted text as first-class note sources
- Uploaded files as first-class upload sources stored as snapshots
- Background indexing jobs with retry and lease-based execution
- Structure-aware chunking
- Semantic search and citation-grounded answering
- Agent traces and indexing/job observability

### 2.2 Out of Scope

- Cross-platform real-time file watching
- Complex filesystem event reconciliation
- URL ingestion in v1
- Multi-user auth
- Cloud sync across devices
- Rich collaborative editing
- Fine-grained deletion of arbitrary text fragments directly at chunk level

### 2.3 Non-Goals

This project does **not** aim to provide:

- Dropbox-like live synchronization
- Perfect mirroring of external file system event history
- OS-specific watcher correctness guarantees

The system only guarantees correctness at **scan/reindex time**, not real-time filesystem tracking.

---

## 3. Product Model

### 3.1 Core Entities

#### Source

A user-declared knowledge entry point.

- a local file path
- a local directory path
- an uploaded file
- a pasted note

#### Document

A normalized content object derived from a source or source item.

Examples:

- one Markdown file
- one uploaded PDF
- one pasted note stored as a document

For note, upload, and file sources, one source maps to exactly one document. A directory source maps to many documents through `source_items`.

#### Chunk

A retrieval unit derived from a document.

#### Job

A background task responsible for scanning, indexing, deleting, or reindexing knowledge.

#### Conversation / Agent Run

User interactions and their underlying retrieval/tool traces.

### 3.2 Source Categories

The system distinguishes between two source classes:

#### A. Reconcilable Sources

Sources whose current external state can be re-read later.

- local file
- local directory

Properties:

- support scan/reconcile
- support changed/unchanged/deleted detection
- support incremental reindex

#### B. Snapshot Sources

Sources whose content is submitted and then managed by the system as an internal snapshot.

- note source
- upload source

Properties:

- no external reconciliation
- each snapshot source owns exactly one document
- notes are mutable and reindex by updating that document's active chunk build
- uploads are immutable snapshots and reindex from stored raw content
- delete means delete managed content
- reindex reads from internally stored raw content

### 3.3 Knowledge Entity Lifecycle

Knowledge is managed as first-class entities, not anonymous text blobs.

#### Managed Entities

- **Note**: pasted or directly entered text
- **Upload**: stored uploaded file snapshot
- **Document**: parsed content from file/upload/source item
- **Source**: file, directory, note, or upload entry point

owner ship model:
```
source(text)
  ├─ note payload
  └─ document
       └─ builds
            └─ chunks

source(upload)
  ├─ upload payload
  └─ document
       └─ builds
            └─ chunks

source(file)
  └─ document
       └─ builds
            └─ chunks

source(directory)
  ├─ source_item(a.md) -> document -> builds -> chunks
  ├─ source_item(b.pdf) -> document -> builds -> chunks
  └─ source_item(c.txt) -> document -> builds -> chunks
```

Users delete or update entities such as sources, notes, uploads, and documents, not arbitrary chunk substrings.

---

## 4. Architecture

### 4.1 Component Overview

```text
┌──────────────┐
│   Web / CLI  │
└──────┬───────┘
       ▼
┌──────────────┐
│    Go API    │
│   (Gin)      │
└──────┬───────┘
       │
       ├──────────────► PostgreSQL
       │               - sources
       │               - uploads
       │               - source_items
       │               - notes
       │               - documents
       │               - index_builds
       │               - chunks
       │               - jobs
       │               - conversations
       │               - messages
       │               - agent_runs
       │
       ├──────────────► Python Indexer
       │               - parsing
       │               - chunking
       │               - embedding
       │               - reindex pipeline
       │
       └──────────────► Python Agent
                       - retrieval
                       - tool use
                       - grounded response generation
```

### 4.2 High-Level Flow

#### Ingestion Flow

1. User adds a source or creates a note.
2. API stores source metadata or managed content.
3. API enqueues scan/index job.
4. Worker claims job with lease.
5. Indexer parses and chunks content.
6. Embeddings are generated.
7. Chunks and document metadata are stored.
8. Job is marked completed.
9. Content becomes searchable by retrieval and agent.

#### Retrieval Flow

1. User asks a question.
2. Agent issues retrieval request.
3. Retrieval layer performs lexical + vector search.
4. Results are merged through score fusion.
5. Context is assembled with citations.
6. Agent answers with explicit grounding.

---

## 5. Source Model

### 5.1 Supported Source Types

| Source Type | Example | Sync Mode | Notes |
| --- | --- | --- | --- |
| `text` | pasted note | `replace` | note source with one owned document |
| `upload` | uploaded PDF | `none` | immutable stored snapshot |
| `file` | `/Users/me/notes/a.md` | `reconcile` | single file scan |
| `directory` | `/Users/me/notes` | `reconcile` | recursive scan |

### 5.2 Sync Modes

| Sync Mode | Meaning |
| --- | --- |
| `reconcile` | system can rescan external state and compare with indexed state |
| `replace` | updates replace managed source content and trigger reindex of the owned document |
| `none` | no synchronization semantics beyond stored snapshot |

Examples:

- `directory -> reconcile`
- `file -> reconcile`
- `text -> replace`
- `upload -> none`

### 5.3 Why No File Watcher

The project intentionally avoids relying on filesystem event streams as a source of truth.

Reasons:

- event semantics differ across operating systems
- recursive watch introduces significant complexity
- restart recovery requires reconciliation anyway
- editor save behavior often generates noisy or misleading events
- watcher correctness does not improve retrieval quality, indexing quality, or agent usefulness

Instead, the system uses explicit scans and reconciliation jobs.

---

## 6. Ingestion and Reconciliation

### 6.1 Explicit Ingestion

Sources enter the system through explicit APIs:

- create note
- upload file
- add file source
- add directory source

This keeps ingestion deterministic and explainable.

### 6.2 Scan-Based Reconciliation

For reconcilable sources, the system computes current source state and compares it against previously indexed state.

Reconciliation answers:

- which items are new
- which items changed
- which items are unchanged
- which items were deleted

It does not answer:

- which exact low-level filesystem events occurred
- whether a change was rename vs replace vs atomic-save temp swap

Only current state matters.

### 6.3 Directory Scan Semantics

A directory source is scanned recursively through an explicit user-triggered scan or a scheduled scan job.

Scan steps:

1. Walk directory tree recursively.
2. Filter supported file types.
3. Normalize paths.
4. Build current item snapshot.
5. Compare with `source_items`.
6. Mark new, changed, unchanged, deleted.
7. Enqueue index jobs for new/changed items.
8. Mark missing items as deleted or archived.

Recursive scan is allowed. Recursive file watching is not required.

### 6.4 Change Detection Strategy

The system uses staged change detection.

#### Fast Identity

Used during scan:

- path / item key
- size
- `mtime`
- optional cheap fingerprint

#### Strong Identity

Used before full reindex:

- content hash, for example `SHA256`

Benefits:

- avoids full hashing every file during every scan
- keeps scan fast
- prevents unnecessary reindex work

---

## 7. Managed Content Model

### 7.1 Notes

Pasted text is modeled as a first-class note source.

Note semantics:

- create note source with title + body
- create one document owned by that note source
- edit note content
- reindex note by building new chunks and updating the owned document's `active_build_id`
- archive note
- delete note

Important rule:

If a user wants to "remove part of the knowledge" from a note, they edit the note body and the system reindexes the note's document. The system does not delete arbitrary chunk fragments directly.

### 7.2 Uploaded Files

Uploaded files are modeled as upload sources stored as internal snapshots.

Upload semantics:

- upload file
- create one document owned by that upload source
- reindex from stored blob by building a new chunk build for that document
- archive/delete upload
- replacement creates a new upload source; the old upload remains until the user explicitly archives or deletes it

Uploads do not depend on original external file paths after import.
Stored blobs live under a managed storage root, for example `storage/uploads/{upload_id}/original`.

### 7.3 External File/Directory Sources

These are re-readable external sources.

File semantics:

- create file source with one owned document
- scan file
- reindex if changed by building a new chunk build for that document
- archive document
- detach source
- purge indexed content

Directory semantics:

- scan directory recursively
- reconcile source items
- reindex changed items
- mark deleted items as deleted/archived

---

## 8. Background Jobs

### 8.1 Job Types

Current queued values in the repo:

- `scan_source`
- `reindex_document`
- `purge_source_content`

Reserved/planned lifecycle and maintenance values:

- `archive_document`
- `mark_document_deleted`
- `gc_inactive_chunks`
- `retry_failed_job`

### 8.2 Job Execution Model

Workers poll jobs from PostgreSQL using lease-based claiming.

Core properties:

- `FOR UPDATE SKIP LOCKED`
- lease expiration for crash recovery
- retry count and backoff
- idempotent execution
- dedupe key to avoid duplicate work
- partial unique index on `jobs.dedupe_key` for `status = pending`
- duplicate enqueue with the same pending dedupe key should return the existing pending job rather than fail

Recommended dedupe semantics:

- final-effect jobs should dedupe by effect target, for example `reindex_document:{document_id}`
- trigger-specific context such as `note_update` or `upload_create` belongs in job payload metadata, not in the dedupe key

### 8.3 Why Jobs Matter

The project's reliability comes from job-based indexing, not event watchers.

Jobs provide:

- recovery after crash
- inspectable progress
- retry semantics
- backpressure control
- explicit failure states

### 8.4 Job Lifecycle

```text
pending -> running -> completed
                │
                ├-> failed_retryable -> pending
                └-> failed_permanent
```

---

## 9. Indexing Pipeline

### 9.1 Pipeline Overview

```text
Raw Content
  -> Parse
  -> Normalize
  -> Chunk
  -> Embed
  -> Store Document + Chunks
  -> Activate Indexed Version
```

### 9.2 Parsing

Parsing depends on content type.

| Format | Strategy |
| --- | --- |
| Markdown | heading-aware parsing |
| Plain text | paragraph/sentence parsing |
| PDF | text extraction with page awareness |
| DOCX | structural extraction |
| Code | symbol-aware chunking (v2) |

### 9.3 Normalized Document Representation

All content is transformed into a common internal model.

Each document may contain:

- title
- source reference
- section tree
- blocks
- extracted plain text
- metadata

This makes retrieval and citation logic format-agnostic.

### 9.4 Chunking Strategy

The system uses structure-aware chunking rather than naive fixed slicing.

Goals:

- preserve semantic boundaries
- preserve section context
- improve retrieval quality
- improve citation quality

Examples:

- `Markdown ->` header hierarchy
- `PDF ->` page/section-aware
- `Plain text ->` paragraph + sentence boundary fallback

Chunk metadata:

- `document_id`
- `chunk_index`
- `section_path`
- `semantic_type`
- `token_count`
- `content_hash`
- `start_offset`, `end_offset`

### 9.5 Embeddings

Embeddings are generated only for searchable chunk content.

Stored data includes:

- embedding vector
- chunk text
- metadata for citation and filtering

---

## 10. Index Build Switching and Deletion

### 10.1 Why Build Switching Exists

Reindexing should not corrupt active retrieval state.

A new chunk build should be prepared before old indexed content is deactivated.

### 10.2 Recommended Lifecycle

On update/reindex:

1. Parse/chunk/embed the new content outside the active retrieval path.
2. Create a new index build record for the document.
3. Insert all chunks for that build.
4. In a single transaction, switch `documents.active_build_id` to the new build.
5. Mark the old build superseded.

This prevents search downtime and preserves recoverability.

### 10.3 Deletion Semantics

Deletion operates on concrete managed entities.

Supported delete actions:

- delete note
- archive document
- mark document deleted
- detach external source
- purge source content

Search behavior:

Deleted or inactive content must not appear in search results.

Recommended implementation:

Soft delete / deactivate first:

- `source.status = deleted`
- `document.status = deleted`
- `index_build.status = superseded` or `deleted`
- retrieval only reads chunks whose `build_id = documents.active_build_id`

A later GC job may physically delete inactive rows/blobs.

### 10.4 Archive vs Delete

Archive is different from delete.

Archive:

- entity remains stored
- entity is excluded from default search
- entity can be restored

Delete:

- entity is removed from active knowledge
- entity may be physically purged later

### 10.5 Storage Growth Control

Knowledge will grow over time, so the system must distinguish:

#### Normal Growth

New notes, new uploads, new file sources, and newly discovered files under directory sources.

#### Technical Bloat

Old index builds, stale chunks, and orphaned blobs.

To control technical bloat:

- update uses replace, not append
- old builds become inactive
- periodic GC removes inactive data
- a build retention policy can be added later

---

## 11. Retrieval System

### 11.1 Retrieval Goals

- relevant results
- traceable evidence
- source-aware filtering
- support for grounded generation

### 11.2 Retrieval Strategy

Recommended hybrid approach:

1. lexical retrieval
2. vector retrieval
3. score fusion
4. context assembly
5. answer generation with citations

V1 does not include reranking.

### 11.3 Search Modes

#### Knowledge Search

Used by the agent and search API:

- semantic and lexical matching
- ranked chunk results
- retrieval only considers chunks whose `build_id` matches the document's `active_build_id`

#### Management Search

Used by management UI/API:

- title search
- path search
- snippet preview search
- filters by type/status

These concerns should be handled through filtered source and document list endpoints rather than a generic catch-all API.

### 11.4 Citation Requirements

Every grounded answer should reference indexed evidence.

Citations should include:

- document title
- source path, directory-relative item path, or managed entity name
- section path if available
- snippet
- chunk identifier or offsets

---

## 12. Agent Layer

### 12.1 Agent Responsibilities

The agent is not only a chatbot. It is a knowledge workflow executor.

Primary tasks:

- answer questions from indexed content
- summarize selected sources/documents
- compare two or more documents
- extract action items and deadlines
- synthesize topic briefs from multiple documents

### 12.2 Agent Tools

- Current v1 tools in the repo:
  - `get_personal_information`
  - `search_knowledge_base`
- Reserved/planned tools:
  - `get_document`
  - `list_documents`
  - `list_sources`
  - `summarize_document`
  - `compare_documents`
  - `extract_actions`

### 12.3 Grounding Rules

The agent should:

- prefer retrieved evidence over prior assumptions
- show uncertainty when evidence is weak
- cite sources used in the answer
- expose tool calls/results via agent run traces

### 12.4 Current Graph Shape

Current implemented graph direction:

1. `analyze_request`
2. `plan_response`
3. optional tool loop
4. `normalize_tool_result`
5. `generate_response`
6. `commit_agent_response`

Current state split:

- `conversation_messages`: user-visible prior turns
- `messages`: internal tool-loop scratchpad used by LangGraph tool routing
- `input_analysis`
- `plan_decision`
- `retrieved_context`
- `final_answer`

### 12.5 Important Remaining Gaps

- Go currently sends only the current user message to the Python agent; prior conversation history is stored in the database but not yet replayed into the agent state
- `retrieved_context` normalization is still simple and should evolve into explicit evidence blocks with citations
- personal information is still stubbed in code and should be moved to database-backed retrieval
- compare/summarize/extract workflows are still planned, not implemented
- LangSmith tracing is useful for debugging, but the project should not depend on trace-only metadata for runtime behavior

---

## 13. API Design

### 13.1 Notes

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/v1/notes` | Create a note source |
| `GET` | `/api/v1/notes` | List note sources |
| `GET` | `/api/v1/notes/{id}` | Get note |
| `PUT` | `/api/v1/notes/{id}` | Update note |
| `DELETE` | `/api/v1/notes/{id}` | Delete note |
| `POST` | `/api/v1/notes/{id}/reindex` | Reindex note |
| `POST` | `/api/v1/notes/{id}/archive` | Archive note |

### 13.2 Uploads

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/v1/uploads` | Create an upload source |
| `GET` | `/api/v1/uploads` | List upload sources |
| `GET` | `/api/v1/uploads/{id}` | Get upload |
| `DELETE` | `/api/v1/uploads/{id}` | Delete upload |
| `POST` | `/api/v1/uploads/{id}/reindex` | Reindex upload |
| `POST` | `/api/v1/uploads/{id}/archive` | Archive upload |

### 13.3 Sources

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/v1/sources` | Add file or directory source |
| `GET` | `/api/v1/sources` | List or search sources via query params such as `?query=` and `?type=` |
| `GET` | `/api/v1/sources/{id}` | Get source |
| `DELETE` | `/api/v1/sources/{id}` | Delete/detach source |
| `POST` | `/api/v1/sources/{id}/scan` | Trigger scan |
| `POST` | `/api/v1/sources/{id}/purge` | Purge indexed content |

### 13.4 Documents

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/v1/documents` | List or search documents via query params such as `?query=` and `?status=` |
| `GET` | `/api/v1/documents/{id}` | Get document |
| `POST` | `/api/v1/documents/{id}/reindex` | Reindex document |
| `POST` | `/api/v1/documents/{id}/archive` | Archive document |
| `POST` | `/api/v1/documents/{id}/mark-deleted` | Mark document deleted |

### 13.5 Search

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/v1/search` | Hybrid knowledge search |
| `POST` | `/api/v1/search/debug` | Search with score breakdown |

### 13.6 Conversations

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/v1/conversations` | Create conversation |
| `GET` | `/api/v1/conversations` | List conversations |
| `GET` | `/api/v1/conversations/{id}` | Get conversation |
| `POST` | `/api/v1/conversations/{id}/messages` | Send message |
| `POST` | `/api/v1/conversations/{id}/messages/stream` | Stream assistant reply while keeping the message row in `streaming` state until finalized |
| `GET` | `/api/v1/conversations/{id}/messages` | List messages |

### 13.7 Jobs and Observability

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/v1/jobs` | List jobs |
| `GET` | `/api/v1/jobs/{id}` | Get job detail |
| `POST` | `/api/v1/jobs/{id}/retry` | Retry failed job |
| `GET` | `/api/v1/agent-runs` | List agent runs |
| `GET` | `/api/v1/agent-runs/{id}` | Get agent trace |

---

## 14. Database Schema (Conceptual)

### 14.1 Main Tables

- `sources`
- `uploads`
- `source_items`
- `notes`
- `documents`
- `index_builds`
- `chunks`
- `jobs`
- `conversations`
- `messages`
- `agent_runs`

### 14.2 Table Responsibilities

#### `sources`

Stores source definitions and lifecycle.

Fields:

- `id`
- `type`
- `sync_mode`
- `locator`
- `display_name`
- `status`
- `last_scan_at`
- `created_at`
- `updated_at`

Note and upload entries are also represented here.

#### `uploads`

Stores upload-source specific metadata and blob location.

Fields:

- `id`
- `source_id`
- `original_filename`
- `storage_path`
- `mime_type`
- `size_bytes`
- `content_hash`
- `metadata`
- `created_at`
- `updated_at`

#### `source_items`

Stores discovered items under reconcilable multi-item sources.

Fields:

- `id`
- `source_id`
- `item_key`
- `locator`
- `display_name`
- `fingerprint`
- `last_seen_at`
- `is_deleted`
- `metadata`
- `created_at`
- `updated_at`

#### `notes`

Stores note-source specific content.

Fields:

- `id`
- `source_id`
- `title`
- `body`
- `content_hash`
- `metadata`
- `created_at`
- `updated_at`

#### `documents`

Stores normalized document identities.

Fields:

- `id`
- `source_id`
- `source_item_id`
- `title`
- `mime_type`
- `status`
- `active_build_id`
- `indexed_at`
- `parser_version`
- `chunker_version`
- `last_error`
- `metadata`
- `created_at`
- `updated_at`

#### `index_builds`

Stores chunk build versions for safe reindex replacement.

Fields:

- `id`
- `document_id`
- `build_no`
- `content_hash`
- `status` (`building`, `active`, `superseded`, `failed`, `deleted`)
- `activated_at`
- `created_at`

#### `chunks`

Stores retrieval units.

Fields:

- `id`
- `document_id`
- `build_id`
- `chunk_index`
- `content`
- `embedding`
- `section_path`
- `semantic_type`
- `token_count`
- `content_hash`
- `start_offset`
- `end_offset`
- `metadata`

The retrieval query must join `documents` and only return chunks whose `chunks.build_id = documents.active_build_id`.

#### `jobs`

Stores background work.

Fields:

- `id`
- `type`
- `payload`
- `dedupe_key`
- `status`
- `attempt_count`
- `lease_expires_at`
- `available_at`
- `last_error`
- `created_at`
- `updated_at`

Important behavior:

- `dedupe_key` is unique only among pending jobs
- repeated enqueue of the same pending logical effect should return the existing pending job

#### `messages`

Stores conversation history and assistant streaming lifecycle.

Fields:

- `id`
- `conversation_id`
- `role`
- `status` (`streaming`, `completed`, `failed`)
- `content`
- `citations`
- `tool_calls`
- `tool_results`
- `token_count`
- `latency_ms`
- `model`
- `parent_message_id`
- `sequence_number`
- `metadata`
- `created_at`
- `updated_at`

Assistant reply lifecycle:

- insert the assistant row before calling the agent with `status = 'streaming'` and empty content
- stream token events to the caller while accumulating the final reply in Go
- for the streaming endpoint, emit explicit downstream lifecycle signals: `start`, `stop`, and `failed`
- on success, update the same row to final content and `status = 'completed'`
- on failure, update the same row to the accumulated partial content and `status = 'failed'`

This keeps the database as the source of truth even when the HTTP client is observing streamed output.

---

## 15. Directory Structure

```text
personal-agent/
├── cmd/
│   ├── apictl/
│   │   ├── cli.go
│   │   ├── conversations.go
│   │   ├── documents.go
│   │   ├── health.go
│   │   ├── http.go
│   │   ├── jobs.go
│   │   ├── main.go
│   │   ├── notes.go
│   │   ├── search.go
│   │   ├── sources.go
│   │   └── uploads.go
│   ├── chat/
│   │   └── main.go
│   └── server/
│       └── main.go
├── internal/
│   ├── agenthttp/
│   │   ├── client.go
│   │   └── sse.go
│   ├── conversations/
│   │   ├── handler.go
│   │   ├── models.go
│   │   ├── service.go
│   │   └── stream.go
│   ├── agentruns/
│   │   ├── handler.go
│   │   ├── models.go
│   │   └── service.go
│   ├── apiutil/
│   │   └── apiutil.go
│   ├── config/
│   │   └── config.go
│   ├── db/
│   │   └── db.go
│   ├── fileutil/
│   │   └── fileutil.go
│   ├── jobqueue/
│   │   └── jobqueue.go
│   ├── jobs/
│   │   ├── handler.go
│   │   ├── models.go
│   │   └── service.go
│   ├── notes/
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── models.go
│   ├── sources/
│   │   ├── handler.go
│   │   ├── models.go
│   │   └── service.go
│   ├── uploads/
│   │   ├── handler.go
│   │   ├── models.go
│   │   └── service.go
│   ├── documents/
│   │   ├── handler.go
│   │   ├── models.go
│   │   └── service.go
│   ├── sseutil/
│   │   └── sseutil.go
│   ├── search/
│   │   ├── handler.go
│   │   ├── models.go
│   │   └── service.go
│   └── server/
│       └── server.go
├── processor/
│   ├── pyproject.toml
│   └── src/
│       └── processor/
│           ├── __main__.py
│           ├── document.py
│           ├── heartbeat.py
│           ├── runtime.py
│           ├── reindex.py
│           ├── scan.py
│           ├── config.py
│           ├── db.py
│           ├── metadata.py
│           ├── parsing/
│           ├── chunking/
│           └── embedding/
├── agent/
│   ├── pyproject.toml
│   └── src/
│       └── agent/
│           ├── __main__.py
│           ├── config.py
│           ├── context.py
│           ├── db.py
│           ├── embedding.py
│           ├── graph.py
│           ├── nodes/
│           ├── retrieval/
│           ├── prompts.py
│           ├── state.py
│           └── tools/
├── storage/
│   └── uploads/
│       └── <upload-id>/
│           └── original
├── sql/
│   ├── schema.sql
│   └── query.sql
├── sqlc/
├── pyrightconfig.json
├── Compose.yaml
├── sqlc.yaml
├── go.mod
├── Makefile
└── plan.md
```

---

## 16. Technology Stack

| Layer | Technology | Purpose |
| --- | --- | --- |
| API | Go, Gin | HTTP API, orchestration |
| Indexer | Python | parsing, chunking, embedding |
| Agent | Python | retrieval + tool use |
| Database | PostgreSQL + pgvector | metadata, jobs, chunks, vectors |
| Codegen | `sqlc` | type-safe SQL access |
| Embeddings | Alibaba DashScope | vector embeddings |
| Chat LLM | OpenRouter | grounded responses and workflows |

---

## 17. Implementation Phases

### Phase 1 - Foundation

Deliverables:

- project structure
- PostgreSQL schema
- basic Go API
- source/note/document CRUD
- jobs table and basic worker loop

### Phase 2 - Managed Knowledge

Deliverables:

- pasted notes as first-class sources
- upload pipeline
- note update/delete/archive
- basic indexing pipeline for notes/uploads

### Phase 3 - Reconcilable Sources

Deliverables:

- file source
- directory source
- recursive scan
- source item reconciliation
- changed/new/deleted detection

### Phase 4 - Retrieval

Deliverables:

- chunk storage
- `pgvector` search
- lexical search
- hybrid ranking
- citations

### Phase 5 - Agent

Deliverables:

- conversation API
- tool-based retrieval
- grounded answering
- streamed final-answer generation
- conversation history replay into agent state
- database-backed personal/profile retrieval
- summarization and compare workflows

### Phase 6 - Observability and Lifecycle

Deliverables:

- job inspection
- retry flow
- agent runs
- archive/delete semantics
- inactive chunk GC

### Phase 7 - Polish

Deliverables:

- error handling
- tests
- performance tuning
- retrieval quality evaluation

---

## 18. Development Commands

```bash
# Generate sqlc code
sqlc generate

# Reset schema
psql $DATABASE_URL -f sql/schema.sql

# Run Go API
go run ./cmd/server

# Run Python processor
python -m processor

# Run Python agent
python -m agent
```

---

## 19. Environment Configuration

```dotenv
# Database
DATABASE_URL=postgres://postgres:postgres@localhost:5433/agentdb

# Embeddings
ALIBABA_API_KEY=sk-...
ALIBABA_EMBEDDING_MODEL=text-embedding-v4

# Chat LLM
OPENROUTER_API_KEY=sk-or-...
OPENROUTER_MODEL=qwen-max

# API
API_PORT=8080
API_HOST=0.0.0.0
LOG_LEVEL=info

# Jobs
WORKER_POOL_SIZE=4
JOB_LEASE_SECONDS=60
JOB_RETRY_LIMIT=5

# Source Scanning
SCAN_INTERVAL_SECONDS=1800
MAX_SCAN_FILE_SIZE_BYTES=52428800

# Local Storage
STORAGE_ROOT=./storage

# Observability
LANGSMITH_API_KEY=ls-...
LANGSMITH_PROJECT=personal-agent
```

---

## 20. Key Design Decisions

| Decision | Choice | Rationale |
| --- | --- | --- |
| Knowledge ingestion | explicit source registration | avoids fragile hidden sync behavior |
| Change detection | scan + reconcile | simpler and more portable than OS watchers |
| Directory support | recursive scan | useful without requiring recursive watch |
| Pasted text model | first-class note source | manageable lifecycle and deletion semantics |
| Upload model | upload source with its own table and immutable stored snapshot | old uploads remain until explicitly archived or deleted |
| Reindex strategy | safe updates via chunk build switching | no full document content history model in v1 |
| Deletion | soft delete + inactive chunks + later GC | preserves recoverability and trace integrity |
| Retrieval | hybrid lexical + vector | better quality than vector-only |
| Active retrieval state | only chunks from `active_build_id` are searchable | prevents mixed-build retrieval |
| Chunking | structure-aware | better retrieval and citation quality |
| Background execution | lease-based jobs in DB | simpler distributed safety and retries |
| Observability | jobs + agent runs | inspectable failures and decision traces |
| File watcher | excluded from core design | complexity does not justify value for this project |

---

## 21. Resume Framing

Suggested framing for resume/interview use:

Built a local-first personal knowledge base agent that ingests notes, files, uploads, and directories through explicit indexing jobs, normalizes heterogeneous content into a searchable corpus, and serves citation-grounded retrieval and streaming assistant responses over a Go API. Designed safe reindexing via chunk-build switching, database-backed job orchestration, and scan-based reconciliation instead of OS-specific file watching, improving portability, recovery, and correctness.

---

## 22. Memory Layer

### 22.1 Purpose and Role

Memory is a second retrieval layer above document knowledge.

Its job is to store a **small set of stable, user-centric, canonical facts** that help the agent answer personal questions efficiently and consistently.

Examples:

- profile facts
- preferences
- relationships
- ongoing projects
- durable constraints or routines

Memory is **not** just another chunk store.
It exists so the system does not have to repeatedly rediscover the same stable user context from raw notes, conversations, and files.

### 22.2 Knowledge Retrieval vs Memory

The system now has two distinct knowledge surfaces:

#### A. Document Knowledge

Backed by:

- `sources`
- `source_items`
- `documents`
- `index_builds`
- `chunks`

Use it for:

- broad semantic retrieval
- citations
- document-grounded answers
- summarization and synthesis over source material

Properties:

- larger
- derived from document content
- optimized for recall and evidence retrieval
- grounded through chunk citations

#### B. Memory

Backed by:

- `memories`
- `memory_suggestions`

Use it for:

- personal/profile questions
- durable preferences
- user-specific context the agent should remember explicitly

Properties:

- much smaller
- canonical rather than exhaustive
- user-centric rather than document-centric
- curated through review before activation

Important boundary:

- chunks remain the retrieval substrate for documents
- memories are standalone records and must not be materialized as chunks in v1

### 22.3 Canonical Memories vs Suggestions

The memory layer has two states of fact:

#### `memory_suggestions`

Candidate facts extracted from user-authored content.

Properties:

- produced by an extractor
- stored with provenance and evidence
- **not trusted as truth**
- reviewable and reversible
- lifecycle: `pending`, `accepted`, `rejected`, `expired`

#### `memories`

Canonical memory records that the agent may rely on directly.

Properties:

- created manually or by accepting a suggestion
- lifecycle: `active`, `archived`, `deleted`
- meant to be concise and durable
- should favor one stable fact per row

Acceptance rule:

- no extractor may auto-promote a suggestion into canonical memory in v1
- only explicit user acceptance creates or updates an active memory

### 22.4 Canonical Memory Shape

Memory should stay intentionally small and lightweight.

Recommended fields for `memories`:

- `id`
- `subject`
- `category`
- `key`
- `value`
- `status`
- `confidence`
- `source_id`
- `document_id`
- `message_id`
- `created_at`
- `updated_at`

Recommended fields for `memory_suggestions`:

- `id`
- `subject`
- `category`
- `key`
- `value`
- `confidence`
- `status`
- `extractor_type`
- `source_id`
- `document_id`
- `message_id`
- `evidence_text`
- `created_at`
- `updated_at`

Design rules:

- avoid a large ontology
- avoid graph edges in v1
- avoid automated conflict resolution in v1
- prefer explicit status transitions and simple CRUD

### 22.5 Provenance Model

Every memory suggestion and canonical memory should preserve lightweight provenance.

V1 provenance fields:

- `source_id` when the fact came from a source-backed note or upload context
- `document_id` when the fact came from a document-owned note
- `message_id` when the fact came from a conversation message
- `evidence_text` on suggestions for short human-reviewable justification

Provenance requirements:

- provenance explains where the candidate came from
- provenance does not by itself make the fact canonical
- accepted memory should retain origin references from the accepted suggestion when available

### 22.6 Canonical Memory Merge Policies

Canonical memory uses per-key merge behavior determined by `(category, key)`.

V1 implementation choice:

- use a small application-level policy map
- do **not** introduce a registry table in v1
- the server, not the LLM, decides whether a key is replace-style or append-style

#### Replace-Style Keys

These keys should normally have one active canonical value per `(subject, category, key)`.

Acceptance rule:

- on accept, archive existing active memories for the same `(subject, category, key)`
- then insert the accepted value as the new active memory

Examples:

- `profile.university`
- `profile.field_of_study`
- `profile.current_city`
- `project.status`

Typical meaning:

- current university
- current field of study
- current city
- current project status

#### Append-Style Keys

These keys may legitimately have multiple active canonical values.

Acceptance rule:

- on accept, insert a new active memory
- do not archive existing active memories for the same `(subject, category, key)`

Examples:

- `event.notable_event`
- `project.past_project`
- `relationship.known_person`
- `preference.liked_language`

Typical meaning:

- notable life or work events
- multiple prior projects
- multiple known people
- multiple liked languages

#### Initial V1 Canonical Key Policy List

The initial application-level policy map should include:

- replace: `profile.university`
- replace: `profile.field_of_study`
- replace: `profile.current_city`
- replace: `project.status`
- append: `event.notable_event`
- append: `project.past_project`
- append: `relationship.known_person`
- append: `preference.liked_language`

Recommended default for unknown keys in v1:

- treat them as append-style to avoid destructive overwrites from under-specified extraction

### 22.7 Extraction and Job Flow

V1 memory extraction is asynchronous and suggestion-first.

Supported triggers:

- new user message persisted in `messages`
- note created
- note updated

Not in scope for v1:

- full historical backfill
- uploads/files/directory corpus extraction
- automatic re-extraction across the entire corpus

Recommended flow:

1. A triggering item is persisted.
2. Go enqueues `extract_memory_suggestions`.
3. The processor claims the job through the existing lease-based queue.
4. The worker loads the relevant note or message payload.
5. The worker calls an LLM with a structured extraction prompt.
6. The worker parses strict JSON output.
7. The worker inserts `memory_suggestions` rows with `status = 'pending'`.
8. Review APIs expose pending suggestions for explicit accept/reject.

Job properties:

- dedupe by trigger target, for example `extract_memory_suggestions:message:{id}` or `extract_memory_suggestions:note:{id}`
- idempotent insert behavior should prevent accidental duplicate pending suggestions for the same trigger/evidence pair
- failures follow the existing retryable/permanent job model

### 22.8 Extraction Prompt Contract

The memory extractor should use an LLM-first structured prompt.

Prompt rules:

- extract only durable user-centric facts
- prefer canonical keys when the fact fits a known key
- ignore transient status chatter and weak speculation
- prefer fewer high-quality candidates over broad recall
- output JSON only
- include `subject`, `category`, `key`, `value`, `confidence`, and short `evidence_text`
- do not decide replace-vs-append behavior
- merge behavior must be determined server-side from the `(category, key)` policy map
- never mark a suggestion as accepted

### 22.9 Suggestion Acceptance Flow

When a suggestion is accepted:

1. Determine merge behavior from `(category, key)`.
2. If the key is replace-style:
   - archive existing active memories for the same `(subject, category, key)`
   - insert the accepted value as the new active memory
3. If the key is append-style:
   - insert a new active memory
   - leave existing active memories for that key untouched
4. Preserve provenance from the accepted suggestion on the new canonical memory row.

If an identical active memory already exists:

- v1 may treat acceptance as a no-op canonical insert and simply mark the suggestion accepted

### 22.10 Agent Retrieval Order

The agent should choose retrieval order based on question type.

For personal/profile/preference/relationship questions:

1. search active memories first
2. if memory is insufficient, fall back to document retrieval
3. answer with clear grounding, using memory as canonical context and documents as supporting evidence when needed

For document/content questions:

1. use normal chunk retrieval first
2. consult memory only when it adds user-specific context that changes interpretation

Implementation intent:

- memory lookup is a distinct tool or helper, not a hidden chunk search
- a helper such as `get_profile_context` may aggregate the most relevant active memories into a compact prompt block

### 22.11 Memory API Design

Memory requires both CRUD and review endpoints.

#### Memories

| Method | Endpoint | Description |
| --- | --- | --- |
| `POST` | `/api/v1/memories` | Create canonical memory |
| `GET` | `/api/v1/memories` | List memories |
| `GET` | `/api/v1/memories/{id}` | Get memory |
| `PUT` | `/api/v1/memories/{id}` | Update memory |
| `DELETE` | `/api/v1/memories/{id}` | Soft delete memory |
| `POST` | `/api/v1/memories/{id}/archive` | Archive memory |

#### Memory Suggestions

| Method | Endpoint | Description |
| --- | --- | --- |
| `GET` | `/api/v1/memory-suggestions` | List suggestions, typically filtered to pending |
| `GET` | `/api/v1/memory-suggestions/{id}` | Get suggestion |
| `POST` | `/api/v1/memory-suggestions/{id}/accept` | Accept suggestion into canonical memory |
| `POST` | `/api/v1/memory-suggestions/{id}/reject` | Reject suggestion |

### 22.12 Phased Memory Implementation Plan

#### Phase M1 - Core Data Model

Deliverables:

- schema for `memories` and `memory_suggestions`
- sqlc queries and models
- Go CRUD/read services for both resources
- explicit accept/reject status transitions

#### Phase M2 - Suggestion Extraction

Deliverables:

- new `extract_memory_suggestions` job type
- enqueue from note create/update and user message persistence
- structured LLM extraction prompt and parser
- pending suggestion storage with provenance

#### Phase M3 - Agent Integration

Deliverables:

- `search_memories` retrieval helper
- optional `get_profile_context`
- memory-first behavior for personal questions
- fallback to chunk retrieval when memory is missing or insufficient

#### Phase M4 - Trust and Operations

Deliverables:

- application-level canonical key policy map
- replace-vs-append acceptance semantics
- duplicate suppression and conflict visibility improvements
- suggestion expiration policy
- review UX improvements
- lightweight evaluation of memory usefulness and extraction precision

### 22.13 Explicit Deferrals

The following are intentionally deferred from v1:

- treating memories as chunks
- auto-accepting extracted suggestions
- graph/ontology expansion
- reranking memory results
- full conflict resolution between competing facts
- corpus-wide historical extraction

## 23. Future Extensions

Possible v2 features:

- code-aware chunking with AST support
- URL ingestion and content cleaning
- reranker integration
- collections/tags
- saved queries and workflows
- retrieval evaluation harness
- optional watcher as a hint-only optimization that triggers scans, never as source of truth
- memory conflict review and merge assistance
- historical backfill for memory suggestions
