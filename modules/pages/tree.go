package pages

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// treeLevel is a tree node under construction.
type treeLevel struct {
	TreeNode
	row  nodeRow
	kids []*treeLevel
	// trashed marks a page whose item is in the trash: its node stays (restoring puts the page back) but the
	// tree does not list it.
	trashed bool
}

func (t *treeLevel) done() TreeNode {
	t.Nodes = nil
	for _, k := range t.kids {
		if !k.trashed {
			t.Nodes = append(t.Nodes, k.done())
		}
	}
	return t.TreeNode
}

// tree lists the children of parent. vis, when set, builds the filter a restricted reader sees through; it
// returns nil for readers that see everything.
func (s *service) tree(ctx context.Context, site, locale string, parent *ids.UUID, depth int, vis func(context.Context, store.SQL, string) (*visibility, error)) ([]TreeNode, error) {
	if err := s.checkSite(site); err != nil {
		return nil, err
	}
	if depth > maxTreeDepth {
		return nil, badRequest("depth is at most %d", maxTreeDepth)
	}
	depth = max(depth, 1)
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out []TreeNode
	err := s.db.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		q := tx.SQL()
		top, ok, err := s.treeTop(ctx, q, site, parent)
		if err != nil || !ok {
			return err
		}
		var v *visibility
		if vis != nil {
			if v, err = vis(ctx, q, site); err != nil {
				return err
			}
			if v != nil && !v.visible(ctx, top) {
				return notFound("page node %s", top.ID)
			}
		}
		g := newGraph(q)
		if err := g.loadNodesWithAncestors(ctx, []nodeRow{top}); err != nil {
			return err
		}
		root := &treeLevel{row: top}
		level := []*treeLevel{root}
		for d := 0; d < depth && len(level) > 0; d++ {
			if level, err = s.expandLevel(ctx, tx, g, locale, level, v); err != nil {
				return err
			}
		}
		out = make([]TreeNode, 0, len(root.kids))
		for _, k := range root.kids {
			if !k.trashed {
				out = append(out, k.done())
			}
		}
		return nil
	})
	return out, err
}

// treeTop finds the node whose children the tree lists; ok is false when the site has no root yet.
func (s *service) treeTop(ctx context.Context, q store.SQL, site string, parent *ids.UUID) (nodeRow, bool, error) {
	if parent != nil {
		n, err := loadNode(ctx, q, *parent)
		if err == nil && n.Site != site {
			err = notFound("page node %s in site %q", *parent, site)
		}
		return n, err == nil, err
	}
	n, err := scanNode(q.QueryRow(ctx, "SELECT "+nodeCols+" FROM page_node WHERE site = ? AND parent_id IS NULL", site))
	if isNoRows(err) {
		return n, false, nil
	}
	return n, err == nil, err
}

// expandLevel loads the children of every node in level, attaches them and returns them as the next level.
func (s *service) expandLevel(ctx context.Context, tx store.Tx, g *graph, locale string, level []*treeLevel, v *visibility) ([]*treeLevel, error) {
	q := tx.SQL()
	byID := make(map[ids.UUID]*treeLevel, len(level))
	parents := make([]ids.UUID, len(level))
	for i, p := range level {
		byID[p.row.ID] = p
		parents[i] = p.row.ID
	}
	var rows []nodeRow
	err := chunks(parents, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		got, err := queryNodes(ctx, q, "SELECT "+nodeCols+" FROM page_node WHERE parent_id IN "+ph+" ORDER BY sort_key, id", args...)
		rows = append(rows, got...)
		return err
	})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(rows, compareKeys)
	next := make([]*treeLevel, 0, len(rows))
	for _, r := range rows {
		permitted := v == nil || v.permitted(ctx, r)
		if !permitted && !v.context[r.ID] {
			continue
		}
		t := newLevel(r, !permitted)
		next = append(next, t)
		p := byID[*r.Parent]
		p.kids = append(p.kids, t)
	}
	if err := s.describe(ctx, tx, g, locale, next); err != nil {
		return nil, err
	}
	return next, nil
}

