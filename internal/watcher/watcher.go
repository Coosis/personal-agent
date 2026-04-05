package watcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

var (
	ErrWatcherNotRunning = errors.New("watcher is not running")
)

// Watcher handles file system watching and change detection
type Watcher struct {
	stop     chan<- struct{}
	fsevents <-chan FSChange
	session  *Session
	mu       sync.Mutex
	db       *db.DB
}

const (
	FSOpCreate = "create"
	FSOpModify = "modify"
	FSOpDelete = "delete"
)

type FSChange struct {
	Op        string // should only be create, delete, or modify
	Path      string
	IsDir     bool
	SizeBytes *int64
}

// NewWatcher creates a new file watcher
func NewWatcher(database *db.DB) (*Watcher, error) {
	return &Watcher{
		db: database,
	}, nil
}

// if already have a session -> no-op
// otherwise:
// 1. creates a new session if not present
// 2. spawn a goroutine to run the new session's event loop
// 3. spawn a goroutine to handle fs events and create file events in db
// 4. performs startup scan
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.session != nil {
		logrus.Info("watcher session already running, skipping start")
		w.mu.Unlock()
		return nil
	}
	stop := make(chan struct{})
	fsevents := make(chan FSChange)
	session, err := NewWatchSession(stop, fsevents)
	if err != nil {
		w.mu.Unlock()
		return err
	}
	w.session = session
	w.stop = stop
	w.fsevents = fsevents
	go w.handle_fs_events(ctx, stop, fsevents)
	go session.eventLoop()
	w.mu.Unlock()

	// Perform startup scan to catch changes while offline
	logrus.Info("starting file watcher startup scan")
	if err := w.startupScan(ctx); err != nil {
		logrus.WithError(err).Warn("startup scan encountered errors")
	}

	// Clean up old processed file events
	if err := w.cleanupOldFileEvents(ctx); err != nil {
		logrus.WithError(err).Warn("failed to cleanup old file events")
	}

	// Add watch directories
	dirs, err := w.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		w.Stop()
		return ErrFailedToListDirectories
	}

	for _, dir := range dirs {
		recursive := dir.Recursive.Valid && dir.Recursive.Bool
		logrus.WithFields(logrus.Fields{
			"path":      dir.Path,
			"recursive": recursive,
		}).Info("adding watch directory")
		err := session.AddWatchRecursive(dir.Path, recursive)
		if err != nil && !errors.Is(err, ErrWatcherNotRunning) {
			logrus.WithError(err).WithField("path", dir.Path).Warn("failed to watch directory")
		}
	}

	logrus.Info("file watcher started")
	return nil
}

func (w *Watcher) Stop() {
	w.mu.Lock()
	session := w.session
	stop := w.stop
	w.session = nil
	w.stop = nil
	w.fsevents = nil
	w.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if session != nil {
		err := session.Stop()
		if err != nil {
			logrus.WithError(err).Warn("error stopping watcher session")
		}
	}
}

func (w *Watcher) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.session != nil
}

func (w *Watcher) handle_fs_events(
	ctx context.Context,
	stop <-chan struct{},
	c <-chan FSChange,
) {
	for {
		select {
		case <-stop:
			return
		case event, ok := <-c:
			if !ok {
				return
			}
			if err := w.handleEvent(ctx, event); err != nil {
				logrus.WithError(err).WithFields(logrus.Fields{
					"path": event.Path,
					"type": event.Op,
				}).Warn("failed to handle fs change event")
			}
		}
	}
}

