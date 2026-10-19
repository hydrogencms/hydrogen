package tests

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/siteproxy"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

// renderOpt stands in for the site module's `render` channel option.
type renderOpt struct{}

func (renderOpt) Name() string { return "render-stub" }
func (renderOpt) Register(r *kernel.Registry) error {
	r.ChannelOption(channel.Option{Name: "render", Decode: func(n *config.Node) (any, error) { return n.String() }})
	return nil
}

func siteProxyApp(t *testing.T, channels string) (*kernel.App, string, error) {
	t.Helper()
	site := t.TempDir()
	if err := os.WriteFile(filepath.Join(site, "index.html"), []byte("<h1>frontend</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := writeFiles(t, map[string]string{"schema/s.yaml": assetSchema, "config/channels.yaml": channels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\nmedia: { storage: { dir: " + filepath.Join(dir, "media") + " } }\n" +
		"modules:\n  site-proxy:\n    static: { dir: " + site + ", spa_fallback: true }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	clk := newClock()
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: openSQLite(t, clk), Stderr: io.Discard, PasswordIterations: 1000},
		sqlite.Module(), renderOpt{}, siteproxy.Module())
	return a, dir, err
}

func TestSiteProxyYieldsToKernelRoutes(t *testing.T) {
	a, _, err := siteProxyApp(t, "languages: { en: {} }\nchannels:\n  web: { languages: [en] }\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	h, err := a.Handler()
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "127.0.0.1:50000"
		h.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	for _, p := range []string{"/", "/some/page", "/api/other", "/api"} {
		if code, body := get(p); code != http.StatusOK || body != "<h1>frontend</h1>" {
			t.Errorf("%s: %d %q, want the frontend", p, code, body)
		}
	}
	if code, body := get("/healthz"); code != 200 || body != "ok\n" {
		t.Errorf("/healthz: %d %q", code, body)
	}
	if code, body := get("/admin/"); code != 200 || strings.Contains(body, "frontend") {
		t.Errorf("/admin/: %d", code)
	}
	if code, _ := get("/api/manage/v1/system"); code != http.StatusUnauthorized {
		t.Errorf("manage API: %d, want 401 from the kernel", code)
	}
	if code, body := get("/api/deliver/v1/nope"); strings.Contains(body, "frontend") || code == 0 {
		t.Errorf("delivery API: %d %q", code, body)
	}
	if code, body := get("/assets/0000/x.png"); strings.Contains(body, "frontend") {
		t.Errorf("/assets: %d %q", code, body)
	}
}

func TestSiteProxyRefusesRenderedChannels(t *testing.T) {
	_, _, err := siteProxyApp(t, "languages: { en: {} }\nchannels:\n  web: { languages: [en], render: go }\n")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with the site-proxy module") {
		t.Fatalf("err = %v", err)
	}
}
