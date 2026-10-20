package schema

import (
	"strings"
	"testing"
)

const uploadFixture = `
types:
  image:
    upload: { accept: [image/*, application/pdf], max_size: 20MB }
    fields:
      alt: { type: string, translatable: true, max: 300 }
  doc:
    upload: true
    fields: {}
  article:
    fields:
      title: { type: string }
      hero:  { type: asset, to: [image] }
      any:   { type: asset }
      gallery: { type: list, of: { type: asset, to: [image], on_delete: block } }
      attach: { type: file }
`

func TestUploadTypeBuiltInFields(t *testing.T) {
	s := mustCompile(t, uploadFixture)
	img, _ := s.Type("image")
	if img.Upload == nil || img.Upload.MaxSize != 20<<20 || len(img.Upload.Accept) != 2 {
		t.Fatalf("upload = %+v", img.Upload)
	}
	var names []string
	for _, f := range img.Fields {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); got != "file,folder,tags,alt" {
		t.Fatalf("fields = %s", got)
	}
	file, _ := img.Field("file")
	folder, _ := img.Field("folder")
	tags, _ := img.Field("tags")
	if !file.Required || file.Translatable || file.Shape.Base != BaseFile || !folder.Index || folder.Default != "/" || !tags.Index {
		t.Fatalf("built-in fields: %+v %+v %+v", file, folder, tags)
	}
	if !img.Upload.Matches("image/png") || !img.Upload.Matches("application/pdf; x=y") || img.Upload.Matches("text/html") {
		t.Fatal("accept patterns")
	}
	if d, _ := s.Type("doc"); d.Upload == nil || !d.Upload.Matches("anything/else") {
		t.Fatal("upload: true accepts everything")
	}
	art, _ := s.Type("article")
	any, _ := art.Field("any")
	if got := strings.Join(any.Shape.To, ","); got != "doc,image" {
		t.Fatalf("default to = %s", got)
	}
	hero, _ := art.Field("hero")
	if hero.Shape.Base != BaseRef || hero.Shape.Format != "asset" || hero.Config.(*refConfig).onDelete != "nullify" {
		t.Fatalf("hero = %+v", hero.Shape)
	}
	gal, _ := art.Field("gallery")
	if gal.Of.Config.(*refConfig).onDelete != "block" {
		t.Fatal("on_delete block")
	}
}

