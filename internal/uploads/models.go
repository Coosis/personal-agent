package uploads

import "time"

type Upload struct {
	ID               int64     `json:"id"`
	SourceID         int64     `json:"source_id"`
	DocumentID       *int64    `json:"document_id,omitempty"`
	OriginalFilename string    `json:"original_filename"`
	StoragePath      string    `json:"storage_path"`
	MimeType         string    `json:"mime_type"`
	SizeBytes        int64     `json:"size_bytes"`
	ContentHash      string    `json:"content_hash"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CreateRequest struct {
	DisplayName string
	Filename    string
	ContentType string
}

type ListRequest struct {
	Query  string `form:"query"`
	Status string `form:"status"`
	Limit  int32  `form:"limit,default=20"`
	Offset int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Uploads []Upload `json:"uploads"`
}
