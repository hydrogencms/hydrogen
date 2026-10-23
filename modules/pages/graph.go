package pages

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
)

// graph holds the nodes and slugs needed to compute paths: some subtrees and the ancestors of everything
// loaded. Paths are derived data; nothing here reads content.
type graph struct {
	q     store.SQL
	nodes map[ids.UUID]*gnode
	slugs map[slugKey]slugRow
	memo  map[pathKey]pathVal
	order map[ids.UUID]string

	// tooLong collects the page routes pageWants dropped for exceeding route.MaxPath.
	tooLong []want
}

type gnode struct {
	nodeRow
	mount *MountSpec
}

type pathKey struct {
	node   ids.UUID
	locale string
}

type pathVal struct {
	path string
	ok   bool
}

func newGraph(q store.SQL) *graph {
	return &graph{q: q, nodes: map[ids.UUID]*gnode{}, slugs: map[slugKey]slugRow{}, memo: map[pathKey]pathVal{},
		order: map[ids.UUID]string{}}
}

func (g *graph) add(rows []nodeRow) error {
	for _, r := range rows {
		if _, ok := g.nodes[r.ID]; ok {
			continue
		}
		n := &gnode{nodeRow: r}
		if r.Kind == KindMount {
			var spec MountSpec
			if err := json.Unmarshal(r.Data, &spec); err != nil {
				return fmt.Errorf("pages: mount %s: bad data: %w", r.ID, err)
			}
			n.mount = &spec
		}
		g.nodes[r.ID] = n
	}
	return nil
}

// missingAncestors lists the ancestors of the given nodes that are not loaded yet.
func (g *graph) missingAncestors(from []nodeRow) ([]ids.UUID, error) {
	var missing []ids.UUID
	seen := map[ids.UUID]bool{}
	for _, n := range from {
		anc, err := ancestry(n.Path)
		if err != nil {
			return nil, err
		}
		for _, id := range anc {
			if _, ok := g.nodes[id]; !ok && !seen[id] {
				seen[id] = true
				missing = append(missing, id)
			}
		}
	}
	return missing, nil
}

// loadAncestors loads the ancestors of the given nodes (and their slugs) that are not loaded yet.
func (g *graph) loadAncestors(ctx context.Context, from []nodeRow) error {
	missing, err := g.missingAncestors(from)
	if err != nil || len(missing) == 0 {
		return err
	}
	rows, err := loadNodes(ctx, g.q, missing)
	if err != nil {
		return err
	}
	if err := g.add(rows); err != nil {
		return err
	}
	return loadSlugsOf(ctx, g.q, missing, g.slugs)
}

// loadSubtree loads root, its descendants and its ancestors. It returns the subtree's nodes.
func (g *graph) loadSubtree(ctx context.Context, root nodeRow) ([]*gnode, error) {
	rows, err := loadRange(ctx, g.q, root.Path)
	if err != nil {
		return nil, err
	}
	if err := g.add(rows); err != nil {
		return nil, err
	}
	if err := loadSlugRange(ctx, g.q, root.Path, g.slugs); err != nil {
		return nil, err
	}
	if err := g.loadAncestors(ctx, []nodeRow{root}); err != nil {
		return nil, err
	}
	out := make([]*gnode, len(rows))
	for i, r := range rows {
		out[i] = g.nodes[r.ID]
	}
	return out, nil
}

// loadNodesWithAncestors loads the given nodes, their slugs and their ancestors.
func (g *graph) loadNodesWithAncestors(ctx context.Context, rows []nodeRow) error {
	fresh := make([]ids.UUID, 0, len(rows))
	for _, r := range rows {
		if _, ok := g.nodes[r.ID]; !ok {
			fresh = append(fresh, r.ID)
		}
	}
	if err := g.add(rows); err != nil {
		return err
	}
	missing, err := g.missingAncestors(rows)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		anc, err := loadNodes(ctx, g.q, missing)
		if err != nil {
			return err
		}
		if err := g.add(anc); err != nil {
			return err
		}
	}
	return loadSlugsOf(ctx, g.q, append(fresh, missing...), g.slugs)
}

// setSlug overrides a page's slug in a locale (publish checks run before the row is written).
func (g *graph) setSlug(node ids.UUID, locale string, s slugRow) {
	g.slugs[slugKey{node, locale}] = s
	clear(g.memo)
}

func (g *graph) slugOf(n *gnode, locale string) string {
	if s, ok := g.slugs[slugKey{n.ID, locale}]; ok && s.Slug != "" {
		return s.Slug
	}
	return g.slugs[slugKey{n.ID, n.MainLocale}].Slug
}

