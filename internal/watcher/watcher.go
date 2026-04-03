package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/sirupsen/logrus"

	"github.com/Coosis/personal-agent/internal/db"
	sqlc "github.com/Coosis/personal-agent/sqlc"
)

// Watcher handles file system watching and change detection
type Watcher struct {
	db       *db.DB
	fsnotify *fsnotify.Watcher
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
}

// NewWatcher creates a new file watcher
func NewWatcher(database *db.DB) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Watcher{
		db:       database,
		fsnotify: fsw,
		stopCh:   make(chan struct{}),
	}, nil
}

// Start starts the file watcher including startup scan
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return nil
	}

	// Perform startup scan to catch changes while offline
	logrus.Info("starting file watcher startup scan")
	if err := w.startupScan(ctx); err != nil {
		logrus.WithError(err).Warn("startup scan encountered errors")
	}

	// Add watch directories
	dirs, err := w.db.Queries.ListWatchDirectories(ctx)
	if err != nil {
		return fmt.Errorf("failed to list watch directories: %w", err)
	}

	for _, dir := range dirs {
		recursive := dir.Recursive.Valid && dir.Recursive.Bool
		logrus.WithFields(logrus.Fields{
			"path":      dir.Path,
			"recursive": recursive,
		}).Info("adding watch directory")
		if err := w.addWatchRecursive(dir.Path, recursive); err != nil {
			logrus.WithError(err).WithField("path", dir.Path).Warn("failed to watch directory")
		}
	}

	w.running = true

	// Start event loop
	go w.eventLoop(ctx)

	logrus.Info("file watcher started")
	return nil
}

// Stop stops the file watcher
func (w *Watcher) Stop() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.running {
		return nil
	}

	close(w.stopCh)
	w.running = false

	if err := w.fsnotify.Close(); err != nil {
		return fmt.Errorf("failed to close fsnotify watcher: %w", err)
	}

	logrus.Info("file watcher stopped")
	return nil
}

// AddWatchLocked adds a new directory to watch under the protection of mutex,
// also is exported for service to call
func (w *Watcher) AddWatchLocked(dirPath string, recursive bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.addWatchRecursive(dirPath, recursive)
}

// ScanDirectory performs an initial scan of a directory to find existing files.
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
		"path": dirPath,
		"found": len(foundPaths),
	}).Info("directory scan complete")

	return nil
}

// addWatch adds a single path to fsnotify, should be called with mutex held
func (w *Watcher) addWatch(path string) error {
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrPathDoesNotExist
		}
		return ErrFailedToStatPath
	}
	if !info.IsDir() {
		return ErrPathNotADirectory
	}

	if err := w.fsnotify.Add(path); err != nil {
		return fmt.Errorf("fsnotify add failed for %s: %w", path, err)
	}
	logrus.WithField("path", path).Info("added fsnotify watch")

	return nil
}

// addWatchRecursive recursively adds directories to watch
// by walking the directory given and calling `addWatch()`
// should be called with mutex held
func (w *Watcher) addWatchRecursive(path string, recursive bool) error {
	if err := w.addWatch(path); err != nil {
		return err
	}

	if !recursive {
		return nil
	}

	return filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && walkPath != path {
			if err := w.addWatch(walkPath); err != nil {
				logrus.WithError(err).WithField("path", walkPath).Debug("failed to add watch")
			}
		}
		return nil
	})
}

// RemoveWatch removes a directory from watching
func (w *Watcher) RemoveWatch(dirPath string) error {
	return w.fsnotify.Remove(dirPath)
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
		logrus.WithField("path", path).Info("detected new file during startup scan")
		return w.createFileEvent(ctx, path, "create", info)
	}

	// Check if modified (mtime only - fast, no I/O)
	// Worker will verify checksum to detect false positives
	if info.ModTime().After(existing.LastModified.Time) {
		logrus.WithField("path", path).Info("detected modified file during startup scan")
		return w.createFileEvent(ctx, path, "modify", info)
	}

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
	for path, doc := range existingDocs {
		if !foundPaths[path] {
			logrus.WithField("path", path).Info("detected deleted file during startup scan")
			if err := w.markDocumentDeleted(ctx, doc.ID); err != nil {
				logrus.WithError(err).WithField("path", path).Warn("failed to mark document as deleted")
			}
		}
	}

	return nil
}

