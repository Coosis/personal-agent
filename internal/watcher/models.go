package watcher

import (
	"time"
)

// WatchDirectory represents a configured watch path
type WatchDirectory struct {
	ID        int64     `json:"id"`
	Path      string    `json:"path"`
	Pattern   string    `json:"pattern"`
	Recursive bool      `json:"recursive"`
	Enabled   bool      `json:"enabled"`
	Priority  int32     `json:"priority"`
	CreatedAt time.Time `json:"created_at"`
}

// AddRequest for registering a watch directory
type AddRequest struct {
	Path      string `json:"path" binding:"required"`
	Pattern   string `json:"pattern"`
	Recursive *bool  `json:"recursive,omitempty"`
	Priority  int32  `json:"priority"`
}

// UpdateRequest for updating watch directories
type UpdateRequest struct {
	Pattern   *string `json:"pattern,omitempty"`
	Recursive *bool   `json:"recursive,omitempty"`
	Enabled   *bool   `json:"enabled,omitempty"`
	Priority  *int32  `json:"priority,omitempty"`
}

// ListResponse for list results
type ListResponse struct {
	Directories []WatchDirectory `json:"directories"`
}
