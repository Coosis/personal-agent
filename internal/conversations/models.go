package conversations

import "time"

type Conversation struct {
	ID        int64     `json:"id"`
	Title     *string   `json:"title,omitempty"`
	Summary   *string   `json:"summary,omitempty"`
	Metadata  []byte    `json:"metadata"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	Role           string    `json:"role"`
	Status         string    `json:"status"`
	Content        string    `json:"content"`
	Citations      []byte    `json:"citations"`
	ToolCalls      []byte    `json:"tool_calls"`
	ToolResults    []byte    `json:"tool_results"`
	TokenCount     *int32    `json:"token_count,omitempty"`
	LatencyMs      *int32    `json:"latency_ms,omitempty"`
	Model          *string   `json:"model,omitempty"`
	SequenceNumber int32     `json:"sequence_number"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateRequest struct {
	Title string `json:"title"`
}

type SendMessageRequest struct {
	Content string `json:"content" binding:"required"`
}

type SendMessageResponse struct {
	UserMessage      Message `json:"user_message"`
	AssistantMessage Message `json:"assistant_message"`
}

type ListRequest struct {
	Limit  int32 `form:"limit,default=20"`
	Offset int32 `form:"offset,default=0"`
}

type ListResponse struct {
	Conversations []Conversation `json:"conversations"`
}

type ListMessagesRequest struct {
	Limit  int32 `form:"limit,default=50"`
	Offset int32 `form:"offset,default=0"`
}
