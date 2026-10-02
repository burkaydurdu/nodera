// Package storage keeps rendered Invoice PDFs behind a small object-store
// interface; the local-filesystem adapter serves dev and tests, a bucket
// adapter can replace it without the callers noticing.
package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrNotFound = errors.New("object not found")
	ErrBadKey   = errors.New("object key escapes the store")
)

type Store interface {
	Put(ctx context.Context, key string, data []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
}

const (
	dirMode  = 0o750
	fileMode = 0o600
)

// Local stores each object as a file under its directory; keys are
// slash-separated paths relative to it.
type Local struct {
	dir string
}

func NewLocal(dir string) *Local {
	return &Local{dir: dir}
}

func (l *Local) Put(_ context.Context, key string, data []byte) error {
	path, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	// A crash mid-write must not leave a truncated PDF under the final name.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, fileMode); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func (l *Local) Get(_ context.Context, key string) ([]byte, error) {
	path, err := l.path(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) // #nosec G304 -- path is confined to the store's directory by l.path
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}

	return data, err
}

func (l *Local) path(key string) (string, error) {
	root := filepath.Clean(l.dir)
	path := filepath.Join(root, filepath.FromSlash(key))
	if !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", ErrBadKey
	}

	return path, nil
}
