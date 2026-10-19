package tests

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/graphql"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

// gqlGet runs a query as GET; gqlPost as POST. Both answer the decoded JSON body.
func (n *dnode) gqlGet(channel, query string, hdr ...string) resp {
	n.t.Helper()
	p := "/graphql?query=" + url.QueryEscape(query)
	if channel != "" {
		p += "&channel=" + channel
	}
	return n.get(p, hdr...)
}

func (n *dnode) gqlPost(channel, query string, vars map[string]any, hdr ...string) resp {
	n.t.Helper()
	m := map[string]string{}
	for i := 0; i+1 < len(hdr); i += 2 {
		m[hdr[i]] = hdr[i+1]
	}
	p := "/api/deliver/v1/graphql"
	if channel != "" {
		p += "?channel=" + channel
	}
	body := map[string]any{"query": query}
	if vars != nil {
		body["variables"] = vars
	}
	return n.do("POST", p, m, body)
}

func gqlData(t *testing.T, r resp, path ...string) any {
	t.Helper()
	if r.Code != 200 {
		t.Fatalf("status %d: %s", r.Code, r.Body)
	}
	j := r.json()
	if errs, ok := j["errors"]; ok {
		t.Fatalf("errors: %v", errs)
	}
	return field(j, append([]string{"data"}, path...)...)
}

func TestGraphQLQueries(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, graphql.Module())
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		a1 := n.create("article", "first", "en", fmt.Sprintf(`{"title":"First","rank":1,"author":%q}`, ann), true)
		n.create("article", "second", "en", `{"title":"Second","rank":3}`, true)
		n.create("article", "third", "en", `{"title":"Third","rank":2}`, true)
		n.create("article", "hidden", "en", `{"title":"Draft","rank":9}`, false)
		_ = a1

		// By key, with a ref resolved through the GraphQL type.
		r := n.gqlGet("web", `{ article(key: "first") { id key title rank locale author { ... on Person { fullName } } } }`)
		art := gqlData(t, r, "article").(map[string]any)
		deq(t, "title", art["title"], "First")
		deq(t, "key", art["key"], "first")
		deq(t, "rank", fmt.Sprint(art["rank"]), "1")
		deq(t, "author", field(art, "author", "fullName"), "Ann")
		deq(t, "unpublished by key", gqlData(t, n.gqlGet("web", `{ article(key: "hidden") { title } }`), "article"), nil)

		// The same by id and through item(id).
		id := art["id"].(string)
		deq(t, "by id", field(asMap(gqlData(t, n.gqlGet("web", fmt.Sprintf(`{ article(id: %q) { title } }`, id)))), "article", "title"), "First")
		deq(t, "item", field(asMap(gqlData(t, n.gqlGet("web", fmt.Sprintf(`{ item(id: %q) { type key } }`, id)))), "item", "type"), "article")

		// A list with where and sort.
		q := `{ articleList(where: {rank: {gte: 2}}, sort: "-rank", limit: 10) { total items { title rank } } }`
		list := asMap(gqlData(t, n.gqlGet("web", q), "articleList"))
		items, _ := list["items"].([]any)
		if len(items) != 2 || field(asMap(items[0]), "title") != "Second" || field(asMap(items[1]), "title") != "Third" {
			t.Errorf("list = %v", list)
		}
		// Variables over POST.
		pq := `query($min: JSON) { articleList(where: $min, sort: "rank") { items { title } } }`
		pr := n.gqlPost("web", pq, map[string]any{"min": map[string]any{"rank": map[string]any{"lt": 3}}})
		items, _ = asMap(gqlData(t, pr, "articleList"))["items"].([]any)
		if len(items) != 2 || field(asMap(items[0]), "title") != "First" {
			t.Errorf("post list = %v", items)
		}

		// Singleton.
		n.create("settings", "", "en", `{"siteName":"Hydrogen"}`, true)
		deq(t, "singleton", field(asMap(gqlData(t, n.gqlGet("web", `{ settings { siteName } }`))), "settings", "siteName"), "Hydrogen")

		// A syntax error is a 400 with GraphQL errors.
		bad := n.gqlGet("web", `{ article(`)
		if bad.Code != 400 || bad.json()["errors"] == nil {
			t.Errorf("syntax error: %d %s", bad.Code, bad.Body)
		}
		// A collection the channel does not serve is not in its schema.
		if r := n.gqlGet("people", `{ articleList { total } }`); r.Code != 400 {
			t.Errorf("collection outside the channel: %d %s", r.Code, r.Body)
		}
		deq(t, "people", field(asMap(gqlData(t, n.gqlGet("people", `{ personList { total } }`))), "personList", "total"), float64(1))
	})
}

