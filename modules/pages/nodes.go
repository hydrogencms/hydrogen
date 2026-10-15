package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/frac"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
	"github.com/hydrogencms/hydrogen/store"
)

// public converts a node row to the API type.
func (n nodeRow) public() (*Node, error) {
	out := &Node{ID: n.ID, Site: n.Site, Parent: n.Parent, Kind: n.Kind, Item: n.Item, Hidden: n.Hidden, Depth: n.Depth,
		CreatedAt: n.Created, UpdatedAt: n.Updated}
	switch n.Kind {
	case KindMount:
		out.Mount = &MountSpec{}
		if err := json.Unmarshal(n.Data, out.Mount); err != nil {
			return nil, fmt.Errorf("pages: mount %s: bad data: %w", n.ID, err)
		}
	case KindLink:
		out.Link = &LinkSpec{}
		if err := json.Unmarshal(n.Data, out.Link); err != nil {
			return nil, fmt.Errorf("pages: link %s: bad data: %w", n.ID, err)
		}
	}
	return out, nil
}

func (s *service) node(ctx context.Context, id ids.UUID) (*Node, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out *Node
	err := s.db.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		n, err := loadNode(ctx, tx.SQL(), id)
		if err != nil {
			return err
		}
		if err := s.authorize(ctx, policyRead, n); err != nil {
			return err
		}
		out, err = n.public()
		return err
	})
	return out, err
}

// parentFor loads and checks a parent: a page node of the site.
func parentFor(ctx context.Context, q store.SQL, site string, id ids.UUID) (nodeRow, error) {
	p, err := loadNode(ctx, q, id)
	switch {
	case err != nil:
		return p, err
	case p.Site != site:
		return p, badRequest("parent %s belongs to site %q, not %q", id, p.Site, site)
	case p.Kind != KindPage:
		return p, badRequest("parent %s is a %s node; only pages can have children", id, p.Kind)
	}
	return p, nil
}

// allowsChild applies the parent type's `children` option to a child page type.
func (s *service) allowsChild(ctx context.Context, c store.ContentStore, parent nodeRow, childType string) error {
	if parent.Item == nil {
		return nil
	}
	pit, err := c.GetItem(ctx, *parent.Item, false)
	if err != nil {
		return fmt.Errorf("pages: read parent item: %w", err)
	}
	t, ok := s.schema.Type(pit.Type)
	if !ok {
		return nil
	}
	list, ok := t.Options[childrenOption].([]any)
	if !ok {
		return nil
	}
	for _, e := range list {
		if e == childType {
			return nil
		}
	}
	return badRequest("a %s page cannot have %s children (allowed: %s)", pit.Type, childType, joinNames(list))
}

func joinNames(list []any) string {
	if len(list) == 0 {
		return "none"
	}
	names := make([]string, len(list))
	for i, e := range list {
		names[i] = fmt.Sprint(e)
	}
	return strings.Join(names, ", ")
}

// sortKeyFor picks the sort key that puts a node under parent right after the sibling `after` (nil: first).
// self is excluded from the siblings when it is being moved.
func sortKeyFor(ctx context.Context, q store.SQL, parent *ids.UUID, self ids.UUID, after *ids.UUID) (string, error) {
	if parent == nil {
		return frac.Between("", "")
	}
	prev := ""
	if after != nil {
		sib, err := loadNode(ctx, q, *after)
		if err != nil {
			return "", err
		}
		if sib.Parent == nil || *sib.Parent != *parent || sib.ID == self {
			return "", badRequest("%s is not a sibling to place the node after", after)
		}
		prev = sib.SortKey
	}
	next := ""
	err := q.QueryRow(ctx, "SELECT sort_key FROM page_node WHERE parent_id = ? AND id <> ? AND sort_key > ? ORDER BY sort_key LIMIT 1",
		*parent, self, prev).Scan(&next)
	if err != nil && !isNoRows(err) {
		return "", err
	}
	key, err := frac.Between(prev, next)
	if err != nil {
		return "", fmt.Errorf("pages: sort key: %w", err)
	}
	return key, nil
}

