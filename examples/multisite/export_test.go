package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

// cli runs a project command against the database in vars.
func cli(t *testing.T, vars map[string]string, args ...string) {
	t.Helper()
	vars["MANAGE_TOKEN"], vars["IOS_TOKEN"] = token, iosToken
	vars["PREVIEW_SECRET"] = "test-preview-secret-0123456789abcdef"
	opts := &config.Options{Dir: "config", Env: "prod", LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	var out strings.Builder
	err := hydrogen.Run(context.Background(), args, configureWithProbe, hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: io.Discard})
	if err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
	t.Logf("%s:\n%s", strings.Join(args, " "), out.String())
}

// observed is what the example's clients can see of the site: every URL located, every item as the management
// API shows it.
func (f *site) observed(t *testing.T) string {
	t.Helper()
	urls := []string{
		"https://www.acme.com/en/about", "https://www.acme.com/en", "https://www.acme.com/de/uber-uns",
		"https://www.acme.com/de-ch/uber-eus", "https://www.acme.com/en/about/team", "https://www.acme.com/en/news/hello",
		"https://www.acme.sg/about", "https://jobs.acme.com/open-roles", "https://jobs.acme.com/",
		"https://www.acme.com/de-ch/kontakt", "https://www.acme.com/de/about", "https://jobs.acme.com/about",
		"https://www.acme.com/english/about", "https://www.acme.com/en/cart", "https://www.acme.com/en/contact",
		"https://www.acme.com/en/reach-us", "https://www.acme.com/de/kontakt",
	}
	out := map[string]any{}
	for _, u := range urls {
		out[u] = f.locate(u)
	}
	for name, id := range map[string]string{"home": f.home, "about": f.about, "contact": f.contact, "team": f.team, "cart": f.cart,
		"roles": f.roles, "jobs": f.jobs, "productEN": f.productEN, "productDE": f.productDE, "productFR": f.productFR,
		"hidden": f.productFRHidden, "article": f.article} {
		var v json.RawMessage
		f.s.must(f.s.do("GET", "/items/"+id, nil, &v), http.StatusOK, "get "+name)
		out["item "+name] = v
		var versions json.RawMessage
		f.s.must(f.s.do("GET", "/items/"+id+"/versions", nil, &versions), http.StatusOK, "versions "+name)
		out["versions "+name] = versions
	}
	b, err := json.MarshalIndent(out, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestExportRoundTrip builds the example site on Postgres, exports it with the `export` command, imports it into
// SQLite and back into Postgres, and checks that every URL, item and version looks the same through the API.
func TestExportRoundTrip(t *testing.T) {
	pg1 := pgtest.Scratch(t)
	srv := start(t, "prod", map[string]string{"DATABASE_URL": pg1}, configureWithProbe)
	f := buildSites(srv)
	t.Run("redirects", f.redirects) // leaves history redirects behind
	want := f.observed(t)

	dir1 := filepath.Join(t.TempDir(), "export1")
	cli(t, map[string]string{"DATABASE_URL": pg1}, "export", dir1)
	if m, err := os.ReadFile(filepath.Join(dir1, "manifest.json")); err != nil || !strings.Contains(string(m), `"dialect": "postgres"`) {
		t.Fatalf("manifest: %v\n%s", err, m)
	}

	dirPub := filepath.Join(t.TempDir(), "published")
	cli(t, map[string]string{"DATABASE_URL": pg1}, "export", dirPub, "-published-only")
	if m, err := os.ReadFile(filepath.Join(dirPub, "manifest.json")); err != nil || !strings.Contains(string(m), `"publishedOnly": true`) {
		t.Fatalf("published-only manifest: %v\n%s", err, m)
	}

	lite := "sqlite:" + filepath.Join(t.TempDir(), "h.db")
	cli(t, map[string]string{"DATABASE_URL": lite}, "migrate")
	cli(t, map[string]string{"DATABASE_URL": lite}, "import", dir1)
	// A second import is refused.
	vars := map[string]string{"DATABASE_URL": lite, "MANAGE_TOKEN": token, "IOS_TOKEN": iosToken, "PREVIEW_SECRET": "test-preview-secret-0123456789abcdef"}
	opts := &config.Options{Dir: "config", Env: "prod", LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	if err := hydrogen.Run(context.Background(), []string{"import", dir1}, configureWithProbe, hydrogen.RunOptions{Config: opts, Stdout: io.Discard, Stderr: io.Discard}); err == nil {
		t.Error("importing twice succeeded")
	}
	g := &site{}
	*g = *f
	g.s = start(t, "prod", map[string]string{"DATABASE_URL": lite}, configureWithProbe)
	if got := g.observed(t); got != want {
		t.Errorf("after import into SQLite:\n%s", diffLines(want, got))
	}

	dir2 := filepath.Join(t.TempDir(), "export2")
	cli(t, map[string]string{"DATABASE_URL": lite}, "export", dir2)
	pg2 := pgtest.Scratch(t)
	cli(t, map[string]string{"DATABASE_URL": pg2}, "migrate")
	cli(t, map[string]string{"DATABASE_URL": pg2}, "import", dir2)
	h := &site{}
	*h = *f
	h.s = start(t, "prod", map[string]string{"DATABASE_URL": pg2}, configureWithProbe)
	if got := h.observed(t); got != want {
		t.Errorf("after import back into Postgres:\n%s", diffLines(want, got))
	}
}

func diffLines(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range min(len(la), len(lb)) {
		if la[i] != lb[i] {
			return "line " + strconv.Itoa(i) + ": " + la[i] + "\n  vs   " + lb[i]
		}
	}
	return "different lengths"
}
