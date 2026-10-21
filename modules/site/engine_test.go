package site

import (
	"errors"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
)

func TestTemplatesParseFileByFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "templates/layout.html", `<body>{{block "main" .}}{{end}}</body>`)
	writeFile(t, dir, "templates/partials/p.html", `good partial`)
	writeFile(t, dir, "templates/pages/default.html", `{{define "main"}}default{{end}}`)
	writeFile(t, dir, "templates/pages/post.html", `{{define "main"}}post: {{partial "partials/p.html"}}{{end}}`)
	writeFile(t, dir, "templates/pages/bad.html", `{{define "main"}}bad v1{{end}}`)
	src := &sources{dir: dir}
	ch := &channel.Channel{Name: "t", Languages: []string{"en"}}
	render := func(e *engine, name string) (string, error) {
		v := &View{Locale: "en", Fields: map[string]any{}, Params: map[string]string{}, Channel: ChannelView{Home: "/"}}
		p := &page{eng: e, ch: ch, locale: "en", def: "en", view: v, links: map[string]string{}}
		out, err := e.render(name, p, v)
		return string(out), err
	}
	good, err := loadEngine(src, false, nil)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, dir, "templates/partials/p.html", "ok\n{{nope}}")
	writeFile(t, dir, "templates/pages/bad.html", `{{define "main"}}{{if}}{{end}}`)
	e, err := loadEngine(src, false, nil)
	if e == nil || err == nil {
		t.Fatalf("broken templates: engine %v, err %v", e, err)
	}
	for _, want := range []string{"partials/p.html:2", "function \"nope\" not defined", "pages/bad.html:1", "site:check"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q:\n%v", want, err)
		}
	}
	if out, err := render(e, "pages/default.html"); err != nil || out != "<body>default</body>" {
		t.Errorf("a page that uses no broken template = %q %v", out, err)
	}
	_, err = render(e, "pages/post.html")
	if pe := (*parseError)(nil); !errors.As(err, &pe) || !strings.Contains(pe.Error(), "partials/p.html:2") {
		t.Errorf("a page that uses the broken partial: %v", err)
	}
	// a page that does not parse fails rather than falling back on pages/default.html
	if name, _ := e.pageTemplate("bad"); name != "pages/bad.html" {
		t.Errorf("bad page resolves to %q", name)
	}
	if _, err := render(e, "pages/bad.html"); err == nil || !strings.Contains(err.Error(), "pages/bad.html:1") {
		t.Errorf("the broken page: %v", err)
	}

	// a reload keeps each broken file's last good version and still reports it
	e, err = loadEngine(src, false, good)
	if err == nil || len(e.errs) != 2 {
		t.Fatalf("reload: %v %v", err, e.errs)
	}
	if out, err := render(e, "pages/post.html"); err != nil || out != "<body>post: good partial</body>" {
		t.Errorf("post with the last good partial = %q %v", out, err)
	}
	if out, err := render(e, "pages/bad.html"); err != nil || out != "<body>bad v1</body>" {
		t.Errorf("last good page = %q %v", out, err)
	}
	if !strings.Contains(string(injectParseErrors([]byte("<html><body>x</body></html>"), e)), "pages/bad.html:1") {
		t.Error("the dev overlay does not name the file")
	}
	// a fixed file is served again; one still broken keeps the version from before
	writeFile(t, dir, "templates/partials/p.html", `fixed`)
	if e, _ = loadEngine(src, false, e); e == nil || len(e.errs) != 1 {
		t.Fatalf("after a fix: %v", e)
	}
	if out, _ := render(e, "pages/post.html"); out != "<body>post: fixed</body>" {
		t.Errorf("fixed partial = %q", out)
	}
	if out, _ := render(e, "pages/bad.html"); out != "<body>bad v1</body>" {
		t.Errorf("last good page across reloads = %q", out)
	}
}
