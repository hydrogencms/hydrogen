package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
	"github.com/hydrogencms/hydrogen/store"
)

const assetChannels = `
languages: { en: {}, de: {}, fr: {} }
channels:
  web:      { languages: [en, de] }
  articles: { languages: [en], collections: [article] }
`

// assetNode is a delivery node on the asset schema with real media storage in a temporary directory.
func assetNode(t testing.TB, db store.DB, clk *clock.Manual, extraYAML string) *dnode {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": assetSchema, "config/channels.yaml": assetChannels})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\nmedia: { storage: { dir: " + filepath.Join(dir, "media") + " } }\n"
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app+extraYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	cdb := &delivDB{DB: db}
	mods := []kernel.Module{sqlite.Module(), postgres.Module()}
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

func runAssetNodes(t *testing.T, fn func(t *testing.T, n *dnode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, assetNode(t, b.open(t, clk), clk, ""))
	})
}

// pngBytes is a valid w×h PNG; shade makes different files.
func pngBytes(t testing.TB, w, h int, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{shade, uint8(x), uint8(y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// upload sends a file to the management API and returns the file value it answers with.
func (n *dnode) upload(name string, data []byte) map[string]any {
	n.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		n.t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/manage/v1/uploads", &body)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Authorization", "Bearer "+kernel.DevManageToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	n.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		n.t.Fatalf("upload %s: %d %s", name, rec.Code, rec.Body)
	}
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		n.t.Fatal(err)
	}
	return v
}

// image uploads a PNG and creates (and optionally publishes) an image item for it.
func (n *dnode) image(name string, shade uint8, fields string, publish bool) (ids.UUID, map[string]any) {
	n.t.Helper()
	file := n.upload(name, pngBytes(n.t, 8, 6, shade))
	fb, _ := json.Marshal(file)
	if fields == "" {
		fields = "{}"
	}
	f := doc(n.t, fields)
	f["file"] = doc(n.t, string(fb))
	n.tick()
	v, err := n.app.Services().Content.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: f})
	if err != nil {
		n.t.Fatal(err)
	}
	if publish {
		n.publish(v.ID)
	}
	return v.ID, file
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func TestAssetDeliveryExpansion(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		img, file := n.image("cat.png", 1, `{"alt":"A cat","caption":"Sleeping","folder":"/pets","tags":["cat"]}`, false)
		n.update(img, "de", `{"alt":"Eine Katze"}`)
		n.publish(img)
		draft, _ := n.image("draft.png", 2, `{"alt":"Never published"}`, false)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q,"gallery":[%q,%q]}`, img, img, draft), false)
		n.update(art, "de", `{"title":"Ä"}`)
		n.publish(art)

		r := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		hero := asMap(field(r.json(), "item", "fields", "hero"))
		deq(t, "id", hero["id"], img.String())
		deq(t, "url", hero["url"], "/assets/"+file["id"].(string)+"/cat.png")
		deq(t, "name", hero["name"], "cat.png")
		deq(t, "mime", hero["mime"], "image/png")
		deq(t, "width", fmt.Sprint(hero["width"]), "8")
		deq(t, "height", fmt.Sprint(hero["height"]), "6")
		deq(t, "size", fmt.Sprint(hero["size"]), fmt.Sprint(file["size"]))
		fields := asMap(hero["fields"])
		deq(t, "alt", fields["alt"], "A cat")
		deq(t, "caption", fields["caption"], "Sleeping")
		for _, k := range []string{"file", "folder", "tags"} {
			if _, ok := fields[k]; ok {
				t.Errorf("built-in field %s leaked into the asset fields: %v", k, fields)
			}
		}
		gallery, _ := field(r.json(), "item", "fields", "gallery").([]any)
		if len(gallery) != 2 || asMap(gallery[0])["id"] != img.String() || gallery[1] != nil {
			t.Fatalf("gallery = %v, want the asset and null for the unpublished one", gallery)
		}

		// The asset comes in the locale of the page; a missing translation falls back like any item.
		r = n.wantStatus("/items/"+art.String()+"?channel=web&locale=de", 200)
		deq(t, "de alt", field(r.json(), "item", "fields", "hero", "fields", "alt"), "Eine Katze")
		deq(t, "page locale", field(r.json(), "item", "locale"), "de")

		// Unset fields stay unset; the other fields of the item are untouched.
		deq(t, "title", field(r.json(), "item", "fields", "title"), "Ä")
		if _, ok := asMap(field(r.json(), "item", "fields"))["cover"]; ok {
			t.Error("an unset asset field appeared")
		}

		// A listing expands too, and the asset item itself is delivered with its raw file value.
		l := n.wantStatus("/collections/article/items?channel=web", 200)
		items, _ := l.json()["items"].([]any)
		deq(t, "listed hero", field(asMap(items[0]), "fields", "hero", "name"), "cat.png")
		ri := n.wantStatus("/items/"+img.String()+"?channel=web", 200)
		deq(t, "asset item file id", field(ri.json(), "item", "fields", "file", "id"), file["id"])
	})
}

func TestAssetDeliveryTagsAndPurge(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		img, _ := n.image("cat.png", 1, `{"alt":"Before"}`, true)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)
		path := "/items/" + art.String() + "?channel=web"

		r := n.wantStatus(path, 200)
		for _, hdr := range []string{"Surrogate-Key", "Cache-Tag"} {
			if !strings.Contains(r.Header().Get(hdr), cachetag.Item(img.String())) {
				t.Errorf("%s = %q lacks the asset's tag", hdr, r.Header().Get(hdr))
			}
		}
		deq(t, "alt", field(r.json(), "item", "fields", "hero", "fields", "alt"), "Before")
		n.wantStatus(path, 200) // cached now
		before := n.db.dbCalls()
		n.wantStatus(path, 200)
		if n.db.dbCalls() != before {
			t.Error("the second response was not served from the cache")
		}

		// Republishing the asset purges the page that shows it.
		n.update(img, "en", `{"alt":"After"}`)
		n.publish(img)
		n.drain()
		eventually(t, "the republished asset in the page", func() bool {
			return field(n.wantStatus(path, 200).json(), "item", "fields", "hero", "fields", "alt") == "After"
		})

		// Unpublishing it turns the field to null.
		n.tick()
		if _, err := n.app.Services().Content.Unpublish(ctx, img); err != nil {
			t.Fatal(err)
		}
		n.drain()
		eventually(t, "the unpublished asset to disappear", func() bool {
			m := asMap(field(n.wantStatus(path, 200).json(), "item", "fields"))
			v, present := m["hero"]
			return present && v == nil
		})
		// Publishing it again brings it back.
		n.publish(img)
		n.drain()
		eventually(t, "the asset to return", func() bool {
			return field(n.wantStatus(path, 200).json(), "item", "fields", "hero", "fields", "alt") == "After"
		})
	})
}

func TestAssetDeliveryChannelWithoutUploadCollection(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		img, _ := n.image("cat.png", 1, `{"alt":"A cat"}`, true)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)
		// The articles channel does not show the image collection, so it does not show its assets either.
		r := n.wantStatus("/items/"+art.String()+"?channel=articles", 200)
		m := asMap(field(r.json(), "item", "fields"))
		if v, present := m["hero"]; !present || v != nil {
			t.Fatalf("hero = %v, want null", m["hero"])
		}
	})
}

func TestAssetDeliveryDraftPreview(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		img, _ := n.image("cat.png", 1, `{"alt":"Published"}`, true)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)
		n.update(img, "en", `{"alt":"Draft alt"}`)

		pub := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		deq(t, "published alt", field(pub.json(), "item", "fields", "hero", "fields", "alt"), "Published")

		tok := n.manage("POST", "/preview-tokens", map[string]any{"channel": "web", "item": art.String()})
		if tok.Code != 200 && tok.Code != 201 {
			t.Fatalf("preview token: %d %s", tok.Code, tok.Body)
		}
		token, _ := tok.json()["token"].(string)
		pre := n.wantStatus("/items/"+art.String()+"?channel=web&preview="+token, 200)
		deq(t, "draft alt in preview", field(pre.json(), "item", "fields", "hero", "fields", "alt"), "Draft alt")
	})
}

func TestAssetDeliveryOpenAPI(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		r := n.wantStatus("/openapi.json", 200)
		schemas := asMap(field(r.json(), "components", "schemas"))
		if asMap(schemas["Asset"]) == nil {
			t.Fatal("no Asset schema")
		}
		raw := r.Body.String()
		if strings.Contains(raw, "x-hydrogen-asset") {
			t.Error("the marker leaked into the document")
		}
		hero := asMap(field(asMap(schemas["Fields_article"]), "properties", "hero"))
		one, _ := hero["oneOf"].([]any)
		if len(one) != 2 || asMap(one[0])["$ref"] != "#/components/schemas/Asset" {
			t.Fatalf("hero schema = %v", hero)
		}
	})
}
