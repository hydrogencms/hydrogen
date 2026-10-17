package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/features/media"
)

const mediaSchema = `
types:
  image:
    upload: { accept: [image/*, application/pdf], max_size: 1MB }
    fields:
      alt: { type: string, translatable: true }
  manual:
    upload: { accept: [application/pdf] }
    fields: {}
  article:
    name: "{title}"
    fields:
      title:      { type: string, required: true }
      attachment: { type: file }
`

// mtGate allows everything except the types in deny.
type mtGate struct{ deny map[string]bool }

func (g mtGate) Screen(_ context.Context, policy string, t access.Target) error {
	if g.deny[t.Type] {
		return fmt.Errorf("%w: %s", access.ErrForbidden, policy)
	}
	return nil
}

// mtRegistry is the kernel's adapter between the media service and the content service's file registry.
type mtRegistry struct{ m *media.Service }

func (r mtRegistry) Lookup(ctx context.Context, sha string) (content.FileInfo, error) {
	f, err := r.m.Lookup(ctx, sha)
	if errors.Is(err, media.ErrNotFound) {
		return content.FileInfo{}, content.ErrFileNotFound
	}
	return content.FileInfo{ID: f.SHA, Mime: f.MIME, Size: f.Size, Width: f.Width, Height: f.Height}, err
}

func (r mtRegistry) Touch(ctx context.Context, sha string) error {
	err := r.m.Touch(ctx, sha)
	if errors.Is(err, media.ErrNotFound) {
		return content.ErrFileNotFound
	}
	return err
}

type mediaEnv struct {
	*env
	m    *media.Service
	dir  string
	gate mtGate
	web  http.Handler
}

