package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/exportfmt"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

const exportSchema = `
types:
  home:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
  section:
    page: true
    children: [section, landing]
    fields:
      title: { type: string, translatable: true, required: true }
  landing:
    page: true
    children: []
    fields:
      title: { type: string, translatable: true, required: true }
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true }
  article:
    name: "{title}"
    fields:
      title:       { type: string, translatable: true, required: true }
      slug:        { type: slug, from: title, translatable: true }
      summary:     { type: text, translatable: true }
      publishDate: { type: datetime, index: true }
      category:    { type: string, index: true }
      kind:        { type: enum, values: [news, video], default: news, index: true }
      rating:      { type: number, min: 0, max: 5, index: true }
      featured:    { type: boolean, default: false, index: true }
      author:      { type: ref, to: [person], on_delete: nullify, index: true }
      related:     { type: list, of: { type: ref, to: [article], on_delete: nullify } }
      tags:        { type: list, of: { type: string }, index: true }
      externalUrl: { type: url }
`

// exportSite is what a scenario knows about the site it built.
type exportSite struct {
	items []ids.UUID
	// published lists the items with a published version.
	published []ids.UUID
}

func exportEnv(t *testing.T, db store.DB, clk *clock.Manual, schemaYAML string, extra ...kernel.Module) *pagesEnv {
	t.Helper()
	return newPagesEnv(t, db, clk, pagesOpts{schema: schemaYAML, channels: webChannels, extra: extra})
}

func noopMigrations() kernel.Module {
	return migrationModule{{Name: "0001_noop", Type: "person", Up: func(*content.MigrationDoc) error { return nil }}}
}

// buildExportSite fills an environment with a site that exercises everything an export must carry.
func buildExportSite(t *testing.T, e *pagesEnv) exportSite {
	t.Helper()
	var s exportSite
	add := func(id ids.UUID, published bool) ids.UUID {
		s.items = append(s.items, id)
		if published {
			s.published = append(s.published, id)
		}
		return id
	}
	home, root := e.placed("home", "Home", nil, nil)
	add(home, true)
	press, pressN := e.placed("section", "Press", &root, nil)
	add(press, true)
	e.translate(press, "de", "Presse")
	team, teamN := e.placed("section", "Team", &pressN, nil)
	add(team, true)
	bio, _ := e.placed("landing", "Bio", &teamN, nil)
	add(bio, true)
	careers, careersN := e.placed("section", "Careers", &root, &pressN)
	add(careers, true)
	e.mount(pressN, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}", RedirectField: "externalUrl", RedirectStatus: 307})
	e.mount(careersN, pages.MountSpec{Segment: "tech", Collection: "article", Route: "{publishDate:yyyy}/{slug}", Filter: map[string]any{"category": "tech"}})

	// Slug change and a move leave history redirects behind.
	e.update(press, "en", `{"slug":"newsroom"}`)
	e.mustPublish(press)
	e.move(teamN, careersN, nil)
	if id, err := e.redirect(`{"from":"/old-press","to":"/newsroom"}`); err != nil {
		t.Fatal(err)
	} else {
		add(id, true)
	}

	// Forced redirects: over the root page for one locale, over a page for all; both are rows of route_force.
	for _, js := range []string{`{"from":"/","to":"/newsroom","force":true,"statusCode":"302","forLocale":"de"}`,
		`{"from":"/careers","to":"/newsroom","force":true}`} {
		id := e.create("redirect", "en", js)
		e.mustPublish(id)
		add(id, true)
	}

	var people []ids.UUID
	for _, n := range []string{"Ada", "Bob"} {
		id := e.create("person", "en", fmt.Sprintf(`{"fullName":%q}`, n))
		e.mustPublish(id)
		people = append(people, add(id, true))
	}
	var arts []ids.UUID
	for i := range 12 {
		cat := []string{"tech", "life", "money"}[i%3]
		js := fmt.Sprintf(`{"title":"Article %d about %s","summary":"Summary number %d with ünïcode & <markup>","category":%q,"rating":%d,
			"featured":%t,"author":%q,"tags":["t%d","common"],"publishDate":"2026-0%d-1%dT10:00:00Z"}`,
			i, cat, i, cat, i%6, i%2 == 0, people[i%2], i%4, 1+i%9, i%10)
		if i == 4 {
			js = strings.Replace(js, `"tags"`, `"externalUrl":"https://example.com/four","tags"`, 1)
		}
		if i >= 2 {
			js = strings.Replace(js, `"tags"`, fmt.Sprintf(`"related":[%q],"tags"`, arts[i-2]), 1)
		}
		id := e.article("en", js)
		arts = append(arts, add(id, true))
		if i%3 == 0 {
			e.translate(id, "de", fmt.Sprintf("Artikel %d", i))
		}
	}
	// More versions: three published versions of article 1, the last with a changed slug (history).
	for v := range 2 {
		e.update(arts[1], "en", fmt.Sprintf(`{"summary":"Revised %d","rating":%d}`, v, v+2))
		e.mustPublish(arts[1])
	}
	// A published article with an unpublished draft on top of it, in the shared part and in German.
	e.update(arts[2], "en", `{"category":"drafted"}`)
	e.update(arts[2], "de", `{"title":"Entwurf"}`)
	// A draft-only article (never published).
	add(e.create("article", "en", `{"title":"Not yet","category":"tech"}`), false)
	// A draft-only page item.
	add(e.create("section", "en", `{"title":"Hidden section"}`), false)
	e.drain()
	return s
}

