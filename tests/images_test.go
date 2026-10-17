package tests

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/images"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const imagesChannels = `
languages: { en: {} }
channels:
  web:    { languages: [en] }
  narrow: { languages: [en], image_presets: [hero] }
`

const imagesApp = `
modules:
  images:
    presets:
      card: { width: 640, height: 360, fit: cover, widths: [320] }
      hero: { width: 1600, format: jpeg }
`

func imagesNode(t testing.TB, db store.DB, clk *clock.Manual, extra string, mods ...kernel.Module) *dnode {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": assetSchema, "config/channels.yaml": imagesChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\nmedia: { storage: { dir: " + filepath.Join(dir, "media") + " } }\n" + imagesApp + extra
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	cdb := &delivDB{DB: db}
	mods = append([]kernel.Module{sqlite.Module(), postgres.Module()}, mods...)
	a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: cdb, Stderr: io.Discard, PasswordIterations: 1000}, mods...)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Services().Content.EnsureSchemaRev(ctx); err != nil {
		t.Fatal(err)
	}
	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := a.Services().Delivery.Start(lctx); err != nil {
		t.Fatal(err)
	}
	h, err := a.Handler()
	if err != nil {
		t.Fatal(err)
	}
	return &dnode{t: t, app: a, db: cdb, h: h, clk: clk}
}

func (n *dnode) raw(method, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:50000"
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	n.h.ServeHTTP(rec, req)
	return rec
}

func runImagesNodes(t *testing.T, fn func(t *testing.T, n *dnode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, imagesNode(t, b.open(t, clk), clk, "", images.Module()))
	})
}

// bigImage uploads a w×h PNG as an image item with a focal point and returns the item and its file value.
func (n *dnode) bigImage(name string, w, h int, shade uint8) (ids.UUID, map[string]any) {
	n.t.Helper()
	return n.imageWith(name, pngBytes(n.t, w, h, shade), 0.25, 0.5)
}

func (n *dnode) imageWith(name string, data []byte, fx, fy float64) (ids.UUID, map[string]any) {
	n.t.Helper()
	file := n.upload(name, data)
	file["focal"] = map[string]any{"x": fx, "y": fy}
	f := map[string]any{"alt": "A cat", "file": file}
	n.tick()
	v, err := n.app.Services().Content.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: f})
	if err != nil {
		n.t.Fatal(err)
	}
	n.publish(v.ID)
	return v.ID, file
}

func pngSize(t *testing.T, b []byte) (int, int) {
	t.Helper()
	if len(b) < 24 || string(b[1:4]) != "PNG" {
		t.Fatalf("not a PNG: %d bytes", len(b))
	}
	return int(binary.BigEndian.Uint32(b[16:])), int(binary.BigEndian.Uint32(b[20:]))
}

func TestImagesVariantsAndServing(t *testing.T) {
	runImagesNodes(t, func(t *testing.T, n *dnode) {
		img, file := n.bigImage("cat.png", 1200, 800, 3)
		sha := file["id"].(string)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)

		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		variants := asMap(field(r.json(), "item", "fields", "hero", "variants"))
		if len(variants) != 2 {
			t.Fatalf("variants: %v", variants)
		}
		card := asMap(variants["card"])
		url, _ := card["url"].(string)
		parts := strings.Split(url, "/")
		if len(parts) != 8 || parts[1] != "images" || parts[2] != "card" || parts[4] != "640" || parts[5] != "2550" || parts[6] != sha || parts[7] != "cat.png" {
			t.Fatalf("card url: %s", url)
		}
		v := parts[3]
		deq(t, "width", fmt.Sprint(card["width"]), "640")
		deq(t, "height", fmt.Sprint(card["height"]), "360")
		deq(t, "type", card["type"], "image/png")
		deq(t, "srcset", card["srcset"], fmt.Sprintf("/images/card/%s/320/2550/%s/cat.png 320w, %s 640w", v, sha, url))
		hero := asMap(variants["hero"])
		deq(t, "hero type", hero["type"], "image/jpeg")
		if !strings.HasSuffix(hero["url"].(string), "/cat.jpg") || hero["srcset"] != nil {
			t.Fatalf("hero: %v", hero)
		}

		// The channel option narrows the list; the asset item itself has no variants block change.
		r = n.wantStatus("/items/"+art.String()+"?channel=narrow", 200)
		if got := asMap(field(r.json(), "item", "fields", "hero", "variants")); len(got) != 1 || got["hero"] == nil {
			t.Fatalf("narrow variants: %v", got)
		}

		g := n.raw("GET", url)
		if g.Code != 200 || g.Header().Get("Hydrogen-Image") != "generated" || g.Header().Get("Content-Type") != "image/png" {
			t.Fatalf("first GET: %d %v", g.Code, g.Header())
		}
		if w, h := pngSize(t, g.Body.Bytes()); w != 640 || h != 360 {
			t.Fatalf("variant size %dx%d", w, h)
		}
		g2 := n.raw("GET", url)
		if g2.Code != 200 || g2.Header().Get("Hydrogen-Image") != "stored" || !bytes.Equal(g2.Body.Bytes(), g.Body.Bytes()) {
			t.Fatalf("second GET: %d %v", g2.Code, g2.Header())
		}
		if c := n.raw("GET", url, "If-None-Match", g.Header().Get("ETag")); c.Code != 304 {
			t.Fatalf("conditional: %d", c.Code)
		}
		if h := n.raw("HEAD", url); h.Code != 200 || h.Body.Len() != 0 {
			t.Fatalf("HEAD: %d", h.Code)
		}
		if s := n.raw("GET", fmt.Sprintf("/images/card/%s/641/2550/%s/cat.png", v, sha)); s.Code != 404 {
			t.Fatalf("unknown width: %d", s.Code)
		}
		stale := n.raw("GET", fmt.Sprintf("/images/card/00000000/640/2550/%s/cat.png", sha))
		if stale.Code != 302 || stale.Header().Get("Location") != url {
			t.Fatalf("stale v: %d %q", stale.Code, stale.Header().Get("Location"))
		}
		if s := n.raw("GET", "/images/card/"+v+"/640/c/"+strings.Repeat("e", 64)+"/cat.png"); s.Code != 404 {
			t.Fatalf("unknown sha: %d", s.Code)
		}
		if s := n.raw("GET", fmt.Sprintf("/images/card/%s/640/7777/%s/cat.png", v, sha)); s.Code != 404 {
			t.Fatalf("made-up focal: %d", s.Code)
		}
		if s := n.raw("GET", fmt.Sprintf("/images/card/%s/640/c/%s/cat.png", v, sha)); s.Code != 200 {
			t.Fatalf("centre: %d", s.Code)
		}
	})
}

func TestImagesSmallSourceNotUpscaled(t *testing.T) {
	runImagesNodes(t, func(t *testing.T, n *dnode) {
		img, _ := n.bigImage("cat.png", 100, 80, 4)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)
		// A 100×80 source is never upscaled: the card is smaller than its preset and has no srcset.
		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		card := asMap(asMap(field(r.json(), "item", "fields", "hero", "variants"))["card"])
		deq(t, "width", fmt.Sprint(card["width"]), "100")
		deq(t, "height", fmt.Sprint(card["height"]), "56")
		if card["srcset"] != nil {
			t.Fatalf("srcset: %v", card["srcset"])
		}
	})
}

func TestImagesAdminConfigAndCSP(t *testing.T) {
	runImagesNodes(t, func(t *testing.T, n *dnode) {
		cfg := n.raw("GET", "/admin/config.json")
		var c struct {
			Media struct {
				ThumbBase string `json:"thumbBase"`
			} `json:"media"`
		}
		if err := json.Unmarshal(cfg.Body.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(c.Media.ThumbBase, "/images/admin/") || !strings.HasSuffix(c.Media.ThumbBase, "/480/c") {
			t.Fatalf("thumbBase: %q", c.Media.ThumbBase)
		}
		_, file := n.bigImage("cat.png", 900, 600, 5)
		g := n.raw("GET", c.Media.ThumbBase+"/"+file["id"].(string)+"/cat.png")
		if g.Code != 200 {
			t.Fatalf("thumbnail: %d", g.Code)
		}
		if w, h := pngSize(t, g.Body.Bytes()); w != 480 || h != 320 {
			t.Fatalf("thumbnail size %dx%d", w, h)
		}
	})
}

func TestImagesAdminCSPOrigins(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := imagesNode(t, b.open(t, clk), clk, "    public_url: https://cdn.example.com/img\n", images.Module())
		csp := n.raw("GET", "/admin/").Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "img-src 'self' data: blob: https://cdn.example.com;") || !strings.Contains(csp, "media-src 'self' https://cdn.example.com") {
			t.Fatalf("CSP: %s", csp)
		}
		if cfg := n.raw("GET", "/admin/config.json").Body.String(); !strings.Contains(cfg, `"thumbBase":"https://cdn.example.com/img/admin/`) {
			t.Fatalf("config.json: %s", cfg)
		}
	})
}

