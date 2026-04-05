package watcher

import (
	"errors"
	"path/filepath"
)

var (
	ErrEmptyPath = errors.New("path cannot be empty")
)

func normalizePath(path string) (string, error) {
	cleaned := filepath.Clean(path)
	if cleaned == "." {
		// empty path
		return "", ErrEmptyPath
	}
	abspath, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	return abspath, nil
}

func parentDir(path string) (string, error) {
	normalized, err := normalizePath(path)
	if err != nil {
		return "", err
	}
	pd := filepath.Dir(normalized)
	if pd == "." {
		return "", ErrEmptyPath
	}
	abspd, err := filepath.Abs(pd)
	if err != nil {
		return "", err
	}
	return abspd, nil
}
