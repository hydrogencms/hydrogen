package site

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
)

type countFS struct {
	fs.FS
	opens atomic.Int64
}

func (c *countFS) Open(name string) (fs.File, error) {
	if name != "." {
		c.opens.Add(1)
	}
	return c.FS.Open(name)
}

func testAssets(t *testing.T, m fstest.MapFS) (*assetSet, *countFS) {
	t.Helper()
	c := &countFS{FS: m}
	a, err := loadAssets([]Theme{{Name: "t", FS: fstest.MapFS{}}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name := range m {
		a.files[name] = layered{name: name, fsys: c, where: "test/" + name}
	}
	return a, c
}

func get(a *assetSet, name string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", assetsURL+name, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	a.serve(w, r, name)
	return w
}

func shortSum(b string) string {
	s := sha256.Sum256([]byte(b))
	return hex.EncodeToString(s[:5])
}

func TestAssetsHashLazily(t *testing.T) {
	a, c := testAssets(t, fstest.MapFS{
		"app.js":  {Data: []byte("js")},
		"big.bin": {Data: []byte("bin")},
	})
	if n := c.opens.Load(); n != 0 {
		t.Fatalf("%d files opened at load", n)
	}
	u, ok, err := a.url("app.js")
	if err != nil || !ok || u != assetsURL+"app."+shortSum("js")+".js" {
		t.Fatalf("url = %q %v %v", u, ok, err)
	}
	if n := c.opens.Load(); n != 1 {
		t.Fatalf("%d files opened for one asset", n)
	}
	_, _, _ = a.url("app.js")
	if n := c.opens.Load(); n != 1 {
		t.Fatalf("hash not memoised: %d opens", n)
	}
	if _, ok, _ := a.url("nope.js"); ok {
		t.Fatal("missing asset found")
	}
	w := get(a, strings.TrimPrefix(u, assetsURL))
	if w.Code != 200 || w.Body.String() != "js" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("fingerprinted: %d %q %v", w.Code, w.Body, w.Header())
	}
	if w := get(a, "app.0000000000.js"); w.Code != 404 {
		t.Fatalf("stale fingerprint answers %d", w.Code)
	}
}

func TestAssetsValidators(t *testing.T) {
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	a, _ := testAssets(t, fstest.MapFS{"sprite.svg": {Data: []byte("<svg/>"), ModTime: mod}})
	w := get(a, "sprite.svg")
	etag := w.Header().Get("ETag")
	if w.Code != 200 || etag == "" || w.Header().Get("Cache-Control") != "no-cache" || w.Header().Get("Last-Modified") == "" {
		t.Fatalf("unfingerprinted: %d %v", w.Code, w.Header())
	}
	if w := get(a, "sprite.svg", "If-None-Match", etag); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("If-None-Match: %d", w.Code)
	}
	if w := get(a, "sprite.svg", "If-Modified-Since", mod.Add(time.Hour).Format(http.TimeFormat)); w.Code != http.StatusNotModified {
		t.Fatalf("If-Modified-Since: %d", w.Code)
	}
}

