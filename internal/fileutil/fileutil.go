package fileutil

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"path/filepath"
)

func DetectMimeType(path string) string {
	if kind := mime.TypeByExtension(filepath.Ext(path)); kind != "" {
		return kind
	}
	return "application/octet-stream"
}

func SHA256String(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func SHA256Reader(r io.Reader) (string, int64, []byte, error) {
	hasher := sha256.New()
	data, err := io.ReadAll(io.TeeReader(r, hasher))
	if err != nil {
		return "", 0, nil, err
	}
	return hex.EncodeToString(hasher.Sum(nil)), int64(len(data)), data, nil
}
