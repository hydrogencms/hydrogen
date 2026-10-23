package tests

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const deliverySchema = `
types:
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true }
  article:
    name: "{title}"
    fields:
      title:   { type: string, translatable: true, required: true }
      rank:    { type: number, integer: true, index: true }
      category: { type: string, index: true }
      featured: { type: boolean, index: true }
      author:  { type: ref, to: [person], on_delete: nullify }
      related: { type: list, of: { type: ref, to: [article, person], on_delete: nullify } }
  settings:
    singleton: true
    fields:
      siteName: { type: string, translatable: true }
`

const deliveryChannels = `
languages: { en: {}, de: {}, fr: {} }
defaults:
  settings: { delivery: { cache_ttl: 120 } }
channels:
  web:     { languages: [en, de] }
  web_de:  { languages: [de, en] }
  fr_only: { languages: [fr] }
  people:  { languages: [en], collections: [person] }
  app:
    languages: [de, en]
    collections: [person, article]
    access: token
    tokens: { v1: "${env:APP_TOKEN}" }
`

const appToken = "app-token-0123456789"

// delivDB counts the reads that bypass the delivery response cache.
type delivDB struct {
	store.DB
	reads, views atomic.Int64
}

type delivReader struct {
	store.Reader
	n *atomic.Int64
}

func (c *delivDB) Reader() store.Reader { return delivReader{c.DB.Reader(), &c.reads} }

func (c *delivDB) View(ctx context.Context, fn func(store.Tx) error) error {
	c.views.Add(1)
	return c.DB.View(ctx, fn)
}

func (c delivReader) Snapshots(ctx context.Context, items []ids.UUID, locales []string) ([]store.Snapshot, error) {
	c.n.Add(1)
	return c.Reader.Snapshots(ctx, items, locales)
}

func (c delivReader) SnapshotsByKey(ctx context.Context, key string, locales []string) ([]store.Snapshot, error) {
	c.n.Add(1)
	return c.Reader.SnapshotsByKey(ctx, key, locales)
}

func (c delivReader) Query(ctx context.Context, q store.Query) (store.QueryResult, error) {
	c.n.Add(1)
	return c.Reader.Query(ctx, q)
}

func (c *delivDB) dbCalls() int64 { return c.reads.Load() + c.views.Load() }

// dnode is one server of a delivery test: an app, its handler and a counting database.
type dnode struct {
	t   testing.TB
	app *kernel.App
	db  *delivDB
	h   http.Handler
	clk *clock.Manual
}

func writeFiles(t testing.TB, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tryNode builds an app on db with the delivery schema and the given channels, migrates it and starts the
// purge listener.
func tryNode(t testing.TB, db store.DB, clk *clock.Manual, channels string, mods ...kernel.Module) (*dnode, error) {
	t.Helper()
	return tryNodeConfig(t, db, clk, channels, "", mods...)
}

// tryNodeConfig is tryNode with extra app.yaml content.
func tryNodeConfig(t testing.TB, db store.DB, clk *clock.Manual, channels, appYAML string, mods ...kernel.Module) (*dnode, error) {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": deliverySchema, "config/channels.yaml": channels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app+appYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(k string) (string, bool) {
			switch k {
			case "APP_TOKEN":
				return appToken, true
			case "PREVIEW_ONE":
				return "preview-secret-one-0123456789abcdef", true
			case "PREVIEW_TWO":
				return "preview-secret-two-0123456789abcdef", true
			}
			return "", false
		}})
	if err != nil {
		t.Fatal(err)
	}
	cdb := &delivDB{DB: db}
	mods = append([]kernel.Module{sqlite.Module(), postgres.Module()}, mods...)
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: cdb, Stderr: io.Discard, PasswordIterations: 1000}, mods...)
	if err != nil {
		return nil, err
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
	return &dnode{t: t, app: a, db: cdb, h: h, clk: clk}, nil
}

func newDeliveryNode(t testing.TB, db store.DB, clk *clock.Manual, mods ...kernel.Module) *dnode {
	t.Helper()
	return newDeliveryNodeWithChannels(t, db, clk, deliveryChannels, mods...)
}

func runDelivery(t *testing.T, fn func(t *testing.T, n *dnode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, newDeliveryNode(t, b.open(t, clk), clk))
	})
}

type resp struct {
	*httptest.ResponseRecorder
}

func (r resp) json() map[string]any {
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		panic(fmt.Sprintf("response %q: %v", r.Body.String(), err))
	}
	return m
}

func (n *dnode) do(method, path string, hdr map[string]string, body any) resp {
	n.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	req.RemoteAddr = "127.0.0.1:50000" // the dev token only works from loopback
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	n.h.ServeHTTP(rec, req)
	return resp{rec}
}

func (n *dnode) get(path string, hdr ...string) resp {
	n.t.Helper()
	m := map[string]string{}
	for i := 0; i+1 < len(hdr); i += 2 {
		m[hdr[i]] = hdr[i+1]
	}
	return n.do("GET", "/api/deliver/v1"+path, m, nil)
}

func (n *dnode) wantStatus(path string, want int, hdr ...string) resp {
	n.t.Helper()
	r := n.get(path, hdr...)
	if r.Code != want {
		n.t.Fatalf("GET %s: status %d, want %d: %s", path, r.Code, want, r.Body)
	}
	return r
}

func (n *dnode) manage(method, path string, body any) resp {
	n.t.Helper()
	return n.do(method, "/api/manage/v1"+path, map[string]string{"Authorization": "Bearer " + kernel.DevManageToken}, body)
}

func (n *dnode) tick() { n.clk.Advance(time.Second) }

func (n *dnode) create(typ, key, locale, fields string, publish bool) ids.UUID {
	n.t.Helper()
	n.tick()
	v, err := n.app.Services().Content.Create(ctx, content.CreateInput{Type: typ, Key: key, Locale: locale, Fields: doc(n.t, fields)})
	if err != nil {
		n.t.Fatal(err)
	}
	if publish {
		n.publish(v.ID)
	}
	return v.ID
}

func (n *dnode) update(id ids.UUID, locale, fields string) {
	n.t.Helper()
	n.tick()
	if _, err := n.app.Services().Content.Update(ctx, id, content.UpdateInput{Locale: locale, Fields: doc(n.t, fields)}); err != nil {
		n.t.Fatal(err)
	}
}

func (n *dnode) publish(id ids.UUID) {
	n.t.Helper()
	n.tick()
	if _, err := n.app.Services().Content.Publish(ctx, id, content.PublishOptions{}); err != nil {
		n.t.Fatal(err)
	}
}

