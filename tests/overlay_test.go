package tests

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/overlays"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const overlaySchema = `
types:
  article:
    name: "{title}"
    fields:
      title:    { type: string, translatable: true, required: true }
      headline: { type: string, translatable: true, index: true }
      summary:  { type: text, translatable: true }
      see:      { type: ref, to: [article], translatable: true, on_delete: nullify }
      kind:     { type: string }
`

var overlayLangs = map[string]channel.Language{
	"en": {Code: "en"}, "de": {Code: "de"},
	"de-CH": {Code: "de-CH", Inherits: "de"},
	"de-ZH": {Code: "de-ZH", Inherits: "de-CH"},
}

func runOverlay(t *testing.T, fn func(t *testing.T, e *env)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
		fn(t, newEnv(t, b.open(t, clk), clk, envConfig{schema: overlaySchema, overlays: overlays.New(overlayLangs)}))
	})
}

func (e *env) stored(id ids.UUID, version int, locale string) map[string]any {
	e.t.Helper()
	vv, err := e.svc.VersionContent(ctx, id, version)
	if err != nil {
		e.t.Fatal(err)
	}
	d, ok := vv.Locales[locale]
	if !ok {
		e.t.Fatalf("version %d has no %s", version, locale)
	}
	return d.Fields
}

func TestOverlayResolution(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		v := e.create("article", "de", `{"title":"Hallo","headline":"Kopf","summary":"Text","kind":"news"}`)
		id := v.ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Grüezi"}`)

		// Draft view: resolved against the parent's draft view.
		d := e.get(id, "de-CH", true)
		eq(t, "title", d.Fields["title"], any("Grüezi"))
		eq(t, "headline", d.Fields["headline"], any("Kopf"))
		eq(t, "kind", d.Fields["kind"], any("news"))
		eq(t, "inherits", d.Inherits, "de")
		eq(t, "overrides", strings.Join(d.Overrides, ","), "title")
		eq(t, "from", d.InheritedFrom["headline"]+d.InheritedFrom["summary"], "dede")
		if _, ok := d.InheritedFrom["title"]; ok {
			t.Error("title is overridden, not inherited")
		}
		// The parent's draft shows through before it is published.
		e.update(id, "de", `{"summary":"Neu"}`)
		eq(t, "draft summary", e.get(id, "de-CH", true).Fields["summary"], any("Neu"))
		eq(t, "published summary", e.get(id, "de", false).Fields["summary"], any("Text"))

		e.publish(id)
		p := e.get(id, "de-CH", false)
		eq(t, "published title", p.Fields["title"], any("Grüezi"))
		eq(t, "published summary", p.Fields["summary"], any("Neu"))
		eq(t, "name", p.Name, "Grüezi")
		eq(t, "locales", strings.Join(p.Locales, ","), "de,de-CH")
		// Stored documents stay sparse.
		if got := e.stored(id, 2, "de-CH"); len(got) != 1 || got["title"] != "Grüezi" {
			t.Errorf("stored de-CH = %v, want only title", got)
		}
		// A root locale view has no overlay fields.
		eq(t, "root inherits", e.get(id, "de", false).Inherits, "")
	})
}

func TestOverlayChainAndDerivedAvailability(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		id := e.create("article", "de", `{"title":"Hallo","headline":"Kopf","summary":"Text"}`).ID
		e.publish(id)

		// de-CH and de-ZH have no stored document, yet are published through the chain.
		v := e.get(id, "de-ZH", false)
		eq(t, "title", v.Fields["title"], any("Hallo"))
		eq(t, "locale", v.Locale, "de-ZH")
		eq(t, "locales", strings.Join(v.Locales, ","), "de")
		eq(t, "available", strings.Join(v.AvailableLocales, ","), "de,de-CH,de-ZH")
		eq(t, "inherits", v.Inherits, "de-CH")
		eq(t, "from", v.InheritedFrom["title"], "de")
		if len(v.Overrides) != 0 {
			t.Errorf("overrides = %v", v.Overrides)
		}
		for _, l := range []string{"de", "de-CH", "de-ZH"} {
			s := e.snapshot(id, l)
			if !strings.Contains(string(s.Payload), `"locale":"`+l+`"`) || !strings.Contains(string(s.Payload), `"Hallo"`) {
				t.Errorf("%s snapshot = %s", l, s.Payload)
			}
		}
		if _, ok := e.snapshotsOf(id)[id.String()+"/en"]; ok {
			t.Error("en has a snapshot")
		}

		// A channel that lists de-CH first serves it whenever de is published.
		ch := &channel.Channel{Name: "ch", Languages: []string{"de-CH", "de"}}
		cv, err := e.forChannel(id, ch, false)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "channel locale", cv.Locale, "de-CH")

		// A middle override flows to the descendant, which keeps inheriting the rest.
		e.update(id, "de-CH", `{"summary":"Schweiz"}`)
		e.publish(id)
		z := e.get(id, "de-ZH", false)
		eq(t, "chain summary", z.Fields["summary"], any("Schweiz"))
		eq(t, "chain from", z.InheritedFrom["summary"]+z.InheritedFrom["headline"], "de-CHde")
		if !strings.Contains(string(e.snapshot(id, "de-ZH").Payload), "Schweiz") {
			t.Error("de-ZH snapshot lacks the chain override")
		}
	})
}

