package tests

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"github.com/hydrogencms/hydrogen/store"
)

var pdSchema = strings.Replace(pagesSchema, "      kind:        { type: string }\n",
	"      kind:        { type: string }\n      related:     { type: ref, to: [section, article], on_delete: nullify }\n", 1)

const pdChannels = `
channels:
  web_en: { languages: [en],     match: { host: a.test, prefix: /en }, frontend: "https://front.a.test", reserved: [/cart] }
  web_de: { languages: [de, en], match: { host: a.test, prefix: /de } }
  sg:     { languages: [en],     match: { host: a.test, header: { name: x-market, value: sg } } }
  gold:   { languages: [en],     match: { host: a.test, prefix: /en, header: { name: x-tier, value: gold } } }
  jobs:   { languages: [en],     match: { host: jobs.test }, site: careers }
  lim:    { languages: [en],     match: { host: lim.test }, collections: [home, section] }
  app:    { languages: [en], access: token, tokens: { v1: "${env:APP_TOKEN}" } }
`

// pdDB counts the SQL statements of DB.Read, which the pages module uses for delivery lookups.
type pdDB struct {
	store.DB
	queries atomic.Int64
}

func (d *pdDB) Read() store.SQL { return countSQL{d.DB.Read(), &d.queries} }

type countSQL struct {
	store.SQL
	n *atomic.Int64
}

func (c countSQL) Query(ctx context.Context, q string, args ...any) (store.Rows, error) {
	c.n.Add(1)
	return c.SQL.Query(ctx, q, args...)
}

func (c countSQL) QueryRow(ctx context.Context, q string, args ...any) store.Row {
	c.n.Add(1)
	return c.SQL.QueryRow(ctx, q, args...)
}

// pdEnv is a delivery node with the pages module and a fixture site.
type pdEnv struct {
	*dnode
	pages *pages.Service
	cnt   *pdDB

	root, press, team, secret, secretKid, landing, draftPage ids.UUID
	rootN, pressN, secretN                                   ids.UUID
	newsMount                                                ids.UUID
	art, jobsArt, roles                                      ids.UUID
}

func newPDNode(t testing.TB, db store.DB, clk *clock.Manual) *pdEnv {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": pdSchema, "config/channels.yaml": pdChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\nmodules:\n  pages: { sites: [main, careers] }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(k string) (string, bool) { return appToken, k == "APP_TOKEN" }})
	if err != nil {
		t.Fatal(err)
	}
	cnt := &pdDB{DB: db}
	cdb := &delivDB{DB: cnt}
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: cdb, Stderr: io.Discard, PasswordIterations: 1000},
		sqlite.Module(), postgres.Module(), pages.Module())
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Services().Content.EnsureSchemaRev(ctx); err != nil {
		t.Fatal(err)
	}
	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.Services().Delivery.Start(lctx); err != nil {
		t.Fatal(err)
	}
	h, err := a.Handler()
	if err != nil {
		t.Fatal(err)
	}
	svc, ok := kernel.LookupService[*pages.Service](a.Services())
	if !ok {
		t.Fatal("no pages service")
	}
	return &pdEnv{dnode: &dnode{t: t, app: a, db: cdb, h: h, clk: clk}, pages: svc, cnt: cnt}
}

func (e *pdEnv) place(site, typ, title string, parent *ids.UUID, publish bool) (ids.UUID, ids.UUID) {
	e.t.Helper()
	item := e.create(typ, "", "en", `{"title":"`+title+`"}`, publish)
	n, err := e.pages.Place(ctx, site, pages.Place{Item: item, Parent: parent})
	if err != nil {
		e.t.Fatalf("place %s: %v", title, err)
	}
	return item, n.ID
}

