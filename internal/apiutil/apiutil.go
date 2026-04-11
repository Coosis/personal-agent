package apiutil

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

func ParseIDParam(c *gin.Context, name string) (int64, error) {
	return strconv.ParseInt(c.Param(name), 10, 64)
}

func Error(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"error": message})
}

func DefaultJSONObject() []byte {
	return []byte("{}")
}

func DefaultJSONArray() []byte {
	return []byte("[]")
}

func MarshalJSON(v any) []byte {
	if v == nil {
		return DefaultJSONObject()
	}

	data, err := json.Marshal(v)
	if err != nil {
		return DefaultJSONObject()
	}
	return data
}

func Text(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}

func Int8(v int64) pgtype.Int8 {
	return pgtype.Int8{Int64: v, Valid: true}
}

func TimePtr(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time
	return &t
}

func TextPtr(v pgtype.Text) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func Int32Ptr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	n := v.Int32
	return &n
}

func Int64Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	n := v.Int64
	return &n
}
