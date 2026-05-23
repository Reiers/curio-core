// Package diskstash is a minimal local-disk implementation of
// curio/lib/paths.StashStore. Used by curio-core to satisfy the
// StashStore dependency in the upstream pdp.PDPService without
// pulling the full curio storage subsystem (which depends on the
// FFI-bound sealer paths).
//
// Layout: each stash file is `<dir>/<uuid>.tmp`. StashURL returns
// `file://<dir>/<uuid>.tmp`. The PDP-side dealdata.CustoreScheme
// override is applied by the caller before the URL hits the DB.
//
// This is the demo-shape implementation. Production deployments
// should swap in a real curio paths.LocalStore — but for "can a
// client upload a piece to curio-core and have it land on disk +
// SQLite", local files are exactly the right primitive.

package diskstash

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// Store is a local-disk StashStore. Construct with New.
type Store struct {
	dir string
}

// New creates the stash directory if it doesn't exist and returns
// a Store rooted at it.
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create stash dir %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// StashCreate creates a new stash file and invokes writeFunc with a
// *os.File pointing at it. The file is closed after writeFunc returns.
// maxSize is advisory; the demo store doesn't enforce a quota (the
// upstream PDP code checks size at a higher layer).
func (s *Store) StashCreate(ctx context.Context, maxSize int64, writeFunc func(f *os.File) error) (uuid.UUID, error) {
	_ = ctx
	id := uuid.New()
	path := filepath.Join(s.dir, id.String()+".tmp")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0o600)
	if err != nil {
		return uuid.Nil, fmt.Errorf("open stash file: %w", err)
	}
	defer f.Close()
	if err := writeFunc(f); err != nil {
		_ = os.Remove(path)
		return uuid.Nil, fmt.Errorf("writeFunc: %w", err)
	}
	return id, nil
}

// ServeAndRemove opens the stash file for reading. The returned
// ReadCloser deletes the file when the caller's Close is invoked.
// (Upstream: "Once the stash has been fully read, the stash file is
// automatically removed.")
func (s *Store) ServeAndRemove(ctx context.Context, id uuid.UUID) (io.ReadCloser, error) {
	_ = ctx
	path := filepath.Join(s.dir, id.String()+".tmp")
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open stash for serve: %w", err)
	}
	return &removeOnClose{f: f, path: path}, nil
}

// StashRemove deletes a stash file without serving it. Used on
// validation failures (e.g. CommP mismatch).
func (s *Store) StashRemove(ctx context.Context, id uuid.UUID) error {
	_ = ctx
	path := filepath.Join(s.dir, id.String()+".tmp")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil // idempotent
	}
	return err
}

// StashURL returns a file:// URL for the stash file. The PDP-side
// caller mutates the Scheme to "custore" before persisting; we just
// need to expose the path payload.
func (s *Store) StashURL(id uuid.UUID) (url.URL, error) {
	path := filepath.Join(s.dir, id.String()+".tmp")
	u := url.URL{Scheme: "file", Path: path}
	return u, nil
}

type removeOnClose struct {
	f    *os.File
	path string
}

func (r *removeOnClose) Read(p []byte) (int, error) { return r.f.Read(p) }
func (r *removeOnClose) Close() error {
	err := r.f.Close()
	if rerr := os.Remove(r.path); rerr != nil && !os.IsNotExist(rerr) {
		if err == nil {
			err = rerr
		}
	}
	return err
}