func newLevel(r nodeRow, restricted bool) *treeLevel {
	t := &treeLevel{row: r}
	t.Restricted = restricted
	t.ID, t.Site, t.Parent, t.Kind, t.Item, t.Hidden, t.Depth = r.ID, r.Site, r.Parent, r.Kind, r.Item, r.Hidden, r.Depth
	t.CreatedAt, t.UpdatedAt = r.Created, r.Updated
	return t
}

// treeRoot describes the site's root node; nil when the site has none or the reader may not see it.
func (s *service) treeRoot(ctx context.Context, site, locale string, vis func(context.Context, store.SQL, string) (*visibility, error)) (*TreeNode, error) {
	if err := s.checkSite(site); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out *TreeNode
	err := s.db.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		q := tx.SQL()
		top, ok, err := s.treeTop(ctx, q, site, nil)
		if err != nil || !ok {
			return err
		}
		permitted := true
		if vis != nil {
			v, err := vis(ctx, q, site)
			if err != nil {
				return err
			}
			if v != nil {
				if permitted = v.permitted(ctx, top); !permitted && !v.visible(ctx, top) {
					return nil
				}
			}
		}
		g := newGraph(q)
		if err := g.loadNodesWithAncestors(ctx, []nodeRow{top}); err != nil {
			return err
		}
		t := newLevel(top, !permitted)
		if err := s.describe(ctx, tx, g, locale, []*treeLevel{t}); err != nil {
			return err
		}
		out = &t.TreeNode
		return nil
	})
	return out, err
}

func compareKeys(a, b nodeRow) int {
	switch {
	case a.SortKey < b.SortKey:
		return -1
	case a.SortKey > b.SortKey:
		return 1
	}
	return slices.Compare(a.ID[:], b.ID[:])
}

// describe fills names, paths, statuses, child counts and link targets of a level.
func (s *service) describe(ctx context.Context, tx store.Tx, g *graph, locale string, level []*treeLevel) error {
	if len(level) == 0 {
		return nil
	}
	q, c := tx.SQL(), tx.Content()
	var pages, all []ids.UUID
	var mounts []nodeRow
	var hidden []*treeLevel
	level = slices.DeleteFunc(slices.Clone(level), func(t *treeLevel) bool {
		if t.Restricted {
			hidden = append(hidden, t)
		}
		return t.Restricted
	})
	if err := s.describeRestricted(ctx, q, locale, hidden); err != nil {
		return err
	}
	if len(level) == 0 {
		return nil
	}
	for _, t := range level {
		all = append(all, t.row.ID)
		switch t.row.Kind {
		case KindPage:
			pages = append(pages, t.row.ID)
		case KindMount:
			mounts = append(mounts, t.row)
		}
	}
	if len(mounts) > 0 {
		if err := g.loadNodesWithAncestors(ctx, mounts); err != nil {
			return err
		}
	}
	counts, err := childCounts(ctx, q, all)
	if err != nil {
		return err
	}
	names, paths, err := pageNamesAndPaths(ctx, q, pages, locale)
	if err != nil {
		return err
	}
	items, drafts, locales, err := s.levelItems(ctx, c, level)
	if err != nil {
		return err
	}
	linkPaths, err := s.linkTargets(ctx, q, g, locale, level)
	if err != nil {
		return err
	}
	for _, t := range level {
		t.Children = counts[t.row.ID]
		switch t.row.Kind {
		case KindPage:
			it, ok := items[*t.row.Item]
			if !ok {
				continue
			}
			t.trashed = it.DeletedAt != nil
			t.Type, t.Path = it.Type, paths[t.row.ID]
			t.Name = names[slugKey{t.row.ID, locale}]
			if t.Name == "" {
				t.Name = names[slugKey{t.row.ID, t.row.MainLocale}]
			}
			t.Locales, t.DraftParts = locales[it.ID], drafts[it.ID]
			t.Status = content.StatusDraft
			if it.PublishedVersion > 0 {
				t.Status = content.StatusPublished
				if len(drafts[it.ID]) > 0 {
					t.Status = content.StatusChanged
				}
			}
			if t.Name == "" {
				parts, err := c.GetDraftParts(ctx, it.ID)
				if err != nil {
					return err
				}
				t.Name = draftName(parts, locale, it.MainLocale)
			}
		case KindMount:
			spec := g.nodes[t.row.ID].mount
			t.Mount = spec
			t.Name = labelFor(spec.Label, locale, spec.Segment)
			t.Path, _ = g.pathOf(t.row.ID, locale)
		case KindLink:
			var spec LinkSpec
			if err := jsonUnmarshal(t.row.Data, &spec); err != nil {
				return err
			}
			t.Link = &spec
			t.Name = labelFor(spec.Label, locale, "")
			t.Target = linkPaths[t.row.ID]
			t.Broken = spec.Item == nil && spec.Node == nil && spec.URL == ""
		}
	}
	return nil
}