func TestGraphQLChannelsAndPreview(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, graphql.Module())
		pub := n.create("article", "pub", "en", `{"title":"Public"}`, true)
		draft := n.create("article", "wip", "en", `{"title":"Work in progress"}`, false)

		// Token channel: no token or a wrong one is 401; the token selects the channel.
		for _, hdr := range [][]string{nil, {"Authorization", "Bearer nope-nope-nope-nope"}} {
			if r := n.gqlGet("app", `{ articleList { total } }`, hdr...); r.Code != 401 {
				t.Errorf("GET %v: %d", hdr, r.Code)
			}
			if r := n.gqlPost("app", `{ articleList { total } }`, nil, hdr...); r.Code != 401 {
				t.Errorf("POST %v: %d", hdr, r.Code)
			}
		}
		bearer := []string{"Authorization", "Bearer " + appToken}
		deq(t, "token total", field(asMap(gqlData(t, n.gqlGet("", `{ articleList { total } }`, bearer...))), "articleList", "total"), float64(1))
		if r := n.gqlPost("", `{ articleList { total } }`, nil, bearer...); r.Code != 200 {
			t.Errorf("POST with token: %d %s", r.Code, r.Body)
		}

		// A draft is hidden until a preview token for it is sent.
		deq(t, "draft hidden", gqlData(t, n.gqlGet("web", `{ article(key: "wip") { title } }`), "article"), nil)
		tok := n.previewToken(map[string]any{"channel": "web", "item": draft.String()}, 200)
		for name, r := range map[string]resp{
			"GET":  n.gqlGet("web", `{ article(key: "wip") { title } }`, previewHeaderName, tok),
			"POST": n.gqlPost("web", `{ article(key: "wip") { title } }`, nil, previewHeaderName, tok),
		} {
			deq(t, name+" preview title", field(asMap(gqlData(t, r)), "article", "title"), "Work in progress")
			if cc := r.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("%s preview Cache-Control = %q", name, cc)
			}
		}
		_ = pub
	})
}

func TestGraphQLCaching(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, graphql.Module())
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		art := n.create("article", "first", "en", fmt.Sprintf(`{"title":"First","author":%q}`, ann), true)
		q := `{ article(key: "first") { title author { ... on Person { fullName } } } }`

		r := n.gqlGet("web", q)
		gqlData(t, r)
		if r.Header().Get("ETag") == "" || !strings.Contains(r.Header().Get("Cache-Control"), "s-maxage=120") {
			t.Errorf("GET cache headers: %v", r.Header())
		}
		sk := r.Header().Get("Surrogate-Key")
		for _, want := range []string{cachetag.Item(art.String()), cachetag.Item(ann.String())} {
			if !strings.Contains(sk, want) {
				t.Errorf("Surrogate-Key %q lacks %s", sk, want)
			}
		}
		// A repeat is served from the cache, and a conditional request is a 304.
		before := n.db.dbCalls()
		r2 := n.gqlGet("web", q)
		if r2.Code != 200 || n.db.dbCalls() != before || r2.Header().Get("ETag") != r.Header().Get("ETag") {
			t.Errorf("repeat: %d, %d db calls, etag %q", r2.Code, n.db.dbCalls()-before, r2.Header().Get("ETag"))
		}
		if c := n.get("/graphql?channel=web&query="+url.QueryEscape(q), "If-None-Match", r.Header().Get("ETag")); c.Code != 304 {
			t.Errorf("conditional GET: %d", c.Code)
		}

		// Republishing purges the cached answer.
		n.update(art, "en", `{"title":"Renamed"}`)
		n.publish(art)
		n.drain()
		eventually(t, "the renamed article", func() bool {
			return field(asMap(gqlData(t, n.gqlGet("web", q))), "article", "title") == "Renamed"
		})

		// POST is never cached.
		p := n.gqlPost("web", q, nil)
		gqlData(t, p)
		if p.Header().Get("Cache-Control") != "no-store" || p.Header().Get("ETag") != "" {
			t.Errorf("POST headers: %v", p.Header())
		}
	})
}

