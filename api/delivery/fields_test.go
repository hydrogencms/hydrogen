package delivery

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
)

func fieldsSchema(t testing.TB) *schema.Schema {
	t.Helper()
	fsys := fstest.MapFS{"schema/s.yaml": {Data: []byte(`types:
  article:
    fields:
      title:  { type: string }
      body:   { type: text }
      author: { type: ref, to: [person] }
      seo:    { type: group, fields: { title: { type: string }, image: { type: ref, to: [person] } } }
      tags:   { type: list, of: { type: string } }
  person: { fields: { fullName: { type: string }, title: { type: string } } }
`)}}
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fsys, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustType(t testing.TB, s *schema.Schema, name string) *schema.Type {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("no type %s", name)
	}
	return typ
}

func TestParseFields(t *testing.T) {
	s := fieldsSchema(t)
	art, per := mustType(t, s, "article"), mustType(t, s, "person")
	f, err := parseFields(" title , seo.title,seo.title", []*schema.Type{art})
	if err != nil {
		t.Fatal(err)
	}
	if want := (selection{"title": nil, "seo": selection{"title": nil}}); !reflect.DeepEqual(f.sel, want) {
		t.Fatalf("sel = %v", f.sel)
	}
	if f.refs["article"] {
		t.Error("title and seo.title hold no references")
	}
	// A whole group wins over a member, in either order.
	for _, raw := range []string{"seo,seo.title", "seo.title,seo"} {
		f, err := parseFields(raw, []*schema.Type{art})
		if err != nil || !reflect.DeepEqual(f.sel, selection{"seo": nil}) {
			t.Fatalf("%s: %v %v", raw, f, err)
		}
		if !f.refs["article"] {
			t.Errorf("%s: the whole seo group can hold a reference", raw)
		}
	}
	if f, _ := parseFields("seo.image", []*schema.Type{art}); !f.refs["article"] {
		t.Error("seo.image is a reference")
	}
	if f, _ := parseFields("", []*schema.Type{art}); f != nil {
		t.Error("empty must be no projection")
	}
	// Any of several types may have the field; each type knows whether its selection can hold references.
	f, err = parseFields("fullName,author", []*schema.Type{art, per})
	if err != nil || !f.refs["article"] || f.refs["person"] {
		t.Fatalf("multi-type: %v %v", f, err)
	}
	for raw, want := range map[string]string{
		"nope":        "type article has no field nope",
		"title.x":     "title of type article is not a group",
		"seo.nope":    "type article has no field seo.nope",
		"title,,body": "comma-separated",
		"a" + strings.Repeat(",a", maxFieldPaths): "at most",
	} {
		if _, err := parseFields(raw, []*schema.Type{art}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%.40s: err = %v, want %q", raw, err, want)
		}
	}
	if _, err := parseFields("nope", []*schema.Type{art, per}); err == nil || !strings.Contains(err.Error(), "no collection of the channel has a field nope") {
		t.Errorf("multi-type unknown: %v", err)
	}
}

func TestProjectPayload(t *testing.T) {
	payload := `{"id":"x","type":"article","locale":"en","version":2,"publishedAt":"2026-01-01T00:00:00.000Z","name":"N",` +
		`"fields":{"title":"T \"q\" {[","body":"long","author":"a","seo":{"title":"S","image":"i"},"tags":["x","}"]}}`
	got, fields, err := projectPayload([]byte(payload), selection{"title": nil, "seo": selection{"image": nil}, "tags": nil, "absent": nil})
	if err != nil {
		t.Fatal(err)
	}
	wantFields := `{"title":"T \"q\" {[","seo":{"image":"i"},"tags":["x","}"]}`
	want := `{"id":"x","type":"article","locale":"en","version":2,"publishedAt":"2026-01-01T00:00:00.000Z","name":"N","fields":` + wantFields + `}`
	if string(got) != want || string(fields) != wantFields {
		t.Fatalf("projected\n %s\nwant\n %s\nfields %s", got, want, fields)
	}
	if got, _, _ := projectPayload([]byte(`{"id":"x","fields":{"a":1}}`), selection{}); string(got) != `{"id":"x","fields":{}}` {
		t.Fatalf("nothing selected = %s", got)
	}
	// A sub-selection of a value that is not an object keeps the value.
	if got, _, _ := projectPayload([]byte(`{"fields":{"seo":null}}`), selection{"seo": selection{"title": nil}}); string(got) != `{"fields":{"seo":null}}` {
		t.Fatalf("null group = %s", got)
	}
	for _, bad := range []string{``, `[]`, `{"fields":{"a":}}`, `{"fields":{"a":"x}}`, `{"fields":{"a":[1,2}}`, `{"a" 1}`, `{"a":1,}`} {
		if _, _, err := projectPayload([]byte(bad), selection{"a": nil}); err == nil {
			t.Errorf("%q: malformed JSON accepted", bad)
		}
	}
}

