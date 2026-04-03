package conversations

import (
	"time"
)

// Conversation represents a chat session
type Conversation struct {
	ID              int64     `json:"id"`
	Title           *string   `json:"title,omitempty"`
	Summary         *string   `json:"summary,omitempty"`
	SourceDocIDs    []int64   `json:"source_document_ids,omitempty"`
	MessageCount    int32     `json:"message_count"`
	TokenUsageTotal int32     `json:"token_usage_total"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Message represents a chat message
type Message struct {
	ID             int64      `json:"id"`
	ConversationID int64      `json:"conversation_id"`
	Role           string     `json:"role"`
	Content        string     `json:"content"`
	TokenCount     *int32     `json:"token_count,omitempty"`
	LatencyMs      *int32     `json:"latency_ms,omitempty"`
	Model          *string    `json:"model,omitempty"`
	SequenceNumber int32      `json:"sequence_number"`
	CreatedAt      time.Time  `json:"created_at"`
}

// CreateRequest for creating conversations
type CreateRequest struct {
	Title    string `json:"title,omitempty"`
	Metadata []byte `json:"metadata,omitempty"`
}

// UpdateRequest for updating conversations
type UpdateRequest struct {
	Title    string `json:"title,omitempty"`
	Metadata []byte `json:"metadata,omitempty"`
}

// SendMessageRequest for sending a message
type SendMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

// ListRequest for listing conversations
type ListRequest struct {
	Limit  int32 `form:"limit,default=20"`
	Offset int32 `form:"offset,default=0"`
}

// ListResponse for list results
type ListResponse struct {
	Conversations []Conversation `json:"conversations"`
	Total         int32          `json:"total"`
}

// ListMessagesRequest for listing messages
type ListMessagesRequest struct {
	Limit  int32 `form:"limit,default=50"`
	Offset int32 `form:"offset,default=0"`
}
