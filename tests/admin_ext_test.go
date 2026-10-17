package tests

import (
	"context"
	"encoding/json"
	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/admin"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

type extMod struct{}

func (extMod) Name() string { return "seo" }
func (extMod) Register(r *kernel.Registry) error {
	if err := r.Policy(access.Policy{Name: "seo.run"}); err != nil {
		return err
	}
	r.AdminExtension(kernel.AdminExtension{
		Files: fstest.MapFS{"el.js": {Data: []byte("export {}")}, "s.json": {Data: []byte(`{"seo":"SEO"}`)}},
		Manifest: admin.Manifest{Elements: []string{"el.js"}, Strings: "s.json",
			Slots: []admin.Slot{{Tag: "x-seo-banner", At: "editor.before", Policy: "seo.run"}}},
	})
	return nil
}

func TestAdminExtensionsEndToEnd(t *testing.T) {
	clk := newClock()
	dir := t.TempDir()
	el := filepath.Join(dir, "elements.js")
	if err := os.WriteFile(el, []byte("export const x = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "admin:\n  elements: [" + el + "]\n  widgets: { hello: x-hello }\n" +
		"  views: [{ path: /tools/import, tag: x-import, menu: Tools, title: Import, policy: seo.run }]\n"
	app := newEventsApp(t, openSQLite(t, clk), clk, yaml, 0, extMod{})
	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	do := func(path string) (int, string) {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w.Code, w.Body.String()
	}
	code, body := do("/admin/config.json")
	var cfg struct {
		Extensions struct {
			Elements []string          `json:"elements"`
			Strings  []string          `json:"strings"`
			Widgets  map[string]string `json:"widgets"`
			Views    []admin.View      `json:"views"`
			Slots    []admin.Slot      `json:"slots"`
		}
	}
	if code != 200 || json.Unmarshal([]byte(body), &cfg) != nil {
		t.Fatalf("config.json: %d %s", code, body)
	}
	e := cfg.Extensions
	if strings.Join(e.Elements, ",") != "/admin/x/m/seo/el.js,/admin/x/0.js" || strings.Join(e.Strings, ",") != "/admin/x/m/seo/s.json" ||
		e.Widgets["hello"] != "x-hello" || len(e.Views) != 1 || len(e.Slots) != 1 {
		t.Fatalf("extensions: %s", body)
	}
	for path, want := range map[string]string{"/admin/x/0.js": "export const x = 1", "/admin/x/m/seo/el.js": "export {}"} {
		if code, body := do(path); code != 200 || body != want {
			t.Errorf("%s: %d %q", path, code, body)
		}
	}
	if code, _ := do("/admin/x/1.js"); code != 404 {
		t.Errorf("undeclared file: %d", code)
	}
}

func TestBuilderPolicyFeedsAdminChecks(t *testing.T) {
	run := func(withPolicy bool) error {
		dir := t.TempDir()
		yaml := "admin:\n" +
			"  views: [{ path: /tools, tag: x-tools, title: Tools, policy: import.run }]\n"
		if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := config.Options{Dir: dir, Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }}
		return hydrogen.Run(context.Background(), []string{"config:check"}, func(h *hydrogen.Builder) {
			h.Use(sqlite.Module())
			if withPolicy {
				h.Policy(access.Policy{Name: "import.run"})
			}
		}, hydrogen.RunOptions{Config: &cfg, Stdout: io.Discard, Stderr: io.Discard})
	}
	if err := run(true); err != nil {
		t.Fatal(err)
	}
	if err := run(false); err == nil || !strings.Contains(err.Error(), `unknown policy`) {
		t.Fatalf("undeclared policy: %v", err)
	}
}