func (e *pdEnv) fixture() {
	e.t.Helper()
	e.root, e.rootN = e.place("main", "home", "Home", nil, true)
	e.press, e.pressN = e.place("main", "section", "Press", &e.rootN, true)
	e.update(e.press, "de", `{"title":"Presse"}`)
	e.publish(e.press)
	e.team, _ = e.place("main", "section", "Team", &e.pressN, true)
	e.secret, e.secretN = e.place("main", "section", "Secret", &e.rootN, true)
	e.secretKid, _ = e.place("main", "landing", "Secret kid", &e.secretN, true)
	if _, err := e.pages.Update(ctx, e.secretN, pages.NodeUpdate{Hidden: new(true)}); err != nil {
		e.t.Fatal(err)
	}
	e.landing, _ = e.place("main", "landing", "Landing", &e.rootN, true)
	e.draftPage, _ = e.place("main", "landing", "Coming soon", &e.rootN, false)
	m, err := e.pages.AddMount(ctx, "main", pages.Mount{Parent: e.rootN, Spec: pages.MountSpec{Segment: "news", Collection: "article",
		Route: "{slug}", Label: map[string]string{"en": "News", "de": "Nachrichten"}}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.newsMount = m.ID
	teamN, err := e.pages.Node(ctx, e.team)
	_ = teamN
	_ = err
	for _, l := range []pages.LinkSpec{
		{Label: map[string]string{"en": "Press link"}, Item: &e.press},
		{Label: map[string]string{"en": "Docs"}, URL: "https://example.com/docs"},
		{Label: map[string]string{"en": "Local"}, URL: "/press"},
	} {
		if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: e.rootN, Spec: l}); err != nil {
			e.t.Fatal(err)
		}
	}
	e.art = e.create("article", "", "en", `{"title":"Hello","related":"`+e.press.String()+`"}`, true)
	var rootC, rolesN ids.UUID
	_, rootC = e.place("careers", "home", "Careers", nil, true)
	e.roles, rolesN = e.place("careers", "section", "Roles", &rootC, true)
	_ = rolesN
	e.jobsArt = e.create("article", "", "en", `{"title":"Jobs","related":"`+e.roles.String()+`"}`, true)
	e.drain()
}

func (e *pdEnv) getJSON(path string, want int, hdr ...string) (resp, map[string]any) {
	e.t.Helper()
	r := e.wantStatus(path, want, hdr...)
	return r, r.json()
}

func runPD(t *testing.T, fn func(t *testing.T, e *pdEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newPDNode(t, b.open(t, clk), clk)
		e.fixture()
		fn(t, e)
	})
}

func routePath(raw, channel string) string {
	return "/route?channel=" + channel + "&url=" + url.QueryEscape(raw)
}