func newMediaEnv(t testing.TB, b backend, opts ...func(*media.Options)) *mediaEnv {
	t.Helper()
	clk := newClock()
	e := newEnv(t, b.open(t, clk), clk, envConfig{schema: mediaSchema})
	me := &mediaEnv{env: e, dir: filepath.Join(t.TempDir(), "media"), gate: mtGate{deny: map[string]bool{}}}
	st, err := media.NewLocal(me.dir)
	if err != nil {
		t.Fatal(err)
	}
	o := media.Options{DB: e.db, Storage: st, Clock: clk, Audit: audit.New(clk), Outbox: e.ob, Access: gateFunc(func(ctx context.Context, p string, tg access.Target) error {
		return me.gate.Screen(ctx, p, tg)
	}), TempDir: t.TempDir(),
		Types: func() []media.UploadType {
			return []media.UploadType{{Name: "image", Accept: []string{"image/*", "application/pdf"}, MaxSize: 1 << 20},
				{Name: "manual", Accept: []string{"application/pdf"}}}
		},
		FileTypes: func() []string { return []string{"image", "manual", "article"} },
		MaxUpload: 2 << 20}
	for _, f := range opts {
		f(&o)
	}
	if me.m, err = media.New(o); err != nil {
		t.Fatal(err)
	}
	e.svc.SetFiles(mtRegistry{me.m})

	api, err := manage.New(manage.Options{Content: e.svc, Token: "tok", Routes: me.m.ManageRoutes(), Prefix: "/api/manage/v1"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/api/manage/v1/", http.StripPrefix("/api/manage/v1", api))
	mux.Handle(media.AssetPattern, me.m.Handler())
	me.web = mux
	return me
}

type gateFunc func(context.Context, string, access.Target) error

func (g gateFunc) Screen(ctx context.Context, policy string, t access.Target) error {
	return g(ctx, policy, t)
}

func mtPNG(w, h int, seed byte) []byte {
	img := image.NewGray(image.Rect(0, 0, w, h))
	img.Pix[0] = seed
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

var mtPDF = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")

func (me *mediaEnv) upload(typ, name string, data []byte) (media.Result, error) {
	return me.m.Upload(ctx, media.UploadInput{Name: name, Body: bytes.NewReader(data), Type: typ})
}

func (me *mediaEnv) mustUpload(typ, name string, data []byte) media.Result {
	me.t.Helper()
	r, err := me.upload(typ, name, data)
	if err != nil {
		me.t.Fatalf("upload %s: %v", name, err)
	}
	return r
}

func (me *mediaEnv) stored() []string {
	me.t.Helper()
	var out []string
	_ = filepath.WalkDir(me.dir, func(p string, d os.DirEntry, _ error) error {
		if d != nil && !d.IsDir() && !strings.Contains(p, ".tmp") {
			out = append(out, d.Name())
		}
		return nil
	})
	return out
}

func (me *mediaEnv) records() []store.Upload {
	me.t.Helper()
	recs, err := me.m.Records(ctx, "", 1000)
	if err != nil {
		me.t.Fatal(err)
	}
	return recs
}

func runMedia(t *testing.T, fn func(t *testing.T, me *mediaEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) { fn(t, newMediaEnv(t, b)) })
}

func TestMediaUploadAndDedupe(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		data := mtPNG(30, 20, 1)
		r := me.mustUpload("image", "cat photo.png", data)
		if !r.Created || r.MIME != "image/png" || r.Width != 30 || r.Height != 20 || r.Size != int64(len(data)) || r.Name != "cat photo.png" || !media.IsSha(r.SHA) {
			t.Fatalf("result = %+v", r)
		}
		first := me.records()
		if len(first) != 1 || first[0].CreatedBy != "system" || first[0].SHA != r.SHA {
			t.Fatalf("records = %+v", first)
		}
		me.clk.Advance(time.Hour)
		again := me.mustUpload("", "renamed.png", data)
		if again.Created || again.SHA != r.SHA || again.Name != "renamed.png" {
			t.Fatalf("second upload = %+v", again)
		}
		recs := me.records()
		if len(recs) != 1 || !recs[0].CreatedAt.Equal(first[0].CreatedAt) || !recs[0].TouchedAt.Equal(first[0].TouchedAt.Add(time.Hour)) {
			t.Fatalf("after dedupe: %+v (first %+v)", recs, first)
		}
		if got := me.stored(); len(got) != 1 || got[0] != r.SHA {
			t.Fatalf("stored files = %v", got)
		}
		var actions []string
		_ = me.db.View(ctx, func(tx store.Tx) error {
			evs, err := tx.Audit().List(ctx, store.AuditQuery{Subject: r.SHA})
			for _, e := range evs {
				actions = append(actions, e.Action)
			}
			return err
		})
		if len(actions) != 1 || actions[0] != "media.upload" {
			t.Fatalf("audit actions = %v", actions)
		}

		// A known file whose bytes went missing is stored again.
		if err := os.Remove(filepath.Join(me.dir, r.SHA[:2], r.SHA[2:4], r.SHA)); err != nil {
			t.Fatal(err)
		}
		me.mustUpload("image", "cat.png", data)
		if got := me.stored(); len(got) != 1 {
			t.Fatalf("bytes not restored: %v", got)
		}

		// The content service finds the file through the registry and owns its metadata.
		v := me.create("image", "en", fmt.Sprintf(`{"file":{"id":%q,"name":"cat.png","size":1,"mime":"text/html"}}`, r.SHA))
		f, _ := v.Fields["file"].(map[string]any)
		if f["mime"] != "image/png" || fmt.Sprint(f["width"]) != "30" || f["id"] != r.SHA {
			t.Fatalf("file value = %v", f)
		}
		_, err := me.svc.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: doc(t, fileJSON(shaOf("9"), "x.png"))})
		if err == nil {
			t.Fatal("an item pointing at an unknown file was created")
		}
	})
}

