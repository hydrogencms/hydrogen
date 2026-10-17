package tests

import (
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/overlays"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"github.com/hydrogencms/hydrogen/store"
)

const odSchema = `
types:
  home:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
  section:
    page: true
    children: [section]
    fields:
      title: { type: string, translatable: true, required: true }
  article:
    name: "{title}"
    fields:
      title:    { type: string, translatable: true, required: true }
      headline: { type: string, translatable: true, index: true }
      slug:     { type: slug, from: title, translatable: true }
      related:  { type: ref, to: [section, article], on_delete: nullify }
`

const odChannels = `
languages:
  en: {}
  de: {}
  de-CH: { inherits: de }
channels:
  web_de:   { languages: [de, en],        match: { host: a.test, prefix: /de },    frontend: "https://front.a.test" }
  web_dech: { languages: [de-CH, de, en], match: { host: a.test, prefix: /de-ch }, frontend: "https://front.a.test" }
  web_en:   { languages: [en],            match: { host: a.test, prefix: /en },    frontend: "https://front.a.test" }
`

type odEnv struct {
	*dnode
	pages *pages.Service
}

func newODNode(t testing.TB, db store.DB, clk *clock.Manual) *odEnv {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": odSchema, "config/channels.yaml": odChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\nmodules:\n  pages: { sites: [main] }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	cdb := &delivDB{DB: db}
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: cdb, Stderr: io.Discard, PasswordIterations: 1000},
		sqlite.Module(), postgres.Module(), pages.Module(), overlays.Module())
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
	return &odEnv{dnode: &dnode{t: t, app: a, db: cdb, h: h, clk: clk}, pages: svc}
}

func runOD(t *testing.T, fn func(t *testing.T, e *odEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, newODNode(t, b.open(t, clk), clk))
	})
}

func (e *odEnv) place(typ, title string, parent *ids.UUID) (ids.UUID, ids.UUID) {
	e.t.Helper()
	item := e.create(typ, "", "de", `{"title":"`+title+`"}`, true)
	n, err := e.pages.Place(ctx, "main", pages.Place{Item: item, Parent: parent})
	if err != nil {
		e.t.Fatal(err)
	}
	e.drain()
	return item, n.ID
}

func titles(list any) []string {
	var out []string
	for _, it := range list.([]any) {
		m := it.(map[string]any)
		out = append(out, m["locale"].(string)+":"+field(m, "fields", "headline").(string))
	}
	return out
}

func eqStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s = %v, want %v", what, got, want)
			return
		}
	}
}

// A channel [de-CH, de, en] serves de-CH whenever de is published, field by field from the overlay.
func TestOverlayDeliveryItemsAndFallback(t *testing.T) {
	runOD(t, func(t *testing.T, e *odEnv) {
		a := e.create("article", "", "de", `{"title":"Hallo","headline":"Zebra"}`, true)
		onlyEn := e.create("article", "", "en", `{"title":"Hello","headline":"Only"}`, true)
		e.drain()

		j := e.wantStatus("/items/"+a.String()+"?channel=web_dech", 200).json()
		deq(t, "locale", field(j, "item", "locale"), "de-CH")
		deq(t, "title", field(j, "item", "fields", "title"), "Hallo")
		if field(j, "item", "fallback") != nil {
			t.Error("de-CH is the channel's first language: no fallback marker")
		}
		j = e.wantStatus("/items/"+a.String()+"?channel=web_de", 200).json()
		deq(t, "de locale", field(j, "item", "locale"), "de")

		// The overlay overrides one field; the rest follows de.
		e.update(a, "de-CH", `{"headline":"Alpha"}`)
		e.publish(a)
		e.drain()
		j = e.wantStatus("/items/"+a.String()+"?channel=web_dech", 200).json()
		deq(t, "override", field(j, "item", "fields", "headline"), "Alpha")
		deq(t, "inherited", field(j, "item", "fields", "title"), "Hallo")
		j = e.wantStatus("/items/"+a.String()+"?channel=web_de", 200).json()
		deq(t, "de untouched", field(j, "item", "fields", "headline"), "Zebra")

		// A parent publish reaches the overlay without touching it; the answer is purged.
		e.update(a, "de", `{"title":"Servus"}`)
		e.publish(a)
		e.drain()
		eventually(t, "de-CH to show the new de title", func() bool {
			return field(e.wantStatus("/items/"+a.String()+"?channel=web_dech", 200).json(), "item", "fields", "title") == "Servus"
		})
		j = e.wantStatus("/items/"+a.String()+"?channel=web_dech", 200).json()
		deq(t, "still overridden", field(j, "item", "fields", "headline"), "Alpha")

		// ?locale narrows to a language of the channel.
		j = e.wantStatus("/items/"+a.String()+"?channel=web_dech&locale=de", 200).json()
		deq(t, "narrowed locale", field(j, "item", "locale"), "de")
		deq(t, "narrowed headline", field(j, "item", "fields", "headline"), "Zebra")

		// An item that is not published in de falls back to en.
		j = e.wantStatus("/items/"+onlyEn.String()+"?channel=web_dech", 200).json()
		deq(t, "fallback locale", field(j, "item", "locale"), "en")
		deq(t, "fallback marker", field(j, "item", "fallback"), true)
		e.wantStatus("/items/"+onlyEn.String()+"?channel=web_de", 200)
		// The en channel does not see de content at all.
		e.wantStatus("/items/"+a.String()+"?channel=web_en", 404)

		// Resetting the override (null) inherits again.
		e.update(a, "de-CH", `{"headline":null}`)
		e.publish(a)
		e.drain()
		eventually(t, "de-CH to inherit the headline", func() bool {
			return field(e.wantStatus("/items/"+a.String()+"?channel=web_dech", 200).json(), "item", "fields", "headline") == "Zebra"
		})
	})
}

