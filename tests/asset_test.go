package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

const assetSchema = `
types:
  image:
    upload: { accept: [image/*], max_size: 1MB }
    fields:
      alt:     { type: string, translatable: true }
      caption: { type: text, translatable: true }
  manual:
    upload: { accept: [application/pdf] }
    fields: {}
  article:
    name: "{title}"
    fields:
      title:      { type: string, translatable: true, required: true }
      hero:       { type: asset, to: [image] }
      cover:      { type: asset, to: [image], on_delete: block }
      gallery:    { type: list, of: { type: asset, to: [image] } }
      attachment: { type: file }
`

// The files the fake registry knows. shaOf makes a distinct valid SHA-256 string per letter.
func shaOf(c string) string { return strings.Repeat(c, 64) }

var (
	shaCat    = shaOf("a")
	shaDog    = shaOf("b")
	shaBig    = shaOf("c")
	shaPDF    = shaOf("d")
	shaHTML   = shaOf("e")
	shaAbsent = shaOf("f")
)

// fakeFiles is the file registry the media module provides in a real project.
type fakeFiles struct {
	mu    sync.Mutex
	known map[string]content.FileInfo
	calls int
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{known: map[string]content.FileInfo{
		shaCat:  {ID: shaCat, Mime: "image/png", Size: 2048, Width: 800, Height: 600},
		shaDog:  {ID: shaDog, Mime: "image/jpeg", Size: 4096, Width: 640, Height: 480},
		shaBig:  {ID: shaBig, Mime: "image/png", Size: 5 << 20, Width: 4000, Height: 3000},
		shaPDF:  {ID: shaPDF, Mime: "application/pdf", Size: 9000},
		shaHTML: {ID: shaHTML, Mime: "text/html", Size: 100},
	}}
}

func (f *fakeFiles) Lookup(_ context.Context, sha string) (content.FileInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if info, ok := f.known[sha]; ok {
		return info, nil
	}
	return content.FileInfo{}, content.ErrFileNotFound
}

func (f *fakeFiles) Touch(_ context.Context, sha string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.known[sha]; !ok {
		return content.ErrFileNotFound
	}
	return nil
}

func newAssetEnv(t testing.TB, b backend) *env {
	t.Helper()
	clk := clock.NewManual(newClock().Now())
	e := newEnv(t, b.open(t, clk), clk, envConfig{schema: assetSchema})
	e.svc.SetFiles(newFakeFiles())
	return e
}

func runAssets(t *testing.T, fn func(t *testing.T, e *env)) {
	forEachBackend(t, func(t *testing.T, b backend) { fn(t, newAssetEnv(t, b)) })
}

func fileJSON(sha, name string) string { return fmt.Sprintf(`{"file":{"id":%q,"name":%q}}`, sha, name) }

func (e *env) image(sha, name string) *content.View {
	e.t.Helper()
	return e.create("image", "en", fileJSON(sha, name))
}

func TestAssetFileMetadataIsServerOwned(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		forged := fmt.Sprintf(`{"file":{"id":%q,"name":"  cat.png ","mime":"text/html","size":1,"width":9999,"height":1,
			"focal":{"x":0.25,"y":0.75,"junk":1}},"folder":"/pets"}`, shaCat)
		_, err := e.svc.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: doc(t, forged)})
		// Unknown keys inside focal are rejected rather than silently dropped.
		_ = wantValidation(t, err)

		forged = strings.Replace(forged, `,"junk":1`, "", 1)
		v := e.create("image", "en", forged)
		file := v.Fields["file"].(map[string]any)
		eq(t, "mime", file["mime"], any("image/png"))
		eq(t, "size", fmt.Sprint(file["size"]), "2048")
		eq(t, "width", fmt.Sprint(file["width"]), "800")
		eq(t, "height", fmt.Sprint(file["height"]), "600")
		eq(t, "name trimmed", file["name"], any("cat.png"))
		focal := file["focal"].(map[string]any)
		eq(t, "focal", fmt.Sprint(focal["x"], " ", focal["y"]), "0.25 0.75")
		eq(t, "folder", v.Fields["folder"], any("/pets"))
		eq(t, "display name is the file name", v.Name, "cat.png")

		// A patch cannot forge it either: replacing the file re-reads the record, and a merge patch keeps the old focal
		// point unless it is nulled.
		v = e.update(v.ID, "en", fmt.Sprintf(`{"file":{"id":%q,"name":"dog.jpg","width":5,"height":5,"focal":null}}`, shaDog))
		file = v.Fields["file"].(map[string]any)
		eq(t, "mime after replace", file["mime"], any("image/jpeg"))
		eq(t, "width after replace", fmt.Sprint(file["width"]), "640")
		if _, ok := file["focal"]; ok {
			t.Errorf("focal survived a replaced file: %v", file)
		}

		// A non-image has no dimensions, whatever the client says.
		m := e.create("manual", "en", fmt.Sprintf(`{"file":{"id":%q,"name":"m.pdf","width":5,"height":5}}`, shaPDF))
		file = m.Fields["file"].(map[string]any)
		eq(t, "pdf mime", file["mime"], any("application/pdf"))
		if _, ok := file["width"]; ok {
			t.Errorf("dimensions kept for a non-image: %v", file)
		}
	})
}

