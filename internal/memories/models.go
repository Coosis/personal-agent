package memories

import "time"

type Memory struct {
	ID         int64     `json:"id"`
	Subject    string    `json:"subject"`
	Category   string    `json:"category"`
	Key        string    `json:"key"`
	Value      string    `json:"value"`
	Status     string    `json:"status"`
	Confidence float64   `json:"confidence"`
	SourceID   *int64    `json:"source_id,omitempty"`
	DocumentID *int64    `json:"document_id,omitempty"`
	MessageID  *int64    `json:"message_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CreateRequest struct {
	Subject    string  `json:"subject" binding:"required"`
	Category   string  `json:"category" binding:"required"`
	Key        string  `json:"key" binding:"required"`
	Value      string  `json:"value" binding:"required"`
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	SourceID   *int64  `json:"source_id"`
	DocumentID *int64  `json:"document_id"`
	MessageID  *int64  `json:"message_id"`
}

type UpdateRequest struct {
	Subject    string  `json:"subject" binding:"required"`
	Category   string  `json:"category" binding:"required"`
	Key        string  `json:"key" binding:"required"`
	Value      string  `json:"value" binding:"required"`
	Status     string  `json:"status"`
	Confidence float64 `json:"confidence"`
	SourceID   *int64  `json:"source_id"`
	DocumentID *int64  `json:"document_id"`
	MessageID  *int64  `json:"message_id"`
}

type ListRequest struct {
	Query           string `form:"query"`
	Status          string `form:"status"`
	IncludeArchived bool   `form:"include_archived"`
	IncludeDeleted  bool   `form:"include_deleted"`
	Limit           int32  `form:"limit,default=20"`
	Offset          int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Memories []Memory `json:"memories"`
}
