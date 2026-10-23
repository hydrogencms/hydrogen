package search

import (
	"encoding/json"
	"reflect"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const testSchema = `
types:
  article:
    name: "{title}"
    fields:
      title:   { type: string, translatable: true }
      body:    { type: text, translatable: true }
      secret:  { type: text, ai: false }
      tags:    { type: list, of: { type: string }, index: true }
      rank:    { type: number, index: true }
      day:     { type: date, index: true }
      pinned:  { type: boolean, index: true }
      author:  { type: ref, to: [person], index: true }
      related: { type: list, of: { type: ref, to: [person] }, index: true }
  person:
    fields:
      fullName: { type: string }
      country:  { type: string, index: true }
  settings:
    singleton: true
    fields: { site: { type: string, index: true } }
  hidden:
    search: false
    fields: { title: { type: string } }
`

func compile(t *testing.T, src string) *schema.Schema {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"s.yaml": {Data: []byte(src)}}, "."); err != nil {
		t.Fatal(err)
	}
	sc, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return sc
}

func snap(t *testing.T, item ids.UUID, typ, locale, main string, always bool, name string, fields map[string]any) store.Snapshot {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"id": item.String(), "type": typ, "locale": locale, "name": name, "fields": fields})
	if err != nil {
		t.Fatal(err)
	}
	return store.Snapshot{ItemID: item, Locale: locale, Version: 1, Type: typ, MainLocale: main, AlwaysAvailable: always,
		Payload: payload, PublishedAt: time.Date(2026, 10, 1, 8, 30, 0, 123_000_000, time.UTC)}
}

func TestBuildDocs(t *testing.T) {
	sc := compile(t, testSchema)
	a, b, c := ids.UUID{1}, ids.UUID{2}, ids.UUID{3}
	author := ids.UUID{9}
	snaps := []store.Snapshot{
		snap(t, a, "article", "fr", "en", true, "Panneaux solaires", map[string]any{"title": "Panneaux solaires", "body": "Énergie"}),
		snap(t, a, "article", "en", "en", true, "Solar panels", map[string]any{
			"title": "Solar panels", "body": "Cheap power", "secret": "hidden words",
			"tags": []any{"energy", "home"}, "rank": 2.5, "day": "2026-09-30", "pinned": true,
			"author": author.String(), "related": []any{author.String()},
		}),
		snap(t, b, "settings", "en", "en", false, "Site", map[string]any{"site": "x"}),
		snap(t, c, "hidden", "en", "en", false, "Nope", map[string]any{"title": "Nope"}),
		snap(t, ids.UUID{4}, "unknown", "en", "en", false, "?", nil),
	}
	docs, err := BuildDocs(sc, snaps)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("docs = %d, want 2 (settings, hidden and unknown types are not searchable)", len(docs))
	}
	if docs[0].Locale != "en" || docs[1].Locale != "fr" {
		t.Fatalf("locales = %s, %s; want en, fr (sorted)", docs[0].Locale, docs[1].Locale)
	}
	d := docs[0]
	want := Doc{Item: a, Type: "article", Locale: "en", MainLocale: "en", AlwaysAvailable: true, Locales: []string{"en", "fr"},
		Name: "Solar panels", Text: "Solar panels Cheap power energy home", PublishedAt: time.Date(2026, 10, 1, 8, 30, 0, 123_000_000, time.UTC),
		Fields: map[string]any{
			"tags": []string{"energy", "home"}, "rank": 2.5, "day": time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
			"pinned": true, "author": author, "related": []string{author.String()},
		}}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("en doc:\n got %#v\nwant %#v", d, want)
	}
	f := docs[1]
	if f.Name != "Panneaux solaires" || f.Text != "Panneaux solaires Énergie" || f.Fields != nil || !reflect.DeepEqual(f.Locales, []string{"en", "fr"}) {
		t.Errorf("fr doc: %#v", f)
	}
}

func TestBuildDocsBadPayload(t *testing.T) {
	sc := compile(t, testSchema)
	_, err := BuildDocs(sc, []store.Snapshot{{ItemID: ids.UUID{1}, Locale: "en", Type: "article", Payload: []byte("[]")}})
	if err == nil {
		t.Fatal("expected an error")
	}
}

func TestSettings(t *testing.T) {
	sc := compile(t, testSchema)
	s := NewSettings(sc)
	want := []FieldSetting{
		{"author", KindRef, false, true}, {"country", KindText, false, true}, {"day", KindTime, false, true},
		{"pinned", KindBool, false, true}, {"rank", KindNum, false, true}, {"related", KindRef, true, false},
		{"tags", KindText, true, false},
	}
	if !reflect.DeepEqual(s.Fields, want) {
		t.Errorf("fields = %v\nwant %v", s.Fields, want)
	}
	if !reflect.DeepEqual(s.Types, []string{"article", "person"}) {
		t.Errorf("types = %v", s.Types)
	}
	if again := NewSettings(compile(t, testSchema)); again.Fingerprint != s.Fingerprint || len(s.Fingerprint) != 64 {
		t.Errorf("fingerprint not stable: %q vs %q", s.Fingerprint, again.Fingerprint)
	}
	changed := NewSettings(compile(t, testSchema+"  note:\n    fields: { n: { type: string, index: true } }\n"))
	if changed.Fingerprint == s.Fingerprint {
		t.Error("fingerprint ignores a new searchable type and field")
	}
	off := NewSettings(compile(t, testSchema+"  extra:\n    search: false\n    fields: { n: { type: string, index: true } }\n"))
	if off.Fingerprint != s.Fingerprint {
		t.Error("a type that is not searchable changed the fingerprint")
	}
}

