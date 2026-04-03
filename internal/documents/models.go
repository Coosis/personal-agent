package documents

import (
	"time"
)

// Status represents document processing status
type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusFailed     Status = "failed"
)

// Document represents a document entity (API model)
type Document struct {
	ID          int64      `json:"id"`
	Path        string     `json:"path"`
	Filename    string     `json:"filename"`
	Extension   string     `json:"extension"`
	MimeType    string     `json:"mime_type"`
	SizeBytes   int64      `json:"size_bytes"`
	Checksum    string     `json:"checksum"`
	Status      string     `json:"status"`
	ErrorMsg    *string    `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	IndexedAt   *time.Time `json:"indexed_at,omitempty"`
}

// ListRequest for listing documents
type ListRequest struct {
	Status string `form:"status"`
	Search string `form:"search"`
	Limit  int32  `form:"limit,default=20"`
	Offset int32  `form:"offset,default=0"`
}

// ListResponse for list results
type ListResponse struct {
	Documents []Document `json:"documents"`
	Total     int32      `json:"total"`
}
