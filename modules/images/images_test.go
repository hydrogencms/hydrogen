package images

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/features/media"
)

const (
	sha1hex = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	svgSha  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func testPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 7, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fakeMedia struct {
	mu      sync.Mutex
	files   map[string]media.File
	data    map[string][]byte
	derived map[string][]byte
	opens   int
	lookups int
	derErr  error
}

func newFake(t testing.TB) *fakeMedia {
	return &fakeMedia{
		files: map[string]media.File{
			sha1hex: {SHA: sha1hex, MIME: "image/png", Width: 1200, Height: 800},
			svgSha:  {SHA: svgSha, MIME: "image/svg+xml", Width: 10, Height: 10},
		},
		data:    map[string][]byte{sha1hex: testPNG(t, 1200, 800)},
		derived: map[string][]byte{},
	}
}

func (f *fakeMedia) Lookup(_ context.Context, sha string) (media.File, error) {
	f.mu.Lock()
	f.lookups++
	f.mu.Unlock()
	if file, ok := f.files[sha]; ok {
		return file, nil
	}
	return media.File{}, media.ErrNotFound
}

func (f *fakeMedia) Open(_ context.Context, sha string) (io.ReadCloser, media.File, error) {
	f.mu.Lock()
	f.opens++
	f.mu.Unlock()
	d, ok := f.data[sha]
	if !ok {
		return nil, media.File{}, media.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(d)), f.files[sha], nil
}

func (f *fakeMedia) PutDerived(_ context.Context, sha, suffix string, r io.Reader, _ int64, _ string) error {
	b, _ := io.ReadAll(r)
	f.mu.Lock()
	f.derived[sha+"-"+suffix] = b
	f.mu.Unlock()
	return nil
}

func (f *fakeMedia) OpenDerived(_ context.Context, sha, suffix string) (io.ReadCloser, media.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.derErr != nil {
		return nil, media.Info{}, f.derErr
	}
	b, ok := f.derived[sha+"-"+suffix]
	if !ok {
		return nil, media.Info{}, media.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), media.Info{Size: int64(len(b))}, nil
}

func (f *fakeMedia) DeleteDerived(_ context.Context, sha, suffix string) error {
	f.mu.Lock()
	delete(f.derived, sha+"-"+suffix)
	f.mu.Unlock()
	return nil
}

func (f *fakeMedia) ListDerived(_ context.Context, after string, limit int) ([]media.Entry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.derived {
		if k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	out := make([]media.Entry, len(keys))
	for i, k := range keys {
		out[i] = media.Entry{Key: k}
	}
	return out, nil
}

func testModule(t testing.TB, cfg Config) (*module, *fakeMedia) {
	t.Helper()
	m := Module().(*module)
	m.cfg = cfg
	s, err := m.cfg.build(2)
	if err != nil {
		t.Fatal(err)
	}
	m.init(s)
	f := newFake(t)
	m.media = f
	return m, f
}

var cardCfg = Config{Presets: map[string]PresetConfig{
	"card": {Width: 640, Height: 360, Fit: "cover", Widths: []int{480, 320}},
	"hero": {Width: 1600, Format: "jpeg", Quality: 80},
}}

func get(h http.Handler, method, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mux(m *module) http.Handler {
	mx := http.NewServeMux()
	mx.Handle("GET "+URLBase+"/{preset}/{v}/{w}/{focal}/{sha}/{name}", m.handler())
	return mx
}

func TestConfigBuild(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	if got := m.s.names; len(got) != 2 || got[0] != "card" || got[1] != "hero" {
		t.Fatalf("names: %v", got)
	}
	if p := m.s.presets["card"]; len(p.Widths) != 2 || p.Widths[0] != 320 {
		t.Fatalf("widths not sorted: %v", p.Widths)
	}
	if a := m.s.presets[AdminPreset]; a.Width != 480 || a.Height != 480 || a.Fit != Contain {
		t.Fatalf("admin preset: %+v", a)
	}
	if m.s.workers != 1 {
		t.Fatalf("workers: %d", m.s.workers)
	}
	over := cardCfg
	over.Presets = map[string]PresetConfig{"admin": {Width: 200, Height: 200}}
	m2, _ := testModule(t, over)
	if m2.s.presets[AdminPreset].Width != 200 || len(m2.s.names) != 0 {
		t.Fatalf("admin override: %+v names %v", m2.s.presets[AdminPreset], m2.s.names)
	}
}

func TestConfigErrors(t *testing.T) {
	good := "0123456789abcdef"
	for name, tc := range map[string]struct {
		cfg  Config
		want string
	}{
		"driver":     {Config{Driver: "x"}, "driver"},
		"public_url": {Config{PublicURL: "cdn"}, "public_url"},
		"preset":     {Config{Presets: map[string]PresetConfig{"Bad-Name": {Width: 10}}}, "Bad-Name"},
		"cover":      {Config{Presets: map[string]PresetConfig{"a": {Width: 100, Fit: "cover"}}}, "height"},
		"webp":       {Config{Presets: map[string]PresetConfig{"a": {Width: 100, Format: "webp"}}}, "imgproxy"},
		"extra":      {Config{Presets: map[string]PresetConfig{"a": {Width: 100, Widths: []int{100}}}}, "widths"},
		"proxy cfg":  {Config{Imgproxy: ImgproxyConfig{URL: "https://i.example.com"}}, "belongs to driver imgproxy"},
		"proxy url":  {Config{Driver: "imgproxy", Imgproxy: ImgproxyConfig{Key: good, Salt: good, Source: "s3://b/{sha}"}}, "imgproxy.url"},
		"proxy key":  {Config{Driver: "imgproxy", Imgproxy: ImgproxyConfig{URL: "https://i.example.com", Key: "zz", Salt: good, Source: "s3://b/{sha}"}}, "key"},
		"proxy src":  {Config{Driver: "imgproxy", Imgproxy: ImgproxyConfig{URL: "https://i.example.com", Key: good, Salt: good, Source: "s3://b"}}, "source"},
		"max_pixels": {Config{MaxPixels: -1}, "max_pixels"},
		"workers":    {Config{Workers: -1}, "workers"},
	} {
		_, err := tc.cfg.build(2)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", name, err, tc.want)
		}
	}
	ok := Config{Driver: "imgproxy", Presets: map[string]PresetConfig{"a": {Width: 100, Format: "webp"}},
		Imgproxy: ImgproxyConfig{URL: "https://i.example.com", Key: good, Salt: good, Source: "s3://b/{sha}"}}
	if _, err := ok.build(2); err != nil {
		t.Fatal(err)
	}
}

type stubDB struct{}

func (stubDB) Name() string { return "stubdb" }
func (stubDB) Register(r *kernel.Registry) error {
	return r.Database("sqlite", func(context.Context, string, store.OpenOptions) (store.DB, error) { return nil, nil })
}

func checkConfig(t *testing.T, app string, env map[string]string) error {
	t.Helper()
	return checkFiles(t, app, "", env)
}

func checkFiles(t *testing.T, app, channels string, env map[string]string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if channels != "" {
		if err := os.WriteFile(filepath.Join(dir, "config/channels.yaml"), []byte(channels), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	app = "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n" + app
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	if err != nil {
		t.Fatal(err)
	}
	return kernel.Check(cfg, stubDB{}, Module())
}

func TestConfigFileErrors(t *testing.T) {
	err := checkConfig(t, "modules:\n  images:\n    presets:\n      card: { width: 640, colour: red }\n", nil)
	if err == nil || !strings.Contains(err.Error(), "colour") || !strings.Contains(err.Error(), "app.yaml:") {
		t.Fatalf("unknown key: %v", err)
	}
	err = checkConfig(t, "modules:\n  images:\n    presets:\n      card: { width: 0 }\n", nil)
	if err == nil || !strings.Contains(err.Error(), "presets.card") {
		t.Fatalf("width: %v", err)
	}
	err = checkConfig(t, "modules:\n  images:\n    driver: imgproxy\n    imgproxy: { url: 'https://i.example.com', key: 0a0b, salt: 0a0b, source: 's3://b/{sha}' }\n", nil)
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("plain secret: %v", err)
	}
	ok := "modules:\n  images:\n    driver: imgproxy\n    presets: { card: { width: 640, format: webp } }\n" +
		"    imgproxy: { url: 'https://i.example.com', key: '${env:K}', salt: '${env:S}', source: 's3://b/{sha}' }\n"
	if err := checkConfig(t, ok, map[string]string{"K": "0a0b", "S": "0c0d"}); err != nil {
		t.Fatal(err)
	}
	if err := checkConfig(t, "", nil); err != nil {
		t.Fatalf("no config: %v", err)
	}
}

func TestImagePresetsChannelOption(t *testing.T) {
	app := "modules:\n  images:\n    presets: { card: { width: 640 }, hero: { width: 1600 } }\n"
	if err := checkFiles(t, app, "languages: { en: {} }\nchannels:\n  web:\n    languages: [en]\n    image_presets: [card, hero]\n", nil); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ yaml, want string }{
		"unknown": {"image_presets: [card, nope]", "unknown preset"},
		"admin":   {"image_presets: [admin]", "unknown preset"},
		"scalar":  {"image_presets: card", "list"},
	} {
		err := checkFiles(t, app, "languages: { en: {} }\nchannels:\n  web:\n    languages: [en]\n    "+tc.yaml+"\n", nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "channels.yaml:") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestHandlerGeneratesThenStores(t *testing.T) {
	m, f := testModule(t, cardCfg)
	h := mux(m)
	v := m.versions["card"]
	path := "/images/card/" + v + "/640/c/" + sha1hex + "/cat.png"
	r := get(h, "GET", path)
	if r.Code != 200 || r.Header().Get("Hydrogen-Image") != "generated" {
		t.Fatalf("first: %d %v", r.Code, r.Header())
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(r.Body.Bytes()))
	if err != nil || cfg.Width != 640 || cfg.Height != 360 {
		t.Fatalf("variant: %+v %v", cfg, err)
	}
	for k, want := range map[string]string{"Content-Type": "image/png", "Cache-Control": "public, max-age=31536000, immutable",
		"X-Content-Type-Options": "nosniff", "ETag": `"` + sha1hex + "-card-" + v + `-640-c"`,
		"Content-Disposition": `inline; filename=cat.png`} {
		if got := r.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if _, ok := f.derived[sha1hex+"-card-"+v+"-640-c"]; !ok {
		t.Fatalf("not stored: %v", f.derived)
	}
	opens := f.opens
	r2 := get(h, "GET", path)
	if r2.Code != 200 || r2.Header().Get("Hydrogen-Image") != "stored" || !bytes.Equal(r2.Body.Bytes(), r.Body.Bytes()) || f.opens != opens {
		t.Fatalf("second: %d %v", r2.Code, r2.Header())
	}
	r3 := get(h, "GET", path, "If-None-Match", r.Header().Get("ETag"))
	if r3.Code != 304 || r3.Body.Len() != 0 {
		t.Fatalf("conditional: %d", r3.Code)
	}
	r4 := get(h, "HEAD", path)
	if r4.Code != 200 || r4.Body.Len() != 0 || r4.Header().Get("Content-Length") == "" {
		t.Fatalf("head: %d len %d %v", r4.Code, r4.Body.Len(), r4.Header())
	}
}

func TestHandlerJPEGFormatName(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	r := get(mux(m), "GET", "/images/hero/"+m.versions["hero"]+"/1600/c/"+sha1hex+"/cat.jpg")
	if r.Code != 200 || r.Header().Get("Content-Type") != "image/jpeg" || !strings.HasPrefix(r.Body.String(), "\xff\xd8") {
		t.Fatalf("hero: %d %v", r.Code, r.Header())
	}
}

func TestHandlerStatuses(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	h := mux(m)
	v := m.versions["card"]
	for name, tc := range map[string]struct {
		path string
		code int
		loc  string
	}{
		"unknown preset": {"/images/nope/" + v + "/640/c/" + sha1hex + "/a.png", 404, ""},
		"bad width":      {"/images/card/" + v + "/641/c/" + sha1hex + "/a.png", 404, ""},
		"width not num":  {"/images/card/" + v + "/x/c/" + sha1hex + "/a.png", 404, ""},
		"unknown sha":    {"/images/card/" + v + "/640/c/" + strings.Repeat("c", 64) + "/a.png", 404, ""},
		"bad sha":        {"/images/card/" + v + "/640/c/zz/a.png", 404, ""},
		"svg":            {"/images/card/" + v + "/640/c/" + svgSha + "/a.svg", 404, ""},
		"stale v":        {"/images/card/deadbeef/640/c/" + sha1hex + "/cat.png", 302, "/images/card/" + v + "/640/c/" + sha1hex + "/cat.png"},
		"stale bad w":    {"/images/card/deadbeef/641/c/" + sha1hex + "/cat.png", 404, ""},
		"admin":          {"/images/admin/" + m.versions["admin"] + "/480/c/" + sha1hex + "/cat.png", 200, ""},
	} {
		r := get(h, "GET", tc.path)
		if r.Code != tc.code || r.Header().Get("Location") != tc.loc {
			t.Errorf("%s: %d %q", name, r.Code, r.Header().Get("Location"))
		}
		if tc.code == 302 && r.Header().Get("Cache-Control") != "public, max-age=3600" {
			t.Errorf("%s: cache %q", name, r.Header().Get("Cache-Control"))
		}
	}
	m.s.maxPixels = 100
	if r := get(h, "GET", "/images/card/"+v+"/640/c/"+sha1hex+"/a.png"); r.Code != 404 {
		t.Errorf("over max_pixels: %d", r.Code)
	}
	m.media = nil
	if r := get(h, "GET", "/images/card/"+v+"/640/c/"+sha1hex+"/a.png"); r.Code != 404 {
		t.Errorf("without media: %d", r.Code)
	}
}

func TestHandlerBusy(t *testing.T) {
	m, f := testModule(t, cardCfg)
	m.wait = 20 * time.Millisecond
	m.sem <- struct{}{} // the only worker is taken
	h := mux(m)
	r := get(h, "GET", "/images/card/"+m.versions["card"]+"/640/c/"+sha1hex+"/a.png")
	if r.Code != 503 || r.Header().Get("Retry-After") != "5" {
		t.Fatalf("busy: %d %v", r.Code, r.Header())
	}
	if r.Header().Get("ETag") != "" {
		t.Fatal("an error must not carry the ETag")
	}
	<-m.sem
	f.mu.Lock()
	n := len(m.flights)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("flights left: %d", n)
	}
	if r := get(h, "GET", "/images/card/"+m.versions["card"]+"/640/c/"+sha1hex+"/a.png"); r.Code != 200 {
		t.Fatalf("after: %d", r.Code)
	}
}

func TestHandlerSharesOneTransform(t *testing.T) {
	m, f := testModule(t, cardCfg)
	h := mux(m)
	path := "/images/card/" + m.versions["card"] + "/640/c/" + sha1hex + "/a.png"
	var wg sync.WaitGroup
	codes := make([]int, 8)
	for i := range codes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i] = get(h, "GET", path).Code
		}()
	}
	wg.Wait()
	for _, c := range codes {
		if c != 200 {
			t.Fatalf("codes: %v", codes)
		}
	}
	if f.opens > 2 {
		t.Fatalf("source opened %d times", f.opens)
	}
}

func TestHandlerImgproxyRedirect(t *testing.T) {
	cfg := Config{Driver: "imgproxy", Presets: map[string]PresetConfig{"card": {Width: 640, Height: 360, Fit: "cover", Format: "webp"}},
		Imgproxy: ImgproxyConfig{URL: "https://img.example.com", Key: "0a0b0c", Salt: "0d0e0f", Source: "https://cms.example.com/assets/{sha}/{name}"}}
	m, f := testModule(t, cfg)
	r := get(mux(m), "GET", "/images/card/"+m.versions["card"]+"/640/c/"+sha1hex+"/cat.webp")
	loc := r.Header().Get("Location")
	if r.Code != 302 || !strings.HasPrefix(loc, "https://img.example.com/") || !strings.HasSuffix(loc, "@webp") ||
		r.Header().Get("Cache-Control") != "public, max-age=86400" {
		t.Fatalf("redirect: %d %q %v", r.Code, loc, r.Header())
	}
	if f.opens != 0 || len(f.derived) != 0 {
		t.Fatal("imgproxy driver must not transform in process")
	}
}

func TestDecorate(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	asset := func(mime string, w, h any) map[string]any {
		a := map[string]any{"id": "x", "url": "/assets/" + sha1hex + "/cat.png", "name": "cat.png", "mime": mime, "size": json.Number("10"),
			"fields": map[string]any{}}
		if w != nil {
			a["width"], a["height"] = w, h
		}
		return a
	}
	file := map[string]any{"id": sha1hex}
	a := asset("image/png", json.Number("1200"), json.Number("800"))
	a["focal"] = map[string]any{"x": json.Number("0.4"), "y": json.Number("0.3")}
	m.DecorateAsset(nil, file, a)
	vs, _ := a["variants"].(map[string]any)
	card, _ := vs["card"].(map[string]any)
	if len(vs) != 2 || card == nil {
		t.Fatalf("variants: %v", a["variants"])
	}
	v := m.versions["card"]
	want := "/images/card/" + v + "/640/4030/" + sha1hex + "/cat.png"
	if card["url"] != want || card["width"] != 640 || card["height"] != 360 || card["type"] != "image/png" {
		t.Fatalf("card: %v", card)
	}
	set := "/images/card/" + v + "/320/4030/" + sha1hex + "/cat.png 320w, /images/card/" + v + "/480/4030/" + sha1hex + "/cat.png 480w, " + want + " 640w"
	if card["srcset"] != set {
		t.Fatalf("srcset: %v", card["srcset"])
	}
	hero := vs["hero"].(map[string]any)
	if hero["type"] != "image/jpeg" || hero["width"] != 1200 || hero["height"] != 800 || hero["srcset"] != nil ||
		!strings.HasSuffix(hero["url"].(string), "/1600/c/"+sha1hex+"/cat.jpg") {
		t.Fatalf("hero: %v", hero)
	}
	if _, err := json.Marshal(a); err != nil {
		t.Fatal(err)
	}

	small := asset("image/png", json.Number("300"), json.Number("200"))
	m.DecorateAsset(nil, file, small)
	c := small["variants"].(map[string]any)["card"].(map[string]any)
	if c["width"] != 300 || c["height"] != 169 && c["height"] != 168 || c["srcset"] != nil {
		t.Fatalf("small source: %v", c)
	}

	for name, a := range map[string]map[string]any{
		"svg": asset("image/svg+xml", json.Number("10"), json.Number("10")), "pdf": asset("application/pdf", nil, nil),
		"no dims": asset("image/png", nil, nil), "huge": asset("image/png", json.Number("20000"), json.Number("20000")),
	} {
		m.DecorateAsset(nil, file, a)
		if _, ok := a["variants"]; ok {
			t.Errorf("%s: got variants", name)
		}
	}

	ch := &channel.Channel{Options: map[string]any{ChannelOption: []string{"hero"}}}
	only := asset("image/png", json.Number("1200"), json.Number("800"))
	m.DecorateAsset(ch, file, only)
	if vs := only["variants"].(map[string]any); len(vs) != 1 || vs["hero"] == nil {
		t.Fatalf("channel option: %v", vs)
	}

	m.media = nil
	inert := asset("image/png", json.Number("1200"), json.Number("800"))
	m.DecorateAsset(nil, file, inert)
	if _, ok := inert["variants"]; ok {
		t.Fatal("variants without media")
	}
}

func TestDecoratePublicURLAndImgproxy(t *testing.T) {
	cfg := cardCfg
	cfg.PublicURL = "https://cdn.example.com/img/"
	m, _ := testModule(t, cfg)
	a := map[string]any{"mime": "image/png", "name": "a b.png", "width": json.Number("1200"), "height": json.Number("800")}
	m.DecorateAsset(nil, map[string]any{"id": sha1hex}, a)
	u := a["variants"].(map[string]any)["hero"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(u, "https://cdn.example.com/img/hero/") || !strings.HasSuffix(u, "/a%20b.jpg") {
		t.Fatalf("url: %s", u)
	}
	im := m.adminImages()
	if im.ThumbBase != "https://cdn.example.com/img/admin/"+m.versions["admin"]+"/480/c" || len(im.Origins) != 1 || im.Origins[0] != "https://cdn.example.com" {
		t.Fatalf("admin images: %+v", im)
	}

	px := Config{Driver: "imgproxy", Presets: map[string]PresetConfig{"card": {Width: 640, Height: 360, Fit: "cover", Widths: []int{320}}},
		Imgproxy: ImgproxyConfig{URL: "https://img.example.com", Key: "0a0b0c", Salt: "0d0e0f", Source: "s3://bucket/{sha}"}}
	m, _ = testModule(t, px)
	b := map[string]any{"mime": "image/png", "name": "a.png", "width": json.Number("1200"), "height": json.Number("800"),
		"focal": map[string]any{"x": json.Number("0.25"), "y": json.Number("0.75")}}
	m.DecorateAsset(nil, map[string]any{"id": sha1hex}, b)
	c := b["variants"].(map[string]any)["card"].(map[string]any)
	if !strings.HasPrefix(c["url"].(string), "https://img.example.com/") || !strings.Contains(c["url"].(string), "g:fp:0.25:0.75") ||
		c["srcset"] == nil {
		t.Fatalf("imgproxy card: %v", c)
	}
	im = m.adminImages()
	if len(im.Origins) != 1 || im.Origins[0] != "https://img.example.com" || !strings.HasPrefix(im.ThumbBase, "/images/admin/") {
		t.Fatalf("admin images: %+v", im)
	}
}

func TestAdminImagesInertWithoutMedia(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	m.media = nil
	if im := m.adminImages(); im.ThumbBase != "" || len(im.Origins) != 0 {
		t.Fatalf("%+v", im)
	}
}

func TestGC(t *testing.T) {
	m, f := testModule(t, cardCfg)
	v := m.versions["card"]
	other := strings.Repeat("c", 64)
	keep := []string{
		sha1hex + "-card-" + v + "-640-c", sha1hex + "-card-" + v + "-320-4030", sha1hex + "-hero-" + m.versions["hero"] + "-1600-c",
		other + "-admin-" + m.versions["admin"] + "-480-c",
	}
	drop := []string{
		sha1hex + "-card-deadbeef-640-c", sha1hex + "-card-" + v + "-641-c", sha1hex + "-gone-0123abcd-640-c", other + "-card-" + v + "-100-c",
		sha1hex + "-card-" + v + "-640", sha1hex + "-card-" + v + "-640-x1", sha1hex + "-hero-" + m.versions["hero"] + "-1600-4030", sha1hex + "-thumb",
	}
	for _, k := range append(slicesClone(keep), drop...) {
		f.derived[k] = []byte("x")
	}
	if err := m.gc(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	for _, k := range keep {
		if _, ok := f.derived[k]; !ok {
			t.Errorf("removed %s", k)
		}
	}
	for _, k := range drop {
		if _, ok := f.derived[k]; ok {
			t.Errorf("kept %s", k)
		}
	}
}

func slicesClone(s []string) []string { return append([]string(nil), s...) }

func TestHandlerFocal(t *testing.T) {
	m, f := testModule(t, cardCfg)
	known := map[string]bool{"2550": true}
	m.focals = func(_ context.Context, sha string) (map[string]bool, error) {
		out := map[string]bool{}
		for k, v := range known {
			out[k] = v && sha == sha1hex
		}
		return out, nil
	}
	h := mux(m)
	v := m.versions["card"]
	// a square source, red on the top half and blue below
	f.files[sha1hex] = media.File{SHA: sha1hex, MIME: "image/png", Width: 1200, Height: 1200}
	f.data[sha1hex] = func() []byte {
		sq := image.NewRGBA(image.Rect(0, 0, 1200, 1200))
		for x := range 1200 {
			for y := range 1200 {
				if y < 600 {
					sq.Set(x, y, color.RGBA{255, 0, 0, 255})
				} else {
					sq.Set(x, y, color.RGBA{0, 0, 255, 255})
				}
			}
		}
		var bb bytes.Buffer
		_ = png.Encode(&bb, sq)
		return bb.Bytes()
	}()
	top := get(h, "GET", "/images/card/"+v+"/640/2510/"+sha1hex+"/a.png")
	if top.Code != 404 {
		t.Fatalf("made-up focal: %d", top.Code)
	}
	known["2510"] = true
	m.focalCache = newTTLCache[focalSet](m.clock, 8) // the miss was remembered
	top = get(h, "GET", "/images/card/"+v+"/640/2510/"+sha1hex+"/a.png")
	bot := get(h, "GET", "/images/card/"+v+"/640/c/"+sha1hex+"/a.png")
	if top.Code != 200 || bot.Code != 200 {
		t.Fatalf("focal: %d %d", top.Code, bot.Code)
	}
	im, err := png.Decode(bytes.NewReader(top.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if r, _, bl, _ := im.At(320, 180).RGBA(); r>>8 < 200 || bl>>8 > 50 {
		t.Fatalf("focal crop did not pick the top (red) part: %v", im.At(320, 180))
	}
	if top.Header().Get("ETag") == bot.Header().Get("ETag") || !strings.HasSuffix(top.Header().Get("ETag"), `-640-2510"`) {
		t.Fatalf("etags: %q %q", top.Header().Get("ETag"), bot.Header().Get("ETag"))
	}
	// stored variants are served without asking again
	m.focalCache = newTTLCache[focalSet](m.clock, 8) // forget what is known
	known["2510"] = false
	if r := get(h, "GET", "/images/card/"+v+"/640/2510/"+sha1hex+"/a.png"); r.Code != 200 || r.Header().Get("Hydrogen-Image") != "stored" {
		t.Fatalf("stored focal: %d", r.Code)
	}
	for name, p := range map[string]string{
		"bad token":   "/images/card/" + v + "/640/25/" + sha1hex + "/a.png",
		"letters":     "/images/card/" + v + "/640/25ab/" + sha1hex + "/a.png",
		"contain":     "/images/hero/" + m.versions["hero"] + "/1600/2550/" + sha1hex + "/a.png",
		"stale focal": "/images/card/deadbeef/640/9999/" + sha1hex + "/a.png",
	} {
		if r := get(h, "GET", p); r.Code != 404 && (name != "stale focal" || r.Code != 302) {
			t.Errorf("%s: %d", name, r.Code)
		}
	}
}

func TestFocalToken(t *testing.T) {
	for in, want := range map[*Focal]string{nil: "c", {0.4, 0.3}: "4030", {0, 1}: "0099", {1, 0.5}: "9950", {0.255, 0.004}: "2600", {-1, 2}: "0099"} {
		if got := focalToken(in); got != want {
			t.Errorf("%v: %s, want %s", in, got, want)
		}
	}
	if f, ok := parseFocal("4030"); !ok || f.X != 0.4 || f.Y != 0.3 {
		t.Fatalf("parse: %v %v", f, ok)
	}
}

func TestPanickingTransformIsContained(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	m.transform = func(context.Context, io.Reader, Preset, int, *Focal, int64) ([]byte, string, error) { panic("boom") }
	h := mux(m)
	path := "/images/card/" + m.versions["card"] + "/640/c/" + sha1hex + "/a.png"
	for range 2 {
		if r := get(h, "GET", path); r.Code != 500 {
			t.Fatalf("panic: %d", r.Code)
		}
	}
	if len(m.flights) != 0 || len(m.sem) != 0 {
		t.Fatalf("flights %d, workers in use %d", len(m.flights), len(m.sem))
	}
}

func TestTransformTimeoutIsNotBusy(t *testing.T) {
	m, _ := testModule(t, cardCfg)
	m.limit = 20 * time.Millisecond
	m.transform = func(ctx context.Context, _ io.Reader, _ Preset, _ int, _ *Focal, _ int64) ([]byte, string, error) {
		<-ctx.Done()
		return nil, "", ctx.Err()
	}
	r := get(mux(m), "GET", "/images/card/"+m.versions["card"]+"/640/c/"+sha1hex+"/a.png")
	if r.Code != 500 || r.Header().Get("Retry-After") != "" {
		t.Fatalf("timeout: %d %v", r.Code, r.Header())
	}
}

func TestStorageErrorDoesNotRegenerate(t *testing.T) {
	m, f := testModule(t, cardCfg)
	f.derErr = errors.New("disk on fire")
	r := get(mux(m), "GET", "/images/card/"+m.versions["card"]+"/640/c/"+sha1hex+"/a.png")
	if r.Code != 500 || f.opens != 0 || len(f.derived) != 0 {
		t.Fatalf("storage error: %d opens %d", r.Code, f.opens)
	}
}

func TestMadeUpFocalCostsOneLoad(t *testing.T) {
	m, f := testModule(t, cardCfg)
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	m.clock = clk
	m.init(m.s)
	m.media = f
	loads := 0
	m.focals = func(context.Context, string) (map[string]bool, error) { loads++; return map[string]bool{}, nil }
	h := mux(m)
	path := "/images/card/" + m.versions["card"] + "/640/7777/" + sha1hex + "/a.png"
	other := "/images/card/" + m.versions["card"] + "/640/7778/" + sha1hex + "/a.png"
	for _, p := range []string{path, path, other} {
		if r := get(h, "GET", p); r.Code != 404 {
			t.Fatalf("%s: %d", p, r.Code)
		}
	}
	if loads != 1 {
		t.Fatalf("loads: %d", loads)
	}
	clk.Advance(time.Minute)
	if r := get(h, "GET", path); loads != 2 || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("after the TTL: %d loads, Cache-Control %q", loads, r.Header().Get("Cache-Control"))
	}
	// a focal point just set is accepted after focalRefresh, not focalTTL
	m.focals = func(context.Context, string) (map[string]bool, error) { loads++; return map[string]bool{"7777": true}, nil }
	if get(h, "GET", path); loads != 2 {
		t.Fatalf("reloaded within focalRefresh: %d", loads)
	}
	clk.Advance(focalRefresh)
	if r := get(h, "GET", path); r.Code != 200 || loads != 3 {
		t.Fatalf("new focal: %d after %d loads", r.Code, loads)
	}
}

func TestLookupIsCached(t *testing.T) {
	m, f := testModule(t, cardCfg)
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	m.clock = clk
	m.init(m.s)
	m.media = f
	h := mux(m)
	path := "/images/card/" + m.versions["card"] + "/640/c/" + sha1hex + "/a.png"
	r := get(h, "GET", path)
	for range 3 {
		get(h, "GET", path, "If-None-Match", r.Header().Get("ETag"))
	}
	missing := "/images/card/" + m.versions["card"] + "/640/c/" + strings.Repeat("d", 64) + "/a.png"
	get(h, "GET", missing)
	get(h, "GET", missing)
	if f.lookups != 2 {
		t.Fatalf("lookups: %d", f.lookups)
	}
	clk.Advance(10 * time.Minute)
	get(h, "GET", path)
	if f.lookups != 3 {
		t.Fatalf("lookups after the TTL: %d", f.lookups)
	}
}

func TestImgproxyUsesCanonicalName(t *testing.T) {
	cfg := Config{Driver: "imgproxy", Presets: map[string]PresetConfig{"card": {Width: 640, Height: 360, Fit: "cover"}},
		Imgproxy: ImgproxyConfig{URL: "https://img.example.com", Key: "0a0b0c", Salt: "0d0e0f", Source: "https://cms.example.com/assets/{sha}/{name}"}}
	m, _ := testModule(t, cfg)
	h := mux(m)
	var locs []string
	for _, name := range []string{"a.png", "evil%20name.png", "x.jpg"} {
		r := get(h, "GET", "/images/card/"+m.versions["card"]+"/640/c/"+sha1hex+"/"+name)
		if r.Code != 302 {
			t.Fatalf("%s: %d", name, r.Code)
		}
		locs = append(locs, r.Header().Get("Location"))
	}
	if locs[0] != locs[1] || locs[1] != locs[2] || !strings.Contains(locs[0], "image.png") {
		t.Fatalf("locations differ: %v", locs)
	}
	a := map[string]any{"mime": "image/png", "name": "My Cat.png", "width": json.Number("1200"), "height": json.Number("800")}
	m.DecorateAsset(nil, map[string]any{"id": sha1hex}, a)
	if u := a["variants"].(map[string]any)["card"].(map[string]any)["url"].(string); strings.Contains(u, "Cat") || !strings.Contains(u, "image.png") {
		t.Fatalf("delivery url: %s", u)
	}
}
