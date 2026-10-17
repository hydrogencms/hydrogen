package pages

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// subtreeResult is what recomputeSubtree found: whether routes changed, and the page routes it could not
// write because they exceed the path limit.
type subtreeResult struct {
	changed bool
	tooLong []want
}

// recomputeSubtree reconciles the routes of the pages at and below root in the given locales (nil: all), and
// of the collections mounted inside the subtree, which depend on the pages above them. When it frees paths,
// every collection mounted in the site is recomputed too: an item that a page's path blocked gets its route
// once the page lets go of it.
func (s *service) recomputeSubtree(ctx context.Context, q store.SQL, root nodeRow, locales []string) (subtreeResult, error) {
	g := newGraph(q)
	nodes, err := g.loadSubtree(ctx, root)
	if err != nil {
		return subtreeResult{}, err
	}
	existing, err := pageRoutesIn(ctx, q, root.Path, locales)
	if err != nil {
		return subtreeResult{}, err
	}
	wants := g.pageWants(nodes, locales)
	res := subtreeResult{tooLong: g.tooLong}
	plan, err := diffRoutes(ctx, q, existing, wants)
	if err != nil {
		return res, err
	}
	if err := s.apply(ctx, q, plan, true); err != nil {
		return res, err
	}
	res.changed = !plan.empty()
	var colls []string
	for _, n := range nodes {
		if n.Kind == KindMount && !slices.Contains(colls, n.Collection) {
			colls = append(colls, n.Collection)
		}
	}
	if len(plan.demote) > 0 {
		site, err := siteCollections(ctx, q, root.Site)
		if err != nil {
			return res, err
		}
		for _, c := range site {
			if !slices.Contains(colls, c) {
				colls = append(colls, c)
			}
		}
	}
	slices.Sort(colls)
	for _, c := range colls {
		if err := s.recomputeCollection(ctx, q, root.Site, c, nil, locales); err != nil {
			return res, err
		}
		res.changed = true
	}
	return res, nil
}

// siteCollections lists the collections mounted in a site.
func siteCollections(ctx context.Context, q store.SQL, site string) ([]string, error) {
	var out []string
	err := each(ctx, q, "SELECT DISTINCT collection FROM page_node WHERE site = ? AND kind = 'mount' ORDER BY collection",
		[]any{site}, func(r store.Rows) error {
			var c string
			err := r.Scan(&c)
			out = append(out, c)
			return err
		})
	return out, err
}

// lockedNodeByItem finds the node of a page item, locks its site and reads the node again, which may have
// moved or gone while waiting for the lock.
func (s *service) lockedNodeByItem(ctx context.Context, tx store.Tx, item ids.UUID) (nodeRow, bool, error) {
	q := tx.SQL()
	n, ok, err := loadNodeByItem(ctx, q, item)
	if err != nil || !ok {
		return n, false, err
	}
	if err := s.lockSite(ctx, tx, n.Site); err != nil {
		return n, false, err
	}
	return loadNodeByItem(ctx, q, item)
}

// syncPage records a published locale of a placed page (slug, name, vanity path) and reconciles the routes it
// changes. It reports where routes changed.
func (s *service) syncPage(ctx context.Context, tx store.Tx, item ids.UUID, mainLocale, locale string, row slugRow) ([]change, error) {
	q := tx.SQL()
	n, ok, err := s.lockedNodeByItem(ctx, tx, item)
	if err != nil || !ok {
		return nil, err
	}
	var old slugRow
	has := false
	err = q.QueryRow(ctx, "SELECT slug, name, vanity FROM page_slug WHERE node_id = ? AND locale = ?", n.ID, locale).
		Scan(&old.Slug, &old.Name, &old.Vanity)
	switch {
	case err == nil:
		has = true
	case !isNoRows(err):
		return nil, err
	}
	mainChanged := n.MainLocale != mainLocale
	if has && old == row && !mainChanged {
		return nil, nil
	}
	if mainChanged {
		if _, err := q.Exec(ctx, "UPDATE page_node SET main_locale = ? WHERE id = ?", mainLocale, n.ID); err != nil {
			return nil, err
		}
	}
	if err := putSlug(ctx, q, n.ID, locale, row); err != nil {
		return nil, fmt.Errorf("pages: save slug of %s: %w", n.ID, err)
	}
	// A new name changes the navigation labels even when no route moves.
	renamed := !has || old.Name != row.Name
	self := []change{{n.Site, &n.ID}}
	if has && old.Slug == row.Slug && old.Vanity == row.Vanity && !mainChanged {
		return self, nil
	}
	locales := []string{locale}
	if locale == mainLocale || mainChanged {
		locales = nil
	}
	n.MainLocale = mainLocale
	res, err := s.recomputeSubtree(ctx, q, n, locales)
	for _, w := range res.tooLong {
		s.log.WarnContext(ctx, "pages: page path too long, no route", "item", w.Item, "locale", w.Locale, "length", len(w.Path))
	}
	if err != nil {
		return nil, err
	}
	if !res.changed && !renamed {
		return nil, nil
	}
	return self, nil
}