func TestMediaUploadLimitsAndSniffing(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		kind := func(err error) error {
			var ue *media.UploadError
			if errors.As(err, &ue) {
				return ue.Kind
			}
			return err
		}
		big := bytes.Repeat([]byte("x"), 1<<20+1)
		if _, err := me.upload("image", "big.bin", big); kind(err) != media.ErrTooLarge {
			t.Fatalf("over the type's max_size: %v", err)
		}
		huge := bytes.Repeat([]byte("x"), 2<<20+1)
		if _, err := me.upload("article", "huge.bin", huge); kind(err) != media.ErrTooLarge {
			t.Fatalf("over max_upload: %v", err)
		}
		if _, err := me.upload("article", "ok.bin", big); err != nil {
			t.Fatalf("a type without max_size takes up to max_upload: %v", err)
		}
		if _, err := me.upload("image", "empty", nil); kind(err) != media.ErrEmptyFile {
			t.Fatalf("empty: %v", err)
		}
		if _, err := me.upload("image", "notes.txt", []byte("just text")); kind(err) != media.ErrUnsupportedType {
			t.Fatalf("text for an image type: %v", err)
		}
		// The client's name and claimed type are not trusted: this is HTML, whatever it is called.
		if _, err := me.upload("image", "photo.png", []byte("<!DOCTYPE html><html><script>alert(1)</script></html>")); kind(err) != media.ErrUnsupportedType {
			t.Fatalf("html named png: %v", err)
		}
		if _, err := me.upload("image", "broken.png", append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 30)...)); kind(err) != media.ErrUnsupportedType {
			t.Fatalf("broken png: %v", err)
		}
		if _, err := me.upload("manual", "pic.png", mtPNG(2, 2, 0)); kind(err) != media.ErrUnsupportedType {
			t.Fatalf("png for a pdf-only type: %v", err)
		}
		if _, err := me.upload("person", "a.png", mtPNG(2, 2, 0)); kind(err) != media.ErrUnknownType {
			t.Fatalf("a type that holds no files: %v", err)
		}
		pdf := me.mustUpload("manual", "manual.pdf", mtPDF)
		if pdf.MIME != "application/pdf" || pdf.Width != 0 {
			t.Fatalf("pdf = %+v", pdf)
		}
		svg := me.mustUpload("image", "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="5"><script>alert(1)</script></svg>`))
		if svg.MIME != media.SVG || svg.Width != 10 || svg.Height != 5 {
			t.Fatalf("svg = %+v", svg)
		}

		// Access: the gate decides per type; a file for a type nobody may write is refused, an upload with no
		// type needs any one.
		me.gate.deny["image"] = true
		if _, err := me.upload("image", "a.png", mtMustPNG()); !errors.Is(err, access.ErrForbidden) {
			t.Fatalf("denied type: %v", err)
		}
		if _, err := me.upload("", "b.png", mtPNG(3, 3, 0)); err != nil {
			t.Fatalf("any writable type is enough: %v", err)
		}
		for _, n := range []string{"image", "manual", "article"} {
			me.gate.deny[n] = true
		}
		if _, err := me.upload("", "c.png", mtPNG(4, 4, 0)); !errors.Is(err, access.ErrForbidden) {
			t.Fatalf("no writable type: %v", err)
		}
	})
}

func mtMustPNG() []byte { return mtPNG(5, 5, 0) }

func multipartBody(t *testing.T, parts ...[3]string) (string, io.Reader) {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, p := range parts { // {field, filename, content}
		h := textproto.MIMEHeader{}
		disp := `form-data; name="` + p[0] + `"`
		if p[1] != "" {
			disp += `; filename="` + p[1] + `"`
			h.Set("Content-Type", "application/octet-stream")
		}
		h.Set("Content-Disposition", disp)
		pw, err := w.CreatePart(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(pw, p[2])
	}
	_ = w.Close()
	return w.FormDataContentType(), &b
}

func (me *mediaEnv) post(ct string, body io.Reader, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/manage/v1/uploads", body)
	req.Header.Set("Content-Type", ct)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	me.web.ServeHTTP(rec, req)
	return rec
}

func mtErrCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct{ Error struct{ Code string } }
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("error body %q: %v", rec.Body.String(), err)
	}
	return e.Error.Code
}

