# Personal Knowledge Base Agent - Project Plan

## 1. Project Overview

### 1.1 Vision
A Personal Multimodal Knowledge Base & Task Assistant - an intelligent agent that acts as a second brain. It automatically ingests, indexes, and organizes local documents, enabling semantic search and agentic task execution.

### 1.2 Core Capabilities
- **Knowledge capture**: Automatic watching and indexing of documents
- **Semantic retrieval**: Context-aware answers grounded in actual documents
- **Agentic execution**: Multi-step task execution via tool use
- **Observability**: Full traceability of agent decisions

---

## 2. Architecture

### 2.1 Component Overview

```
┌─────────────┐     ┌───────────────┐     ┌───────────────┐
│  Web UI     │────▶│   Go API      │────▶│  Python Agent │
│  (Optional) │     │   (Gin)       │     │  (LangGraph)  │
└─────────────┘     └───────┬───────┘     └───────┬───────┘
                            │                     │
                            │                     ▼
                            │              ┌───────────────┐
                            │              │  LLM Service  │
                            │              │  (OpenRouter) │
                            │              │  Qwen/Claude  │
                            │              └───────────────┘
                            │
                            ▼
                     ┌───────────────┐
                     │  PostgreSQL   │◀──── Python Processor
                     │  pgvector     │      (async/celery)
                     └───────┬───────┘           │
                             │                   │
                             │            ┌──────┴──────────┐
                             │            ▼                 ▼
                             │     ┌──────────┐      ┌──────────┐
                             │     │Semantic  │      │ Document │
                             │     │Chunking  │      │ Parsing  │
                             │     └────┬─────┘      └──────────┘
                             │            │
                             │            ▼
                             │     ┌───────────────┐
                             │     │  Embedding    │
                             │     │  (Alibaba)    │
                             │     │  text-embed   │
                             │     └───────────────┘
                             │
                             ▼
                      ┌───────────────┐
                      │  File Watcher │
                      │  (fsnotify)   │
                      └───────────────┘
```

### 2.2 Technology Stack

| Layer | Technology | Purpose |
|-------|-----------|---------|
| API | Go 1.25, Gin, logrus | HTTP server, file watcher, worker pool |
| Agent | Python 3.13, LangGraph, LangChain | Chat agent with ReAct reasoning |
| Processor | Python 3.13, APScheduler/Celery, Unstructured.io | Document parsing, chunking, embeddings |
| Database | PostgreSQL 15+, pgvector | Document metadata, chunks, vectors, conversations |
| Codegen | sqlc | Type-safe SQL |
| Embeddings | Alibaba DashScope (text-embedding-v3) | Vector embeddings for semantic search |
| Chat LLM | OpenRouter (qwen-max/claude) | Conversation generation, reasoning |

---

## 3. Happy Path User Journey

```
1. User starts server
   └── Server connects to PostgreSQL
   └── File watcher starts (if configured)

2. User configures watch directories
   └── POST /api/v1/watch/directories
   └── { "path": "/Users/me/Documents" }

3. File watcher detects new file
   └── File event created in file_events table
   └── Document record created with "pending" status

4. Background processor picks up document
   └── Extracts text based on file type
   └── Generates semantic chunks
   └── Sends chunks to embedding service
   └── Stores chunks with vectors in database
   └── Marks document as "completed"

5. User creates a conversation
   └── POST /api/v1/conversations
   └── { "title": "Research on AI" }

6. User sends a message
   └── POST /api/v1/conversations/123/messages
   └── { "content": "What do my documents say about ML?" }
   └── API calls Python agent with context

7. Agent processes request
   └── Uses ReAct loop: think → act → observe
   └── Calls search_knowledge tool (semantic search)
   └── Retrieves relevant chunks from pgvector
   └── Generates answer grounded in documents
   └── Streams response back via SSE

8. User views conversation history
   └── GET /api/v1/conversations/123
   └── Returns conversation + all messages

9. User searches documents directly
   └── POST /api/v1/search
   └── { "query": "neural networks", "limit": 5 }
   └── Returns top chunks with similarity scores
```

---

## 4. File Watcher & Change Detection

### 4.1 How It Works (Metadata-Based)

Instead of maintaining in-memory state that is lost on restart, the watcher uses the database as the source of truth:

