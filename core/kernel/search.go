package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
)

// SearchConsumer is the name of the outbox consumer that keeps an external search index current.
const SearchConsumer = "search.index"

// SearchReindexCommand rebuilds the external search index.
const SearchReindexCommand = "search:reindex"

const (
	searchRebuildLock = "search:rebuild"
	searchPage        = 100
	searchRetry       = 30 * time.Second
)

// SearchEngine registers the engine that answers searches (Services.Search). At most one module may register
// one; without it the database's full-text search answers. An engine that is a search.Index is kept in sync by
// the kernel: the search.index outbox consumer follows publishes, unpublishes, trash and purges, and a
// background rebuild runs at start-up when the engine asks for it.
func (r *Registry) SearchEngine(e search.Engine) error {
	if e == nil {
		return fmt.Errorf("search engine: nil")
	}
	if r.engine != nil {
		return fmt.Errorf("search engine registered by both %s and %s", r.engine.module, r.current)
	}
	r.engine = &registered[search.Engine]{r.current, e}
	return nil
}

// searchIndex returns the registered engine when it has an index of its own.
func (r *Registry) searchIndex() (search.Index, bool) {
	if r.engine == nil {
		return nil, false
	}
	idx, ok := r.engine.value.(search.Index)
	return idx, ok
}

// searchConsumerAttempts lets a delivery fail for about a day (the back-off caps at five minutes), so an engine
// that restarts or is unreachable for a while does not cost changes.
const searchConsumerAttempts = 300

// searchConsumer keeps idx current. Every handler re-reads the item's published snapshots and replaces its
// documents, so redelivery and reordering are harmless. After a successful index write it purges the cached
// delivery responses of the item's collection, since the cache.purge consumer may have run before the index
// caught up. Items are handled one node at a time: a busy item fails the delivery and is retried.
func searchConsumer(idx search.Index, db store.DB, sc *schema.Schema) outbox.Consumer {
	return outbox.Consumer{
		Name:        SearchConsumer,
		Topics:      []string{"item.published", "item.unpublished", "item.trashed", "item.restored", "item.deleted", "item.updated"},
		MaxAttempts: searchConsumerAttempts,
		Handle: func(ctx context.Context, e outbox.Event) error {
			var p struct {
				ID    ids.UUID `json:"id"`
				Type  string   `json:"type"`
				Draft bool     `json:"draft"`
			}
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return fmt.Errorf("search: bad %s payload: %w", e.Topic, err)
			}
			if p.Draft {
				return nil // a draft write changes nothing that is published
			}
			if err := reindexItem(ctx, idx, db, sc, p.ID); err != nil {
				return err
			}
			if t, ok := sc.Type(p.Type); ok && t.Searchable() {
				return db.Tx(ctx, func(tx store.Tx) error { return tx.Notify(cachetag.PurgeChannel, cachetag.Collection(p.Type)) })
			}
			return nil
		},
	}
}

var errSearchItemBusy = errors.New("search: the item is being indexed by another node")

func reindexItem(ctx context.Context, idx search.Index, db store.DB, sc *schema.Schema, id ids.UUID) error {
	lock, ok, err := db.Locker().TryLock(ctx, "search:item:"+id.String())
	if err != nil {
		return err
	}
	if !ok {
		return errSearchItemBusy
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	docs, err := itemDocs(ctx, db, sc, []ids.UUID{id})
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		return idx.Delete(ctx, []ids.UUID{id})
	}
	return idx.Upsert(ctx, docs)
}

// itemDocs builds the documents of the published searchable items among items, from all their snapshots (which
// include the locales that inherit through overlays).
func itemDocs(ctx context.Context, db store.DB, sc *schema.Schema, items []ids.UUID) ([]search.Doc, error) {
	snaps, err := db.Reader().Snapshots(ctx, items, nil)
	if err != nil {
		return nil, fmt.Errorf("search: read snapshots: %w", err)
	}
	return search.BuildDocs(sc, snaps)
}

// rebuildPager returns the next function of Index.Rebuild: it pages through every published snapshot of the
// searchable types, a batch of items at a time.
func rebuildPager(db store.DB, sc *schema.Schema, types []string) func(ctx context.Context) ([]search.Doc, error) {
	var (
		ti    int
		after ids.UUID
	)
	return func(ctx context.Context) ([]search.Doc, error) {
		for ti < len(types) {
			var page []ids.UUID
			err := db.View(ctx, func(tx store.Tx) error {
				var err error
				page, err = tx.Content().ItemIDsOfType(ctx, types[ti], after, searchPage)
				return err
			})
			if err != nil {
				return nil, fmt.Errorf("search: list %s items: %w", types[ti], err)
			}
			if len(page) < searchPage {
				ti++
				after = ids.UUID{}
			} else {
				after = page[len(page)-1]
			}
			docs, err := itemDocs(ctx, db, sc, page)
			if err != nil {
				return nil, err
			}
			if len(docs) > 0 {
				return docs, nil
			}
		}
		return nil, nil
	}
}