// drain delivers the outbox, which sends the purge notifications.
func (n *dnode) drain() {
	n.t.Helper()
	if _, err := n.app.Services().Outbox.Drain(ctx); err != nil {
		n.t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func field(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func TestDeliveryItems(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		ann := n.create("person", "ann", "en", `{"fullName":"Ann"}`, true)
		art := n.create("article", "hello", "en", `{"title":"Hello","rank":3,"author":"`+ann.String()+`"}`, false)
		n.update(art, "de", `{"title":"Hallo"}`)
		n.publish(art)
		n.create("settings", "settings", "en", `{"siteName":"Acme"}`, true)

		// Item by id: stored payload under "item", cache headers, tags.
		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		deq(t, "type", field(r.json(), "item", "type"), "article")
		deq(t, "locale", field(r.json(), "item", "locale"), "en")
		deq(t, "title", field(r.json(), "item", "fields", "title"), "Hello")
		if field(r.json(), "item", "fallback") != nil {
			t.Error("fallback set for the first language")
		}
		h := r.Header()
		if !strings.HasPrefix(h.Get("Etag"), `"`) || len(h.Get("Etag")) != 66 {
			t.Errorf("etag = %q", h.Get("Etag"))
		}
		deq(t, "cache-control", h.Get("Cache-Control"), "public, max-age=0, s-maxage=120, stale-while-revalidate=120")
		deq(t, "vary", h.Get("Vary"), "Authorization")
		deq(t, "content-type", h.Get("Content-Type"), "application/json; charset=utf-8")
		deq(t, "nosniff", h.Get("X-Content-Type-Options"), "nosniff")
		for _, hdr := range []string{"Surrogate-Key", "Cache-Tag"} {
			if !strings.Contains(h.Get(hdr), "i:"+art.String()) {
				t.Errorf("%s = %q", hdr, h.Get(hdr))
			}
		}
		if strings.Contains(h.Get("Surrogate-Key"), ",") || !strings.Contains(h.Get("Cache-Tag"), ",") && strings.Count(h.Get("Surrogate-Key"), " ") > 0 {
			t.Errorf("tag separators: %q / %q", h.Get("Surrogate-Key"), h.Get("Cache-Tag"))
		}
		// The stored payload is served byte for byte: ETag is the hash of the body.
		etag := h.Get("Etag")
		r2 := n.wantStatus("/items/"+art.String()+"?channel=web", 304, "If-None-Match", etag)
		if r2.Body.Len() != 0 || r2.Header().Get("Etag") != etag {
			t.Errorf("304 = %q %q", r2.Body, r2.Header().Get("Etag"))
		}
		n.wantStatus("/items/"+art.String()+"?channel=web", 200, "If-None-Match", `"other"`)

		// By key and singleton.
		deq(t, "by key", field(n.wantStatus("/items/by-key/hello?channel=web", 200).json(), "item", "id"), art.String())
		deq(t, "singleton", field(n.wantStatus("/singletons/settings?channel=web", 200).json(), "item", "fields", "siteName"), "Acme")
		if tags := n.get("/singletons/settings?channel=web").Header().Get("Surrogate-Key"); !strings.Contains(tags, "s:settings") {
			t.Errorf("singleton tags = %q", tags)
		}

		// Locale narrowing and fallback.
		r = n.wantStatus("/items/"+art.String()+"?channel=web&locale=de", 200)
		deq(t, "de title", field(r.json(), "item", "fields", "title"), "Hallo")
		if field(r.json(), "item", "fallback") != nil {
			t.Error("an explicitly requested locale is not a fallback")
		}
		r = n.wantStatus("/items/"+ann.String()+"?channel=web_de", 200)
		deq(t, "fallback locale", field(r.json(), "item", "locale"), "en")
		deq(t, "fallback flag", field(r.json(), "item", "fallback"), true)
		r = n.wantStatus("/items/"+art.String()+"?channel=web&locale=fr", 400)
		deq(t, "code", field(r.json(), "error", "code"), "bad_request")

		// One 404 for everything unavailable: unknown, other language, disallowed collection, unpublished, not an id.
		draft := n.create("article", "", "en", `{"title":"Draft"}`, false)
		var msgs []string
		for _, p := range []string{"/items/" + ids.UUID{9}.String() + "?channel=web", "/items/" + art.String() + "?channel=fr_only",
			"/items/" + art.String() + "?channel=people", "/items/" + draft.String() + "?channel=web", "/items/nope?channel=web",
			"/items/by-key/zzz?channel=web", "/singletons/article?channel=web", "/singletons/settings?channel=people"} {
			r := n.wantStatus(p, 404)
			msgs = append(msgs, field(r.json(), "error", "message").(string))
			if r.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s: cache-control %q", p, r.Header().Get("Cache-Control"))
			}
		}
		for _, m := range msgs[1:] {
			deq(t, "404 message", m, msgs[0])
		}

		// Unknown parameters and channels.
		n.wantStatus("/items/"+art.String()+"?channel=web&bogus=1", 400)
		n.wantStatus("/items/"+art.String()+"?channel=nope", 400)
		n.wantStatus("/items/"+art.String(), 400) // several channels: one must be chosen
		n.wantStatus("/nothing/here?channel=web", 404)
	})
}

func TestDeliveryIncludeAndCycles(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		a := n.create("article", "a", "en", `{"title":"A","author":"`+ann.String()+`"}`, true)
		b := n.create("article", "b", "en", `{"title":"B","related":["`+a.String()+`"]}`, true)
		n.update(a, "en", `{"related":["`+b.String()+`"]}`) // a -> b -> a
		n.publish(a)
		hidden := n.create("article", "", "en", `{"title":"Hidden"}`, false)
		n.update(b, "en", `{"related":["`+a.String()+`","`+hidden.String()+`"]}`)
		n.publish(b)

		r := n.wantStatus("/items/"+a.String()+"?channel=web&include=1", 200).json()
		inc := r["included"].(map[string]any)
		if len(inc) != 2 || inc[b.String()] == nil || inc[ann.String()] == nil {
			t.Fatalf("include=1: %v", inc)
		}
		r = n.wantStatus("/items/"+a.String()+"?channel=web&include=3", 200).json()
		if inc = r["included"].(map[string]any); len(inc) != 2 || inc[a.String()] != nil || inc[hidden.String()] != nil {
			t.Fatalf("include=3 must stay cycle-safe and skip unpublished items: %v", inc)
		}
		if r = n.wantStatus("/items/"+a.String()+"?channel=web", 200).json(); r["included"] != nil {
			t.Error("included without include")
		}
		// Included items outside the channel's collections are left out.
		r = n.wantStatus("/items/"+a.String()+"?channel=app&include=2", 200, "Authorization", "Bearer "+appToken).json()
		if len(r["included"].(map[string]any)) != 2 {
			t.Errorf("app included = %v", r["included"])
		}
		tags := n.get("/items/" + a.String() + "?channel=web&include=1").Header().Get("Surrogate-Key")
		for _, id := range []ids.UUID{a, b, ann} {
			if !strings.Contains(tags, "i:"+id.String()) {
				t.Errorf("tags %q lack %s", tags, id)
			}
		}
		for _, bad := range []string{"4", "-1", "x"} {
			n.wantStatus("/items/"+a.String()+"?channel=web&include="+bad, 400)
		}
	})
}

