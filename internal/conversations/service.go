package conversations

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

// Errors
var (
	ErrNotFound    = errors.New("conversation not found")
	ErrMsgNotFound = errors.New("message not found")
)

// Service provides conversation business logic
type Service struct {
	db *db.DB
}

// NewService creates a new conversation service
func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

// List retrieves conversations with pagination
func (s *Service) List(ctx context.Context, req ListRequest) ([]Conversation, error) {
	rows, err := s.db.Queries.ListConversations(ctx, sqlc.ListConversationsParams{
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	convs := make([]Conversation, len(rows))
	for i, r := range rows {
		convs[i] = fromSQLC(r)
	}
	return convs, nil
}

// Get retrieves a single conversation by ID with messages
func (s *Service) Get(ctx context.Context, id int64) (*Conversation, []Message, error) {
	row, err := s.db.Queries.GetConversationByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}

	conv := fromSQLC(row)

	msgs, err := s.db.Queries.GetMessagesByConversation(ctx, sqlc.GetMessagesByConversationParams{
		ConversationID: id,
		Limit:          100,
		Offset:         0,
	})
	if err != nil {
		return nil, nil, err
	}

	messages := make([]Message, len(msgs))
	for i, m := range msgs {
		messages[i] = messageFromSQLC(m)
	}

	return &conv, messages, nil
}

// Create creates a new conversation
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Conversation, error) {
	row, err := s.db.Queries.CreateConversation(ctx, sqlc.CreateConversationParams{
		Title:    pgtype.Text{String: req.Title, Valid: req.Title != ""},
		Metadata: req.Metadata,
	})
	if err != nil {
		return nil, err
	}

	conv := fromSQLC(row)
	return &conv, nil
}

// Update updates a conversation
func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*Conversation, error) {
	row, err := s.db.Queries.GetConversationByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	conv, err := s.db.Queries.UpdateConversation(ctx, sqlc.UpdateConversationParams{
		ID:              id,
		Title:           pgtype.Text{String: req.Title, Valid: req.Title != ""},
		MessageCount:    row.MessageCount,
		TokenUsageTotal: row.TokenUsageTotal,
		Metadata:        req.Metadata,
	})
	if err != nil {
		return nil, err
	}

	result := fromSQLC(conv)
	return &result, nil
}

// Delete removes a conversation
func (s *Service) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Queries.DeleteConversation(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// SendMessage adds a message to a conversation
func (s *Service) SendMessage(ctx context.Context, convID int64, req SendMessageRequest) (*Message, error) {
	// Check conversation exists
	_, err := s.db.Queries.GetConversationByID(ctx, convID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Get next sequence number
	seq, err := s.db.Queries.GetLatestMessageSequence(ctx, convID)
	if err != nil {
		return nil, err
	}

	var seqNum int32
	switch v := seq.(type) {
	case int32:
		seqNum = v + 1
	case int64:
		seqNum = int32(v) + 1
	case float64:
		seqNum = int32(v) + 1
	default:
		seqNum = 1
	}

	row, err := s.db.Queries.CreateMessage(ctx, sqlc.CreateMessageParams{
		ConversationID: convID,
		Role:           "user",
		Content:        req.Content,
		SequenceNumber: seqNum,
	})
	if err != nil {
		return nil, err
	}

	msg := messageFromSQLC(row)
	return &msg, nil
}

// fromSQLC converts sqlc Conversation to API Conversation
func fromSQLC(c sqlc.Conversation) Conversation {
	conv := Conversation{
		ID:              c.ID,
		MessageCount:    c.MessageCount.Int32,
		TokenUsageTotal: c.TokenUsageTotal.Int32,
		CreatedAt:       c.CreatedAt.Time,
		UpdatedAt:       c.UpdatedAt.Time,
	}
	if c.Title.Valid {
		conv.Title = &c.Title.String
	}
	if c.Summary.Valid {
		conv.Summary = &c.Summary.String
	}
	conv.SourceDocIDs = c.SourceDocumentIds
	return conv
}

// messageFromSQLC converts sqlc Message to API Message
func messageFromSQLC(m sqlc.Message) Message {
	msg := Message{
		ID:             m.ID,
		ConversationID: m.ConversationID,
		Role:           m.Role,
		Content:        m.Content,
		SequenceNumber: m.SequenceNumber,
		CreatedAt:      m.CreatedAt.Time,
	}
	if m.TokenCount.Valid {
		msg.TokenCount = &m.TokenCount.Int32
	}
	if m.LatencyMs.Valid {
		msg.LatencyMs = &m.LatencyMs.Int32
	}
	if m.Model.Valid {
		msg.Model = &m.Model.String
	}
	return msg
}
