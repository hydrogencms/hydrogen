package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const appToken = "app-token-0123456789"

const testSchema = `
groups:
  seo:
    fields:
      metaTitle:       { type: string }
      metaDescription: { type: text }
blocks:
  hero:
    props:
      heading: { type: string, required: true }
types:
  image:
    upload: { accept: [image/*] }
    fields:
      alt:     { type: string, translatable: true }
      caption: { type: text, translatable: true }
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true, index: true }
      bio:     { type: richtext }
      company: { type: ref, to: [company] }
  company:
    name: "{title}"
    fields:
      title: { type: string, required: true }
  settings:
    singleton: true
    fields:
      siteName: { type: string, translatable: true, required: true }
  article:
    name: "{title}"
    fields:
      title:       { type: string, translatable: true, required: true, index: true }
      slug:        { type: slug, from: title, translatable: true, index: true }
      summary:     { type: text, translatable: true }
      body:        { type: richtext, translatable: true }
      kind:        { type: enum, values: [news, video], default: news, index: true }
      stage:       { type: enum, values: [in-progress, done] }
      url:         { type: url }
      contact:     { type: email }
      publishDate: { type: datetime, index: true }
      eventDay:    { type: date }
      rating:      { type: number, integer: true, index: true }
      price:       { type: number }
      featured:    { type: boolean, index: true }
      hero:        { type: asset, to: [image] }
      gallery:     { type: list, of: { type: asset, to: [image] } }
      attachment:  { type: file }
      author:      { type: ref, to: [person], index: true }
      related:     { type: list, of: { type: ref, to: [article] } }
      subject:     { type: ref, to: [person, article, company] }
      logo:        { type: ref, to: [image] }
      tags:        { type: list, of: { type: string }, index: true }
      address:     { type: group, fields: { street: { type: string }, city: { type: string }, geo: { type: group, fields: { lat: { type: number }, lng: { type: number } } } } }
      seo:         { type: group, use: seo }
      places:      { type: list, of: { type: group, fields: { label: { type: string }, rank: { type: number, integer: true } } } }
      extra:       { type: json }
      sections:    { type: blocks, allow: [hero] }
`

const testChannels = `
languages: { en: {}, de: {} }
channels:
  web: { languages: [en, de] }
  lim: { languages: [en], collections: [article, person, company] }
  app: { languages: [en], access: token, tokens: { v1: "${env:APP_TOKEN}" } }
`

type env struct {
	t   *testing.T
	app *kernel.App
	h   http.Handler
	mod *module
	clk *clock.Manual
	// calls is the number of internal delivery calls of the last GraphQL request.
	calls int
}

type envOpts struct {
	schema   string
	channels string
	app      string // extra app.yaml
	mods     []kernel.Module
}

func newEnv(t *testing.T, o envOpts) *env {
	t.Helper()
	if o.schema == "" {
		o.schema = testSchema
	}
	if o.channels == "" {
		o.channels = testChannels
	}
	root := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/s.yaml", o.schema)
	write("config/channels.yaml", o.channels)
	write("config/app.yaml", "content: { schema_dir: "+filepath.Join(root, "schema")+" }\n"+
		"database: { url: \"${env:DBURL}\" }\n"+
		"media: { storage: { dir: "+filepath.Join(root, "media")+" } }\n"+o.app)
	cfg, err := config.Load(config.Options{Dir: filepath.Join(root, "config"), Env: "dev", LookupEnv: func(k string) (string, bool) {
		switch k {
		case "DBURL":
			return "sqlite:" + filepath.Join(root, "test.db"), true
		case "APP_TOKEN":
			return appToken, true
		}
		return "", false
	}})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, mod: Module().(*module), clk: clock.NewManual(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))}
	e.mod.observe = func(n int) { e.calls = n }
	mods := append([]kernel.Module{sqlite.Module()}, o.mods...)
	mods = append(mods, e.mod)
	e.app, err = kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: e.clk, Stderr: io.Discard, PasswordIterations: 1000}, mods...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.app.Close() })
	ctx := context.Background()
	if err := e.app.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.app.Services().Content.EnsureSchemaRev(ctx); err != nil {
		t.Fatal(err)
	}
	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.app.Services().Delivery.Start(lctx); err != nil {
		t.Fatal(err)
	}
	if e.h, err = e.app.Handler(); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) tick() { e.clk.Advance(time.Second) }

