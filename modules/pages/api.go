package pages

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/ids"
)

// pagesAPI is the part of *Service the management routes use; tests substitute a fake.
type pagesAPI interface {
	SitesFor(ctx context.Context) ([]string, error)
	Place(ctx context.Context, site string, in Place) (*Node, error)
	AddMount(ctx context.Context, site string, in Mount) (*Node, error)
	AddLink(ctx context.Context, site string, in Link) (*Node, error)
	Node(ctx context.Context, id ids.UUID) (*Node, error)
	Update(ctx context.Context, id ids.UUID, in NodeUpdate) (*Node, error)
	DeleteNode(ctx context.Context, id ids.UUID) error
	Tree(ctx context.Context, site, locale string, parent *ids.UUID, depth int) ([]TreeNode, error)
	TreeRoot(ctx context.Context, site, locale string) (*TreeNode, error)
	Resolve(ctx context.Context, site, locale, path string) (*Resolution, error)
	Paths(ctx context.Context, item ids.UUID) ([]Route, error)
	Locate(ctx context.Context, rawURL string, h http.Header) (*Located, error)
}

const (
	defaultTreeDepth = 1
	maxTreeDepth     = 10
	defaultPrefix    = "/api/manage/v1"
)

// routes returns the module's management routes (called from Register, which hands them to
// kernel.Registry.ManageRoute). svc is called per request, so the service may be built after registration.
func routes(svc func() *Service) []manage.Route {
	return apiRoutes(func() pagesAPI {
		if s := svc(); s != nil {
			return s
		}
		return nil
	})
}

func apiRoutes(get func() pagesAPI) []manage.Route {
	h := &handlers{get: get}
	return []manage.Route{
		{Method: "GET", Path: "/pages/sites", Summary: "List page sites", Policy: policyRead, Handler: h.sites, OpenAPI: opSites()},
		{Method: "GET", Path: "/pages/sites/{site}/tree", Summary: "Page tree", Policy: policyRead, Handler: h.tree, OpenAPI: opTree()},
		{Method: "POST", Path: "/pages/sites/{site}/nodes", Summary: "Add a node", Policy: policyEdit, Handler: h.create, OpenAPI: opCreate()},
		{Method: "GET", Path: "/pages/nodes/{id}", Summary: "Get a node", Policy: policyRead, Handler: h.node, OpenAPI: opNode()},
		{Method: "PATCH", Path: "/pages/nodes/{id}", Summary: "Move, reorder or update a node", Policy: policyEdit, Handler: h.patch, OpenAPI: opPatch()},
		{Method: "DELETE", Path: "/pages/nodes/{id}", Summary: "Delete a mount or link node", Policy: policyEdit, Handler: h.remove, OpenAPI: opDelete()},
		{Method: "GET", Path: "/pages/sites/{site}/resolve", Summary: "Resolve a path", Policy: policyRead, Handler: h.resolve, OpenAPI: opResolve()},
		{Method: "GET", Path: "/pages/items/{id}/paths", Summary: "Paths of an item", Policy: policyRead, Handler: h.paths, OpenAPI: opPaths()},
		{Method: "GET", Path: "/pages/locate", Summary: "Locate a URL through the channels", Policy: policyRead, Handler: h.locate, OpenAPI: opLocate()},
	}
}

type handlers struct{ get func() pagesAPI }

func (h *handlers) api(w http.ResponseWriter, r *http.Request) pagesAPI {
	a := h.get()
	if a == nil {
		manage.Fail(w, r, &manage.Error{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "pages module is not ready"})
	}
	return a
}

func pathID(r *http.Request, name string) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue(name))
	if err != nil {
		return ids.Nil, manage.BadRequest("%s must be a UUID", name)
	}
	return id, nil
}

func optUUID(what, s string) (*ids.UUID, error) {
	if s == "" {
		return nil, nil
	}
	id, err := ids.Parse(s)
	if err != nil {
		return nil, manage.BadRequest("%s must be a UUID", what)
	}
	return &id, nil
}