func TestOverlayParentPublishRebuildsDescendants(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		id := e.create("article", "de", `{"title":"Hallo","summary":"Eins"}`).ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Grüezi"}`)
		e.publish(id)
		before := e.snapshot(id, "de-CH")
		e.drain()

		e.update(id, "de", `{"summary":"Zwei","title":"Hallo2"}`)
		e.publish(id, "de")
		after := e.snapshot(id, "de-CH")
		if after.ETag == before.ETag || after.Version != before.Version+1 {
			t.Errorf("de-CH snapshot not rebuilt: v%d %s -> v%d %s", before.Version, before.ETag, after.Version, after.ETag)
		}
		s := string(after.Payload)
		if !strings.Contains(s, "Zwei") || !strings.Contains(s, "Grüezi") || strings.Contains(s, "Hallo2") {
			t.Errorf("de-CH snapshot = %s", s)
		}
		if got := e.stored(id, after.Version, "de-CH"); len(got) != 1 {
			t.Errorf("de-CH stored document grew: %v", got)
		}
		var saw bool
		for _, ev := range e.drain() {
			if ev.Topic == "item.published" && strings.Contains(string(ev.Payload), `"de-CH"`) && strings.Contains(string(ev.Payload), `"de-ZH"`) {
				saw = true
			}
		}
		if !saw {
			t.Error("item.published does not list the descendant locales")
		}
	})
}

func TestOverlayNullInherits(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		id := e.create("article", "de", `{"title":"Hallo","headline":"Kopf"}`).ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Grüezi","headline":"Chopf"}`)
		e.publish(id)
		eq(t, "override", e.get(id, "de-CH", false).Fields["headline"], any("Chopf"))

		v := e.update(id, "de-CH", `{"headline":null}`)
		eq(t, "draft headline", v.Fields["headline"], any("Kopf"))
		eq(t, "overrides", strings.Join(v.Overrides, ","), "title")
		eq(t, "from", v.InheritedFrom["headline"], "de")
		e.publish(id)
		eq(t, "published headline", e.get(id, "de-CH", false).Fields["headline"], any("Kopf"))
		if got := e.stored(id, 3, "de-CH"); len(got) != 1 || got["title"] != "Grüezi" {
			t.Errorf("stored = %v", got)
		}
		// Overriding with the parent's current value is still an override.
		v = e.update(id, "de-CH", `{"headline":"Kopf"}`)
		eq(t, "overrides", strings.Join(v.Overrides, ","), "headline,title")
		// Resetting everything leaves no draft to publish for the locale.
		e.update(id, "de-CH", `{"headline":null,"title":null}`)
		e.publish(id)
		if got := e.stored(id, 4, "de-CH"); len(got) != 0 {
			t.Errorf("stored = %v", got)
		}
		eq(t, "title", e.get(id, "de-CH", false).Fields["title"], any("Hallo"))
	})
}

func TestOverlayValidationOnResolvedDocument(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		id := e.create("article", "de", `{"title":"Hallo"}`).ID
		e.publish(id)
		// title is required: the parent provides it.
		e.update(id, "de-CH", `{"summary":"Nur Zusammenfassung"}`)
		e.publish(id)
		eq(t, "title", e.get(id, "de-CH", false).Fields["title"], any("Hallo"))

		// Without a parent document the required field is missing from the resolved document.
		en := e.create("article", "en", `{"title":"Hello"}`).ID
		e.publish(en)
		e.update(en, "de-CH", `{"summary":"x"}`)
		e.tick()
		_, err := e.svc.Publish(ctx, en, content.PublishOptions{})
		var ve *content.ValidationError
		if !errors.As(err, &ve) || ve.Locale != "de-CH" {
			t.Fatalf("publish = %v, want a de-CH validation error", err)
		}
	})
}

