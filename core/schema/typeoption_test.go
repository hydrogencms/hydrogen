package schema

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

func childrenCheck(t *TypeBuilder, v any) error {
	list, ok := v.([]any)
	if !ok {
		return fmt.Errorf("must be a list of page type names")
	}
	if !t.Page() {
		return fmt.Errorf("is only valid on page types")
	}
	for _, e := range list {
		name, _ := e.(string)
		c, ok := t.Type(name)
		if !ok || !c.Page() {
			return fmt.Errorf("%v is not a page type", e)
		}
	}
	return nil
}

func optBuilder(t *testing.T, yamlSrc string) (*Builder, error) {
	t.Helper()
	reg := NewRegistry()
	if err := reg.TypeOption("children", childrenCheck); err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(reg)
	return b, b.LoadFS(fstest.MapFS{"s.yaml": {Data: []byte(yamlSrc)}}, ".")
}

func TestTypeOptionRegistration(t *testing.T) {
	reg := NewRegistry()
	chk := func(*TypeBuilder, any) error { return nil }
	for _, bad := range []string{"fields", "page", "Bad", "", "1x"} {
		if err := reg.TypeOption(bad, chk); err == nil {
			t.Errorf("%q registered", bad)
		}
	}
	if err := reg.TypeOption("ok", nil); err == nil {
		t.Error("nil check accepted")
	}
	if err := reg.TypeOption("ok", chk); err != nil {
		t.Fatal(err)
	}
	if err := reg.TypeOption("ok", chk); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("duplicate: %v", err)
	}
}

const pagesYAML = `
types:
  home:
    page: true
    children: [article]
    fields: { title: { type: string } }
  article:
    page: true
    fields: { title: { type: string } }
`

func TestTypeOptionYAML(t *testing.T) {
	b, err := optBuilder(t, pagesYAML)
	if err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := s.Type("home")
	if got := home.Options["children"]; fmt.Sprint(got) != "[article]" {
		t.Errorf("options = %v", home.Options)
	}
	if a, _ := s.Type("article"); a.Options != nil {
		t.Errorf("article options = %v", a.Options)
	}
	if !strings.Contains(string(mustJSON(t, s)), `"options":{"children":["article"]}`) {
		t.Errorf("canonical json lacks options: %s", mustJSON(t, s))
	}
}

func mustJSON(t *testing.T, s *Schema) []byte {
	t.Helper()
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestTypeOptionCheckError(t *testing.T) {
	tests := map[string]string{
		"s.yaml:4: type \"home\": option \"children\": ghost is not a page type":     "types:\n  home:\n    page: true\n    children: [ghost]\n",
		"s.yaml:3: type \"plain\": option \"children\": is only valid on page types": "types:\n  plain:\n    children: []\n",
	}
	for want, src := range tests {
		b, err := optBuilder(t, src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Compile(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

func TestTypeOptionUnknownKeyStillFails(t *testing.T) {
	b, _ := optBuilder(t, "types:\n  a:\n    nope: 1\n")
	_, err := b.Compile()
	if err == nil || !strings.Contains(err.Error(), `s.yaml:3: unknown key "nope"`) || !strings.Contains(err.Error(), "children") {
		t.Errorf("err = %v", err)
	}
	// Without registration the option key is unknown.
	b = NewBuilder(NewRegistry())
	_ = b.LoadFS(fstest.MapFS{"s.yaml": {Data: []byte(pagesYAML)}}, ".")
	if _, err := b.Compile(); err == nil || !strings.Contains(err.Error(), `unknown key "children"`) {
		t.Errorf("err = %v", err)
	}
}

func TestTypeOptionHash(t *testing.T) {
	hash := func(src string) string {
		b, err := optBuilder(t, src)
		if err != nil {
			t.Fatal(err)
		}
		s, err := b.Compile()
		if err != nil {
			t.Fatal(err)
		}
		return s.Hash()
	}
	a := hash(pagesYAML)
	b := hash(strings.Replace(pagesYAML, "[article]", "[article, home]", 1))
	c := hash(strings.Replace(pagesYAML, "    children: [article]\n", "", 1))
	if a == b || a == c || b == c {
		t.Error("hash ignores option values")
	}
	if a != hash(pagesYAML) {
		t.Error("hash not deterministic")
	}
}

func TestTypeBuilderSetOption(t *testing.T) {
	reg := NewRegistry()
	if err := reg.TypeOption("children", childrenCheck); err != nil {
		t.Fatal(err)
	}
	b := NewBuilder(reg)
	b.SetOrigin("module pages")
	for _, n := range []string{"home", "article"} {
		if err := b.AddType(n, Spec{"page": true}); err != nil {
			t.Fatal(err)
		}
	}
	home, _ := b.Type("home")
	if err := home.SetOption("nope", 1); err == nil {
		t.Error("unregistered option accepted")
	}
	if err := home.SetOption("children", []string{"article"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := home.Option("children"); !ok || fmt.Sprint(v) != "[article]" {
		t.Errorf("Option = %v %v", v, ok)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if ty, _ := s.Type("home"); fmt.Sprint(ty.Options["children"]) != "[article]" {
		t.Errorf("options = %v", ty.Options)
	}
	if err := home.SetOption("children", []string{"ghost"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Compile(); err == nil || !strings.Contains(err.Error(), "module pages") {
		t.Errorf("err = %v", err)
	}
}