func childCounts(ctx context.Context, q store.SQL, parents []ids.UUID) (map[ids.UUID]int, error) {
	counts := map[ids.UUID]int{}
	err := chunks(parents, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		return each(ctx, q, "SELECT parent_id, COUNT(*) FROM page_node WHERE parent_id IN "+ph+" GROUP BY parent_id", args,
			func(r store.Rows) error {
				var id ids.UUID
				var n int
				err := r.Scan(&id, &n)
				counts[id] = n
				return err
			})
	})
	return counts, err
}

// pageNamesAndPaths reads the published names (in the locale and the page's main locale) and current paths
// of page nodes.
func pageNamesAndPaths(ctx context.Context, q store.SQL, pages []ids.UUID, locale string) (map[slugKey]string, map[ids.UUID]string, error) {
	names := map[slugKey]string{}
	paths := map[ids.UUID]string{}
	err := chunks(pages, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		err := each(ctx, q, `SELECT s.node_id, s.locale, s.name FROM page_slug s JOIN page_node n ON n.id = s.node_id
WHERE s.node_id IN `+ph+` AND (s.locale = ? OR s.locale = n.main_locale)`, append(args, locale),
			func(r store.Rows) error {
				var k slugKey
				var name string
				err := r.Scan(&k.Node, &k.Locale, &name)
				names[k] = name
				return err
			})
		if err != nil {
			return err
		}
		ph, args = inArgs(part, locale)
		return each(ctx, q, "SELECT node_id, path FROM route WHERE state = 'current' AND locale = ? AND node_id IN "+ph, args,
			func(r store.Rows) error {
				var id ids.UUID
				var p string
				err := r.Scan(&id, &p)
				paths[id] = p
				return err
			})
	})
	return names, paths, err
}

// levelItems reads the items behind a level's page nodes with their draft parts and published locales.
func (s *service) levelItems(ctx context.Context, c store.ContentStore, level []*treeLevel) (map[ids.UUID]store.Item, map[ids.UUID][]string, map[ids.UUID][]string, error) {
	var items []ids.UUID
	for _, t := range level {
		if t.row.Item != nil {
			items = append(items, *t.row.Item)
		}
	}
	rows, err := c.GetItems(ctx, items)
	if err != nil {
		return nil, nil, nil, err
	}
	byID := make(map[ids.UUID]store.Item, len(rows))
	all := make([]ids.UUID, 0, len(rows))
	for _, it := range rows {
		byID[it.ID] = it
		all = append(all, it.ID)
	}
	drafts := map[ids.UUID][]string{}
	locales := map[ids.UUID][]string{}
	err = chunks(all, func(part []ids.UUID) error {
		d, err := c.DraftPartsOf(ctx, part)
		if err != nil {
			return err
		}
		maps.Copy(drafts, d)
		l, err := c.PublishedLocalesOf(ctx, part)
		maps.Copy(locales, l)
		return err
	})
	return byID, drafts, locales, err
}

func draftName(parts []store.DraftPart, locale, main string) string {
	for _, want := range []string{locale, main} {
		for _, p := range parts {
			if p.Part == want && p.Name != "" {
				return p.Name
			}
		}
	}
	for _, p := range parts {
		if p.Part != "" && p.Name != "" {
			return p.Name
		}
	}
	return ""
}

