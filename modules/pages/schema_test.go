package pages

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/core/schema"
)

func compileWith(t *testing.T, yaml string, sites ...string) (*schema.Schema, error) {
	t.Helper()
	m := &module{cfg: Config{Sites: sites}}
	reg := schema.NewRegistry()
	if err := reg.TypeOption(childrenOption, checkChildren); err != nil {
		t.Fatal(err)
	}
	b := schema.NewBuilder(reg)
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(yaml)}}, "schema"); err != nil {
		return nil, err
	}
	if err := m.Schema(b); err != nil {
		return nil, err
	}
	return b.Compile()
}

func TestSchemaTransform(t *testing.T) {
	s, err := compileWith(t, `
types:
  landing:
    page: true
    fields:
      title: { type: string, translatable: true }
  custom:
    page: true
    fields:
      slug: { type: slug, translatable: true }
      heading: { type: string }
  article:
    fields:
      title: { type: string }
`, "main", "shop")
	if err != nil {
		t.Fatal(err)
	}
	landing, _ := s.Type("landing")
	slug, ok := landing.Field("slug")
	if !ok || !slug.Translatable {
		t.Fatalf("landing.slug = %+v", slug)
	}
	if from, ok := s.Option(slug, "from"); !ok || from != "title" {
		t.Errorf("slug from = %v, %v", from, ok)
	}
	if v, ok := landing.Field("vanityPath"); !ok || !v.Translatable {
		t.Errorf("landing.vanityPath = %+v", v)
	}
	custom, _ := s.Type("custom")
	if len(custom.Fields) != 3 { // its own slug is kept; vanityPath is added
		t.Errorf("custom fields = %d", len(custom.Fields))
	}
	article, _ := s.Type("article")
	if _, ok := article.Field("slug"); ok {
		t.Error("collection types are left alone")
	}
	red, ok := s.Type("redirect")
	if !ok || red.Page {
		t.Fatalf("redirect type = %+v", red)
	}
	var names []string
	for _, f := range red.Fields {
		names = append(names, f.Name)
	}
	if got := strings.Join(names, ","); got != "from,to,statusCode,site,forLocale,force" {
		t.Errorf("redirect fields = %s", got)
	}
}

func TestSchemaRedirectClash(t *testing.T) {
	if _, err := compileWith(t, "types:\n  redirect:\n    fields:\n      x: { type: string }\n", "main"); err == nil {
		t.Error("a project type named redirect must be refused")
	}
}

func TestChildrenOption(t *testing.T) {
	ok := "types:\n  a: { page: true, children: [b, a], fields: { t: { type: string } } }\n  b: { page: true, children: [], fields: { t: { type: string } } }\n"
	if _, err := compileWith(t, ok, "main"); err != nil {
		t.Fatal(err)
	}
	for name, yaml := range map[string]string{
		"not a page type": "types:\n  a: { page: true, children: [c], fields: { t: { type: string } } }\n  c: { fields: { t: { type: string } } }\n",
		"unknown type":    "types:\n  a: { page: true, children: [nope], fields: { t: { type: string } } }\n",
		"on a collection": "types:\n  a: { children: [a], fields: { t: { type: string } } }\n",
		"not a list":      "types:\n  a: { page: true, children: a, fields: { t: { type: string } } }\n",
		"not names":       "types:\n  a: { page: true, children: [1], fields: { t: { type: string } } }\n",
	} {
		if _, err := compileWith(t, yaml, "main"); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	s, _ := compileWith(t, ok, "main")
	a, _ := s.Type("a")
	if got, _ := a.Options[childrenOption].([]any); len(got) != 2 {
		t.Errorf("options = %v", a.Options)
	}
}

func TestConfigValidate(t *testing.T) {
	for sites, ok := range map[string]bool{"main": true, "main,shop": true, "": false, "Main": false, "a b": false, "1a": false, "a,a": false, "a-1": true} {
		var list []string
		if sites != "" {
			list = strings.Split(sites, ",")
		}
		c := Config{Sites: list}
		if err := c.validate(); (err == nil) != ok {
			t.Errorf("sites %q: %v", sites, err)
		}
	}
}