func TestProjectRefs(t *testing.T) {
	s := fieldsSchema(t)
	a, b := ids.UUID{15: 1}, ids.UUID{15: 2}
	payload := `{"id":"x","type":"article","fields":{"title":"T","author":"` + a.String() + `","seo":{"image":"` + b.String() + `"}}}`
	st := &state{a: &API{o: Options{Schema: s}}, m: &chanMeta{ch: &channel.Channel{Name: "c", Languages: []string{"en"}}}}
	project := func(raw string) *resolved {
		t.Helper()
		f, err := parseFields(raw, []*schema.Type{mustType(t, s, "article")})
		if err != nil {
			t.Fatal(err)
		}
		r := &resolved{typ: "article", payload: []byte(payload), refs: []ids.UUID{a, b}, etag: "e"}
		if err := st.project([]*resolved{r}, f); err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := project("title")
	if len(r.refs) != 0 || r.etag != "" || string(r.source()) != payload {
		t.Fatalf("title: refs %v etag %q source %s", r.refs, r.etag, r.source())
	}
	if r = project("seo.image,author"); !reflect.DeepEqual(r.refs, []ids.UUID{a, b}) {
		t.Fatalf("refs = %v", r.refs)
	}
	if r = project("seo.title,author"); !reflect.DeepEqual(r.refs, []ids.UUID{a}) {
		t.Fatalf("refs = %v", r.refs)
	}
}

// FuzzEachMember checks the scanner against encoding/json on the objects it decodes (payloads are valid UTF-8).
func FuzzEachMember(f *testing.F) {
	for _, s := range []string{`{}`, ` { "a" : 1 , "b":[1,{"c":"]"}], "d":"\\\"" } `, `{"a":true,"b":null,"c":-1.5e3}`,
		`{"aA":{"x":{}}}`, `{"a":"x\"}"}`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		var want map[string]json.RawMessage
		if !utf8.ValidString(s) || json.Unmarshal([]byte(s), &want) != nil || want == nil {
			return
		}
		got := map[string]json.RawMessage{}
		err := eachMember([]byte(s), func(key, val []byte, _ bool) error {
			name, err := keyName(key)
			got[name] = append(json.RawMessage(nil), val...)
			return err
		})
		if err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%q: members %v, want %v", s, got, want)
		}
		for k, v := range want {
			var a, b any
			if json.Unmarshal(v, &a) != nil || json.Unmarshal(got[k], &b) != nil || !reflect.DeepEqual(a, b) {
				t.Fatalf("%q: member %q = %s, want %s", s, k, got[k], v)
			}
		}
	})
}

// BenchmarkProjectPayload projects three small fields out of a page-builder payload of about 1.5 MB.
func BenchmarkProjectPayload(b *testing.B) {
	var blocks strings.Builder
	for i := range 4000 {
		if i > 0 {
			blocks.WriteByte(',')
		}
		blocks.WriteString(`{"id":"0192a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","type":"richText","props":{"text":"Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat. Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur.","tone":"light"}}`)
	}
	payload := []byte(`{"id":"x","type":"landing","locale":"en","version":3,"publishedAt":"2026-01-01T00:00:00.000Z","name":"N",` +
		`"fields":{"title":"Landing","navAlias":"Short","showInMain":true,"body":[` + blocks.String() + `],"seo":{"title":"S"}}}`)
	sel := selection{"title": nil, "navAlias": nil, "showInMain": nil}
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := projectPayload(payload, sel); err != nil {
			b.Fatal(err)
		}
	}
}
