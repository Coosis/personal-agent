package jobs

import "time"

type Job struct {
	ID             int64      `json:"id"`
	Type           string     `json:"type"`
	Payload        []byte     `json:"payload"`
	DedupeKey      *string    `json:"dedupe_key,omitempty"`
	Status         string     `json:"status"`
	AttemptCount   int32      `json:"attempt_count"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	AvailableAt    time.Time  `json:"available_at"`
	LastError      *string    `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type ListRequest struct {
	Status string `form:"status"`
	Limit  int32  `form:"limit,default=20"`
	Offset int32  `form:"offset,default=0"`
}

type ListResponse struct {
	Jobs []Job `json:"jobs"`
}
