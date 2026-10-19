package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func dist() fstest.MapFS {
	return fstest.MapFS{
		"index.html":       {Data: []byte(`<head><script type="module" src="./assets/app-1.js"></script><link href="./assets/app-1.css"></head>`)},
		"assets/app-1.js":  {Data: []byte("console.log(1)")},
		"favicon.svg":      {Data: []byte("<svg/>")},
		"assets/sub/x.css": {Data: []byte("a{}")},
	}
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(method, target, nil))
	return w
}

func TestHandler(t *testing.T) {
	h := Handler("/admin", dist(), Options{APIPrefix: "/api/manage/v1", DeliveryPrefix: "/api/deliver/v1", Pages: true})
	for _, tc := range []struct {
		path, cache string
		status      int
	}{
		{"/admin/", "no-store", 200},
		{"/admin/content/articles/123", "no-store", 200},
		{"/admin/index.html", "no-store", 200},
		{"/admin/assets/app-1.js", "public, max-age=31536000, immutable", 200},
		{"/admin/assets/sub/x.css", "public, max-age=31536000, immutable", 200},
		{"/admin/favicon.svg", "no-cache", 200},
		{"/admin/assets/missing.js", "", 404},
		{"/admin/missing.png", "no-store", 200},
		{"/admin/content/a.b/v1.2", "no-store", 200},
		{"/admin/assets", "no-store", 200},
		{"/admin/../secret.txt", "no-store", 200},
		{"/other/", "", 404},
	} {
		w := get(h, "GET", tc.path)
		if w.Code != tc.status {
			t.Errorf("%s: status %d, want %d", tc.path, w.Code, tc.status)
			continue
		}
		if tc.status == 200 {
			if got := w.Header().Get("Cache-Control"); got != tc.cache {
				t.Errorf("%s: Cache-Control %q, want %q", tc.path, got, tc.cache)
			}
			if w.Header().Get("Content-Security-Policy") == "" {
				t.Errorf("%s: no CSP", tc.path)
			}
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	w := get(Handler("/admin", dist(), Options{}), "GET", "/admin/")
	for k, v := range map[string]string{
		"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "same-origin",
	} {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	c := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self';", "style-src 'self' 'unsafe-inline'", "frame-ancestors 'none'", "base-uri 'none'", "connect-src 'self'"} {
		if !strings.Contains(c, want) {
			t.Errorf("CSP lacks %q: %s", want, c)
		}
	}
	for _, d := range strings.Split(c, ";") {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, "script-src") && strings.Contains(d, "unsafe") {
			t.Errorf("scripts must stay 'self': %s", d)
		}
	}
}

func TestIndexLinksUnderPrefix(t *testing.T) {
	body := get(Handler("/cms/ui", dist(), Options{}), "GET", "/cms/ui/settings/users").Body.String()
	if !strings.Contains(body, `src="/cms/ui/assets/app-1.js"`) || !strings.Contains(body, `href="/cms/ui/assets/app-1.css"`) {
		t.Fatalf("index links not rewritten: %s", body)
	}
}

func TestConfigJSON(t *testing.T) {
	w := get(Handler("/admin", dist(), Options{APIPrefix: "/api/manage/v1", DeliveryPrefix: "/api/deliver/v1", Pages: true, Collab: true}), "GET", "/admin/config.json")
	want := `{"apiPrefix":"/api/manage/v1","prefix":"/admin","pages":true,"collab":true,"deliveryPrefix":"/api/deliver/v1","extensions":` +
		`{"elements":[],"theme":{"name":""},"strings":[],"widgets":{},"cells":{},"panels":[],"actions":[],"dashboard":[],"views":[],"slots":[],"previews":{},"listActions":[]}}`
	if w.Body.String() != want || w.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("config.json: %s %v", w.Body, w.Header())
	}
}

