package pages

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/store"
)

// Policies and limitations the module registers.
const (
	policyRead   = "pages.read"
	policyEdit   = "pages.edit"
	policyMount  = "pages.mount"
	limitSite    = "pages.site"
	limitSubtree = "pages.subtree"
)

// Target.Extra keys of pages policy checks. A target carrying extraSite is about a tree node; any other target
// is about a content item (content.* policies).
//
//	pages.site  string  the site acted on
//	pages.site  string  the site acted on
//	pages.path  string  ancestry path of the node acted on ("/<id>/<id>/", the node last); for creating and moving
//	                    nodes it is the destination parent's, "" for a site's root
//
// A content target may carry the destination an unplaced item is being placed at; its limitations are then
// evaluated there instead of at the item's placements:
//
//	pages.dest.site  string  the destination site
//	pages.dest.path  string  the destination parent's ancestry path, "" for a site's root
const (
	extraSite     = "pages.site"
	extraPath     = "pages.path"
	extraDestSite = "pages.dest.site"
	extraDestPath = "pages.dest.path"
)

// Module index fields (module "pages") that scope content listings.
const (
	indexModule   = "pages"
	fieldSite     = "pages.site"     // text: every site the item is placed in
	fieldAncestor = "pages.ancestor" // ref: every node id on the path of every placement, the node itself included
)

func (m *module) registerAccess(r *kernel.Registry) error {
	return RegisterAccess(r, m.svc, m.cfg.Sites)
}

// AccessRegistrar is what RegisterAccess declares on: the kernel registry, or an adapter over an
// access.Catalog to build a Checker outside the kernel.
type AccessRegistrar interface {
	Policy(access.Policy) error
	Limitation(access.Limitation) error
	PolicyLimitation(policy, limitation string) error
}

// RegisterAccess declares the pages policies and limitations, and applies the limitations to the content
// policies. The module calls it with the kernel registry.
func RegisterAccess(r AccessRegistrar, svc *Service, sites []string) error {
	get := svc.get
	for _, p := range []access.Policy{
		{Name: policyRead, Description: "Read the page tree: sites, nodes, paths, resolution", Limitations: []string{limitSite, limitSubtree}},
		{Name: policyEdit, Description: "Add, move, reorder, update and delete page tree nodes", Limitations: []string{limitSite, limitSubtree}},
		{Name: policyMount, Description: "Mount collections in the page tree: gives access to every item of the collection", Limitations: []string{limitSite, limitSubtree}},
	} {
		if err := r.Policy(p); err != nil {
			return err
		}
	}
	for _, l := range []access.Limitation{siteLimit{sites, get}, subtreeLimit{get}} {
		if err := r.Limitation(l); err != nil {
			return err
		}
	}
	for _, pol := range []string{access.PolicyContentRead, access.PolicyContentCreate, access.PolicyContentEdit,
		access.PolicyContentPublish, access.PolicyContentDelete} {
		for _, l := range []string{limitSite, limitSubtree} {
			if err := r.PolicyLimitation(pol, l); err != nil {
				return err
			}
		}
	}
	return nil
}

// placement is where an item sits in a site's tree: its page node, or a mount it is routed under.
type placement struct{ Site, Path string }

// readQ queries through the transaction the caller holds (set by store.WithTx), else through the pool: a check
// that takes a second connection while the first is held can wait forever on a small pool.
func (s *service) readQ(ctx context.Context) store.SQL {
	if tx, ok := store.TxFrom(ctx); ok {
		return tx.SQL()
	}
	return s.db.Read()
}

// placements lists the placements of an item with one query.
func (s *service) placements(ctx context.Context, item ids.UUID) ([]placement, error) {
	var out []placement
	err := each(ctx, s.readQ(ctx), `SELECT site, path FROM page_node WHERE item_id = ?
UNION SELECT n.site, n.path FROM page_mount_item m JOIN page_node n ON n.id = m.node_id WHERE m.item_id = ?`,
		[]any{item, item}, func(r store.Rows) error {
			var p placement
			err := r.Scan(&p.Site, &p.Path)
			out = append(out, p)
			return err
		})
	return out, err
}