```
On Server Startup:
├── For each watch directory:
│   └── Walk filesystem
│   └── For each file:
│       ├── Check documents table by path
│       ├── If not exists → NEW → create file_event
│       └── If exists but mtime > last_modified → MODIFIED → create file_event
│
On Add Watch Directory (runtime):
├── Add directory to fsnotify
└── Trigger async scan of existing files
    └── Same logic as startup scan
│
Realtime (fsnotify):
├── CREATE → create file_event
├── WRITE → create file_event  
├── REMOVE → soft delete document (set status='deleted')
└── RENAME → treat as REMOVE + CREATE
```

### 4.2 Metadata Storage

The `documents` table stores:
- `path`: File path (indexed, unique)
- `checksum`: SHA256 of content
- `last_modified`: Filesystem mtime
- `size_bytes`: File size
- `processing_status`: pending/processing/completed/failed

The `file_events` table stores:
- `path`: File path
- `event_type`: create/modify/delete
- `size_bytes`: File size (from stat, fast)
- `processed`: Boolean (processor marks true after handling)
- `document_id`: Links to documents table after processing

### 4.3 Startup Scan Algorithm

```go
func (w *Watcher) startupScan(ctx context.Context) error {
    dirs := db.GetWatchDirectories()
    
    for _, dir := range dirs {
        filepath.Walk(dir.Path, func(path string, info fs.FileInfo, err error) error {
            // Skip directories, check pattern match
            
            existingDoc := db.GetDocumentByPath(path)
            
            if existingDoc == nil {
                // NEW FILE
                db.CreateFileEvent(path, "create", size)
            } else if info.ModTime().After(existingDoc.LastModified) {
                // MODIFIED FILE (mtime check only, worker verifies checksum)
                db.CreateFileEvent(path, "modify", size)
            }
            
            return nil
        })
    }
    
    // Check for deleted files
    allDocs := db.GetAllDocuments()
    for _, doc := range allDocs {
        if _, err := os.Stat(doc.Path); os.IsNotExist(err) {
            db.UpdateDocumentStatus(doc.ID, "deleted")
        }
    }
}
```

---

## 5. Worker Pool (Go)

### 5.1 Overview

The worker pool is a Go-based background processor that handles file events created by the file watcher. It runs multiple workers concurrently to process events atomically using database transactions.

### 5.2 Two-Stage Processing Architecture

The system uses two separate polling stages to decouple file detection from content processing:

```
Stage 1: File Detection (Go)
============================
File Watcher → File Events Table → Go Worker Pool
(fsnotify)       (fast queue)         (lightweight)
                   ↓
              ┌─────────────┐
              │ path        │
              │ size        │  ← No checksum (fast)
              │ mtime       │
              │ status=new  │
              └─────────────┘

Stage 2: Content Processing (Python)
===================================
Documents Table → Python Processor → Chunks Table
(status=pending)   (heavy lifting)    (embeddings)
      ↑
┌─────────────┐
│ checksum    │  ← Computed here
│ text content│
│ chunks      │
│ vectors     │
└─────────────┘
```

**Why two stages?**
1. **File Watcher must be fast** - fsnotify events can be dropped if handler blocks
2. **Checksums are slow** - Computing SHA256 for large files blocks the watcher
3. **Separation of concerns** - Go handles metadata, Python handles NLP/ML

### 5.3 Go Worker Flow

```
┌──────────────────┐
| Poll DB for      │
| unprocessed      │
| file events      │
└────────┬─────────┘
         │ FOR UPDATE SKIP LOCKED
         ▼
┌──────────────────┐
| Compute checksum │  ← Slow I/O done here
| (SHA256)         │    (not in watcher)
└────────┬─────────┘
         ▼
┌──────────────────┐
| Upsert document  │
| (path + checksum)│
└────────┬─────────┘
         ▼
┌──────────────────┐
| Mark event       │
| processed        │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
| Document ready   │
| for Python       │
└──────────────────┘
```

### 5.3 Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `WORKER_POOL_SIZE` | 4 | Number of concurrent workers |

### 5.4 Key Features

