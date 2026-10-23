package site

import (
	"html/template"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
)

func TestViewFuncsAndQueryWithoutRequest(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "templates/layout.html", `<body>{{.Query "a"}}|{{len (.QueryValues "a")}}|{{here}}|{{count}}|{{blocks (list (dict "type" "n" "props" (dict)))}}</body>`)
	writeFile(t, dir, "templates/blocks/n.html", `{{.Page.Path}}`)
	writeFile(t, dir, "templates/pages/default.html", ``)
	ext := newExtensions()
	err := ext.AddViewFuncs(func(v *View) template.FuncMap {
		return template.FuncMap{
			"here":  func() string { return v.Path },
			"count": func() (int, error) { items, err := v.List("post", nil); return len(items), err },
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	e, err := loadEngine(&sources{dir: dir, view: ext.view, viewNames: ext.viewNames}, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	v := &View{Locale: "en", Path: "/x", Fields: map[string]any{}, Params: map[string]string{}, Channel: ChannelView{Home: "/"}}
	p := &page{eng: e, ch: &channel.Channel{Name: "t", Languages: []string{"en"}}, locale: "en", def: "en", view: v, links: map[string]string{}, strict: true}
	out, err := e.render("pages/default.html", p, v)
	if err != nil || string(out) != "<body>|0|/x|0|/x</body>" {
		t.Fatalf("%q %v", out, err)
	}
	if got := p.queryNames(); len(got) != 1 || got[0] != "a" {
		t.Errorf("read = %v", got)
	}
	if err := ext.AddViewFuncs(func(*View) template.FuncMap { return template.FuncMap{"here": func() string { return "" }} }); err == nil {
		t.Error("a duplicate name was accepted")
	}
	if err := ext.AddFuncs(template.FuncMap{"count": func() int { return 1 }}); err == nil {
		t.Error("a name taken by a view function was accepted")
	}
}