// joinedMounts lists the mount nodes a collection item with this document would belong to. Mount membership
// follows the data, so a limitation that only looked at placements would let an editor change a filter field
// and move an item under another subtree. The reconcile consumer applies memberships asynchronously, so
// membership and index rows lag one outbox drain behind a publish; the publish hook checks the document at
// publish time.
func (s *service) joinedMounts(ctx context.Context, t access.Target) ([]placement, error) {
	if t.Doc == nil || t.Type == "" || s.classOf(t.Type) != classCollection {
		return nil, nil
	}
	set, err := s.mountsOf(ctx, s.readQ(ctx), t.Type)
	if err != nil || len(set.rows) == 0 {
		return nil, err
	}
	var item ids.UUID
	if t.Item != nil {
		item = t.Item.ID
	}
	byID := make(map[ids.UUID]nodeRow, len(set.rows))
	for _, r := range set.rows {
		byID[r.ID] = r
	}
	var out []placement
	for _, m := range set.memberships(item, "", t.Doc) {
		r := byID[m.Node]
		out = append(out, placement{r.Site, r.Path})
	}
	return out, nil
}

// contentMatch is Match for content targets. A destination (placing an unplaced item) stands for the item's
// placements; without one, no item (create) matches and an item matches when one of its placements does. When
// the target carries a document, every mount it would join must match too.
func contentMatch(ctx context.Context, get func() (*service, error), t access.Target, in func(placement) bool) (bool, error) {
	dest, hasDest := t.Extra[extraDestSite].(string)
	if t.Item == nil && t.Doc == nil && !hasDest {
		return true, nil
	}
	s, err := get()
	if err != nil {
		return false, err
	}
	joined, err := s.joinedMounts(ctx, t)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(joined, func(p placement) bool { return !in(p) }) {
		return false, nil
	}
	if hasDest {
		path, _ := t.Extra[extraDestPath].(string)
		return in(placement{dest, path}), nil
	}
	if t.Item == nil {
		return true, nil
	}
	pls, err := s.placements(ctx, t.Item.ID)
	if err != nil {
		return false, err
	}
	return slices.ContainsFunc(pls, in), nil
}

type siteLimit struct {
	sites []string
	get   func() (*service, error)
}

func (siteLimit) Name() string { return limitSite }

func (l siteLimit) Validate(_ context.Context, values []string) error {
	for _, v := range values {
		if !slices.Contains(l.sites, v) {
			return fmt.Errorf("unknown site %q", v)
		}
	}
	return nil
}

func (l siteLimit) Match(ctx context.Context, _ *access.Principal, values []string, t access.Target) (bool, error) {
	if site, ok := t.Extra[extraSite].(string); ok {
		return slices.Contains(values, site), nil
	}
	return contentMatch(ctx, l.get, t, func(p placement) bool { return slices.Contains(values, p.Site) })
}

func (siteLimit) Scope(_ context.Context, _ *access.Principal, values []string, _ string) (store.ScopeTerm, bool, error) {
	cond := store.Cond{Field: fieldSite, Column: store.ColText, Op: store.OpIn, Value: slices.Clone(values)}
	return store.ScopeTerm{Where: []store.Cond{cond}}, true, nil
}

func (l siteLimit) Choices(context.Context) ([]access.Choice, error) {
	out := make([]access.Choice, len(l.sites))
	for i, s := range l.sites {
		out[i] = access.Choice{Value: s, Label: s}
	}
	return out, nil
}

type subtreeLimit struct{ get func() (*service, error) }

func (subtreeLimit) Name() string { return limitSubtree }

func (l subtreeLimit) Validate(ctx context.Context, values []string) error {
	s, err := l.get()
	if err != nil {
		return err
	}
	for _, v := range values {
		id, err := ids.Parse(v)
		if err != nil || id.String() != v {
			return fmt.Errorf("%q is not a node id", v)
		}
		if _, err := loadNode(ctx, s.readQ(ctx), id); errors.Is(err, content.ErrNotFound) {
			return fmt.Errorf("page node %s does not exist", v)
		} else if err != nil {
			return err
		}
	}
	return nil
}

