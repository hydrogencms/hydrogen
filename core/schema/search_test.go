package schema

import (
	"strings"
	"testing"
	"testing/fstest"
)

func compileSearch(t *testing.T, src string) (*Schema, error) {
	t.Helper()
	b := NewBuilder(NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"s.yaml": {Data: []byte(src)}}, "."); err != nil {
		return nil, err
	}
	return b.Compile()
}

func TestSearchableDefaults(t *testing.T) {
	s, err := compileSearch(t, `
types:
  article: { fields: { title: { type: string } } }
  settings: { singleton: true, fields: { title: { type: string } } }
  image: { upload: true, fields: { alt: { type: string } } }
  hidden: { search: false, fields: { title: { type: string } } }
  config: { singleton: true, search: true, fields: { title: { type: string } } }
`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"article": true, "settings": false, "image": false, "hidden": false, "config": true}
	for name, w := range want {
		ty, _ := s.Type(name)
		if got := ty.Searchable(); got != w {
			t.Errorf("%s searchable = %v, want %v", name, got, w)
		}
	}
}

func TestSearchOptionShape(t *testing.T) {
	_, err := compileSearch(t, "types:\n  a: { search: 'yes', fields: { t: { type: string } } }\n")
	if err == nil || !strings.Contains(err.Error(), "must be true, false or an object") {
		t.Fatalf("err = %v", err)
	}
}

const weightedSrc = `
types:
  article:
    search:
      weights: { name: high, summary: high, comments: low, title: normal }
      exclude: hidden
    fields:
      title: { type: string }
      summary: { type: text }
      body: { type: richtext }
      comments: { type: list, of: { type: string } }
      hidden: { type: boolean }
      views: { type: number }
  config:
    singleton: true
    search: { weights: { title: high } }
    fields: { title: { type: string } }
  off:
    search: { enabled: false, exclude: gone }
    fields: { title: { type: string }, gone: { type: boolean } }
`

func TestSearchConfig(t *testing.T) {
	s, err := compileSearch(t, weightedSrc)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Type("article")
	cfg := a.Search()
	if !cfg.Enabled || cfg.Exclude != "hidden" || cfg.Tier("name") != TierHigh || cfg.Tier("summary") != TierHigh ||
		cfg.Tier("comments") != TierLow || cfg.Tier("title") != TierNormal || cfg.Tier("body") != TierNormal {
		t.Errorf("article %+v", cfg)
	}
	if c, _ := s.Type("config"); !c.Searchable() {
		t.Error("an object turns a singleton on")
	}
	if o, _ := s.Type("off"); o.Searchable() {
		t.Error("enabled: false")
	}
	doc := map[string]any{"title": "Solar", "summary": "Cheap power", "comments": []any{"nice", "ok"}, "hidden": false, "views": 3.0}
	got := s.SearchTiers(a, "Solar panels", doc)
	if got.High != "Solar panels Cheap power" || got.Normal != "Solar" || got.Low != "nice ok" {
		t.Errorf("tiers %+v", got)
	}
	if a.SearchExcluded(doc) {
		t.Error("excluded")
	}
	doc["hidden"] = true
	if !a.SearchExcluded(doc) {
		t.Error("not excluded")
	}
}

func TestSearchConfigErrors(t *testing.T) {
	for _, tc := range []struct{ opt, want string }{
		{"{ weights: { nope: high } }", "not a field of the type"},
		{"{ weights: { views: high } }", "no search text"},
		{"{ weights: { title: heavy } }", "must be high, normal or low"},
		{"{ weights: [title] }", "weights must map"},
		{"{ exclude: views }", "must be a boolean field"},
		{"{ exclude: nope }", "must be a boolean field"},
		{"{ exclude: '' }", "exclude must name"},
		{"{ boost: 2 }", "unknown key"},
		{"{ enabled: 1 }", "enabled must be"},
	} {
		_, err := compileSearch(t, "types:\n  a:\n    search: "+tc.opt+"\n    fields: { title: { type: string }, views: { type: number } }\n")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.opt, err, tc.want)
		}
	}
}

func TestSearchConfigChangesHash(t *testing.T) {
	a, err := compileSearch(t, weightedSrc)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compileSearch(t, strings.Replace(weightedSrc, "summary: high", "summary: low", 1))
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash() == b.Hash() {
		t.Error("a weight change keeps the schema hash, so the projections would not be rebuilt")
	}
}