// labelFor picks a label by locale, else the first by locale code, else the fallback.
func labelFor(labels map[string]string, locale, fallback string) string {
	if l := labels[locale]; l != "" {
		return l
	}
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		if labels[k] != "" {
			return labels[k]
		}
	}
	return fallback
}

// linkTargets resolves the link nodes of a level to a path or URL in the locale.
func (s *service) linkTargets(ctx context.Context, q store.SQL, g *graph, locale string, level []*treeLevel) (map[ids.UUID]string, error) {
	out := map[ids.UUID]string{}
	var itemIDs, nodeIDs []ids.UUID
	specs := map[ids.UUID]LinkSpec{}
	for _, t := range level {
		if t.row.Kind != KindLink {
			continue
		}
		var spec LinkSpec
		if err := jsonUnmarshal(t.row.Data, &spec); err != nil {
			return nil, err
		}
		specs[t.row.ID] = spec
		switch {
		case spec.Item != nil:
			itemIDs = append(itemIDs, *spec.Item)
		case spec.Node != nil:
			nodeIDs = append(nodeIDs, *spec.Node)
		}
	}
	if len(specs) == 0 {
		return out, nil
	}
	itemPath := map[ids.UUID]string{}
	err := chunks(itemIDs, func(part []ids.UUID) error {
		ph, args := inArgs(part, locale)
		return each(ctx, q, "SELECT item_id, path FROM route WHERE state = 'current' AND locale = ? AND item_id IN "+ph+
			" ORDER BY site, path", args, func(r store.Rows) error {
			var id ids.UUID
			var p string
			if err := r.Scan(&id, &p); err != nil {
				return err
			}
			if _, ok := itemPath[id]; !ok {
				itemPath[id] = p
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	nodePath := map[ids.UUID]string{}
	if len(nodeIDs) > 0 {
		rows, err := loadNodes(ctx, q, nodeIDs)
		if err != nil {
			return nil, err
		}
		if err := g.loadNodesWithAncestors(ctx, rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			if p, ok := g.pathOf(r.ID, locale); ok && (r.Kind == KindMount || g.slugs[slugKey{r.ID, locale}].Slug != "" || r.Parent == nil) {
				nodePath[r.ID] = p
			}
		}
	}
	for id, spec := range specs {
		switch {
		case spec.Item != nil:
			out[id] = itemPath[*spec.Item]
		case spec.Node != nil:
			out[id] = nodePath[*spec.Node]
		default:
			out[id] = spec.URL
		}
	}
	return out, nil
}

// describeRestricted fills what a context ancestor shows: its name. The child count is left out, since it would
// count children the reader cannot see.
func (s *service) describeRestricted(ctx context.Context, q store.SQL, locale string, level []*treeLevel) error {
	if len(level) == 0 {
		return nil
	}
	var pages []ids.UUID
	for _, t := range level {
		if t.row.Kind == KindPage {
			pages = append(pages, t.row.ID)
		}
	}
	names, _, err := pageNamesAndPaths(ctx, q, pages, locale)
	if err != nil {
		return err
	}
	for _, t := range level {
		t.Item, t.Hidden, t.CreatedAt, t.UpdatedAt = nil, false, time.Time{}, time.Time{}
		switch t.row.Kind {
		case KindPage:
			t.Name = names[slugKey{t.row.ID, locale}]
			if t.Name == "" {
				t.Name = names[slugKey{t.row.ID, t.row.MainLocale}]
			}
		case KindMount:
			var spec MountSpec
			if err := jsonUnmarshal(t.row.Data, &spec); err != nil {
				return err
			}
			t.Name = labelFor(spec.Label, locale, spec.Segment)
		case KindLink:
			var spec LinkSpec
			if err := jsonUnmarshal(t.row.Data, &spec); err != nil {
				return err
			}
			t.Name = labelFor(spec.Label, locale, "")
		}
	}
	return nil
}
