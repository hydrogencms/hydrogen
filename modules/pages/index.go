package pages

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// accessIndexBuilt is recorded (as a content migration name, which no registered migration can use) once the
// module index rows of every placed item exist, for databases that held pages before they did.
const accessIndexBuilt = "pages:access-index"

const (
	indexBatch = 200
	indexLock  = "pages:index"
)

// reindexItems rewrites the module index rows (pages.site, pages.ancestor) of items from their placements.
func (s *service) reindexItems(ctx context.Context, tx store.Tx, items []ids.UUID) error {
	items = slices.Clone(items)
	slices.SortFunc(items, func(a, b ids.UUID) int { return slices.Compare(a[:], b[:]) })
	items = slices.Compact(items)
	return chunks(items, func(part []ids.UUID) error {
		have := map[ids.UUID][]placement{}
		queries := []string{
			"SELECT item_id, site, path FROM page_node WHERE item_id IN ",
			"SELECT m.item_id, n.site, n.path FROM page_mount_item m JOIN page_node n ON n.id = m.node_id WHERE m.item_id IN ",
		}
		for _, q := range queries {
			ph, args := inArgs(part)
			err := each(ctx, tx.SQL(), q+ph, args, func(r store.Rows) error {
				var id ids.UUID
				var p placement
				err := r.Scan(&id, &p.Site, &p.Path)
				have[id] = append(have[id], p)
				return err
			})
			if err != nil {
				return err
			}
		}
		for _, id := range part {
			rows, err := indexRowsOf(have[id])
			if err != nil {
				return err
			}
			if err := tx.Content().ReplaceModuleIndex(ctx, id, indexModule, rows); err != nil {
				return fmt.Errorf("pages: index %s: %w", id, err)
			}
		}
		return nil
	})
}

// indexRowsOf builds the rows of an item's placements: its sites (sorted) and the node ids of their paths.
func indexRowsOf(pls []placement) ([]store.IndexRow, error) {
	pls = slices.Clone(pls)
	slices.SortFunc(pls, func(a, b placement) int {
		return cmp.Or(strings.Compare(a.Site, b.Site), strings.Compare(a.Path, b.Path))
	})
	var rows []store.IndexRow
	var nodes []ids.UUID
	lastSite := ""
	for i, p := range pls {
		if i == 0 || p.Site != lastSite {
			site := p.Site
			rows = append(rows, store.IndexRow{Field: fieldSite, Text: &site})
			lastSite = p.Site
		}
		anc, err := ancestry(p.Path)
		if err != nil {
			return nil, err
		}
		for _, id := range anc {
			if !slices.Contains(nodes, id) {
				nodes = append(nodes, id)
				ref := id
				rows = append(rows, store.IndexRow{Field: fieldAncestor, Ref: &ref})
			}
		}
	}
	return rows, nil
}

// itemsInRange lists the items placed at or below the node at prefix: page items and mounted items.
func itemsInRange(ctx context.Context, q store.SQL, prefix string) ([]ids.UUID, error) {
	var out []ids.UUID
	err := each(ctx, q, `SELECT item_id FROM page_node WHERE path >= ? AND path < ? AND item_id IS NOT NULL
UNION SELECT m.item_id FROM page_mount_item m JOIN page_node n ON n.id = m.node_id WHERE n.path >= ? AND n.path < ?`,
		[]any{prefix, pathUpper(prefix), prefix, pathUpper(prefix)}, func(r store.Rows) error {
			var id ids.UUID
			err := r.Scan(&id)
			out = append(out, id)
			return err
		})
	return out, err
}

// mountItemsOf lists the items routed under a mount.
func mountItemsOf(ctx context.Context, q store.SQL, node ids.UUID) ([]ids.UUID, error) {
	var out []ids.UUID
	err := each(ctx, q, "SELECT DISTINCT item_id FROM page_mount_item WHERE node_id = ?", []any{node}, func(r store.Rows) error {
		var id ids.UUID
		err := r.Scan(&id)
		out = append(out, id)
		return err
	})
	return out, err
}

// reindexSubtree rewrites the rows of every item placed at or below a node, after the subtree moved.
func (s *service) reindexSubtree(ctx context.Context, tx store.Tx, n nodeRow) error {
	items, err := itemsInRange(ctx, tx.SQL(), n.Path)
	if err != nil {
		return err
	}
	return s.reindexItems(ctx, tx, items)
}

