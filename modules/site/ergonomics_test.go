package site

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
)

type getProbe struct {
	Name string `json:"name"`
	Sub  *getProbe
	Tags []string
}

func TestGetAppendTernary(t *testing.T) {
	data := map[string]any{
		"f":    map[string]any{"seo": map[string]any{"image": map[string]any{"alt": "A"}}, "list": []any{map[string]any{"v": 1.0}, "x"}, "nil": nil},
		"it":   &Item{ID: "i1", Fields: map[string]any{"title": "T"}},
		"p":    &getProbe{Name: "n", Sub: &getProbe{Name: "m", Tags: []string{"a", "b"}}},
		"nilp": (*getProbe)(nil),
		"l":    []any{"a", "b", "c"},
	}
	for tpl, want := range map[string]string{
		`{{get "seo.image.alt" .f}} [{{get "seo.image.src" .f}}] [{{get "seo.image.alt.x" .f}}] [{{get "nil.a.b" .f}}]`: "A [] [] []",
		`{{get "list.0.v" .f}} {{get "list.1" .f}} [{{get "list.2" .f}}] [{{get "list.-1" .f}}] [{{get "list.x" .f}}]`:  "1 x [] [] []",
		`{{get .f "seo.image.alt"}} {{.f | get "seo.image.alt"}}`:                                                       "A A",
		`{{get "Fields.title" .it}} {{get "id" .it}} {{get "Sub.Sub.Name" .p}} [{{get "Sub.Name" .nilp}}]`:              "T i1  []",
		`{{get "name" .p}} {{get "Sub.name" .p}} {{get "Sub.Tags.1" .p}} [{{get "a" .missing}}] [{{get "a" nil}}]`:      "n m b [] []",
		`{{range $i, $e := .l}}{{$e}}{{if not (isLast $i $.l)}},{{end}}{{end}} {{isLast 0 (list 1)}} {{isLast 0 nil}}`:  "a,b,c true false",
		`{{range append "d" .l}}{{.}}{{end}} {{len .l}} {{len (append 1 2 nil)}} {{join "-" (append "x" "y" .l)}}`:      "abcd 3 2 a-b-c-x-y",
		`{{ternary "a" "b" true}} {{ternary "a" "b" ""}} {{.f.nil | ternary "y" "n"}} {{ternary 1 2 .l}}`:               "a b n 1",
	} {
		got, err := run(t, tpl, data)
		if err != nil || got != want {
			t.Errorf("%s\n got %q (%v)\nwant %q", tpl, got, err, want)
		}
	}
	for _, tpl := range []string{`{{append 1}}`, `{{append 1 "s"}}`} {
		if got, err := run(t, tpl, nil); err == nil {
			t.Errorf("%s = %q, want an error", tpl, got)
		}
	}
}

func TestGetInBothModesAndNewlines(t *testing.T) {
	x := newExtensions()
	chain, _ := x.chain("starter")
	dir := t.TempDir()
	writeFile(t, dir, "templates/partials/url.html", "/a/{{.Slug}}\n")
	writeFile(t, dir, "templates/partials/m.html", `[{{get "nothing" .}}][{{get "a.b" .}}]<a href="{{partial "partials/url.html" (dict "Slug" "x")}}">`)
	for _, strict := range []bool{true, false} {
		e, err := loadEngine(&sources{chain: chain, dir: dir}, strict, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := &page{eng: e, ch: &channel.Channel{Name: "t"}, locale: "en", def: "en", strict: strict, links: map[string]string{},
			view: &View{Locale: "en", Fields: map[string]any{}, Params: map[string]string{}}}
		p.t, _ = e.blockSet(p)
		var sb strings.Builder
		err = p.t.ExecuteTemplate(&sb, "partials/m.html", map[string]any{})
		if err != nil || sb.String() != `[][]<a href="/a/x">` {
			t.Errorf("strict=%v: %q %v", strict, sb.String(), err)
		}
	}
}

func TestFollowSymlinks(t *testing.T) {
	x := newExtensions()
	chain, _ := x.chain("starter")
	shared := t.TempDir()
	writeFile(t, shared, "card.html", "card")
	dir := t.TempDir()
	writeFile(t, dir, "templates/layout.html", "x")
	if err := os.Symlink(shared, filepath.Join(dir, "templates", "partials")); err != nil {
		t.Skip(err)
	}
	if err := os.Symlink(filepath.Join(dir, "templates"), filepath.Join(shared, "up")); err != nil {
		t.Fatal(err)
	}
	e, err := loadEngine(&sources{chain: chain, dir: dir}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.base.Lookup("partials/card.html") == nil {
		t.Errorf("symlinked directory not followed: %v", e.names)
	}
	for _, n := range e.names {
		if strings.Contains(n, "up/") {
			t.Errorf("cycle followed: %s", n)
		}
	}
	before := dirSignature(dir)
	writeFile(t, shared, "card.html", "changed")
	if dirSignature(dir) == before {
		t.Error("a change behind a symlink does not change the signature")
	}
}