func TestGraphQLLimitsAndSDL(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n, err := tryNodeConfig(t, b.open(t, clk), clk, deliveryChannels, "modules: { graphql: { max_depth: 3, max_query_bytes: 400 } }\n", graphql.Module())
		if err != nil {
			t.Fatal(err)
		}
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		n.create("article", "first", "en", fmt.Sprintf(`{"title":"First","author":%q}`, ann), true)

		deep := `{ article(key: "first") { related { ... on Article { related { ... on Article { title } } } } } }`
		for name, r := range map[string]resp{"GET": n.gqlGet("web", deep), "POST": n.gqlPost("web", deep, nil)} {
			if r.Code != 400 || !strings.Contains(strings.ToLower(r.Body.String()), "deep") {
				t.Errorf("%s depth limit: %d %s", name, r.Code, r.Body)
			}
		}
		if r := n.gqlPost("web", `{ article(key: "first") { title } }`, nil); r.Code != 200 {
			t.Errorf("shallow query: %d %s", r.Code, r.Body)
		}
		long := `{ article(key: "first") { title } }` + strings.Repeat(" ", 500)
		if r := n.gqlPost("web", long, nil); r.Code != 400 {
			t.Errorf("long query: %d", r.Code)
		}

		// SDL for tooling: per channel, only the channel's collections.
		sdl := n.wantStatus("/graphql/schema.graphql?channel=people", 200)
		body := sdl.Body.String()
		if !strings.Contains(sdl.Header().Get("Content-Type"), "text/plain") && !strings.Contains(sdl.Header().Get("Content-Type"), "graphql") {
			t.Errorf("SDL content type %q", sdl.Header().Get("Content-Type"))
		}
		if !strings.Contains(body, "type Person") || strings.Contains(body, "type Article") || !strings.Contains(body, "type Query") {
			t.Errorf("people SDL:\n%s", body)
		}
		if web := n.wantStatus("/graphql/schema.graphql?channel=web", 200).Body.String(); !strings.Contains(web, "type Article") {
			t.Errorf("web SDL lacks Article:\n%s", web)
		}
		n.wantStatus("/graphql/schema.graphql?channel=app", 401)
	})
}

// gqlAssetNode is assetNode with the graphql module.
func gqlAssetNode(t testing.TB, db store.DB, clk *clock.Manual) *dnode {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": assetSchema, "config/channels.yaml": assetChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\nmedia: { storage: { dir: " + filepath.Join(dir, "media") + " } }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	cdb := &delivDB{DB: db}
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: cdb, Stderr: io.Discard, PasswordIterations: 1000},
		sqlite.Module(), postgres.Module(), graphql.Module())
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
	return &dnode{t: t, app: a, db: cdb, h: h, clk: clk}
}

func TestGraphQLAssets(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := gqlAssetNode(t, b.open(t, clk), clk)
		img, file := n.image("cat.png", 1, `{"alt":"A cat"}`, true)
		n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q,"gallery":[%q]}`, img, img), true)

		q := `{ article(key: "a") { hero { id url name mime width height alt } gallery { name } cover { name } } }`
		art := asMap(gqlData(t, n.gqlGet("web", q), "article"))
		hero := asMap(art["hero"])
		deq(t, "hero id", hero["id"], img.String())
		deq(t, "hero name", hero["name"], "cat.png")
		deq(t, "hero mime", hero["mime"], "image/png")
		deq(t, "hero url", hero["url"], "/assets/"+file["id"].(string)+"/cat.png")
		deq(t, "hero width", fmt.Sprint(hero["width"]), "8")
		deq(t, "hero height", fmt.Sprint(hero["height"]), "6")
		deq(t, "hero alt", hero["alt"], "A cat")
		deq(t, "gallery", field(asMap(art["gallery"].([]any)[0]), "name"), "cat.png")
		deq(t, "unset cover", art["cover"], nil)
	})
}
