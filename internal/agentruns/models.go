package agentruns

import "time"

type AgentRun struct {
	ID                int64      `json:"id"`
	ConversationID    *int64     `json:"conversation_id,omitempty"`
	TriggerMessageID  *int64     `json:"trigger_message_id,omitempty"`
	Status            string     `json:"status"`
	Trace             []byte     `json:"trace"`
	ToolsUsed         []string   `json:"tools_used"`
	DocumentsAccessed []int64    `json:"documents_accessed"`
	StartTime         time.Time  `json:"start_time"`
	EndTime           *time.Time `json:"end_time,omitempty"`
	TotalTokens       int32      `json:"total_tokens"`
	TotalLatencyMs    int32      `json:"total_latency_ms"`
	StepCount         int32      `json:"step_count"`
	ErrorType         *string    `json:"error_type,omitempty"`
	ErrorMessage      *string    `json:"error_message,omitempty"`
	Metadata          []byte     `json:"metadata"`
	CreatedAt         time.Time  `json:"created_at"`
}

type ListRequest struct {
	Limit  int32 `form:"limit,default=20"`
	Offset int32 `form:"offset,default=0"`
}

type ListResponse struct {
	AgentRuns []AgentRun `json:"agent_runs"`
}
