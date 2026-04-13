package memorysuggestions

import "time"

type MemorySuggestion struct {
	ID            int64     `json:"id"`
	Subject       string    `json:"subject"`
	Category      string    `json:"category"`
	Key           string    `json:"key"`
	Value         string    `json:"value"`
	Confidence    float64   `json:"confidence"`
	Status        string    `json:"status"`
	ExtractorType string    `json:"extractor_type"`
	SourceID      *int64    `json:"source_id,omitempty"`
	DocumentID    *int64    `json:"document_id,omitempty"`
	MessageID     *int64    `json:"message_id,omitempty"`
	EvidenceText  string    `json:"evidence_text"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ListRequest struct {
	Query  string `form:"query"`
	Status string `form:"status"`
	Limit  int32  `form:"limit,default=20"`
	Offset int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Suggestions []MemorySuggestion `json:"suggestions"`
}

type AcceptResponse struct {
	Suggestion MemorySuggestion `json:"suggestion"`
	MemoryID   int64            `json:"memory_id"`
}