func TestDeliveryCollection(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		var arts []ids.UUID
		for i := 1; i <= 5; i++ {
			arts = append(arts, n.create("article", "", "en", fmt.Sprintf(`{"title":"T%d","rank":%d}`, i, i), true))
		}
		de := n.create("article", "", "en", `{"title":"Only en","rank":9}`, false)
		n.update(de, "de", `{"title":"Nur de"}`)
		n.publish(de)
		n.create("article", "", "en", `{"title":"Unpublished","rank":11}`, false)

		r := n.wantStatus("/collections/article/items?channel=web&sort=-rank&limit=3", 200).json()
		items := r["items"].([]any)
		if len(items) != 3 || r["hasMore"] != true || r["total"] != nil {
			t.Fatalf("items %d hasMore %v total %v", len(items), r["hasMore"], r["total"])
		}
		r2 := n.wantStatus("/collections/article/items?channel=web&sort=-rank&limit=3&total=true", 200).json()
		if r2["total"].(float64) != 6 || r2["hasMore"] != true {
			t.Fatalf("with total: %v", r2)
		}
		r2 = n.wantStatus("/collections/article/items?channel=web&sort=-rank&limit=3&offset=3", 200).json()
		if len(r2["items"].([]any)) != 3 || r2["hasMore"] != false {
			t.Fatalf("last page: %v", r2)
		}
		deq(t, "first", field(items[0].(map[string]any), "fields", "title"), "Only en")
		deq(t, "second", field(items[1].(map[string]any), "fields", "title"), "T5")

		r = n.wantStatus("/collections/article/items?channel=web&where[rank][gte]=4&where[rank][lt]=9&sort=rank", 200).json()
		items = r["items"].([]any)
		if len(items) != 2 || field(items[0].(map[string]any), "fields", "title") != "T4" {
			t.Fatalf("filtered = %v", items)
		}
		r = n.wantStatus("/collections/article/items?channel=web&limit=2&offset=2&sort=rank", 200).json()
		deq(t, "offset", field(r["items"].([]any)[0].(map[string]any), "fields", "title"), "T3")

		// The locale picks per item: web_de shows the German title where there is one, falling back to en.
		r = n.wantStatus("/collections/article/items?channel=web_de&where[rank][gte]=5&where[rank][lte]=9&sort=rank", 200).json()
		items = r["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("web_de items = %d", len(items))
		}
		deq(t, "t5 fallback", field(items[0].(map[string]any), "fallback"), true)
		deq(t, "de item", field(items[1].(map[string]any), "locale"), "de")
		if field(items[1].(map[string]any), "fallback") != nil {
			t.Error("a first-language item is not a fallback")
		}

		// Items without a language of the channel are not listed, and do not count in the total.
		n.create("article", "", "fr", `{"title":"Seulement fr","rank":10}`, true)
		r = n.wantStatus("/collections/article/items?channel=fr_only&sort=rank", 200).json()
		if items = r["items"].([]any); len(items) != 1 || field(items[0].(map[string]any), "fields", "title") != "Seulement fr" {
			t.Errorf("fr_only items = %v", items)
		}
		deq(t, "fr_only total", n.wantStatus("/collections/article/items?channel=fr_only&total=true", 200).json()["total"], float64(1))

		// Tags: the collection and every item shown.
		tags := n.get("/collections/article/items?channel=web&sort=rank&limit=2").Header().Get("Surrogate-Key")
		if !strings.Contains(tags, "c:article") || !strings.Contains(tags, "i:"+arts[0].String()) {
			t.Errorf("tags = %q", tags)
		}
		// Errors: singleton as collection, disallowed collection, bad filters.
		n.wantStatus("/collections/settings/items?channel=web", 404)
		n.wantStatus("/collections/article/items?channel=people", 404)
		n.wantStatus("/collections/nope/items?channel=web", 404)
		n.wantStatus("/collections/article/items?channel=web&where[title][eq]=x", 400)
		n.wantStatus("/collections/article/items?channel=web&status=draft", 400)
		n.wantStatus("/collections/article/items?channel=web&limit=0", 400)

		// Parameter order does not matter to the cache.
		n.get("/collections/article/items?channel=web&limit=2&sort=rank")
		before := n.db.dbCalls()
		n.wantStatus("/collections/article/items?sort=rank&channel=web&limit=2", 200)
		deq(t, "db calls", n.db.dbCalls(), before)
	})
}

func TestDeliveryTokenChannel(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		art := n.create("article", "", "en", `{"title":"A"}`, true)
		bearer := []string{"Authorization", "Bearer " + appToken}
		p := "/items/" + ann.String()

		for _, tc := range []struct {
			name string
			path string
			hdr  []string
			want int
		}{
			{"no token", p + "?channel=app", nil, 401},
			{"no token no channel", p, nil, 400},
			{"wrong token", p, []string{"Authorization", "Bearer nope-nope-nope-nope"}, 401},
			{"basic auth", p, []string{"Authorization", "Basic abc"}, 401},
			{"empty bearer", p, []string{"Authorization", "Bearer "}, 401},
			{"token", p, bearer, 200},
			{"token and its channel", p + "?channel=app", bearer, 200},
			{"token and another channel", p + "?channel=web", bearer, 400},
			{"bad token on public channel", p + "?channel=people", []string{"Authorization", "Bearer nope-nope-nope-nope"}, 401},
		} {
			r := n.get(tc.path, tc.hdr...)
			if r.Code != tc.want {
				t.Errorf("%s: %d, want %d: %s", tc.name, r.Code, tc.want, r.Body)
			}
			if tc.want == 401 && r.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("%s: WWW-Authenticate = %q", tc.name, r.Header().Get("WWW-Authenticate"))
			}
		}
		r := n.wantStatus(p, 200, bearer...)
		deq(t, "cache-control", r.Header().Get("Cache-Control"), "private, max-age=0, s-maxage=120, stale-while-revalidate=120")
		deq(t, "vary", r.Header().Get("Vary"), "Authorization")
		// The app channel is [de, en]; an English-only item is a fallback there.
		r = n.wantStatus("/items/"+art.String(), 200, bearer...)
		deq(t, "fallback", field(r.json(), "item", "fallback"), true)

		// A cached response is not served without the token.
		n.wantStatus(p, 200, bearer...)
		n.wantStatus(p+"?channel=app", 401)
	})
}