func TestOverlayProjections(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		target := e.create("article", "de", `{"title":"Ziel"}`).ID
		other := e.create("article", "de", `{"title":"Anderes"}`).ID
		e.publish(target)
		e.publish(other)
		id := e.create("article", "de", `{"title":"Hallo","headline":"Kopf","see":"`+target.String()+`"}`).ID
		e.publish(id)

		// Index rows exist for the overlay locale from the inherited field.
		q := content.ListOptions{Status: store.StatusPublished, Locale: "de-CH", Where: []content.Filter{{Field: "headline", Op: store.OpEq, Value: "Kopf"}}}
		eq(t, "de-CH rows", len(e.delivered(q)), 1)
		q.Locale = "de-ZH"
		eq(t, "de-ZH rows", len(e.delivered(q)), 1)
		q.Locale = "en"
		eq(t, "en rows", len(e.delivered(q)), 0)
		res, err := e.svc.List(ctx, content.ListOptions{Locale: "de-CH", Search: "Hallo", Status: store.StatusPublished})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "search in de-CH", len(res.Items), 1)

		// Overriding the headline moves the row; the inherited locale keeps the old one.
		e.update(id, "de-CH", `{"headline":"Chopf","see":"`+other.String()+`"}`)
		e.publish(id)
		q.Locale = "de-CH"
		eq(t, "old value in de-CH", len(e.delivered(q)), 0)
		q.Where[0].Value = "Chopf"
		eq(t, "new value in de-CH", len(e.delivered(q)), 1)
		q.Locale = "de-ZH"
		eq(t, "descendant follows", len(e.delivered(q)), 1)
		q.Locale = "de"
		eq(t, "parent unchanged", len(e.delivered(q)), 0)

		// References come from the resolved documents.
		if refs := e.snapshot(id, "de-CH").Refs; len(refs) != 1 || refs[0] != other {
			t.Errorf("de-CH refs = %v", refs)
		}
		if refs := e.snapshot(id, "de").Refs; len(refs) != 1 || refs[0] != target {
			t.Errorf("de refs = %v", refs)
		}
		for _, who := range []ids.UUID{target, other} {
			r, err := e.svc.References(ctx, who)
			if err != nil || len(r) != 1 || r[0].ItemID != id {
				t.Errorf("references of %s = %v, %v", who, r, err)
			}
		}
	})
}

func TestOverlayRestoreVersionKeepsSparse(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		id := e.create("article", "de", `{"title":"Hallo","headline":"Kopf"}`).ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Eins"}`)
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Zwei","headline":"Chopf"}`)
		e.publish(id)
		e.tick()
		v, err := e.svc.RestoreVersion(ctx, id, 2, []string{"de-CH"})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "title", v.Fields["title"], any("Eins"))
		eq(t, "headline", v.Fields["headline"], any("Kopf"))
		eq(t, "overrides", strings.Join(v.Overrides, ","), "title")
	})
}

func TestOverlayUnpublishAndTrash(t *testing.T) {
	runOverlay(t, func(t *testing.T, e *env) {
		id := e.create("article", "de", `{"title":"Hallo"}`).ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Grüezi"}`)
		e.publish(id)
		e.tick()
		if _, err := e.svc.Unpublish(ctx, id); err != nil {
			t.Fatal(err)
		}
		if got := e.snapshotsOf(id); len(got) != 0 {
			t.Errorf("snapshots after unpublish: %d", len(got))
		}
		v := e.get(id, "de-CH", true)
		eq(t, "draft after unpublish", v.Fields["title"], any("Grüezi"))
		e.publish(id)
		eq(t, "republished", e.get(id, "de-CH", false).Fields["title"], any("Grüezi"))
		eq(t, "derived republished", e.get(id, "de-ZH", false).Fields["title"], any("Grüezi"))
	})
}

func TestOverlayOffKeepsLocalesIndependent(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{schema: overlaySchema})
		id := e.create("article", "de", `{"title":"Hallo","headline":"Kopf"}`).ID
		e.publish(id)
		e.update(id, "de-CH", `{"title":"Grüezi"}`)
		e.publish(id)
		v := e.get(id, "de-CH", false)
		if _, ok := v.Fields["headline"]; ok || v.Inherits != "" {
			t.Errorf("view = %+v, want an independent locale", v)
		}
		if _, ok := e.snapshotsOf(id)[id.String()+"/de-ZH"]; ok {
			t.Error("derived snapshot without the module")
		}
		eq(t, "available", strings.Join(v.AvailableLocales, ","), "de,de-CH")
	})
}

func TestOverlayConfigWarning(t *testing.T) {
	dir := writeFiles(t, map[string]string{"schema/s.yaml": overlaySchema,
		"config/channels.yaml": "languages:\n  de: {}\n  de-CH: { inherits: de }\n  en-SG: { inherits: en }\n  en: {}\n",
		"config/app.yaml":      "content: { schema_dir: SCHEMA }\n"})
	app := "content: { schema_dir: " + dir + "/schema }\n"
	if err := osWrite(dir+"/config/app.yaml", app); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: dir + "/config", Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	w, err := kernel.Warnings(cfg, sqlite.Module())
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 1 || !strings.Contains(w[0], "de-CH, en-SG") || !strings.Contains(w[0], "overlays module") {
		t.Errorf("warnings = %q", w)
	}
	if w, err = kernel.Warnings(cfg, sqlite.Module(), overlays.Module()); err != nil || len(w) != 0 {
		t.Errorf("with the module: %q, %v", w, err)
	}
}

func osWrite(path, data string) error { return os.WriteFile(path, []byte(data), 0o600) }
