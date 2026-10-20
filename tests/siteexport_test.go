package tests

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/site"
	"github.com/hydrogencms/hydrogen/modules/siteexport"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

type siteExpEnv struct {
	*siteEnv
	out, pub string
}

func siteExpApp(t *testing.T, b backend, publisher string) *siteExpEnv {
	t.Helper()
	clk := newClock()
	dir := t.TempDir()
	out, pub := filepath.Join(dir, "out"), filepath.Join(dir, "public")
	if publisher == "" {
		publisher = "{ dir: { path: " + pub + ", keep: 2 } }"
	}
	write := func(name, data string) {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/s.yaml", siteSchema)
	write("config/channels.yaml", siteChannels)
	write("config/app.yaml", "content: { schema_dir: "+filepath.Join(dir, "schema")+" }\nmodules:\n  pages: { sites: [main] }\n"+
		"  site: { theme: routes, dir: "+filepath.Join(dir, "nosite")+", routes: [{ path: /posts, type: article, template: posts, limit: 2 }] }\n"+
		"  site-export:\n    out: "+out+"\n    auto: true\n    concurrency: 4\n    redirects: [netlify, nginx, s3, html]\n"+
		"    publisher: "+publisher+"\n")
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	reg := &siteRegistrar{}
	siteRoutes(reg)
	mods := append([]kernel.Module{sqlite.Module(), postgres.Module(), pages.Module(), site.Module(), siteexport.Module()}, reg.mods...)
	app, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard, PasswordIterations: 1000}, mods...)
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
	ps, _ := kernel.LookupService[*pages.Service](s)
	ex, _ := kernel.LookupService[*site.Exporter](s)
	return &siteExpEnv{siteEnv: &siteEnv{t: t, app: app, c: s.Content, pages: ps, ex: ex, clk: clk}, out: out, pub: pub}
}

func (e *siteExpEnv) command(args ...string) {
	e.t.Helper()
	cmd, ok := e.app.Command("site:export")
	if !ok {
		e.t.Fatal("no site:export command")
	}
	if err := cmd.Run(ctx, e.app, args); err != nil {
		e.t.Fatal(err)
	}
}