func TestDeliveryChannelInfoAndOpenAPI(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		r := n.wantStatus("/channels/web", 200).json()
		deq(t, "name", r["name"], "web")
		deq(t, "languages", asJSON(r["languages"]), `["en","de"]`)
		deq(t, "collections", asJSON(r["collections"]), `["article","person","settings"]`)
		if _, leaked := r["settings"]; leaked {
			t.Error("settings are not public")
		}
		if _, leaked := r["tokens"]; leaked {
			t.Error("tokens leaked")
		}
		deq(t, "people collections", asJSON(n.wantStatus("/channels/people", 200).json()["collections"]), `["person"]`)
		n.wantStatus("/channels/nope", 404)
		n.wantStatus("/channels/app", 401)
		n.wantStatus("/channels/app", 401, "Authorization", "Bearer nope-nope-nope-nope")
		n.wantStatus("/channels/app", 200, "Authorization", "Bearer "+appToken)
		n.wantStatus("/channels/web?channel=app", 200)

		// OpenAPI is public, typed per content type and revalidates with its etag.
		o := n.wantStatus("/openapi.json", 200)
		doc := o.json()
		deq(t, "openapi", doc["openapi"], "3.1.0")
		paths := doc["paths"].(map[string]any)
		for _, p := range []string{"/items/{id}", "/items/by-key/{key}", "/singletons/settings", "/collections/article/items",
			"/collections/person/items", "/channels/{name}", "/openapi.json"} {
			if paths[p] == nil {
				t.Errorf("openapi lacks %s", p)
			}
		}
		if paths["/collections/settings/items"] != nil || paths["/singletons/article"] != nil {
			t.Error("openapi lists a path for the wrong kind of type")
		}
		schemas := field(doc, "components", "schemas").(map[string]any)
		if schemas["Fields_article"] == nil || schemas["Item_article"] == nil || schemas["ListResponse_article"] == nil {
			t.Error("openapi lacks per-type schemas")
		}
		n.wantStatus("/openapi.json", 304, "If-None-Match", o.Header().Get("Etag"))
	})
}

func TestDeliveryCacheHitsSkipTheDatabase(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"A"}`, true)
		paths := []string{"/items/" + art.String() + "?channel=web", "/items/by-key/k?channel=web",
			"/collections/article/items?channel=web", "/channels/web?x=1&channel=web"}
		paths[3] = "/channels/web"
		var first []string
		for _, p := range paths {
			first = append(first, n.wantStatus(p, 200).Body.String())
		}
		before := n.db.dbCalls()
		for i, p := range paths {
			r := n.wantStatus(p, 200)
			deq(t, p, r.Body.String(), first[i])
			n.wantStatus(p, 304, "If-None-Match", r.Header().Get("Etag"))
		}
		deq(t, "database calls on hits", n.db.dbCalls(), before)
		if entries, _ := n.app.Services().Delivery.CacheStats(); entries != len(paths) {
			t.Errorf("%d cache entries, want %d", entries, len(paths))
		}
		// Errors are never cached.
		n.wantStatus("/items/"+ids.UUID{7}.String()+"?channel=web", 404)
		if entries, _ := n.app.Services().Delivery.CacheStats(); entries != len(paths) {
			t.Errorf("an error was cached: %d entries", entries)
		}
	})
}

func TestDeliveryPurgeOnPublish(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"One","rank":1}`, true)
		other := n.create("article", "", "en", `{"title":"Other","rank":2}`, true)
		n.create("settings", "settings", "en", `{"siteName":"S1"}`, true)
		n.drain()

		item := "/items/" + art.String() + "?channel=web"
		list := "/collections/article/items?channel=web&sort=rank"
		otherItem := "/items/" + other.String() + "?channel=web"
		single := "/singletons/settings?channel=web"
		for _, p := range []string{item, list, otherItem, single} {
			n.wantStatus(p, 200)
		}
		title := func(path string) any { return field(n.wantStatus(path, 200).json(), "item", "fields", "title") }
		deq(t, "before", title(item), "One")

		n.update(art, "en", `{"title":"Two"}`)
		deq(t, "unpublished change is invisible", title(item), "One")
		n.publish(art)
		n.drain()
		eventually(t, "the purge to reach the cache", func() bool { return title(item) == "Two" })

		items := n.wantStatus(list, 200).json()["items"].([]any)
		deq(t, "listing is purged too", field(items[0].(map[string]any), "fields", "title"), "Two")

		// Untouched entries survive the purge.
		before := n.db.dbCalls()
		n.wantStatus(otherItem, 200)
		n.wantStatus(single, 200)
		deq(t, "unrelated entries stay cached", n.db.dbCalls(), before)

		// A singleton publish purges its tag.
		sid := field(n.wantStatus(single, 200).json(), "item", "id").(string)
		id, _ := ids.Parse(sid)
		n.update(id, "en", `{"siteName":"S2"}`)
		n.publish(id)
		n.drain()
		eventually(t, "the singleton purge", func() bool {
			return field(n.wantStatus(single, 200).json(), "item", "fields", "siteName") == "S2"
		})
	})
}

func TestDeliveryPurgeAllOnEmptyNotification(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "", "en", `{"title":"A"}`, true)
		n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		if e, _ := n.app.Services().Delivery.CacheStats(); e != 1 {
			t.Fatalf("%d entries", e)
		}
		// An empty payload is what listeners get after a reconnect: messages may have been missed.
		err := n.db.Tx(ctx, func(tx store.Tx) error { return tx.Notify(cachetag.PurgeChannel, "") })
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "the cache to clear", func() bool { e, _ := n.app.Services().Delivery.CacheStats(); return e == 0 })
		n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		err = n.db.Tx(ctx, func(tx store.Tx) error { return tx.Notify(cachetag.PurgeChannel, cachetag.All) })
		if err != nil {
			t.Fatal(err)
		}
		eventually(t, "the cache to clear on *", func() bool { e, _ := n.app.Services().Delivery.CacheStats(); return e == 0 })
	})
}