func TestUploadSchemaErrors(t *testing.T) {
	cases := map[string]struct{ yaml, want string }{
		"declared built-in": {"types: { image: { upload: true, fields: { file: { type: string } } } }", `field "file" is built into upload types`},
		"declared tags":     {"types: { image: { upload: true, fields: { tags: { type: string } } } }", `field "tags" is built into upload types`},
		"singleton":         {"types: { image: { upload: true, singleton: true } }", "cannot also be singleton"},
		"page":              {"types: { image: { upload: true, page: true } }", "cannot also be page"},
		"bad accept":        {"types: { image: { upload: { accept: [image] } } }", "not a MIME type"},
		"bad size":          {"types: { image: { upload: { max_size: lots } } }", "max_size"},
		"zero size":         {"types: { image: { upload: { max_size: 0 } } }", "max_size"},
		"unknown key":       {"types: { image: { upload: { size: 5 } } }", `unknown key "size" in upload`},
		"asset to plain":    {"types: { image: { upload: true }, a: { fields: { x: { type: asset, to: [a] } } } }", "not an upload type"},
		"asset to unknown":  {"types: { image: { upload: true }, a: { fields: { x: { type: asset, to: [nope] } } } }", `unknown type "nope"`},
		"asset no uploads":  {"types: { a: { fields: { x: { type: asset } } } }", "no upload types"},
		"asset bad delete":  {"types: { image: { upload: true }, a: { fields: { x: { type: asset, on_delete: x } } } }", "on_delete must be"},
		"file option":       {"types: { a: { fields: { x: { type: file, max: 3 } } } }", `unknown option "max"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := compile(t, map[string]string{"schema/s.yaml": c.yaml})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestFileValidation(t *testing.T) {
	s := mustCompile(t, uploadFixture)
	img, _ := s.Type("image")
	sha := strings.Repeat("ab", 32)
	ok := func() map[string]any {
		return map[string]any{"file": map[string]any{"id": sha, "name": "a.png"}, "folder": "/x/y", "tags": []any{"a"}}
	}
	if errs := s.Validate(img, ok(), ModePublish); len(errs) != 0 {
		t.Fatalf("valid: %v", errs)
	}
	bad := map[string]map[string]any{
		"id":    {"id": "XYZ", "name": "a"},
		"name":  {"id": sha, "name": "a/b"},
		"empty": {"id": sha, "name": " "},
		"size":  {"id": sha, "name": "a", "size": -1},
		"focal": {"id": sha, "name": "a", "focal": map[string]any{"x": 2, "y": 0.5}},
		"extra": {"id": sha, "name": "a", "path": "/etc"},
	}
	for name, v := range bad {
		d := ok()
		d["file"] = v
		if errs := s.Validate(img, d, ModeDraft); len(errs) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	d := ok()
	d["folder"] = "/x/"
	if errs := s.Validate(img, d, ModeDraft); len(errs) != 1 || errs[0].Code != "pattern" {
		t.Errorf("folder: %v", errs)
	}
	if errs := s.Validate(img, map[string]any{}, ModePublish); len(errs) != 1 || errs[0].Path != "file" {
		t.Errorf("missing file: %v", errs)
	}
}

func TestUploadNameIsFileName(t *testing.T) {
	s := mustCompile(t, uploadFixture)
	img, _ := s.Type("image")
	doc := map[string]any{"alt": "A cat", "file": map[string]any{"id": strings.Repeat("a", 64), "name": " cat.png "}}
	if got := s.Name(img, doc); got != "cat.png" {
		t.Fatalf("name = %q", got)
	}
}

func TestAssetRefs(t *testing.T) {
	s := mustCompile(t, uploadFixture)
	art, _ := s.Type("article")
	refs := s.Refs(art, map[string]any{"hero": id1, "gallery": []any{id2}})
	if len(refs) != 2 || refs[0].OnDelete != "nullify" || refs[1].OnDelete != "block" {
		t.Fatalf("refs = %+v", refs)
	}
	var seen []string
	s.MapValues(art, map[string]any{"hero": id1, "gallery": []any{id2}}, func(f *Field) bool { return f.Shape.Format == "asset" },
		func(_ *Field, path string, v any) (any, bool) { seen = append(seen, path); return nil, true })
	if strings.Join(seen, ",") != "hero,gallery[0]" {
		t.Fatalf("seen = %v", seen)
	}
	if !s.HasFields(art, func(f *Field) bool { return f.Shape.Base == BaseFile }) {
		t.Fatal("HasFields")
	}
	if p, _ := s.Type("doc"); s.HasFields(p, func(f *Field) bool { return f.Shape.Format == "asset" }) {
		t.Fatal("doc has no asset field")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"20MB": 20 << 20, "512 KB": 512 << 10, "1.5GB": 3 << 29, "100": 100, "2MiB": 2 << 20} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("%q = %d, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "x", "-1MB", "0"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestURLItemRefs(t *testing.T) {
	s := mustCompile(t, `
blocks:
  cta:
    props:
      link: { type: group, fields: { url: { type: url, relative: true } } }
types:
  page:
    fields:
      more: { type: url }
      links: { type: list, of: { type: url } }
      body: { type: blocks, allow: [cta] }
`)
	p, _ := s.Type("page")
	refs := s.Refs(p, map[string]any{"more": "item:" + id1, "links": []any{"https://x.example", "item:" + id2},
		"body": []any{map[string]any{"type": "cta", "props": map[string]any{"link": map[string]any{"url": "item:" + id1}}}}})
	want := []string{"more", "links[1]", "body[0].props.link.url"}
	if len(refs) != len(want) {
		t.Fatalf("refs = %+v", refs)
	}
	for i, r := range refs {
		if r.Path != want[i] || r.Kind != "link" || r.OnDelete != "warn" {
			t.Errorf("ref %d = %+v", i, r)
		}
	}
}