func (s *service) insertNode(ctx context.Context, q store.SQL, n nodeRow) error {
	_, err := q.Exec(ctx, "INSERT INTO page_node ("+nodeCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		n.ID, n.Site, n.Parent, n.Path, n.Depth, n.SortKey, n.Kind, n.Item, n.Hidden, rawOrNil(n.Data), n.MainLocale,
		n.Collection, n.Created, n.Updated)
	return err
}

func rawOrNil(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// newNode builds a node row under parent (nil: a root).
func (s *service) newNode(site string, parent *nodeRow, kind string, key string) nodeRow {
	id := s.ids.New()
	now := s.clock.Now()
	n := nodeRow{ID: id, Site: site, Kind: kind, SortKey: key, Path: "/" + id.String() + "/", Created: now, Updated: now}
	if parent != nil {
		n.Parent = &parent.ID
		n.Path = ancestryPath(parent.Path, id)
		n.Depth = parent.Depth + 1
	}
	return n
}

// maxNodeDepth caps the page tree; the root is at depth 0.
const maxNodeDepth = 32

// placeable loads the page item a place call puts into the tree and checks nothing else holds it.
func (s *service) placeable(ctx context.Context, tx store.Tx, id ids.UUID) (store.Item, error) {
	it, err := tx.Content().GetItem(ctx, id, false)
	if isNoRows(err) {
		return it, notFound("item %s", id)
	}
	if err != nil {
		return it, err
	}
	if !s.isPageType(it.Type) {
		return it, badRequest("item %s is a %s, which is not a page type", id, it.Type)
	}
	if _, placed, err := loadNodeByItem(ctx, tx.SQL(), it.ID); err != nil {
		return it, err
	} else if placed {
		return it, conflict("item %s is already in the page tree", it.ID)
	}
	return it, nil
}

// placeParent finds and checks the parent a page goes under; nil means the site's root, which must not exist.
func (s *service) placeParent(ctx context.Context, tx store.Tx, site string, id *ids.UUID, it store.Item) (*nodeRow, error) {
	q := tx.SQL()
	if id == nil {
		var roots int
		if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM page_node WHERE site = ? AND parent_id IS NULL", site).Scan(&roots); err != nil {
			return nil, err
		}
		if roots > 0 {
			return nil, conflict("site %q already has a root page", site)
		}
		return nil, nil
	}
	p, err := parentFor(ctx, q, site, *id)
	if err != nil {
		return nil, err
	}
	if p.Depth+1 > maxNodeDepth {
		return nil, badRequest("the page tree is at most %d levels deep", maxNodeDepth)
	}
	if err := s.allowsChild(ctx, tx.Content(), p, it.Type); err != nil {
		return nil, err
	}
	return &p, nil
}

func tooLongError(res subtreeResult) error {
	if len(res.tooLong) == 0 {
		return nil
	}
	w := res.tooLong[0]
	return badRequest("a page path in %q would be %d bytes (%d pages affected); paths are at most %d", w.Locale, len(w.Path),
		len(res.tooLong), route.MaxPath)
}