func TestMediaUploadEndpoint(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		png1 := string(mtPNG(12, 8, 3))
		ct, body := multipartBody(t, [3]string{"type", "", "image"}, [3]string{"file", "../dir/My Pic.png", png1})
		rec := me.post(ct, body, "tok")
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		var v map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &v)
		sha, _ := v["id"].(string)
		if !media.IsSha(sha) || v["name"] != "My Pic.png" || v["mime"] != "image/png" || v["width"] != float64(12) || v["height"] != float64(8) || v["size"] != float64(len(png1)) {
			t.Fatalf("file value = %v", v)
		}
		if loc := rec.Header().Get("Location"); loc != "/assets/"+sha+"/My%20Pic.png" {
			t.Fatalf("Location = %q", loc)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("Cache-Control = %q", rec.Header().Get("Cache-Control"))
		}

		// The same bytes: 200, and the file value feeds straight into an item.
		ct, body = multipartBody(t, [3]string{"file", "other.png", png1})
		rec = me.post(ct, body, "tok")
		if rec.Code != http.StatusOK {
			t.Fatalf("second upload: %d %s", rec.Code, rec.Body)
		}
		item := me.create("image", "en", `{"file":`+rec.Body.String()+`}`)
		if f, _ := item.Fields["file"].(map[string]any); f["id"] != sha || f["name"] != "other.png" {
			t.Fatalf("item file = %v", item.Fields["file"])
		}

		for name, c := range map[string]struct {
			parts [][3]string
			token string
			want  int
			code  string
		}{
			"no credentials":     {[][3]string{{"file", "a.png", png1}}, "", 401, "unauthorized"},
			"missing file":       {[][3]string{{"type", "", "image"}}, "tok", 400, "bad_request"},
			"two files":          {[][3]string{{"file", "a.png", png1}, {"file", "b.png", png1}}, "tok", 400, "bad_request"},
			"type after file":    {[][3]string{{"file", "a.png", png1}, {"type", "", "image"}}, "tok", 400, "bad_request"},
			"unknown type":       {[][3]string{{"type", "", "nope"}, {"file", "a.png", png1}}, "tok", 400, "unknown_type"},
			"not accepted":       {[][3]string{{"type", "", "manual"}, {"file", "a.png", png1}}, "tok", 415, "unsupported_media_type"},
			"over the type max":  {[][3]string{{"type", "", "image"}, {"file", "a.bin", strings.Repeat("x", 1<<20+1)}}, "tok", 413, "too_large"},
			"over the global":    {[][3]string{{"type", "", "article"}, {"file", "a.bin", strings.Repeat("x", 3<<20)}}, "tok", 413, "too_large"},
			"empty file":         {[][3]string{{"file", "a.png", ""}}, "tok", 400, "bad_request"},
			"text for the image": {[][3]string{{"type", "", "image"}, {"file", "a.txt", "hello"}}, "tok", 415, "unsupported_media_type"},
		} {
			ct, body := multipartBody(t, c.parts...)
			rec := me.post(ct, body, c.token)
			if rec.Code != c.want || mtErrCode(t, rec) != c.code {
				t.Errorf("%s: %d %s, want %d %s", name, rec.Code, rec.Body, c.want, c.code)
			}
		}
		if rec := me.post("application/json", strings.NewReader("{}"), "tok"); rec.Code != 400 {
			t.Errorf("json body: %d %s", rec.Code, rec.Body)
		}

		me.gate.deny["image"] = true
		ct, body = multipartBody(t, [3]string{"type", "", "image"}, [3]string{"file", "a.png", png1})
		if rec := me.post(ct, body, "tok"); rec.Code != 403 {
			t.Errorf("forbidden: %d %s", rec.Code, rec.Body)
		}
	})
}

func (me *mediaEnv) get(path string, hdr ...string) *httptest.ResponseRecorder {
	return me.do(http.MethodGet, path, hdr...)
}

func (me *mediaEnv) do(method, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	me.web.ServeHTTP(rec, req)
	return rec
}

