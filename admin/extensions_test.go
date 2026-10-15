package admin

import (
	"bufio"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func extFixture(t *testing.T) (*Extensions, string) {
	t.Helper()
	dir := t.TempDir()
	for name, data := range map[string]string{"a.js": "export const a = 1", "b.js": "export const b = 2", "logo.svg": "<svg/>",
		"t.css": ":root{--h-accent:#0b6}", "s.json": `{"k":"v"}`, "secret.txt": "no"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := func(n string) string { return filepath.Join(dir, n) }
	x, err := Compose("/admin", []Source{
		{Manifest: Manifest{Elements: []string{p("a.js"), p("b.js")}, Strings: p("s.json"),
			Theme:   Theme{Name: "Acme", Logo: p("logo.svg"), Accent: "#0b6", CSS: p("t.css")},
			Widgets: map[string]string{"brand": "x-brand"}, Panels: []Panel{{Tag: "x-seo", Title: "SEO", Types: []string{"article"}}},
			Views: []View{{Path: "/tools", Tag: "x-tools", Menu: "Tools", Title: "Tools", Policy: "import.run"}}}},
		{Module: "seo", FS: fstest.MapFS{"dist/seo.js": {Data: []byte("export {}")}, "s.json": {Data: []byte("{}")}},
			Manifest: Manifest{Elements: []string{"dist/seo.js"}, Strings: "s.json", Slots: []Slot{{Tag: "x-b", At: "editor.after"}}}},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	return x, dir
}

func TestConfigJSONExtensions(t *testing.T) {
	x, _ := extFixture(t)
	w := get(Handler("/admin", dist(), Options{Extensions: x}), "GET", "/admin/config.json")
	var got struct{ Extensions json.RawMessage }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := `{"elements":["/admin/x/m/seo/dist/seo.js","/admin/x/0.js","/admin/x/1.js"],` +
		`"theme":{"name":"Acme","logo":"/admin/x/logo.svg","accent":"#0b6","css":"/admin/x/theme.css"},` +
		`"strings":["/admin/x/m/seo/s.json","/admin/x/strings.json"],"widgets":{"brand":"x-brand"},"cells":{},` +
		`"panels":[{"tag":"x-seo","title":"SEO","types":["article"]}],"actions":[],"dashboard":[],` +
		`"views":[{"path":"/tools","tag":"x-tools","menu":"Tools","title":"Tools","policy":"import.run"}],` +
		`"slots":[{"tag":"x-b","at":"editor.after"}]}`
	if string(got.Extensions) != want {
		t.Fatalf("extensions:\n%s\nwant\n%s", got.Extensions, want)
	}
}

func TestServeExtensionFiles(t *testing.T) {
	x, dir := extFixture(t)
	h := Handler("/admin", dist(), Options{Extensions: x})
	for _, tc := range []struct {
		path, typ, body string
		status          int
	}{
		{"/admin/x/0.js", "text/javascript; charset=utf-8", "export const a = 1", 200},
		{"/admin/x/1.js", "text/javascript; charset=utf-8", "export const b = 2", 200},
		{"/admin/x/logo.svg", "image/svg+xml", "<svg/>", 200},
		{"/admin/x/theme.css", "text/css; charset=utf-8", ":root{--h-accent:#0b6}", 200},
		{"/admin/x/strings.json", "application/json", `{"k":"v"}`, 200},
		{"/admin/x/m/seo/dist/seo.js", "text/javascript; charset=utf-8", "export {}", 200},
		{"/admin/x/m/seo/s.json", "application/json", "{}", 200},
		{"/admin/x/2.js", "", "", 404},
		{"/admin/x/secret.txt", "", "", 404},
		{"/admin/x/logo.png", "", "", 404},
		{"/admin/x/m/seo/other.js", "", "", 404},
		{"/admin/x/m/other/s.json", "", "", 404},
		{"/admin/x/../config.json/", "", "", 404},
		{"/admin/x/", "", "", 404},
		{"/admin/x/dev/src/elements.ts", "", "", 404},
		{"/admin/x/" + dir + "/a.js", "", "", 404},
	} {
		w := get(h, "GET", tc.path)
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, w.Code, tc.status)
			continue
		}
		if tc.status != 200 {
			continue
		}
		if w.Header().Get("Content-Type") != tc.typ || w.Body.String() != tc.body {
			t.Errorf("%s: %q %q", tc.path, w.Header().Get("Content-Type"), w.Body)
		}
		if w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("ETag") == "" ||
			!strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: headers %v", tc.path, w.Header())
		}
	}
	if w := get(h, "POST", "/admin/x/0.js"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", w.Code)
	}
}

func TestExtensionETag(t *testing.T) {
	x, dir := extFixture(t)
	h := Handler("/admin", dist(), Options{Extensions: x})
	first := get(h, "GET", "/admin/x/0.js").Header().Get("ETag")
	cond := func(etag string) int {
		r := httptest.NewRequest("GET", "/admin/x/0.js", nil)
		r.Header.Set("If-None-Match", etag)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if cond(first) != http.StatusNotModified {
		t.Fatal("matching ETag not answered with 304")
	}
	p := filepath.Join(dir, "a.js")
	if err := os.WriteFile(p, []byte("export const a = 100"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
	w := get(h, "GET", "/admin/x/0.js")
	if w.Header().Get("ETag") == first || w.Body.String() != "export const a = 100" || cond(first) != 200 {
		t.Fatalf("edit on disk not served: %q %v", w.Body, w.Header())
	}
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if w := get(h, "GET", "/admin/x/0.js"); w.Code != 404 {
		t.Fatalf("removed file: %d", w.Code)
	}
	m1 := get(h, "GET", "/admin/x/m/seo/s.json").Header().Get("ETag")
	if m1 == "" || m1 != get(h, "GET", "/admin/x/m/seo/s.json").Header().Get("ETag") {
		t.Fatal("module ETag not stable")
	}
}

func TestComposeErrors(t *testing.T) {
	if _, err := Compose("/admin", []Source{{Manifest: Manifest{Elements: []string{"/no/such.js"}}}}, ""); err == nil {
		t.Error("missing project file accepted")
	}
	if _, err := Compose("/admin", []Source{{Module: "m", FS: fstest.MapFS{}, Manifest: Manifest{Elements: []string{"x.js"}}}}, ""); err == nil {
		t.Error("missing module file accepted")
	}
	a := Source{Manifest: Manifest{Cells: map[string]string{"c": "x-a"}}}
	b := Source{Module: "m", FS: fstest.MapFS{}, Manifest: Manifest{Cells: map[string]string{"c": "x-b"}}}
	if _, err := Compose("/admin", []Source{a, b}, ""); err == nil {
		t.Error("duplicate cell name accepted")
	}
}

func TestElementsDevProxy(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			c, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }()
			_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nhello\n")
			_ = rw.Flush()
			return
		}
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		_, _ = w.Write([]byte("vite"))
	}))
	defer up.Close()
	x, err := Compose("/admin", []Source{{Manifest: Manifest{Elements: []string{"unbuilt.js"}}}}, "/src/elements.ts")
	if err != nil {
		t.Fatal(err)
	}
	h := Handler("/admin", dist(), Options{Extensions: x, ElementsDevURL: up.URL})
	w := get(h, "GET", "/admin/x/dev/src/elements.ts")
	if w.Body.String() != "vite" || w.Header().Get("X-Upstream-Path") != "/admin/x/dev/src/elements.ts" {
		t.Fatalf("proxy: %q %v", w.Body, w.Header())
	}
	if c := get(h, "GET", "/admin/config.json").Body.String(); !strings.Contains(c, `"elements":["/admin/x/dev/src/elements.ts"]`) {
		t.Fatalf("config.json: %s", c)
	}
	if w := get(h, "GET", "/admin/x/0.js"); w.Code != 404 {
		t.Fatalf("built file served in dev mode: %d", w.Code)
	}

	srv := httptest.NewServer(h)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/admin/x/dev/", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("upgrade: %d", resp.StatusCode)
	}
	if line, _ := bufio.NewReader(resp.Body).ReadString('\n'); line != "hello\n" {
		t.Fatalf("upgraded stream: %q", line)
	}
}