func inSubtree(path string, values []string) bool {
	return slices.ContainsFunc(values, func(v string) bool { return strings.Contains(path, "/"+v+"/") })
}

func (l subtreeLimit) Match(ctx context.Context, _ *access.Principal, values []string, t access.Target) (bool, error) {
	if _, ok := t.Extra[extraSite]; ok {
		path, _ := t.Extra[extraPath].(string)
		return inSubtree(path, values), nil
	}
	return contentMatch(ctx, l.get, t, func(p placement) bool { return inSubtree(p.Path, values) })
}

func (subtreeLimit) Scope(_ context.Context, _ *access.Principal, values []string, _ string) (store.ScopeTerm, bool, error) {
	nodes := make([]ids.UUID, len(values))
	for i, v := range values {
		id, err := ids.Parse(v)
		if err != nil {
			return store.ScopeTerm{}, false, fmt.Errorf("pages.subtree: %q is not a node id", v)
		}
		nodes[i] = id
	}
	cond := store.Cond{Field: fieldAncestor, Column: store.ColRef, Op: store.OpIn, Value: nodes}
	return store.ScopeTerm{Where: []store.Cond{cond}}, true, nil
}

func (subtreeLimit) Choices(context.Context) ([]access.Choice, error) { return nil, nil }

// can authorizes a policy on the node at path in site (the destination parent when adding or moving nodes). A
// service built without the checker allows everything.
func (s *service) can(ctx context.Context, policy, site, path string) error {
	if !s.restricted(ctx) {
		return nil
	}
	return s.svcs.Access.Can(ctx, policy, access.Target{Extra: map[string]any{extraSite: site, extraPath: path}})
}

func (s *service) canNode(ctx context.Context, policy string, n nodeRow) error {
	return s.can(ctx, policy, n.Site, n.Path)
}

// restricted reports whether the principal in ctx is subject to grants (not the system or a super principal).
func (s *service) restricted(ctx context.Context) bool {
	if s.svcs == nil || s.svcs.Access == nil {
		return false
	}
	p := access.PrincipalFrom(ctx)
	return p != nil && !p.Super && p.Kind != access.KindSystem
}

// prepare loads the principal's grants before the service opens a transaction, so checks inside it never take
// a second connection.
func (s *service) prepare(ctx context.Context) error {
	if !s.restricted(ctx) {
		return nil
	}
	return s.svcs.Access.Prepare(ctx)
}

// seen reports whether the principal may see a node at all: it can read or edit it.
func (s *service) seen(ctx context.Context, n nodeRow) bool {
	return s.canNode(ctx, policyRead, n) == nil || s.canNode(ctx, policyEdit, n) == nil
}

// authorize checks a policy on a node. A node the principal cannot see at all is reported as missing, so its
// existence stays hidden; one it sees but lacks the policy on is forbidden.
func (s *service) authorize(ctx context.Context, policy string, n nodeRow) error {
	err := s.canNode(ctx, policy, n)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, access.ErrForbidden):
		return err
	case !s.seen(ctx, n):
		return notFound("page node %s", n.ID)
	}
	return err
}

// authorizeParent is authorize for the parent a node goes under; a nil parent is the site's root.
func (s *service) authorizeParent(ctx context.Context, q store.SQL, site string, id *ids.UUID, policies ...string) error {
	if !s.restricted(ctx) {
		return nil
	}
	if id == nil {
		for _, p := range policies {
			if err := s.can(ctx, p, site, ""); err != nil {
				return err
			}
		}
		return nil
	}
	n, err := loadNode(ctx, q, *id)
	if err != nil {
		return err
	}
	for _, p := range policies {
		if err := s.authorize(ctx, p, n); err != nil {
			return err
		}
	}
	return nil
}

