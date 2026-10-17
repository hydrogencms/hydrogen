package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
)

// Release storage decisions (H8a):
//   - Statuses and kinds are free text to the store; the release service owns the state machine.
//   - ErrNotFound: GetRelease/UpdateRelease/DeleteRelease of an unknown id. PutReleaseItem for a release or item
//     that does not exist returns ErrConstraint. RemoveReleaseItem is idempotent. CreateRelease with a taken id
//     returns ErrDuplicateKey.
//   - A release item has foreign keys to both the release and the item: purging an item removes it from every
//     release, deleting a release removes its items. Trashing an item keeps its release items.
//   - ListReleases is newest first (created_at, id descending); ReleaseItems is ordered by added_at, item id.
//   - Item names and types are not stored here; the service reads them from the content store.

// ErrStateChanged is returned by TransitionRelease when the release is not in one of the expected statuses (another
// node moved it first).
var ErrStateChanged = errors.New("store: state changed")

// Release groups item actions that are published together, now or at ScheduledAt.
type Release struct {
	ID          ids.UUID
	Name        string
	Description string
	Kind        string // "release" or "item" (a one-item schedule); free text to the store
	Status      string // open, scheduled, publishing, published, failed, cancelled
	ScheduledAt *time.Time
	PublishedAt *time.Time
	Errors      json.RawMessage // nil: none; otherwise a JSON document as stored
	CreatedBy   string          // "kind:id" of the actor
	UpdatedBy   string
	ScheduledBy string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ReleaseItem is one item's action in a release. Action is "publish" or "unpublish"; Locales limits a publish
// to those locales (empty: the default of the publish operation).
type ReleaseItem struct {
	ReleaseID ids.UUID
	ItemID    ids.UUID
	Action    string
	Locales   []string
	AddedBy   string
	AddedAt   time.Time
}

// ReleaseFilter narrows ListReleases; zero fields match everything.
type ReleaseFilter struct {
	Statuses []string // any of these
	Kind     string
	Item     ids.UUID // zero: any; otherwise only releases that contain this item
}

type ReleaseStore interface {
	CreateRelease(ctx context.Context, r Release) error
	// UpdateRelease writes every mutable column (everything but ID, CreatedAt, CreatedBy).
	UpdateRelease(ctx context.Context, r Release) error
	// TransitionRelease is UpdateRelease guarded by the current status: it writes only when the stored status is
	// one of from, in one statement, so two nodes racing for the same transition cannot both win (the loser
	// waits for the winner's transaction on Postgres, then sees ErrStateChanged). ErrNotFound: unknown id.
	// Inside a transaction it also locks the row until the transaction ends.
	TransitionRelease(ctx context.Context, r Release, from []string) error
	GetRelease(ctx context.Context, id ids.UUID) (Release, error)
	DeleteRelease(ctx context.Context, id ids.UUID) error // with its items
	ListReleases(ctx context.Context, f ReleaseFilter, p Page) ([]Release, int, error)
	PutReleaseItem(ctx context.Context, it ReleaseItem) error // upsert by (release, item)
	RemoveReleaseItem(ctx context.Context, release, item ids.UUID) error
	ReleaseItems(ctx context.Context, release ids.UUID) ([]ReleaseItem, error)
	// ReleaseItemCounts counts the items of many releases in one query; releases without items are absent.
	ReleaseItemCounts(ctx context.Context, releases []ids.UUID) (map[ids.UUID]int, error)
	// ReleaseItemsOfItem lists the item's entry in every release that has it (any order).
	ReleaseItemsOfItem(ctx context.Context, item ids.UUID) ([]ReleaseItem, error)
	// DueReleases lists releases with status "scheduled" and ScheduledAt <= now, oldest first.
	DueReleases(ctx context.Context, now time.Time, limit int) ([]Release, error)
}