func (s *service) place(ctx context.Context, site string, in Place) (*Node, error) {
	if err := s.checkSite(site); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out *Node
	err := s.structural(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		if err := s.lockItems(ctx, tx, []ids.UUID{in.Item}); err != nil {
			return err
		}
		if err := s.lockSite(ctx, tx, site); err != nil {
			return err
		}
		q := tx.SQL()
		if err := s.authorizeParent(ctx, q, site, in.Parent, policyEdit); err != nil {
			return err
		}
		if err := s.authorizePlace(ctx, tx, in.Item, site, s.pathOfParent(ctx, q, in.Parent)); err != nil {
			return err
		}
		it, err := s.placeable(ctx, tx, in.Item)
		if err != nil {
			return err
		}
		parent, err := s.placeParent(ctx, tx, site, in.Parent, it)
		if err != nil {
			return err
		}
		var parentID *ids.UUID
		if parent != nil {
			parentID = &parent.ID
		}
		key, err := sortKeyFor(ctx, q, parentID, ids.Nil, in.After)
		if err != nil {
			return err
		}
		n := s.newNode(site, parent, KindPage, key)
		n.Item, n.MainLocale = &it.ID, it.MainLocale
		if err := s.insertNode(ctx, q, n); err != nil {
			return placeErr(err, site)
		}
		if err := s.reindexItems(ctx, tx, []ids.UUID{it.ID}); err != nil {
			return err
		}
		docs, err := publishedDocs(ctx, tx.Content(), it)
		if err != nil {
			return err
		}
		for _, d := range docs {
			if err := putSlug(ctx, q, n.ID, d.Locale, s.slugRowOf(it.Type, d.Doc)); err != nil {
				return fmt.Errorf("pages: save slug: %w", err)
			}
		}
		res, err := s.recomputeSubtree(ctx, q, n, nil)
		if err != nil {
			return err
		}
		if err := tooLongError(res); err != nil {
			return err
		}
		if err := s.record(ctx, tx, "page.place", n, map[string]any{"item": it.ID}); err != nil {
			return err
		}
		out, err = n.public()
		return err
	})
	return out, err
}

func placeErr(err error, site string) error {
	if isDuplicate(err) {
		return conflict("site %q already has a root page, or the item is already placed", site)
	}
	return fmt.Errorf("pages: insert node: %w", err)
}

func (s *service) addMount(ctx context.Context, site string, in Mount) (*Node, error) {
	if err := s.checkSite(site); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	if s.restricted(ctx) {
		if err := s.svcs.Access.Holds(ctx, policyMount); err != nil {
			return nil, err
		}
	}
	spec := in.Spec
	if _, err := s.checkMountSpec(&spec); err != nil {
		return nil, err
	}
	var out *Node
	err := s.structural(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		members, err := s.membershipItems(ctx, tx, &spec)
		if err != nil {
			return err
		}
		if err := s.lockItems(ctx, tx, members); err != nil {
			return err
		}
		if err := s.lockSite(ctx, tx, site); err != nil {
			return err
		}
		q := tx.SQL()
		if err := s.authorizeParent(ctx, q, site, &in.Parent, policyEdit, policyMount); err != nil {
			return err
		}
		if err := s.readsAll(ctx, spec.Collection); err != nil {
			return err
		}
		parent, err := parentFor(ctx, q, site, in.Parent)
		if err != nil {
			return err
		}
		if err := checkSegmentFree(ctx, q, parent.ID, spec.Segment, ids.Nil); err != nil {
			return err
		}
		key, err := sortKeyFor(ctx, q, &parent.ID, ids.Nil, in.After)
		if err != nil {
			return err
		}
		n := s.newNode(site, &parent, KindMount, key)
		n.Data, n.Collection = mustJSON(spec), spec.Collection
		if err := s.insertNode(ctx, q, n); err != nil {
			return fmt.Errorf("pages: insert node: %w", err)
		}
		if err := s.rebuildMembership(ctx, tx, n, &spec); err != nil {
			return err
		}
		if err := s.reindexMount(ctx, tx, n.ID, nil); err != nil {
			return err
		}
		if err := s.recomputeCollection(ctx, q, site, spec.Collection, nil, nil); err != nil {
			return err
		}
		if err := s.record(ctx, tx, "page.mount", n, map[string]any{"collection": spec.Collection, "segment": spec.Segment}); err != nil {
			return err
		}
		out, err = n.public()
		return err
	})
	return out, err
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("pages: encode %T: %v", v, err))
	}
	return b
}