func (e *env) create(typ, key, locale, fields string, publish bool) ids.UUID {
	e.t.Helper()
	e.tick()
	var f map[string]any
	d := json.NewDecoder(strings.NewReader(fields))
	d.UseNumber()
	if err := d.Decode(&f); err != nil {
		e.t.Fatal(err)
	}
	v, err := e.app.Services().Content.Create(context.Background(), content.CreateInput{Type: typ, Key: key, Locale: locale, Fields: f})
	if err != nil {
		e.t.Fatal(err)
	}
	if publish {
		e.publish(v.ID)
	}
	return v.ID
}

func (e *env) update(id ids.UUID, locale, fields string) {
	e.t.Helper()
	e.tick()
	var f map[string]any
	d := json.NewDecoder(strings.NewReader(fields))
	d.UseNumber()
	if err := d.Decode(&f); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.app.Services().Content.Update(context.Background(), id, content.UpdateInput{Locale: locale, Fields: f}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) publish(id ids.UUID) {
	e.t.Helper()
	e.tick()
	if _, err := e.app.Services().Content.Publish(context.Background(), id, content.PublishOptions{}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) drain() {
	e.t.Helper()
	if _, err := e.app.Services().Outbox.Drain(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

type result struct {
	*httptest.ResponseRecorder
}

func (r result) json() map[string]any {
	r.Body.Len()
	var m map[string]any
	if err := json.Unmarshal(r.Body.Bytes(), &m); err != nil {
		panic("not JSON: " + r.Body.String())
	}
	return m
}

// data returns the data object of a GraphQL response.
func (r result) data() map[string]any {
	d, _ := r.json()["data"].(map[string]any)
	return d
}

func (r result) errors() []any {
	l, _ := r.json()["errors"].([]any)
	return l
}

func (e *env) do(method, path string, hdr map[string]string, body string) result {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return result{rec}
}

const base = "/api/deliver/v1/graphql"

// post runs a query on a channel as a POST with a JSON body.
func (e *env) post(channel, query string, vars map[string]any, extra ...string) result {
	e.t.Helper()
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	path := base + "?channel=" + channel
	for _, x := range extra {
		path += "&" + x
	}
	return e.do(http.MethodPost, path, map[string]string{"Content-Type": "application/json"}, string(body))
}

func (e *env) get(channel, query string, extra ...string) result {
	e.t.Helper()
	path := base + "?channel=" + channel + "&query=" + urlEscape(query)
	for _, x := range extra {
		path += "&" + x
	}
	return e.do(http.MethodGet, path, nil, "")
}

func (e *env) manage(method, path string, body any) result {
	e.t.Helper()
	b, _ := json.Marshal(body)
	return e.do(method, "/api/manage/v1"+path, map[string]string{"Authorization": "Bearer " + kernel.DevManageToken, "Content-Type": "application/json"}, string(b))
}

// image uploads a PNG and creates and publishes an image item for it.
func (e *env) image(name string, shade uint8, fields string) ids.UUID {
	e.t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for x := range 8 {
		for y := range 6 {
			img.Set(x, y, color.RGBA{shade, uint8(x), uint8(y), 255})
		}
	}
	var png_ bytes.Buffer
	if err := png.Encode(&png_, img); err != nil {
		e.t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(png_.Bytes())
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/manage/v1/uploads", &body)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("Authorization", "Bearer "+kernel.DevManageToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		e.t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	var file map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &file)
	f := map[string]any{}
	_ = json.Unmarshal([]byte(fields), &f)
	f["file"] = file
	e.tick()
	v, err := e.app.Services().Content.Create(context.Background(), content.CreateInput{Type: "image", Locale: "en", Fields: f})
	if err != nil {
		e.t.Fatal(err)
	}
	e.publish(v.ID)
	return v.ID
}

// path reads a nested value of decoded JSON.
func path(v any, keys ...any) any {
	for _, k := range keys {
		switch x := k.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[x]
		case int:
			l, _ := v.([]any)
			if x >= len(l) {
				return nil
			}
			v = l[x]
		}
	}
	return v
}

func urlEscape(s string) string { return url.QueryEscape(s) }
