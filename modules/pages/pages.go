// Package pages is the pages module: a page tree per site, mounts that expose collections under a path, links,
// paths computed from slugs and route patterns, route history with automatic redirects, manual redirects, and
// URL resolution. Contract: planning/design/pages-h2.md.
//
// Projects add it with h.Use(pages.Module()). The Local API is *pages.Service, available to other modules via
// kernel.Lookup[*pages.Service] and kernel.LookupService[*pages.Service].
package pages

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
)

// Node kinds.
const (
	KindPage  = "page"
	KindMount = "mount"
	KindLink  = "link"
)

// Route states.
const (
	StateCurrent  = "current"  // the item's canonical path
	StateAlias    = "alias"    // also serves the item (vanity path, non-canonical mount)
	StateRedirect = "redirect" // a former path of the item; redirects to its current path
	StateManual   = "manual"   // a published redirect item (Route.Force: it wins over pages and mounted items)
)

// stateForced is what a lookup reports for a row of route_force: a forced redirect, ranked above every route.
const stateForced = "forced"

// Resolution kinds.
const (
	ResolvedPage     = "page"
	ResolvedItem     = "item" // a mounted collection item
	ResolvedRedirect = "redirect"
	ResolvedNotFound = "not_found"
)

// Config is modules.pages in app.yaml.
type Config struct {
	Sites []string `yaml:"sites"` // default [main]
}

type MountSpec struct {
	Segment    string            `json:"segment"`          // the mount's own path segment under its parent
	Collection string            `json:"collection"`       // content type
	Filter     map[string]any    `json:"filter,omitempty"` // top-level field: value, or field: [values] (any of)
	Route      string            `json:"route"`            // pattern relative to the mount, e.g. "{publishDate:yyyy}/{slug}"
	Canonical  bool              `json:"canonical,omitempty"`
	Label      map[string]string `json:"label,omitempty"` // by locale, for navigation
	// RedirectField names a string field of the collection: an item with a non-empty value answers a redirect to
	// it (an absolute http(s) URL or a site path starting with /) instead of showing itself, and its links point
	// there. RedirectStatus is the status (301, 302, 307 or 308; default 302).
	RedirectField  string `json:"redirectField,omitempty"`
	RedirectStatus int    `json:"redirectStatus,omitempty"`
}

type LinkSpec struct {
	Label map[string]string `json:"label"` // by locale
	Item  *ids.UUID         `json:"item,omitempty"`
	Node  *ids.UUID         `json:"node,omitempty"`
	URL   string            `json:"url,omitempty"` // absolute URL or site path
}