// authorizePlace lets a restricted principal place an item only when it could edit it at the destination, and
// it made the item or holds an edit grant for its type without pages limits: otherwise placing would hand it
// every item it can name. An item the principal cannot read is reported as missing.
func (s *service) authorizePlace(ctx context.Context, tx store.Tx, id ids.UUID, site, destPath string) error {
	if !s.restricted(ctx) {
		return nil
	}
	it, err := tx.Content().GetItem(ctx, id, false)
	if isNoRows(err) {
		return notFound("item %s", id)
	}
	if err != nil {
		return err
	}
	chk := s.svcs.Access
	err = chk.Can(ctx, access.PolicyContentEdit, access.Target{Type: it.Type, Item: &it,
		Extra: map[string]any{extraDestSite: site, extraDestPath: destPath}})
	if err == nil {
		err = s.madeOrUnlimited(ctx, it)
	}
	if err == nil || !errors.Is(err, access.ErrForbidden) {
		return err
	}
	if chk.Can(ctx, access.PolicyContentRead, access.Target{Type: it.Type, Item: &it}) != nil {
		return notFound("item %s", id)
	}
	return err
}

// madeOrUnlimited: the principal created the item, or holds a content.edit grant that covers its type without
// pages limitations.
func (s *service) madeOrUnlimited(ctx context.Context, it store.Item) error {
	p := access.PrincipalFrom(ctx)
	if it.CreatedBy == p.Actor().String() {
		return nil
	}
	grants, err := s.svcs.Access.Grants(ctx, p)
	if err != nil {
		return err
	}
	t := access.Target{Type: it.Type, Item: &it}
grants:
	for _, g := range grants {
		if g.Policy != access.PolicyContentEdit && g.Policy != access.PolicyAll {
			continue
		}
		for _, l := range g.Limits {
			lim, ok := s.svcs.Catalog.Limitation(l.Name)
			if !ok || strings.HasPrefix(l.Name, "pages.") {
				continue grants
			}
			if m, err := lim.Match(ctx, p, l.Values, t); err != nil || !m {
				continue grants
			}
		}
		return nil
	}
	return forbidden(access.PolicyContentEdit)
}

// readsAll requires the principal to read a whole collection: mounting it exposes every item.
func (s *service) readsAll(ctx context.Context, typ string) error {
	if !s.restricted(ctx) {
		return nil
	}
	scoped, _, err := s.svcs.Access.ListScope(ctx, access.PolicyContentRead, typ)
	if err != nil {
		return err
	}
	if scoped {
		return forbidden(access.PolicyContentRead)
	}
	return nil
}

func forbidden(policy string) error { return fmt.Errorf("%w: %s", access.ErrForbidden, policy) }

// visibility is what a restricted reader sees of one site's tree: nodes the pages.read grants allow, and as
// context the ancestors of the subtrees they allow.
type visibility struct {
	s       *service
	context map[ids.UUID]bool
}

// newVisibility returns nil for readers that see everything.
func (s *service) newVisibility(ctx context.Context, q store.SQL, site string) (*visibility, error) {
	if !s.restricted(ctx) {
		return nil, nil
	}
	grants, err := s.svcs.Access.Grants(ctx, access.PrincipalFrom(ctx))
	if err != nil {
		return nil, err
	}
	var roots []ids.UUID
	for _, g := range grants {
		if g.Policy != policyRead && g.Policy != access.PolicyAll {
			continue
		}
		for _, l := range g.Limits {
			if l.Name != limitSubtree {
				continue
			}
			for _, v := range l.Values {
				if id, err := ids.Parse(v); err == nil && !slices.Contains(roots, id) {
					roots = append(roots, id)
				}
			}
		}
	}
	v := &visibility{s: s, context: map[ids.UUID]bool{}}
	rows, err := loadNodes(ctx, q, roots)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		if r.Site != site || !v.permitted(ctx, r) {
			continue
		}
		anc, err := ancestry(r.Path)
		if err != nil {
			return nil, err
		}
		for _, id := range anc {
			v.context[id] = true
		}
	}
	return v, nil
}

