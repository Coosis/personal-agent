package watcher

import (
	"context"
	"errors"
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
	ErrNotFound                = errors.New("watch directory not found")
	ErrAlreadyExists           = errors.New("watch directory already exists")
	ErrNestedPath              = errors.New("path is nested within an existing recursive watch directory")
	ErrParentPathExists        = errors.New("a recursive watch directory already covers this path")
	ErrPathDoesNotExist        = errors.New("path does not exist")
	ErrPathNotADirectory       = errors.New("path is not a directory")
	ErrFailedToStatPath        = errors.New("failed to stat path")
	ErrFailedToListDirectories = errors.New("failed to list watch directories")
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
	s.watcher.Stop()
	return nil
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
	normalizedPath, err := normalizePath(req.Path)
	if err != nil {
		return nil, err
	}

	// Check if path already exists
	existing, err := s.db.Queries.GetWatchDirectoryByPath(ctx, normalizedPath)
	if err == nil && existing.ID != 0 {
		return nil, ErrAlreadyExists
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	// Check if path exists
	info, err := os.Stat(normalizedPath)
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
		return nil, ErrFailedToListDirectories
	}

	for _, dir := range allDirs {
		// Skip non-recursive directories - they don't cause conflicts
		if !dir.Recursive.Valid || !dir.Recursive.Bool {
			continue
		}

		// Check if new path is inside existing recursive directory
		if isPathInside(normalizedPath, dir.Path) {
			return nil, ErrNestedPath
		}

		// Check if existing recursive directory is inside new path
		// (only matters if new path is also recursive)
		if recursive && isPathInside(dir.Path, normalizedPath) {
			return nil, ErrParentPathExists
		}
	}

	row, err := s.db.Queries.CreateWatchDirectory(ctx, sqlc.CreateWatchDirectoryParams{
		Path:      normalizedPath,
		Pattern:   pgtype.Text{String: pattern, Valid: true},
		Recursive: pgtype.Bool{Bool: recursive, Valid: true},
		Priority:  pgtype.Int4{Int32: req.Priority, Valid: true},
		Metadata:  []byte("{}"),
	})
	if err != nil {
		return nil, err
	}

	s.syncRuntimeWatch(nil, &row)
	s.scheduleScan(row, true)

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

	priority := row.Priority
	if req.Priority != nil {
		priority = pgtype.Int4{Int32: *req.Priority, Valid: true}
	}

	if err := s.checkConflictsAgainstDirectories(ctx, id, row.Path, recursive.Valid && recursive.Bool); err != nil {
		return nil, err
	}

	updated, err := s.db.Queries.UpdateWatchDirectory(ctx, sqlc.UpdateWatchDirectoryParams{
		ID:        id,
		Pattern:   pattern,
		Recursive: recursive,
		Priority:  priority,
		Metadata:  row.Metadata,
	})
	if err != nil {
		return nil, err
	}

	s.syncRuntimeWatch(&row, &updated)
	s.scheduleScan(updated, shouldRescanAfterUpdate(row, updated))

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

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	q := s.db.Queries.WithTx(tx)

	_, err = q.DeleteWatchDirectory(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	remainingDirs, err := q.ListWatchDirectories(ctx)
	if err != nil {
		return err
	}

	if err := s.cleanupRemovedDirectoryData(ctx, q, row.Path, remainingDirs); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	s.syncRuntimeWatch(&row, nil)
	return nil
}

func (s *Service) cleanupRemovedDirectoryData(
	ctx context.Context,
	q *sqlc.Queries,
	dirPath string,
	remainingDirs []sqlc.WatchDirectory,
) error {
	docs, err := q.ListDocumentsUnderPath(ctx, sqlc.ListDocumentsUnderPathParams{
		Path:           dirPath,
		SubtreePattern: subtreeLikePattern(dirPath),
	})
	if err != nil {
		return err
	}

	for _, doc := range docs {
		if isPathCoveredByAnyWatch(doc.Path, remainingDirs) {
			continue
		}
		if _, err := q.DeleteDocument(ctx, doc.ID); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) checkConflictsAgainstDirectories(
	ctx context.Context,
	excludeID int64,
	path string,
	recursive bool,
) error {
	allDirs, err := s.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		return ErrFailedToListDirectories
	}

	for _, dir := range allDirs {
		if dir.ID == excludeID {
			continue
		}
		if !dir.Recursive.Valid || !dir.Recursive.Bool {
			continue
		}
		if isPathInside(path, dir.Path) {
			return ErrNestedPath
		}
		if recursive && isPathInside(dir.Path, path) {
			return ErrParentPathExists
		}
	}

	return nil
}

func (s *Service) syncRuntimeWatch(previous, current *sqlc.WatchDirectory) {
	if !s.watcher.IsRunning() {
		return
	}
	if previous != nil && current != nil && sameRuntimeConfig(*previous, *current) {
		return
	}

	if previous != nil {
		recursive := previous.Recursive.Valid && previous.Recursive.Bool
		if err := s.watcher.RemoveWatchTree(previous.Path, recursive); err != nil && !errors.Is(err, ErrWatcherNotRunning) {
			logrus.WithError(err).WithField("path", previous.Path).Warn("failed to remove watch directory from active session")
		}
	}

	if current != nil {
		recursive := current.Recursive.Valid && current.Recursive.Bool
		if err := s.watcher.AddWatch(current.Path, recursive); err != nil && !errors.Is(err, ErrWatcherNotRunning) {
			logrus.WithError(err).WithField("path", current.Path).Warn("failed to add watch directory to active session")
		}
	}
}

func sameRuntimeConfig(previous, current sqlc.WatchDirectory) bool {
	return previous.Path == current.Path &&
		previous.Recursive.Valid == current.Recursive.Valid &&
		previous.Recursive.Bool == current.Recursive.Bool
}

func (s *Service) scheduleScan(dir sqlc.WatchDirectory, force bool) {
	if !force || !s.watcher.IsRunning() {
		return
	}

	recursive := dir.Recursive.Valid && dir.Recursive.Bool
	go func() {
		logrus.WithField("path", dir.Path).Info("starting directory scan")
		if err := s.watcher.ScanDirectory(context.Background(), dir.Path, recursive); err != nil {
			logrus.WithError(err).WithField("path", dir.Path).Warn("directory scan failed")
		}
	}()
}

func shouldRescanAfterUpdate(previous, current sqlc.WatchDirectory) bool {
	if previous.Pattern.String != current.Pattern.String || previous.Pattern.Valid != current.Pattern.Valid {
		return true
	}
	if previous.Recursive.Bool != current.Recursive.Bool || previous.Recursive.Valid != current.Recursive.Valid {
		return true
	}
	return false
}

func isPathCoveredByAnyWatch(path string, dirs []sqlc.WatchDirectory) bool {
	for _, dir := range dirs {
		if isPathCoveredByWatch(path, dir) {
			return true
		}
	}
	return false
}

func isPathCoveredByWatch(path string, dir sqlc.WatchDirectory) bool {
	if !isPathInside(path, dir.Path) {
		return false
	}

	if !(dir.Recursive.Valid && dir.Recursive.Bool) {
		parent, err := parentDir(path)
		if err != nil || parent != dir.Path {
			return false
		}
	}

	pattern := "*"
	if dir.Pattern.Valid && dir.Pattern.String != "" {
		pattern = dir.Pattern.String
	}

	return matchGlob(filepath.Base(path), pattern)
}

func subtreeLikePattern(path string) string {
	return filepath.Clean(path) + string(filepath.Separator) + "%"
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
	if d.Priority.Valid {
		dir.Priority = d.Priority.Int32
	}
	return dir
}

// isPathInside checks if child is inside parent (or is the same as parent)
// Both paths should be absolute and cleaned
func isPathInside(child, parent string) bool {
	if normalizedChild, err := normalizePath(child); err == nil {
		child = normalizedChild
	} else {
		child = filepath.Clean(child)
	}
	if normalizedParent, err := normalizePath(parent); err == nil {
		parent = normalizedParent
	} else {
		parent = filepath.Clean(parent)
	}

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