func TestPagesDeliveryRoute(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		// A page: located, the item with url and links, tags.
		r, j := e.getJSON(routePath("https://a.test/en/press", "web_en"), 200)
		deq(t, "kind", field(j, "located", "kind"), "page")
		deq(t, "channel", field(j, "located", "channel"), "web_en")
		deq(t, "locale", field(j, "located", "locale"), "en")
		deq(t, "item id", field(j, "item", "id"), e.press.String())
		deq(t, "item url", field(j, "item", "url"), "https://front.a.test/en/press")
		deq(t, "canonical", field(j, "located", "canonical"), "https://front.a.test/en/press")
		for _, tag := range []string{"r:main", "i:" + e.press.String()} {
			if !strings.Contains(r.Header().Get("Surrogate-Key"), tag) {
				t.Errorf("Surrogate-Key %q lacks %s", r.Header().Get("Surrogate-Key"), tag)
			}
		}
		if !strings.HasPrefix(r.Header().Get("Cache-Control"), "public") || r.Header().Get("Etag") == "" {
			t.Errorf("cache headers: %v", r.Header())
		}
		e.wantStatus(routePath("https://a.test/en/press", "web_en"), 304, "If-None-Match", r.Header().Get("Etag"))

		// The root, with a trailing slash redirect and a path-only URL.
		_, j = e.getJSON(routePath("https://a.test/en", "web_en"), 200)
		deq(t, "root", field(j, "item", "id"), e.root.String())
		_, j = e.getJSON(routePath("https://a.test/en/press/", "web_en"), 200)
		deq(t, "slash redirect", field(j, "located", "kind"), "redirect")
		deq(t, "slash redirect status", field(j, "located", "resolution", "status"), float64(308))
		_, j = e.getJSON("/route?url=/m/press", 404, "Authorization", "Bearer "+appToken)
		deq(t, "channel without a matcher", field(j, "error", "code"), "not_found")

		// A mounted item: links to the page it references, in the channel's own relative form.
		_, j = e.getJSON(routePath("https://a.test/en/news/hello", "web_en"), 200)
		deq(t, "mounted kind", field(j, "located", "kind"), "item")
		deq(t, "mounted item", field(j, "item", "id"), e.art.String())
		deq(t, "mounted url", field(j, "item", "url"), "https://front.a.test/en/news/hello")
		deq(t, "link", field(j, "item", "links", e.press.String()), "/en/press")

		// Fallback: the German channel shows the English page for a page without a German route.
		_, j = e.getJSON(routePath("https://a.test/de/presse", "web_de"), 200)
		deq(t, "de locale", field(j, "located", "locale"), "de")
		deq(t, "de title", field(j, "item", "fields", "title"), "Presse")
		_, j = e.getJSON(routePath("https://a.test/de/press/team", "web_de"), 200)
		deq(t, "fallback locale", field(j, "located", "locale"), "en")
		deq(t, "item locale", field(j, "item", "locale"), "en")

		// not found and reserved are answers, cacheable under the site's routes.
		r, j = e.getJSON(routePath("https://a.test/en/nothing", "web_en"), 200)
		deq(t, "not found", field(j, "located", "kind"), "not_found")
		if j["item"] != nil || !strings.Contains(r.Header().Get("Surrogate-Key"), "r:main") || r.Header().Get("Etag") == "" {
			t.Errorf("not_found: %v %v", j, r.Header())
		}
		r, j = e.getJSON(routePath("https://a.test/en/cart", "web_en"), 200)
		deq(t, "reserved", field(j, "located", "kind"), "reserved")
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "r:main") {
			t.Errorf("reserved tags: %v", r.Header())
		}
		// Unpublished pages and pages outside the channel's collections are not found.
		_, j = e.getJSON(routePath("https://a.test/en/coming-soon", "web_en"), 200)
		deq(t, "draft page", field(j, "located", "kind"), "not_found")
		_, j = e.getJSON(routePath("https://lim.test/landing", "lim"), 200)
		deq(t, "collection not allowed", field(j, "located", "kind"), "not_found")
		_, j = e.getJSON(routePath("https://lim.test/press", "lim"), 200)
		deq(t, "collection allowed", field(j, "located", "kind"), "page")

		// Another site, another channel.
		_, j = e.getJSON(routePath("https://jobs.test/roles", "jobs"), 200)
		deq(t, "careers", field(j, "item", "id"), e.roles.String())
		tags := e.wantStatus(routePath("https://jobs.test/roles", "jobs"), 200).Header().Get("Surrogate-Key")
		if !strings.Contains(tags, "r:careers") || strings.Contains(tags, "r:main") {
			t.Errorf("careers tags %q", tags)
		}

		// A URL of another channel is a 404, and so are missing and unknown parameters.
		e.wantStatus(routePath("https://a.test/de/presse", "web_en"), 404)
		e.wantStatus(routePath("https://nowhere.test/", "web_en"), 404)
		e.wantStatus("/route?channel=web_en", 400)
		e.wantStatus("/route?channel=web_en&url=/x&bogus=1", 400)
		e.wantStatus("/route?channel=web_en&url="+url.QueryEscape("http://[::1"), 400)
	})
}

func TestPagesDeliveryRedirects(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		old := routePath("https://a.test/en/press", "web_en")
		e.wantStatus(old, 200)
		e.update(e.press, "en", `{"slug":"media"}`)
		e.publish(e.press)
		e.drain()
		var j map[string]any
		eventually(t, "the renamed page's old route to be purged", func() bool {
			_, j = e.getJSON(old, 200)
			return field(j, "located", "kind") == "redirect"
		})
		deq(t, "status", field(j, "located", "resolution", "status"), float64(301))
		deq(t, "location", field(j, "located", "resolution", "location"), "/en/media")
		if j["item"] != nil {
			t.Error("a redirect carries no item")
		}
		r := e.wantStatus(old, 200)
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "r:main") || r.Header().Get("Etag") == "" {
			t.Errorf("redirect headers: %v", r.Header())
		}
		_, j = e.getJSON(routePath("https://a.test/en/media", "web_en"), 200)
		deq(t, "new route", field(j, "item", "id"), e.press.String())

		// A manual redirect item.
		rd := e.create("redirect", "", "en", `{"from":"/legacy","to":"/media"}`, true)
		_ = rd
		e.drain()
		eventually(t, "the manual redirect", func() bool {
			_, j = e.getJSON(routePath("https://a.test/en/legacy", "web_en"), 200)
			return field(j, "located", "kind") == "redirect"
		})
		deq(t, "manual location", field(j, "located", "resolution", "location"), "/en/media")
	})
}