// placedItems lists up to limit placed item ids after the given one, in id order.
func placedItems(ctx context.Context, q store.SQL, after ids.UUID, limit int) ([]ids.UUID, error) {
	var out []ids.UUID
	err := each(ctx, q, `SELECT item_id FROM (SELECT item_id FROM page_node WHERE item_id IS NOT NULL
UNION SELECT item_id FROM page_mount_item) AS placed WHERE item_id > ? ORDER BY item_id LIMIT ?`,
		[]any{after, limit}, func(r store.Rows) error {
			var id ids.UUID
			err := r.Scan(&id)
			out = append(out, id)
			return err
		})
	return out, err
}

// rebuildIndex rewrites the module index rows of every placed item, one run of fn per batch (a transaction, or
// the caller's own). It is idempotent and returns the number of items.
func (s *service) rebuildIndex(ctx context.Context, run func(func(store.Tx) error) error) (int, error) {
	total := 0
	after := ids.Nil
	for {
		var batch []ids.UUID
		err := run(func(tx store.Tx) (err error) {
			if batch, err = placedItems(ctx, tx.SQL(), after, indexBatch); err != nil {
				return err
			}
			if err := s.lockItems(ctx, tx, batch); err != nil {
				return err
			}
			if err := s.lockSites(ctx, tx, s.sites); err != nil {
				return err
			}
			return s.reindexItems(ctx, tx, batch)
		})
		if err != nil {
			return total, fmt.Errorf("pages: rebuild the access index: %w", err)
		}
		total += len(batch)
		if len(batch) < indexBatch {
			return total, nil
		}
		after = batch[len(batch)-1]
	}
}

// ensureAccessIndex builds the module index rows once for a database that held pages before they existed. When
// another process holds the lock it returns at once, since that process is building them.
func (s *service) ensureAccessIndex(ctx context.Context) error {
	done := func() (bool, error) {
		var applied []string
		err := s.db.View(ctx, func(tx store.Tx) (err error) {
			applied, err = tx.Content().AppliedContentMigrations(ctx)
			return err
		})
		return slices.Contains(applied, accessIndexBuilt), err
	}
	if ok, err := done(); err != nil || ok {
		return err
	}
	lock, ok, err := s.db.Locker().TryLock(ctx, indexLock)
	if err != nil || !ok {
		return err
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	if ok, err := done(); err != nil || ok {
		return err
	}
	if _, err := s.rebuildIndex(ctx, func(fn func(store.Tx) error) error { return s.db.Tx(ctx, fn) }); err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx store.Tx) error {
		return tx.Content().RecordContentMigration(ctx, accessIndexBuilt, s.clock.Now())
	})
}

// lockItems locks items for update, in id order, before a site lock: content writes lock the item first and the
// site second, and module index rows reference the item, so writing them under a site lock without holding the
// item lock first could deadlock with a concurrent publish. SQLite has a single writer.
func (s *service) lockItems(ctx context.Context, tx store.Tx, list []ids.UUID) error {
	if s.db.Dialect() != "postgres" || len(list) == 0 {
		return nil
	}
	list = slices.Clone(list)
	slices.SortFunc(list, func(a, b ids.UUID) int { return slices.Compare(a[:], b[:]) })
	return chunks(slices.Compact(list), func(part []ids.UUID) error {
		ph, args := inArgs(part)
		return each(ctx, tx.SQL(), "SELECT id FROM item WHERE id IN "+ph+" ORDER BY id FOR UPDATE", args, func(r store.Rows) error {
			var id ids.UUID
			return r.Scan(&id)
		})
	})
}

// lockForUpdate locks what an update of a node will reindex: the items under a moved node, and the earlier and
// later members of a reconfigured mount.
func (s *service) lockForUpdate(ctx context.Context, tx store.Tx, id ids.UUID, in NodeUpdate) error {
	q := tx.SQL()
	n, err := loadNode(ctx, q, id)
	if err != nil {
		return nil // reported by the update itself
	}
	var list []ids.UUID
	if in.Move != nil {
		if list, err = itemsInRange(ctx, q, n.Path); err != nil {
			return err
		}
	}
	if in.Mount != nil && n.Kind == KindMount {
		old, err := mountItemsOf(ctx, q, n.ID)
		if err != nil {
			return err
		}
		list = append(list, old...)
		spec := *in.Mount
		if _, err := s.checkMountSpec(&spec); err == nil {
			more, err := s.membershipItems(ctx, tx, &spec)
			if err != nil {
				return err
			}
			list = append(list, more...)
		}
	}
	return s.lockItems(ctx, tx, list)
}
