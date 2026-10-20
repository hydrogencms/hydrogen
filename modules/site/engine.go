package site

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
)

// helpers are the template functions that need the page being rendered; parsing needs their names, rendering
// a clone of the set with the real ones.
var helpers = []string{"blocks", "partial", "hasTemplate", "items", "item", "query", "richtext", "link", "t", "meta", "nav", "breadcrumbs", "asset", "edit", "setting", "component"}

// engine is the parsed template set: the shared files (layout, partials, blocks) in one set and a clone of
// it with each page-like template parsed on top.
type engine struct {
	base    *template.Template
	pages   map[string]*template.Template // pages/*, collections/*, 404.html
	names   []string                      // every template file, sorted
	trans   translations
	assets  *assetSet
	sig     string
	strict  bool
	sources map[string]string
	comps   map[string]ComponentFunc
}

// sources are what an engine loads: the theme chain (base first), the project's directory and the registered
// components.
type sources struct {
	chain []Theme
	dir   string
	comps map[string]ComponentFunc
	funcs template.FuncMap
}

func isPageTemplate(name string) bool {
	return strings.HasPrefix(name, "pages/") || strings.HasPrefix(name, "collections/") || strings.HasPrefix(name, "routes/") || name == "404.html"
}

func loadEngine(src *sources, strict bool) (*engine, error) {
	list, err := files(layers(src.chain, src.dir, "templates"), ".html")
	if err != nil {
		return nil, err
	}
	e := &engine{pages: map[string]*template.Template{}, strict: strict, sources: map[string]string{}, comps: src.comps}
	stubs := template.FuncMap{}
	for _, h := range helpers {
		stubs[h] = func(...any) string { return "" }
	}
	e.base = template.New("").Funcs(builtinFuncs()).Funcs(src.funcs).Funcs(stubs)
	if strict {
		e.base.Option("missingkey=error")
	}
	type pageFile struct{ name, src string }
	var pageFiles []pageFile
	for _, f := range list {
		b, err := fs.ReadFile(f.fsys, f.name)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", f.where, err)
		}
		e.names = append(e.names, f.name)
		e.sources[f.name] = f.where
		if isPageTemplate(f.name) {
			pageFiles = append(pageFiles, pageFile{f.name, string(b)})
			continue
		}
		if _, err := e.base.New(f.name).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("template %s: %w", f.where, err)
		}
	}
	for _, f := range pageFiles {
		c, err := e.base.Clone()
		if err != nil {
			return nil, err
		}
		if _, err := c.New(f.name).Parse(f.src); err != nil {
			return nil, fmt.Errorf("template %s: %w", e.sources[f.name], err)
		}
		e.pages[f.name] = c
	}
	if e.assets, err = loadAssets(src.chain, src.dir); err != nil {
		return nil, err
	}
	if e.trans, err = loadTranslations(src.chain, src.dir); err != nil {
		return nil, err
	}
	e.sig = dirSignature(src.dir)
	return e, nil
}

func (e *engine) has(name string) bool {
	if e.comps[name] != nil {
		return true
	}
	if isPageTemplate(name) {
		return e.pages[name] != nil
	}
	return e.base.Lookup(name) != nil
}

func (e *engine) hasPage(name string) bool { return e.pages[name] != nil || e.comps[name] != nil }

// pageTemplate picks pages/<type>.html, else the fallback pages/default.html.
func (e *engine) pageTemplate(typ string) (string, bool) {
	for _, n := range []string{"pages/" + typ + ".html", "pages/default.html"} {
		if e.hasPage(n) {
			return n, true
		}
	}
	return "", false
}

// itemTemplate is for mounted items: collections/<type>.html, then the page templates.
func (e *engine) itemTemplate(typ string) (string, bool) {
	if n := "collections/" + typ + ".html"; e.hasPage(n) {
		return n, true
	}
	return e.pageTemplate(typ)
}

// render executes layout.html in the set of a page-like template with the page's helpers.
func (e *engine) render(name string, p *page, v *View) ([]byte, error) {
	v.p = p
	if f := e.comps[name]; f != nil {
		return e.renderComponent(name, f, p, v)
	}
	set := e.pages[name]
	if set == nil {
		return nil, fmt.Errorf("missing template %s", name)
	}
	t, err := set.Clone()
	if err != nil {
		return nil, err
	}
	p.t = t
	t.Funcs(p.funcs())
	if t.Lookup("layout.html") == nil {
		return nil, fmt.Errorf("missing template layout.html")
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", v); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// renderBlock executes one block template on its own.
func (e *engine) renderBlock(b *BlockView, p *page) ([]byte, error) {
	t, err := e.base.Clone()
	if err != nil {
		return nil, err
	}
	p.t = t
	p.view.p = p
	t.Funcs(p.funcs())
	var buf bytes.Buffer
	if err := p.blockInto(&buf, b); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// renderComponent runs a registered component for the page being rendered.
func (e *engine) renderComponent(name string, f ComponentFunc, p *page, v *View) ([]byte, error) {
	c := f(v)
	if c == nil {
		return nil, fmt.Errorf("component %s returned no renderer", name)
	}
	var buf bytes.Buffer
	if err := c.Render(p.context(), &buf); err != nil {
		return nil, fmt.Errorf("component %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// blockSet is the shared template set for helpers called outside a template execution (components).
func (e *engine) blockSet(p *page) (*template.Template, error) {
	t, err := e.base.Clone()
	if err != nil {
		return nil, err
	}
	t.Funcs(p.funcs())
	return t, nil
}
