package tests

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

// accessEnv is a pages environment with a Checker built from the pages module's policies and limitations.
type accessEnv struct {
	*pagesEnv
	pm    kernel.Module
	chk   *access.Checker
	admin *access.Admin
	// fixed tree: home > (A > (a1 > a1x), B > b1)
	home, a, a1, a1x, b, b1       ids.UUID
	homeN, aN, a1N, a1xN, bN, b1N ids.UUID
}

type catalogReg struct{ c *access.Catalog }

func (r catalogReg) Policy(p access.Policy) error         { return r.c.AddPolicy(p) }
func (r catalogReg) Limitation(l access.Limitation) error { return r.c.AddLimitation(l) }
func (r catalogReg) PolicyLimitation(p, l string) error   { return r.c.AddPolicyLimitation(p, l) }

func newAccessEnv(t *testing.T, b backend) *accessEnv {
	t.Helper()
	clk := newClock()
	db := b.open(t, clk)
	dir := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/s.yaml", pagesSchema)
	write("config/app.yaml", "content: { schema_dir: "+filepath.Join(dir, "schema")+" }\nmodules:\n  pages: { sites: [main, shop] }\n")
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	pm := pages.Module()
	app, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, DB: db, Stderr: io.Discard, PasswordIterations: 1000},
		sqlite.Module(), postgres.Module(), pm)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s := app.Services()
	if _, err := s.Content.EnsureSchemaRev(ctx); err != nil {
		t.Fatal(err)
	}
	svc, _ := kernel.LookupService[*pages.Service](s)
	cat := access.NewCatalog()
	for _, p := range access.CorePolicies() {
		if err := cat.AddPolicy(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range access.CoreLimitations(s.Schema, nil) {
		if err := cat.AddLimitation(l); err != nil {
			t.Fatal(err)
		}
	}
	if err := pages.RegisterAccess(catalogReg{cat}, svc, svc.Sites()); err != nil {
		t.Fatal(err)
	}
	chk := access.NewChecker(access.CheckerOptions{DB: db, Catalog: cat, Schema: s.Schema})
	s.Access, s.Catalog = chk, cat
	admin, err := access.NewAdmin(access.AdminOptions{DB: db, Outbox: s.Outbox, Audit: s.Audit, Clock: clk, IDs: s.IDs,
		Catalog: cat, Checker: chk, PasswordIterations: testIterations})
	if err != nil {
		t.Fatal(err)
	}
	e := &accessEnv{pagesEnv: &pagesEnv{t: t, db: db, clk: clk, app: app, c: s.Content, pages: svc}, pm: pm, chk: chk, admin: admin}
	e.home, e.homeN = e.placed("home", "Home", nil, nil)
	e.a, e.aN = e.placed("section", "A", &e.homeN, nil)
	e.b, e.bN = e.placed("section", "B", &e.homeN, &e.aN)
	e.a1, e.a1N = e.placed("section", "A1", &e.aN, nil)
	e.a1x, e.a1xN = e.placed("landing", "A1x", &e.a1N, nil)
	e.b1, e.b1N = e.placed("landing", "B1", &e.bN, nil)
	return e
}

func runAccess(t *testing.T, fn func(t *testing.T, e *accessEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) { fn(t, newAccessEnv(t, b)) })
}

// as creates a user holding the policies and returns a context acting as them.
func (e *accessEnv) as(login string, pols ...access.RolePolicy) context.Context {
	e.t.Helper()
	role, err := e.admin.CreateRole(ctx, access.RoleInput{Name: login + "-role", Policies: pols})
	if err != nil {
		e.t.Fatalf("role %s: %v", login, err)
	}
	u, err := e.admin.CreateUser(ctx, access.UserInput{Login: login, Password: authPassword})
	if err != nil {
		e.t.Fatal(err)
	}
	_, err = e.admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: role.ID,
		Principal: store.PrincipalRef{Kind: store.PrincipalUser, ID: u.ID}})
	if err != nil {
		e.t.Fatal(err)
	}
	return access.WithPrincipal(ctx, &access.Principal{Kind: access.KindUser, ID: u.ID, Name: login})
}