- **Row-level locking**: Uses `FOR UPDATE SKIP LOCKED` to prevent multiple workers from processing the same event
- **Atomic transactions**: Document upsert and event marking happen in a single transaction
- **Idempotent processing**: Safe to retry - upsert handles duplicates, processed events are skipped
- **Concurrent workers**: Multiple workers poll the database independently for higher throughput

---

## 6. Background Processor (Python)

### 6.1 Architecture Decision: Pure Python

The background processor is implemented in Python because:
- **Semantic chunking** requires NLP libraries (LangChain, Unstructured.io)
- **Document parsing** ecosystem is Python-first (PyPDF, python-docx, etc.)
- **Simpler codebase** - no cross-service calls for the processing pipeline

### 6.2 Processor Flow

```
┌──────────────┐
│ Poll DB for  │
│ pending docs │
└──────┬───────┘
       ▼
┌──────────────────┐
│ Get document and │
│ file content     │
└────────┬─────────┘
         ▼
┌──────────────────┐
│ Parse document   │
│ (Unstructured.io)│
└────────┬─────────┘
         ▼
┌──────────────────┐
│ Semantic chunking│
│ (LangChain)      │
└────────┬─────────┘
         ▼
┌──────────────────┐
│ Get embeddings   │
│ (Alibaba API)    │
└────────┬─────────┘
         ▼
┌──────────────────┐
│ Store chunks     │
│ in pgvector      │
└────────┬─────────┘
         ▼
┌──────────────────┐
│ Mark document    │
│ as completed     │
└──────────────────┘
```

### 6.3 Processor Components

| Component | Purpose | Library |
|-----------|---------|---------|
| **Scheduler** | Poll DB for work | APScheduler / Celery |
| **Parser** | Extract text from files | Unstructured.io |
| **Chunker** | Semantic chunking | LangChain TextSplitter |
| **Embedder** | Generate vectors | Alibaba DashScope |
| **DB Client** | Store results | psycopg3 / asyncpg |

### 6.4 Document Processing States

```
pending ──────► processing ──────► completed
    │                │
    │                ▼
    │             failed (retryable)
    │                │
    │                ▼
    └────────────► failed (permanent)
```

---

## 7. API Reference

### 7.1 Documents
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/documents | List documents with pagination |
| GET | /api/v1/documents/{id} | Get document details |
| DELETE | /api/v1/documents/{id} | Delete document |
| POST | /api/v1/documents/{id}/reindex | Trigger re-indexing |
| POST | /api/v1/documents/scan | Trigger directory scan |

**Query Parameters (List)**:
- `status`: Filter by processing status (pending|processing|completed|failed)
- `search`: Full-text search on filename/path
- `limit`, `offset`: Pagination

### 7.2 Search & Retrieval
| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | /api/v1/search | Semantic search across chunks |
| POST | /api/v1/search/similar | Find similar chunks to a text |

**Search Request Body**:
```json
{
  "query": "string",
  "document_ids": [123, 456],
  "limit": 10,
  "min_similarity": 0.7,
  "include_metadata": true
}
```

### 7.3 Conversations
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/conversations | List conversations |
| POST | /api/v1/conversations | Create new conversation |
| GET | /api/v1/conversations/{id} | Get conversation with messages |
| PUT | /api/v1/conversations/{id} | Update conversation |
| DELETE | /api/v1/conversations/{id} | Delete conversation |
| POST | /api/v1/conversations/{id}/messages | Send message (supports streaming) |
| GET | /api/v1/conversations/{id}/messages | Get messages |

**Streaming Response Format** (SSE):
```
event: thinking
data: {"step": "analyze", "thought": "..."}

event: tool_call
data: {"tool": "search_knowledge", "args": {...}}

event: tool_result
data: {"tool": "search_knowledge", "results": [...]}

event: answer
data: {"delta": "..."}

event: done
data: {"message_id": 789, "tokens_used": 234}
```

### 7.4 Agent Runs
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/agent-runs | List agent execution traces |
| GET | /api/v1/agent-runs/{id} | Get detailed trace |

### 7.5 Configuration & Watch
| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | /api/v1/config | Get all configuration |
| GET | /api/v1/config/{key} | Get specific config |
| PUT | /api/v1/config/{key} | Update configuration |
| GET | /api/v1/watch/directories | Get watched directories |
| POST | /api/v1/watch/directories | Add watch directory |
| PUT | /api/v1/watch/directories/{id} | Update watch config |
| DELETE | /api/v1/watch/directories/{id} | Remove watch directory |
| POST | /api/v1/watch/start | Start file watcher |
| POST | /api/v1/watch/stop | Stop file watcher |

