package jobqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	sqlc "github.com/Coosis/personal-agent/sqlc"
)

func Enqueue(
	ctx context.Context,
	q *sqlc.Queries,
	jobType string,
	payload any,
	dedupeKey string,
) (sqlc.Job, error) {
	if dedupeKey == "" {
		return sqlc.Job{}, errors.New("dedupeKey is required")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sqlc.Job{}, err
	}

	job, err := q.CreateJob(ctx, sqlc.CreateJobParams{
		Type:        jobType,
		Payload:     body,
		DedupeKey:   nullableText(dedupeKey),
		Status:      "pending",
		AvailableAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err == nil {
		return job, nil
	}

	if dedupeKey == "" {
		return sqlc.Job{}, err
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return q.GetPendingJobByDedupeKey(ctx, pgtype.Text{Valid: true, String: dedupeKey})
	}

	return sqlc.Job{}, err
}

func ReindexDocumentDedupeKey(documentID int64) string {
	return fmt.Sprintf("reindex_document:%d", documentID)
}

func ScanSourceDedupeKey(sourceID int64) string {
	return fmt.Sprintf("scan_source:%d", sourceID)
}

func PurgeSourceContentDedupeKey(sourceID int64) string {
	return fmt.Sprintf("purge_source_content:%d", sourceID)
}

func ExtractMemorySuggestionsFromNoteDedupeKey(noteID int64) string {
	return fmt.Sprintf("extract_memory_suggestions:note:%d", noteID)
}

func ExtractMemorySuggestionsFromMessageDedupeKey(messageID int64) string {
	return fmt.Sprintf("extract_memory_suggestions:message:%d", messageID)
}

func SummarizeConversationDedupeKey(conversationID int64, passIndex int32) string {
	return fmt.Sprintf("summarize_conversation:%d:%d", conversationID, passIndex)
}

func nullableText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: s, Valid: true}
}