func TestDeliveryPurgeAcrossNodes(t *testing.T) {
	url := pgtest.Scratch(t)
	clk := newClock()
	open := func() store.DB {
		db, err := postgres.Open(ctx, url, store.OpenOptions{Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	a := newDeliveryNode(t, open(), clk)
	b := newDeliveryNode(t, open(), clk)

	art := a.create("article", "k", "en", `{"title":"One"}`, true)
	path := "/items/" + art.String() + "?channel=web"
	title := func() any { return field(b.wantStatus(path, 200).json(), "item", "fields", "title") }
	deq(t, "node b before", title(), "One")
	before := b.db.dbCalls()
	b.wantStatus(path, 200)
	deq(t, "node b serves from its cache", b.db.dbCalls(), before)

	a.update(art, "en", `{"title":"Two"}`)
	a.publish(art)
	a.drain() // node A's outbox sends the purge; node B's listener receives it through Postgres
	eventually(t, "node B to drop its entry", func() bool { return title() == "Two" })
}

// previewToken mints a token through the management API.
func (n *dnode) previewToken(body map[string]any, want int) string {
	n.t.Helper()
	r := n.manage("POST", "/preview-tokens", body)
	if r.Code != want {
		n.t.Fatalf("POST /preview-tokens %v: %d, want %d: %s", body, r.Code, want, r.Body)
	}
	if want != 200 {
		return ""
	}
	m := r.json()
	if _, err := time.Parse(time.RFC3339, m["expiresAt"].(string)); err != nil {
		n.t.Fatal(err)
	}
	return m["token"].(string)
}

func TestDeliveryPreview(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		// A never-published item.
		draft := n.create("article", "", "en", `{"title":"Never published","rank":1}`, false)
		path := "/items/" + draft.String() + "?channel=web"
		n.wantStatus(path, 404)

		tok := n.previewToken(map[string]any{"channel": "web"}, 200)
		r := n.wantStatus(path, 200, previewHeaderName, tok)
		deq(t, "draft title", field(r.json(), "item", "fields", "title"), "Never published")
		deq(t, "type", field(r.json(), "item", "type"), "article")
		deq(t, "version", field(r.json(), "item", "version"), float64(0))
		h := r.Header()
		deq(t, "cache-control", h.Get("Cache-Control"), "no-store")
		for _, name := range []string{"Etag", "Surrogate-Key", "Cache-Tag"} {
			if h.Get(name) != "" {
				t.Errorf("preview response has %s", name)
			}
		}
		// The token also works as ?preview= and chooses the channel by itself.
		deq(t, "query token", field(n.wantStatus("/items/"+draft.String()+"?preview="+tok, 200).json(), "item", "fields", "title"), "Never published")
		// Previews are never cached: edits show at once, and the published view stays untouched.
		n.update(draft, "en", `{"title":"Edited"}`)
		deq(t, "edited", field(n.wantStatus(path, 200, previewHeaderName, tok).json(), "item", "fields", "title"), "Edited")
		n.wantStatus(path, 404)
		if e, _ := n.app.Services().Delivery.CacheStats(); e != 0 {
			t.Errorf("%d cache entries after previews", e)
		}

		// Published items show their draft changes only in a preview.
		art := n.create("article", "k", "en", `{"title":"Live","rank":2}`, true)
		n.update(art, "en", `{"title":"Next"}`)
		ap := "/items/" + art.String() + "?channel=web"
		deq(t, "published", field(n.wantStatus(ap, 200).json(), "item", "fields", "title"), "Live")
		deq(t, "draft", field(n.wantStatus(ap, 200, previewHeaderName, tok).json(), "item", "fields", "title"), "Next")
		deq(t, "by key", field(n.wantStatus("/items/by-key/k?channel=web", 200, previewHeaderName, tok).json(), "item", "fields", "title"), "Next")
		deq(t, "after preview", field(n.wantStatus(ap, 200).json(), "item", "fields", "title"), "Live")

		// A listing in preview includes drafts.
		list := n.wantStatus("/collections/article/items?channel=web&sort=rank", 200, previewHeaderName, tok).json()
		if len(list["items"].([]any)) != 2 || list["hasMore"] != false {
			t.Errorf("preview listing = %v", list)
		}
		deq(t, "published listing", n.wantStatus("/collections/article/items?channel=web&total=true", 200).json()["total"], float64(1))

		// An item-scoped token only previews that item.
		scoped := n.previewToken(map[string]any{"channel": "web", "item": draft.String()}, 200)
		deq(t, "scoped item", field(n.wantStatus(path, 200, previewHeaderName, scoped).json(), "item", "fields", "title"), "Edited")
		deq(t, "other item is published", field(n.wantStatus(ap, 200, previewHeaderName, scoped).json(), "item", "fields", "title"), "Live")
		n.wantStatus("/items/"+n.create("article", "", "en", `{"title":"Other draft"}`, false).String()+"?channel=web", 404, previewHeaderName, scoped)

		// Bad tokens: another channel, tampered, garbage, expired.
		n.wantStatus(path+"&locale=en", 200, previewHeaderName, tok)
		n.wantStatus("/items/"+draft.String()+"?channel=web_de", 401, previewHeaderName, tok)
		n.wantStatus(path, 401, previewHeaderName, tok+"x")
		n.wantStatus(path, 401, previewHeaderName, "garbage")
		n.wantStatus(path, 401, previewHeaderName, tamper(t, scoped))
		n.clk.Advance(16 * time.Minute)
		n.wantStatus(path, 401, previewHeaderName, tok)
		n.wantStatus(path, 404) // the published view needs no token
	})
}

const previewHeaderName = "X-Hydrogen-Preview"

func TestDeliveryPreviewOfTokenChannelNeedsTheToken(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		draft := n.create("person", "", "en", `{"fullName":"Dan"}`, false)
		tok := n.previewToken(map[string]any{"channel": "app"}, 200)
		p := "/items/" + draft.String()
		n.wantStatus(p, 401, previewHeaderName, tok) // a preview token does not replace the channel token
		r := n.wantStatus(p, 200, previewHeaderName, tok, "Authorization", "Bearer "+appToken)
		deq(t, "no-store", r.Header().Get("Cache-Control"), "no-store")
		n.wantStatus(p, 404, "Authorization", "Bearer "+appToken)
	})
}

