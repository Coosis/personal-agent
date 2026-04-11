package documents

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/jobqueue"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var ErrNotFound = errors.New("document not found")

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Document, error) {
	rows, err := s.db.Queries.ListDocuments(ctx, sqlc.ListDocumentsParams{
		Column1: req.Query,
		Column2: req.Status,
		Limit:   req.Limit,
		Offset:  req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Document, 0, len(rows))
	for _, row := range rows {
		items = append(items, toDocument(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Document, error) {
	row, err := s.db.Queries.GetDocumentByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	doc := toDocument(row)
	return &doc, nil
}

func (s *Service) Reindex(ctx context.Context, id int64) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetDocumentByID(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		_, err := jobqueue.Enqueue(ctx, q, "reindex_document", map[string]any{
			"document_id": id,
			"trigger":     "document_reindex",
		}, jobqueue.ReindexDocumentDedupeKey(id))
		return err
	})
}

func (s *Service) Archive(ctx context.Context, id int64) error {
	_, err := s.db.Queries.ArchiveDocument(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
	}
	return err
}

func (s *Service) MarkDeleted(ctx context.Context, id int64) error {
	_, err := s.db.Queries.MarkDocumentDeleted(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
	}
	return err
}

func toDocument(row sqlc.Document) Document {
	return Document{
		ID:             row.ID,
		SourceID:       row.SourceID,
		SourceItemID:   apiutil.Int64Ptr(row.SourceItemID),
		Title:          row.Title,
		MimeType:       row.MimeType,
		Status:         row.Status,
		ActiveBuildID:  apiutil.Int64Ptr(row.ActiveBuildID),
		IndexedAt:      apiutil.TimePtr(row.IndexedAt),
		ParserVersion:  apiutil.TextPtr(row.ParserVersion),
		ChunkerVersion: apiutil.TextPtr(row.ChunkerVersion),
		LastError:      apiutil.TextPtr(row.LastError),
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}
