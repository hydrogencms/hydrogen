package pages

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

type routeKey struct {
	Site   string
	Locale string
	Path   string
}

// routeRow is a row of the route table.
type routeRow struct {
	routeKey
	State  string
	Item   *ids.UUID
	Node   *ids.UUID
	Target string
	Status int
}

const routeCols = "site, locale, path, state, item_id, node_id, target, status"

func scanRoute(r scanner) (routeRow, error) {
	var x routeRow
	var target *string
	err := r.Scan(&x.Site, &x.Locale, &x.Path, &x.State, &x.Item, &x.Node, &target, &x.Status)
	if target != nil {
		x.Target = *target
	}
	return x, err
}

func sameOwner(a, b *ids.UUID) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

// rank orders what may claim a path: current and alias routes beat manual redirects, which beat history.
func rank(state string) int {
	switch state {
	case StateCurrent, StateAlias:
		return 3
	case StateManual:
		return 2
	}
	return 1
}

// loadRoutesAt returns the rows that occupy the given keys, including manual redirects of every locale.
func loadRoutesAt(ctx context.Context, q store.SQL, keys []routeKey) (map[routeKey][]routeRow, error) {
	type sl struct{ site, locale string }
	bySL := map[sl][]string{}
	for _, k := range keys {
		bySL[sl{k.Site, k.Locale}] = append(bySL[sl{k.Site, k.Locale}], k.Path)
	}
	order := make([]sl, 0, len(bySL))
	for k := range bySL {
		order = append(order, k)
	}
	slices.SortFunc(order, func(a, b sl) int { return cmp.Or(strings.Compare(a.site, b.site), strings.Compare(a.locale, b.locale)) })
	out := map[routeKey][]routeRow{}
	for _, k := range order {
		err := chunks(bySL[k], func(paths []string) error {
			ph, args := inArgs(paths, k.site, k.locale)
			return each(ctx, q, "SELECT "+routeCols+" FROM route WHERE site = ? AND (locale = ? OR locale = '') AND path IN "+
				ph, args, func(r store.Rows) error {
				x, err := scanRoute(r)
				if err != nil {
					return err
				}
				key := routeKey{x.Site, k.locale, x.Path}
				out[key] = append(out[key], x)
				return nil
			})
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// pageRoutesIn returns the current and alias routes of the page nodes at or below the ancestry prefix.
func pageRoutesIn(ctx context.Context, q store.SQL, prefix string, locales []string) (map[routeKey]routeRow, error) {
	query := `SELECT r.site, r.locale, r.path, r.state, r.item_id, r.node_id, r.target, r.status FROM route r
JOIN page_node n ON n.id = r.node_id WHERE n.path >= ? AND n.path < ? AND n.kind = 'page' AND r.state IN ('current', 'alias')`
	args := []any{prefix, pathUpper(prefix)}
	if locales != nil {
		ph, largs := inArgs(locales)
		query += " AND r.locale IN " + ph
		args = append(args, largs...)
	}
	out := map[routeKey]routeRow{}
	err := each(ctx, q, query, args, func(r store.Rows) error {
		x, err := scanRoute(r)
		out[x.routeKey] = x
		return err
	})
	return out, err
}

// mountRoutes returns the current and alias routes of the mount nodes (of one item when item is set).
func mountRoutes(ctx context.Context, q store.SQL, mounts []ids.UUID, item *ids.UUID, locales []string) (map[routeKey]routeRow, error) {
	out := map[routeKey]routeRow{}
	err := chunks(mounts, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		query := "SELECT " + routeCols + " FROM route WHERE state IN ('current', 'alias') AND node_id IN " + ph
		if item != nil {
			query += " AND item_id = ?"
			args = append(args, *item)
		}
		if locales != nil {
			lph, largs := inArgs(locales)
			query += " AND locale IN " + lph
			args = append(args, largs...)
		}
		return each(ctx, q, query, args, func(r store.Rows) error {
			x, err := scanRoute(r)
			out[x.routeKey] = x
			return err
		})
	})
	return out, err
}

// clash is a wanted route that another owner already holds.
type clash struct {
	want want
	held routeRow
}

func (c clash) String() string {
	by := "by another item"
	switch {
	case c.held.State == StateManual:
		by = "by a redirect"
	case c.held.Item != nil:
		by = "by item " + c.held.Item.String()
	}
	return fmt.Sprintf("the path %s is already used %s", c.want.Path, by)
}

// routePlan is the difference between the stored routes and the wanted ones.
type routePlan struct {
	demote  []routeRow // current and alias rows nobody wants any more: they become redirects
	upsert  []want
	clashes []clash
}

func (p *routePlan) empty() bool { return len(p.demote) == 0 && len(p.upsert) == 0 }

// diffRoutes compares the wanted routes with the stored ones of the same scope. A wanted route clashes when a
// route of another item (or a manual redirect) holds its path; history redirects are replaced.
func diffRoutes(ctx context.Context, q store.SQL, existing map[routeKey]routeRow, wants []want) (*routePlan, error) {
	slices.SortStableFunc(wants, func(a, b want) int {
		return cmp.Or(strings.Compare(a.Site, b.Site), strings.Compare(a.Locale, b.Locale), strings.Compare(a.Path, b.Path),
			boolRank(b.page)-boolRank(a.page), bytes.Compare(idBytes(a.Item), idBytes(b.Item)))
	})
	plan := &routePlan{}
	deduped := wants[:0:0]
	for _, w := range wants {
		if n := len(deduped); n > 0 && deduped[n-1].routeKey == w.routeKey {
			prev := deduped[n-1]
			if prev.page && w.page && !sameOwner(prev.Item, w.Item) {
				plan.clashes = append(plan.clashes, clash{w, prev.routeRow})
			}
			continue
		}
		deduped = append(deduped, w)
	}
	var changed []want
	wanted := make(map[routeKey]bool, len(deduped))
	for _, w := range deduped {
		wanted[w.routeKey] = true
		if e, ok := existing[w.routeKey]; ok && e.State == w.State && sameOwner(e.Item, w.Item) && sameOwner(e.Node, w.Node) {
			continue
		}
		changed = append(changed, w)
	}
	keys := make([]routeKey, len(changed))
	for i, w := range changed {
		keys[i] = w.routeKey
	}
	held, err := loadRoutesAt(ctx, q, keys)
	if err != nil {
		return nil, err
	}
	for _, w := range changed {
		var blocker *routeRow
		for _, h := range held[w.routeKey] {
			if rank(h.State) > 1 && (h.State == StateManual || !sameOwner(h.Item, w.Item)) {
				blocker = &h
				break
			}
		}
		if blocker != nil {
			plan.clashes = append(plan.clashes, clash{w, *blocker})
			continue
		}
		plan.upsert = append(plan.upsert, w)
	}
	for k, e := range existing {
		if !wanted[k] {
			plan.demote = append(plan.demote, e)
		}
	}
	slices.SortFunc(plan.demote, func(a, b routeRow) int {
		return cmp.Or(strings.Compare(a.Site, b.Site), strings.Compare(a.Locale, b.Locale), strings.Compare(a.Path, b.Path))
	})
	return plan, nil
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

func idBytes(id *ids.UUID) []byte {
	if id == nil {
		return nil
	}
	return id[:]
}

// apply writes the plan. Clashes of mounted items are skipped and logged (pages beat mounts; the item gets
// its route once the path is free and the collection is recomputed); any other clash fails with
// content.ErrConflict, and so does strict.
func (s *service) apply(ctx context.Context, q store.SQL, plan *routePlan, strict bool) error {
	for _, c := range plan.clashes {
		if strict || c.want.page {
			return conflict("%s", c)
		}
		s.log.WarnContext(ctx, "pages: mounted item skipped, its path is taken",
			"item", c.want.Item, "site", c.want.Site, "locale", c.want.Locale, "path", c.want.Path, "held_by", c.held.Item)
	}
	if plan.empty() {
		return nil
	}
	now := s.clock.Now()
	type sl struct{ site, locale string }
	groups := map[sl][]string{}
	var order []sl
	for _, d := range plan.demote {
		k := sl{d.Site, d.Locale}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], d.Path)
	}
	for _, k := range order {
		err := chunks(groups[k], func(paths []string) error {
			ph, args := inArgs(paths, now, k.site, k.locale)
			_, err := q.Exec(ctx, "UPDATE route SET state = 'redirect', node_id = NULL, target = NULL, status = 301, updated_at = ? "+
				"WHERE site = ? AND locale = ? AND state IN ('current', 'alias') AND path IN "+ph, args...)
			return err
		})
		if err != nil {
			return fmt.Errorf("pages: demote routes: %w", err)
		}
	}
	rows := make([][]any, len(plan.upsert))
	for i, w := range plan.upsert {
		rows[i] = []any{w.Site, w.Locale, w.Path, w.State, w.Item, w.Node, nil, 0, now}
	}
	return s.upsertRoutes(ctx, q, rows)
}

// upsertRoutes inserts rows [site, locale, path, state, item, node, target, status, updated_at]. A row replaces
// a history redirect or a row of the same item only; anything else was taken meanwhile.
func (s *service) upsertRoutes(ctx context.Context, q store.SQL, rows [][]any) error {
	if len(rows) == 0 {
		return nil
	}
	n, err := insertRows(ctx, q, "route", "site, locale, path, state, item_id, node_id, target, status, updated_at", `
ON CONFLICT (site, locale, path) DO UPDATE SET state = excluded.state, item_id = excluded.item_id,
node_id = excluded.node_id, target = excluded.target, status = excluded.status, updated_at = excluded.updated_at
WHERE route.state = 'redirect' OR route.item_id = excluded.item_id`, rows)
	if err != nil {
		return fmt.Errorf("pages: write routes: %w", err)
	}
	if n != int64(len(rows)) {
		return conflict("a route changed while this was saving; try again")
	}
	return nil
}