func TestNoElementsDevProxyByDefault(t *testing.T) {
	if w := get(Handler("/admin", dist(), Options{}), "GET", "/admin/x/dev/src/elements.ts"); w.Code != 404 {
		t.Fatalf("dev path without a dev server: %d", w.Code)
	}
}

func TestBuiltinRoutesMatchTheAdmin(t *testing.T) {
	b, err := os.ReadFile("ui/src/routes.ts")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range regexp.MustCompile(`path: '(/[^']*)'`).FindAllStringSubmatch(string(b), -1) {
		first := "/" + strings.SplitN(strings.TrimPrefix(m[1], "/"), "/", 2)[0]
		got[first] = true
	}
	want := map[string]bool{}
	for _, r := range BuiltinRoutes {
		want[r] = true
	}
	if !maps.Equal(got, want) {
		t.Fatalf("BuiltinRoutes %v does not match the first segments in routes.ts %v", slices.Sorted(maps.Keys(want)), slices.Sorted(maps.Keys(got)))
	}
}

func TestViewPathSyntax(t *testing.T) {
	for p, ok := range map[string]bool{"/tools": true, "/tools/import": true, "/t/:id": true, "/t/:id/x": true, "/t/*": true, "/t/:id/*": true,
		"/t/{id}": false, "/t/*/x": false, "/t/:": false, "/t/**": false, "tools": false, "/t/": false, "/t//x": false, "/": false} {
		m := Manifest{Views: []View{{Path: p, Tag: "x-a", Title: "t"}}}
		err := m.Check(CheckOptions{Where: "admin"})
		if (err == nil) != ok {
			t.Errorf("%q: err = %v, want ok=%v", p, err, ok)
		}
	}
}
