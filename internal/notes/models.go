package notes

import "time"

type Note struct {
	ID          int64     `json:"id"`
	SourceID    int64     `json:"source_id"`
	DocumentID  *int64    `json:"document_id,omitempty"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	ContentHash string    `json:"content_hash"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body" binding:"required"`
}

type UpdateRequest struct {
	Title string `json:"title"`
	Body  string `json:"body" binding:"required"`
}

type ListRequest struct {
	Query           string `form:"query"`
	IncludeArchived bool   `form:"include_archived"`
	IncludeDeleted  bool   `form:"include_deleted"`
	Limit           int32  `form:"limit,default=20"`
	Offset          int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Notes []Note `json:"notes"`
}
