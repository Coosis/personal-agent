package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

// performance issues:
// 1. concurrently adding a directory and one of its deep subdirectories recursively,
// which results in two concurrent file system walks, one of which contains the other entirely

type Session struct {
	// protected fields:
	// file system watcher, all operations on this must have mutex on `Session` acquired
	fsw     *fsnotify.Watcher
	watched map[string]struct{} // set of currently watched paths
	mu      sync.Mutex

	// fields safe to access without lock:
	fsevents chan<- FSChange // used to publish events to watcher for processing

	stop <-chan struct{}
}

func NewWatchSession(
	stop <-chan struct{},
	fsevents chan<- FSChange,
) (*Session, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	return &Session{
		fsw:     fsw,
		watched: make(map[string]struct{}),

		fsevents: fsevents,
		stop:     stop,
	}, nil
}

func (s *Session) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fsw.Close()
}

func (s *Session) eventLoop() {
	for {
		select {
		case <-s.stop:
			return
		case event, ok := <-s.fsw.Events:
			if !ok {
				return
			}
			opType := OpType(event)

			if opType == FSOpCreate || opType == FSOpModify {
				info, err := os.Stat(event.Name)
				if err != nil {
					logrus.WithError(err).WithField("path", event.Name).Debug("failed to stat path for event")
					continue
				}
				fsc := FSChange{
					Op:        opType,
					Path:      event.Name,
					IsDir:     info.IsDir(),
					SizeBytes: sizeBytesFromInfo(info),
				}
				select {
				case <-s.stop:
					return
				case s.fsevents <- fsc:
				}
			} else if opType == FSOpDelete {
				isDir := s.forgetDeletedWatchPath(event.Name)
				fsc := FSChange{
					Op:        opType,
					Path:      event.Name,
					IsDir:     isDir,
					SizeBytes: nil,
				}
				select {
				case <-s.stop:
					return
				case s.fsevents <- fsc:
				}
			} else {
				logrus.WithField("event", event).Debug("ignoring unknown fsnotify event type")
			}
		case err, ok := <-s.fsw.Errors:
			if !ok {
				return
			}
			logrus.WithError(err).Error("fsnotify error")
		}
	}
}

func (s *Session) forgetDeletedWatchPath(path string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.watched[path]; !ok {
		return false
	}

	for watchedPath := range s.watched {
		if watchedPath == path || isPathInside(watchedPath, path) {
			delete(s.watched, watchedPath)
		}
	}

	return true
}

// safe to call concurrently
func (s *Session) AddWatch(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.watched[path]; ok {
		return nil
	}
	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrPathDoesNotExist
		}
		return ErrFailedToStatPath
	}
	if !info.IsDir() {
		return ErrPathNotADirectory
	}
	if err := s.fsw.Add(path); err != nil {
		return fmt.Errorf("fsnotify add failed for %s: %w", path, err)
	}
	s.watched[path] = struct{}{}
	logrus.WithField("path", path).Info("added fsnotify watch")

	return nil
}

func (s *Session) RemoveWatch(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.watched[path]; !ok {
		return nil
	}
	if err := s.fsw.Remove(path); err != nil {
		return fmt.Errorf("fsnotify remove failed for %s: %w", path, err)
	}
	delete(s.watched, path)
	logrus.WithField("path", path).Info("removed fsnotify watch")
	return nil
}

func (s *Session) RemoveWatchTree(dirPath string, recursive bool) error {
	if !recursive {
		return s.RemoveWatch(dirPath)
	}

	root := filepath.Clean(dirPath)

	s.mu.Lock()
	paths := make([]string, 0, len(s.watched))
	for watchedPath := range s.watched {
		if watchedPath == root || isPathInside(watchedPath, root) {
			paths = append(paths, watchedPath)
		}
	}
	s.mu.Unlock()

	sort.Slice(paths, func(i, j int) bool {
		return len(paths[i]) > len(paths[j])
	})

	for _, path := range paths {
		if err := s.RemoveWatch(path); err != nil {
			return err
		}
	}

	return nil
}

// Recursively adds directories to the session.
// Safe to call concurrently
func (s *Session) AddWatchRecursive(path string, recursive bool) error {
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

	if err := s.AddWatch(path); err != nil {
		return err
	}
	if !recursive {
		return nil
	}

	if err := filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && walkPath != path {
			if err := s.AddWatch(walkPath); err != nil {
				logrus.WithError(err).WithField("path", walkPath).Debug("failed to add watch for subdirectory during recursive add")
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func OpType(event fsnotify.Event) string {
	if event.Op&fsnotify.Create == fsnotify.Create {
		return FSOpCreate
	} else if event.Op&fsnotify.Write == fsnotify.Write {
		return FSOpModify
	} else if event.Op&fsnotify.Remove == fsnotify.Remove ||
		event.Op&fsnotify.Rename == fsnotify.Rename {
		return FSOpDelete
	} else {
		return "unknown"
	}
}