func TestMintPreviewTokenValidation(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		n.previewToken(map[string]any{"channel": "web", "ttl": 3600}, 200)
		n.previewToken(map[string]any{"channel": "web", "ttl": "30m"}, 200)
		n.previewToken(map[string]any{"channel": "web", "ttl": 3601}, 400)
		n.previewToken(map[string]any{"channel": "web", "ttl": "2h"}, 400)
		n.previewToken(map[string]any{"channel": "web", "ttl": 0}, 400)
		n.previewToken(map[string]any{"channel": "nope"}, 400)
		n.previewToken(map[string]any{}, 400)
		n.previewToken(map[string]any{"channel": "web", "item": "nope"}, 400)
		n.previewToken(map[string]any{"channel": "web", "extra": 1}, 400)
		if r := n.do("POST", "/api/manage/v1/preview-tokens", nil, map[string]any{"channel": "web"}); r.Code != 401 {
			t.Errorf("without the management token: %d", r.Code)
		}
		doc := n.manage("GET", "/openapi.json", nil).json()
		if field(doc, "paths", "/preview-tokens", "post", "operationId") != "createPreviewToken" {
			t.Error("manage OpenAPI lacks POST /preview-tokens")
		}
		// The expiry is a wall time from the injected clock.
		var out struct{ ExpiresAt time.Time }
		raw := n.manage("POST", "/preview-tokens", map[string]any{"channel": "web", "ttl": "10m"})
		if err := json.Unmarshal(raw.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if want := n.clk.Now().Add(10 * time.Minute); !out.ExpiresAt.Equal(want) {
			t.Errorf("expiresAt = %v, want %v", out.ExpiresAt, want)
		}
	})
}

// deliveryMod exercises the extension points: a decorator, a route, a public channel option and Vary.
type deliveryMod struct{ decorated atomic.Int64 }

type urlDecorator struct{ m *deliveryMod }

func (d urlDecorator) Decorate(ctx context.Context, ch *channel.Channel, items []*delivery.Item) error {
	d.m.decorated.Add(1)
	for _, it := range items {
		it.Extra = map[string]any{"url": "/" + ch.Name + "/" + it.Locale + "/" + it.ID, "links": map[string]string{"zeta": "/z", "alpha": "/a"}}
	}
	delivery.TagContext(ctx, "x:decorated")
	return nil
}

func (m *deliveryMod) Name() string { return "deliverymod" }

func (m *deliveryMod) Register(r *kernel.Registry) error {
	r.ChannelOption(channel.Option{Name: "flavour", Decode: func(n *config.Node) (any, error) {
		s, err := n.String()
		return map[string]string{"name": s}, err
	}})
	r.PublicChannelOption("flavour")
	r.Decorator(urlDecorator{m})
	r.PreviewURL(func(_ context.Context, ch *channel.Channel, item ids.UUID, typ, locale string) (string, bool, error) {
		if typ == "person" {
			return "", false, errors.New("resolver failed")
		}
		return "/" + ch.Name + "/" + locale + "/" + typ + "/" + item.String() + "?x=1", ch.Name == "web" && typ == "article", nil
	})
	r.DeliveryVary(func(ch *channel.Channel) []string {
		if ch.Name == "web" {
			return []string{"x-market"}
		}
		return nil
	})
	r.DeliveryRoute(delivery.Route{Method: "GET", Path: "/echo/{id}", Summary: "Echo",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			id, err := ids.Parse(r.PathValue("id"))
			if err != nil {
				delivery.Fail(w, r, content.ErrNotFound)
				return
			}
			out, err := delivery.Items(r, "de", id)
			if err != nil || out[0] == nil {
				delivery.Fail(w, r, content.ErrNotFound)
				return
			}
			delivery.Tag(r, "x:echo")
			delivery.WriteJSON(w, r, 200, map[string]any{"channel": delivery.Channel(r).Name, "locale": delivery.Locale(r),
				"languages": delivery.Languages(r), "item": out[0], "market": r.Header.Get("X-Market")})
		}})
	return nil
}

func TestMintPreviewURL(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, &deliveryMod{})
		art := n.create("article", "k", "en", `{"title":"Hi"}`, false)
		mint := func(body map[string]any) map[string]any {
			r := n.manage("POST", "/preview-tokens", body)
			if r.Code != 200 {
				t.Fatalf("%v: %d %s", body, r.Code, r.Body)
			}
			return r.json()
		}
		m := mint(map[string]any{"channel": "web", "item": art.String()})
		if want := "/web/en/article/" + art.String() + "?x=1&preview=" + url.QueryEscape(m["token"].(string)); m["url"] != want {
			t.Errorf("url %v, want %q", m["url"], want)
		}
		if _, ok := mint(map[string]any{"channel": "web"})["url"]; ok {
			t.Error("url without an item")
		}
		if _, ok := mint(map[string]any{"channel": "app", "item": art.String()})["url"]; ok {
			t.Error("url from a provider that declined")
		}
		n.previewToken(map[string]any{"channel": "web", "item": art.String(), "locale": "xx"}, 400)
		if _, ok := mint(map[string]any{"channel": "people", "item": art.String()})["url"]; ok {
			t.Error("url for a collection the channel does not serve")
		}
		person := n.create("person", "", "en", `{"fullName":"Ann"}`, false)
		if r := n.manage("POST", "/preview-tokens", map[string]any{"channel": "web", "item": person.String()}); r.Code != 500 {
			t.Errorf("resolver error: status %d", r.Code)
		}
	})
}

