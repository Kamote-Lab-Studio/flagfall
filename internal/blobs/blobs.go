// Package blobs stores challenge files by SHA-256, on local disk or in an
// S3-compatible bucket (AWS S3, Cloudflare R2, MinIO, Backblaze B2, ...).
//
// Keys are lowercase hex SHA-256 digests, so a key always names the same
// bytes: storing twice is harmless and nothing is ever overwritten.
//
// The backend is chosen when the server starts (see FromConfig), not in the
// admin panel: switching it during an event would break every download.
package blobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// ErrNotFound means no blob has that key.
var ErrNotFound = errors.New("blob not found")

// ErrTooLarge means a blob went over its size limit.
var ErrTooLarge = errors.New("file is too large")

// keyRE is the only shape of key the stores accept.
var keyRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidKey reports whether key is a SHA-256 hex digest.
func ValidKey(key string) bool { return keyRE.MatchString(key) }

// backend is one kind of storage.
type backend interface {
	put(ctx context.Context, key string, r io.Reader, size int64) error
	open(ctx context.Context, key string) (io.ReadSeekCloser, error)
	exists(ctx context.Context, key string) (bool, error)
	check(ctx context.Context) error
}

// Store keeps blobs in a backend.
type Store struct {
	b     backend
	spool string // local folder for temporary copies while hashing

	// Kind is "local" or "s3"; Location says where, for the admin panel.
	Kind, Location string
	// Redirect, when set, makes downloads go straight to the bucket with a
	// short-lived signed URL instead of through the server.
	Redirect func(ctx context.Context, key, filename string) (string, error)
}

// Local stores blobs as files in dir.
func Local(dir string) *Store {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return &Store{b: localBackend{dir: dir}, spool: dir, Kind: "local", Location: abs}
}

// Put stores size bytes from r under key. The caller vouches that key is
// the SHA-256 of the bytes; use PutHashed when it isn't known.
func (s *Store) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if !ValidKey(key) {
		return errors.New("blobs: invalid key")
	}
	if ok, err := s.b.exists(ctx, key); err != nil || ok {
		return err
	}
	return s.b.put(ctx, key, r, size)
}

// PutBytes stores data under its SHA-256 and returns the key.
func (s *Store) PutBytes(ctx context.Context, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	key := hex.EncodeToString(sum[:])
	return key, s.Put(ctx, key, bytes.NewReader(data), int64(len(data)))
}

// PutHashed streams r (at most max bytes) into the store under its SHA-256.
// It copies r to a temporary file first, since the key must be known
// before the upload starts.
func (s *Store) PutHashed(ctx context.Context, r io.Reader, max int64) (key string, size int64, err error) {
	if err := os.MkdirAll(s.spool, 0o750); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(s.spool, ".upload-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name()) // no-op after a local rename
	defer tmp.Close()
	h := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r, max+1))
	if err != nil {
		return "", 0, err
	}
	if size > max {
		return "", 0, ErrTooLarge
	}
	key = hex.EncodeToString(h.Sum(nil))
	if ok, err := s.b.exists(ctx, key); err != nil || ok {
		return key, size, err
	}
	if lb, ok := s.b.(localBackend); ok {
		// Same disk: move the file into place instead of copying it.
		if err := tmp.Close(); err != nil {
			return "", 0, err
		}
		return key, size, lb.adopt(tmp.Name(), key)
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", 0, err
	}
	return key, size, s.b.put(ctx, key, tmp, size)
}

// Open returns the blob, or ErrNotFound.
func (s *Store) Open(ctx context.Context, key string) (io.ReadSeekCloser, error) {
	if !ValidKey(key) {
		return nil, ErrNotFound
	}
	return s.b.open(ctx, key)
}

// Exists reports whether a blob is stored under key.
func (s *Store) Exists(ctx context.Context, key string) (bool, error) {
	if !ValidKey(key) {
		return false, nil
	}
	return s.b.exists(ctx, key)
}

// Check makes sure the store can be reached, for a clear error at startup.
func (s *Store) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return s.b.check(ctx)
}

// ---------------------------------------------------------------- local

type localBackend struct{ dir string }

func (l localBackend) path(key string) string { return filepath.Join(l.dir, key) }

func (l localBackend) put(_ context.Context, key string, r io.Reader, _ int64) error {
	if err := os.MkdirAll(l.dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(l.dir, ".put-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return l.adopt(tmp.Name(), key)
}

func (l localBackend) adopt(tmp, key string) error {
	if err := os.Chmod(tmp, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, l.path(key))
}

func (l localBackend) open(_ context.Context, key string) (io.ReadSeekCloser, error) {
	f, err := os.Open(l.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	return f, err
}

func (l localBackend) exists(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(l.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// LocalDir is where local blobs live inside the data directory.
func LocalDir(dataDir string) string { return filepath.Join(dataDir, "files") }

func (l localBackend) check(context.Context) error {
	if err := os.MkdirAll(l.dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(l.dir, ".check-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}
