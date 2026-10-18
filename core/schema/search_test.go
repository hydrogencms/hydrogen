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

func TestSearchOptionMustBeBool(t *testing.T) {
	_, err := compileSearch(t, "types:\n  a: { search: 'yes', fields: { t: { type: string } } }\n")
	if err == nil || !strings.Contains(err.Error(), "must be true or false") {
		t.Fatalf("err = %v", err)
	}
}