func pol(name, limit string, values ...string) access.RolePolicy {
	return access.RolePolicy{Policy: name, Limitations: map[string][]string{limit: values}}
}

func (e *accessEnv) item(id ids.UUID) *store.Item {
	e.t.Helper()
	var it store.Item
	err := e.db.View(ctx, func(tx store.Tx) (err error) { it, err = tx.Content().GetItem(ctx, id, false); return })
	if err != nil {
		e.t.Fatal(err)
	}
	return &it
}

func (e *accessEnv) can(uctx context.Context, policy string, id ids.UUID) bool {
	e.t.Helper()
	t := access.Target{Type: e.item(id).Type, Item: e.item(id)}
	err := e.chk.Can(uctx, policy, t)
	if err != nil && !errors.Is(err, access.ErrForbidden) {
		e.t.Fatal(err)
	}
	return err == nil
}

func (e *accessEnv) scoped(uctx context.Context) []ids.UUID {
	e.t.Helper()
	var out []ids.UUID
	for _, typ := range []string{"home", "section", "landing", "article"} {
		scoped, terms, err := e.chk.ListScope(uctx, access.PolicyContentRead, typ)
		if err != nil {
			e.t.Fatal(err)
		}
		var res store.QueryResult
		err = e.db.View(ctx, func(tx store.Tx) (err error) {
			res, err = tx.Content().Query(ctx, store.Query{Type: typ, Working: true, Scoped: scoped, Scope: terms, Limit: 100})
			return
		})
		if err != nil {
			e.t.Fatal(err)
		}
		for _, it := range res.Items {
			out = append(out, it.ID)
		}
	}
	return out
}

func sameIDs(a, b []ids.UUID) bool {
	less := func(x, y ids.UUID) int { return slices.Compare(x[:], y[:]) }
	a, b = slices.Clone(a), slices.Clone(b)
	slices.SortFunc(a, less)
	slices.SortFunc(b, less)
	return slices.Equal(a, b)
}

func (e *accessEnv) wantScoped(uctx context.Context, want ...ids.UUID) {
	e.t.Helper()
	if got := e.scoped(uctx); !sameIDs(got, want) {
		e.t.Errorf("scoped listing = %v, want %v", got, want)
	}
}

func wantAccessForbidden(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, access.ErrForbidden) {
		t.Errorf("%s: err = %v, want forbidden", what, err)
	}
}

func TestPagesAccessContentMatch(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		art := e.article("en", `{"title":"Hello"}`)
		e.mount(e.aN, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		loose := e.page("landing", "Loose")
		bArt := e.article("en", `{"title":"Elsewhere"}`)
		_ = bArt
		u := e.as("subtree-a", pol("content.edit", "pages.subtree", e.a1N.String()),
			pol("content.create", "pages.subtree", e.a1N.String()))
		for _, c := range []struct {
			name string
			id   ids.UUID
			want bool
		}{
			{"page at the subtree root", e.a1, true}, {"page below it", e.a1x, true}, {"page beside it", e.b1, false},
			{"ancestor", e.a, false}, {"unplaced page", loose, false}, {"mounted outside the subtree", art, false},
		} {
			if got := e.can(u, access.PolicyContentEdit, c.id); got != c.want {
				t.Errorf("edit %s: %v, want %v", c.name, got, c.want)
			}
		}
		if err := e.chk.Can(u, access.PolicyContentCreate, access.Target{Type: "landing"}); err != nil {
			t.Errorf("create without an item: %v", err)
		}

		mounted := e.as("subtree-mount", pol("content.read", "pages.subtree", e.aN.String()))
		if !e.can(mounted, access.PolicyContentRead, art) {
			t.Error("a mounted item is inside its mount's subtree")
		}
		if e.can(mounted, access.PolicyContentRead, e.b) {
			t.Error("B is outside A")
		}

		shop, err := e.pages.Place(ctx, "shop", pages.Place{Item: loose})
		if err != nil || shop == nil {
			t.Fatalf("place in shop: %v", err)
		}
		site := e.as("shop-only", pol("content.read", "pages.site", "shop"))
		for id, want := range map[ids.UUID]bool{loose: true, e.a: false, art: false} {
			if got := e.can(site, access.PolicyContentRead, id); got != want {
				t.Errorf("site shop read %v: %v, want %v", id, got, want)
			}
		}
	})
}

