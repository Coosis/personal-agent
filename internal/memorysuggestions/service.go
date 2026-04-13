package memorysuggestions

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var (
	ErrNotFound     = errors.New("memory suggestion not found")
	ErrInvalidState = errors.New("memory suggestion is not pending")
)

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]MemorySuggestion, error) {
	rows, err := s.db.Queries.ListMemorySuggestions(ctx, sqlc.ListMemorySuggestionsParams{
		Query:            req.Query,
		Status:           req.Status,
		SuggestionOffset: req.Offset,
		SuggestionLimit:  req.Limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]MemorySuggestion, 0, len(rows))
	for _, row := range rows {
		items = append(items, toMemorySuggestion(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*MemorySuggestion, error) {
	row, err := s.db.Queries.GetMemorySuggestionByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toMemorySuggestion(row)
	return &item, nil
}

func (s *Service) Accept(ctx context.Context, id int64) (*AcceptResponse, error) {
	var resp AcceptResponse
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		suggestion, err := q.GetMemorySuggestionByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if suggestion.Status != "pending" {
			return ErrInvalidState
		}

		policy := policyFor(suggestion.Category, suggestion.Key)
		memory, err := q.FindActiveMemoryMatch(ctx, sqlc.FindActiveMemoryMatchParams{
			Subject:  suggestion.Subject,
			Category: suggestion.Category,
			Key:      suggestion.Key,
			Value:    suggestion.Value,
		})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if errors.Is(err, pgx.ErrNoRows) {
			if policy == mergePolicyReplace {
				if err := q.ArchiveActiveMemoriesBySubjectCategoryKeyExceptID(ctx, sqlc.ArchiveActiveMemoriesBySubjectCategoryKeyExceptIDParams{
					Subject:  suggestion.Subject,
					Category: suggestion.Category,
					Key:      suggestion.Key,
					ID:       0,
				}); err != nil {
					return err
				}
			}

			memory, err = q.CreateMemory(ctx, sqlc.CreateMemoryParams{
				Subject:    suggestion.Subject,
				Category:   suggestion.Category,
				Key:        suggestion.Key,
				Value:      suggestion.Value,
				Status:     "active",
				Confidence: suggestion.Confidence,
				SourceID:   suggestion.SourceID,
				DocumentID: suggestion.DocumentID,
				MessageID:  suggestion.MessageID,
			})
			if err != nil {
				return err
			}
		} else if policy == mergePolicyReplace {
			if err := q.ArchiveActiveMemoriesBySubjectCategoryKeyExceptID(ctx, sqlc.ArchiveActiveMemoriesBySubjectCategoryKeyExceptIDParams{
				Subject:  suggestion.Subject,
				Category: suggestion.Category,
				Key:      suggestion.Key,
				ID:       memory.ID,
			}); err != nil {
				return err
			}
		}

		suggestion, err = q.UpdateMemorySuggestionStatus(ctx, sqlc.UpdateMemorySuggestionStatusParams{
			ID:     suggestion.ID,
			Status: "accepted",
		})
		if err != nil {
			return err
		}

		resp = AcceptResponse{
			Suggestion: toMemorySuggestion(suggestion),
			MemoryID:   memory.ID,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (s *Service) Reject(ctx context.Context, id int64) (*MemorySuggestion, error) {
	row, err := s.db.Queries.GetMemorySuggestionByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if row.Status != "pending" {
		return nil, ErrInvalidState
	}

	row, err = s.db.Queries.UpdateMemorySuggestionStatus(ctx, sqlc.UpdateMemorySuggestionStatusParams{
		ID:     id,
		Status: "rejected",
	})
	if err != nil {
		return nil, err
	}

	item := toMemorySuggestion(row)
	return &item, nil
}

func toMemorySuggestion(row sqlc.MemorySuggestion) MemorySuggestion {
	return MemorySuggestion{
		ID:            row.ID,
		Subject:       row.Subject,
		Category:      row.Category,
		Key:           row.Key,
		Value:         row.Value,
		Confidence:    row.Confidence,
		Status:        row.Status,
		ExtractorType: row.ExtractorType,
		SourceID:      apiutil.Int64Ptr(row.SourceID),
		DocumentID:    apiutil.Int64Ptr(row.DocumentID),
		MessageID:     apiutil.Int64Ptr(row.MessageID),
		EvidenceText:  row.EvidenceText,
		CreatedAt:     row.CreatedAt.Time,
		UpdatedAt:     row.UpdatedAt.Time,
	}
}
