package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

// Errors
var (
	ErrNotFound          = errors.New("watch directory not found")
	ErrAlreadyExists     = errors.New("watch directory already exists")
	ErrNestedPath        = errors.New("path is nested within an existing recursive watch directory")
	ErrParentPathExists  = errors.New("a recursive watch directory already covers this path")
	ErrPathDoesNotExist  = errors.New("path does not exist")
	ErrPathNotADirectory = errors.New("path is not a directory")
	ErrFailedToStatPath  = errors.New("failed to stat path")
)

// Service provides watch directory business logic and manages the file watcher
type Service struct {
	db      *db.DB
	watcher *Watcher
}

// NewService creates a new watcher service
func NewService(database *db.DB) (*Service, error) {
	w, err := NewWatcher(database)
	if err != nil {
		return nil, err
	}

	return &Service{
		db:      database,
		watcher: w,
	}, nil
}

// StartWatcher starts the file watcher (including startup scan)
func (s *Service) StartWatcher(ctx context.Context) error {
	logrus.Info("starting file watcher service")
	return s.watcher.Start(ctx)
}

// StopWatcher stops the file watcher
func (s *Service) StopWatcher() error {
	logrus.Info("stopping file watcher service")
	return s.watcher.Stop()
}

// IsWatcherRunning returns whether the file watcher is running
func (s *Service) IsWatcherRunning() bool {
	return s.watcher.IsRunning()
}

// List retrieves all watch directories
func (s *Service) List(ctx context.Context) ([]WatchDirectory, error) {
	rows, err := s.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		return nil, err
	}

	dirs := make([]WatchDirectory, len(rows))
	for i, r := range rows {
		dirs[i] = fromSQLC(r)
	}
	return dirs, nil
}

// Get retrieves a watch directory by ID
func (s *Service) Get(ctx context.Context, id int64) (*WatchDirectory, error) {
	row, err := s.db.Queries.GetWatchDirectoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	dir := fromSQLC(row)
	return &dir, nil
}

// Add registers a new watch directory and adds it to the watcher if running
func (s *Service) Add(ctx context.Context, req AddRequest) (*WatchDirectory, error) {
	// Check if path already exists
	existing, err := s.db.Queries.GetWatchDirectoryByPath(ctx, req.Path)
	if err == nil && existing.ID != 0 {
		return nil, ErrAlreadyExists
	}

	// Check if path exists
	info, err := os.Stat(req.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPathDoesNotExist
		}
		return nil, ErrFailedToStatPath
	}
	if !info.IsDir() {
		return nil, ErrPathNotADirectory
	}

	recursive := true
	if req.Recursive != nil {
		recursive = *req.Recursive
	}

	pattern := req.Pattern
	if pattern == "" {
		pattern = "*"
	}

	// Check for nested paths with recursive watches
	allDirs, err := s.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list watch directories: %w", err)
	}

	for _, dir := range allDirs {
		// Skip non-recursive directories - they don't cause conflicts
		if !dir.Recursive.Valid || !dir.Recursive.Bool {
			continue
		}

		// Check if new path is inside existing recursive directory
		if isPathInside(req.Path, dir.Path) {
			return nil, ErrNestedPath
		}

		// Check if existing recursive directory is inside new path
		// (only matters if new path is also recursive)
		if recursive && isPathInside(dir.Path, req.Path) {
			return nil, ErrParentPathExists
		}
	}

	row, err := s.db.Queries.CreateWatchDirectory(ctx, sqlc.CreateWatchDirectoryParams{
		Path:      req.Path,
		Pattern:   pgtype.Text{String: pattern, Valid: true},
		Recursive: pgtype.Bool{Bool: recursive, Valid: true},
		Enabled:   pgtype.Bool{Bool: true, Valid: true},
		Priority:  pgtype.Int4{Int32: req.Priority, Valid: true},
	})
	if err != nil {
		return nil, err
	}

	// Add to running watcher if active
	if s.watcher.IsRunning() {
		if err := s.watcher.AddWatchLocked(req.Path, recursive); err != nil {
			logrus.WithError(err).WithField("path", req.Path).Warn("failed to add watch for new directory")
		}

		// Trigger async scan of existing files (don't block API response)
		go func() {
			logrus.WithField("path", req.Path).Info("starting initial scan for new watch directory")
			if err := s.watcher.ScanDirectory(context.Background(), req.Path, recursive); err != nil {
				logrus.WithError(err).WithField("path", req.Path).Warn("initial scan failed")
			}
		}()
	}

	dir := fromSQLC(row)
	return &dir, nil
}

// Update updates a watch directory
func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*WatchDirectory, error) {
	row, err := s.db.Queries.GetWatchDirectoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Use existing values if not provided
	pattern := row.Pattern
	if req.Pattern != nil {
		pattern = pgtype.Text{String: *req.Pattern, Valid: true}
	}

	recursive := row.Recursive
	if req.Recursive != nil {
		recursive = pgtype.Bool{Bool: *req.Recursive, Valid: true}
	}

	enabled := row.Enabled
	if req.Enabled != nil {
		enabled = pgtype.Bool{Bool: *req.Enabled, Valid: true}
	}

	priority := row.Priority
	if req.Priority != nil {
		priority = pgtype.Int4{Int32: *req.Priority, Valid: true}
	}

	updated, err := s.db.Queries.UpdateWatchDirectory(ctx, sqlc.UpdateWatchDirectoryParams{
		ID:        id,
		Pattern:   pattern,
		Recursive: recursive,
		Enabled:   enabled,
		Priority:  priority,
		Metadata:  row.Metadata,
	})
	if err != nil {
		return nil, err
	}

	dir := fromSQLC(updated)
	return &dir, nil
}

// Remove unregisters a watch directory
func (s *Service) Remove(ctx context.Context, id int64) error {
	// Get the path first to remove from watcher
	row, err := s.db.Queries.GetWatchDirectoryByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	// Remove from watcher if running
	if s.watcher.IsRunning() {
		if err := s.watcher.RemoveWatch(row.Path); err != nil {
			logrus.WithError(err).WithField("path", row.Path).Debug("failed to remove watch")
		}
	}

	_, err = s.db.Queries.DeleteWatchDirectory(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// fromSQLC converts sqlc WatchDirectory to API WatchDirectory
func fromSQLC(d sqlc.WatchDirectory) WatchDirectory {
	dir := WatchDirectory{
		ID:        d.ID,
		Path:      d.Path,
		CreatedAt: d.CreatedAt.Time,
	}
	if d.Pattern.Valid {
		dir.Pattern = d.Pattern.String
	}
	if d.Recursive.Valid {
		dir.Recursive = d.Recursive.Bool
	}
	if d.Enabled.Valid {
		dir.Enabled = d.Enabled.Bool
	}
	if d.Priority.Valid {
		dir.Priority = d.Priority.Int32
	}
	return dir
}

// isPathInside checks if child is inside parent (or is the same as parent)
// Both paths should be absolute and cleaned
func isPathInside(child, parent string) bool {
	// Clean and get absolute paths
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)

	// Same path - considered "inside" for our purposes
	if child == parent {
		return true
	}

	// Ensure parent ends with separator for prefix matching
	if !strings.HasSuffix(parent, string(filepath.Separator)) {
		parent += string(filepath.Separator)
	}

	return strings.HasPrefix(child+string(filepath.Separator), parent)
}