// removeItem drops everything the module keeps for a deleted item: its page node, memberships and routes,
// including history and manual redirects, and the targets of links that pointed at it. A page node with
// children cannot go (content delete refuses it earlier); it returns content.ErrConflict. It reports what
// changed.
func (s *service) removeItem(ctx context.Context, tx store.Tx, item ids.UUID) ([]change, error) {
	q := tx.SQL()
	n, ok, err := s.lockedNodeByItem(ctx, tx, item)
	if err != nil {
		return nil, err
	}
	if ok {
		children, err := countChildren(ctx, q, n.ID)
		if err != nil {
			return nil, err
		}
		if children > 0 {
			return nil, conflict("page %s still has children; move or delete its children first", item)
		}
	}
	freed, err := q.Exec(ctx, "DELETE FROM route WHERE item_id = ?", item)
	if err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, "DELETE FROM page_mount_item WHERE item_id = ?", item); err != nil {
		return nil, err
	}
	out, err := s.clearLinks(ctx, q, "link_item", item)
	if err != nil || !ok {
		return out, err
	}
	for _, stmt := range []string{"DELETE FROM route WHERE node_id = ?", "DELETE FROM page_slug WHERE node_id = ?"} {
		if _, err := q.Exec(ctx, stmt, n.ID); err != nil {
			return nil, err
		}
	}
	if _, err := q.Exec(ctx, "DELETE FROM page_node WHERE id = ?", n.ID); err != nil {
		if errors.Is(err, store.ErrConstraint) {
			return nil, conflict("page %s still has children; move or delete its children first", item)
		}
		return nil, err
	}
	linked, err := s.clearLinks(ctx, q, "link_node", n.ID)
	if err != nil {
		return nil, err
	}
	out = append(out, linked...)
	if freed > 0 {
		cols, err := siteCollections(ctx, q, n.Site)
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			if err := s.recomputeCollection(ctx, q, n.Site, c, nil, nil); err != nil {
				return nil, err
			}
		}
	}
	return append(out, change{Site: n.Site}), nil
}

// dropRoutes removes what the module serves for an item that is not published: its routes, memberships and,
// for a page, its slugs. The page node stays in the tree. The history rows (former paths, state redirect) stay:
// they resolve to nothing while the item is offline (their target is its current route) and redirect again once
// it is restored and published; any live route may take their path over meanwhile. Only a purge
// (removeItem) drops them. Idempotent.
func (s *service) dropRoutes(ctx context.Context, tx store.Tx, item ids.UUID) ([]change, error) {
	q := tx.SQL()
	n, ok, err := s.lockedNodeByItem(ctx, tx, item)
	if err != nil {
		return nil, err
	}
	freed, err := q.Exec(ctx, "DELETE FROM route WHERE item_id = ? AND state <> 'redirect'", item)
	if err != nil {
		return nil, err
	}
	if _, err := q.Exec(ctx, "DELETE FROM page_mount_item WHERE item_id = ?", item); err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	slugs, err := q.Exec(ctx, "DELETE FROM page_slug WHERE node_id = ?", n.ID)
	if err != nil {
		return nil, err
	}
	if freed > 0 {
		cols, err := siteCollections(ctx, q, n.Site)
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			if err := s.recomputeCollection(ctx, q, n.Site, c, nil, nil); err != nil {
				return nil, err
			}
		}
	}
	if freed == 0 && slugs == 0 {
		return nil, nil
	}
	return []change{{n.Site, &n.ID}}, nil
}

func countChildren(ctx context.Context, q store.SQL, node ids.UUID) (int, error) {
	var n int
	err := q.QueryRow(ctx, "SELECT COUNT(*) FROM page_node WHERE parent_id = ?", node).Scan(&n)
	return n, err
}

// clearLinks removes the target of the links that point at an item (column link_item) or a node (link_node),
// which no longer exists. The links stay, without a target.
func (s *service) clearLinks(ctx context.Context, q store.SQL, column string, target ids.UUID) ([]change, error) {
	rows, err := queryNodes(ctx, q, "SELECT "+nodeCols+" FROM page_node WHERE "+column+" = ?", target)
	if err != nil {
		return nil, err
	}
	var out []change
	for _, r := range rows {
		var spec LinkSpec
		if err := jsonUnmarshal(r.Data, &spec); err != nil {
			return nil, err
		}
		spec.Item, spec.Node = nil, nil
		if _, err := q.Exec(ctx, "UPDATE page_node SET data = ?, link_item = NULL, link_node = NULL, updated_at = ? WHERE id = ?",
			mustJSON(spec), s.clock.Now(), r.ID); err != nil {
			return nil, err
		}
		id := r.ID
		out = append(out, change{r.Site, &id})
	}
	return out, nil
}
