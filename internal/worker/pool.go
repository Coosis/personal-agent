package worker

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

// ErrNoEvents indicates no unprocessed events available
var ErrNoEvents = fmt.Errorf("no unprocessed events")

// Pool manages a pool of workers that process file events
type Pool struct {
	db     *db.DB
	size   int
	wg     sync.WaitGroup
	stopCh chan struct{}
}

// NewPool creates a new worker pool
func NewPool(database *db.DB, size int) *Pool {
	return &Pool{
		db:     database,
		size:   size,
		stopCh: make(chan struct{}),
	}
}

// Start starts the worker pool
func (p *Pool) Start(ctx context.Context) {
	logrus.WithField("size", p.size).Info("starting worker pool")

	for i := 0; i < p.size; i++ {
		p.wg.Add(1)
		go p.worker(ctx, i)
	}
}

// Stop stops the worker pool gracefully
func (p *Pool) Stop() {
	logrus.Info("stopping worker pool")
	close(p.stopCh)
	p.wg.Wait()
	logrus.Info("worker pool stopped")
}

// worker is the main loop for each worker
func (p *Pool) worker(ctx context.Context, id int) {
	defer p.wg.Done()

	logrus.WithField("worker_id", id).Debug("worker started")

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		default:
			if err := p.processNextEvent(ctx); err != nil {
				if err != ErrNoEvents {
					logrus.WithError(err).WithField("worker_id", id).Error("failed to process event")
				}
				// Sleep briefly to avoid hammering the DB when empty
				time.Sleep(time.Second)
			}
		}
	}
}

// processNextEvent polls for and processes the next unprocessed file event
func (p *Pool) processNextEvent(ctx context.Context) error {
	// Start transaction first to hold the row lock during entire processing
	return p.db.WithTx(ctx, func(q *sqlc.Queries) error {
		// Get unprocessed events with row-level locking (SKIP LOCKED)
		// This lock is held until the transaction commits
		events, err := q.GetUnprocessedFileEventsForUpdate(ctx, 1)
		if err != nil {
			return fmt.Errorf("failed to poll events: %w", err)
		}

		if len(events) == 0 {
			return ErrNoEvents
		}
		event := events[0]

		logrus.WithFields(logrus.Fields{
			"event_id":   event.ID,
			"path":       event.Path,
			"event_type": event.EventType,
		}).Info("processing file event")

		// Process based on event type
		switch event.EventType {
		case "create", "modify":
			return p.handleCreateOrModifyTx(ctx, q, event)
		case "delete":
			return p.handleDeleteTx(ctx, q, event)
		default:
			// Unknown event type, mark as processed with error
			return p.markEventProcessedWithTx(ctx, q, event.ID, 0, fmt.Errorf("unknown event type: %s", event.EventType))
		}
	})
}

// handleCreateOrModifyTx handles create or modify events within a transaction
func (p *Pool) handleCreateOrModifyTx(ctx context.Context, q *sqlc.Queries, event sqlc.FileEvent) error {
	// Get file info
	info, err := getFileInfo(event.Path)
	if err != nil {
		// File might have been deleted between event creation and processing
		return p.markEventProcessedWithTx(ctx, q, event.ID, 0, err)
	}

	// Upsert document
	doc, err := q.UpsertDocument(ctx, sqlc.UpsertDocumentParams{
		Path:             event.Path,
		Filename:         filepath.Base(event.Path),
		Extension:        filepath.Ext(event.Path),
		MimeType:         info.mimeType,
		SizeBytes:        info.size,
		Checksum:         info.checksum,
		ContentHash:      pgtype.Text{},
		LastModified:     pgtype.Timestamptz{Time: info.modTime, Valid: true},
		Metadata:         []byte("{}"),
		ProcessingStatus: "pending",
	})
	if err != nil {
		return fmt.Errorf("failed to upsert document: %w", err)
	}

	// Mark event as processed and linked to document
	return p.markEventProcessedWithTx(ctx, q, event.ID, doc.ID, nil)
}

// handleDeleteTx handles delete events within a transaction
func (p *Pool) handleDeleteTx(ctx context.Context, q *sqlc.Queries, event sqlc.FileEvent) error {
	// Find existing document by path
	doc, err := q.GetDocumentByPath(ctx, event.Path)
	if err != nil && err != pgx.ErrNoRows {
		return fmt.Errorf("failed to get document: %w", err)
	}

	var docID int64
	if err == nil {
		docID = doc.ID
		// Soft delete - mark as deleted
		_, err = q.UpdateDocumentStatus(ctx, sqlc.UpdateDocumentStatusParams{
			ID:               doc.ID,
			ProcessingStatus: "deleted",
		})
		if err != nil {
			return fmt.Errorf("failed to mark document deleted: %w", err)
		}
	}

	// Mark event as processed
	return p.markEventProcessedWithTx(ctx, q, event.ID, docID, nil)
}

// fileInfo holds information about a file
type fileInfo struct {
	size     int64
	modTime  time.Time
	checksum string
	mimeType string
}

// getFileInfo extracts information from a file path
func getFileInfo(path string) (*fileInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file")
	}

	checksum, err := computeFileChecksum(path)
	if err != nil {
		return nil, fmt.Errorf("failed to compute checksum: %w", err)
	}

	return &fileInfo{
		size:     stat.Size(),
		modTime:  stat.ModTime(),
		checksum: checksum,
		mimeType: detectMimeType(path),
	}, nil
}

// computeFileChecksum computes a simple FNV hash of file content
func computeFileChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := fnv.New64a()
	h.Write(data)
	return fmt.Sprintf("%x", h.Sum64()), nil
}

// detectMimeType detects MIME type from file extension
func detectMimeType(path string) string {
	ext := filepath.Ext(path)
	switch ext {
	case ".txt":
		return "text/plain"
	case ".md":
		return "text/markdown"
	case ".pdf":
		return "application/pdf"
	case ".doc", ".docx":
		return "application/msword"
	case ".html", ".htm":
		return "text/html"
	case ".json":
		return "application/json"
	case ".go":
		return "text/x-go"
	case ".py":
		return "text/x-python"
	default:
		return "application/octet-stream"
	}
}

// markEventProcessedWithTx marks a file event as processed within a transaction
func (p *Pool) markEventProcessedWithTx(ctx context.Context, q *sqlc.Queries, eventID int64, docID int64, procErr error) error {
	errMsg := pgtype.Text{}
	if procErr != nil {
		errMsg = pgtype.Text{String: procErr.Error(), Valid: true}
	}

	_, err := q.LinkFileEventToDocument(ctx, sqlc.LinkFileEventToDocumentParams{
		ID:         eventID,
		DocumentID: pgtype.Int8{Int64: docID, Valid: docID > 0},
		Error:      errMsg,
	})
	return err
}