func (w *Watcher) handleEvent(ctx context.Context, event FSChange) error {
	switch event.Op {
	case FSOpDelete:
		if event.IsDir {
			return w.handleDirectoryDelete(ctx, event.Path)
		}
		// Check if it's a document we know about
		_, err := w.db.Queries.GetDocumentByPath(ctx, event.Path)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				logrus.WithField("path", event.Path).Debug("ignoring delete event for unknown file")
				return nil
			}
			return fmt.Errorf("failed to query document by path: %w", err)
		}
		if err := w.createFileEvent(ctx, event.Path, event.Op, nil); err != nil {
			return err
		}
	case FSOpCreate:
		if event.IsDir {
			pd, err := w.findWatchedAncestor(ctx, event.Path)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					// no ancestor being watched found, usually doesn't happen
					logrus.WithField("path", event.Path).Debug("created/modified directory has no watched ancestor, ignoring")
					return nil
				}
				return fmt.Errorf("failed to find watched ancestor: %w", err)
			}
			if pd.Recursive.Valid && pd.Recursive.Bool {
				// recursive
				if err := w.AddWatch(event.Path, true); err != nil && !errors.Is(err, ErrWatcherNotRunning) {
					return fmt.Errorf("failed to add recursive watch for new dir: %w", err)
				}
			}
			// non-recursive - no need to add watch
			return nil
		}
		return w.handleFileCreateOrModify(ctx, event.Path, event.Op, event.SizeBytes)
	case FSOpModify:
		if event.IsDir {
			return nil
		}
		return w.handleFileCreateOrModify(ctx, event.Path, event.Op, event.SizeBytes)
	default:
		logrus.WithField("op", event.Op).Debug("unhandled fs change event")
	}
	return nil
}

// AddWatch adds a new directory to the active watcher session.
func (w *Watcher) AddWatch(dirPath string, recursive bool) error {
	w.mu.Lock()
	session := w.session
	w.mu.Unlock()
	if session == nil {
		return ErrWatcherNotRunning
	}
	return session.AddWatchRecursive(dirPath, recursive)
}

// RemoveWatch removes a directory from watching
func (w *Watcher) RemoveWatch(dirPath string) error {
	w.mu.Lock()
	session := w.session
	w.mu.Unlock()
	if session == nil {
		return ErrWatcherNotRunning
	}
	return session.RemoveWatch(dirPath)
}

func (w *Watcher) RemoveWatchTree(dirPath string, recursive bool) error {
	w.mu.Lock()
	session := w.session
	w.mu.Unlock()
	if session == nil {
		return ErrWatcherNotRunning
	}
	return session.RemoveWatchTree(dirPath, recursive)
}

// ---------------------------------------------------------------------------------------

// ScanDirectory performs an initial scan of a directory to find existing files,
// called by the service layer.
// This is called asynchronously when a new watch directory is added.
// Does NOT need mutex - only reads/writes DB, not watcher state.
func (w *Watcher) ScanDirectory(ctx context.Context, dirPath string, recursive bool) error {
	// Get existing documents for comparison
	existingDocs, err := w.listAllDocuments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list documents: %w", err)
	}

	foundPaths := make(map[string]bool)

	// Get watch directory config from DB
	watchDir, err := w.db.Queries.GetWatchDirectoryByPath(ctx, dirPath)
	if err != nil {
		// Use defaults if not found
		watchDir = sqlc.WatchDirectory{
			Path:      dirPath,
			Recursive: pgtype.Bool{Bool: recursive, Valid: true},
			Pattern:   pgtype.Text{String: "*", Valid: true},
		}
	}

	logrus.WithFields(logrus.Fields{
		"path":      dirPath,
		"recursive": recursive,
	}).Info("scanning directory for existing files")

	if err := w.scanDirectory(ctx, dirPath, recursive, watchDir.Pattern, existingDocs, foundPaths); err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"path":  dirPath,
		"found": len(foundPaths),
	}).Info("directory scan complete")

	return nil
}

