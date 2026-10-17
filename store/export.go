package store

import (
	"context"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
)

// ExportStore is the bulk read side of export and import. Every adapter's ContentStore implements it
// (reach it with ExportOf); writes during an import use the ordinary ContentStore methods (InsertItem,
// InsertVersion, PutDraftPart, AddSchemaRev, RecordContentMigration), which keep the ids and timestamps they
// are given.
type ExportStore interface {
	// SchemaRevisions lists every recorded schema revision, oldest first.
	SchemaRevisions(ctx context.Context) ([]SchemaRevision, error)
	// ItemsAfter returns up to limit items that are not in the trash with an id greater than after, in id order.
	// The zero id starts at the beginning. Trashed items are not exported.
	ItemsAfter(ctx context.Context, after ids.UUID, limit int) ([]Item, error)
	// VersionsOf returns every version of those items with its locales, ordered by item and version; with
	// publishedOnly, only the version each item currently has published (none for unpublished items).
	VersionsOf(ctx context.Context, items []ids.UUID, publishedOnly bool) ([]Version, error)
	// DraftPartsFor returns the draft parts of those items, ordered by item and part.
	DraftPartsFor(ctx context.Context, items []ids.UUID) ([]DraftPart, error)
	// ContentMigrationLog lists the applied content migrations by name.
	ContentMigrationLog(ctx context.Context) ([]AppliedContentMigration, error)
	// ItemTypes lists the distinct types of all items, trashed ones included, sorted.
	ItemTypes(ctx context.Context) ([]string, error)
	// CountItems returns the number of items that are not in the trash.
	CountItems(ctx context.Context) (int, error)
}

// SchemaRevision is a recorded compiled schema.
type SchemaRevision struct {
	Rev       int
	Hash      string
	Compiled  []byte
	CreatedAt time.Time
}

type AppliedContentMigration struct {
	Name      string
	AppliedAt time.Time
}

// ExportOf returns the bulk read side of the transaction's content store.
func ExportOf(tx Tx) (ExportStore, error) {
	es, ok := tx.Content().(ExportStore)
	if !ok {
		return nil, fmt.Errorf("store: this database adapter does not support export")
	}
	return es, nil
}

// ConsistentViewer is implemented by adapters whose plain View is weaker than a snapshot: ViewConsistent
// runs fn in a read-only transaction that sees one snapshot of the database (Postgres: repeatable read).
// Adapters whose View already is one (SQLite) need not implement it.
type ConsistentViewer interface {
	ViewConsistent(ctx context.Context, fn func(Tx) error) error
}

// ViewConsistent runs fn in a single-snapshot read transaction.
func ViewConsistent(ctx context.Context, db DB, fn func(Tx) error) error {
	if cv, ok := db.(ConsistentViewer); ok {
		return cv.ViewConsistent(ctx, fn)
	}
	return db.View(ctx, fn)
}