func TestConfigJSONMedia(t *testing.T) {
	w := get(Handler("/admin", dist(), Options{APIPrefix: "/api/manage/v1", DeliveryPrefix: "/api/deliver/v1",
		Media: &Media{MaxUpload: 52428800, AssetBase: "https://cdn.example.com/assets"}}), "GET", "/admin/config.json")
	want := `"deliveryPrefix":"/api/deliver/v1","media":{"maxUpload":52428800,"assetBase":"https://cdn.example.com/assets"},"extensions":`
	if !strings.Contains(w.Body.String(), want) {
		t.Fatalf("config.json: %s", w.Body)
	}
}

func TestMethods(t *testing.T) {
	h := Handler("/admin", dist(), Options{})
	if w := get(h, "POST", "/admin/"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
	if w := get(h, "HEAD", "/admin/"); w.Code != 200 {
		t.Fatalf("HEAD: %d", w.Code)
	}
}

func TestEmbeddedPlaceholder(t *testing.T) {
	w := get(Handler("/admin", Dist(), Options{}), "GET", "/admin/")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<html") {
		t.Fatalf("embedded dist: %d %s", w.Code, w.Body)
	}
}

func TestDevProxy(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		_, _ = w.Write([]byte("vite"))
	}))
	defer up.Close()
	h := Handler("/admin", dist(), Options{DevURL: up.URL, Pages: true})
	w := get(h, "GET", "/admin/src/main.ts")
	if w.Body.String() != "vite" || w.Header().Get("X-Upstream-Path") != "/admin/src/main.ts" {
		t.Fatalf("proxy: %q %v", w.Body, w.Header())
	}
	if w.Header().Get("Content-Security-Policy") != "" {
		t.Fatal("dev proxy must not add a CSP (Vite injects inline scripts)")
	}
	if c := get(h, "GET", "/admin/config.json"); !strings.Contains(c.Body.String(), `"pages":true`) {
		t.Fatalf("config.json is not served locally: %s", c.Body)
	}
}

func TestFrameSrcAllowsChannelFrontendsOnly(t *testing.T) {
	got := frameSrc([]string{"http://localhost:3000/en", "https://www.acme.com", "https://www.acme.com/de",
		"javascript:alert(1)", "https://evil.example ; script-src *", "", "ftp://files.acme.com"})
	want := "; frame-src 'self' http://localhost:3000 https://www.acme.com"
	if got != want {
		t.Fatalf("frameSrc = %q, want %q", got, want)
	}
}

func TestConfigJSONThumbBase(t *testing.T) {
	w := get(Handler("/admin", dist(), Options{Media: &Media{MaxUpload: 1, AssetBase: "/assets", ThumbBase: "/images/admin/1a2b3c4d/480"}}), "GET", "/admin/config.json")
	if want := `"media":{"maxUpload":1,"assetBase":"/assets","thumbBase":"/images/admin/1a2b3c4d/480"}`; !strings.Contains(w.Body.String(), want) {
		t.Fatalf("config.json: %s", w.Body)
	}
}

func TestImageOriginsInCSP(t *testing.T) {
	csp := func(o Options) string {
		return get(Handler("/admin", dist(), o), "GET", "/admin/").Header().Get("Content-Security-Policy")
	}
	if c := csp(Options{}); strings.Contains(c, "media-src") || !strings.Contains(c, "img-src 'self' data: blob:;") {
		t.Fatalf("default CSP changed: %s", c)
	}
	c := csp(Options{ImageOrigins: []string{"https://cdn.example.com", "https://cdn.example.com", "", "javascript:x", "https://img.example.com ; script-src *"}})
	for _, want := range []string{"img-src 'self' data: blob: https://cdn.example.com;", "media-src 'self' https://cdn.example.com"} {
		if !strings.Contains(c, want) {
			t.Errorf("CSP lacks %q: %s", want, c)
		}
	}
	if strings.Contains(c, "img.example.com") || strings.Contains(c, "script-src *") || !strings.Contains(c, "script-src 'self';") {
		t.Fatalf("invalid origin accepted: %s", c)
	}
}
