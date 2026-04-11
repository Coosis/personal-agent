package jobs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var ErrNotFound = errors.New("job not found")

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Job, error) {
	rows, err := s.db.Queries.ListJobs(ctx, sqlc.ListJobsParams{
		Column1: req.Status,
		Limit:   req.Limit,
		Offset:  req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Job, 0, len(rows))
	for _, row := range rows {
		items = append(items, toJob(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Job, error) {
	row, err := s.db.Queries.GetJobByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toJob(row)
	return &item, nil
}

func (s *Service) Retry(ctx context.Context, id int64) (*Job, error) {
	row, err := s.db.Queries.RetryJob(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toJob(row)
	return &item, nil
}

func toJob(row sqlc.Job) Job {
	availableAt := row.AvailableAt.Time
	return Job{
		ID:             row.ID,
		Type:           row.Type,
		Payload:        row.Payload,
		DedupeKey:      apiutil.TextPtr(row.DedupeKey),
		Status:         row.Status,
		AttemptCount:   row.AttemptCount,
		LeaseExpiresAt: apiutil.TimePtr(row.LeaseExpiresAt),
		AvailableAt:    availableAt,
		LastError:      apiutil.TextPtr(row.LastError),
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}
