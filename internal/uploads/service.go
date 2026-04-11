package uploads

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Coosis/personal-agent/internal/apiutil"
	"github.com/Coosis/personal-agent/internal/db"
	"github.com/Coosis/personal-agent/internal/fileutil"
	"github.com/Coosis/personal-agent/internal/jobqueue"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var ErrNotFound = errors.New("upload not found")

type Service struct {
	db          *db.DB
	storageRoot string
}

func NewService(database *db.DB, storageRoot string) *Service {
	return &Service{db: database, storageRoot: storageRoot}
}

func (s *Service) Create(ctx context.Context, req CreateRequest, file io.Reader) (*Upload, error) {
	hash, size, data, err := fileutil.SHA256Reader(file)
	if err != nil {
		return nil, err
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		filename = "upload.bin"
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		displayName = filename
	}

	mimeType := strings.TrimSpace(req.ContentType)
	if mimeType == "" {
		mimeType = fileutil.DetectMimeType(filename)
	}

	var created Upload
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		source, err := q.CreateSource(ctx, sqlc.CreateSourceParams{
			Type:        "upload",
			SyncMode:    "none",
			Locator:     pgtype.Text{},
			DisplayName: apiutil.Text(displayName),
			Status:      "active",
			Metadata:    apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		upload, err := q.CreateUpload(ctx, sqlc.CreateUploadParams{
			SourceID:         source.ID,
			OriginalFilename: filename,
			StoragePath:      "",
			MimeType:         mimeType,
			SizeBytes:        size,
			ContentHash:      hash,
			Metadata:         apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		document, err := q.CreateDocument(ctx, sqlc.CreateDocumentParams{
			SourceID:       source.ID,
			SourceItemID:   pgtype.Int8{},
			Title:          displayName,
			MimeType:       mimeType,
			Status:         "active",
			ParserVersion:  pgtype.Text{},
			ChunkerVersion: pgtype.Text{},
			Metadata:       apiutil.DefaultJSONObject(),
		})
		if err != nil {
			return err
		}

		storagePath := filepath.Join(s.storageRoot, "uploads", strconv.FormatInt(upload.ID, 10), "original")
		if err := os.MkdirAll(filepath.Dir(storagePath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(storagePath, data, 0o644); err != nil {
			return err
		}

		upload, err = q.UpdateUploadStoredFile(ctx, sqlc.UpdateUploadStoredFileParams{
			ID:          upload.ID,
			StoragePath: storagePath,
			MimeType:    mimeType,
			SizeBytes:   size,
			ContentHash: hash,
			Metadata:    upload.Metadata,
		})
		if err != nil {
			return err
		}

		if _, err := jobqueue.Enqueue(ctx, q, "reindex_document", map[string]any{
			"source_id":   source.ID,
			"upload_id":   upload.ID,
			"document_id": document.ID,
			"trigger":     "upload_create",
		}, jobqueue.ReindexDocumentDedupeKey(document.ID)); err != nil {
			return err
		}

		created = toUpload(upload, &document.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &created, nil
}

func (s *Service) List(ctx context.Context, req ListRequest) ([]Upload, error) {
	rows, err := s.db.Queries.ListUploads(ctx, sqlc.ListUploadsParams{
		Query:  req.Query,
		Status: req.Status,
		Limit:  req.Limit,
		Offset: req.Offset,
	})
	if err != nil {
		return nil, err
	}

	items := make([]Upload, 0, len(rows))
	for _, row := range rows {
		docID := s.documentIDBySource(ctx, row.SourceID)
		items = append(items, toUpload(row, docID))
	}
	return items, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Upload, error) {
	row, err := s.db.Queries.GetUploadByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	item := toUpload(row, s.documentIDBySource(ctx, row.SourceID))
	return &item, nil
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

func (s *Service) enqueueDocumentJob(ctx context.Context, uploadID int64, jobType string) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		upload, err := q.GetUploadByID(ctx, uploadID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		document, err := q.GetDocumentBySourceID(ctx, upload.SourceID)
		if err != nil {
			return err
		}

		_, err = jobqueue.Enqueue(ctx, q, jobType, map[string]any{
			"source_id":   upload.SourceID,
			"upload_id":   upload.ID,
			"document_id": document.ID,
			"trigger":     "upload_reindex",
		}, jobqueue.ReindexDocumentDedupeKey(document.ID))
		return err
	})
}

func (s *Service) updateLifecycle(ctx context.Context, uploadID int64, status string) error {
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		upload, err := q.GetUploadByID(ctx, uploadID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}

		if _, err := q.UpdateUploadStatus(ctx, sqlc.UpdateUploadStatusParams{
			ID:     uploadID,
			Status: status,
		}); err != nil {
			return err
		}

		if _, err := q.UpdateSourceStatus(ctx, sqlc.UpdateSourceStatusParams{
			ID:     upload.SourceID,
			Status: status,
		}); err != nil {
			return err
		}

		document, err := q.GetDocumentBySourceID(ctx, upload.SourceID)
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

func toUpload(row sqlc.Upload, documentID *int64) Upload {
	return Upload{
		ID:               row.ID,
		SourceID:         row.SourceID,
		DocumentID:       documentID,
		OriginalFilename: row.OriginalFilename,
		StoragePath:      row.StoragePath,
		MimeType:         row.MimeType,
		SizeBytes:        row.SizeBytes,
		ContentHash:      row.ContentHash,
		CreatedAt:        row.CreatedAt.Time,
		UpdatedAt:        row.UpdatedAt.Time,
	}
}
