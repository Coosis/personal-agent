package sources

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/fileutil"
	"github.com/Coosis/personal-agent/internal/jobqueue"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var (
	ErrNotFound    = errors.New("source not found")
	ErrInvalidType = errors.New("invalid source type")
)

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Source, error) {
	sourceType := strings.TrimSpace(req.Type)
	if sourceType != "file" && sourceType != "directory" {
		return nil, ErrInvalidType
	}

	locator := strings.TrimSpace(req.Locator)
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = filepath.Base(locator)
		if displayName == "" {
			displayName = locator
		}
	}

	var created Source
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		source, err := q.CreateSource(ctx, sqlc.CreateSourceParams{
			Type:        sourceType,
			SyncMode:    "reconcile",
			Locator:     apiutil.Text(locator),
			DisplayName: apiutil.Text(displayName),
			Status:      "active",
			Metadata:    apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		if sourceType == "file" {
			if _, err := q.CreateDocument(ctx, sqlc.CreateDocumentParams{
				SourceID:       source.ID,
				SourceItemID:   pgtype.Int8{},
				Title:          displayName,
				MimeType:       fileutil.DetectMimeType(locator),
				Status:         "active",
				ParserVersion:  pgtype.Text{},
				ChunkerVersion: pgtype.Text{},
				Metadata:       apiutil.DefaultJSONObject(),
			}); err != nil {
				return err
			}
		}

		if _, err := jobqueue.Enqueue(ctx, q, "scan_source", map[string]any{
			"source_id": source.ID,
		}, jobqueue.ScanSourceDedupeKey(source.ID)); err != nil {
			return err
		}

		created = toSource(source)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &created, nil
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Source, error) {
	rows, err := s.db.Queries.ListSources(ctx, sqlc.ListSourcesParams{
		Query:           req.Query,
		Type:            req.Type,
		Status:          req.Status,
		IncludeArchived: req.IncludeArchived,
		IncludeDeleted:  req.IncludeDeleted,
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Source, 0, len(rows))
	for _, row := range rows {
		items = append(items, toSource(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Source, error) {
	row, err := s.db.Queries.GetSourceByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toSource(row)
	return &item, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetSourceByID(ctx, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		if _, err := q.UpdateSourceStatus(ctx, sqlc.UpdateSourceStatusParams{
			ID:     id,
			Status: "deleted",
		}); err != nil {
			return err
		}

		if err := q.MarkDocumentsDeletedBySourceID(ctx, id); err != nil {
			return err
		}

		return q.MarkSourceItemsDeletedBySourceID(ctx, id)
	})
}

func (s *Service) Scan(ctx context.Context, id int64) error {
	return s.enqueueSourceJob(ctx, id, "scan_source")
}

func (s *Service) Purge(ctx context.Context, id int64) error {
	return s.enqueueSourceJob(ctx, id, "purge_source_content")
}

func (s *Service) enqueueSourceJob(ctx context.Context, id int64, jobType string) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		source, err := q.GetSourceByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if source.Type != "file" && source.Type != "directory" {
			return nil
		}

		_, err = jobqueue.Enqueue(ctx, q, jobType, map[string]any{
			"source_id": source.ID,
		}, sourceJobDedupeKey(jobType, source.ID))
		return err
	})
}

func sourceJobDedupeKey(jobType string, sourceID int64) string {
	switch jobType {
	case "scan_source":
		return jobqueue.ScanSourceDedupeKey(sourceID)
	case "purge_source_content":
		return jobqueue.PurgeSourceContentDedupeKey(sourceID)
	default:
		return fmt.Sprintf("%s:%d", jobType, sourceID)
	}
}

func toSource(row sqlc.Source) Source {
	return Source{
		ID:          row.ID,
		Type:        row.Type,
		SyncMode:    row.SyncMode,
		Locator:     apiutil.TextPtr(row.Locator),
		DisplayName: apiutil.TextPtr(row.DisplayName),
		Status:      row.Status,
		LastScanAt:  apiutil.TimePtr(row.LastScanAt),
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}