// Listings filter and sort on the resolved index rows of the overlay locale.
func TestOverlayDeliveryListings(t *testing.T) {
	runOD(t, func(t *testing.T, e *odEnv) {
		zebra := e.create("article", "", "de", `{"title":"Eins","headline":"Zebra"}`, true)
		mango := e.create("article", "", "de", `{"title":"Zwei","headline":"Mango"}`, true)
		e.create("article", "", "en", `{"title":"Three","headline":"Only"}`, true)
		e.update(mango, "de-CH", `{"headline":"Zulu"}`)
		e.publish(mango)
		e.drain()
		list := "/collections/article/items?channel="

		got := e.wantStatus(list+"web_dech&sort=headline", 200).json()["items"]
		eqStrings(t, "de-CH by headline", titles(got), []string{"de-CH:Zebra", "de-CH:Zulu", "en:Only"})
		got = e.wantStatus(list+"web_de&sort=headline", 200).json()["items"]
		eqStrings(t, "de by headline", titles(got), []string{"de:Mango", "de:Zebra", "en:Only"})

		// The filter sees the override and the inherited value, not the parent's overridden one.
		q := func(ch, v string) []string {
			return titles(e.wantStatus(list+ch+"&where[headline][eq]="+url.QueryEscape(v), 200).json()["items"])
		}
		eqStrings(t, "override", q("web_dech", "Zulu"), []string{"de-CH:Zulu"})
		eqStrings(t, "overridden parent value", q("web_dech", "Mango"), nil)
		eqStrings(t, "inherited", q("web_dech", "Zebra"), []string{"de-CH:Zebra"})
		eqStrings(t, "de", q("web_de", "Mango"), []string{"de:Mango"})
		_ = zebra

		j := e.wantStatus(list+"web_dech&total=true", 200).json()
		deq(t, "total", j["total"], float64(3))
	})
}

// Routes, links, alternates and navigation of an overlay locale.
func TestOverlayDeliveryRoutesLinksAndTree(t *testing.T) {
	runOD(t, func(t *testing.T, e *odEnv) {
		_, home := e.place("home", "Start", nil)
		press, _ := e.place("section", "Presse", &home)
		art := e.create("article", "", "de", `{"title":"Hallo","related":"`+press.String()+`"}`, true)
		e.drain()

		route := func(raw, ch string) map[string]any {
			return e.wantStatus(routePath(raw, ch), 200).json()
		}
		j := route("https://a.test/de-ch/presse", "web_dech")
		deq(t, "kind", field(j, "located", "kind"), "page")
		deq(t, "located locale", field(j, "located", "locale"), "de-CH")
		deq(t, "item", field(j, "item", "id"), press.String())
		deq(t, "item locale", field(j, "item", "locale"), "de-CH")
		deq(t, "url", field(j, "item", "url"), "https://front.a.test/de-ch/presse")
		var alts []string
		for _, a := range field(j, "located", "alternates").([]any) {
			m := a.(map[string]any)
			alts = append(alts, m["channel"].(string)+" "+m["locale"].(string)+" "+m["url"].(string))
		}
		eqStrings(t, "alternates", alts, []string{"web_de de https://front.a.test/de/presse",
			"web_dech de-CH https://front.a.test/de-ch/presse"})
		j = route("https://a.test/de/presse", "web_de")
		deq(t, "de locale", field(j, "located", "locale"), "de")

		// Links in an item of the overlay channel point at the overlay's URL for the page.
		j = e.wantStatus("/items/"+art.String()+"?channel=web_dech", 200).json()
		deq(t, "link", field(j, "item", "links", press.String()), "/de-ch/presse")
		deq(t, "item locale", field(j, "item", "locale"), "de-CH")
		j = e.wantStatus("/items/"+art.String()+"?channel=web_de", 200).json()
		deq(t, "de link", field(j, "item", "links", press.String()), "/de/presse")

		// Navigation in the overlay locale: inherited names, overlay paths.
		j = e.wantStatus("/tree?channel=web_dech&depth=2", 200).json()
		deq(t, "tree locale", j["locale"], "de-CH")
		nodes := j["nodes"].([]any)
		child := nodes[0].(map[string]any)
		deq(t, "tree name", child["name"], "Presse")
		deq(t, "tree path", child["path"], "/presse")
		deq(t, "tree url", child["url"], "https://front.a.test/de-ch/presse")

		// A slug override of the overlay moves its route; the answer cache follows.
		e.update(press, "de-CH", `{"title":"Presse CH","slug":"druck"}`)
		e.publish(press)
		e.drain()
		eventually(t, "the overlay route to move", func() bool {
			return field(e.wantStatus(routePath("https://a.test/de-ch/druck", "web_dech"), 200).json(), "located", "kind") == "page"
		})
		j = route("https://a.test/de-ch/presse", "web_dech")
		deq(t, "history redirect", field(j, "located", "kind"), "redirect")
		eventually(t, "links to follow the override", func() bool {
			return field(e.wantStatus("/items/"+art.String()+"?channel=web_dech", 200).json(), "item", "links", press.String()) == "/de-ch/druck"
		})
		deq(t, "de link unchanged", field(e.wantStatus("/items/"+art.String()+"?channel=web_de", 200).json(), "item", "links", press.String()), "/de/presse")
	})
}
