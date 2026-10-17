// Package collab is the co-editing session service of the item editor: presence, soft locks and the relay of
// opaque Yjs updates between the editors of one item. The server never decodes an update: it authenticates,
// orders, stores and relays them, and arbitrates who seeds a part of the shared document first. Participants
// save the merged draft through the normal draft API. Protocol and payloads: docs/collaboration.md.
package collab

import (
	"context"
	"errors"
	"hash/fnv"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Limits and timings of the protocol.
const (
	// ClaimExpiry is how long a client may take to seed a part before another may claim it.
	ClaimExpiry = 15 * time.Second
	// ClientTimeout is how long a client may go without a heartbeat before it is dropped.
	ClientTimeout = 45 * time.Second
	// SessionIdle is how long a session without clients is kept.
	SessionIdle = 2 * time.Minute
	// CleanupSchedule is the schedule of the collab.cleanup job.
	CleanupSchedule = "@every 30s"

	MaxUpdateBytes  = 512 << 10
	MaxCompactBytes = 2560 << 10
	MaxClients      = 20
	// CompactCount and CompactBytes are the log size at which a client is asked to compact it.
	CompactCount = 300
	CompactBytes = 4 << 20

	// RightsRecheck is how often a client's rights are checked again (on its heartbeats and updates).
	RightsRecheck = 10 * time.Second
	// CompactWindow is how long a compaction request stays open.
	CompactWindow = 2 * time.Minute

	// HardCapCount and HardCapBytes bound an item's log: past them updates are refused (409 compact_required)
	// until a compaction.
	HardCapCount = 2000
	HardCapBytes = 32 << 20
	// PageUpdates and PageBytes bound the updates of a join answer and of a page of the log.
	PageUpdates = 200
	PageBytes   = 2 << 20
	// RateBurst and RateSustained limit a client's updates: a bucket of RateBurst refilled at RateSustained a second.
	RateBurst     = 50
	RateSustained = 20

	// DefaultMaxStreamsPerItem and DefaultMaxStreams cap the open event streams of one item and of one server.
	DefaultMaxStreamsPerItem = MaxClients
	DefaultMaxStreams        = 2000
	// MaxStreamsPerPrincipal caps the open streams of one principal on one item.
	MaxStreamsPerPrincipal = 6
	// DefaultFeedBudget is the memory a node spends on the caches of recent updates (feeds) of all items.
	DefaultFeedBudget = 32 << 20

	maxFocus = 256
	maxParts = 8
)

// JobName is the cleanup job.
const JobName = "collab.cleanup"

// Part names: "shared" and "l:<locale>".
const SharedPart = "shared"

// Palette is the fixed set of presence colors; a principal always gets the same one.
var Palette = [8]string{"#d9480f", "#2f9e44", "#1971c2", "#9c36b5", "#c2255c", "#0c8599", "#e67700", "#5f3dc4"}

var (
	clientRE = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	localeRE = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

// Content is what the service needs from the content service.
type Content interface {
	Get(ctx context.Context, id ids.UUID, o content.GetOptions) (*content.View, error)
	CanRead(ctx context.Context, id ids.UUID) (bool, error)
}

var _ Content = (*content.Service)(nil)

// Options configures New.
type Options struct {
	DB      store.DB
	Content Content
	Clock   clock.Clock
	IDs     *ids.Generator
	Log     *slog.Logger
	// MaxStreamsPerItem and MaxStreams cap open event streams (defaults 20 and 2000).
	MaxStreamsPerItem, MaxStreams int
	// FeedBudget bounds the bytes of the feeds of all items on this node (default 32 MB).
	FeedBudget int
}

// Service is the collaboration service. It is safe for concurrent use.
type Service struct {
	o   Options
	hub *hub

	mu        sync.Mutex
	lastCheck map[ids.UUID]time.Time // last log-size check per item, for compaction requests
	checked   map[string]time.Time   // when a client's rights were last checked
	buckets   map[string]*bucket     // update budgets of clients
}

// New returns the service. Call Start to subscribe to the notifications of other servers.
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("collab: DB is required")
	case o.Content == nil:
		return nil, errors.New("collab: Content is required")
	case o.Clock == nil || o.IDs == nil:
		return nil, errors.New("collab: Clock and IDs are required")
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.MaxStreamsPerItem <= 0 {
		o.MaxStreamsPerItem = DefaultMaxStreamsPerItem
	}
	if o.MaxStreams <= 0 {
		o.MaxStreams = DefaultMaxStreams
	}
	if o.FeedBudget <= 0 {
		o.FeedBudget = DefaultFeedBudget
	}
	s := &Service{o: o, lastCheck: map[ids.UUID]time.Time{}, checked: map[string]time.Time{}, buckets: map[string]*bucket{}}
	s.hub = newHub(s)
	return s, nil
}

// Participant is a presence entry.
type Participant struct {
	Client    string `json:"client"`
	Principal string `json:"principal"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Locale    string `json:"locale"`
	Focus     string `json:"focus"`
	Idle      bool   `json:"idle"`
	Edit      bool   `json:"edit"`
}

func participant(c store.CollabClient) Participant {
	return Participant{Client: c.Client, Principal: c.Principal, Name: c.Name, Color: c.Color, Locale: c.Locale,
		Focus: c.Focus, Idle: c.Idle, Edit: c.Edit}
}

func participants(cs []store.CollabClient) []Participant {
	out := make([]Participant, len(cs))
	for i, c := range cs {
		out[i] = participant(c)
	}
	return out
}

// colorOf is the stable color of a principal.
func colorOf(principal string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(principal))
	return Palette[h.Sum32()%uint32(len(Palette))]
}

// who identifies the principal acting in ctx: its actor string ("user:<id>") and a display name.
func who(ctx context.Context) (principal, name string) {
	p := access.PrincipalFrom(ctx)
	principal = p.Actor().String()
	if p != nil {
		name = p.Name
	}
	if name == "" {
		name = principal
	}
	return principal, name
}

// notify queues a collaboration notification in tx.
func notify(tx store.Tx, item ids.UUID, rest ...string) error {
	payload := item.String()
	for _, r := range rest {
		payload += "|" + r
	}
	return tx.Notify(store.CollabChannel, payload)
}

// Etag is the entity tag of an item's working state, as the management API's ETag header carries it.
func Etag(updatedAt time.Time) string { return `"` + content.StateOf(updatedAt) + `"` }
