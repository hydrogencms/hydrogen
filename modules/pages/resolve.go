package pages

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
)

func notFoundResolution() *Resolution { return &Resolution{Kind: ResolvedNotFound, Status: 404} }

// routeHit is the route row Resolve found and, for the rows that have one, its page node.
type routeHit struct {
	routeRow
	nodePath string // ancestry path of the node
	nodeKind string
	itemName string // mounted items: the published name in the locale, else the main locale
	hasNode  bool
}

// resolvablePath picks the string to look up: a valid site path, or any legacy path a manual redirect may
// match exactly. Invalid UTF-8, NUL bytes and encoded slashes (%2F) never resolve.
func resolvablePath(path string) (string, bool) {
	if !utf8.ValidString(path) || strings.Contains(path, "\x00") || hasEncodedSlash(path) {
		return path, false
	}
	if _, err := route.Clean(path); err == nil {
		return path, true
	}
	return path, checkLegacyPath(path) == nil
}

// resolve answers from two queries without a transaction: the route row joined with its page node, and the
// ancestors of that node with their slugs. History and alias targets add a lookup of the current path.
func (s *service) resolve(ctx context.Context, site, locale, path string) (*Resolution, error) {
	if err := s.checkSite(site); err != nil {
		return nil, err
	}
	p, ok := resolvablePath(path)
	if !ok {
		return notFoundResolution(), nil
	}
	rd := s.db.Read()
	best, err := lookupRoute(ctx, rd, site, locale, p)
	if err != nil {
		return nil, err
	}
	if best == nil {
		return notFoundResolution(), nil
	}
	switch best.State {
	case StateCurrent, StateAlias:
		return s.resolveServed(ctx, rd, site, locale, best)
	case StateRedirect:
		return s.resolveHistory(ctx, rd, site, locale, best.routeRow)
	}
	return s.resolveManual(ctx, rd, site, locale, best.routeRow)
}

// lookupRoute is the first query: the route that serves a path, with its node and a mounted item's name.
func lookupRoute(ctx context.Context, q store.SQL, site, locale, p string) (*routeHit, error) {
	var best *routeHit
	err := each(ctx, q, `SELECT r.site, r.locale, r.path, r.state, r.item_id, r.node_id, r.target, r.status,
n.path, n.kind,
CASE WHEN n.kind = 'mount' THEN (SELECT l.name FROM item i JOIN item_version_locale l
    ON l.item_id = i.id AND l.version = i.published_version
    WHERE i.id = r.item_id AND (l.locale = ? OR l.locale = i.main_locale)
    ORDER BY CASE WHEN l.locale = ? THEN 0 ELSE 1 END LIMIT 1) END
FROM route r LEFT JOIN page_node n ON n.id = r.node_id
WHERE r.site = ? AND r.path = ? AND (r.locale = ? OR r.locale = '')`,
		[]any{locale, locale, site, p, locale}, func(r store.Rows) error {
			var h routeHit
			var target, nodePath, kind, name *string
			err := r.Scan(&h.Site, &h.Locale, &h.Path, &h.State, &h.Item, &h.Node, &target, &h.Status,
				&nodePath, &kind, &name)
			if err != nil {
				return err
			}
			if target != nil {
				h.Target = *target
			}
			if nodePath != nil {
				h.hasNode, h.nodePath, h.nodeKind = true, *nodePath, *kind
			}
			if name != nil {
				h.itemName = *name
			}
			if best == nil || rank(h.State) > rank(best.State) || (rank(h.State) == rank(best.State) && h.Locale == locale) {
				best = &h
			}
			return nil
		})
	return best, err
}

// currentPathOf is the current route path of an item in a site and locale.
func currentPathOf(ctx context.Context, q store.SQL, site, locale string, item ids.UUID) (string, bool, error) {
	var p string
	err := q.QueryRow(ctx, "SELECT path FROM route WHERE site = ? AND locale = ? AND item_id = ? AND state = 'current'",
		site, locale, item).Scan(&p)
	if isNoRows(err) {
		return "", false, nil
	}
	return p, err == nil, err
}

func (s *service) resolveServed(ctx context.Context, q store.SQL, site, locale string, h *routeHit) (*Resolution, error) {
	if h.Node == nil || h.Item == nil || !h.hasNode {
		return notFoundResolution(), nil
	}
	res := &Resolution{Kind: ResolvedPage, Item: h.Item, Node: h.Node, Path: h.Path, Status: 200}
	if h.State == StateAlias {
		if cur, ok, err := currentPathOf(ctx, q, site, locale, *h.Item); err != nil {
			return nil, err
		} else if ok {
			res.Path = cur
		}
	}
	if h.nodeKind == KindMount {
		res.Kind = ResolvedItem
	}
	var err error
	res.Breadcrumbs, err = s.breadcrumbs(ctx, q, h, locale, res.Path)
	return res, err
}