func TestPagesAccessScopeAndIndexRows(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		u := e.as("reader-a", pol("content.read", "pages.subtree", e.aN.String()))
		e.wantScoped(u, e.a, e.a1, e.a1x)

		art := e.article("en", `{"title":"Hello"}`)
		mount := e.mount(e.aN, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		e.wantScoped(u, e.a, e.a1, e.a1x, art)

		e.move(e.a1N, e.bN, nil)
		e.wantScoped(u, e.a, art)
		e.move(e.a1N, e.aN, nil)
		e.wantScoped(u, e.a, e.a1, e.a1x, art)

		// moving the mount takes its items along
		e.move(mount.ID, e.bN, nil)
		e.wantScoped(u, e.a, e.a1, e.a1x)
		e.move(mount.ID, e.aN, nil)
		e.wantScoped(u, e.a, e.a1, e.a1x, art)

		if err := e.pages.DeleteNode(ctx, mount.ID); err != nil {
			t.Fatal(err)
		}
		e.wantScoped(u, e.a, e.a1, e.a1x)

		both := e.as("reader-sites", pol("content.read", "pages.site", "main"), pol("content.read", "pages.subtree", e.b1N.String()))
		e.wantScoped(both, e.home, e.a, e.b, e.a1, e.a1x, e.b1)
		none := e.as("reader-none")
		e.wantScoped(none)

		// an unplaced page is in no scope; placing it in shop puts it in the shop scope only
		loose := e.page("landing", "Loose")
		shop := e.as("reader-shop", pol("content.read", "pages.site", "shop"))
		e.wantScoped(shop)
		if _, err := e.pages.Place(ctx, "shop", pages.Place{Item: loose}); err != nil {
			t.Fatal(err)
		}
		e.wantScoped(shop, loose)
	})
}

func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, content.ErrNotFound) || errors.Is(err, access.ErrForbidden) {
		t.Errorf("%s: err = %v, want not found", what, err)
	}
}