type Node struct {
	ID        ids.UUID   `json:"id"`
	Site      string     `json:"site"`
	Parent    *ids.UUID  `json:"parent,omitempty"`
	Kind      string     `json:"kind"`
	Item      *ids.UUID  `json:"item,omitempty"`
	Hidden    bool       `json:"hidden"`
	Depth     int        `json:"depth"`
	Mount     *MountSpec `json:"mount,omitempty"`
	Link      *LinkSpec  `json:"link,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// TreeNode is a node as navigation and the admin tree show it, in one locale.
type TreeNode struct {
	Node
	Type     string `json:"type,omitempty"`   // content type of a page node
	Name     string `json:"name"`             // page name in the locale (main locale fallback); mount/link label
	Path     string `json:"path,omitempty"`   // current path in the locale ("" when it has none)
	Status   string `json:"status,omitempty"` // page item status: draft, published, changed
	Children int    `json:"children"`
	// Locales are the published locales of a page's item; DraftParts its pending draft parts ("" = shared).
	Locales    []string `json:"locales,omitempty"`
	DraftParts []string `json:"draftParts,omitempty"`
	// Restricted marks an ancestor shown as context for a subtree the reader may see: only identity and the
	// name are filled.
	Restricted bool       `json:"restricted,omitempty"`
	Target     string     `json:"target,omitempty"` // links: resolved path or URL
	Broken     bool       `json:"broken,omitempty"` // links: the item or node they pointed at is gone
	Nodes      []TreeNode `json:"nodes,omitempty"`  // children, when depth allows
}

type Crumb struct {
	Node ids.UUID  `json:"node"`
	Item *ids.UUID `json:"item,omitempty"`
	Name string    `json:"name"`
	Path string    `json:"path"`
}

type Resolution struct {
	Kind        string    `json:"kind"`
	Item        *ids.UUID `json:"item,omitempty"`
	Node        *ids.UUID `json:"node,omitempty"`
	Path        string    `json:"path,omitempty"`     // canonical current path of the resolved item
	Status      int       `json:"status"`             // 200, 301/302/307/308, 404
	Location    string    `json:"location,omitempty"` // redirects: target path or URL
	Breadcrumbs []Crumb   `json:"breadcrumbs,omitempty"`
}

// Route is one row of the route table.
type Route struct {
	Site   string    `json:"site"`
	Locale string    `json:"locale"`
	Path   string    `json:"path"`
	State  string    `json:"state"`
	Item   *ids.UUID `json:"item,omitempty"`
	Node   *ids.UUID `json:"node,omitempty"`
	Target string    `json:"target,omitempty"`
	Status int       `json:"status,omitempty"`
	Force  bool      `json:"force,omitempty"` // manual redirects: wins over a page or mounted item at the path
}

// Place puts a page item into the tree. Parent nil places the site's root page.
type Place struct {
	Item   ids.UUID
	Parent *ids.UUID
	After  *ids.UUID // sibling to follow; nil = last, or first with First
	First  bool      // put the node first among its siblings (After must be nil)
}

type Mount struct {
	Parent ids.UUID
	After  *ids.UUID // as Place.After
	First  bool
	Spec   MountSpec
}

type Link struct {
	Parent ids.UUID
	After  *ids.UUID // as Place.After
	First  bool
	Spec   LinkSpec
}

// Move changes a node's parent and/or position.
type Move struct {
	Parent ids.UUID
	After  *ids.UUID
}

type NodeUpdate struct {
	Move   *Move
	Hidden *bool
	Mount  *MountSpec // mount nodes only: replaces the spec
	Link   *LinkSpec  // link nodes only: replaces the spec
}

// Service is the pages Local API. Errors wrap content.ErrNotFound, content.ErrBadRequest and
// content.ErrConflict so the management API maps them like core errors.
type Service struct {
	impl *service
}

// Sites lists the configured site names.
func (s *Service) Sites() []string {
	if s.impl == nil {
		return nil
	}
	return slices.Clone(s.impl.sites)
}

// SitesFor lists the sites the principal in ctx may see.
func (s *Service) SitesFor(ctx context.Context) ([]string, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.sitesFor(ctx)
}

// Place puts a page item into the tree of a site.
func (s *Service) Place(ctx context.Context, site string, in Place) (*Node, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.place(ctx, site, in)
}

// AddMount creates a mount node and routes the items of its collection below it.
func (s *Service) AddMount(ctx context.Context, site string, in Mount) (*Node, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.addMount(ctx, site, in)
}

// AddLink creates a link node.
func (s *Service) AddLink(ctx context.Context, site string, in Link) (*Node, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.addLink(ctx, site, in)
}

// Node returns one node.
func (s *Service) Node(ctx context.Context, id ids.UUID) (*Node, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.node(ctx, id)
}

// Update moves, reorders, hides or reconfigures a node.
func (s *Service) Update(ctx context.Context, id ids.UUID, in NodeUpdate) (*Node, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.update(ctx, id, in)
}

// DeleteNode deletes a mount or link node; page nodes go away when their item is deleted.
func (s *Service) DeleteNode(ctx context.Context, id ids.UUID) error {
	impl, err := s.get()
	if err != nil {
		return err
	}
	return impl.deleteNode(ctx, id)
}

// Tree returns parent's children (parent nil: the site's root) to depth levels (1 = children only).
func (s *Service) Tree(ctx context.Context, site, locale string, parent *ids.UUID, depth int) ([]TreeNode, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.tree(ctx, site, locale, parent, depth, impl.newVisibility)
}

// TreeRoot describes the site's root node; nil when the site has no root page.
func (s *Service) TreeRoot(ctx context.Context, site, locale string) (*TreeNode, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.treeRoot(ctx, site, locale, impl.newVisibility)
}

// Resolve finds what a path serves in a site and locale.
func (s *Service) Resolve(ctx context.Context, site, locale, path string) (*Resolution, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	if err := impl.canSee(ctx, site); errors.Is(err, access.ErrForbidden) {
		return nil, notFound("site %q", site)
	} else if err != nil {
		return nil, err
	}
	res, err := impl.resolve(ctx, site, locale, path)
	if err != nil {
		return nil, err
	}
	if hidden, err := impl.limitResolution(ctx, res); err != nil {
		return nil, err
	} else if hidden {
		return notFoundResolution(), nil
	}
	return res, nil
}

// Redirects lists the routes of every site that redirect: former paths of moved items (StateRedirect, which
// lead to the item's current path) and manual redirects (StateManual). It is for tools that mirror the route
// table, such as static export; a request is answered by Resolve. The caller is trusted (no access check).
func (s *Service) Redirects(ctx context.Context) ([]Route, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.redirectRoutes(ctx)
}

// Paths lists every route of an item ("where this appears").
func (s *Service) Paths(ctx context.Context, item ids.UUID) ([]Route, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.pathsFor(ctx, item)
}