func TestMediaServing(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		data := mtPNG(20, 10, 2)
		r := me.mustUpload("image", "cat.png", data)
		path := "/assets/" + r.SHA + "/cat.png"

		rec := me.get(path)
		h := rec.Header()
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), data) {
			t.Fatalf("status %d, %d bytes", rec.Code, rec.Body.Len())
		}
		for k, v := range map[string]string{
			"Content-Type": "image/png", "Cache-Control": "public, max-age=31536000, immutable", "ETag": `"` + r.SHA + `"`,
			"X-Content-Type-Options": "nosniff", "Content-Disposition": `inline; filename=cat.png`, "Accept-Ranges": "bytes",
			"Content-Length": fmt.Sprint(len(data)),
		} {
			if h.Get(k) != v {
				t.Errorf("%s = %q, want %q", k, h.Get(k), v)
			}
		}
		if h.Get("Content-Security-Policy") != "" {
			t.Errorf("a png carries a CSP: %q", h.Get("Content-Security-Policy"))
		}
		if h.Get("Last-Modified") == "" {
			t.Error("no Last-Modified")
		}

		// The name is only the download name: any name serves the same file.
		if rec := me.get("/assets/" + r.SHA + "/whatever.jpg"); rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" ||
			rec.Header().Get("Content-Disposition") != "inline; filename=whatever.jpg" {
			t.Errorf("other name: %d %v", rec.Code, rec.Header())
		}

		// Conditional and range requests, HEAD.
		if rec := me.get(path, "If-None-Match", `"`+r.SHA+`"`); rec.Code != 304 || rec.Body.Len() != 0 || rec.Header().Get("ETag") == "" {
			t.Errorf("If-None-Match: %d %q", rec.Code, rec.Body)
		}
		rec = me.get(path, "Range", "bytes=2-9")
		if rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), data[2:10]) || rec.Header().Get("Content-Range") != fmt.Sprintf("bytes 2-9/%d", len(data)) ||
			rec.Header().Get("ETag") != `"`+r.SHA+`"` || rec.Header().Get("Cache-Control") == "" {
			t.Errorf("range: %d %v %q", rec.Code, rec.Header(), rec.Body)
		}
		if rec := me.get(path, "Range", "bytes=-4"); rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), data[len(data)-4:]) {
			t.Errorf("suffix range: %d %q", rec.Code, rec.Body)
		}
		if rec := me.get(path, "Range", "bytes=99999-"); rec.Code != 416 {
			t.Errorf("unsatisfiable range: %d", rec.Code)
		}
		if rec := me.get(path, "Range", "bytes=0-3,6-8"); rec.Code != 206 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "multipart/byteranges") {
			t.Errorf("multi range: %d %v", rec.Code, rec.Header())
		}
		if rec := me.get(path, "Range", "bytes=1-3", "If-Range", `"`+r.SHA+`"`); rec.Code != 206 {
			t.Errorf("If-Range with the etag: %d", rec.Code)
		}
		if rec := me.do(http.MethodHead, path); rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") != fmt.Sprint(len(data)) {
			t.Errorf("HEAD: %d %d %v", rec.Code, rec.Body.Len(), rec.Header())
		}

		// Types that a browser could render as a document get the sandbox; downloads are attachments.
		svg := me.mustUpload("image", "logo.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><script>alert(1)</script></svg>`))
		rec = me.get("/assets/" + svg.SHA + "/logo.svg")
		if rec.Header().Get("Content-Type") != "image/svg+xml" || rec.Header().Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" ||
			!strings.HasPrefix(rec.Header().Get("Content-Disposition"), "inline") || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("svg headers: %v", rec.Header())
		}
		pdf := me.mustUpload("manual", "Plan 2026.pdf", mtPDF)
		rec = me.get("/assets/" + pdf.SHA + "/Plan%202026.pdf")
		if rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("Content-Disposition") != `inline; filename="Plan 2026.pdf"` ||
			rec.Header().Get("Content-Security-Policy") != "" {
			t.Errorf("pdf headers: %v", rec.Header())
		}
		txt := me.mustUpload("article", "notes.csv", []byte("a,b\n1,2\n"))
		rec = me.get("/assets/" + txt.SHA + "/notes.csv")
		if rec.Header().Get("Content-Type") != "text/csv; charset=utf-8" || rec.Header().Get("Content-Disposition") != "attachment; filename=notes.csv" {
			t.Errorf("csv headers: %v", rec.Header())
		}
		html := me.mustUpload("article", "page.html", []byte("<!DOCTYPE html><html><body><script>1</script></body></html>"))
		rec = me.get("/assets/" + html.SHA + "/page.html")
		if rec.Header().Get("Content-Disposition") != "attachment; filename=page.html" || rec.Header().Get("Content-Security-Policy") == "" ||
			!strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("html headers: %v", rec.Header())
		}

		// Unknown and malformed identifiers, and a record whose bytes are gone.
		for _, p := range []string{"/assets/" + shaOf("7") + "/x.png", "/assets/nothex/x.png", "/assets/" + strings.ToUpper(r.SHA) + "/x.png"} {
			if rec := me.get(p); rec.Code != 404 {
				t.Errorf("%s: %d", p, rec.Code)
			}
		}
		if err := os.Remove(filepath.Join(me.dir, r.SHA[:2], r.SHA[2:4], r.SHA)); err != nil {
			t.Fatal(err)
		}
		if rec := me.get(path); rec.Code != 404 {
			t.Errorf("missing bytes: %d", rec.Code)
		}
	})
}