---

## 8. Chunking Strategy

### 7.1 Semantic Chunking Architecture

Instead of naive paragraph splitting, implement strategy-based chunking that preserves semantic coherence:

```
Document -> Parser -> Structure Extractor -> Chunk Strategy -> Overlap Handler -> Chunks
```

### 7.2 Chunk Strategies by Format

| Format | Strategy | Key Features |
|--------|----------|--------------|
| **Markdown** | Header-based Hierarchical | Split at header boundaries; preserve heading context |
| **Code** | AST/Parse Tree | Split at function/class level; preserve imports |
| **PDF/DOCX** | Layout-aware | Respect page boundaries; extract captions/tables |
| **HTML** | DOM Structure | Section-based; preserve link context |
| **Plain Text** | Semantic+Fixed Hybrid | Paragraph + sentence boundary detection |

### 7.3 Chunk Metadata

Each chunk carries:
- `heading_path[]`: Hierarchical section headers
- `start_offset`, `end_offset`: Position in source
- `parent_chunk_id`: For hierarchical relationships
- `document_metadata`: Propagated from source
- `semantic_type`: (heading|body|code|table|caption)

### 7.4 Chunking Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| chunk_target_size | 512 tokens | Target chunk size |
| chunk_overlap_ratio | 0.1 | Overlap between adjacent chunks |
| max_chunk_size | 1024 tokens | Hard upper limit |
| semantic_boundary_threshold | 0.3 | NLP boundary detection threshold |

---

## 9. Database Schema (Conceptual)

### 8.1 Entity Relationships

```
documents (1) ---< (N) chunks
    |
    +--< (N) file_events

conversations (1) ---< (N) messages
    |
    +--< (N) agent_runs (via trigger_message_id)

watch_directories ---< file_events (via path matching)
```

### 8.2 Key Tables

- **documents**: File metadata, processing status, checksums
- **chunks**: Text chunks with vector embeddings (pgvector)
- **conversations**: Chat session metadata
- **messages**: Full conversation history
- **agent_runs**: Agent execution traces with ReAct trajectory
- **watch_directories**: Configured paths to monitor
- **file_events**: Change events from file watcher
- **config**: Runtime configuration

---

## 10. Directory Structure

```
personal-agent/
├── cmd/
│   └── server/
│       └── main.go              # Application entry point
├── internal/                    # Private application code (domain-organized)
│   ├── config/
│   │   └── config.go            # Environment config with const defaults
│   ├── db/
│   │   └── db.go                # Database pool + sqlc wrapper
│   ├── documents/
│   │   ├── handler.go           # HTTP handlers
│   │   ├── service.go           # Business logic
│   │   └── models.go            # API models
│   ├── conversations/
│   │   ├── handler.go
│   │   ├── service.go
│   │   └── models.go
│   ├── watcher/                 # File watcher implementation
│   │   ├── watcher.go           # fsnotify + startup scan
│   │   ├── service.go           # Watch directory CRUD
│   │   ├── handler.go
│   │   └── models.go
│   ├── worker/                  # Background worker pool
│   │   └── pool.go              # File event processor workers
│   └── server/
│       └── server.go            # Gin server setup + route wiring
├── processor/                   # Python background processor
│   ├── pyproject.toml           # Dependencies
│   ├── requirements.txt         # Alternative deps file
│   └── src/
│       └── processor/
│           ├── __init__.py
│           ├── __main__.py      # Entry point
│           ├── config.py        # Settings
│           ├── db.py            # Database operations
│           ├── parsing.py       # Document text extraction
│           ├── chunking.py      # Semantic chunking
│           └── embedding.py     # Alibaba DashScope client
├── agent/                       # Python LangGraph agent
│   ├── pyproject.toml
│   └── src/
│       ├── agent/
│       ├── tools/
│       └── retrieval/
├── sql/
│   ├── schema.sql               # DDL definitions (with DROP IF EXISTS)
│   └── query.sql                # sqlc queries
├── sqlc/                        # Generated code (gitignored)
│   ├── db.go                    # sqlc DB interface
│   ├── models.go                # sqlc generated types (uses int64 IDs)
│   ├── querier.go               # sqlc interface
│   └── query.sql.go             # sqlc generated queries
├── stubs/                       # Python type stubs for pyright
│   ├── psycopg/                 # PostgreSQL driver stubs
│   ├── tenacity/                # Retry library stubs
│   └── README.md                # Stub generation guide
├── pyrightconfig.json           # Pyright configuration
├── Compose.yml
├── sqlc.yaml
├── go.mod
└── Makefile
```