func TestAssetsCSSRewrite(t *testing.T) {
	m := fstest.MapFS{
		"css/site.css": {Data: []byte(`@import "base.css";
@import url(other.css) screen;
/* url(fonts/nope.woff2) "x" */
@font-face { src: url(../fonts/a.woff2) format("woff2"), url('../fonts/a.woff?#iefix'), url( "../fonts/a.woff2" ); }
.a { background: url(/img/bg.png); content: "url(../fonts/a.woff2)"; }
.b { background: url("data:image/png;base64,AAA=") , url(https://x.test/a.png), url(//x.test/a.png), url(#frag), url(missing.png), url(../../escape.png); }
.c { fill: url(../img/sprite.svg#icon); }
`)},
		"css/base.css":     {Data: []byte("body{margin:0}")},
		"css/other.css":    {Data: []byte("p{color:red}")},
		"fonts/a.woff2":    {Data: []byte("font2")},
		"fonts/a.woff":     {Data: []byte("font1")},
		"img/bg.png":       {Data: []byte("png")},
		"img/sprite.svg":   {Data: []byte("svg")},
		"css/loop-a.css":   {Data: []byte(`@import "loop-b.css"; a{background:url(../img/bg.png)}`)},
		"css/loop-b.css":   {Data: []byte(`@import "loop-a.css"; b{background:url(../img/bg.png)}`)},
		"css/plain.css":    {Data: []byte("x{y:z}")},
		"css/spaced a.png": {Data: []byte("sp")},
		"css/sp.css":       {Data: []byte("a{b:url(spaced%20a.png)}")},
	}
	a, _ := testAssets(t, m)
	fp := func(n string) string {
		u, ok, err := a.url(n)
		if !ok || err != nil {
			t.Fatalf("url %s: %v %v", n, ok, err)
		}
		return u
	}
	body := get(a, "css/site.css").Body.String()
	for _, want := range []string{
		`@import "` + fp("css/base.css") + `";`,
		`@import url(` + fp("css/other.css") + `) screen;`,
		`url(` + fp("fonts/a.woff2") + `) format("woff2")`,
		`url('` + fp("fonts/a.woff") + `?#iefix')`,
		`url( "` + fp("fonts/a.woff2") + `" )`,
		`url(` + fp("img/bg.png") + `)`,
		`content: "url(../fonts/a.woff2)"`,
		`/* url(fonts/nope.woff2) "x" */`,
		`url("data:image/png;base64,AAA=")`,
		`url(https://x.test/a.png)`, `url(//x.test/a.png)`, `url(#frag)`, `url(missing.png)`, `url(../../escape.png)`,
		`url(` + fp("img/sprite.svg") + `#icon)`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in\n%s", want, body)
		}
	}
	if got := fp("css/plain.css"); got != assetsURL+"css/plain."+shortSum("x{y:z}")+".css" {
		t.Errorf("css without references changed fingerprint: %s", got)
	}
	if !strings.Contains(get(a, "css/sp.css").Body.String(), `url("`+fp("css/spaced a.png")+`")`) {
		t.Errorf("escaped reference not rewritten: %s", get(a, "css/sp.css").Body)
	}
	// the served body hashes to the fingerprint
	u := strings.TrimPrefix(fp("css/site.css"), assetsURL)
	w := get(a, u)
	if w.Code != 200 || !strings.Contains(u, shortSum(w.Body.String())) || w.Body.String() != body {
		t.Errorf("fingerprint does not cover the rewritten body: %s %d", u, w.Code)
	}
	if get(a, "css/site.css").Header().Get("ETag") != get(a, u).Header().Get("ETag") {
		t.Error("etag differs between the plain and the fingerprinted request")
	}

	// cycles: references stay, whichever sheet is asked first
	fresh, _ := testAssets(t, m)
	fa, _, _ := fresh.url("css/loop-a.css")
	fb, _, _ := fresh.url("css/loop-b.css")
	other, _ := testAssets(t, m)
	ob, _, _ := other.url("css/loop-b.css")
	oa, _, _ := other.url("css/loop-a.css")
	if fa != oa || fb != ob {
		t.Errorf("cycle fingerprints depend on order: %s %s / %s %s", fa, fb, oa, ob)
	}
	if w := get(fresh, strings.TrimPrefix(fa, assetsURL)); w.Code != 200 || !strings.Contains(w.Body.String(), `@import "loop-b.css"`) {
		t.Errorf("cyclic reference: %d %s", w.Code, w.Body)
	}
}

func TestAssetsCSSFollowsReferences(t *testing.T) {
	dir := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(dir, "assets", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("site.css", "a{src:url(f.woff2)}")
	write("f.woff2", "one")
	load := func() string {
		a, err := loadAssets(nil, dir)
		if err != nil {
			t.Fatal(err)
		}
		u, _, err := a.url("site.css")
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	before := load()
	write("f.woff2", "two")
	if after := load(); after == before {
		t.Error("css fingerprint did not change with the font")
	}
}

func TestAssetsDevReloadRehashes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "assets", "a.js")
	if err := os.WriteFile(f, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	sig := dirSignature(dir)
	a1, _ := loadAssets(nil, dir)
	u1, _, _ := a1.url("a.js")
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(f, []byte("two!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirSignature(dir) == sig {
		t.Fatal("signature unchanged")
	}
	a2, _ := loadAssets(nil, dir)
	u2, _, _ := a2.url("a.js")
	if u1 == u2 || path.Ext(u2) != ".js" {
		t.Errorf("urls %s %s", u1, u2)
	}
}

func TestAssetsAllForExport(t *testing.T) {
	a, _ := testAssets(t, fstest.MapFS{"a.css": {Data: []byte("a{b:url(x.png)}")}, "x.png": {Data: []byte("p")}})
	list, err := a.all()
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	r, _, done, err := a.body("a.css")
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	b := make([]byte, 100)
	n, _ := r.Read(b)
	if !strings.Contains(string(b[:n]), "x."+shortSum("p")+".png") {
		t.Errorf("export body not rewritten: %s", b[:n])
	}
}