func TestMediaURLWithPublicBase(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		me := newMediaEnv(t, b, func(o *media.Options) { o.PublicURL = "https://cdn.example.com/f/" })
		if got := me.m.URL(shaOf("a"), "a b.png"); got != "https://cdn.example.com/f/"+shaOf("a")+"/a%20b.png" {
			t.Fatalf("URL = %s", got)
		}
	})
}

func TestMediaGC(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		up := func(name string, seed byte) media.Result { return me.mustUpload("image", name, mtPNG(8, 8, seed)) }
		alone := up("alone.png", 10)
		published := up("published.png", 11)
		draftOnly := up("draft.png", 12)
		oldVersion := up("old.png", 13)
		newVersion := up("new.png", 14)
		trashed := up("trashed.png", 15)
		inArticle := me.mustUpload("article", "doc.bin", []byte("attachment bytes"))
		touched := up("touched.png", 16)

		pub := me.create("image", "en", fileJSON(published.SHA, "p.png"))
		me.publish(pub.ID)
		me.create("image", "en", fileJSON(draftOnly.SHA, "d.png")) // a draft that was never published
		ver := me.create("image", "en", fileJSON(oldVersion.SHA, "o.png"))
		me.publish(ver.ID)
		me.update(ver.ID, "en", fileJSON(newVersion.SHA, "n.png"))
		me.publish(ver.ID) // the published version now holds newVersion; oldVersion stays in the version history
		tr := me.create("image", "en", fileJSON(trashed.SHA, "t.png"))
		if err := me.svc.Trash(ctx, tr.ID); err != nil {
			t.Fatal(err)
		}
		art := me.create("article", "en", fmt.Sprintf(`{"title":"A","attachment":{"id":%q,"name":"doc.bin"}}`, inArticle.SHA))
		_ = art

		// Nothing is old enough yet.
		res, err := me.m.GC(ctx)
		if err != nil || res.Uploads != 0 {
			t.Fatalf("early gc: %+v %v", res, err)
		}

		me.clk.Advance(20 * time.Hour)
		me.mustUpload("image", "touched.png", mtPNG(8, 8, 16)) // uploaded again: its 24 hours start over
		young := up("young.png", 17)
		me.clk.Advance(5 * time.Hour) // 25 h after the first uploads, 5 h after the touch

		res, err = me.m.GC(ctx)
		if err != nil || res.Uploads != 1 || res.Orphans != 0 {
			t.Fatalf("gc: %+v %v", res, err)
		}
		have := map[string]bool{}
		for _, r := range me.records() {
			have[r.SHA] = true
		}
		for name, sha := range map[string]string{"published": published.SHA, "draft only": draftOnly.SHA, "old version": oldVersion.SHA,
			"new version": newVersion.SHA, "trashed item": trashed.SHA, "other type's file field": inArticle.SHA, "touched": touched.SHA, "young": young.SHA} {
			if !have[sha] {
				t.Errorf("%s was collected", name)
			}
		}
		if have[alone.SHA] {
			t.Error("the unreferenced upload survived")
		}
		if got := me.stored(); len(got) != 8 {
			t.Errorf("stored files = %d: %v", len(got), got)
		}
		var actions []string
		_ = me.db.View(ctx, func(tx store.Tx) error {
			evs, err := tx.Audit().List(ctx, store.AuditQuery{Action: "media.gc"})
			for _, e := range evs {
				actions = append(actions, e.ActorKind+":"+e.Action+":"+string(e.Data))
			}
			return err
		})
		if len(actions) != 1 || !strings.Contains(actions[0], alone.SHA) || !strings.HasPrefix(actions[0], "system:") {
			t.Errorf("gc audit = %v", actions)
		}

		// A purge removes the item, its versions and drafts; its file goes with the next run.
		if err := me.svc.Purge(ctx, tr.ID); err != nil {
			t.Fatal(err)
		}
		hardDelete(t, me.svc, ver.ID)
		me.clk.Advance(time.Hour)
		res, err = me.m.GC(ctx)
		if err != nil || res.Uploads != 3 { // trashed, old version, new version
			t.Fatalf("second gc: %+v %v", res, err)
		}
		// Idempotent.
		if res, err = me.m.GC(ctx); err != nil || res.Uploads != 0 {
			t.Fatalf("third gc: %+v %v", res, err)
		}
	})
}