func TestPagesDeliveryTree(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		names := func(j map[string]any) []string {
			var out []string
			for _, n := range j["nodes"].([]any) {
				out = append(out, n.(map[string]any)["name"].(string))
			}
			return out
		}
		r, j := e.getJSON("/tree?channel=web_en", 200)
		// Hidden and unpublished pages are out. New nodes go first.
		deq(t, "top level", strings.Join(names(j), ","), "Local,Docs,Press link,News,Landing,Press")
		deq(t, "site", j["site"], "main")
		deq(t, "locale", j["locale"], "en")
		by := func(j map[string]any, name string) map[string]any {
			for _, n := range j["nodes"].([]any) {
				if n.(map[string]any)["name"] == name {
					return n.(map[string]any)
				}
			}
			t.Fatalf("no node %s", name)
			return nil
		}
		pr := by(j, "Press")
		deq(t, "page path", pr["path"], "/press")
		deq(t, "page url", pr["url"], "https://front.a.test/en/press")
		deq(t, "page type", pr["type"], "section")
		deq(t, "page item", pr["item"], e.press.String())
		if pr["nodes"] != nil {
			t.Error("depth 1 lists no children")
		}
		deq(t, "mount url", by(j, "News")["url"], "https://front.a.test/en/news")
		deq(t, "item link", by(j, "Press link")["url"], "/en/press")
		deq(t, "external link", by(j, "Docs")["url"], "https://example.com/docs")
		deq(t, "site path link", by(j, "Local")["url"], "https://front.a.test/en/press")
		tags := r.Header().Get("Surrogate-Key")
		if !strings.Contains(tags, "nav:main") || !strings.Contains(tags, "r:main") {
			t.Errorf("tags %q", tags)
		}
		e.wantStatus("/tree?channel=web_en", 304, "If-None-Match", r.Header().Get("Etag"))

		// depth
		_, j = e.getJSON("/tree?channel=web_en&depth=2", 200)
		kids := by(j, "Press")["nodes"].([]any)
		deq(t, "children", kids[0].(map[string]any)["name"], "Team")
		deq(t, "child path", kids[0].(map[string]any)["path"], "/press/team")
		e.wantStatus("/tree?channel=web_en&depth=0", 400)
		e.wantStatus("/tree?channel=web_en&depth=11", 400)
		e.wantStatus("/tree?channel=web_en&depth=x", 400)
		e.wantStatus("/tree?channel=web_en&bogus=1", 400)

		// locale
		_, j = e.getJSON("/tree?channel=web_de", 200)
		deq(t, "de locale", j["locale"], "de")
		de := by(j, "Presse")
		deq(t, "de url", de["url"], "https://a.test/de/presse")
		_, j = e.getJSON("/tree?channel=web_de&locale=en", 200)
		deq(t, "en narrowed", by(j, "Press")["name"], "Press")
		e.wantStatus("/tree?channel=web_en&locale=de", 400)

		// collections the channel allows
		_, j = e.getJSON("/tree?channel=lim", 200)
		deq(t, "lim", strings.Join(names(j), ","), "Local,Docs,Press link,Press")

		// other site; unknown site
		_, j = e.getJSON("/tree?channel=web_en&site=careers", 200)
		deq(t, "careers", strings.Join(names(j), ","), "Roles")
		_, j = e.getJSON("/tree?channel=jobs", 200)
		deq(t, "default site of the channel", j["site"], "careers")
		e.wantStatus("/tree?channel=web_en&site=nope", 404)

		// token channel: no match, so URLs are channel-less paths; item links resolve to a web channel.
		_, j = e.getJSON("/tree", 200, "Authorization", "Bearer "+appToken)
		deq(t, "app", by(j, "Press")["name"], "Press")
		deq(t, "app item link", strings.HasPrefix(by(j, "Press link")["url"].(string), "https://"), true)
	})
}

