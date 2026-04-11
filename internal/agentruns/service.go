package agentruns

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var ErrNotFound = errors.New("agent run not found")

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]AgentRun, error) {
	rows, err := s.db.Queries.ListAgentRuns(ctx, sqlc.ListAgentRunsParams{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]AgentRun, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAgentRun(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*AgentRun, error) {
	row, err := s.db.Queries.GetAgentRunByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toAgentRun(row)
	return &item, nil
}

func toAgentRun(row sqlc.AgentRun) AgentRun {
	return AgentRun{
		ID:                row.ID,
		ConversationID:    apiutil.Int64Ptr(row.ConversationID),
		TriggerMessageID:  apiutil.Int64Ptr(row.TriggerMessageID),
		Status:            row.Status,
		Trace:             row.Trace,
		ToolsUsed:         row.ToolsUsed,
		DocumentsAccessed: row.DocumentsAccessed,
		StartTime:         row.StartTime.Time,
		EndTime:           apiutil.TimePtr(row.EndTime),
		TotalTokens:       row.TotalTokens,
		TotalLatencyMs:    row.TotalLatencyMs,
		StepCount:         row.StepCount,
		ErrorType:         apiutil.TextPtr(row.ErrorType),
		ErrorMessage:      apiutil.TextPtr(row.ErrorMessage),
		Metadata:          row.Metadata,
		CreatedAt:         row.CreatedAt.Time,
	}
}
