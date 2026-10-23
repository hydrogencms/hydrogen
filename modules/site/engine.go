package site

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"strconv"
	"strings"
)

// helpers are the template functions that need the page being rendered; parsing needs their names, rendering
// a clone of the set with the real ones.
var helpers = []string{"blocks", "partial", "hasTemplate", "items", "itemsPage", "item", "itemBlock", "query", "richtext", "link", "t", "meta", "nav", "breadcrumbs", "asset", "edit", "setting", "component"}

// engine is the parsed template set: the shared files (layout, partials, blocks) in one set and a clone of
// it with each page-like template parsed on top.
type engine struct {
	base    *template.Template
	pages   map[string]*template.Template // pages/*, collections/*, 404.html
	names   []string                      // every template file, sorted
	rich    map[string]bool               // node and mark types with a richtext/<type>.html override; nil when none
	trans   translations
	assets  *assetSet
	sig     uint64
	strict  bool
	sources map[string]string
	comps   map[string]ComponentFunc
	// errs are the files that do not parse. A shared one is replaced by its last good text (prev's, on a dev
	// reload) or by a template that fails with the error, so only the pages that use it fail.
	errs map[string]error
	good map[string]string // the text each file was parsed from
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

// parseErrorFunc is what a shared file that does not parse is replaced by: it fails with the parse error.
const parseErrorFunc = "hydrogenParseError"

// loadEngine parses the templates file by file. It returns no engine when the files cannot be read; with
// files that do not parse it returns the engine without them and an error naming each one. prev, when set,
// gives the last good text of a file that no longer parses.
func loadEngine(src *sources, strict bool, prev *engine) (*engine, error) {
	list, err := files(layers(src.chain, src.dir, "templates"), ".html")
	if err != nil {
		return nil, err
	}
	e := &engine{pages: map[string]*template.Template{}, strict: strict, sources: map[string]string{}, comps: src.comps,
		errs: map[string]error{}, good: map[string]string{}}
	stubs := template.FuncMap{}
	for _, h := range helpers {
		stubs[h] = func(...any) string { return "" }
	}
	broken := template.FuncMap{parseErrorFunc: func(name string) (string, error) { return "", e.errs[name] }}
	e.base = template.New("").Funcs(builtinFuncs()).Funcs(src.funcs).Funcs(stubs).Funcs(broken)
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
		// A failed Parse adds nothing to the set but an empty entry for the name, which the next Parse replaces.
		if e.parse(e.base, f.name, string(b), prev) {
			continue
		}
		if _, err := e.base.New(f.name).Parse("{{" + parseErrorFunc + " " + strconv.Quote(f.name) + "}}"); err != nil {
			return nil, err
		}
	}
	for _, f := range pageFiles {
		c, err := e.base.Clone()
		if err != nil {
			return nil, err
		}
		if e.parse(c, f.name, f.src, prev) {
			e.pages[f.name] = c
		}
	}
	if e.assets, err = loadAssets(src.chain, src.dir); err != nil {
		return nil, err
	}
	if e.trans, err = loadTranslations(src.chain, src.dir); err != nil {
		return nil, err
	}
	for _, n := range e.names {
		if t, ok := strings.CutPrefix(n, "richtext/"); ok {
			if t, ok = strings.CutSuffix(t, ".html"); ok && t != "" && !strings.Contains(t, "/") {
				if e.rich == nil {
					e.rich = map[string]bool{}
				}
				e.rich[t] = true
			}
		}
	}
	e.sig = dirSignature(src.dir)
	return e, e.parseErr()
}

// parse adds a file to a set; when it does not parse, it records the error and falls back on prev's text.
func (e *engine) parse(set *template.Template, name, text string, prev *engine) bool {
	_, err := set.New(name).Parse(text)
	if err == nil {
		e.good[name] = text
		return true
	}
	e.errs[name] = &parseError{fmt.Errorf("template %s: %w", e.sources[name], err)}
	if old, ok := prev.lastGood(name); ok && old != text {
		if _, err := set.New(name).Parse(old); err == nil {
			e.good[name] = old
			return true
		}
	}
	return false
}

// parseError is a file that does not parse; dev pages that fail with one show it.
type parseError struct{ err error }

func (p *parseError) Error() string { return p.err.Error() }
func (p *parseError) Unwrap() error { return p.err }

func (e *engine) lastGood(name string) (string, bool) {
	if e == nil {
		return "", false
	}
	s, ok := e.good[name]
	return s, ok
}

// parseErr names every file that does not parse, sorted; nil when all do.
func (e *engine) parseErr() error {
	if len(e.errs) == 0 {
		return nil
	}
	msgs := make([]string, 0, len(e.errs))
	for _, n := range sortedKeys(e.errs) {
		msgs = append(msgs, e.errs[n].Error())
	}
	return fmt.Errorf("%d template(s) do not parse (hydrogen site:check reports every problem):\n%s", len(msgs), strings.Join(msgs, "\n"))
}

func (e *engine) has(name string) bool {
	if e.comps[name] != nil {
		return true
	}
	if isPageTemplate(name) {
		return e.hasPage(name)
	}
	return e.base.Lookup(name) != nil
}

// hasPage includes a page that does not parse, so that rendering it fails rather than falls back.
func (e *engine) hasPage(name string) bool {
	return e.pages[name] != nil || e.errs[name] != nil || e.comps[name] != nil
}

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
	t, err := e.pageSet(name, p)
	if err != nil {
		return nil, err
	}
	if t.Lookup("layout.html") == nil {
		return nil, fmt.Errorf("missing template layout.html")
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.html", v); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// renderPartial executes a template or component of the set on its own, without the layout, with the page's
// context and helpers.
func (e *engine) renderPartial(name string, p *page, v *View) ([]byte, error) {
	v.p = p
	if f := e.comps[name]; f != nil {
		return e.renderComponent(name, f, p, v)
	}
	var t *template.Template
	var err error
	if isPageTemplate(name) {
		t, err = e.pageSet(name, p)
	} else if t, err = e.blockSet(p); err == nil {
		p.t = t
	}
	if err != nil {
		return nil, err
	}
	if t.Lookup(name) == nil {
		return nil, fmt.Errorf("missing template %s", name)
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, v); err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

// pageSet is a clone of a page-like template's set with the page's helpers, set as the page's templates.
func (e *engine) pageSet(name string, p *page) (*template.Template, error) {
	set := e.pages[name]
	if set == nil && e.errs[name] != nil {
		return nil, e.errs[name]
	}
	if set == nil {
		return nil, fmt.Errorf("missing template %s", name)
	}
	t, err := set.Clone()
	if err != nil {
		return nil, err
	}
	p.t = t
	t.Funcs(p.funcs())
	return t, nil
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