func TestPagesDeliveryTreePurge(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		path := "/tree?channel=web_en&depth=2"
		// The page sits last among the top-level nodes.
		pressNode := func(j map[string]any) map[string]any {
			nodes := j["nodes"].([]any)
			return nodes[len(nodes)-1].(map[string]any)
		}
		title := func() any {
			_, j := e.getJSON(path, 200)
			return pressNode(j)["name"]
		}
		deq(t, "before", title(), "Press")
		e.update(e.press, "en", `{"title":"Media","slug":"media"}`)
		e.publish(e.press)
		e.drain()
		eventually(t, "the tree to be purged", func() bool { return title() == "Media" })
		_, j := e.getJSON(path, 200)
		deq(t, "path", pressNode(j)["path"], "/media")
		deq(t, "child path", pressNode(j)["nodes"].([]any)[0].(map[string]any)["path"], "/media/team")

		// Hiding a page and unpublishing a node purge it as well.
		if _, err := e.pages.Update(ctx, e.pressN, pages.NodeUpdate{Hidden: new(true)}); err != nil {
			t.Fatal(err)
		}
		e.drain()
		eventually(t, "hidden page to leave the tree", func() bool { return title() == "Landing" })
	})
}

func TestPagesDeliveryDecorator(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		// url in the channel, and links to pages, within the channel and across sites.
		r, j := e.getJSON("/items/"+e.art.String()+"?channel=web_en", 200)
		deq(t, "url", field(j, "item", "url"), "https://front.a.test/en/news/hello")
		deq(t, "relative link", field(j, "item", "links", e.press.String()), "/en/press")
		for _, tag := range []string{"i:" + e.art.String(), "i:" + e.press.String(), "r:main"} {
			if !strings.Contains(r.Header().Get("Surrogate-Key"), tag) {
				t.Errorf("tags %q lack %s", r.Header().Get("Surrogate-Key"), tag)
			}
		}
		// The careers article links to a page that only exists in the other site: an absolute URL there.
		r, j = e.getJSON("/items/"+e.jobsArt.String()+"?channel=web_en", 200)
		deq(t, "cross-site link", field(j, "item", "links", e.roles.String()), "https://jobs.test/roles")
		deq(t, "mounted url", field(j, "item", "url"), "https://front.a.test/en/news/jobs")
		_, j = e.getJSON("/items/"+e.roles.String()+"?channel=web_en", 200)
		if _, ok := j["item"].(map[string]any)["url"]; ok {
			t.Error("a page of another site has no url in this channel")
		}
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "r:careers") {
			t.Errorf("tags %q lack the linked site", r.Header().Get("Surrogate-Key"))
		}
		// Seen from the careers channel the same link is relative.
		_, j = e.getJSON("/items/"+e.jobsArt.String()+"?channel=jobs", 200)
		deq(t, "same-site link", field(j, "item", "links", e.roles.String()), "/roles")
		// A channel that doesn't allow the type has no link to it.
		_, j = e.getJSON("/items/"+e.press.String()+"?channel=lim", 200)
		deq(t, "page url in lim", field(j, "item", "url"), "https://lim.test/press")
		// The token channel gets absolute links.
		_, j = e.getJSON("/items/"+e.art.String(), 200, "Authorization", "Bearer "+appToken)
		if l, _ := field(j, "item", "links", e.press.String()).(string); !strings.HasPrefix(l, "https://") {
			t.Errorf("app link = %v", l)
		}

		// Pages showing in another language carry that locale's URL.
		_, j = e.getJSON("/items/"+e.press.String()+"?channel=web_de", 200)
		deq(t, "de url", field(j, "item", "url"), "https://a.test/de/presse")

		// One batch query per response, however many items and links.
		for i := range 5 {
			e.create("article", "", "en", fmt.Sprintf(`{"title":"Extra %d","related":"%s"}`, i, e.press), true)
		}
		e.drain()
		before := e.cnt.queries.Load()
		r, j = e.getJSON("/collections/article/items?channel=web_en", 200)
		if got := e.cnt.queries.Load() - before; got != 1 {
			t.Errorf("decoration took %d queries", got)
		}
		items := j["items"].([]any)
		if len(items) < 7 {
			t.Fatalf("%d items", len(items))
		}
		for _, it := range items {
			if field(it.(map[string]any), "links", e.press.String()) == nil && field(it.(map[string]any), "links", e.roles.String()) == nil {
				t.Errorf("item without links: %v", it)
			}
		}
		before = e.cnt.queries.Load()
		e.wantStatus("/collections/article/items?channel=web_en", 200)
		deq(t, "cached listing queries", e.cnt.queries.Load(), before)
		_ = r
	})
}