// ErrSearchRebuilding is returned by ReindexSearch when another node holds the rebuild lock.
var ErrSearchRebuilding = errors.New("search: another node is rebuilding the index")

// ReindexSearch rebuilds the external search index from the published content now, whether or not its
// settings changed. It returns false when the engine has no index of its own (the database answers searches).
func (s *Services) ReindexSearch(ctx context.Context) (bool, error) {
	idx, ok := s.Search.(search.Index)
	if !ok {
		return false, nil
	}
	set := search.NewSettings(s.Schema)
	if _, err := idx.Sync(ctx, set); err != nil {
		return true, fmt.Errorf("search: sync: %w", err)
	}
	return true, s.rebuildSearch(ctx, idx, set, true)
}

func (s *Services) rebuildSearch(ctx context.Context, idx search.Index, set search.Settings, wait bool) error {
	lock, ok, err := s.DB.Locker().TryLock(ctx, searchRebuildLock)
	if err != nil {
		return err
	}
	if !ok {
		if wait {
			return ErrSearchRebuilding
		}
		s.Log.Info("search: another node is rebuilding the index")
		return nil
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	s.Log.Info("search: rebuilding the index", "engine", idx.Name())
	started := s.Clock.Now()
	if err := idx.Rebuild(ctx, set, rebuildPager(s.DB, s.Schema, set.Types)); err != nil {
		return fmt.Errorf("search: rebuild: %w", err)
	}
	if err := s.catchUpSearch(ctx, idx, set, started.Add(-time.Minute)); err != nil {
		return fmt.Errorf("search: catch up after rebuild: %w", err)
	}
	s.Log.Info("search: index rebuilt", "engine", idx.Name())
	return nil
}

// catchUpSearch re-indexes the items of searchable types changed since `since`: edits made while the rebuild
// paged through the content may have been missed by it (and the consumer's writes went to the old index).
func (s *Services) catchUpSearch(ctx context.Context, idx search.Index, set search.Settings, since time.Time) error {
	if len(set.Types) == 0 {
		return nil
	}
	for offset := 0; ; offset += searchPage {
		res, err := s.DB.Reader().Query(ctx, store.Query{Types: set.Types, Trashed: store.TrashedInclude,
			Sort: []store.Sort{{Field: "updatedAt", Desc: true}}, Limit: searchPage, Offset: offset})
		if err != nil {
			return err
		}
		for _, it := range res.Items {
			if it.UpdatedAt.Before(since) {
				return nil
			}
			if err := reindexItem(ctx, idx, s.DB, s.Schema, it.ID); err != nil {
				return err
			}
		}
		if len(res.Items) < searchPage {
			return nil
		}
	}
}

// syncSearch applies the settings to the engine's index and rebuilds it when the engine asks to.
func (s *Services) syncSearch(ctx context.Context, idx search.Index) error {
	set := search.NewSettings(s.Schema)
	rebuild, err := idx.Sync(ctx, set)
	if err != nil {
		return fmt.Errorf("search: sync: %w", err)
	}
	if !rebuild {
		return nil
	}
	return s.rebuildSearch(ctx, idx, set, false)
}

// startSearch syncs the engine at start-up, retrying until it succeeds or ctx ends: an engine that is down
// must not stop the server, and its consumer retries events meanwhile.
func (s *Services) startSearch(ctx context.Context) {
	idx, ok := s.Search.(search.Index)
	if !ok {
		return
	}
	for {
		err := s.syncSearch(ctx, idx)
		if err == nil || ctx.Err() != nil {
			return
		}
		s.Log.Error("search: start-up sync failed, retrying", "engine", idx.Name(), "in", searchRetry, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(searchRetry):
		}
	}
}

func searchReindexCommand() Command {
	return Command{
		Name:    SearchReindexCommand,
		Summary: "rebuild the external search index from the published content",
		Run: func(ctx context.Context, app *App, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("%s takes no arguments", SearchReindexCommand)
			}
			s := app.Services()
			indexed, err := s.ReindexSearch(ctx)
			if err != nil {
				return err
			}
			if !indexed {
				s.Log.Info("search: the engine has no index of its own; the database searches live content")
			}
			return nil
		},
	}
}

// HealSearchRows rebuilds the content projections (the same as content:reindex) when items are published but
// the published search rows are missing altogether, as after a migration that dropped them. It reports whether
// it ran.
func (s *Services) HealSearchRows(ctx context.Context) (bool, error) {
	var empty bool
	err := s.DB.View(ctx, func(tx store.Tx) error {
		var err error
		empty, err = tx.Content().SearchIndexEmpty(ctx)
		return err
	})
	if err != nil || !empty {
		return false, err
	}
	lock, ok, err := s.DB.Locker().TryLock(ctx, "search:heal")
	if err != nil {
		return false, err
	}
	if !ok {
		s.Log.Info("search: another node is reindexing the content")
		return false, nil
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	s.Log.Warn("search: published items have no search rows; reindexing the content (searches find nothing until it is done)")
	n, err := s.Content.Reindex(ctx, nil)
	if err != nil {
		return true, err
	}
	s.Log.Info("search: content reindexed", "items", n)
	return true, nil
}
