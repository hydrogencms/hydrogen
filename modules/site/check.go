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
	if c.eng, err = loadEngine(m.cfg.Dir, true); err != nil {
		c.fail("%v", err)
		return c.result()
	}
	fmt.Fprintf(out, "templates: %d parsed\n", len(c.eng.names))
	c.required()
	c.blocks(chans[0])
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
	p := &page{eng: c.eng, ch: ch, locale: loc, def: c.svc.Core.Content.DefaultLocale, preview: preview, strict: true, view: v,
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
	srv, err := newServer(&cfg, c.svc, c.m.pages)
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
	}
}

func (c *checker) requestItem(srv *server, ch *channel.Channel, rt *route, it *Item) {
	label := fmt.Sprintf("%s %s (%s)", it.Type, it.ID, it.Name)
	path := ""
	if it.URL != "" {
		if u, err := url.Parse(it.URL); err == nil {
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