func TestPagesDeliveryPurgesLinkedResponses(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		item := "/items/" + e.art.String() + "?channel=web_en"
		link := func() any {
			_, j := e.getJSON(item, 200)
			return field(j, "item", "links", e.press.String())
		}
		deq(t, "before", link(), "/en/press")
		// Moving the parent changes the page's path without an item event.
		_, sectionN := e.place("main", "section", "Area", &e.rootN, true)
		if _, err := e.pages.Update(ctx, e.pressN, pages.NodeUpdate{Move: &pages.Move{Parent: sectionN}}); err != nil {
			t.Fatal(err)
		}
		e.drain()
		eventually(t, "the linking item to be purged", func() bool { return link() == "/en/area/press" })
		// A not-found answer turns into a page once it exists.
		nf := routePath("https://a.test/en/later", "web_en")
		_, j := e.getJSON(nf, 200)
		deq(t, "not yet", field(j, "located", "kind"), "not_found")
		e.place("main", "section", "Later", &e.rootN, true)
		e.drain()
		eventually(t, "the page to appear", func() bool {
			_, j := e.getJSON(nf, 200)
			return field(j, "located", "kind") == "page"
		})
	})
}

func TestPagesDeliveryVaryAndHeaderChannels(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		r := e.wantStatus(routePath("https://a.test/en/press", "web_en"), 200)
		// A channel varies on the headers of every channel that can match the same URLs.
		deq(t, "co-matching vary", r.Header().Get("Vary"), "Authorization, X-Market, X-Tier")
		deq(t, "isolated vary", e.wantStatus(routePath("https://jobs.test/", "jobs"), 200).Header().Get("Vary"), "Authorization")
		deq(t, "app vary", e.wantStatus("/channels/app", 200, "Authorization", "Bearer "+appToken).Header().Get("Vary"), "Authorization")
		p := routePath("https://a.test/press", "sg")
		r = e.wantStatus(p, 200, "X-Market", "sg")
		deq(t, "header vary", r.Header().Get("Vary"), "Authorization, X-Market, X-Tier")
		deq(t, "header channel", field(r.json(), "located", "channel"), "sg")
		// Without the header the URL belongs to no channel; the cache must not answer it from the entry above.
		e.wantStatus(p, 404)
		e.wantStatus(p, 404, "X-Market", "de")
		e.wantStatus(p, 200, "X-Market", "sg")
		r = e.wantStatus("/tree?channel=sg", 200)
		deq(t, "tree vary", r.Header().Get("Vary"), "Authorization, X-Market, X-Tier")
		deq(t, "channels vary", e.wantStatus("/channels/sg", 200).Header().Get("Vary"), "Authorization, X-Market, X-Tier")
	})
}

// A cached answer of web_en must not reach a request that belongs to the more specific header channel gold.
func TestPagesDeliveryVaryAcrossCoMatchingChannels(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		p := routePath("https://a.test/en/press", "web_en")
		deq(t, "plain", field(e.wantStatus(p, 200).json(), "located", "channel"), "web_en")
		e.wantStatus(p, 404, "X-Tier", "gold")
		deq(t, "plain again", field(e.wantStatus(p, 200).json(), "located", "channel"), "web_en")
		g := routePath("https://a.test/en/press", "gold")
		e.wantStatus(g, 404)
		deq(t, "gold", field(e.wantStatus(g, 200, "X-Tier", "gold").json(), "located", "channel"), "gold")
		e.wantStatus(g, 404)
		e.wantStatus(g, 404, "X-Tier", "silver")
	})
}

