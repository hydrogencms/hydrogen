package site

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/schema"
)

func (m *module) runCheck(ctx context.Context, app *kernel.App, args []string) error {
	return m.check(ctx, app.Services(), os.Stdout, args)
}

// check implements site:check: it parses every template and renders each page type, collection route and
// block type of the rendered channels against sample content generated from the schema (every field present,
// every allowed block used) with undefined keys as errors. With -db it also requests every published page and
// collection item through the module's handler.
func (m *module) check(ctx context.Context, svc *kernel.Services, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("site:check", flag.ContinueOnError)
	fs.SetOutput(out)
	only := fs.String("channel", "", "check one channel (default: every channel with render: go)")
	withDB := fs.Bool("db", false, "also render every published page and collection item of the database")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("usage: site:check [-channel name] [-db]")
	}
	var chans []*channel.Channel
	for _, ch := range svc.Channels.All() {
		if renders(ch) && (*only == "" || ch.Name == *only) {
			chans = append(chans, ch)
		}
	}
	if len(chans) == 0 {
		return fmt.Errorf("site:check: no channel sets render: go%s", map[bool]string{true: " with that name"}[*only != ""])
	}
	routes, err := parseRoutes(m.cfg.Routes)
	if err != nil {
		return err
	}
	c := &checker{m: m, svc: svc, out: out, routes: routes, seen: map[string]bool{}, g: &gen{s: svc.Schema}}
	if err := c.checkRoutes(); err != nil {
		c.fail("%v", err)
	}
	chain, err := m.ext.chain(m.cfg.Theme)
	if err != nil {
		return err
	}
	c.src = &sources{chain: chain, dir: m.cfg.Dir, comps: m.ext.comps, funcs: m.ext.funcs, view: m.ext.view, viewNames: m.ext.viewNames}
	if c.eng, err = loadEngine(c.src, true, nil); c.eng == nil {
		c.fail("%v", err)
		return c.result()
	}
	if len(c.eng.errs) > 0 {
		for _, n := range sortedKeys(c.eng.errs) {
			c.fail("%v", c.eng.errs[n])
		}
		return c.result()
	}
	fmt.Fprintf(out, "templates: %d parsed\n", len(c.eng.names))
	c.required()
	c.blocks(chans[0])
	c.richtext(chans[0])
	for _, ch := range chans {
		c.channel(ch)
	}
	if *withDB {
		c.database(ctx, chans)
	}
	return c.result()
}

type checker struct {
	m      *module
	svc    *kernel.Services
	out    io.Writer
	eng    *engine
	src    *sources
	routes []*route
	g      *gen
	seen   map[string]bool
	probs  []string
	ok     int
}

func (c *checker) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if c.seen[msg] {
		return
	}
	c.seen[msg] = true
	c.probs = append(c.probs, msg)
}

func (c *checker) pass() { c.ok++ }

func (c *checker) result() error {
	if len(c.probs) > 0 {
		return fmt.Errorf("site:check: %d problem(s):\n  - %s", len(c.probs), strings.Join(c.probs, "\n  - "))
	}
	fmt.Fprintf(c.out, "site ok: %d renders\n", c.ok)
	return nil
}

func (c *checker) checkRoutes() error {
	return checkRoutes(c.routes, c.svc.Schema)
}

func (c *checker) required() {
	for _, n := range []string{"layout.html", "404.html"} {
		if !c.eng.has(n) {
			c.fail("missing template %s", n)
		}
	}
}

// richtext renders each richtext/<type>.html override over a sample document that holds that node or mark.
func (c *checker) richtext(ch *channel.Channel) {
	rt := c.svc.Schema.RichText()
	for _, typ := range sortedKeys(c.eng.rich) {
		var doc map[string]any
		if builtinType(typ) {
			doc = richSampleDoc()
		} else if n, ok := rt.Node(typ); ok {
			doc = map[string]any{"type": "doc", "content": []any{customSample(n.Name, n.Attrs, n.Content)}}
		} else if m, ok := rt.Mark(typ); ok {
			doc = map[string]any{"type": "doc", "content": []any{map[string]any{"type": "paragraph", "content": []any{
				map[string]any{"type": "text", "text": "Sample", "marks": []any{map[string]any{"type": m.Name, "attrs": sampleAttrs(m.Attrs)}}}}}}}
		} else {
			c.fail("richtext/%s.html: %s is neither a built-in nor a declared rich text node or mark", typ, typ)
			continue
		}
		p := c.fakePage(ch, nil, false)
		if _, err := p.richText(doc, RichTextOptions{}); err != nil {
			c.fail("richtext/%s.html: %v", typ, err)
			continue
		}
		c.pass()
	}
}

