package notes

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/fileutil"
	"github.com/Coosis/personal-agent/internal/jobqueue"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var ErrNotFound = errors.New("note not found")

type Service struct {
	db *db.DB
}

func NewService(database *db.DB) *Service {
	return &Service{db: database}
}

func (s *Service) Create(ctx context.Context, req CreateRequest) (*Note, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled note"
	}

	body := strings.TrimSpace(req.Body)
	hash := fileutil.SHA256String(body)

	var created Note
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		source, err := q.CreateSource(ctx, sqlc.CreateSourceParams{
			Type:        "text",
			SyncMode:    "replace",
			Locator:     pgtype.Text{Valid: false},
			DisplayName: apiutil.Text(title),
			Status:      "active",
			Metadata:    apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		note, err := q.CreateNote(ctx, sqlc.CreateNoteParams{
			SourceID:    source.ID,
			Title:       title,
			Body:        body,
			ContentHash: hash,
			Metadata:    apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		document, err := q.CreateDocument(ctx, sqlc.CreateDocumentParams{
			SourceID:       source.ID,
			SourceItemID:   pgtype.Int8{Valid: false},
			Title:          title,
			MimeType:       "text/plain",
			Status:         "active",
			ParserVersion:  pgtype.Text{Valid: false},
			ChunkerVersion: pgtype.Text{Valid: false},
			Metadata:       apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		if _, err := jobqueue.Enqueue(ctx, q, "reindex_document", map[string]any{
			"source_id":   source.ID,
			"note_id":     note.ID,
			"document_id": document.ID,
			"trigger":     "note_create",
		}, jobqueue.ReindexDocumentDedupeKey(document.ID)); err != nil {
			return err
		}

		if _, err := jobqueue.Enqueue(ctx, q, "extract_memory_suggestions", map[string]any{
			"note_id":     note.ID,
			"source_id":   source.ID,
			"document_id": document.ID,
			"trigger":     "note_create",
		}, jobqueue.ExtractMemorySuggestionsFromNoteDedupeKey(note.ID)); err != nil {
			return err
		}

		created = toNote(note, &document.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &created, nil
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Note, error) {
	rows, err := s.db.Queries.ListNotes(ctx, sqlc.ListNotesParams{
		Query:           req.Query,
		IncludeArchived: req.IncludeArchived,
		IncludeDeleted:  req.IncludeDeleted,
		Limit:           req.Limit,
		Offset:          req.Offset,
	})
	if err != nil {
		return nil, err
	}

	notes := make([]Note, 0, len(rows))
	for _, row := range rows {
		docID := s.documentIDBySource(ctx, row.SourceID)
		notes = append(notes, toNote(row, docID))
	}
	return notes, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Note, error) {
	row, err := s.db.Queries.GetNoteByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	note := toNote(row, s.documentIDBySource(ctx, row.SourceID))
	return &note, nil
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*Note, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Untitled note"
	}

	body := strings.TrimSpace(req.Body)
	hash := fileutil.SHA256String(body)

	var updated Note
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		note, err := q.GetNoteByID(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		source, err := q.GetSourceByID(ctx, note.SourceID)
		if err != nil {
			return err
		}

		note, err = q.UpdateNote(ctx, sqlc.UpdateNoteParams{
			ID:          id,
			Title:       title,
			Body:        body,
			ContentHash: hash,
			Metadata:    note.Metadata,
		})
		if err != nil {
			return err
		}

		if _, err := q.UpdateSourceBasics(ctx, sqlc.UpdateSourceBasicsParams{
			ID:          source.ID,
			Locator:     source.Locator,
			DisplayName: apiutil.Text(title),
			Metadata:    source.Metadata,
		}); err != nil {
			return err
		}

		document, err := q.GetDocumentBySourceID(ctx, note.SourceID)
		if err != nil {
			return err
		}

		if _, err := q.UpdateSourceStatus(ctx, sqlc.UpdateSourceStatusParams{
			ID:     note.SourceID,
			Status: "active",
		}); err != nil {
			return err
		}

		if _, err := jobqueue.Enqueue(ctx, q, "reindex_document", map[string]any{
			"source_id":   note.SourceID,
			"note_id":     note.ID,
			"document_id": document.ID,
			"trigger":     "note_update",
		}, jobqueue.ReindexDocumentDedupeKey(document.ID)); err != nil {
			return err
		}

		if _, err := jobqueue.Enqueue(ctx, q, "extract_memory_suggestions", map[string]any{
			"note_id":     note.ID,
			"source_id":   note.SourceID,
			"document_id": document.ID,
			"trigger":     "note_update",
		}, jobqueue.ExtractMemorySuggestionsFromNoteDedupeKey(note.ID)); err != nil {
			return err
		}

		updated = toNote(note, &document.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &updated, nil
}

func (s *Service) Reindex(ctx context.Context, id int64) error {
	return s.enqueueDocumentJob(ctx, id, "reindex_document")
}

func (s *Service) Archive(ctx context.Context, id int64) error {
	return s.updateLifecycle(ctx, id, "archived")
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.updateLifecycle(ctx, id, "deleted")
}

func (s *Service) enqueueDocumentJob(ctx context.Context, noteID int64, jobType string) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		note, err := q.GetNoteByID(ctx, noteID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		document, err := q.GetDocumentBySourceID(ctx, note.SourceID)
		if err != nil {
			return err
		}

		_, err = jobqueue.Enqueue(ctx, q, jobType, map[string]any{
			"source_id":   note.SourceID,
			"note_id":     note.ID,
			"document_id": document.ID,
			"trigger":     "note_reindex",
		}, jobqueue.ReindexDocumentDedupeKey(document.ID))
		return err
	})
}

func (s *Service) updateLifecycle(ctx context.Context, noteID int64, status string) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		note, err := q.GetNoteByID(ctx, noteID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		if _, err := q.UpdateNoteStatus(ctx, sqlc.UpdateNoteStatusParams{
			ID:     noteID,
			Status: status,
		}); err != nil {
			return err
		}

		if _, err := q.UpdateSourceStatus(ctx, sqlc.UpdateSourceStatusParams{
			ID:     note.SourceID,
			Status: status,
		}); err != nil {
			return err
		}

		document, err := q.GetDocumentBySourceID(ctx, note.SourceID)
		if err == nil {
			if status == "archived" {
				_, err = q.ArchiveDocument(ctx, document.ID)
			} else {
				_, err = q.MarkDocumentDeleted(ctx, document.ID)
			}
		}
		return err
	})
}

func (s *Service) documentIDBySource(ctx context.Context, sourceID int64) *int64 {
	document, err := s.db.Queries.GetDocumentBySourceID(ctx, sourceID)
	if err != nil {
		return nil
	}
	return &document.ID
}

func toNote(row sqlc.Note, documentID *int64) Note {
	return Note{
		ID:          row.ID,
		SourceID:    row.SourceID,
		DocumentID:  documentID,
		Title:       row.Title,
		Body:        row.Body,
		ContentHash: row.ContentHash,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
}
