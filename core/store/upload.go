package store

import (
	"context"
	"time"
)

// Upload is the record of one stored file. Files are content-addressed: SHA is the lowercase hex SHA-256 of
// the bytes and the primary key. Width and Height are 0 for files that are not images.
type Upload struct {
	SHA       string
	MIME      string
	Size      int64
	Width     int
	Height    int
	CreatedBy string
	CreatedAt time.Time
	// TouchedAt is the last time the file was uploaded (an upload of a known file touches it). Garbage
	// collection only considers uploads not touched for a while.
	TouchedAt time.Time
}

// UploadStore keeps the upload records. The bytes live in the media storage, not here.
type UploadStore interface {
	// Put inserts the record, or touches the existing one (TouchedAt = u.TouchedAt) and leaves the rest as it
	// was. created is false when the file was already known.
	Put(ctx context.Context, u Upload) (created bool, err error)
	Get(ctx context.Context, sha string) (Upload, error) // ErrNotFound
	// List returns up to limit records with a SHA greater than after, in SHA order.
	List(ctx context.Context, after string, limit int) ([]Upload, error)
	// ListStale is List restricted to records last touched before the given time.
	ListStale(ctx context.Context, before time.Time, after string, limit int) ([]Upload, error)
	// Touch sets TouchedAt to at when that is later and reports whether the record exists: an item write that
	// attaches a file calls it in its transaction, so a concurrent collection cannot remove the file.
	Touch(ctx context.Context, sha string, at time.Time) (bool, error)
	// DeleteStale removes the record when it was last touched before the given time and reports whether it did:
	// an upload that touched it since keeps it.
	DeleteStale(ctx context.Context, sha string, before time.Time) (bool, error)
	// Delete removes the record; false when there was none.
	Delete(ctx context.Context, sha string) (bool, error)
}
