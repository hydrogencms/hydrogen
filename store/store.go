// Package store defines the storage interfaces. Core never writes SQL: each database adapter (a module)
// implements these interfaces with its own queries and migrations, and storetest is the contract every adapter
// must pass.
package store

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
)

// ErrMultiNode is returned when configuration asks a single-node adapter for multi-node behaviour.
var ErrMultiNode = errors.New("store: this database adapter supports a single node only")

// Opener opens a database from a URL whose scheme the adapter registered (e.g. "sqlite", "postgres").
type Opener func(ctx context.Context, url string, o OpenOptions) (DB, error)

type OpenOptions struct {
	Clock clock.Clock
	Log   *slog.Logger
}

type Capabilities struct {
	// MultiNode: several servers can share the database (cross-process notifications and locks).
	MultiNode bool
}

type DB interface {
	Dialect() string
	Capabilities() Capabilities

	// Tx runs fn in a read-write transaction, committing if fn returns nil. Notifications sent with
	// Tx.Notify are delivered only after a successful commit.
	Tx(ctx context.Context, fn func(Tx) error) error
	// View runs fn in a read-only transaction.
	View(ctx context.Context, fn func(Tx) error) error
	// Read returns read-only statements that run without a transaction (autocommit), for hot read paths of
	// one or two queries: a BEGIN/COMMIT pair costs two extra round trips on Postgres. Conversions are those of
	// Tx.SQL. Each statement sees its own snapshot, so use View when several statements must agree. Exec
	// returns an error. A write committed by Tx is visible to every Read that starts after it returns.
	Read() SQL
	// Reader serves content reads for delivery without a transaction.
	Reader() Reader

	Notifier() Notifier
	Locker() Locker

	// Migrate applies the adapter's core migrations, then each set in order. Applied migrations are
	// recorded with a checksum; an edited migration fails.
	Migrate(ctx context.Context, sets []MigrationSet) error
	// Pending lists migrations not yet applied.
	Pending(ctx context.Context, sets []MigrationSet) ([]Migration, error)

	Ping(ctx context.Context) error
	Close() error
}

type Tx interface {
	Outbox() OutboxStore
	Audit() AuditStore
	Content() ContentStore
	Auth() AuthStore
	Preview() PreviewStore
	Releases() ReleaseStore
	Reviews() ReviewStore
	Collab() CollabStore
	// SQL runs a module's own statements in this transaction.
	SQL() SQL
	// Notify queues a notification on channel, sent after commit.
	Notify(channel, payload string) error
}

type Notification struct {
	Channel string
	Payload string
}

// Notifier delivers notifications sent by committed transactions, across processes when the adapter is
// multi-node. Delivery may have gaps, so listeners re-check state instead of relying on every message:
// a notification with an empty payload means "anything may have changed" (clear everything, re-scan). It is
// sent after a reconnect, and to a listener whose buffer overflowed (a slow consumer): the adapter never drops
// silently, it marks the listener and sends the empty notification as soon as there is room, and
// notifications that arrive before then are not queued.
type Notifier interface {
	// Listen returns a channel that is closed when ctx ends.
	Listen(ctx context.Context, channel string) (<-chan Notification, error)
}

type Locker interface {
	// TryLock takes a named lock without waiting. ok is false when someone else holds it.
	TryLock(ctx context.Context, name string) (l Lock, ok bool, err error)
}

type Lock interface {
	Release(ctx context.Context) error
}

type OutboxEvent struct {
	ID        int64
	Topic     string
	Payload   []byte
	CreatedAt time.Time
}

type Delivery struct {
	Event    OutboxEvent
	Consumer string
	Attempts int // including this one
}

// OutboxStore keeps events and one delivery per subscribed consumer, so consumers retry independently.
type OutboxStore interface {
	Append(ctx context.Context, topic string, payload []byte, consumers []string, at time.Time) (int64, error)
	// Claim leases up to limit due deliveries for consumer until now+lease and counts the attempt.
	Claim(ctx context.Context, consumer string, now time.Time, lease time.Duration, limit int) ([]Delivery, error)
	Complete(ctx context.Context, consumer string, eventID int64, at time.Time) error
	// Retry makes a delivery due again at retryAt.
	Retry(ctx context.Context, consumer string, eventID int64, retryAt time.Time, reason string) error
	// Fail gives up on a delivery.
	Fail(ctx context.Context, consumer string, eventID int64, at time.Time, reason string) error
	Stats(ctx context.Context, consumer string) (OutboxStats, error)
}

type OutboxStats struct {
	Pending, Done, Failed int
}

type AuditEvent struct {
	ID        int64
	At        time.Time
	ActorKind string // user, agent, system
	ActorID   string
	Action    string
	Subject   string
	Data      []byte // JSON
}

type AuditQuery struct {
	Subject   string
	Action    string // a trailing "*" matches by prefix
	ActorKind string
	ActorID   string
	Since     time.Time // zero: no lower bound
	Before    int64     // when > 0, only events with a smaller id (a paging cursor)
	Limit     int       // newest first; 0 means 100
	Offset    int
}

// ActionMatch splits the action filter: a trailing "*" makes it a prefix match.
func (q AuditQuery) ActionMatch() (exact, prefix string) {
	if p, ok := strings.CutSuffix(q.Action, "*"); ok {
		return "", p
	}
	return q.Action, ""
}

type AuditStore interface {
	Write(ctx context.Context, e AuditEvent) (int64, error)
	List(ctx context.Context, q AuditQuery) ([]AuditEvent, error)
}