func TestPagesAccessEditEnforcement(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		a := e.a1N.String()
		_ = a
		sub := e.aN.String()
		u := e.as("editor-a", pol("pages.edit", "pages.subtree", sub), pol("pages.read", "pages.subtree", sub),
			pol("content.create", "pages.subtree", sub), pol("content.edit", "pages.subtree", sub))

		// placing needs the destination, and an item the caller made (or may edit without pages limits)
		foreign := e.page("landing", "Foreign")
		_, err := e.pages.Place(u, "main", pages.Place{Item: foreign, Parent: &e.bN})
		wantNotFound(t, "place under B", err)
		_, err = e.pages.Place(u, "main", pages.Place{Item: foreign, Parent: &e.aN})
		wantNotFound(t, "place an item the caller cannot read", err)
		_, err = e.pages.Place(u, "main", pages.Place{Item: ids.UUID{7}, Parent: &e.aN})
		wantNotFound(t, "place a missing item", err)
		v, err := e.c.Create(u, content.CreateInput{Type: "landing", Locale: "en", Fields: doc(t, `{"title":"Mine"}`)})
		if err != nil {
			t.Fatal(err)
		}
		e.mustPublish(v.ID)
		if _, err := e.pages.Place(u, "main", pages.Place{Item: v.ID, Parent: &e.aN}); err != nil {
			t.Fatalf("place an item the caller made under A: %v", err)
		}
		mine2, err := e.c.Create(u, content.CreateInput{Type: "landing", Locale: "en", Fields: doc(t, `{"title":"Mine too"}`)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.pages.Place(u, "shop", pages.Place{Item: mine2.ID})
		wantAccessForbidden(t, "place a root", err)

		_, err = e.pages.Update(u, e.a1N, pages.NodeUpdate{Move: &pages.Move{Parent: e.bN}})
		wantNotFound(t, "move out of the subtree", err)
		_, err = e.pages.Update(u, e.b1N, pages.NodeUpdate{Move: &pages.Move{Parent: e.aN}})
		wantNotFound(t, "move in from outside", err)
		hidden := true
		_, err = e.pages.Update(u, e.b1N, pages.NodeUpdate{Hidden: &hidden})
		wantNotFound(t, "update outside", err)
		if _, err := e.pages.Update(u, e.a1N, pages.NodeUpdate{Move: &pages.Move{Parent: e.aN}}); err != nil {
			t.Errorf("reorder inside: %v", err)
		}
		if _, err := e.pages.Update(u, e.a1xN, pages.NodeUpdate{Hidden: &hidden}); err != nil {
			t.Errorf("hide inside: %v", err)
		}

		// mounting hands out a whole collection: it needs pages.mount and unrestricted reads of the collection
		spec := pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"}
		_, err = e.pages.AddMount(u, "main", pages.Mount{Parent: e.aN, Spec: spec})
		wantAccessForbidden(t, "mount without pages.mount", err)
		limited := e.as("mounter-limited", pol("pages.edit", "pages.subtree", sub), pol("pages.read", "pages.subtree", sub),
			pol("pages.mount", "pages.subtree", sub), pol("content.read", "pages.subtree", sub))
		_, err = e.pages.AddMount(limited, "main", pages.Mount{Parent: e.aN, Spec: spec})
		wantAccessForbidden(t, "mount without reading the collection", err)
		m := e.as("mounter", pol("pages.edit", "pages.subtree", sub), pol("pages.read", "pages.subtree", sub),
			pol("pages.mount", "pages.subtree", sub), access.RolePolicy{Policy: "content.read"})
		_, err = e.pages.AddMount(m, "main", pages.Mount{Parent: e.bN, Spec: spec})
		wantNotFound(t, "mount under B", err)
		mn, err := e.pages.AddMount(m, "main", pages.Mount{Parent: e.aN, Spec: spec})
		if err != nil {
			t.Fatalf("mount under A: %v", err)
		}
		other := pages.MountSpec{Segment: "other", Collection: "article", Route: "{slug}", Filter: map[string]any{"category": "x"}}
		if _, err := e.pages.Update(u, mn.ID, pages.NodeUpdate{Mount: &other}); err == nil {
			t.Error("changing a mount's filter without pages.mount succeeded")
		}
		if _, err := e.pages.Update(m, mn.ID, pages.NodeUpdate{Mount: &other}); err != nil {
			t.Errorf("changing a mount's filter with pages.mount: %v", err)
		}
		bm := e.mount(e.bN, pages.MountSpec{Segment: "feed", Collection: "article", Route: "{slug}"})
		wantNotFound(t, "delete outside", e.pages.DeleteNode(u, bm.ID))
		if err := e.pages.DeleteNode(u, mn.ID); err != nil {
			t.Errorf("delete inside: %v", err)
		}
		_, err = e.pages.AddLink(u, "main", pages.Link{Parent: e.bN, Spec: pages.LinkSpec{Label: map[string]string{"en": "x"}, URL: "/x"}})
		wantNotFound(t, "link under B", err)

		// reads need pages.read; edit alone sees the node but may not read it
		w := e.as("writer", pol("pages.edit", "pages.subtree", sub))
		_, err = e.pages.Node(w, e.a1N)
		wantAccessForbidden(t, "read without pages.read", err)
		if _, err := e.pages.Node(u, e.a1N); err != nil {
			t.Errorf("read inside: %v", err)
		}
		_, err = e.pages.Node(u, e.b1N)
		wantNotFound(t, "read outside", err)

		// the system acts without grants
		if _, err := e.pages.Update(ctx, e.b1N, pages.NodeUpdate{Hidden: &hidden}); err != nil {
			t.Errorf("system update: %v", err)
		}
	})
}

