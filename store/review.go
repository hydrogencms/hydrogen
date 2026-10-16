package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
)

// Review storage API (H8b), reached through Tx.Reviews(). The review service owns the state machine; the store
// keeps one ItemReview row per item and an append-only event thread.
//
//   - State is free text to the store ("none", "in_review", "approved", "changes_requested"). Event kinds are
//     free text too.
//   - Get returns ErrNotFound when the item has no review row (the service treats that as state "none"). A row
//     with state "none" is allowed (closed review; the thread stays).
//   - Put upserts by item and writes every column. It returns ErrConstraint when the item does not exist.
//     Put is unconditional: use Transition for state changes that must not race.
//   - Transition writes the row only when the stored state is one of fromStates, in one statement (like
//     ReleaseStore.TransitionRelease): ErrNotFound when no row exists, ErrStateChanged when the state differs.
//     An absent row is not a state: to start a review from nothing use Put (racing submitters: Put a "none" row
//     first, which is idempotent through Get+Put, then Transition). Inside a transaction it locks the row.
//   - Delete removes the row (the events stay; they go with the item). Idempotent.
//   - AddEvent appends an event; ErrConstraint when the item does not exist, ErrDuplicateKey for a taken ID.
//     Events are never updated. Deleting (purging) an item removes its review and events.
//   - Events lists newest first (At desc, ID desc). Before is the ID of the last event of the previous page
//     (zero: from the newest); an unknown Before returns ErrNotFound. Limit <= 0 means 50; max 500.
//   - List returns review rows plus the item's type, newest submission first (SubmittedAt desc, Item desc);
//     rows of trashed items are left out. Item names are not stored here: the service resolves them from the
//     content store. The total counts all matches. A zero filter matches every row (including state "none").
//   - ReviewFilter.Reviewers matches rows whose Reviewers contains any of the given principal refs exactly
//     ("user:<id>", "group:<id>"); the caller passes the user's ref plus the refs of the user's groups.
//   - Times are millisecond precision, UTC. SubmittedAt/DecidedAt are nil when unset; an empty Reviewers list
//     is read back as nil.

// ItemReview is the review of an item's current draft.
type ItemReview struct {
	Item        ids.UUID
	State       string
	DraftHash   string // hash of the draft parts the review is about (set on submit/approve)
	SubmittedBy string // "kind:id"
	SubmittedAt *time.Time
	Reviewers   []string // "user:<id>" / "group:<id>"; informational
	DecidedBy   string
	DecidedAt   *time.Time
	UpdatedAt   time.Time
}

// ReviewEvent is one entry of an item's review thread. Data is an optional JSON document (nil: none).
type ReviewEvent struct {
	ID      ids.UUID // UUIDv7
	Item    ids.UUID
	Kind    string
	Comment string
	Actor   string // "kind:id"
	At      time.Time
	Data    json.RawMessage
}

// ReviewFilter narrows ListReviews; zero fields match everything.
type ReviewFilter struct {
	States    []string // any of these
	Types     []string // item types, any of these
	Reviewers []string // principal refs, any of these (exact match in the review's reviewers)
}

// ReviewRow is a listed review with its item's type.
type ReviewRow struct {
	ItemReview
	Type string
}

type ReviewStore interface {
	Get(ctx context.Context, item ids.UUID) (ItemReview, error)
	Put(ctx context.Context, r ItemReview) error
	Transition(ctx context.Context, r ItemReview, fromStates []string) error
	Delete(ctx context.Context, item ids.UUID) error
	AddEvent(ctx context.Context, e ReviewEvent) error
	Events(ctx context.Context, item ids.UUID, limit int, before ids.UUID) ([]ReviewEvent, error)
	List(ctx context.Context, f ReviewFilter, p Page) ([]ReviewRow, int, error)
}