func TestAssetUploadDefaults(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		v := e.image(shaCat, "cat.png")
		eq(t, "folder default", v.Fields["folder"], any("/"))
		e.update(v.ID, "en", `{"alt":"A cat"}`)
		pub := e.publish(v.ID)
		eq(t, "name", pub.Name, "cat.png")
		eq(t, "alt", pub.Fields["alt"], any("A cat"))
	})
}

func TestAssetUploadRules(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		cases := map[string]struct{ sha, code string }{
			"unknown file": {shaAbsent, "file_not_found"},
			"wrong type":   {shaHTML, "file_type"},
			"pdf":          {shaPDF, "file_type"},
			"too big":      {shaBig, "file_size"},
		}
		for name, c := range cases {
			_, err := e.svc.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: doc(t, fileJSON(c.sha, "f"))})
			ve := wantValidation(t, err)
			if len(ve.Errors) == 0 || ve.Errors[0].Code != c.code {
				t.Errorf("%s: errors = %+v, want %s", name, ve.Errors, c.code)
			}
		}
		// The manual type takes PDFs only.
		if _, err := e.svc.Create(ctx, content.CreateInput{Type: "manual", Locale: "en", Fields: doc(t, fileJSON(shaPDF, "m.pdf"))}); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.Create(ctx, content.CreateInput{Type: "manual", Locale: "en", Fields: doc(t, fileJSON(shaCat, "m.png"))})
		_ = wantValidation(t, err)
		// Malformed values never reach the registry.
		for _, bad := range []string{`{"file":{"id":"nope","name":"x"}}`, `{"file":{"id":"` + shaCat + `"}}`, `{"file":"x"}`,
			`{"file":{"id":"` + shaCat + `","name":"a/b"}}`, `{"file":{"id":"` + shaCat + `","name":"a","focal":{"x":3,"y":0}}}`} {
			_, err := e.svc.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: doc(t, bad)})
			_ = wantValidation(t, err)
		}
		// A file is required to publish.
		v, err := e.svc.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: doc(t, `{"alt":"no file"}`)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.svc.Publish(ctx, v.ID, content.PublishOptions{})
		_ = wantValidation(t, err)
	})
}

func TestAssetFileFieldOnPlainType(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		a := e.create("article", "en", fmt.Sprintf(`{"title":"A","attachment":{"id":%q,"name":"cat.png","size":1}}`, shaCat))
		att := a.Fields["attachment"].(map[string]any)
		eq(t, "size", fmt.Sprint(att["size"]), "2048")
		_, err := e.svc.Update(ctx, a.ID, content.UpdateInput{Fields: doc(t, fmt.Sprintf(`{"attachment":{"id":%q,"name":"x"}}`, shaAbsent))})
		_ = wantValidation(t, err)
	})
}

func TestAssetWithoutRegistryRefusesFileValues(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{schema: assetSchema})
		if _, err := e.svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: doc(t, `{"title":"No files"}`)}); err != nil {
			t.Fatal(err) // no file value, no registry needed
		}
		_, err := e.svc.Create(ctx, content.CreateInput{Type: "image", Locale: "en", Fields: doc(t, fileJSON(shaCat, "x"))})
		if err == nil || errors.Is(err, content.ErrInvalid) {
			t.Fatalf("err = %v, want a server error", err)
		}
	})
}