// customSample is a node of a declared type with sample attributes and, unless it is a leaf, a paragraph.
func customSample(name string, attrs []*schema.RichAttr, content string) map[string]any {
	n := map[string]any{"type": name, "attrs": sampleAttrs(attrs)}
	if content != "none" {
		n["content"] = []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": "Sample"}}}}
	}
	return n
}

func sampleAttrs(list []*schema.RichAttr) map[string]any {
	out := map[string]any{}
	for _, a := range list {
		switch {
		case a.Default != nil:
			out[a.Name] = a.Default
		case a.Type == "enum" && len(a.Values) > 0:
			out[a.Name] = a.Values[0]
		case a.Type == "int":
			out[a.Name] = int64(1)
		case a.Type == "bool":
			out[a.Name] = true
		case a.Type == "url":
			out[a.Name] = "/sample"
		default:
			out[a.Name] = "Sample"
		}
	}
	return out
}

// richSample is a document with one of every built-in node and mark.
func richSampleDoc() map[string]any {
	text := func(s string, marks ...string) map[string]any {
		n := map[string]any{"type": "text", "text": s}
		var ms []any
		for _, m := range marks {
			mk := map[string]any{"type": m}
			if m == "link" {
				mk["attrs"] = map[string]any{"href": "/sample"}
			}
			ms = append(ms, mk)
		}
		if ms != nil {
			n["marks"] = ms
		}
		return n
	}
	para := func(kids ...any) map[string]any { return map[string]any{"type": "paragraph", "content": kids} }
	item := func(kids ...any) map[string]any { return map[string]any{"type": "listItem", "content": kids} }
	cell := func(typ string) map[string]any { return map[string]any{"type": typ, "content": []any{para(text("c"))}} }
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "heading", "attrs": map[string]any{"level": float64(2)}, "content": []any{text("H")}},
		para(text("a", "bold"), text("b", "italic"), text("c", "code"), text("d", "underline"), text("e", "strike"), text("f", "superscript"),
			text("g", "subscript"), text("h", "link"), map[string]any{"type": "hardBreak"}),
		map[string]any{"type": "bulletList", "content": []any{item(para(text("x")))}},
		map[string]any{"type": "orderedList", "content": []any{item(para(text("x")))}},
		map[string]any{"type": "blockquote", "content": []any{para(text("q"))}},
		map[string]any{"type": "codeBlock", "content": []any{text("code")}},
		map[string]any{"type": "horizontalRule"},
		map[string]any{"type": "image", "attrs": map[string]any{"src": "/sample.png", "alt": "s"}},
		map[string]any{"type": "table", "content": []any{
			map[string]any{"type": "tableRow", "content": []any{cell("tableHeader"), cell("tableCell")}}}},
	}}
}

// blocks renders every block type on its own and requires its root to carry data-h-block.
func (c *checker) blocks(ch *channel.Channel) {
	var names []string
	for _, bt := range c.svc.Schema.Blocks() {
		names = append(names, bt.Name)
	}
	for _, name := range names {
		if !c.eng.has("blocks/" + name + ".html") {
			c.fail("missing template blocks/%s.html (block type %s)", name, name)
			continue
		}
		raw := c.g.block(name, 0)
		if raw == nil {
			continue
		}
		p := c.fakePage(ch, c.g.sampleItem(c.pageTypes(ch)[0], ch.Languages[0]), true)
		bv := p.parseBlocks([]any{raw}, "fields/body", false)
		body, err := c.eng.renderBlock(bv[0], p)
		switch {
		case err != nil:
			c.fail("block %s: %v", name, err)
		case !strings.Contains(string(body), `data-h-block="`+name+`"`):
			c.fail("blocks/%s.html: the root element lacks data-h-block (put {{.Attrs}} on it)", name)
		default:
			c.pass()
		}
	}
}