// startupScan scans all watch directories and compares with database
func (w *Watcher) startupScan(ctx context.Context) error {
	dirs, err := w.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		return fmt.Errorf("failed to list watch directories: %w", err)
	}

	// Build set of existing documents by path
	existingDocs, err := w.listAllDocuments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list documents: %w", err)
	}

	foundPaths := make(map[string]bool)

	// Scan each watch directory
	for _, dir := range dirs {
		logrus.WithField("path", dir.Path).Debug("scanning directory")

		recursive := dir.Recursive.Valid && dir.Recursive.Bool
		if err := w.scanDirectory(ctx, dir.Path, recursive, dir.Pattern, existingDocs, foundPaths); err != nil {
			logrus.WithError(err).WithField("path", dir.Path).Warn("error scanning directory")
		}
	}

	// Check for deleted files
	for path, _ := range existingDocs {
		if !foundPaths[path] {
			logrus.WithField("path", path).Info("detected deleted file during startup scan")
			if err := w.createFileEvent(ctx, path, "delete", nil); err != nil {
				logrus.WithError(err).WithField("path", path).Warn("failed to create delete file event")
			}
		}
	}

	return nil
}

// scanDirectory scans a directory, respecting the recursive setting
func (w *Watcher) scanDirectory(
	ctx context.Context,
	dirPath string,
	recursive bool,
	pattern pgtype.Text,
	existingDocs map[string]sqlc.Document,
	foundPaths map[string]bool,
) error {
	getPattern := func() string {
		if pattern.Valid && pattern.String != "" {
			return pattern.String
		}
		return "*"
	}

	if recursive {
		// Use filepath.Walk for recursive scanning
		return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				logrus.WithError(err).WithField("path", path).Debug("error accessing path")
				return nil // Continue walking
			}

			// Skip directories
			if info.IsDir() {
				return nil
			}

			// Check pattern match
			if !matchGlob(filepath.Base(path), getPattern()) {
				return nil
			}

			foundPaths[path] = true
			return w.processFile(ctx, path, info, existingDocs)
		})
	}

	// Non-recursive: only scan top-level directory
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // Skip subdirectories
		}

		path := filepath.Join(dirPath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			logrus.WithError(err).WithField("path", path).Debug("failed to get file info")
			continue
		}

		// Check pattern match
		if !matchGlob(entry.Name(), getPattern()) {
			continue
		}

		foundPaths[path] = true
		if err := w.processFile(ctx, path, info, existingDocs); err != nil {
			logrus.WithError(err).WithField("path", path).Debug("failed to process file")
		}
	}

	return nil
}

// ---------------------------------------------------------------------------------------

// processFile checks if a file is new or modified and creates an event
// Uses only mtime comparison (fast) - worker will do checksum verification
func (w *Watcher) processFile(
	ctx context.Context,
	path string,
	info os.FileInfo,
	existingDocs map[string]sqlc.Document,
) error {
	existing, ok := existingDocs[path]
	if !ok {
		// New file
		logrus.WithField("path", path).Info("detected new file during scan")
		return w.createFileEvent(ctx, path, "create", sizeBytesFromInfo(info))
	}

	// Check if modified (mtime only - fast, no I/O)
	// Worker will verify checksum to detect false positives
	if info.ModTime().After(existing.LastModified.Time) {
		logrus.WithField("path", path).Info("detected modified file during scan")
		return w.createFileEvent(ctx, path, "modify", sizeBytesFromInfo(info))
	}

	return nil
}

// creates a file_event record in db
// The worker will compute checksum when processing the event.
func (w *Watcher) createFileEvent(
	ctx context.Context,
	path string,
	eventType string,
	sizeBytes *int64,
) error {
	size := pgtype.Int8{}
	if sizeBytes != nil {
		size = pgtype.Int8{Int64: *sizeBytes, Valid: true}
	}
	// Create the file event immediately (fast, non-blocking)
	// Worker will compute checksum and handle deduplication
	_, err := w.db.Queries.CreateFileEvent(ctx, sqlc.CreateFileEventParams{
		Path:      path,
		EventType: eventType,
		SizeBytes: size,
	})

	if err != nil {
		return fmt.Errorf("failed to create file event: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"path": path,
		"type": eventType,
	}).Info("created file event")

	return nil
}