func TestPagesDeliveryPreview(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		tok := e.previewToken(map[string]any{"channel": "web_en"}, 200)
		// A draft edit of a published page shows in the preview only.
		e.update(e.press, "en", `{"title":"Press draft"}`)
		p := routePath("https://a.test/en/press", "web_en")
		deq(t, "published", field(e.wantStatus(p, 200).json(), "item", "fields", "title"), "Press")
		r := e.wantStatus(p, 200, "X-Hydrogen-Preview", tok)
		deq(t, "draft", field(r.json(), "item", "fields", "title"), "Press draft")
		deq(t, "cache-control", r.Header().Get("Cache-Control"), "no-store")
		if r.Header().Get("Etag") != "" || r.Header().Get("Surrogate-Key") != "" {
			t.Errorf("preview is cached: %v", r.Header())
		}
		// A never-published page: its items are served as drafts by id, not through a route.
		d := e.previewToken(map[string]any{"channel": "web_en", "item": e.draftPage.String()}, 200)
		e.wantStatus("/items/"+e.draftPage.String()+"?channel=web_en", 404)
		r = e.wantStatus("/items/"+e.draftPage.String()+"?channel=web_en", 200, "X-Hydrogen-Preview", d)
		deq(t, "draft page", field(r.json(), "item", "fields", "title"), "Coming soon")
		e.wantStatus("/tree?channel=web_en", 200, "X-Hydrogen-Preview", tok)
	})
}

func TestPagesDeliveryChannelInfoAndOpenAPI(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		_, j := e.getJSON("/channels/web_en", 200)
		opts := j["options"].(map[string]any)
		deq(t, "match", field(opts, "match", "prefix"), "/en")
		deq(t, "match host", field(opts, "match", "host"), "a.test")
		deq(t, "reserved", opts["reserved"].([]any)[0], "/cart")
		_, j = e.getJSON("/channels/jobs", 200)
		deq(t, "site", field(j, "options", "site"), "careers")
		_, j = e.getJSON("/openapi.json", 200)
		paths := j["paths"].(map[string]any)
		for _, p := range []string{"/route", "/tree"} {
			op, _ := paths[p].(map[string]any)["get"].(map[string]any)
			if op == nil || op["operationId"] == nil || op["responses"] == nil {
				t.Errorf("openapi lacks %s: %v", p, paths[p])
			}
		}
	})
}

func TestPagesDeliveryCacheHitsSkipTheDatabase(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newPDNode(t, b.open(t, clk), clk)
		e.fixture()
		p := routePath("https://a.test/en/press", "web_en")
		e.wantStatus(p, 200)
		before := e.cnt.queries.Load()
		e.wantStatus(p, 200)
		deq(t, "cache hit queries", e.cnt.queries.Load(), before)
	})
}

func benchPD(b *testing.B, be backend) *pdEnv {
	b.Helper()
	clk := newClock()
	e := newPDNode(b, be.open(b, clk), clk)
	e.fixture()
	return e
}

// BenchmarkPagesDeliveryRoute measures GET /route for a mounted item: uncached (the response cache is
// emptied before every request) and from the cache.
func BenchmarkPagesDeliveryRoute(b *testing.B) {
	target := routePath("https://a.test/en/news/hello", "web_en")
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			e := benchPD(b, be)
			d := e.app.Services().Delivery
			h := d.Handler()
			req := httptest.NewRequest("GET", target, nil)
			req.URL.Path = "/route"
			w := &nopWriter{h: http.Header{}}
			for range 20 {
				d.Purge("*")
				h.ServeHTTP(w, req)
			}
			b.Run("uncached", func(b *testing.B) {
				times := make([]time.Duration, 0, b.N)
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					d.Purge("*")
					clear(w.h)
					b.StartTimer()
					start := time.Now()
					h.ServeHTTP(w, req)
					times = append(times, time.Since(start))
				}
				b.StopTimer()
				slices.Sort(times)
				b.ReportMetric(float64(times[len(times)/2].Microseconds()), "p50-µs")
				b.ReportMetric(float64(times[len(times)*99/100].Microseconds()), "p99-µs")
			})
			b.Run("cached", func(b *testing.B) {
				h.ServeHTTP(w, req)
				b.ReportAllocs()
				for b.Loop() {
					clear(w.h)
					h.ServeHTTP(w, req)
				}
			})
		})
	}
}

