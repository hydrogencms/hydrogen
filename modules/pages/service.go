package pages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// service holds the module's dependencies; Service delegates to it once the kernel has booted.
type service struct {
	sites  []string
	db     store.DB
	schema *schema.Schema
	clock  clock.Clock
	ids    *ids.Generator
	log    *slog.Logger
	audit  *audit.Log
	outbox *outbox.Outbox
	svcs   *kernel.Services

	mounts mountCache
	web    *webIndex
}

func newService(sites []string, s *kernel.Services) *service {
	var chans []*channel.Channel
	if s.Channels != nil {
		chans = s.Channels.All()
	}
	return &service{sites: slices.Clone(sites), db: s.DB, schema: s.Schema, clock: s.Clock, ids: s.IDs, log: s.Log,
		audit: s.Audit, outbox: s.Outbox, svcs: s, web: newWebIndex(chans, sites)}
}

func (s *Service) get() (*service, error) {
	if s.impl == nil {
		return nil, errors.New("pages: the module has not booted yet")
	}
	return s.impl, nil
}

func (s *service) checkSite(site string) error {
	if !slices.Contains(s.sites, site) {
		return notFound("site %q (sites: %v)", site, s.sites)
	}
	return nil
}

// record writes an audit entry for a node mutation and queues the page.changed event.
func (s *service) record(ctx context.Context, tx store.Tx, action string, n nodeRow, data map[string]any) error {
	if data == nil {
		data = map[string]any{}
	}
	data["site"] = n.Site
	err := s.audit.Write(ctx, tx, audit.Entry{Actor: audit.ActorFrom(ctx), Action: action, Subject: n.ID.String(), Data: data})
	if err != nil {
		return fmt.Errorf("pages: audit %s: %w", action, err)
	}
	return s.emitChanged(ctx, tx, n.Site, &n.ID)
}

// emitChanged queues page.changed so caches and frontends refresh navigation and routes.
func (s *service) emitChanged(ctx context.Context, tx store.Tx, site string, node *ids.UUID) error {
	payload := map[string]any{"site": site, "node": node}
	if _, err := s.outbox.Publish(ctx, tx, "page.changed", payload); err != nil {
		return fmt.Errorf("pages: outbox page.changed: %w", err)
	}
	return nil
}

func (s *service) isPageType(name string) bool {
	t, ok := s.schema.Type(name)
	return ok && t.Page
}

// change says that routes of a site changed, below a node when known.
type change struct {
	Site string
	Node *ids.UUID
}

// emitChanges queues one page.changed per distinct (site, node).
func (s *service) emitChanges(ctx context.Context, tx store.Tx, changes []change) error {
	for _, c := range dedupeChanges(changes) {
		if err := s.emitChanged(ctx, tx, c.Site, c.Node); err != nil {
			return err
		}
	}
	return nil
}

func dedupeChanges(changes []change) []change {
	var out []change
	for _, c := range changes {
		if !slices.ContainsFunc(out, func(o change) bool { return o.Site == c.Site && sameOwner(o.Node, c.Node) }) {
			out = append(out, c)
		}
	}
	return out
}

// lockSite serialises structural writes of a site (place, move, mount and link changes, route sync) so that
// the checks made before a write still hold when it happens. Postgres takes a transaction-scoped advisory
// lock, which the transaction releases at its end; callers re-read what they checked earlier once they hold
// it. SQLite has a single writer.
func (s *service) lockSite(ctx context.Context, tx store.Tx, site string) error {
	if s.db.Dialect() != "postgres" {
		return nil
	}
	if _, err := tx.SQL().Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "pages:"+site); err != nil {
		return fmt.Errorf("pages: lock site %q: %w", site, err)
	}
	return nil
}

// lockSites locks several sites in name order, so concurrent transactions cannot deadlock on each other.
func (s *service) lockSites(ctx context.Context, tx store.Tx, sites []string) error {
	for _, site := range slices.Sorted(slices.Values(sites)) {
		if err := s.lockSite(ctx, tx, site); err != nil {
			return err
		}
	}
	return nil
}

// mountCacheTTL bounds how long another node's mount changes can go unseen by the publish hooks. Mount
// creation rebuilds the memberships of the collection's published items and the reconcile consumer re-checks
// every publish, so a stale read is repaired; this node's own changes invalidate the cache at once.
const mountCacheTTL = 2 * time.Second

// mountCache holds the mounts of a collection (every site) for the hooks of items published in a burst.
type mountCache struct {
	mu      sync.Mutex
	gen     uint64
	entries map[string]mountEntry
}

type mountEntry struct {
	set     *mountSet
	expires time.Time
}

func (c *mountCache) get(collection string, now time.Time) (*mountSet, uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[collection]; ok && now.Before(e.expires) {
		return e.set, c.gen, true
	}
	return nil, c.gen, false
}

// put stores a set read while the cache was at generation gen; an invalidation since then drops it.
func (c *mountCache) put(collection string, set *mountSet, gen uint64, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if c.entries == nil {
		c.entries = map[string]mountEntry{}
	}
	c.entries[collection] = mountEntry{set, expires}
}

func (c *mountCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	clear(c.entries)
}

// structural runs a transaction that changes the tree and drops the cached mounts around it.
func (s *service) structural(ctx context.Context, fn func(store.Tx) error) error {
	err := s.db.Tx(ctx, fn)
	s.mounts.invalidate()
	return err
}