func TestDeliveryExtensionPoints(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		m := &deliveryMod{}
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, m)
		art := n.create("article", "k", "en", `{"title":"Hi"}`, false)
		n.update(art, "de", `{"title":"Hallo"}`)
		n.publish(art)

		r := n.wantStatus("/items/"+art.String()+"?channel=web_de", 200)
		item := field(r.json(), "item", "url")
		deq(t, "url", item, "/web_de/de/"+art.String())
		// Decorator keys are spliced in after the brace, sorted, ahead of the stored payload.
		body := r.Body.String()
		wantPrefix := `{"item":{"links":{"alpha":"/a","zeta":"/z"},"url":"/web_de/de/` + art.String() + `","id":"` + art.String() + `"`
		if !strings.HasPrefix(body, wantPrefix) {
			t.Errorf("body = %.200s", body)
		}
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "x:decorated") {
			t.Errorf("decorator tag missing: %q", r.Header().Get("Surrogate-Key"))
		}
		// With a fallback the marker sorts among the decorator keys.
		ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
		body = n.wantStatus("/items/"+ann.String()+"?channel=web_de", 200).Body.String()
		if !strings.HasPrefix(body, `{"item":{"fallback":true,"links":`) {
			t.Errorf("fallback body = %.120s", body)
		}
		before := m.decorated.Load()
		n.wantStatus("/items/"+ann.String()+"?channel=web_de", 200)
		deq(t, "decorated again on a cache hit", m.decorated.Load(), before)
		n.wantStatus("/items/"+art.String()+"?channel=web_de&include=1", 200)
		deq(t, "one decorator call per response", m.decorated.Load(), before+1)

		// A module route: channel, locale, helpers, tags, caching.
		e := n.wantStatus("/echo/"+art.String()+"?channel=web&locale=en", 200)
		em := e.json()
		deq(t, "channel", em["channel"], "web")
		deq(t, "locale", em["locale"], "en")
		deq(t, "items locale", field(em, "item", "locale"), "de")
		deq(t, "languages", asJSON(em["languages"]), `["en","de"]`)
		tags := e.Header().Get("Surrogate-Key")
		if !strings.Contains(tags, "x:echo") || !strings.Contains(tags, "i:"+art.String()) {
			t.Errorf("route tags = %q", tags)
		}
		deq(t, "route etag", e.Header().Get("Etag") != "", true)
		n.wantStatus("/echo/"+art.String()+"?channel=web&locale=en", 304, "If-None-Match", e.Header().Get("Etag"))
		n.wantStatus("/echo/nope?channel=web", 404)
	})
}

func TestDeliveryVaryHeaders(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, &deliveryMod{})
		art := n.create("article", "", "en", `{"title":"Hi"}`, true)
		p := "/items/" + art.String() + "?channel=web"
		deq(t, "vary", n.wantStatus(p, 200).Header().Get("Vary"), "Authorization, X-Market")
		deq(t, "vary elsewhere", n.wantStatus("/items/"+art.String()+"?channel=web_de", 200).Header().Get("Vary"), "Authorization")
		// The header value is part of the cache key.
		e := n.wantStatus("/echo/"+art.String()+"?channel=web", 200, "X-Market", "sg").json()
		deq(t, "market", e["market"], "sg")
		e = n.wantStatus("/echo/"+art.String()+"?channel=web", 200, "X-Market", "de").json()
		deq(t, "market", e["market"], "de")
		e = n.wantStatus("/echo/"+art.String()+"?channel=web", 200, "X-Market", "sg").json()
		deq(t, "market", e["market"], "sg")
	})
}

func TestDeliveryPublicChannelOptions(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWithChannels(t, b.open(t, clk), clk, deliveryChannels+"  tasty: { languages: [en], flavour: mint }\n", &deliveryMod{})
		r := n.wantStatus("/channels/tasty", 200).json()
		deq(t, "option", asJSON(r["options"]), `{"flavour":{"name":"mint"}}`)
		deq(t, "no options elsewhere", asJSON(n.wantStatus("/channels/web", 200).json()["options"]), `{}`)
	})
}

func TestPublicChannelOptionMustExist(t *testing.T) {
	clk := newClock()
	_, err := tryNode(t, openSQLite(t, clk), clk, deliveryChannels, fnMod{"badmod", func(r *kernel.Registry) error {
		r.PublicChannelOption("nope")
		return nil
	}})
	if err == nil || !strings.Contains(err.Error(), `public channel option "nope"`) {
		t.Errorf("err = %v", err)
	}
}

func newDeliveryNodeWith(t *testing.T, db store.DB, clk *clock.Manual, mods ...kernel.Module) *dnode {
	t.Helper()
	return newDeliveryNode(t, db, clk, mods...)
}

func newDeliveryNodeWithChannels(t testing.TB, db store.DB, clk *clock.Manual, channels string, mods ...kernel.Module) *dnode {
	t.Helper()
	n, err := tryNode(t, db, clk, channels, mods...)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func deq(t *testing.T, what string, got, want any) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestDeliveryCacheDisabled(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n, err := tryNodeConfig(t, b.open(t, clk), clk, deliveryChannels, "delivery: { cache_bytes: 0 }\n")
		if err != nil {
			t.Fatal(err)
		}
		art := n.create("article", "", "en", `{"title":"A"}`, true)
		p := "/items/" + art.String() + "?channel=web"
		r := n.wantStatus(p, 200)
		before := n.db.dbCalls()
		n.wantStatus(p, 304, "If-None-Match", r.Header().Get("Etag"))
		if n.db.dbCalls() == before {
			t.Error("a response was served from a disabled cache")
		}
		if e, _ := n.app.Services().Delivery.CacheStats(); e != 0 {
			t.Errorf("%d entries", e)
		}
	})
}

func TestDeliveryCacheBytesBound(t *testing.T) {
	clk := newClock()
	n, err := tryNodeConfig(t, openSQLite(t, clk), clk, deliveryChannels, "delivery: { cache_bytes: 20000 }\n")
	if err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		art := n.create("article", "", "en", fmt.Sprintf(`{"title":"Article %d"}`, i), true)
		n.wantStatus("/items/"+art.String()+"?channel=web", 200)
	}
	entries, used := n.app.Services().Delivery.CacheStats()
	if used > 20000 || entries == 0 || entries >= 30 {
		t.Errorf("%d entries, %d bytes in a 20000 byte cache", entries, used)
	}
}