// dump reads everything the export covers out of a database, in canonical form.
type dump struct {
	Items      []dumpItem
	Migrations []string
}

type dumpItem struct {
	Item     store.Item
	Versions []dumpVersion
	Drafts   []store.DraftPart
}

type dumpVersion struct {
	Version   int
	Schema    string
	CreatedAt string
	CreatedBy string
	Shared    string
	Locales   []store.VersionLocale
}

// canon re-encodes a JSON document with sorted keys so that jsonb and text storage compare equal.
func canon(t testing.TB, b []byte) string {
	t.Helper()
	var v any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("canon %q: %v", b, err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func dumpDB(t testing.TB, db store.DB) dump {
	t.Helper()
	var d dump
	err := db.View(ctx, func(tx store.Tx) error {
		es, err := store.ExportOf(tx)
		if err != nil {
			return err
		}
		revs, err := es.SchemaRevisions(ctx)
		if err != nil {
			return err
		}
		hash := map[int]string{}
		for _, r := range revs {
			hash[r.Rev] = r.Hash
		}
		var after ids.UUID
		for {
			items, err := es.ItemsAfter(ctx, after, 50)
			if err != nil || len(items) == 0 {
				return err
			}
			after = items[len(items)-1].ID
			list := make([]ids.UUID, len(items))
			for i, it := range items {
				list[i] = it.ID
			}
			versions, err := es.VersionsOf(ctx, list, false)
			if err != nil {
				return err
			}
			drafts, err := es.DraftPartsFor(ctx, list)
			if err != nil {
				return err
			}
			for _, it := range items {
				di := dumpItem{Item: it}
				for _, v := range versions {
					if v.ItemID != it.ID {
						continue
					}
					dv := dumpVersion{Version: v.Version, Schema: hash[v.SchemaRev], CreatedAt: v.CreatedAt.String(), CreatedBy: v.CreatedBy,
						Shared: canon(t, v.Shared)}
					for _, l := range v.Locales {
						l.Data = []byte(canon(t, l.Data))
						dv.Locales = append(dv.Locales, l)
					}
					di.Versions = append(di.Versions, dv)
				}
				for _, p := range drafts {
					if p.ItemID == it.ID {
						p.Data = []byte(canon(t, p.Data))
						di.Drafts = append(di.Drafts, p)
					}
				}
				d.Items = append(d.Items, di)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	err = db.View(ctx, func(tx store.Tx) error {
		es, _ := store.ExportOf(tx)
		log, err := es.ContentMigrationLog(ctx)
		for _, m := range log {
			d.Migrations = append(d.Migrations, m.Name+"@"+m.AppliedAt.String())
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func asText(v any) string { b, _ := json.MarshalIndent(v, "", " "); return string(b) }

// exportTo exports e into a new directory.
func exportTo(t *testing.T, e *pagesEnv, published bool) (string, *exportfmt.Manifest) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "export")
	m, err := e.app.Export(ctx, dir, kernel.ExportOptions{Version: "test", PublishedOnly: published})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	return dir, m
}

func importFrom(t *testing.T, e *pagesEnv, dir string, allow bool) (*exportfmt.ImportResult, error) {
	t.Helper()
	return e.app.Import(ctx, dir, kernel.ImportOptions{AllowSchemaChange: allow})
}

// compareSites checks that two environments hold identical content and serve identical routes.
func compareSites(t *testing.T, a, b *pagesEnv, s exportSite) {
	t.Helper()
	if da, db := dumpDB(t, a.db), dumpDB(t, b.db); asText(da) != asText(db) {
		t.Errorf("stored items differ:\n%s\n---\n%s", firstDiff(asText(da), asText(db)), "")
	}
	// Snapshots: byte for byte, with etags and references.
	sa, err := a.db.Reader().Snapshots(ctx, s.items, nil)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := b.db.Reader().Snapshots(ctx, s.items, nil)
	if err != nil {
		t.Fatal(err)
	}
	key := func(x store.Snapshot) string { return x.ItemID.String() + "/" + x.Locale }
	byKey := map[string]store.Snapshot{}
	for _, x := range sb {
		byKey[key(x)] = x
	}
	if len(sa) != len(sb) || len(sa) == 0 {
		t.Errorf("%d snapshots, %d after import", len(sa), len(sb))
	}
	for _, x := range sa {
		y, ok := byKey[key(x)]
		if !ok {
			t.Errorf("snapshot %s lost", key(x))
			continue
		}
		if !bytes.Equal(x.Payload, y.Payload) || x.ETag != y.ETag || x.Version != y.Version || !slices.Equal(x.Refs, y.Refs) ||
			!x.PublishedAt.Equal(y.PublishedAt) || x.Type != y.Type || x.Key != y.Key || x.MainLocale != y.MainLocale {
			t.Errorf("snapshot %s differs:\n%s\n%s", key(x), x.Payload, y.Payload)
		}
	}
	// References, as incoming rows per item.
	refs := func(db store.DB) string {
		var out []string
		err := db.View(ctx, func(tx store.Tx) error {
			for _, id := range s.items {
				rs, err := tx.Content().IncomingRefs(ctx, id)
				if err != nil {
					return err
				}
				for _, r := range rs {
					out = append(out, fmt.Sprintf("%s<-%s %s %s %s", id, r.From, r.FieldPath, r.Kind, r.OnDelete))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(out, "\n")
	}
	if ra, rb := refs(a.db), refs(b.db); ra != rb || ra == "" {
		t.Errorf("references differ or are empty:\n%s\n---\n%s", ra, rb)
	}
	// Field index and search through queries.
	for i, q := range []content.ListOptions{
		{Type: "article", Status: store.StatusPublished, Where: []content.Filter{{Field: "category", Op: store.OpEq, Value: "tech"}}},
		{Type: "article", Status: store.StatusPublished, Where: []content.Filter{{Field: "rating", Op: store.OpGte, Value: 3.0}}, Sort: []content.SortSpec{{Field: "rating", Desc: true}, {Field: "name"}}},
		{Type: "article", Status: store.StatusPublished, Where: []content.Filter{{Field: "tags", Op: store.OpContains, Value: "common"}}},
		{Type: "article", Status: store.StatusPublished, Where: []content.Filter{{Field: "featured", Op: store.OpEq, Value: true}}},
		{Type: "article", Status: store.StatusPublished, Sort: []content.SortSpec{{Field: "publishDate"}, {Field: "name"}}},
		{Type: "article", Status: store.StatusPublished, Search: "ünïcode summary"},
		{Type: "article", Status: store.StatusPublished, Search: "Artikel", Locale: "de"},
		{Type: "article", Status: store.StatusAny},
		{Type: "section", Status: store.StatusAny},
	} {
		q.Limit = 100
		ra, err := a.c.List(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		rb, err := b.c.List(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		if asText(ra) != asText(rb) || (i != 6 && len(ra.Items) == 0) {
			t.Errorf("query %d differs (%d vs %d items)", i, len(ra.Items), len(rb.Items))
		}
	}
	// Routes: every route row of every item, Resolve and Locate on each path.
	var paths []string
	for _, id := range s.items {
		pa, err := a.pages.Paths(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		pb, err := b.pages.Paths(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if asText(pa) != asText(pb) {
			t.Errorf("routes of %s differ:\n%s\n%s", id, asText(pa), asText(pb))
		}
		for _, r := range pa {
			paths = append(paths, r.Locale+" "+r.Path)
		}
	}
	paths = append(paths, "en /", "de /", "en /missing", "en /old-press", "de /old-press")
	if len(paths) < 20 {
		t.Errorf("only %d routes to compare", len(paths))
	}
	for _, p := range paths {
		locale, path, _ := strings.Cut(p, " ")
		if locale == "" {
			locale = "en"
		}
		ra, ea := a.pages.Resolve(ctx, "main", locale, path)
		rb, eb := b.pages.Resolve(ctx, "main", locale, path)
		if (ea == nil) != (eb == nil) || asText(ra) != asText(rb) {
			t.Errorf("resolve %s: %s / %v vs %s / %v", p, asText(ra), ea, asText(rb), eb)
		}
		for _, host := range []string{"https://a.test/en", "https://a.test/de"} {
			la, ea := a.pages.Locate(ctx, host+path, http.Header{})
			lb, eb := b.pages.Locate(ctx, host+path, http.Header{})
			if (ea == nil) != (eb == nil) || asText(la) != asText(lb) {
				t.Errorf("locate %s%s: %s vs %s", host, path, asText(la), asText(lb))
			}
		}
	}
	// Views: the working and published view of every item in every locale it has.
	for _, id := range s.items {
		for _, loc := range []string{"en", "de"} {
			for _, draft := range []bool{false, true} {
				va, ea := a.c.Get(ctx, id, content.GetOptions{Locale: loc, Draft: draft})
				vb, eb := b.c.Get(ctx, id, content.GetOptions{Locale: loc, Draft: draft})
				if (ea == nil) != (eb == nil) || asText(va) != asText(vb) {
					t.Errorf("view %s %s draft=%v differs:\n%s\n%s", id, loc, draft, asText(va), asText(vb))
				}
			}
		}
	}
}

func firstDiff(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range min(len(la), len(lb)) {
		if la[i] != lb[i] {
			lo := max(0, i-3)
			return fmt.Sprintf("line %d:\n%s\n---\n%s", i, strings.Join(la[lo:min(len(la), i+3)], "\n"), strings.Join(lb[lo:min(len(lb), i+3)], "\n"))
		}
	}
	return fmt.Sprintf("lengths %d, %d", len(la), len(lb))
}

func freshEnv(t *testing.T, b backend, schemaYAML string, extra ...kernel.Module) *pagesEnv {
	t.Helper()
	clk := newClock()
	clk.Advance(48 * 3600e9) // the import happens later than the export's content
	return exportEnv(t, b.open(t, clk), clk, schemaYAML, extra...)
}

func TestExportImportRoundTrip(t *testing.T) {
	for _, from := range backends {
		for _, to := range backends {
			t.Run(from.name+"_to_"+to.name, func(t *testing.T) {
				src := freshEnv(t, from, exportSchema, noopMigrations())
				site := buildExportSite(t, src)
				if done, err := src.c.Migrate(ctx); err != nil || len(done) != 1 {
					t.Fatalf("migrate: %v %v", done, err)
				}
				dir, m := exportTo(t, src, false)
				if m.Counts.Items != len(site.items) || m.Counts.Versions < len(site.published)+2 || m.Counts.Drafts < 3 ||
					m.Counts.ContentMigrations != 1 || m.Counts.Modules["pages"] < 20 || m.Dialect != from.name || m.PublishedOnly {
					t.Fatalf("counts = %+v", m)
				}
				dst := freshEnv(t, to, exportSchema, noopMigrations())
				res, err := importFrom(t, dst, dir, false)
				if err != nil {
					t.Fatalf("import: %v", err)
				}
				if res.Items != m.Counts.Items || res.Modules["pages"] != m.Counts.Modules["pages"] {
					t.Errorf("result = %+v", res)
				}
				compareSites(t, src, dst, site)
				dst.checkSnapshotsNotStale(site)

				// The import emits exactly one event, which purges every cache.
				if tags := tagsOfImport(t, dst); !slices.Equal(tags, []string{"*"}) {
					t.Errorf("content.imported tags = %v", tags)
				}

				// The imported site is live: it can publish, and new items do not collide with imported ones.
				id := dst.article("en", `{"title":"After import"}`)
				if slices.Contains(site.items, id) {
					t.Errorf("new id collides")
				}
				dst.wantRoutes(id, "en current /newsroom/news/after-import")
				// A second import is refused.
				if _, err := importFrom(t, dst, dir, false); !errors.Is(err, exportfmt.ErrNotEmpty) {
					t.Errorf("second import: %v", err)
				}
			})
		}
	}
}

// checkSnapshotsNotStale verifies the imported snapshots match what the content service builds itself.
func (e *pagesEnv) checkSnapshotsNotStale(s exportSite) {
	e.t.Helper()
	before, err := e.db.Reader().Snapshots(ctx, s.published, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := rebuildItems(e.db, e.c, s.published...); err != nil {
		e.t.Fatal(err)
	}
	after, err := e.db.Reader().Snapshots(ctx, s.published, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	if len(before) != len(after) {
		e.t.Fatalf("%d snapshots, %d after a rebuild", len(before), len(after))
	}
	byKey := map[string]store.Snapshot{}
	for _, x := range after {
		byKey[x.ItemID.String()+x.Locale] = x
	}
	for _, x := range before {
		if y := byKey[x.ItemID.String()+x.Locale]; !bytes.Equal(x.Payload, y.Payload) {
			e.t.Errorf("snapshot %s/%s changes on rebuild", x.ItemID, x.Locale)
		}
	}
}

// tagsOfImport checks that the import queued exactly one content.imported event for the purge consumer and
// returns the cache tags it maps to.
func tagsOfImport(t *testing.T, e *pagesEnv) []string {
	t.Helper()
	var n int
	err := e.db.Tx(ctx, func(tx store.Tx) error {
		ds, err := tx.Outbox().Claim(ctx, kernel.PurgeConsumer, e.clk.Now(), 0, 100000)
		for _, d := range ds {
			if d.Event.Topic == exportfmt.ImportedTopic {
				n++
			}
			if strings.HasPrefix(d.Event.Topic, "item.") {
				t.Errorf("import emitted %s", d.Event.Topic)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d content.imported events", n)
	}
	return e.app.Services().Tags(exportfmt.ImportedTopic, nil)
}

func TestExportThreeHops(t *testing.T) {
	// Postgres → SQLite → Postgres: payload bytes, etags and routing identical at every hop.
	pg, sq := backends[1], backends[0]
	a := freshEnv(t, pg, exportSchema)
	site := buildExportSite(t, a)
	dir1, _ := exportTo(t, a, false)
	b := freshEnv(t, sq, exportSchema)
	if _, err := importFrom(t, b, dir1, false); err != nil {
		t.Fatal(err)
	}
	dir2, _ := exportTo(t, b, false)
	c := freshEnv(t, pg, exportSchema)
	if _, err := importFrom(t, c, dir2, false); err != nil {
		t.Fatal(err)
	}
	compareSites(t, a, b, site)
	compareSites(t, a, c, site)

	// The two exports agree on everything but the dialect and the creation time.
	for _, name := range []string{"items.ndjson", "modules/pages.ndjson", "content_migrations.ndjson"} {
		x, _ := os.ReadFile(filepath.Join(dir1, filepath.FromSlash(name)))
		y, _ := os.ReadFile(filepath.Join(dir2, filepath.FromSlash(name)))
		if canonLines(t, x) != canonLines(t, y) {
			t.Errorf("%s differs between the first and the second export:\n%s", name, firstDiff(canonLines(t, x), canonLines(t, y)))
		}
	}
}

// canonLines canonicalises the JSON of every line (jsonb reorders keys inside documents).
func canonLines(t *testing.T, b []byte) string {
	t.Helper()
	var out []string
	for _, l := range bytes.Split(bytes.TrimSpace(b), []byte("\n")) {
		if len(l) > 0 {
			out = append(out, canon(t, l))
		}
	}
	return strings.Join(out, "\n")
}

func TestExportRefusals(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		src := freshEnv(t, b, exportSchema)
		buildExportSite(t, src)
		dir, _ := exportTo(t, src, false)

		// A non-empty database is refused, and nothing is written.
		busy := freshEnv(t, b, exportSchema)
		busy.page("home", "Home")
		before := dumpDB(t, busy.db)
		if _, err := importFrom(t, busy, dir, false); !errors.Is(err, exportfmt.ErrNotEmpty) {
			t.Errorf("non-empty: %v", err)
		}
		if asText(before) != asText(dumpDB(t, busy.db)) {
			t.Error("a refused import wrote")
		}

		// Another schema is refused unless allowed.
		changed := strings.Replace(exportSchema, "      tags:        { type: list, of: { type: string }, index: true }",
			"      tags:        { type: list, of: { type: string }, index: true }\n      extra:       { type: string }", 1)
		other := freshEnv(t, b, changed)
		_, err := importFrom(t, other, dir, false)
		if !errors.Is(err, exportfmt.ErrSchemaMismatch) {
			t.Fatalf("schema mismatch: %v", err)
		}
		if n := len(dumpDB(t, other.db).Items); n != 0 {
			t.Errorf("%d items written by a refused import", n)
		}
		if _, err := importFrom(t, other, dir, true); err != nil {
			t.Fatalf("allowed schema change: %v", err)
		}
		// Versions keep the schema revision they were written under, which stays the latest one so that
		// serve reports what needs a content migration.
		var latest string
		if err := other.db.View(ctx, func(tx store.Tx) error {
			_, h, _, err := tx.Content().LatestSchemaRev(ctx)
			latest = h
			return err
		}); err != nil || latest != src.app.Services().Schema.Hash() {
			t.Errorf("latest schema revision = %s, %v; want the export's", latest, err)
		}
		if bc, err := other.c.BreakingChanges(ctx); err != nil || len(bc) != 0 {
			t.Errorf("breaking changes after import: %v %v", bc, err)
		}
	})
}

func TestExportTamperedRefusedBeforeWriting(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		src := freshEnv(t, b, exportSchema)
		buildExportSite(t, src)
		dir, _ := exportTo(t, src, false)
		p := filepath.Join(dir, "items.ndjson")
		data, _ := os.ReadFile(p)
		data[len(data)/2] ^= 0x01
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		dst := freshEnv(t, b, exportSchema)
		if _, err := importFrom(t, dst, dir, false); err == nil || !strings.Contains(err.Error(), "damaged") {
			t.Fatalf("import of a tampered export: %v", err)
		}
		if n := len(dumpDB(t, dst.db).Items); n != 0 {
			t.Errorf("%d items written", n)
		}
	})
}

func TestExportPublishedOnly(t *testing.T) {
	for fi, from := range backends {
		t.Run(from.name, func(t *testing.T) {
			src := freshEnv(t, from, exportSchema)
			site := buildExportSite(t, src)
			full := dumpDB(t, src.db)
			dir, m := exportTo(t, src, true)
			if !m.PublishedOnly || m.Counts.Items != len(site.published) || m.Counts.Drafts != 0 || m.Counts.Versions != len(site.published) {
				t.Fatalf("counts = %+v (want %d published items)", m.Counts, len(site.published))
			}
			dst := freshEnv(t, backends[len(backends)-1-fi], exportSchema)
			if _, err := importFrom(t, dst, dir, false); err != nil {
				t.Fatal(err)
			}
			got := dumpDB(t, dst.db)
			if len(got.Items) != len(site.published) {
				t.Fatalf("%d items imported, want %d", len(got.Items), len(site.published))
			}
			byID := map[ids.UUID]dumpItem{}
			for _, it := range full.Items {
				byID[it.Item.ID] = it
			}
			for _, it := range got.Items {
				want := byID[it.Item.ID]
				if len(it.Drafts) != 0 || len(it.Versions) != 1 || it.Versions[0].Version != want.Item.PublishedVersion {
					t.Errorf("item %s: %d versions, %d drafts, published v%d", it.Item.ID, len(it.Versions), len(it.Drafts), want.Item.PublishedVersion)
					continue
				}
				if asText(it.Versions[0]) != asText(want.Versions[want.Item.PublishedVersion-1]) || it.Item != want.Item {
					t.Errorf("item %s differs from its published version in the source", it.Item.ID)
				}
			}
			// What visitors see is unchanged: snapshots and routes.
			site.items = site.published
			compareSnapshotsAndRoutes(t, src, dst, site)
		})
	}
}

func compareSnapshotsAndRoutes(t *testing.T, a, b *pagesEnv, s exportSite) {
	t.Helper()
	sa, _ := a.db.Reader().Snapshots(ctx, s.items, nil)
	sb, _ := b.db.Reader().Snapshots(ctx, s.items, nil)
	if len(sa) != len(sb) || len(sa) == 0 {
		t.Fatalf("%d vs %d snapshots", len(sa), len(sb))
	}
	byKey := map[string]store.Snapshot{}
	for _, x := range sb {
		byKey[x.ItemID.String()+x.Locale] = x
	}
	for _, x := range sa {
		if y := byKey[x.ItemID.String()+x.Locale]; !bytes.Equal(x.Payload, y.Payload) {
			t.Errorf("snapshot %s/%s differs", x.ItemID, x.Locale)
		}
	}
	for _, id := range s.items {
		pa, _ := a.pages.Paths(ctx, id)
		pb, _ := b.pages.Paths(ctx, id)
		if asText(pa) != asText(pb) {
			t.Errorf("routes of %s differ", id)
		}
	}
}

// BenchmarkExportImport5000 exports and imports 5,000 published articles (a fifth with a second version and a
// draft, a tenth translated). One iteration is one full export or import; run with -benchtime=1x.
func BenchmarkExportImport5000(b *testing.B) {
	const n = 5000
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			clk := newClock()
			src := newEnv(b, be.open(b, clk), clk, envConfig{})
			author := src.person("Ada")
			for i := range n {
				v, err := src.svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: doc(b, fmt.Sprintf(
					`{"title":"Article %d","summary":"Some summary text for article %d","rating":%d,"author":%q,"tags":["a","b"],
					"publishDate":"2026-01-01T00:00:00Z"}`, i, i, i%6, author.ID))})
				if err != nil {
					b.Fatal(err)
				}
				if i%10 == 0 {
					src.update(v.ID, "de", fmt.Sprintf(`{"title":"Artikel %d"}`, i))
				}
				src.publish(v.ID)
				if i%5 == 0 {
					src.update(v.ID, "en", `{"summary":"second version"}`)
					src.publish(v.ID)
					src.update(v.ID, "en", `{"summary":"a draft"}`)
				}
				if i%100 == 0 {
					src.tick()
				}
			}
			compiled, err := src.svc.Schema().MarshalJSON()
			if err != nil {
				b.Fatal(err)
			}
			base := b.TempDir()
			dir := ""
			var m *exportfmt.Manifest
			b.Run("export", func(b *testing.B) {
				for i := range b.N {
					dir = filepath.Join(base, fmt.Sprintf("export%d", i))
					m, err = exportfmt.Export(ctx, exportfmt.ExportOptions{DB: src.db, Dir: dir, Clock: clk, Version: "bench",
						SchemaHash: src.svc.Schema().Hash(), Schema: compiled})
					if err != nil {
						b.Fatal(err)
					}
				}
				var size int64
				for _, f := range m.Files {
					size += f.Bytes
				}
				b.ReportMetric(float64(m.Counts.Items)/b.Elapsed().Seconds()*float64(b.N), "items/s")
				b.ReportMetric(float64(size)/1e6, "MB")
			})
			b.Run("import", func(b *testing.B) {
				for range b.N {
					b.StopTimer()
					dclk := newClock()
					dst := newEnv(b, be.open(b, dclk), dclk, envConfig{})
					b.StartTimer()
					res, err := exportfmt.Import(ctx, exportfmt.ImportOptions{DB: dst.db, Content: dst.svc, Outbox: dst.ob, Clock: dclk, Dir: dir})
					if err != nil {
						b.Fatal(err)
					}
					b.ReportMetric(float64(res.Items)/b.Elapsed().Seconds()*float64(b.N), "items/s")
				}
			})
		})
	}
}

// The items layout restores the same site as items.ndjson, and both fingerprint alike.
func TestExportItemsLayout(t *testing.T) {
	src := freshEnv(t, backends[0], exportSchema, noopMigrations())
	site := buildExportSite(t, src)
	if _, err := src.c.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	flat, mf := exportTo(t, src, false)
	dir := filepath.Join(t.TempDir(), "export")
	m, err := src.app.Export(ctx, dir, kernel.ExportOptions{Version: "test", Layout: exportfmt.LayoutItems})
	if err != nil {
		t.Fatal(err)
	}
	if m.Fingerprint() != mf.Fingerprint() {
		t.Fatal("the layout changes the fingerprint")
	}
	if _, err := os.Stat(filepath.Join(dir, "items.ndjson")); err == nil {
		t.Fatal("items.ndjson written in the items layout")
	}
	var files int
	for _, f := range m.Files {
		if strings.HasPrefix(f.Name, "items/") {
			files++
			data, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f.Name)))
			if !bytes.HasPrefix(data, []byte("{\n  \"id\"")) || !bytes.HasSuffix(data, []byte("}\n")) {
				t.Fatalf("%s is not an indented document:\n%.80s", f.Name, data)
			}
		}
	}
	if files != m.Counts.Items {
		t.Fatalf("%d item files for %d items", files, m.Counts.Items)
	}
	for _, d := range []string{dir, flat} {
		dst := freshEnv(t, backends[0], exportSchema, noopMigrations())
		if _, err := importFrom(t, dst, d, false); err != nil {
			t.Fatalf("import %s: %v", d, err)
		}
		compareSites(t, src, dst, site)
	}
}