// checkSegmentFree fails when another mount under the parent already uses the segment.
func checkSegmentFree(ctx context.Context, q store.SQL, parent ids.UUID, segment string, self ids.UUID) error {
	taken := false
	err := each(ctx, q, "SELECT id, data FROM page_node WHERE parent_id = ? AND kind = 'mount'", []any{parent}, func(r store.Rows) error {
		var id ids.UUID
		var data json.RawMessage
		if err := r.Scan(&id, &data); err != nil {
			return err
		}
		var spec MountSpec
		if err := json.Unmarshal(data, &spec); err != nil {
			return err
		}
		if id != self && spec.Segment == segment {
			taken = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if taken {
		return conflict("another mount under this page already uses the segment %q", segment)
	}
	return nil
}

func (s *service) checkLinkSpec(ctx context.Context, q store.SQL, c store.ContentStore, site string, spec *LinkSpec) error {
	targets := 0
	if spec.Item != nil {
		targets++
	}
	if spec.Node != nil {
		targets++
	}
	if spec.URL != "" {
		targets++
	}
	if targets != 1 {
		return badRequest("a link needs exactly one target: item, node or url")
	}
	if !slices.ContainsFunc(slices.Collect(maps.Values(spec.Label)), func(v string) bool { return v != "" }) {
		return badRequest("a link needs a label")
	}
	if err := checkLabels(spec.Label); err != nil {
		return err
	}
	switch {
	case spec.Item != nil:
		if _, err := c.GetItem(ctx, *spec.Item, false); isNoRows(err) {
			return notFound("item %s", spec.Item)
		} else if err != nil {
			return err
		}
	case spec.Node != nil:
		t, err := loadNode(ctx, q, *spec.Node)
		if err != nil {
			return err
		}
		if t.Site != site {
			return badRequest("the link target belongs to site %q, not %q", t.Site, site)
		}
	default:
		return checkLinkURL(spec.URL)
	}
	return nil
}

// checkLinkURL accepts a site path or an absolute http(s) URL, without characters browsers read as another
// URL (`/\host`).
func checkLinkURL(raw string) error {
	if unsafeText(raw) {
		return badRequest("link url %q cannot contain control characters or backslashes", raw)
	}
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.ContainsAny(raw, " \t\r\n") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return badRequest("link url %q must be an absolute http(s) URL or a site path starting with /", raw)
	}
	return nil
}

// setLinkTargets records what a link node points at, so the pointers can be cleared when the target goes.
func setLinkTargets(ctx context.Context, q store.SQL, id ids.UUID, spec LinkSpec) error {
	_, err := q.Exec(ctx, "UPDATE page_node SET link_item = ?, link_node = ? WHERE id = ?", spec.Item, spec.Node, id)
	return err
}

func (s *service) addLink(ctx context.Context, site string, in Link) (*Node, error) {
	if err := s.checkSite(site); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out *Node
	err := s.structural(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		if err := s.lockSite(ctx, tx, site); err != nil {
			return err
		}
		q := tx.SQL()
		if err := s.authorizeParent(ctx, q, site, &in.Parent, policyEdit); err != nil {
			return err
		}
		parent, err := parentFor(ctx, q, site, in.Parent)
		if err != nil {
			return err
		}
		if err := s.checkLinkSpec(ctx, q, tx.Content(), site, &in.Spec); err != nil {
			return err
		}
		key, err := sortKeyFor(ctx, q, &parent.ID, ids.Nil, in.After)
		if err != nil {
			return err
		}
		n := s.newNode(site, &parent, KindLink, key)
		n.Data = mustJSON(in.Spec)
		if err := s.insertNode(ctx, q, n); err != nil {
			return fmt.Errorf("pages: insert node: %w", err)
		}
		if err := setLinkTargets(ctx, q, n.ID, in.Spec); err != nil {
			return err
		}
		if err := s.record(ctx, tx, "page.link", n, nil); err != nil {
			return err
		}
		out, err = n.public()
		return err
	})
	return out, err
}

// lockedNode loads a node, locks its site and reads the node again, which may have moved or gone meanwhile.
func (s *service) lockedNode(ctx context.Context, tx store.Tx, id ids.UUID) (nodeRow, error) {
	n, err := loadNode(ctx, tx.SQL(), id)
	if err != nil {
		return n, err
	}
	if err := s.lockSite(ctx, tx, n.Site); err != nil {
		return n, err
	}
	return loadNode(ctx, tx.SQL(), id)
}

func (s *service) update(ctx context.Context, id ids.UUID, in NodeUpdate) (*Node, error) {
	if in.Move == nil && in.Hidden == nil && in.Mount == nil && in.Link == nil {
		return nil, badRequest("nothing to update")
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out *Node
	err := s.structural(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		q := tx.SQL()
		if err := s.lockForUpdate(ctx, tx, id, in); err != nil {
			return err
		}
		n, err := s.lockedNode(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.authorize(ctx, policyEdit, n); err != nil {
			return err
		}
		var did []string
		if in.Move != nil {
			if n, err = s.move(ctx, tx, n, *in.Move); err != nil {
				return err
			}
			did = append(did, "move")
		}
		if in.Hidden != nil {
			if _, err := q.Exec(ctx, "UPDATE page_node SET hidden = ?, updated_at = ? WHERE id = ?", *in.Hidden, s.clock.Now(), n.ID); err != nil {
				return err
			}
			did = append(did, "hidden")
		}
		if in.Mount != nil {
			if n.Kind != KindMount {
				return badRequest("node %s is a %s, not a mount", id, n.Kind)
			}
			if err := s.updateMount(ctx, tx, n, *in.Mount); err != nil {
				return err
			}
			did = append(did, "mount")
		}
		if in.Link != nil {
			if err := s.updateLink(ctx, tx, n, *in.Link); err != nil {
				return err
			}
			did = append(did, "link")
		}
		if n, err = loadNode(ctx, q, id); err != nil {
			return err
		}
		action := "page.update"
		if len(did) == 1 && did[0] == "move" {
			action = "page.move"
		}
		if err := s.record(ctx, tx, action, n, map[string]any{"changed": did}); err != nil {
			return err
		}
		out, err = n.public()
		return err
	})
	return out, err
}

func (s *service) updateLink(ctx context.Context, tx store.Tx, n nodeRow, spec LinkSpec) error {
	q := tx.SQL()
	if n.Kind != KindLink {
		return badRequest("node %s is a %s, not a link", n.ID, n.Kind)
	}
	if err := s.checkLinkSpec(ctx, q, tx.Content(), n.Site, &spec); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, "UPDATE page_node SET data = ?, updated_at = ? WHERE id = ?", mustJSON(spec), s.clock.Now(), n.ID); err != nil {
		return err
	}
	return setLinkTargets(ctx, q, n.ID, spec)
}

// checkMoveTarget validates moving n under the parent np and returns np's row: not under itself, within the
// depth limit, allowed by the parent's type, and no mount segment clash.
func (s *service) checkMoveTarget(ctx context.Context, tx store.Tx, n nodeRow, np nodeRow) error {
	q := tx.SQL()
	if strings.HasPrefix(np.Path, n.Path) {
		return badRequest("a node cannot be moved under itself")
	}
	var deepest int
	err := q.QueryRow(ctx, "SELECT MAX(depth) FROM page_node WHERE path >= ? AND path < ?", n.Path, pathUpper(n.Path)).Scan(&deepest)
	if err != nil {
		return err
	}
	if deepest-n.Depth+np.Depth+1 > maxNodeDepth {
		return badRequest("the page tree is at most %d levels deep", maxNodeDepth)
	}
	switch n.Kind {
	case KindPage:
		if n.Item == nil {
			return nil
		}
		it, err := tx.Content().GetItem(ctx, *n.Item, false)
		if err != nil {
			return fmt.Errorf("pages: read item: %w", err)
		}
		return s.allowsChild(ctx, tx.Content(), np, it.Type)
	case KindMount:
		spec, err := mountSpecOf(n)
		if err != nil {
			return err
		}
		return checkSegmentFree(ctx, q, np.ID, spec.Segment, n.ID)
	}
	return nil
}

// move reparents and/or reorders a node and reconciles the routes of what moved. A pure reorder changes no
// path; only the mounts below the node are recomputed, since the first mount in tree order is canonical.
func (s *service) move(ctx context.Context, tx store.Tx, n nodeRow, mv Move) (nodeRow, error) {
	q := tx.SQL()
	if n.Parent == nil {
		return n, badRequest("the root page cannot be moved")
	}
	np, err := parentFor(ctx, q, n.Site, mv.Parent)
	if err != nil {
		return n, err
	}
	if err := s.authorize(ctx, policyEdit, np); err != nil {
		return n, err
	}
	if err := s.checkMoveTarget(ctx, tx, n, np); err != nil {
		return n, err
	}
	key, err := sortKeyFor(ctx, q, &np.ID, n.ID, mv.After)
	if err != nil {
		return n, err
	}
	reparent := np.ID != *n.Parent
	if reparent {
		newPath := ancestryPath(np.Path, n.ID)
		_, err := q.Exec(ctx, "UPDATE page_node SET path = ? || substr(path, ?), depth = depth + ? WHERE path >= ? AND path < ?",
			newPath, len(n.Path)+1, np.Depth+1-n.Depth, n.Path, pathUpper(n.Path))
		if err != nil {
			return n, fmt.Errorf("pages: move subtree: %w", err)
		}
	}
	if _, err := q.Exec(ctx, "UPDATE page_node SET parent_id = ?, sort_key = ?, updated_at = ? WHERE id = ?", np.ID, key, s.clock.Now(), n.ID); err != nil {
		return n, err
	}
	if n, err = loadNode(ctx, q, n.ID); err != nil {
		return n, err
	}
	if reparent {
		if err := s.reindexSubtree(ctx, tx, n); err != nil {
			return n, err
		}
	}
	switch {
	case n.Kind == KindMount:
		return n, s.recomputeCollection(ctx, q, n.Site, n.Collection, nil, nil)
	case n.Kind == KindPage && reparent:
		res, err := s.recomputeSubtree(ctx, q, n, nil)
		if err != nil {
			return n, err
		}
		return n, tooLongError(res)
	case n.Kind == KindPage:
		return n, s.recomputeMountsBelow(ctx, q, n)
	}
	return n, nil
}

func mountSpecOf(n nodeRow) (MountSpec, error) {
	var spec MountSpec
	if err := json.Unmarshal(n.Data, &spec); err != nil {
		return spec, fmt.Errorf("pages: mount %s: bad data: %w", n.ID, err)
	}
	return spec, nil
}

// recomputeMountsBelow recomputes the collections mounted at or below a node.
func (s *service) recomputeMountsBelow(ctx context.Context, q store.SQL, n nodeRow) error {
	var colls []string
	err := each(ctx, q, "SELECT DISTINCT collection FROM page_node WHERE path >= ? AND path < ? AND kind = 'mount' ORDER BY collection",
		[]any{n.Path, pathUpper(n.Path)}, func(r store.Rows) error {
			var c string
			err := r.Scan(&c)
			colls = append(colls, c)
			return err
		})
	if err != nil {
		return err
	}
	for _, c := range colls {
		if err := s.recomputeCollection(ctx, q, n.Site, c, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

// updateMount replaces a mount's spec and reconciles its items' routes.
func (s *service) updateMount(ctx context.Context, tx store.Tx, n nodeRow, spec MountSpec) error {
	q := tx.SQL()
	if _, err := s.checkMountSpec(&spec); err != nil {
		return err
	}
	old, err := mountSpecOf(n)
	if err != nil {
		return err
	}
	if n.Parent != nil {
		if err := checkSegmentFree(ctx, q, *n.Parent, spec.Segment, n.ID); err != nil {
			return err
		}
	}
	rebuild := old.Collection != spec.Collection || old.Route != spec.Route || !equalFilters(old.Filter, spec.Filter)
	if old.Collection != spec.Collection || !equalFilters(old.Filter, spec.Filter) {
		if err := s.authorize(ctx, policyMount, n); err != nil {
			return err
		}
		if err := s.readsAll(ctx, spec.Collection); err != nil {
			return err
		}
	}
	var before []ids.UUID
	if rebuild {
		if before, err = mountItemsOf(ctx, q, n.ID); err != nil {
			return err
		}
	}
	if old.Collection != spec.Collection {
		if _, err := q.Exec(ctx, "DELETE FROM page_mount_item WHERE node_id = ?", n.ID); err != nil {
			return err
		}
		if err := s.recomputeCollection(ctx, q, n.Site, old.Collection, nil, nil); err != nil {
			return err
		}
	}
	_, err = q.Exec(ctx, "UPDATE page_node SET data = ?, collection = ?, updated_at = ? WHERE id = ?",
		mustJSON(spec), spec.Collection, s.clock.Now(), n.ID)
	if err != nil {
		return err
	}
	if rebuild {
		if err := s.rebuildMembership(ctx, tx, n, &spec); err != nil {
			return err
		}
		if err := s.reindexMount(ctx, tx, n.ID, before); err != nil {
			return err
		}
	}
	return s.recomputeCollection(ctx, q, n.Site, spec.Collection, nil, nil)
}

func equalFilters(a, b map[string]any) bool {
	return string(mustJSON(a)) == string(mustJSON(b))
}

func (s *service) deleteNode(ctx context.Context, id ids.UUID) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	return s.structural(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		q := tx.SQL()
		if pre, err := mountItemsOf(ctx, q, id); err != nil {
			return err
		} else if err := s.lockItems(ctx, tx, pre); err != nil {
			return err
		}
		n, err := s.lockedNode(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := s.authorize(ctx, policyEdit, n); err != nil {
			return err
		}
		if n.Kind == KindPage {
			return badRequest("page nodes go away when their item is deleted")
		}
		data := map[string]any{"kind": n.Kind}
		var mounted []ids.UUID
		if n.Kind == KindMount {
			if mounted, err = mountItemsOf(ctx, q, n.ID); err != nil {
				return err
			}
			for _, stmt := range []string{"DELETE FROM route WHERE node_id = ?", "DELETE FROM page_mount_item WHERE node_id = ?"} {
				if _, err := q.Exec(ctx, stmt, n.ID); err != nil {
					return err
				}
			}
			data["collection"] = n.Collection
		}
		if _, err := q.Exec(ctx, "DELETE FROM page_node WHERE id = ?", n.ID); err != nil {
			return err
		}
		if err := s.reindexItems(ctx, tx, mounted); err != nil {
			return err
		}
		changes, err := s.clearLinks(ctx, q, "link_node", n.ID)
		if err != nil {
			return err
		}
		if err := s.emitChanges(ctx, tx, changes); err != nil {
			return err
		}
		if n.Kind == KindMount {
			if err := s.recomputeCollection(ctx, q, n.Site, n.Collection, nil, nil); err != nil {
				return err
			}
		}
		return s.record(ctx, tx, "page.delete", n, data)
	})
}

// pathOfParent is the ancestry path of a parent node, "" for a site's root or a missing node.
func (s *service) pathOfParent(ctx context.Context, q store.SQL, id *ids.UUID) string {
	if id == nil {
		return ""
	}
	n, err := loadNode(ctx, q, *id)
	if err != nil {
		return ""
	}
	return n.Path
}
