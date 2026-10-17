package tests

import (
	"context"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

func TestAdminMount(t *testing.T) {
	get := func(app interface{ Handler() (http.Handler, error) }, path string) *httptest.ResponseRecorder {
		h, err := app.Handler()
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	clk := newClock()
	for _, tc := range []struct {
		name, yaml, path string
		status           int
	}{
		{"default", "", "/admin/", 200},
		{"default deep link", "", "/admin/content/article", 200},
		{"default config", "", "/admin/config.json", 200},
		{"disabled", "admin: { enabled: false }\n", "/admin/", 404},
		{"custom prefix", "admin: { prefix: /ui }\n", "/ui/", 200},
		{"custom prefix leaves the default", "admin: { prefix: /ui }\n", "/admin/", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newEventsApp(t, openSQLite(t, clk), clk, tc.yaml, 0)
			w := get(app, tc.path)
			if w.Code != tc.status {
				t.Fatalf("%s: %d, want %d", tc.path, w.Code, tc.status)
			}
			if tc.status == 200 && !strings.Contains(w.Header().Get("Content-Security-Policy"), "default-src 'self'") {
				t.Fatal("no CSP")
			}
		})
	}
}

func TestRoleTemplatesFollowPagesModule(t *testing.T) {
	clk := newClock()
	policies := func(mods ...kernel.Module) map[string][]string {
		app := newEventsApp(t, openSQLite(t, clk), clk, "", 0, mods...)
		if err := app.Services().Admin.SeedTemplates(ctx); err != nil {
			t.Fatal(err)
		}
		roles, _, err := app.Services().Admin.ListRoles(ctx, store.Page{Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		out := map[string][]string{}
		for _, r := range roles {
			for _, p := range r.Policies {
				out[r.Name] = append(out[r.Name], p.Policy)
			}
		}
		return out
	}
	without := policies()
	with := policies(pages.Module())
	for _, name := range []string{"editor", "contributor", "ai-contributor", "viewer"} {
		if slices.Contains(without[name], "pages.read") {
			t.Errorf("%s has pages.read without the pages module", name)
		}
		if !slices.Contains(with[name], "pages.read") {
			t.Errorf("%s lacks pages.read with the pages module", name)
		}
	}
	for name, want := range map[string]bool{"editor": true, "contributor": true, "ai-contributor": false, "viewer": false} {
		if slices.Contains(with[name], "pages.edit") != want {
			t.Errorf("%s pages.edit = %v, want %v", name, !want, want)
		}
	}
	if slices.Contains(with["administrator"], "pages.read") {
		t.Error("administrator holds * already")
	}
}

func TestManageAPIWithoutContentTypes(t *testing.T) {
	clk := newClock()
	dir := t.TempDir()
	for name, data := range map[string]string{
		"schema/s.yaml":   "types: {}\n",
		"config/app.yaml": "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n",
	} {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	app, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: openSQLite(t, clk), Stderr: io.Discard,
		PasswordIterations: 1000}, sqlite.Module())
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]int{"/api/manage/v1/auth/session": 401, "/admin/config.json": 200} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != want {
			t.Errorf("%s: %d, want %d", path, w.Code, want)
		}
	}
}

// TestAdminFrameOriginsMatchPreviewOrigins checks the CSP frame-src names the origin the pages module builds
// URLs with: the frontend, else https plus the matched host (and never an http dev frontend upgraded).
func TestAdminFrameOriginsMatchPreviewOrigins(t *testing.T) {
	clk := newClock()
	n, err := tryNode(t, openSQLite(t, clk), clk, `
languages: { en: {} }
channels:
  dev:    { languages: [en], frontend: "http://localhost:3000", match: { prefix: /dev } }
  hosted: { languages: [en], match: { host: www.acme.com } }
  bare:   { languages: [en], match: { prefix: /bare } }
`, pages.Module())
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	n.h.ServeHTTP(w, httptest.NewRequest("GET", "/admin/", nil))
	csp := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-src 'self' http://localhost:3000 https://www.acme.com") && !strings.Contains(csp, "frame-src 'self' https://www.acme.com http://localhost:3000") {
		t.Errorf("CSP = %s", csp)
	}
	if strings.Contains(csp, "https://localhost") {
		t.Errorf("an http frontend was upgraded: %s", csp)
	}
}