func TestMarked(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a \x02b<c>\x03 & d", "a <mark>b&lt;c&gt;</mark> &amp; d"},
		{"<script>\x02x\x03</script>", "&lt;script&gt;<mark>x</mark>&lt;/script&gt;"},
		{"plain", "plain"},
	} {
		if got := Marked(tc.in); got != tc.want {
			t.Errorf("Marked(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMarkTerms(t *testing.T) {
	for _, tc := range []struct {
		text  string
		terms []string
		want  string
	}{
		{"Solar panels", []string{"solar"}, "\x02Solar\x03 panels"},
		{"Solar panels for solar", []string{"SOL", "pan"}, "\x02Sol\x03ar \x02pan\x03els for \x02sol\x03ar"},
		{"Subsolar", []string{"solar"}, "Subsolar"},
		{"Énergie solaire", []string{"éner"}, "\x02Éner\x03gie solaire"},
		{"nothing", []string{"x"}, "nothing"},
		{"nothing", nil, "nothing"},
	} {
		if got := MarkTerms(tc.text, tc.terms); got != tc.want {
			t.Errorf("MarkTerms(%q, %v) = %q, want %q", tc.text, tc.terms, got, tc.want)
		}
	}
}

func TestRequestValidate(t *testing.T) {
	ok := Request{Types: []string{"a"}, Locale: "en", Limit: 10}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*Request){
		"types":  func(r *Request) { r.Types = nil },
		"locale": func(r *Request) { r.Locale = "" },
		"limit0": func(r *Request) { r.Limit = 0 },
		"limit":  func(r *Request) { r.Limit = 101 },
		"offset": func(r *Request) { r.Offset = 1001 },
		"facets": func(r *Request) { r.Facets = []string{"a", "b", "c", "d", "e", "f"} },
	} {
		r := ok
		mod(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestFingerprintFollowsTextFields(t *testing.T) {
	base := NewSettings(compile(t, testSchema)).Fingerprint
	more := NewSettings(compile(t, testSchema+"  note:\n    fields: { t: { type: string } }\n")).Fingerprint
	if more == base {
		t.Error("a new searchable type with text left the fingerprint alone")
	}
	withText := NewSettings(compile(t, `
types:
  a: { fields: { t: { type: string } } }
`)).Fingerprint
	moreText := NewSettings(compile(t, `
types:
  a: { fields: { t: { type: string }, u: { type: text } } }
`)).Fingerprint
	hiddenText := NewSettings(compile(t, `
types:
  a: { fields: { t: { type: string }, u: { type: text, ai: false } } }
`)).Fingerprint
	if withText == moreText {
		t.Error("a new text field left the fingerprint alone")
	}
	if withText != hiddenText {
		t.Error("an ai: false field changed the fingerprint")
	}
}

func TestBuildDocsExclude(t *testing.T) {
	const src = `
types:
  a:
    search: { exclude: noindex }
    fields: { t: { type: string, translatable: true }, noindex: { type: boolean, translatable: true } }
`
	sc := compile(t, src)
	item := ids.UUID{1}
	docs, err := BuildDocs(sc, []store.Snapshot{
		snap(t, item, "a", "en", "en", false, "One", map[string]any{"t": "one"}),
		snap(t, item, "a", "de", "en", false, "Eins", map[string]any{"t": "eins", "noindex": true}),
		snap(t, ids.UUID{2}, "a", "en", "en", false, "Two", map[string]any{"t": "two", "noindex": false}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Locale != "en" || docs[0].Item != item || docs[1].Item != (ids.UUID{2}) {
		t.Errorf("docs = %+v", docs)
	}
	plain := NewSettings(compile(t, `
types:
  a: { fields: { t: { type: string, translatable: true }, noindex: { type: boolean, translatable: true } } }
`)).Fingerprint
	if NewSettings(sc).Fingerprint == plain {
		t.Error("an exclusion field left the fingerprint alone")
	}
}

func TestMarksStripRawMarkers(t *testing.T) {
	if got := MarkTerms("a\x02b\x03 solar", []string{"solar"}); got != "ab \x02solar\x03" {
		t.Errorf("MarkTerms = %q", got)
	}
	if got := MarkTerms("a\x02b", nil); got != "ab" {
		t.Errorf("MarkTerms without terms = %q", got)
	}
}

func TestValidateTerms(t *testing.T) {
	r := Request{Types: []string{"a"}, Locale: "en", Limit: 1, Text: "a b c d e f g h i j k l m n o p"}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	r.Text += " q"
	if err := r.Validate(); err == nil {
		t.Error("17 terms accepted")
	}
}