func (v *visibility) permitted(ctx context.Context, n nodeRow) bool {
	return v.s.canNode(ctx, policyRead, n) == nil
}

func (v *visibility) visible(ctx context.Context, n nodeRow) bool {
	return v.context[n.ID] || v.permitted(ctx, n)
}

// canSee requires the principal to see something of the site: its root is permitted or context, or, when the
// site has no pages yet, the site as a whole is permitted.
func (s *service) canSee(ctx context.Context, site string) error {
	if !s.restricted(ctx) {
		return nil
	}
	if err := s.prepare(ctx); err != nil {
		return err
	}
	if err := s.checkSite(site); err != nil {
		return err
	}
	q := s.db.Read()
	v, err := s.newVisibility(ctx, q, site)
	if err != nil || v == nil {
		return err
	}
	root, ok, err := s.treeTop(ctx, q, site, nil)
	if err != nil {
		return err
	}
	if !ok {
		return s.can(ctx, policyRead, site, "")
	}
	if !v.visible(ctx, root) {
		return forbidden(policyRead)
	}
	return nil
}

// sitesFor lists the sites the principal may see.
func (s *service) sitesFor(ctx context.Context) ([]string, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(s.sites))
	for _, site := range s.sites {
		err := s.canSee(ctx, site)
		switch {
		case err == nil:
			out = append(out, site)
		case !errors.Is(err, access.ErrForbidden):
			return nil, err
		}
	}
	return out, nil
}

// limitResolution reports whether a resolution must be shown as not found: it serves a node the principal may
// not read, or redirects to an item placed nowhere the principal may read. Breadcrumbs above the permitted
// subtrees lose their item and path.
func (s *service) limitResolution(ctx context.Context, res *Resolution) (hidden bool, err error) {
	if res == nil || !s.restricted(ctx) {
		return false, nil
	}
	if err := s.prepare(ctx); err != nil {
		return false, err
	}
	var list []ids.UUID
	if res.Node != nil {
		list = append(list, *res.Node)
	}
	for _, c := range res.Breadcrumbs {
		list = append(list, c.Node)
	}
	rows, err := loadNodes(ctx, s.db.Read(), list)
	if err != nil {
		return false, err
	}
	allowed := map[ids.UUID]bool{}
	for _, n := range rows {
		allowed[n.ID] = s.canNode(ctx, policyRead, n) == nil
	}
	if res.Node != nil && !allowed[*res.Node] {
		return true, nil
	}
	if res.Node == nil && res.Item != nil && res.Kind == ResolvedRedirect {
		pls, err := s.placements(ctx, *res.Item)
		if err != nil {
			return false, err
		}
		if !slices.ContainsFunc(pls, func(p placement) bool { return s.can(ctx, policyRead, p.Site, p.Path) == nil }) {
			return true, nil
		}
	}
	for i := range res.Breadcrumbs {
		if c := &res.Breadcrumbs[i]; !allowed[c.Node] {
			c.Item, c.Path = nil, ""
		}
	}
	return false, nil
}

// pathsFor lists an item's routes in the places the principal may read: a route of a node by the node, any
// other by its site as a whole.
func (s *service) pathsFor(ctx context.Context, item ids.UUID) ([]Route, error) {
	routes, err := s.paths(ctx, item)
	if err != nil || !s.restricted(ctx) {
		return routes, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var list []ids.UUID
	for _, r := range routes {
		if r.Node != nil {
			list = append(list, *r.Node)
		}
	}
	rows, err := loadNodes(ctx, s.db.Read(), list)
	if err != nil {
		return nil, err
	}
	paths := make(map[ids.UUID]string, len(rows))
	for _, n := range rows {
		paths[n.ID] = n.Path
	}
	out := routes[:0]
	for _, r := range routes {
		path := ""
		if r.Node != nil {
			path = paths[*r.Node]
		}
		if s.can(ctx, policyRead, r.Site, path) == nil {
			out = append(out, r)
		}
	}
	return out, nil
}