func TestMediaGCOrphans(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		kept := me.mustUpload("image", "kept.png", mtPNG(6, 6, 20))
		me.create("image", "en", fileJSON(kept.SHA, "kept.png"))
		// Bytes with no record: the leftover of an upload that stopped after storing, and a young one.
		old, young := keyFor("orphan-old"), keyFor("orphan-young")
		for _, k := range []string{old, young} {
			p := filepath.Join(me.dir, k[:2], k[2:4], k)
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil || os.WriteFile(p, []byte("x"), 0o640) != nil {
				t.Fatal("cannot create an orphan")
			}
		}
		past := time.Now().Add(-72 * time.Hour)
		_ = os.Chtimes(filepath.Join(me.dir, old[:2], old[2:4], old), past, past)
		// A far-future clock makes the 24-hour cutoff pass for the young orphan's real mtime too, so move
		// the clock only as far as needed.
		me.clk.Set(time.Now().Add(-12 * time.Hour))
		res, err := me.m.GC(ctx)
		if err != nil || res.Orphans != 1 || res.Uploads != 0 {
			t.Fatalf("gc: %+v %v", res, err)
		}
		got := map[string]bool{}
		for _, n := range me.stored() {
			got[n] = true
		}
		if got[old] || !got[young] || !got[kept.SHA] {
			t.Fatalf("stored = %v", got)
		}
	})
}

func keyFor(s string) string {
	b := ids.NewGenerator(newClock(), strings.NewReader(strings.Repeat(s, 64))).New()
	return fmt.Sprintf("%x%x", b[:], b[:])[:64]
}

func TestMediaRestore(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		data := mtPNG(9, 9, 30)
		r := me.mustUpload("image", "a.png", data)
		rec := me.records()[0]
		other := newMediaEnvFor(t, me)
		if err := other.m.Restore(ctx, rec, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		got := other.records()
		if len(got) != 1 || got[0].SHA != r.SHA || !got[0].CreatedAt.Equal(rec.CreatedAt) || got[0].MIME != "image/png" || got[0].Width != 9 {
			t.Fatalf("restored = %+v", got)
		}
		if err := other.m.Restore(ctx, store.Upload{SHA: shaOf("1")}, bytes.NewReader(data)); err == nil {
			t.Fatal("bytes that do not match the recorded hash were restored")
		}
		rc, f, err := other.m.Open(ctx, r.SHA)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rc.Close() }()
		if b, _ := io.ReadAll(rc); !bytes.Equal(b, data) || f.SHA != r.SHA {
			t.Fatal("restored bytes differ")
		}
	})
}

// newMediaEnvFor builds a second environment on the same kind of database.
func newMediaEnvFor(t *testing.T, like *mediaEnv) *mediaEnv {
	t.Helper()
	for _, b := range backends {
		if b.name == strings.ToLower(like.db.Dialect()) {
			return newMediaEnv(t, b)
		}
	}
	t.Fatal("no backend for " + like.db.Dialect())
	return nil
}