// pathOf is the path of a node in a locale: ancestors without a translation use their main-locale slug.
// Nodes whose ancestors have no slug have no path.
func (g *graph) pathOf(id ids.UUID, locale string) (string, bool) {
	k := pathKey{id, locale}
	if v, ok := g.memo[k]; ok {
		return v.path, v.ok
	}
	var v pathVal
	if n := g.nodes[id]; n != nil {
		if n.Parent == nil {
			v = pathVal{"/", true}
		} else if pp, ok := g.pathOf(*n.Parent, locale); ok {
			seg := ""
			if n.mount != nil {
				seg = n.mount.Segment
			} else {
				seg = g.slugOf(n, locale)
			}
			if seg != "" {
				v = pathVal{route.Join(pp, seg), true}
			}
		}
	}
	g.memo[k] = v
	return v.path, v.ok
}

// treeKey orders nodes depth-first: the sort keys from the root to the node.
func (g *graph) treeKey(id ids.UUID) string {
	if k, ok := g.order[id]; ok {
		return k
	}
	n := g.nodes[id]
	if n == nil {
		return ""
	}
	k := n.SortKey
	if n.Parent != nil {
		k = g.treeKey(*n.Parent) + "/" + n.SortKey
	}
	g.order[id] = k
	return k
}

// want is a route row the tree and content imply.
type want struct {
	routeRow
	page bool // a page (or vanity) route; pages win over mounted items
}

// pageWants lists the current and alias routes of the page nodes among nodes, in the given locales (nil: all).
func (g *graph) pageWants(nodes []*gnode, locales []string) []want {
	byNode := map[ids.UUID][]string{}
	for k := range g.slugs {
		byNode[k.Node] = append(byNode[k.Node], k.Locale)
	}
	var out []want
	for _, n := range nodes {
		if n.Kind != KindPage || n.Item == nil {
			continue
		}
		locs := byNode[n.ID]
		slices.Sort(locs)
		for _, loc := range locs {
			if locales != nil && !slices.Contains(locales, loc) {
				continue
			}
			if n.Parent != nil && g.slugs[slugKey{n.ID, loc}].Slug == "" {
				continue
			}
			p, ok := g.pathOf(n.ID, loc)
			if !ok {
				continue
			}
			if cp, err := route.Clean(p); err != nil || cp != p {
				if len(p) > route.MaxPath {
					g.tooLong = append(g.tooLong, want{page: true, routeRow: routeRow{
						routeKey: routeKey{n.Site, loc, p}, State: StateCurrent, Item: n.Item}})
				}
				continue
			}
			item, node := n.Item, n.ID
			out = append(out, want{page: true, routeRow: routeRow{
				routeKey: routeKey{n.Site, loc, p}, State: StateCurrent, Item: item, Node: &node}})
			if v := g.slugs[slugKey{n.ID, loc}].Vanity; v != "" && v != p {
				if cv, err := route.Clean(v); err == nil && cv != p {
					out = append(out, want{page: true, routeRow: routeRow{
						routeKey: routeKey{n.Site, loc, cv}, State: StateAlias, Item: item, Node: &node}})
				}
			}
		}
	}
	return out
}

// membership says a collection item matches a mount in a locale, at path rel below it.
type membership struct {
	Node   ids.UUID
	Item   ids.UUID
	Locale string
	Rel    string
	// Redirect is the target the item answers instead of itself ("" for none).
	Redirect string
}

// mountWants lists the routes of mounted items. An item matching several mounts of a site gets one current
// route (the canonical mount's, else the first in tree order) and aliases for the rest.
func (g *graph) mountWants(ms []membership, locales []string) []want {
	type group struct {
		site   string
		item   ids.UUID
		locale string
	}
	type cand struct {
		node     *gnode
		path     string
		order    string
		redirect string
	}
	groups := map[group][]cand{}
	for _, m := range ms {
		n := g.nodes[m.Node]
		if n == nil || n.mount == nil || (locales != nil && !slices.Contains(locales, m.Locale)) {
			continue
		}
		mp, ok := g.pathOf(m.Node, m.Locale)
		if !ok {
			continue
		}
		p := route.Join(mp, m.Rel)
		if cp, err := route.Clean(p); err != nil || cp != p {
			continue
		}
		k := group{n.Site, m.Item, m.Locale}
		groups[k] = append(groups[k], cand{n, p, g.treeKey(m.Node) + "|" + m.Node.String(), m.Redirect})
	}
	keys := make([]group, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b group) int {
		return cmp.Or(strings.Compare(a.site, b.site), strings.Compare(a.locale, b.locale), slices.Compare(a.item[:], b.item[:]))
	})
	var out []want
	for _, k := range keys {
		cs := groups[k]
		slices.SortFunc(cs, func(a, b cand) int { return strings.Compare(a.order, b.order) })
		canon := 0
		for i, c := range cs {
			if c.node.mount.Canonical {
				canon = i
				break
			}
		}
		for i, c := range cs {
			state := StateAlias
			if i == canon {
				state = StateCurrent
			}
			item, node := k.item, c.node.ID
			w := want{routeRow: routeRow{routeKey: routeKey{k.site, k.locale, c.path},
				State: state, Item: &item, Node: &node}}
			if c.redirect != "" {
				w.Target, w.Status = c.redirect, mountRedirectStatus(c.node.mount)
			}
			out = append(out, w)
		}
	}
	return out
}