func (c *checker) pageTypes(ch *channel.Channel) []*schema.Type {
	var out []*schema.Type
	for _, t := range c.svc.Schema.Types() {
		if t.Page && ch.AllowsCollection(t.Name) {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		out = append(out, c.svc.Schema.Types()[0])
	}
	return out
}

func (c *checker) channel(ch *channel.Channel) {
	loc := ch.Languages[0]
	for _, t := range c.svc.Schema.Types() {
		if !t.Page || !ch.AllowsCollection(t.Name) {
			continue
		}
		name, ok := c.eng.pageTemplate(t.Name)
		if !ok {
			c.fail("page type %s has no template: add pages/%s.html or pages/default.html", t.Name, t.Name)
			continue
		}
		it := c.g.sampleItem(t, loc)
		c.renderItem(ch, name, it, nil)
	}
	for _, rt := range c.routes {
		if !ch.AllowsCollection(rt.Type) {
			continue
		}
		if !c.eng.has(rt.templateName()) {
			c.fail("route %s: missing template %s", rt.Path, rt.templateName())
			continue
		}
		t, _ := c.svc.Schema.Type(rt.Type)
		if rt.param == "" {
			c.renderItem(ch, rt.templateName(), nil, []*Item{c.g.sampleItem(t, loc), c.g.sampleItem(t, loc)})
			continue
		}
		c.renderItem(ch, rt.templateName(), c.g.sampleItem(t, loc), nil)
	}
	for _, cr := range c.m.ext.routes {
		if !cr.applies(ch) {
			continue
		}
		if cr.sample == nil {
			c.note("custom route %s has no Sample: not rendered (declare one with .Sample)", cr.pattern)
			continue
		}
		c.renderCustom(ch, cr, cr.sample())
	}
	if c.eng.has("404.html") {
		p := c.fakePage(ch, nil, false)
		p.view.Status, p.view.Title = http.StatusNotFound, p.tFn("not_found_title")
		if _, err := c.eng.render("404.html", p, p.view); err != nil {
			c.fail("404.html: %v", err)
		} else {
			c.pass()
		}
	}
}

func (c *checker) note(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !c.seen["note:"+msg] {
		c.seen["note:"+msg] = true
		fmt.Fprintln(c.out, msg)
	}
}

// renderCustom renders a custom route's sample answer normally and in preview.
func (c *checker) renderCustom(ch *channel.Channel, cr *CustomRoute, resp *Response) {
	switch {
	case resp == nil:
		c.fail("custom route %s: Sample returned nil", cr.pattern)
		return
	case len(resp.kinds()) != 1:
		c.fail("custom route %s: the Sample must set exactly one of Template, Partial, Body, JSON, XML, Redirect (has %s)", cr.pattern, strings.Join(resp.kinds(), ", "))
		return
	case resp.Redirect != "":
		c.pass()
		return
	}
	if _, _, ok, err := resp.encode(); ok {
		if err != nil {
			c.fail("custom route %s: %v", cr.pattern, err)
		} else {
			c.pass()
		}
		return
	}
	name, render := resp.Template, c.eng.render
	if resp.Partial != "" {
		name, render = resp.Partial, c.eng.renderPartial
	}
	if !c.eng.has(name) {
		c.fail("custom route %s: template %q does not exist (add site/templates/routes/<name>.html)", cr.pattern, name)
		return
	}
	for _, preview := range []bool{false, true} {
		p := c.fakePage(ch, nil, preview)
		p.view.Data, p.view.Status = resp.Data, max(resp.Status, http.StatusOK)
		if resp.Title != "" {
			p.view.Title = resp.Title
		}
		for _, n := range cr.names {
			p.view.Params[n] = "sample"
		}
		if _, err := render(name, p, p.view); err != nil {
			c.fail("custom route %s: %s: %v", cr.pattern, name, err)
			return
		}
	}
	c.pass()
}

// renderItem renders a template with a sample item (or listing) normally and in preview, and checks that
// every block the item holds shows up marked.
func (c *checker) renderItem(ch *channel.Channel, tpl string, it *Item, items []*Item) {
	for _, preview := range []bool{false, true} {
		p := c.fakePage(ch, it, preview)
		p.view.Items = items
		body, err := c.eng.render(tpl, p, p.view)
		if err != nil {
			c.fail("%s: %v", tpl, err)
			return
		}
		if it == nil {
			continue
		}
		var types []string
		for _, v := range it.Fields {
			if l, ok := v.([]any); ok {
				blockTypesOf(l, &types)
			}
		}
		for _, t := range types {
			if !strings.Contains(string(body), `data-h-block="`+t+`"`) {
				c.fail("%s: block %s was not rendered with data-h-block (does the template call {{blocks \"<field>\"}}?)", tpl, t)
			}
		}
		if preview && len(types) > 0 && !strings.Contains(string(body), "data-h-path=") {
			c.fail("%s: no data-h-path in preview", tpl)
		}
	}
	c.pass()
}

// fakePage is a page over sample data: settings from the singleton types, a fixed navigation, and every
// referenced item linked to a sample URL.
func (c *checker) fakePage(ch *channel.Channel, it *Item, preview bool) *page {
	loc := ch.Languages[0]
	v := &View{Item: it, Fields: map[string]any{}, Title: "Sample", Locale: loc, Path: "/sample", Status: http.StatusOK, Preview: preview,
		Channel:  ChannelView{Name: ch.Name, Languages: ch.Languages, Home: "/"},
		Settings: map[string]map[string]any{}, Params: map[string]string{}}
	if it != nil {
		v.Fields, v.Title = it.Fields, itemTitle(it)
	}
	p := &page{eng: c.eng, ch: ch, locale: loc, def: c.svc.Core.Content.DefaultLocale, preview: preview, strict: true, view: v, rt: c.svc.Schema.RichText(),
		links: map[string]string{}, canon: "https://example.com/sample", crumbs: []Crumb{{Name: "Home", URL: "/"}, {Name: "Sample", URL: "/sample"}}}
	for _, t := range c.svc.Schema.Types() {
		if t.Singleton && ch.AllowsCollection(t.Name) {
			v.Settings[t.Name] = c.g.fields(t.Fields)
		}
	}
	p.siteName, _ = p.settingFn("siteName").(string)
	if it != nil {
		var ids []string
		collectIDs(it.Fields, &ids)
		for _, id := range ids {
			p.links[id] = "/sample"
		}
	}
	p.loadNav = func() ([]*NavItem, error) {
		return []*NavItem{{Name: "Home", URL: "/"}, {Name: "Sample", URL: "/sample", Active: true, Open: true,
			Children: []*NavItem{{Name: "Child", URL: "/sample/child"}}}}, nil
	}
	return p
}

// database requests every published page and collection item through the handler: what the site would
// serve, templates and content together.
func (c *checker) database(ctx context.Context, chans []*channel.Channel) {
	cfg := c.m.cfg
	cfg.CacheBytes = 0
	srv, err := newServer(&cfg, c.m.ext, c.svc, c.m.pages)
	if err != nil {
		c.fail("%v", err)
		return
	}
	srv.dev = false
	if srv.eng.Load() == nil {
		return
	}
	for _, ch := range chans {
		f := srv.fetcher(mustRequest(ctx, "http://localhost/"), ch.Name, "")
		for _, t := range c.svc.Schema.Types() {
			if t.Singleton || !ch.AllowsCollection(t.Name) {
				continue
			}
			rt := srv.detail(t.Name)
			if !t.Page && rt == nil {
				continue
			}
			for offset := 0; ; offset += 100 {
				l, err := f.list(ctx, t.Name, url.Values{"limit": {"100"}, "offset": {fmt.Sprint(offset)}}, false)
				if err != nil {
					c.fail("channel %s: list %s: %v", ch.Name, t.Name, err)
					break
				}
				for _, it := range l.Items {
					c.requestItem(srv, ch, rt, it)
				}
				if !l.HasMore {
					break
				}
			}
		}
		for _, rt := range srv.routes {
			if rt.param == "" && ch.AllowsCollection(rt.Type) {
				path, _ := srv.pages.SiteURL(ch, rt.Path)
				c.request(srv, ch, path, fmt.Sprintf("listing %s", rt.Path))
			}
		}
		client := &Client{ctx: ctx, f: f}
		for _, cr := range srv.ext.routes {
			if !cr.applies(ch) || !cr.exportable() {
				continue
			}
			sets, err := cr.static(context.WithValue(ctx, clientKey{}, client))
			if err != nil {
				c.fail("channel %s: custom route %s: static params: %v", ch.Name, cr.pattern, err)
				continue
			}
			for _, params := range sets {
				p, err := cr.fill(params)
				if err != nil {
					c.fail("channel %s: %v", ch.Name, err)
					continue
				}
				path, _ := srv.pages.SiteURL(ch, p)
				c.request(srv, ch, path, "custom route "+cr.pattern)
			}
		}
	}
}

func (c *checker) requestItem(srv *server, ch *channel.Channel, rt *route, it *Item) {
	label := fmt.Sprintf("%s %s (%s)", it.Type, it.ID, it.Name)
	path := ""
	if it.URL != "" {
		if u, err := url.Parse(it.URL); err == nil && !srv.foreign(u) {
			path = u.Path
		}
	} else if rt != nil {
		if p, ok := rt.build(it.ID, it.Fields); ok {
			path, _ = srv.pages.SiteURL(ch, p)
		}
	}
	if path == "" {
		return
	}
	c.request(srv, ch, path, label)
}

func mustRequest(ctx context.Context, rawURL string) *http.Request {
	r, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	return r
}

// request GETs a path of a channel through the handler and expects a page.
func (c *checker) request(srv *server, ch *channel.Channel, path, label string) {
	host := "localhost"
	if v, ok := ch.Options["match"]; ok {
		if h := hostOf(v); h != "" {
			host = h
		}
	}
	r := mustRequest(context.Background(), "http://"+host+path)
	rec := &recorder{h: http.Header{}}
	srv.serveSite(rec, r)
	switch {
	case rec.code == http.StatusOK:
		c.pass()
	case rec.code/100 == 3:
		c.pass()
	default:
		c.fail("channel %s: %s at %s: status %d", ch.Name, label, path, rec.code)
	}
}

// hostOf reads the host of a channel's match option through its JSON form.
func hostOf(match any) string {
	b, err := json.Marshal(match)
	if err != nil {
		return ""
	}
	var m struct {
		Host string `json:"host"`
	}
	_ = json.Unmarshal(b, &m)
	return m.Host
}
