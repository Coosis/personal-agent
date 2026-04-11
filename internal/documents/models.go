package documents

import "time"

type Document struct {
	ID             int64      `json:"id"`
	SourceID       int64      `json:"source_id"`
	SourceItemID   *int64     `json:"source_item_id,omitempty"`
	Title          string     `json:"title"`
	MimeType       string     `json:"mime_type"`
	Status         string     `json:"status"`
	ActiveBuildID  *int64     `json:"active_build_id,omitempty"`
	IndexedAt      *time.Time `json:"indexed_at,omitempty"`
	ParserVersion  *string    `json:"parser_version,omitempty"`
	ChunkerVersion *string    `json:"chunker_version,omitempty"`
	LastError      *string    `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ListRequest struct {
	Query  string `form:"query"`
	Status string `form:"status"`
	Limit  int32  `form:"limit,default=20"`
	Offset int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Documents []Document `json:"documents"`
}
