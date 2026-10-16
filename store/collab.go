package store

import (
	"context"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
)

// CollabChannel carries collaboration notifications, sent with Tx.Notify after the transaction that changed the
// state commits. Payloads are '|'-separated; the first field is always the item id (canonical UUID text):
//
//	<item>|update|<seq>            an update was appended (read it with UpdatesSince)
//	<item>|presence                the client list changed (join, leave, heartbeat change, stale removal)
//	<item>|saved|<client>|<etag>|<seq>  a participant saved the draft (client is its collab client id; etag is the
//	                               state the write produced; seq the highest update it had applied, 0 when unknown)
//	<item>|reset|<reason>          reason is external, expired or trashed: the log was dropped, clients re-join
//	<item>|compact|<client>|<upto> asks <client> to post a compacted state of seq <= upto
//
// The store itself sends none of these (the collab service decides when); the constant and the formats live here
// so every node and the store agree. Client ids, etags and reasons never contain '|'. An empty payload means
// "anything may have changed" (see Notifier): listeners re-read their sessions.
const CollabChannel = "hydrogen_collab"

// CollabUpdate is one opaque Yjs update of an item's session log.
//
// Ordering contract: Seq is strictly increasing per item in commit order, and a reader never observes seq n+1
// before n is visible (no gap that fills later); seqs missing for good (rolled back appends, compacted ranges)
// are possible, so readers track "last seen seq", not "count". Order across items is unspecified.
type CollabUpdate struct {
	Seq    int64 // set by AppendUpdate
	Item   ids.UUID
	Client string
	Data   []byte
	Init   []string // parts this update seeded; none for ordinary updates
	At     time.Time
}

// CollabPart is the seeding state of one part ("shared", "l:<locale>") of an item's session.
type CollabPart struct {
	Item      ids.UUID
	Part      string
	ClaimedBy string
	ClaimedAt time.Time
	Seeded    bool
}

// CollabClaim is the outcome of ClaimParts, partitioning the requested parts.
type CollabClaim struct {
	Claimed []string // this client must seed these (includes claims it already held and takeovers of expired ones)
	Pending []string // being seeded by another client under a live claim
	Seeded  []string // already seeded; nothing to do
}

// CollabSession is the per-item session row: the draft state the session's seeding is based on, and the
// outstanding compaction request.
type CollabSession struct {
	Item      ids.UUID
	Etag      string // the item's working state (content.StateOf) the session was seeded from; kept current by saves
	StartedAt time.Time
	// CompactClient is the client asked to post a compacted state of seq <= CompactUpto at CompactAt; "" when no
	// request is outstanding.
	CompactClient string
	CompactUpto   int64
	CompactAt     time.Time
}

// CollabClient is a participant of an item's session.
type CollabClient struct {
	Client    string // random id chosen by the server
	Item      ids.UUID
	Principal string // "kind:id"
	Name      string
	Locale    string
	Focus     string
	Idle      bool
	Edit      bool
	Color     string
	JoinedAt  time.Time
	SeenAt    time.Time
}