func (h *handlers) sites(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	items, err := a.SitesFor(r.Context())
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if items == nil {
		items = []string{}
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *handlers) tree(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	q := r.URL.Query()
	parent, err := optUUID("parent", q.Get("parent"))
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	depth := defaultTreeDepth
	if s := q.Get("depth"); s != "" {
		n, perr := strconv.Atoi(s)
		if perr != nil || n < 1 || n > maxTreeDepth {
			manage.Fail(w, r, manage.BadRequest("depth must be an integer from 1 to %d", maxTreeDepth))
			return
		}
		depth = n
	}
	nodes, err := a.Tree(r.Context(), r.PathValue("site"), q.Get("locale"), parent, depth)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if nodes == nil {
		nodes = []TreeNode{}
	}
	out := map[string]any{"items": nodes}
	if parent == nil {
		root, err := a.TreeRoot(r.Context(), r.PathValue("site"), q.Get("locale"))
		if err != nil {
			manage.Fail(w, r, err)
			return
		}
		if root != nil {
			out["root"] = root
		}
	}
	manage.WriteJSON(w, http.StatusOK, out)
}

type createBody struct {
	Kind       string            `json:"kind"`
	Item       *ids.UUID         `json:"item"`
	Parent     *ids.UUID         `json:"parent"`
	After      *ids.UUID         `json:"after"`
	Segment    string            `json:"segment"`
	Collection string            `json:"collection"`
	Filter     map[string]any    `json:"filter"`
	Route      string            `json:"route"`
	Canonical  bool              `json:"canonical"`
	Label      map[string]string `json:"label"`
	Node       *ids.UUID         `json:"node"`
	URL        string            `json:"url"`
}

func (h *handlers) create(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	var in createBody
	if err := manage.DecodeJSON(w, r, &in); err != nil {
		manage.Fail(w, r, err)
		return
	}
	site := r.PathValue("site")
	var (
		n   *Node
		err error
	)
	switch in.Kind {
	case KindPage:
		if in.Item == nil {
			err = manage.BadRequest("item is required for a page node")
		} else if in.Segment != "" || in.Collection != "" || in.Route != "" || in.Filter != nil || in.Label != nil || in.Node != nil || in.URL != "" {
			err = manage.BadRequest("a page node takes only item, parent and after")
		} else {
			n, err = a.Place(r.Context(), site, Place{Item: *in.Item, Parent: in.Parent, After: in.After})
		}
	case KindMount:
		label := in.Label
		switch {
		case in.Parent == nil:
			err = manage.BadRequest("parent is required for a mount")
		case in.Segment == "" || in.Collection == "" || in.Route == "":
			err = manage.BadRequest("segment, collection and route are required for a mount")
		case in.Item != nil || in.Node != nil || in.URL != "":
			err = manage.BadRequest("a mount takes no item, node or url")
		default:
			n, err = a.AddMount(r.Context(), site, Mount{Parent: *in.Parent, After: in.After, Spec: MountSpec{
				Segment: in.Segment, Collection: in.Collection, Filter: in.Filter, Route: in.Route,
				Canonical: in.Canonical, Label: label}})
		}
	case KindLink:
		label := in.Label
		targets := 0
		if in.Item != nil {
			targets++
		}
		if in.Node != nil {
			targets++
		}
		if in.URL != "" {
			targets++
		}
		switch {
		case in.Parent == nil:
			err = manage.BadRequest("parent is required for a link")
		case len(label) == 0:
			err = manage.BadRequest("label is required for a link")
		case targets != 1:
			err = manage.BadRequest("a link needs exactly one of item, node or url")
		case in.Segment != "" || in.Collection != "" || in.Route != "" || in.Filter != nil || in.Canonical:
			err = manage.BadRequest("a link takes no mount settings")
		default:
			n, err = a.AddLink(r.Context(), site, Link{Parent: *in.Parent, After: in.After,
				Spec: LinkSpec{Label: label, Item: in.Item, Node: in.Node, URL: in.URL}})
		}
	default:
		err = manage.BadRequest("kind must be page, mount or link")
	}
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	w.Header().Set("Location", requestPrefix(r, "/pages/sites/"+site+"/nodes")+"/pages/nodes/"+n.ID.String())
	manage.WriteJSON(w, http.StatusCreated, n)
}

// requestPrefix is where the management API is mounted: the original request URI minus the route's own path.
func requestPrefix(r *http.Request, route string) string {
	p := r.RequestURI
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	if pre, ok := strings.CutSuffix(p, route); ok && strings.HasPrefix(pre, "/") {
		return pre
	}
	if r.RequestURI == "" {
		return defaultPrefix
	}
	return ""
}

func (h *handlers) node(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	n, err := a.Node(r.Context(), id)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, n)
}

type patchBody struct {
	Parent *ids.UUID       `json:"parent"`
	After  json.RawMessage `json:"after"` // absent, null (first) or a sibling id
	Hidden *bool           `json:"hidden"`
	Mount  *MountSpec      `json:"mount"`
	Link   *LinkSpec       `json:"link"`
}

func (h *handlers) patch(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	var in patchBody
	if err := manage.DecodeJSON(w, r, &in); err != nil {
		manage.Fail(w, r, err)
		return
	}
	hasAfter := in.After != nil
	var after *ids.UUID
	if hasAfter && string(in.After) != "null" {
		var s string
		if json.Unmarshal(in.After, &s) != nil {
			manage.Fail(w, r, manage.BadRequest("after must be a UUID or null"))
			return
		}
		if after, err = optUUID("after", s); err != nil || after == nil {
			manage.Fail(w, r, manage.BadRequest("after must be a UUID or null"))
			return
		}
	}
	if in.Parent == nil && !hasAfter && in.Hidden == nil && in.Mount == nil && in.Link == nil {
		manage.Fail(w, r, manage.BadRequest("nothing to update"))
		return
	}
	if in.Mount != nil && in.Link != nil {
		manage.Fail(w, r, manage.BadRequest("mount and link cannot be updated together"))
		return
	}
	up := NodeUpdate{Hidden: in.Hidden, Mount: in.Mount, Link: in.Link}
	switch {
	case in.Parent != nil:
		up.Move = &Move{Parent: *in.Parent, After: after}
	case hasAfter:
		cur, err := a.Node(r.Context(), id)
		if err != nil {
			manage.Fail(w, r, err)
			return
		}
		if cur.Parent == nil {
			manage.Fail(w, r, manage.BadRequest("the root cannot be reordered"))
			return
		}
		up.Move = &Move{Parent: *cur.Parent, After: after}
	}
	n, err := a.Update(r.Context(), id, up)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, n)
}

func (h *handlers) remove(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if err := a.DeleteNode(r.Context(), id); err != nil {
		manage.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handlers) resolve(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	q := r.URL.Query()
	if !q.Has("path") {
		manage.Fail(w, r, manage.BadRequest("path is required"))
		return
	}
	res, err := a.Resolve(r.Context(), r.PathValue("site"), q.Get("locale"), q.Get("path"))
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, res)
}

func (h *handlers) paths(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	rts, err := a.Paths(r.Context(), id)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if rts == nil {
		rts = []Route{}
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"items": rts})
}