func TestImagesInertWithoutModule(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := assetNode(t, b.open(t, clk), clk, "")
		img, file := n.image("cat.png", 1, "", true)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)
		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		if _, ok := asMap(field(r.json(), "item", "fields", "hero"))["variants"]; ok {
			t.Fatal("variants without the module")
		}
		if g := n.raw("GET", "/images/card/00000000/640/"+file["id"].(string)+"/a.png"); g.Code != 404 {
			t.Fatalf("/images without the module: %d", g.Code)
		}
		if c := n.raw("GET", "/admin/config.json"); strings.Contains(c.Body.String(), "thumbBase") {
			t.Fatalf("thumbBase without the module: %s", c.Body)
		}
	})
}

func TestImagesGCJob(t *testing.T) {
	runImagesNodes(t, func(t *testing.T, n *dnode) {
		img, file := n.bigImage("cat.png", 400, 300, 6)
		sha := file["id"].(string)
		m := n.app.Services().Media
		put := func(suffix string) {
			if err := m.PutDerived(ctx, sha, suffix, strings.NewReader("x"), 1, "image/png"); err != nil {
				t.Fatal(err)
			}
		}
		for _, s := range []string{"card-deadbeef-640", "gone-0123abcd-640", "card-deadbeef-641"} {
			put(s)
		}
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)
		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		url := asMap(asMap(field(r.json(), "item", "fields", "hero", "variants"))["card"])["url"].(string)
		if g := n.raw("GET", url); g.Code != 200 {
			t.Fatalf("variant: %d", g.Code)
		}
		if err := n.app.RunJob(ctx, images.JobName); err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"card-deadbeef-640", "gone-0123abcd-640", "card-deadbeef-641"} {
			if rc, _, err := m.OpenDerived(ctx, sha, s); err == nil {
				_ = rc.Close()
				t.Errorf("%s survived", s)
			}
		}
		if g := n.raw("GET", url); g.Header().Get("Hydrogen-Image") != "stored" {
			t.Fatalf("the current variant was removed: %v", g.Header())
		}
	})
}