// CollabStore keeps the collaboration session state of items: the update log, part claims and clients.
// All of it is transient: it is deleted with the item (foreign keys cascade) and by DeleteSession.
//
// Every method that takes an item returns ErrNotFound when the item does not exist, if it would otherwise create
// rows. Methods are safe under concurrent transactions on every adapter.
type CollabStore interface {
	// AppendUpdate adds an update and returns its seq. Appends of one item are serialized (Postgres takes a
	// per-item transaction-scoped advisory lock, held until commit; SQLite has a single writer), which is what
	// makes seq order equal commit order. Keep transactions that append short.
	AppendUpdate(ctx context.Context, u CollabUpdate) (int64, error)
	// UpdatesSince returns the updates with seq > after in seq order; limit <= 0 means all.
	UpdatesSince(ctx context.Context, item ids.UUID, after int64, limit int) ([]CollabUpdate, error)
	// LastSeq is the highest seq of the item's log (0 when empty). After a Compact it is still the highest seq.
	LastSeq(ctx context.Context, item ids.UUID) (int64, error)
	// Compact replaces every update with seq <= upto by one state update that keeps seq = upto, so a reader that
	// has seen upto skips it and one that has not receives the state (applying it twice is harmless for Yjs). upto
	// is clamped to LastSeq; it returns the effective upto, or 0 when there was nothing to compact (no change).
	// The state's Init is the union of the replaced updates' Init.
	Compact(ctx context.Context, item ids.UUID, upto int64, data []byte, client string, at time.Time) (int64, error)
	// LockSession serializes the transactions that change one item's session until they commit (Postgres: the
	// per-item transaction-scoped advisory lock AppendUpdate and DeleteSession take; SQLite has a single writer, so
	// it does nothing). A transaction that touches several of the session's rows takes it first, so that it cannot
	// deadlock with DeleteSession, which takes it before deleting.
	LockSession(ctx context.Context, item ids.UUID) error
	// EnsureSession returns the item's session row, creating it with the given etag and start time when there is
	// none (created reports that); ErrNotFound when the item does not exist.
	EnsureSession(ctx context.Context, item ids.UUID, etag string, at time.Time) (s CollabSession, created bool, err error)
	// GetSession returns the session row; ErrNotFound when the item has none.
	GetSession(ctx context.Context, item ids.UUID) (CollabSession, error)
	// UpdateSession replaces the row's Etag and compaction request; ErrNotFound when the item has no session row.
	UpdateSession(ctx context.Context, s CollabSession) error
	// DeleteSession removes the item's updates, parts, clients and session row. Idempotent.
	DeleteSession(ctx context.Context, item ids.UUID) error
	// LogSize is the number of updates and their total data bytes.
	LogSize(ctx context.Context, item ids.UUID) (count int, bytes int64, err error)

	// ClaimParts claims, for client, each requested part that is unclaimed or whose unseeded claim is older than
	// expiry (claimed_at <= now - expiry), atomically per part: of concurrent claimers exactly one wins. A part
	// the client already holds under a live claim stays claimed and keeps its claimed_at (re-claiming never extends
	// a claim, so a client cannot hold a part forever); once the claim has expired the same client claims it
	// afresh. A seeded part is never claimed again.
	ClaimParts(ctx context.Context, item ids.UUID, parts []string, client string, now time.Time, expiry time.Duration) (CollabClaim, error)
	// MarkSeeded marks the parts claimed by client as seeded and returns those it marked; a part whose claim was
	// lost to another client, never claimed or already seeded is left alone and not returned. The caller posting
	// an init update should call it in the same transaction as AppendUpdate and abort when it returns fewer parts
	// than requested.
	MarkSeeded(ctx context.Context, item ids.UUID, parts []string, client string) ([]string, error)
	// Parts lists the item's part rows ordered by part.
	Parts(ctx context.Context, item ids.UUID) ([]CollabPart, error)
	// ReleaseClaims deletes the unseeded parts claimed by client so they can be claimed again at once.
	ReleaseClaims(ctx context.Context, item ids.UUID, client string) error
	// ReleaseExpiredClaims deletes unseeded claims with claimed_at <= before and returns the affected items.
	ReleaseExpiredClaims(ctx context.Context, before time.Time) ([]ids.UUID, error)

	// PutClient inserts or replaces a client (keyed by Client; JoinedAt is kept on replace).
	PutClient(ctx context.Context, c CollabClient) error
	// GetClient returns a client; ErrNotFound when unknown.
	GetClient(ctx context.Context, client string) (CollabClient, error)
	// SetClientEdit sets whether the client may post updates; ErrNotFound when the client is unknown.
	SetClientEdit(ctx context.Context, client string, edit bool) error
	// TouchClient sets the heartbeat time and the presence fields; ErrNotFound when the client is unknown.
	TouchClient(ctx context.Context, client string, locale, focus string, idle bool, at time.Time) error
	// RemoveClient deletes the client and releases its unseeded claims. Idempotent.
	RemoveClient(ctx context.Context, item ids.UUID, client string) error
	// Clients lists the item's clients by JoinedAt, then Client.
	Clients(ctx context.Context, item ids.UUID) ([]CollabClient, error)
	// RemoveStaleClients deletes clients with SeenAt < before (and their unseeded claims) and returns the
	// affected items, each once, in no particular order.
	RemoveStaleClients(ctx context.Context, before time.Time) ([]ids.UUID, error)
	// DropIdleSessions deletes (as DeleteSession) every session without clients whose last activity (latest
	// update At, part ClaimedAt or session start) is before the given time, and returns the items.
	DropIdleSessions(ctx context.Context, before time.Time) ([]ids.UUID, error)
}
