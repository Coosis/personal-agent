package conversations

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

type preparedMessages struct {
	user      sqlc.Message
	assistant sqlc.Message
	history   []agenthttp.ChatMessage
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
	// db stuff
	prepared, err := s.prepareMessages(ctx, conversationID, req)
	if err != nil {
		return nil, err
	}

	// streaming
	assistantRow, err := s.runAssistantReplyStream(ctx, prepared.assistant, req.Content, prepared.history, nil)
	response := &SendMessageResponse{
		UserMessage:      toMessage(prepared.user),
		AssistantMessage: toMessage(assistantRow),
	}
	if err != nil {
		return response, err
	}
	return response, nil
}

// insert messages into db in single tx started within
func (s *Service) prepareMessages(
	ctx context.Context,
	conversationID int64,
	req SendMessageRequest,
) (*preparedMessages, error) {
	if _, err := s.db.Queries.GetConversationByID(ctx, conversationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if s.agent == nil {
		return nil, ErrAgentUnavailable
	}

	var prepared preparedMessages
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		historyRows, err := q.ListCompletedMessagesByConversation(ctx, conversationID)
		if err != nil {
			return err
		}
		prepared.history = buildAgentConversationHistory(historyRows)

		seq, err := q.GetLatestMessageSequence(ctx, conversationID)
		if err != nil {
			return err
		}

		prepared.user, err = q.CreateMessage(ctx, sqlc.CreateMessageParams{
			ConversationID:  conversationID,
			Role:            "user",
			Status:          "completed",
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
			return err
		}

		prepared.assistant, err = q.CreateMessage(ctx, sqlc.CreateMessageParams{
			ConversationID:  conversationID,
			Role:            "assistant",
			Status:          "streaming",
			Content:         "",
			Citations:       apiutil.DefaultJSONArray(),
			ToolCalls:       apiutil.DefaultJSONArray(),
			ToolResults:     apiutil.DefaultJSONArray(),
			TokenCount:      pgtype.Int4{},
			LatencyMs:       pgtype.Int4{},
			Model:           pgtype.Text{},
			ParentMessageID: pgtype.Int8{Int64: prepared.user.ID, Valid: true},
			SequenceNumber:  seq + 2,
			Metadata:        apiutil.DefaultJSONObject(),
		})
		return err
	})
	if err != nil {
		return nil, err
	}

	return &prepared, nil
}

func (s *Service) runAssistantReplyStream(
	ctx context.Context,
	assistant sqlc.Message,
	content string,
	history []agenthttp.ChatMessage,
	onToken func(string) error,
) (sqlc.Message, error) {
	var builder strings.Builder

	// streaming part
	err := s.agent.ChatStream(ctx, content, history, func(token string) error {
		builder.WriteString(token)
		if onToken == nil {
			return nil
		}
		return onToken(token)
	})
	// streaming finished, clean up phase

	finalizeCtx := context.WithoutCancel(ctx)
	if err != nil {
		failed, updateErr := s.db.Queries.UpdateMessageContentAndStatus(finalizeCtx, sqlc.UpdateMessageContentAndStatusParams{
			ID:      assistant.ID,
			Content: builder.String(),
			Status:  "failed",
		})
		if updateErr != nil {
			return assistant, fmt.Errorf("agent stream failed: %w; additionally failed to mark message failed: %v", err, updateErr)
		}
		return failed, err
	}

	completed, err := s.db.Queries.UpdateMessageContentAndStatus(finalizeCtx, sqlc.UpdateMessageContentAndStatusParams{
		ID:      assistant.ID,
		Content: builder.String(),
		Status:  "completed",
	})
	if err != nil {
		return assistant, err
	}
	return completed, nil
}

func buildAgentConversationHistory(rows []sqlc.Message) []agenthttp.ChatMessage {
	history := make([]agenthttp.ChatMessage, 0, len(rows))
	for _, row := range rows {
		if row.Role != "user" && row.Role != "assistant" && row.Role != "system" && row.Role != "tool" {
			continue
		}
		history = append(history, agenthttp.ChatMessage{
			Role:    row.Role,
			Content: row.Content,
		})
	}
	return history
}

// sqlc convenience functions

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
		Status:         row.Status,
		Content:        row.Content,
		Citations:      row.Citations,
		ToolCalls:      row.ToolCalls,
		ToolResults:    row.ToolResults,
		TokenCount:     apiutil.Int32Ptr(row.TokenCount),
		LatencyMs:      apiutil.Int32Ptr(row.LatencyMs),
		Model:          apiutil.TextPtr(row.Model),
		SequenceNumber: row.SequenceNumber,
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}
}