func TestImagesFocalCrop(t *testing.T) {
	runImagesNodes(t, func(t *testing.T, n *dnode) {
		// red above y=380, blue below: the centre crop of a cover card shows blue at its middle
		img := image.NewRGBA(image.Rect(0, 0, 1200, 800))
		for x := range 1200 {
			for y := range 800 {
				c := color.RGBA{0, 0, 255, 255}
				if y < 380 {
					c = color.RGBA{255, 0, 0, 255}
				}
				img.Set(x, y, c)
			}
		}
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			t.Fatal(err)
		}
		item, _ := n.imageWith("split.png", b.Bytes(), 0.5, 0.01)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, item), true)
		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		url := asMap(asMap(field(r.json(), "item", "fields", "hero", "variants"))["card"])["url"].(string)
		if !strings.Contains(url, "/640/5001/") {
			t.Fatalf("url: %s", url)
		}
		middle := func(path string) (red bool) {
			g := n.raw("GET", path)
			if g.Code != 200 {
				t.Fatalf("GET %s: %d", path, g.Code)
			}
			im, err := png.Decode(bytes.NewReader(g.Body.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			rr, _, _, _ := im.At(320, 180).RGBA()
			return rr>>8 > 200
		}
		if !middle(url) {
			t.Error("the focal crop did not show the top part")
		}
		if middle(strings.Replace(url, "/5001/", "/c/", 1)) {
			t.Error("the centre crop showed the top part")
		}
		if g := n.raw("GET", strings.Replace(url, "/5001/", "/5002/", 1)); g.Code != 404 {
			t.Errorf("made-up focal: %d", g.Code)
		}
	})
}
