package memories

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var (
	ErrNotFound      = errors.New("memory not found")
	ErrInvalidStatus = errors.New("invalid memory status")
)

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Memory, error) {
	row, err := s.db.Queries.CreateMemory(ctx, sqlc.CreateMemoryParams{
		Subject:    strings.TrimSpace(req.Subject),
		Category:   strings.TrimSpace(req.Category),
		Key:        strings.TrimSpace(req.Key),
		Value:      strings.TrimSpace(req.Value),
		Status:     normalizeStatus(req.Status),
		Confidence: req.Confidence,
		SourceID:   nullableInt8(req.SourceID),
		DocumentID: nullableInt8(req.DocumentID),
		MessageID:  nullableInt8(req.MessageID),
	})
	if err != nil {
		return nil, err
	}

	item := toMemory(row)
	return &item, nil
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Memory, error) {
	rows, err := s.db.Queries.ListMemories(ctx, sqlc.ListMemoriesParams{
		Query:           req.Query,
		Status:          req.Status,
		IncludeArchived: req.IncludeArchived,
		IncludeDeleted:  req.IncludeDeleted,
		MemoryOffset:    req.Offset,
		MemoryLimit:     req.Limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Memory, 0, len(rows))
	for _, row := range rows {
		items = append(items, toMemory(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Memory, error) {
	row, err := s.db.Queries.GetMemoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toMemory(row)
	return &item, nil
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*Memory, error) {
	current, err := s.db.Queries.GetMemoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	status := current.Status
	if strings.TrimSpace(req.Status) != "" {
		status = normalizeStatus(req.Status)
	}

	row, err := s.db.Queries.UpdateMemory(ctx, sqlc.UpdateMemoryParams{
		ID:         id,
		Subject:    strings.TrimSpace(req.Subject),
		Category:   strings.TrimSpace(req.Category),
		Key:        strings.TrimSpace(req.Key),
		Value:      strings.TrimSpace(req.Value),
		Status:     status,
		Confidence: req.Confidence,
		SourceID:   nullableInt8(req.SourceID),
		DocumentID: nullableInt8(req.DocumentID),
		MessageID:  nullableInt8(req.MessageID),
	})
	if err != nil {
		return nil, err
	}

	item := toMemory(row)
	return &item, nil
}

func (s *Service) Archive(ctx context.Context, id int64) error {
	return s.updateStatus(ctx, id, "archived")
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.updateStatus(ctx, id, "deleted")
}

func (s *Service) updateStatus(ctx context.Context, id int64, status string) error {
	_, err := s.db.Queries.UpdateMemoryStatus(ctx, sqlc.UpdateMemoryStatusParams{
		ID:     id,
		Status: status,
	})
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func normalizeStatus(status string) string {
	switch strings.TrimSpace(status) {
	case "", "active":
		return "active"
	case "archived":
		return "archived"
	case "deleted":
		return "deleted"
	default:
		return "active"
	}
}

func nullableInt8(v *int64) pgtype.Int8 {
	if v == nil {
		return pgtype.Int8{}
	}
	return apiutil.Int8(*v)
}

func toMemory(row sqlc.Memory) Memory {
	return Memory{
		ID:         row.ID,
		Subject:    row.Subject,
		Category:   row.Category,
		Key:        row.Key,
		Value:      row.Value,
		Status:     row.Status,
		Confidence: row.Confidence,
		SourceID:   apiutil.Int64Ptr(row.SourceID),
		DocumentID: apiutil.Int64Ptr(row.DocumentID),
		MessageID:  apiutil.Int64Ptr(row.MessageID),
		CreatedAt:  row.CreatedAt.Time,
		UpdatedAt:  row.UpdatedAt.Time,
	}
}