func (e *siteExpEnv) read(rel string) string {
	b, err := os.ReadFile(filepath.Join(e.out, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(b)
}

func (e *siteExpEnv) manifest() *siteexport.Manifest {
	e.t.Helper()
	m, err := siteexport.LoadManifest(e.out)
	if err != nil || m == nil {
		e.t.Fatalf("manifest: %v %v", m, err)
	}
	return m
}

func (e *siteExpEnv) drain() {
	e.t.Helper()
	if _, err := e.app.Services().Outbox.Drain(ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *siteExpEnv) redirect(from, to string) ids.UUID {
	e.t.Helper()
	e.clk.Advance(1e9)
	v, err := e.c.Create(ctx, content.CreateInput{Type: "redirect", Locale: "en", Fields: doc(e.t, `{"from":"`+from+`","to":"`+to+`"}`)})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.c.Publish(ctx, v.ID, content.PublishOptions{Locales: []string{"en"}}); err != nil {
		e.t.Fatal(err)
	}
	return v.ID
}

func TestSiteExportFullAndIncremental(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := siteExpApp(t, b, "")
		_, rootNode := e.place("home", "Home", nil)
		aboutItem, aboutNode := e.place("section", "About", &rootNode)
		e.place("section", "Team", &aboutNode)
		leafItem, _ := e.place("section", "Leaf", &rootNode)
		for _, title := range []string{"One", "Two", "Three"} {
			e.publishItem("article", title)
		}
		e.redirect("/legacy", "/about")
		e.drain() // no export exists yet: the first event creates it, like the command

		m := e.manifest()
		for _, f := range []string{"en/en/index.html", "en/en/about/index.html", "en/en/about/team/index.html", "en/en/posts/index.html",
			"en/en/posts/page/2/index.html", "en/en/tags/a/index.html", "en/en/tags/b c/index.html", "en/en/404.html"} {
			if e.read(f) == "" {
				t.Errorf("%s missing; manifest: %v", f, slices.Sorted(func(y func(string) bool) {
					for k := range m.Files {
						if !y(k) {
							return
						}
					}
				}))
			}
			if _, ok := m.Files[f]; !ok {
				t.Errorf("%s not in the manifest", f)
			}
		}
		about := m.Files["en/en/about/index.html"]
		if !slices.Contains(about.Tags, "i:"+aboutItem.String()) || about.Kind != "page" || len(about.Hash) != 64 || about.URL != "/en/about" {
			t.Errorf("about entry = %+v", about)
		}
		for f, ent := range m.Files {
			if strings.HasPrefix(f, "en/__hydrogen/assets/") && ent.Kind != "asset" {
				t.Errorf("%s: %+v", f, ent)
			}
		}
		if !slices.ContainsFunc(slices.Collect(func(y func(string) bool) {
			for k := range m.Files {
				if !y(k) {
					return
				}
			}
		}), func(f string) bool { return strings.HasPrefix(f, "en/__hydrogen/assets/") }) {
			t.Error("no asset exported")
		}
		if !strings.Contains(e.read("en/en/about/index.html"), "About") {
			t.Error("about page body")
		}

		// redirects in every format
		if r := e.read("en/_redirects"); !strings.Contains(r, "/en/legacy /en/about 301") && !strings.Contains(r, "/en/legacy /en/about 30") {
			t.Errorf("_redirects = %q", r)
		}
		if r := e.read("redirects.nginx.conf"); !strings.Contains(r, `"~^[^/]+/en/legacy$" "/en/about"`) {
			t.Errorf("nginx = %q", r)
		}
		var rules []map[string]map[string]string
		if err := json.Unmarshal([]byte(e.read("en/s3-routing-rules.json")), &rules); err != nil || len(rules) != 1 ||
			rules[0]["Condition"]["KeyPrefixEquals"] != "en/legacy" || rules[0]["Redirect"]["ReplaceKeyWith"] != "en/about" {
			t.Errorf("s3 rules = %v %v", rules, err)
		}
		if r := e.read("en/en/legacy/index.html"); !strings.Contains(r, `url=/en/about`) {
			t.Errorf("refresh page = %q", r)
		}

		// the dir publisher delivered a release behind the current symlink
		if got, err := os.ReadFile(filepath.Join(e.pub, "current", "en", "en", "about", "index.html")); err != nil || !strings.Contains(string(got), "About") {
			t.Errorf("published: %v", err)
		}

		// a second command run changes nothing
		before := e.manifest()
		e.command()
		after := e.manifest()
		if !equalHashes(before, after) {
			t.Error("a repeated export changed files")
		}

		// incremental: a publish re-renders the page that depends on it only
		homeBefore := e.read("en/en/index.html")
		e.clk.Advance(1e9)
		if _, err := e.c.Update(ctx, aboutItem, content.UpdateInput{Locale: "en", Fields: doc(t, `{"title":"About us"}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.c.Publish(ctx, aboutItem, content.PublishOptions{Locales: []string{"en"}}); err != nil {
			t.Fatal(err)
		}
		e.drain()
		if !strings.Contains(e.read("en/en/about/index.html"), "About us") {
			t.Error("the changed page was not re-rendered")
		}
		// the same tags again: only pages that depend on them render, and nothing is written
		svc, _ := kernel.LookupService[*siteexport.Service](e.app.Services())
		res, err := svc.Export(ctx, siteexport.Options{Tags: []string{"i:" + aboutItem.String()}})
		if err != nil || res.Rendered < 1 || res.Rendered > 3 || len(res.Written) != 0 || len(res.Removed) != 0 {
			t.Errorf("incremental export: %v %+v", err, res)
		}
		// custom routes are tagged with what their layout reads too: the tag page shows the renamed page in its nav
		if !strings.Contains(e.read("en/en/tags/a/index.html"), "About us") {
			t.Error("a custom route page kept the stale navigation")
		}
		_ = homeBefore

		// a new article appears in the listing and gets its own page of the listing
		e.publishItem("article", "Four")
		e.drain()
		if !strings.Contains(e.read("en/en/posts/index.html")+e.read("en/en/posts/page/2/index.html")+e.read("en/en/posts/page/3/index.html"), "Four") {
			t.Error("the new article is in no listing page")
		}

		// unpublishing removes the file
		e.clk.Advance(1e9)
		if e.read("en/en/leaf/index.html") == "" {
			t.Fatal("leaf page missing before it is unpublished")
		}
		if _, err := e.c.Unpublish(ctx, leafItem); err != nil {
			t.Fatal(err)
		}
		e.drain()
		if e.read("en/en/leaf/index.html") != "" {
			t.Error("removed routes left files behind")
		}
		if _, ok := e.manifest().Files["en/en/leaf/index.html"]; ok {
			t.Error("removed file still in the manifest")
		}

		// a new redirect shows up in the files without touching pages
		e.redirect("/old-team", "/")
		e.drain()
		if r := e.read("en/_redirects"); !strings.Contains(r, "/en/old-team") {
			t.Errorf("_redirects = %q", r)
		}
		// the publisher keeps the configured number of releases
		ents, _ := os.ReadDir(filepath.Join(e.pub, "releases"))
		var dirs int
		for _, d := range ents {
			if d.IsDir() {
				dirs++
			}
		}
		if dirs != 2 {
			t.Errorf("releases = %d, want 2 kept", dirs)
		}
		// -full rewrites everything and drops strays
		if err := os.WriteFile(filepath.Join(e.out, "stray.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		e.command("-full")
		if e.read("stray.txt") != "" {
			t.Error("-full kept a stray file")
		}
	})
}

func equalHashes(a, b *siteexport.Manifest) bool {
	if len(a.Files) != len(b.Files) {
		return false
	}
	for f, x := range a.Files {
		if b.Files[f].Hash != x.Hash {
			return false
		}
	}
	return true
}

func TestSiteExportRetriesFailedPublish(t *testing.T) {
	b := backends[0]
	dir := t.TempDir()
	script := filepath.Join(dir, "deploy.sh")
	marker, ran := filepath.Join(dir, "fail"), filepath.Join(dir, "ran")
	body := "#!/bin/sh\necho x >> " + ran + "\n[ ! -e " + marker + " ] || exit 1\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	e := siteExpApp(t, b, "{ command: { argv: ["+script+"] } }")
	e.place("home", "Home", nil)
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	svc, _ := kernel.LookupService[*siteexport.Service](e.app.Services())
	if _, err := svc.Export(ctx, siteexport.Options{}); err == nil {
		t.Fatal("a failing publisher did not fail the export")
	}
	// nothing changed since, yet the next run publishes again, and then not any more
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Export(ctx, siteexport.Options{})
	if err != nil || !res.Published || len(res.Written) != 0 {
		t.Fatalf("retry: %v %+v", err, res)
	}
	res, err = svc.Export(ctx, siteexport.Options{})
	if err != nil || res.Published {
		t.Errorf("published twice: %v %+v", err, res)
	}
	if n := strings.Count(readFile(t, ran), "x"); n != 2 {
		t.Errorf("deploy ran %d times, want 2", n)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