// listAllDocuments returns all documents indexed by path
func (w *Watcher) listAllDocuments(ctx context.Context) (map[string]sqlc.Document, error) {
	// Use ListDocuments with large limit to get all
	rows, err := w.db.Queries.ListDocuments(ctx, sqlc.ListDocumentsParams{
		Limit:  100000,
		Offset: 0,
	})
	if err != nil {
		return nil, err
	}

	result := make(map[string]sqlc.Document, len(rows))
	for _, doc := range rows {
		result[doc.Path] = doc
	}

	return result, nil
}

// cleanupOldFileEvents deletes processed file events older than 7 days
func (w *Watcher) cleanupOldFileEvents(ctx context.Context) error {
	err := w.db.Queries.DeleteOldProcessedFileEvents(ctx)
	if err != nil {
		return err
	}
	logrus.Debug("cleaned up old processed file events")
	return nil
}

func (w *Watcher) findWatchedAncestor(
	ctx context.Context,
	path string,
) (sqlc.WatchDirectory, error) {
	cur := filepath.Clean(path)
	for {
		dir, err := w.db.Queries.GetWatchDirectoryByPath(ctx, cur)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlc.WatchDirectory{}, err
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			return sqlc.WatchDirectory{}, pgx.ErrNoRows
		}
		cur = parent
	}
}

func (w *Watcher) handleFileCreateOrModify(
	ctx context.Context,
	path string,
	op string,
	sizeBytes *int64,
) error {
	pd, err := w.findWatchedAncestor(ctx, path)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			logrus.WithField("path", path).Debug("file has no watched ancestor, ignoring")
			return nil
		}
		return fmt.Errorf("failed to find watched ancestor: %w", err)
	}

	pat := "*"
	if pd.Pattern.Valid && pd.Pattern.String != "" {
		pat = pd.Pattern.String
	}
	if !matchGlob(filepath.Base(path), pat) {
		logrus.WithFields(logrus.Fields{
			"path":    path,
			"pattern": pat,
		}).Debug("file does not match watch pattern, ignoring")
		return nil
	}

	return w.createFileEvent(ctx, path, op, sizeBytes)
}

func (w *Watcher) handleDirectoryDelete(ctx context.Context, dirPath string) error {
	if err := w.removeWatchDirectoriesForDeletedPath(ctx, dirPath); err != nil {
		return err
	}

	docs, err := w.db.Queries.ListDocumentsUnderPath(ctx, sqlc.ListDocumentsUnderPathParams{
		Path:           dirPath,
		SubtreePattern: subtreeLikePattern(dirPath),
	})
	if err != nil {
		return fmt.Errorf("failed to list documents under deleted directory: %w", err)
	}

	for _, doc := range docs {
		if err := w.createFileEvent(ctx, doc.Path, FSOpDelete, nil); err != nil {
			return err
		}
	}

	return nil
}

func (w *Watcher) removeWatchDirectoriesForDeletedPath(ctx context.Context, dirPath string) error {
	return w.db.WithTx(ctx, func(q *sqlc.Queries) error {
		watchDirs, err := q.ListWatchDirectoriesUnderPath(ctx, sqlc.ListWatchDirectoriesUnderPathParams{
			Path:           dirPath,
			SubtreePattern: subtreeLikePattern(dirPath),
		})
		if err != nil {
			return fmt.Errorf("failed to list watch directories under deleted path: %w", err)
		}

		for _, watchDir := range watchDirs {
			if _, err := q.DeleteWatchDirectory(ctx, watchDir.ID); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return fmt.Errorf("failed to delete watch directory %s: %w", watchDir.Path, err)
			}
		}

		return nil
	})
}

func sizeBytesFromInfo(info os.FileInfo) *int64 {
	if info == nil || info.IsDir() {
		return nil
	}
	size := info.Size()
	return &size
}

// matchGlob performs simple glob matching
func matchGlob(name, pattern string) bool {
	// Very simple glob: only supports * at start, end, or both
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(name, pattern[1:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(name, pattern[1:])
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, pattern[:len(pattern)-1])
	}
	return name == pattern
}
