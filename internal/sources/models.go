package sources

import "time"

type Source struct {
	ID          int64      `json:"id"`
	Type        string     `json:"type"`
	SyncMode    string     `json:"sync_mode"`
	Locator     *string    `json:"locator,omitempty"`
	DisplayName *string    `json:"display_name,omitempty"`
	Status      string     `json:"status"`
	LastScanAt  *time.Time `json:"last_scan_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type CreateRequest struct {
	Type        string `json:"type" binding:"required"`
	Locator     string `json:"locator" binding:"required"`
	DisplayName string `json:"display_name"`
}

type ListRequest struct {
	Query           string `form:"query"`
	Type            string `form:"type"`
	Status          string `form:"status"`
	IncludeArchived bool   `form:"include_archived"`
	IncludeDeleted  bool   `form:"include_deleted"`
	Limit           int32  `form:"limit,default=20"`
	Offset          int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Sources []Source `json:"sources"`
}