func TestAssetReferencesAndUsage(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		img := e.publish(e.image(shaCat, "cat.png").ID)
		other := e.publish(e.image(shaDog, "dog.jpg").ID)
		a := e.create("article", "en", fmt.Sprintf(`{"title":"A","hero":%q,"gallery":[%q,%q]}`, strings.ToUpper(img.ID.String()), img.ID, other.ID))
		e.publish(a.ID)
		eq(t, "ids are normalised", a.Fields["hero"], any(img.ID.String()))

		refs, err := e.svc.References(ctx, img.ID)
		if err != nil {
			t.Fatal(err)
		}
		fields := map[string]string{}
		for _, r := range refs {
			eq(t, "referrer", r.ItemID, a.ID)
			fields[r.FieldPath] = r.OnDelete
		}
		if len(fields) != 2 || fields["hero"] != "nullify" || fields["gallery[0]"] != "nullify" {
			t.Fatalf("usage = %v", fields)
		}
		refs, _ = e.svc.References(ctx, other.ID)
		if len(refs) != 1 || refs[0].FieldPath != "gallery[1]" {
			t.Fatalf("usage of the second image = %+v", refs)
		}

		// Only upload items fit an asset field.
		p := e.person2()
		e.update(a.ID, "en", fmt.Sprintf(`{"hero":%q}`, p))
		_, err = e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		_ = wantValidation(t, err)
	})
}

// person2 makes an article to use as a wrong-typed target.
func (e *env) person2() string { return e.article("Not an image").ID.String() }

func TestAssetOnDeleteSemantics(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		img := e.publish(e.image(shaCat, "cat.png").ID)
		spare := e.publish(e.image(shaDog, "dog.jpg").ID)
		a := e.create("article", "en", fmt.Sprintf(`{"title":"A","hero":%q,"gallery":[%q,%q]}`, img.ID, spare.ID, img.ID))
		e.publish(a.ID)

		// cover is on_delete: block, so using the image there blocks its removal.
		e.update(a.ID, "en", fmt.Sprintf(`{"cover":%q}`, img.ID))
		e.publish(a.ID)
		err := e.svc.Delete(ctx, img.ID)
		wantErr(t, err, content.ErrReferenced)
		var re *content.ReferencedError
		if !errors.As(err, &re) || len(re.Referrers) == 0 {
			t.Fatalf("referenced = %v", err)
		}
		blocked := false
		for _, r := range re.Referrers {
			blocked = blocked || (r.FieldPath == "cover" && r.OnDelete == "block")
		}
		if !blocked {
			t.Fatalf("referrers = %+v", re.Referrers)
		}

		// Without the blocking field the default nullify clears the others.
		e.update(a.ID, "en", `{"cover":null}`)
		e.publish(a.ID)
		hardDelete(t, e.svc, img.ID)
		pub := e.get(a.ID, "", false)
		if _, ok := pub.Fields["hero"]; ok {
			t.Errorf("hero kept: %v", pub.Fields)
		}
		eq(t, "gallery after nullify", asJSON(pub.Fields["gallery"]), fmt.Sprintf(`["%s"]`, spare.ID))
	})
}

func TestAssetFilters(t *testing.T) {
	runAssets(t, func(t *testing.T, e *env) {
		a := e.create("image", "en", fmt.Sprintf(`{"file":{"id":%q,"name":"cat.png"},"folder":"/pets","tags":["cat","cute"]}`, shaCat))
		e.create("image", "en", fmt.Sprintf(`{"file":{"id":%q,"name":"dog.jpg"},"folder":"/pets/dogs","tags":["dog"]}`, shaDog))
		list := func(field string, op store.Op, v any) []ids.UUID {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "image", Where: []content.Filter{{Field: field, Op: op, Value: v}}})
			if err != nil {
				t.Fatal(err)
			}
			var out []ids.UUID
			for _, it := range res.Items {
				out = append(out, it.ID)
			}
			return out
		}
		if got := list("file", store.OpEq, shaCat); len(got) != 1 || got[0] != a.ID {
			t.Errorf("file eq = %v", got)
		}
		if got := list("file", store.OpEq, shaAbsent); len(got) != 0 {
			t.Errorf("file eq absent = %v", got)
		}
		if got := list("folder", store.OpEq, "/pets"); len(got) != 1 || got[0] != a.ID {
			t.Errorf("folder eq = %v", got)
		}
		if got := list("folder", store.OpPrefix, "/pets"); len(got) != 2 {
			t.Errorf("folder prefix = %v", got)
		}
		if got := list("tags", store.OpContains, "cute"); len(got) != 1 || got[0] != a.ID {
			t.Errorf("tags contains = %v", got)
		}
	})
}
