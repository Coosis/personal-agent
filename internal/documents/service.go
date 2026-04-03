package documents

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

// Errors
var ErrNotFound = errors.New("document not found")

// Service provides document business logic
type Service struct {
	db *db.DB
}

// NewService creates a new document service
func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

// List retrieves documents with pagination
func (s *Service) List(ctx context.Context, req ListRequest) ([]Document, error) {
	rows, err := s.db.Queries.ListDocuments(ctx, sqlc.ListDocumentsParams{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	docs := make([]Document, len(rows))
	for i, r := range rows {
		docs[i] = fromSQLC(r)
	}
	return docs, nil
}

// Get retrieves a single document by ID
func (s *Service) Get(ctx context.Context, id int64) (*Document, error) {
	row, err := s.db.Queries.GetDocumentByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	doc := fromSQLC(row)
	return &doc, nil
}

// Delete removes a document and its chunks
func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.db.Queries.DeleteChunksByDocument(ctx, id)
	if err != nil {
		return err
	}

	_, err = s.db.Queries.DeleteDocument(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// Reindex triggers re-processing of a document
func (s *Service) Reindex(ctx context.Context, id int64) error {
	_, err := s.db.Queries.UpdateDocumentStatus(ctx, sqlc.UpdateDocumentStatusParams{
		ID:               id,
		ProcessingStatus: string(StatusPending),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// Scan triggers a full directory scan
func (s *Service) Scan(ctx context.Context) error {
	// TODO: trigger file watcher scan via watcher service
	logrus.Info("triggering directory scan")
	return nil
}

// fromSQLC converts sqlc Document to API Document
func fromSQLC(d sqlc.Document) Document {
	doc := Document{
		ID:        d.ID,
		Path:      d.Path,
		Filename:  d.Filename,
		Extension: d.Extension,
		MimeType:  d.MimeType,
		SizeBytes: d.SizeBytes,
		Checksum:  d.Checksum,
		Status:    d.ProcessingStatus,
		CreatedAt: d.CreatedAt.Time,
		UpdatedAt: d.UpdatedAt.Time,
	}
	if d.ErrorMessage.Valid {
		doc.ErrorMsg = &d.ErrorMessage.String
	}
	if d.IndexedAt.Valid {
		t := d.IndexedAt.Time
		doc.IndexedAt = &t
	}
	return doc
}
