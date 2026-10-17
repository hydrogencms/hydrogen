// Package media stores uploaded files and serves them.
//
// Files are content-addressed: the storage key of a file is the lowercase hex SHA-256 of its bytes, so the same
// file uploaded twice is stored once. Storage is a small interface with a local directory driver and an
// S3-compatible driver (AWS, MinIO, R2) that signs requests in-house (SigV4); the Service records every file in
// the media_upload table, validates uploads, serves GET /assets/{sha}/{name} and collects unreferenced files.
package media

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNotFound is returned by a Storage (and the Service) for a missing object.
var ErrNotFound = errors.New("media: not found")

// Info describes a stored object.
type Info struct {
	Size    int64
	MIME    string    // as given to Put; drivers that don't keep it (local) leave it empty
	ModTime time.Time // zero when unknown
}

// Entry is one object of a listing.
type Entry struct {
	Key     string
	Size    int64
	ModTime time.Time
}

// Storage keeps file bytes under flat keys (the SHA-256 hex of the content). Implementations must be safe for
// concurrent use.
type Storage interface {
	// Put stores size bytes from r under key, replacing an existing object atomically: readers see the old or
	// the new object, never a partial one. It fails when r yields a different number of bytes.
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	// Get opens the object. The caller closes the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, Info, error)
	// GetRange opens length bytes starting at offset (clamped to the object's end); Info describes the whole
	// object.
	GetRange(ctx context.Context, key string, offset, length int64) (io.ReadCloser, Info, error)
	Stat(ctx context.Context, key string) (Info, error)
	// Delete removes the object; a missing one is not an error.
	Delete(ctx context.Context, key string) error
	// List returns up to limit objects with a key that starts with prefix and sorts after `after`, in key order.
	List(ctx context.Context, prefix, after string, limit int) ([]Entry, error)
}

// validKey accepts the characters of a SHA-256 hex string and the few more a test might use.
func validKey(key string) bool {
	if len(key) < 4 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

func errBadKey(key string) error { return errors.New("media: invalid storage key " + quote(key)) }

func quote(s string) string {
	if len(s) > 40 {
		s = s[:40] + "..."
	}
	return `"` + s + `"`
}