func TestDeliveryPreviewSecret(t *testing.T) {
	clk := newClock()
	mk := func(yaml string) *dnode {
		n, err := tryNodeConfig(t, openSQLite(t, clk), clk, deliveryChannels, yaml)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	a, b, c := mk("delivery: { preview_secret: \"${env:PREVIEW_ONE}\" }\n"), mk("delivery: { preview_secret: \"${env:PREVIEW_ONE}\" }\n"), mk("delivery: { preview_secret: \"${env:PREVIEW_TWO}\" }\n")
	tok := a.previewToken(map[string]any{"channel": "web"}, 200)
	id := ids.UUID{5}.String()
	for name, n := range map[string]*dnode{"same secret": b} {
		if r := n.get("/items/"+id+"?channel=web", previewHeaderName, tok); r.Code != 404 {
			t.Errorf("%s: %d", name, r.Code)
		}
	}
	if r := c.get("/items/"+id+"?channel=web", previewHeaderName, tok); r.Code != 401 {
		t.Errorf("another secret: %d", r.Code)
	}
	// Without a configured secret it derives from the management token, so nodes sharing it agree.
	d, e := mk(""), mk("")
	tok = d.previewToken(map[string]any{"channel": "web"}, 200)
	if r := e.get("/items/"+id+"?channel=web", previewHeaderName, tok); r.Code != 404 {
		t.Errorf("derived secret: %d", r.Code)
	}
	if r := a.get("/items/"+id+"?channel=web", previewHeaderName, tok); r.Code != 401 {
		t.Errorf("derived vs configured: %d", r.Code)
	}
}

func TestDeliveryRouteRegistrationErrors(t *testing.T) {
	noop := func(http.ResponseWriter, *http.Request) {}
	for name, tc := range map[string]struct {
		rt   delivery.Route
		want string
	}{
		"post":      {delivery.Route{Method: "POST", Path: "/x", Handler: noop}, "method GET"},
		"no path":   {delivery.Route{Method: "GET", Path: "x", Handler: noop}, "path starting with /"},
		"no func":   {delivery.Route{Method: "GET", Path: "/x"}, "handler"},
		"duplicate": {delivery.Route{Method: "GET", Path: "/items/{id}", Handler: noop}, "module badmod: route GET /items/{id}"},
	} {
		clk := newClock()
		_, err := tryNode(t, openSQLite(t, clk), clk, deliveryChannels, fnMod{"badmod", func(r *kernel.Registry) error {
			r.DeliveryRoute(tc.rt)
			return nil
		}})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestDeliveryHandlerPanicIsContained(t *testing.T) {
	clk := newClock()
	n, err := tryNode(t, openSQLite(t, clk), clk, deliveryChannels, fnMod{"boom", func(r *kernel.Registry) error {
		r.DeliveryRoute(delivery.Route{Method: "GET", Path: "/boom", Handler: func(http.ResponseWriter, *http.Request) { panic("boom") }})
		r.DeliveryRoute(delivery.Route{Method: "GET", Path: "/fail", Handler: func(w http.ResponseWriter, r *http.Request) {
			delivery.Fail(w, r, fmt.Errorf("secret detail"))
		}})
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	n.wantStatus("/boom?channel=web", 500)
	r := n.wantStatus("/fail?channel=web", 500)
	if strings.Contains(r.Body.String(), "secret detail") {
		t.Errorf("an internal error leaked: %s", r.Body)
	}
	n.wantStatus("/items/x/y/z", 404)
}

// TestPreviewListsCoveredDraft checks that a token scoped to a never-published item lists it first, and that
// paging through such a listing still shows every published item exactly once.
func TestPreviewListsCoveredDraft(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		var want []string
		for i := 1; i <= 12; i++ {
			title := fmt.Sprintf("T%02d", i)
			n.create("article", "", "en", fmt.Sprintf(`{"title":%q,"rank":%d}`, title, i), true)
			want = append(want, title)
		}
		draft := n.create("article", "", "en", `{"title":"Draft","rank":100}`, false)
		otherDraft := n.create("article", "", "en", `{"title":"Other","rank":101}`, false)
		published := n.create("article", "", "en", `{"title":"Live","rank":50}`, true)
		n.update(published, "en", `{"title":"Live v2"}`)
		tok := n.previewToken(map[string]any{"channel": "web", "item": draft.String()}, 200)
		q := "&preview=" + url.QueryEscape(tok)

		list := func(query string) (titles []string, hasMore bool, total float64) {
			t.Helper()
			m := n.wantStatus("/collections/article/items?sort=rank&"+query, 200).json()
			for _, it := range m["items"].([]any) {
				titles = append(titles, it.(map[string]any)["fields"].(map[string]any)["title"].(string))
			}
			total, _ = m["total"].(float64)
			return titles, m["hasMore"].(bool), total
		}
		eq := func(what string, got []string, want ...string) {
			t.Helper()
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s: %v, want %v", what, got, want)
			}
		}

		got, _, _ := list("where[rank][gte]=10" + q)
		eq("matching where", got, "Draft", "T10", "T11", "T12", "Live")
		got, _, _ = list("where[rank][lt]=10" + q)
		eq("where that does not match", got, "T01", "T02", "T03", "T04", "T05", "T06", "T07", "T08", "T09")
		got, _, _ = list("q=Draft" + q)
		eq("with q", got)
		got, _, _ = list("channel=web&where[rank][gte]=100")
		eq("without a token", got)
		other := n.previewToken(map[string]any{"channel": "web", "item": otherDraft.String()}, 200)
		got, _, _ = list("where[rank][gte]=100&preview=" + url.QueryEscape(other))
		eq("another item's token", got, "Other")
		liveTok := n.previewToken(map[string]any{"channel": "web", "item": published.String()}, 200)
		got, _, _ = list("where[rank][gte]=50&preview=" + url.QueryEscape(liveTok))
		eq("published item", got, "Live v2")

		all := append([]string{"Draft"}, want...)
		all = append(all, "Live")
		for _, limit := range []int{1, 5, 7, 14, 500} {
			for _, total := range []bool{false, true} {
				var seen []string
				for offset := 0; offset < 20; offset += limit {
					query := fmt.Sprintf("limit=%d&offset=%d", limit, offset)
					if total {
						query += "&total=true"
					}
					page, more, count := list(query + q)
					if total && count != float64(len(all)) {
						t.Errorf("limit %d offset %d: total %v, want %d", limit, offset, count, len(all))
					}
					seen = append(seen, page...)
					if !more {
						break
					}
					if len(page) != limit {
						t.Errorf("limit %d offset %d: %d items but more follow", limit, offset, len(page))
					}
				}
				eq(fmt.Sprintf("paging limit %d total %v", limit, total), seen, all...)
			}
		}
	})
}

// tamper returns the token with one bit of its signature flipped, so it always differs from the original.
func tamper(t *testing.T, tok string) string {
	t.Helper()
	i := strings.LastIndexByte(tok, '.')
	sig, err := base64.RawURLEncoding.DecodeString(tok[i+1:])
	if err != nil || len(sig) == 0 {
		t.Fatalf("token %q has no decodable signature: %v", tok, err)
	}
	sig[0] ^= 1
	return tok[:i+1] + base64.RawURLEncoding.EncodeToString(sig)
}