// eventLoop handles fsnotify events
func (w *Watcher) eventLoop(ctx context.Context) {
	for {
		select {
		case <-w.stopCh:
			return
		case <-ctx.Done():
			return
		case event, ok := <-w.fsnotify.Events:
			if !ok {
				return
			}
			w.handleEvent(ctx, event)
		case err, ok := <-w.fsnotify.Errors:
			if !ok {
				return
			}
			logrus.WithError(err).Error("fsnotify error")
		}
	}
}

// handleEvent processes a single fsnotify event and calls `createFileEvent()` or `markDocumentDeleted()` accordingly
func (w *Watcher) handleEvent(ctx context.Context, event fsnotify.Event) {
	logrus.WithFields(logrus.Fields{
		"op":   event.Op.String(),
		"name": event.Name,
	}).Info("fsnotify event")

	switch {
	case event.Op&fsnotify.Create == fsnotify.Create:
		info, err := os.Stat(event.Name)
		if err != nil {
			logrus.WithError(err).WithField("path", event.Name).Debug("failed to stat file")
			return
		}

		if info.IsDir() {
			// New directory - add watcher if recursive
			if err := w.addWatchRecursive(event.Name, true); err != nil {
				logrus.WithError(err).WithField("path", event.Name).Debug("failed to watch new directory")
			}
			return
		}

		if err := w.createFileEvent(ctx, event.Name, "create", info); err != nil {
			logrus.WithError(err).WithField("path", event.Name).Warn("failed to create file event")
		}

	case event.Op&fsnotify.Write == fsnotify.Write:
		info, err := os.Stat(event.Name)
		if err != nil {
			return
		}
		if info.IsDir() {
			return
		}

		if err := w.createFileEvent(ctx, event.Name, "modify", info); err != nil {
			logrus.WithError(err).WithField("path", event.Name).Warn("failed to create file event")
		}

	case event.Op&fsnotify.Remove == fsnotify.Remove, event.Op&fsnotify.Rename == fsnotify.Rename:
		// Check if it's a document we know about
		doc, err := w.db.Queries.GetDocumentByPath(ctx, event.Name)
		if err != nil {
			return // Not tracked, ignore
		}

		if err := w.markDocumentDeleted(ctx, doc.ID); err != nil {
			logrus.WithError(err).WithField("path", event.Name).Warn("failed to mark document as deleted")
		}
	default:
		logrus.WithField("op", event.Op.String()).Debug("unhandled fsnotify event")
	}
}

// createFileEvent creates a file_event record.
// Note: Checksum is NOT computed here (avoid blocking fsnotify).
// The worker will compute checksum when processing the event.
func (w *Watcher) createFileEvent(
	ctx context.Context,
	path string,
	eventType string,
	info os.FileInfo,
) error {
	// Create the file event immediately (fast, non-blocking)
	// Worker will compute checksum and handle deduplication
	_, err := w.db.Queries.CreateFileEvent(ctx, sqlc.CreateFileEventParams{
		Path:      path,
		EventType: eventType,
		SizeBytes: pgtype.Int8{Int64: info.Size(), Valid: true},
		// Checksum is empty - worker will compute it
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

// markDocumentDeleted marks a document as deleted
func (w *Watcher) markDocumentDeleted(ctx context.Context, id int64) error {
	_, err := w.db.Queries.UpdateDocumentStatus(ctx, sqlc.UpdateDocumentStatusParams{
		ID:               id,
		ProcessingStatus: "deleted",
	})
	return err
}

// listAllDocuments returns all non-deleted documents indexed by path
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
		// Skip deleted documents
		if doc.ProcessingStatus == "deleted" {
			continue
		}
		result[doc.Path] = doc
	}

	return result, nil
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

// IsRunning returns whether the watcher is running
func (w *Watcher) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}