---

## 11. Implementation Phases

| Phase | Timeline | Deliverables |
|-------|----------|--------------|
| **1. Foundation** | Week 1-2 | Project structure, database, sqlc codegen, basic Gin server |
| **2. File Pipeline** | Week 3-4 | File watcher (fsnotify + startup scan), file_events, metadata tracking |
| **3. Processor** | Week 5 | Python processor, document parsing, basic chunking |
| **4. Semantic Chunking** | Week 6 | Format-specific chunkers, embedding integration |
| **5. Agent Core** | Week 7-8 | Python agent, tool registry, knowledge retrieval |
| **6. Conversations** | Week 9-10 | Chat API, streaming, conversation management |
| **7. Observability** | Week 11 | Agent runs logging, performance metrics |
| **8. Polish** | Week 12 | Error handling, comprehensive testing |

---

## 12. Development Commands

```bash
# Generate sqlc code (outputs to sqlc/ directory)
sqlc generate

# Reset database to fresh state (schema.sql has DROP IF EXISTS)
psql $DATABASE_URL -f sql/schema.sql

# Run Go server
go run ./cmd/server

# Run Python processor (from processor/ directory)
cd processor
pip install -e .
python -m processor

# Or with requirements.txt
cd processor
pip install -r requirements.txt
python -m processor
```

---

## 13. Environment Configuration

```bash
# Database
DATABASE_URL=postgres://postgres:postgres@localhost:5432/agentdb

# Embedding Service (Alibaba DashScope)
# Used only for: semantic chunking, vector indexing
ALIBABA_API_KEY=sk-...
ALIBABA_EMBEDDING_MODEL=text-embedding-v3

# Chat LLM (OpenRouter)
# Used only for: conversation, reasoning, agent actions
OPENROUTER_API_KEY=sk-or-...
OPENROUTER_MODEL=qwen-max

# API Server
API_PORT=8080
API_HOST=0.0.0.0
LOG_LEVEL=info

# File Watcher
WATCH_DIRS="/Users/me/Documents,/Users/me/Notes"
SCAN_INTERVAL_SECONDS=3600

# Worker Pool
WORKER_POOL_SIZE=4

# Observability (optional)
LANGSMITH_API_KEY=ls-...
LANGSMITH_PROJECT=personal-agent
```

---

## 14. Key Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Architecture | Go API + Python Agent + Python Processor | Go for API performance, Python for NLP/ML |
| Chunking | Semantic strategies | Better retrieval quality than naive splitting |
| Database | PostgreSQL + pgvector | ACID + native vector storage |
| API Style | REST + SSE streaming | Simplicity + real-time chat experience |
| Auth | None (local-only) | Personal use assumption |
| sqlc Output | `sqlc/` (gitignored) | Generated code should not be version controlled |
| Code Organization | Domain-based | Groups handlers/service/routes by domain |
| Logging | logrus | Structured logging with levels |
| Primary Keys | BIGINT (not UUID) | Better insert performance for local use |
| No migrations | DROP IF EXISTS in schema.sql | Quick rebuild during development |
| Change Detection | DB metadata (mtime only in watcher) + async initial scan | Fast watcher, checksum computed by worker, existing files caught on add |
| Processing Pipeline | Two-stage (Go metadata → Python content) | Watcher stays fast, heavy I/O in worker |
| Worker Pool | Go-based with row-level locking | Concurrent event processing with SKIP LOCKED |
| Embedding Service | Alibaba DashScope (text-embedding-v3) | Dedicated for semantic search only |
| Chat LLM | OpenRouter (qwen-max/claude) | Dedicated for conversation/reasoning only |

---

*Last Updated: April 2, 2026*