// breadcrumbs is the second query: the ancestors of a node from the root, ending with the node (a page) or
// the mount and the mounted item.
func (s *service) breadcrumbs(ctx context.Context, q store.SQL, h *routeHit, locale, itemPath string) ([]Crumb, error) {
	anc, err := ancestry(h.nodePath)
	if err != nil {
		return nil, err
	}
	g, err := loadAncestorGraph(ctx, q, anc, locale)
	if err != nil {
		return nil, err
	}
	out := make([]Crumb, 0, len(anc)+1)
	for _, id := range anc {
		gn := g.nodes[id]
		if gn == nil {
			continue
		}
		c := Crumb{Node: id, Item: gn.Item}
		c.Path, _ = g.pathOf(id, locale)
		if gn.mount != nil {
			c.Name = labelFor(gn.mount.Label, locale, gn.mount.Segment)
		} else {
			c.Name = g.slugs[slugKey{id, locale}].Name
			if c.Name == "" {
				c.Name = g.slugs[slugKey{id, gn.MainLocale}].Name
			}
		}
		out = append(out, c)
	}
	if h.nodeKind == KindMount && h.Item != nil {
		out = append(out, Crumb{Node: *h.Node, Item: h.Item, Name: h.itemName, Path: itemPath})
	}
	return out, nil
}

// loadAncestorGraph reads the nodes of an ancestry (root first) with their slugs in the locale and in each
// page's main locale, in one query.
func loadAncestorGraph(ctx context.Context, q store.SQL, anc []ids.UUID, locale string) (*graph, error) {
	g := newGraph(q)
	ph, args := inArgs(anc, locale)
	err := each(ctx, q, `SELECT n.id, n.kind, n.item_id, n.main_locale, n.data, s.locale, s.slug, s.name
FROM page_node n LEFT JOIN page_slug s ON s.node_id = n.id AND (s.locale = ? OR s.locale = n.main_locale)
WHERE n.id IN `+ph, args, func(r store.Rows) error {
		var n nodeRow
		var data *json.RawMessage
		var loc, slug, name *string
		if err := r.Scan(&n.ID, &n.Kind, &n.Item, &n.MainLocale, &data, &loc, &slug, &name); err != nil {
			return err
		}
		if data != nil {
			n.Data = *data
		}
		if _, seen := g.nodes[n.ID]; !seen {
			if err := g.add([]nodeRow{n}); err != nil {
				return err
			}
		}
		if loc != nil {
			g.slugs[slugKey{n.ID, *loc}] = slugRow{Slug: *slug, Name: *name}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(anc); i++ {
		if n := g.nodes[anc[i]]; n != nil {
			parent := anc[i-1]
			n.Parent = &parent
		}
	}
	return g, nil
}

func (s *service) resolveHistory(ctx context.Context, q store.SQL, site, locale string, r routeRow) (*Resolution, error) {
	if r.Item == nil {
		return notFoundResolution(), nil
	}
	cur, ok, err := currentPathOf(ctx, q, site, locale, *r.Item)
	if err != nil {
		return nil, err
	}
	if !ok {
		return notFoundResolution(), nil
	}
	return &Resolution{Kind: ResolvedRedirect, Item: r.Item, Path: cur, Status: statusOr(r.Status), Location: cur}, nil
}

func (s *service) resolveManual(ctx context.Context, q store.SQL, site, locale string, r routeRow) (*Resolution, error) {
	res := &Resolution{Kind: ResolvedRedirect, Status: statusOr(r.Status), Location: r.Target}
	if rest, ok := strings.CutPrefix(r.Target, "item:"); ok {
		id, err := ids.Parse(rest)
		if err != nil {
			return notFoundResolution(), nil
		}
		cur, found, err := currentPathOf(ctx, q, site, locale, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return notFoundResolution(), nil
		}
		res.Item, res.Location, res.Path = &id, cur, cur
	}
	return res, nil
}

func statusOr(s int) int {
	if s == 0 {
		return 301
	}
	return s
}

func (s *service) paths(ctx context.Context, item ids.UUID) ([]Route, error) {
	out := []Route{}
	err := each(ctx, s.db.Read(), "SELECT "+routeCols+" FROM route WHERE item_id = ? ORDER BY site, locale, path", []any{item},
		func(r store.Rows) error {
			x, err := scanRoute(r)
			out = append(out, Route{Site: x.Site, Locale: x.Locale, Path: x.Path, State: x.State, Item: x.Item, Node: x.Node,
				Target: x.Target, Status: x.Status})
			return err
		})
	return out, err
}

// redirectRoutes lists the routes that only redirect: former paths of moved items and manual redirects.
func (s *service) redirectRoutes(ctx context.Context) ([]Route, error) {
	out := []Route{}
	err := each(ctx, s.db.Read(), "SELECT "+routeCols+" FROM route WHERE state IN ('redirect', 'manual') ORDER BY site, locale, path", nil,
		func(r store.Rows) error {
			x, err := scanRoute(r)
			out = append(out, Route{Site: x.Site, Locale: x.Locale, Path: x.Path, State: x.State, Item: x.Item, Node: x.Node,
				Target: x.Target, Status: x.Status})
			return err
		})
	return out, err
}