func TestPagesDeliveryNavPurgedByRename(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		path := "/tree?channel=web_en&depth=2"
		names := func() []string {
			_, j := e.getJSON(path, 200)
			var out []string
			for _, n := range j["nodes"].([]any) {
				out = append(out, n.(map[string]any)["name"].(string))
			}
			return out
		}
		before := names()
		if !slices.Contains(before, "Press") {
			t.Fatalf("names = %v", before)
		}
		// Same slug and routes, new name: only the navigation changes.
		e.update(e.press, "en", `{"title":"Press room","slug":"press"}`)
		e.publish(e.press)
		e.drain()
		eventually(t, "the tree to show the new name", func() bool { return slices.Contains(names(), "Press room") })
		if slices.Contains(names(), "Press") {
			t.Errorf("names = %v", names())
		}
	})
}

func TestPagesDeliveryInputValidation(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		before := e.cnt.queries.Load()
		for _, p := range []string{
			"/route?channel=web_en&url=%2Fpress&url=%2Fother",
			"/route?channel=web_en&url=" + url.QueryEscape("/a\x00b"),
			"/route?channel=web_en&url=" + url.QueryEscape("/a\xffb"),
			"/route?channel=web_en&url=" + url.QueryEscape("/"+strings.Repeat("a", 5000)),
			"/route?channel=web_en&url=%2Fpress&locale=en&locale=en",
			"/route?channel=web_en&url=%2Fpress&channel=web_en",
			"/tree?channel=web_en&depth=1&depth=2",
			"/tree?channel=web_en&site=main&site=careers",
			"/tree?channel=web_en&locale=en&locale=en",
			"/tree?channel=web_en&site=" + url.QueryEscape("ma\x00in"),
			"/tree?channel=web_en&site=" + url.QueryEscape(strings.Repeat("a", 10000)),
			"/tree?channel=web_en&depth=" + strings.Repeat("9", 400),
			"/tree?channel=web_en&" + url.QueryEscape("k\xff") + "=1",
		} {
			e.wantStatus(p, 400)
		}
		deq(t, "queries for rejected input", e.cnt.queries.Load(), before)
		// Paths that can never exist are answered without a lookup.
		for _, raw := range []string{"https://a.test/en/%00x", "https://a.test/en/%ffx", "https://a.test/en/a%2Fb"} {
			j := e.wantStatus(routePath(raw, "web_en"), 200).json()
			deq(t, raw, field(j, "located", "kind"), "not_found")
		}
		deq(t, "queries for impossible paths", e.cnt.queries.Load(), before)
	})
}

func TestPagesDeliveryTagsLookedUpButUnavailable(t *testing.T) {
	runPD(t, func(t *testing.T, e *pdEnv) {
		tags := func(r resp) string { return r.Header().Get("Surrogate-Key") }
		has := func(r resp, want ...string) {
			t.Helper()
			for _, w := range want {
				if !slices.Contains(strings.Fields(strings.ReplaceAll(tags(r), ",", " ")), w) {
					t.Errorf("tags %q lack %s", tags(r), w)
				}
			}
		}
		// A routed page tags its item, and the routes of the channel's group.
		r := e.wantStatus(routePath("https://a.test/en/press", "web_en"), 200)
		has(r, "i:"+e.press.String(), "r:main")

		// A link to a page that is not published tags every site: its publication purges the navigation.
		draft, _ := e.place("main", "section", "Soon", &e.rootN, false)
		if _, err := e.pages.AddLink(ctx, "main", pages.Link{Parent: e.rootN, Spec: pages.LinkSpec{
			Label: map[string]string{"en": "Soon link"}, Item: &draft}}); err != nil {
			t.Fatal(err)
		}
		e.drain()
		tree := "/tree?channel=web_en"
		r = e.wantStatus(tree, 200)
		has(r, "nav:main", "r:main", "r:careers", "i:"+draft.String())
		if strings.Contains(r.Body.String(), "Soon link") {
			t.Fatal("link to an unpublished page listed")
		}
		e.publish(draft)
		e.drain()
		eventually(t, "the link to appear", func() bool { return strings.Contains(e.wantStatus(tree, 200).Body.String(), "Soon link") })

		// An item referencing an unpublished page tags that page.
		later, _ := e.place("main", "section", "Later", &e.rootN, false)
		art := e.create("article", "", "en", `{"title":"Ref","related":"`+later.String()+`"}`, true)
		e.drain()
		r = e.wantStatus("/items/"+art.String()+"?channel=web_en", 200)
		has(r, "i:"+later.String(), "r:main", "r:careers")
	})
}