func TestPagesAccessMembershipFollowsTheDocument(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		e.mount(e.aN, pages.MountSpec{Segment: "news-a", Collection: "article", Route: "{slug}", Filter: map[string]any{"category": "a"}})
		e.mount(e.bN, pages.MountSpec{Segment: "news-b", Collection: "article", Route: "{slug}", Filter: map[string]any{"category": "b"}})
		art := e.article("en", `{"title":"Story","category":"a"}`)
		e.drain()
		if got := e.moduleRows(art); !slices.Contains(got, "ancestor="+e.aN.String()) {
			t.Fatalf("article rows = %v, want it under A", got)
		}
		sub := e.aN.String()
		u := e.as("editor-a", pol("content.read", "pages.subtree", sub), pol("content.edit", "pages.subtree", sub),
			pol("content.publish", "pages.subtree", sub))
		it := e.item(art)
		for cat, want := range map[string]bool{"a": true, "b": false} {
			err := e.chk.Can(u, access.PolicyContentPublish, access.Target{Type: "article", Item: it, Doc: doc(t, `{"slug":"story","category":"`+cat+`"}`)})
			if (err == nil) != want {
				t.Errorf("publish with category %s: %v, want allowed=%v", cat, err, want)
			}
		}
		// the draft edit may already be refused (when the content service hands over the document), else the publish is
		if _, err := e.c.Update(u, art, content.UpdateInput{Locale: "en", Fields: doc(t, `{"category":"b"}`)}); err == nil {
			if _, err := e.c.Publish(u, art, content.PublishOptions{Locales: []string{"en"}}); err == nil {
				t.Fatal("publishing a document that joins B's mount succeeded")
			}
		}
		if _, err := e.c.Update(u, art, content.UpdateInput{Locale: "en", Fields: doc(t, `{"category":"a","title":"Story two"}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.c.Publish(u, art, content.PublishOptions{Locales: []string{"en"}}); err != nil {
			t.Fatalf("publishing inside the subtree: %v", err)
		}
	})
}

func nodeNames(nodes []pages.TreeNode) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.Name)
	}
	return out
}

func TestPagesAccessTreeFilter(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		u := e.as("reader-a1", pol("pages.read", "pages.subtree", e.a1N.String()))
		nodes, err := e.pages.Tree(u, "main", "en", nil, 4)
		if err != nil {
			t.Fatal(err)
		}
		if got := nodeNames(nodes); !slices.Equal(got, []string{"A"}) {
			t.Fatalf("top level = %v, want only A (B is invisible)", got)
		}
		a := nodes[0]
		if !a.Restricted || a.Children != 0 || a.Item != nil || a.Path != "" || a.Type != "" || a.Status != "" {
			t.Errorf("A = %+v, want a bare restricted ancestor with one child", a)
		}
		if len(a.Nodes) != 1 || a.Nodes[0].Name != "A1" || a.Nodes[0].Restricted || a.Nodes[0].Item == nil {
			t.Fatalf("A children = %+v, want the permitted A1", a.Nodes)
		}
		if len(a.Nodes[0].Nodes) != 1 || a.Nodes[0].Nodes[0].Name != "A1x" {
			t.Errorf("A1 children = %+v", a.Nodes[0].Nodes)
		}
		// asking for the children of an invisible node is refused; of a context node it is filtered
		_, err = e.pages.Tree(u, "main", "en", &e.bN, 1)
		wantNotFound(t, "tree of B", err)
		kids, err := e.pages.Tree(u, "main", "en", &e.aN, 1)
		if err != nil || len(kids) != 1 {
			t.Errorf("tree of A = %v, %v", kids, err)
		}

		sites, err := e.pages.SitesFor(u)
		if err != nil || !slices.Equal(sites, []string{"main"}) {
			t.Errorf("sites = %v, %v; main has pages and shop none", sites, err)
		}
		shop := e.as("shop-reader", pol("pages.read", "pages.site", "shop"))
		sites, err = e.pages.SitesFor(shop)
		if err != nil || !slices.Equal(sites, []string{"shop"}) {
			t.Errorf("sites = %v, %v, want shop", sites, err)
		}
		_, err = e.pages.Tree(shop, "main", "en", nil, 1)
		wantNotFound(t, "tree of another site", err)
		all, _ := e.pages.SitesFor(ctx)
		if !slices.Equal(all, []string{"main", "shop"}) {
			t.Errorf("system sites = %v", all)
		}

		// resolve and paths follow the same rules
		r, err := e.pages.Resolve(u, "main", "en", "/a/a1/a1x")
		if err != nil || r.Node == nil {
			t.Fatalf("resolve inside: %+v %v", r, err)
		}
		for _, c := range r.Breadcrumbs {
			inside := c.Node == e.a1N || c.Node == e.a1xN
			if inside != (c.Item != nil) || inside != (c.Path != "") {
				t.Errorf("crumb %+v: inside=%v", c, inside)
			}
		}
		for _, p := range []string{"/b/b1", "/a"} {
			if r, err := e.pages.Resolve(u, "main", "en", p); err != nil || r.Kind != pages.ResolvedNotFound {
				t.Errorf("resolve %s = %+v, %v, want not found", p, r, err)
			}
		}
		routes, err := e.pages.Paths(u, e.b1)
		if err != nil || len(routes) != 0 {
			t.Errorf("paths outside = %v, %v", routes, err)
		}
		routes, err = e.pages.Paths(u, e.a1x)
		if err != nil || len(routes) == 0 {
			t.Errorf("paths inside = %v, %v", routes, err)
		}
	})
}

func TestPagesAccessLimitationValues(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		for name, p := range map[string]access.RolePolicy{
			"unknown site":     pol("pages.read", "pages.site", "nowhere"),
			"unknown node":     pol("pages.read", "pages.subtree", ids.UUID{9}.String()),
			"not a node id":    pol("pages.edit", "pages.subtree", "home"),
			"non canonical":    pol("pages.edit", "pages.subtree", "  "+e.aN.String()),
			"content, no site": pol("content.read", "pages.site", "nowhere"),
		} {
			_, err := e.admin.CreateRole(ctx, access.RoleInput{Name: "bad " + name, Policies: []access.RolePolicy{p}})
			var ie *access.InvalidError
			if !errors.As(err, &ie) {
				t.Errorf("%s: err = %v, want rejected values", name, err)
			}
		}
		if _, err := e.admin.CreateRole(ctx, access.RoleInput{Name: "good", Policies: []access.RolePolicy{
			pol("pages.read", "pages.site", "main", "shop"), pol("content.edit", "pages.subtree", e.aN.String())}}); err != nil {
			t.Errorf("valid role: %v", err)
		}
	})
}

// moduleRows reads the module index rows of an item as "field=value".
func (e *accessEnv) moduleRows(id ids.UUID) []string {
	e.t.Helper()
	var out []string
	err := e.db.View(ctx, func(tx store.Tx) error {
		for _, site := range []string{"main", "shop"} {
			res, err := tx.Content().Query(ctx, store.Query{Working: true, Limit: 100,
				Where: []store.Cond{{Field: "pages.site", Column: store.ColText, Op: store.OpEq, Value: site}}})
			if err != nil {
				return err
			}
			if slices.ContainsFunc(res.Items, func(s store.ItemSummary) bool { return s.ID == id }) {
				out = append(out, "site="+site)
			}
		}
		for _, n := range []ids.UUID{e.homeN, e.aN, e.bN, e.a1N, e.a1xN, e.b1N} {
			res, err := tx.Content().Query(ctx, store.Query{Working: true, Limit: 100,
				Where: []store.Cond{{Field: "pages.ancestor", Column: store.ColRef, Op: store.OpEq, Value: n}}})
			if err != nil {
				return err
			}
			if slices.ContainsFunc(res.Items, func(s store.ItemSummary) bool { return s.ID == id }) {
				out = append(out, "ancestor="+n.String())
			}
		}
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func TestPagesAccessUpgradeStep(t *testing.T) {
	runAccess(t, func(t *testing.T, e *accessEnv) {
		art := e.article("en", `{"title":"Hello"}`)
		e.mount(e.aN, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		all := []ids.UUID{e.home, e.a, e.b, e.a1, e.a1x, e.b1, art}
		before := map[ids.UUID][]string{}
		for _, id := range all {
			before[id] = e.moduleRows(id)
			if len(before[id]) == 0 {
				t.Fatalf("item %v has no module rows", id)
			}
		}
		if got := before[e.a1x]; len(got) != 5 {
			t.Errorf("a1x rows = %v, want site + home, A, A1, A1x", got)
		}
		// a database from before the rows existed: wipe them, and Start rebuilds them once
		wipe := func() {
			err := e.db.Tx(ctx, func(tx store.Tx) error {
				for _, id := range all {
					if err := tx.Content().ReplaceModuleIndex(ctx, id, "pages", nil); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		wipe()
		if got := e.moduleRows(e.a1x); len(got) != 0 {
			t.Fatalf("wipe left %v", got)
		}
		start := func() {
			if err := e.pm.(kernel.Starter).Start(ctx); err != nil {
				t.Fatal(err)
			}
		}
		start()
		for _, id := range all {
			if got := e.moduleRows(id); !slices.Equal(got, before[id]) {
				t.Errorf("rebuilt rows of %v = %v, want %v", id, got, before[id])
			}
		}
		// recorded: a second start does not run again (rows wiped by hand stay wiped)
		wipe()
		start()
		if got := e.moduleRows(e.a1x); len(got) != 0 {
			t.Errorf("second start rebuilt rows: %v", got)
		}
	})
}

// siteItems lists the items with a pages.site index row.
func siteItems(t *testing.T, e *pagesEnv, site string) []ids.UUID {
	t.Helper()
	var res store.QueryResult
	err := e.db.View(ctx, func(tx store.Tx) (err error) {
		res, err = tx.Content().Query(ctx, store.Query{Working: true, Limit: 500,
			Where: []store.Cond{{Field: "pages.site", Column: store.ColText, Op: store.OpEq, Value: site}}})
		return
	})
	if err != nil {
		t.Fatal(err)
	}
	out := make([]ids.UUID, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.ID
	}
	return out
}

func TestPagesAccessImportRebuildsRows(t *testing.T) {
	for _, from := range backends {
		for _, to := range backends {
			t.Run(from.name+"_to_"+to.name, func(t *testing.T) {
				src := freshEnv(t, from, exportSchema, noopMigrations())
				buildExportSite(t, src)
				want := siteItems(t, src, "main")
				if len(want) == 0 {
					t.Fatal("source has no module rows")
				}
				dir, _ := exportTo(t, src, false)
				dst := freshEnv(t, to, exportSchema, noopMigrations())
				if _, err := importFrom(t, dst, dir, false); err != nil {
					t.Fatal(err)
				}
				if got := siteItems(t, dst, "main"); !sameIDs(got, want) {
					t.Errorf("imported rows cover %d items, want %d", len(got), len(want))
				}
			})
		}
	}
}

// A principal's checks run inside the service's transactions; with a small pool they must never wait for a
// second connection while holding the first.
func TestPagesAccessSmallPostgresPool(t *testing.T) {
	small := backend{"postgres", func(t testing.TB, clk clock.Clock) store.DB {
		url := pgtest.Scratch(t)
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		db, err := postgres.Open(ctx, url+sep+"pool_max_conns=4", store.OpenOptions{Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		if err := db.Migrate(ctx, nil); err != nil {
			t.Fatal(err)
		}
		return db
	}}
	e := newAccessEnv(t, small)
	const n = 20
	users := make([]context.Context, n)
	for i := range users {
		users[i] = e.as(fmt.Sprintf("reader-%d", i), pol("pages.read", "pages.site", "main"), pol("pages.edit", "pages.subtree", e.aN.String()))
	}
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.pages.Tree(u, "main", "en", nil, 3); err != nil {
				errs <- err
				return
			}
			if _, err := e.pages.Node(u, e.a1N); err != nil {
				errs <- err
				return
			}
			hidden := false
			_, err := e.pages.Update(u, e.a1N, pages.NodeUpdate{Hidden: &hidden})
			errs <- err
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("concurrent checks on a 4-connection pool did not finish")
	}
	close(errs)
	for err := range errs {
		if err != nil {
			t.Error(err)
		}
	}
}
