package conversations

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/agenthttp"
	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var (
	ErrNotFound         = errors.New("conversation not found")
	ErrAgentUnavailable = errors.New("agent unavailable")
)

type Service struct {
	db    *db.DB
	agent *agenthttp.Client
}

func NewService(database *db.DB, agent *agenthttp.Client) *Service {
	return &Service{db: database, agent: agent}
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Conversation, error) {
	rows, err := s.db.Queries.ListConversations(ctx, sqlc.ListConversationsParams{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Conversation, 0, len(rows))
	for _, row := range rows {
		items = append(items, toConversation(row))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Conversation, error) {
	row, err := s.db.Queries.GetConversationByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toConversation(row)
	return &item, nil
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Conversation, error) {
	row, err := s.db.Queries.CreateConversation(ctx, sqlc.CreateConversationParams{
		Title:    apiutil.Text(req.Title),
		Summary:  pgtype.Text{},
		Metadata: apiutil.DefaultJSONObject(),
	})
	if err != nil {
		return nil, err
	}

	item := toConversation(row)
	return &item, nil
}

func (s *Service) ListMessages(ctx context.Context, id int64, req ListMessagesRequest) ([]Message, error) {
	if _, err := s.db.Queries.GetConversationByID(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	rows, err := s.db.Queries.ListMessagesByConversation(ctx, sqlc.ListMessagesByConversationParams{
		ConversationID: id,
		Limit:          req.Limit,
		Offset:         req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Message, 0, len(rows))
	for _, row := range rows {
		items = append(items, toMessage(row))
	}
	return items, nil
}

func (s *Service) SendMessage(ctx context.Context, conversationID int64, req SendMessageRequest) (*SendMessageResponse, error) {
	if _, err := s.db.Queries.GetConversationByID(ctx, conversationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	seq, err := s.db.Queries.GetLatestMessageSequence(ctx, conversationID)
	if err != nil {
		return nil, err
	}

	userRow, err := s.db.Queries.CreateMessage(ctx, sqlc.CreateMessageParams{
		ConversationID:  conversationID,
		Role:            "user",
		Content:         req.Content,
		Citations:       apiutil.DefaultJSONArray(),
		ToolCalls:       apiutil.DefaultJSONArray(),
		ToolResults:     apiutil.DefaultJSONArray(),
		TokenCount:      pgtype.Int4{},
		LatencyMs:       pgtype.Int4{},
		Model:           pgtype.Text{},
		ParentMessageID: pgtype.Int8{},
		SequenceNumber:  seq + 1,
		Metadata:        apiutil.DefaultJSONObject(),
	})
	if err != nil {
		return nil, err
	}

	if s.agent == nil {
		return nil, ErrAgentUnavailable
	}

	content, err := s.agent.Chat(ctx, req.Content)
	if err != nil {
		return nil, err
	}

	assistantRow, err := s.db.Queries.CreateMessage(ctx, sqlc.CreateMessageParams{
		ConversationID:  conversationID,
		Role:            "assistant",
		Content:         content,
		Citations:       apiutil.DefaultJSONArray(),
		ToolCalls:       apiutil.DefaultJSONArray(),
		ToolResults:     apiutil.DefaultJSONArray(),
		TokenCount:      pgtype.Int4{},
		LatencyMs:       pgtype.Int4{},
		Model:           pgtype.Text{},
		ParentMessageID: pgtype.Int8{Int64: userRow.ID, Valid: true},
		SequenceNumber:  seq + 2,
		Metadata:        apiutil.DefaultJSONObject(),
	})
	if err != nil {
		return nil, err
	}

	return &SendMessageResponse{
		UserMessage:      toMessage(userRow),
		AssistantMessage: toMessage(assistantRow),
	}, nil
}

func toConversation(row sqlc.Conversation) Conversation {
	return Conversation{
		ID:        row.ID,
		Title:     apiutil.TextPtr(row.Title),
		Summary:   apiutil.TextPtr(row.Summary),
		Metadata:  row.Metadata,
		CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func toMessage(row sqlc.Message) Message {
	return Message{
		ID:             row.ID,
		ConversationID: row.ConversationID,
		Role:           row.Role,
		Content:        row.Content,
		Citations:      row.Citations,
		ToolCalls:      row.ToolCalls,
		ToolResults:    row.ToolResults,
		TokenCount:     apiutil.Int32Ptr(row.TokenCount),
		LatencyMs:      apiutil.Int32Ptr(row.LatencyMs),
		Model:          apiutil.TextPtr(row.Model),
		SequenceNumber: row.SequenceNumber,
		CreatedAt:      row.CreatedAt.Time,
	}
}
