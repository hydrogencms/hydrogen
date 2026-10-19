package site

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

func (s *server) internal(rc *reqCtx, err error) *result {
	s.log.Error("site: render failed", "path", rc.r.URL.Path, "err", err)
	return &result{status: http.StatusInternalServerError, body: []byte("<!doctype html><title>Error</title><h1>Something went wrong</h1>")}
}

func (s *server) adminOrigin(r *http.Request) string {
	if s.cfg.AdminURL != "" {
		return s.cfg.AdminURL
	}
	o, _ := normalizeOrigin(s.schemeOf(r) + "://" + r.Host)
	return o
}

// resolve turns a request into a page: collection routes first (a plain string match), then the pages
// module's routing, which answers pages, mounts, redirects, reserved paths and misses.
func (s *server) resolve(rc *reqCtx) *result {
	for _, rt := range s.routes {
		if !rc.ch.AllowsCollection(rt.Type) {
			continue
		}
		if n, ok := rt.matchPage(rc.rest); ok {
			return s.listing(rc, rt, n)
		}
		val, ok := rt.match(rc.rest)
		if !ok {
			continue
		}
		if res := s.collection(rc, rt, val); res != nil {
			return res
		}
		if rt.param != "" {
			return s.notFound(rc)
		}
	}
	rr, rep, err := rc.f.route(rc.ctx, rc.raw)
	switch {
	case err != nil:
		return s.internal(rc, err)
	case rr == nil && rep.status == http.StatusUnauthorized:
		return &result{status: http.StatusUnauthorized, body: []byte("invalid or expired preview token")}
	case rr == nil && rep.status == http.StatusNotFound:
		return s.notFound(rc)
	case rr == nil:
		return s.internal(rc, fmt.Errorf("routing answered %d: %s", rep.status, truncate(string(rep.body), 200)))
	}
	loc := rr.Located
	if loc == nil {
		return s.notFound(rc)
	}
	if loc.Kind == pages.LocatedReserved {
		return &result{status: http.StatusNotFound, body: []byte("404 page not found\n"), tags: rc.f.tagList()}
	}
	if loc.Resolution == nil {
		return s.notFound(rc)
	}
	switch loc.Kind {
	case pages.ResolvedRedirect:
		return &result{status: loc.Resolution.Status, location: loc.Resolution.Location, tags: rc.f.tagList()}
	case pages.ResolvedPage, pages.ResolvedItem:
		if rr.Item == nil {
			return s.notFound(rc)
		}
		name, ok := rc.eng.pageTemplate(rr.Item.Type)
		if loc.Kind == pages.ResolvedItem {
			name, ok = rc.eng.itemTemplate(rr.Item.Type)
		}
		if !ok {
			return s.internal(rc, fmt.Errorf("no template for page type %s (add pages/%s.html or pages/default.html)", rr.Item.Type, rr.Item.Type))
		}
		return s.page(rc, name, rr.Item, loc, nil)
	}
	return s.notFound(rc)
}

func (s *server) notFound(rc *reqCtx) *result {
	if !rc.eng.has("404.html") {
		return &result{status: http.StatusNotFound, body: []byte("404 page not found\n"), tags: rc.f.tagList()}
	}
	p, v := s.newPage(rc, rc.ch.Languages[0], nil)
	v.Status = http.StatusNotFound
	v.Title = p.tFn("not_found_title")
	p.loadNav = s.navLoader(rc, p)
	body, err := rc.eng.render("404.html", p, v)
	if err != nil {
		return s.internal(rc, err)
	}
	return &result{status: http.StatusNotFound, body: s.finish(rc, body), tags: rc.f.tagList()}
}

// finish adds the preview scaffolding to a rendered page.
func (s *server) finish(rc *reqCtx, body []byte) []byte {
	if !rc.preview() {
		return body
	}
	sum := sha256.Sum256([]byte(rc.token))
	return injectPreview(body, html.EscapeString(s.adminOrigin(rc.r)), hex.EncodeToString(sum[:]))
}

// newPage builds the render state of a request: helpers, settings and the view.
func (s *server) newPage(rc *reqCtx, locale string, it *Item) (*page, *View) {
	rel := func(path string) string {
		r, _ := s.pages.SiteURL(rc.ch, path)
		return r
	}
	v := &View{Item: it, Fields: map[string]any{}, Locale: locale, Status: http.StatusOK, Preview: rc.preview(),
		Channel:  ChannelView{Name: rc.ch.Name, Languages: rc.ch.Languages, Home: rel("/")},
		Path:     rc.r.URL.Path,
		Settings: map[string]map[string]any{}, Params: map[string]string{}}
	if it != nil {
		v.Fields = it.Fields
		v.Title = itemTitle(it)
	}
	p := &page{ctx: rc.ctx, eng: rc.eng, ch: rc.ch, locale: locale, def: s.svc.Core.Content.DefaultLocale, preview: rc.preview(), log: s.log,
		view: v, links: map[string]string{}}
	if it != nil {
		for k, u := range it.Links {
			p.links[k] = u
		}
	}
	for _, typ := range s.singletons {
		if !rc.ch.AllowsCollection(typ) {
			continue
		}
		si, err := rc.f.singleton(rc.ctx, typ)
		if err != nil {
			s.log.Warn("site: settings unavailable", "type", typ, "err", err)
			continue
		}
		if si != nil {
			v.Settings[typ] = si.Fields
		}
	}
	p.siteName, _ = p.settingFn("siteName").(string)
	return p, v
}

func itemTitle(it *Item) string {
	if t, _ := it.Fields["title"].(string); t != "" {
		return t
	}
	return it.Name
}

// navLoader reads the page tree of the channel's site: two levels, with the current page marked.
func (s *server) navLoader(rc *reqCtx, p *page) func() ([]*NavItem, error) {
	return func() ([]*NavItem, error) {
		nodes, err := rc.f.tree(rc.ctx, 2)
		if err != nil {
			return nil, err
		}
		return s.navItems(rc.ch, nodes, p.view.Path), nil
	}
}

func (s *server) navItems(ch *channel.Channel, nodes []treeNode, current string) []*NavItem {
	var out []*NavItem
	for _, n := range nodes {
		u := n.URL
		if n.Path != "" {
			u, _ = s.pages.SiteURL(ch, n.Path)
		}
		it := &NavItem{Name: n.Name, URL: u, Active: u == current}
		it.Children = s.navItems(ch, n.Nodes, current)
		it.Open = it.Active || (u != "" && strings.HasPrefix(current, u+"/"))
		out = append(out, it)
	}
	return out
}

// page renders an item with a template. loc is the routing result for pages; route is set for collection
// items.
func (s *server) page(rc *reqCtx, tpl string, it *Item, loc *pages.Located, rt *route) *result {
	p, v := s.newPage(rc, it.Locale, it)
	p.loadNav = s.navLoader(rc, p)
	s.extraLinks(rc, p, it)
	switch {
	case loc != nil:
		v.Path, _ = s.pages.SiteURL(rc.ch, loc.Resolution.Path)
		p.canon, p.alts = loc.Canonical, loc.Alternates
		for _, c := range loc.Resolution.Breadcrumbs {
			u, _ := s.pages.SiteURL(rc.ch, c.Path)
			p.crumbs = append(p.crumbs, Crumb{Name: c.Name, URL: u})
		}
	case rt != nil:
		path, _ := rt.build(it.ID, it.Fields)
		var abs string
		v.Path, abs = s.pages.SiteURL(rc.ch, path)
		p.canon = abs
		p.crumbs = []Crumb{{Name: p.tFn("home"), URL: v.Channel.Home}, {Name: v.Title, URL: v.Path}}
		p.alts = s.collectionAlternates(rc, rt, it)
	}
	if len(p.alts) > 1 {
		for _, a := range p.alts {
			if other, ok := s.svc.Channels.Get(a.Channel); ok && other.Frontend != "" {
				a.URL = strings.TrimPrefix(a.URL, strings.TrimRight(other.Frontend, "/"))
			}
			v.Alternates = append(v.Alternates, a)
		}
		def := s.svc.Core.Content.DefaultLocale
		slices.SortStableFunc(v.Alternates, func(a, b pages.Alternate) int {
			return cmp.Compare(b2i(b.Locale == def), b2i(a.Locale == def))
		})
	}
	body, err := rc.eng.render(tpl, p, v)
	if err != nil {
		return s.internal(rc, err)
	}
	return &result{status: http.StatusOK, body: s.finish(rc, body), tags: rc.f.tagList(), store: !rc.preview()}
}

// extraLinks resolves references to items that have no page but a collection route, which delivery's links
// leave out.
func (s *server) extraLinks(rc *reqCtx, p *page, it *Item) {
	if len(s.routes) == 0 {
		return
	}
	var want []string
	collectIDs(it.Fields, &want)
	n := 0
	for _, id := range want {
		if _, ok := p.links[id]; ok || id == it.ID {
			continue
		}
		if n++; n > 64 {
			break
		}
		ref, err := rc.f.item(rc.ctx, id, false, "")
		if err != nil || ref == nil {
			continue
		}
		rt := s.detail(ref.Type)
		if rt == nil || !rc.ch.AllowsCollection(ref.Type) {
			continue
		}
		if path, ok := rt.build(ref.ID, ref.Fields); ok {
			p.links[id], _ = s.pages.SiteURL(rc.ch, path)
		}
	}
}

// collectIDs gathers item ids a document refers to: item: link targets and bare ids in values.
func collectIDs(v any, out *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for _, k := range sortedKeys(x) {
			if k == "id" {
				continue
			}
			collectIDs(x[k], out)
		}
	case []any:
		for _, e := range x {
			collectIDs(e, out)
		}
	case string:
		id := strings.TrimPrefix(x, "item:")
		if isUUID(id) && !slices.Contains(*out, id) {
			*out = append(*out, id)
		}
	}
}

// collection serves a collection route; nil when the path matched but no item did (routing goes on).
func (s *server) collection(rc *reqCtx, rt *route, val string) *result {
	if rt.param == "" {
		return s.listing(rc, rt, 1)
	}
	it, err := s.findItem(rc, rt, val)
	if err != nil {
		return s.internal(rc, err)
	}
	if it == nil {
		return nil
	}
	return s.page(rc, rt.templateName(), it, nil, rt)
}

func (s *server) listing(rc *reqCtx, rt *route, page int) *result {
	q := url.Values{"limit": {fmt.Sprint(rt.Limit)}}
	if page > 1 {
		q.Set("offset", fmt.Sprint((page-1)*rt.Limit))
	}
	if rt.Sort != "" {
		q.Set("sort", rt.Sort)
	}
	l, err := rc.f.list(rc.ctx, rt.Type, q, rc.preview())
	if err != nil {
		return s.internal(rc, err)
	}
	if page > 1 && len(l.Items) == 0 {
		return s.notFound(rc)
	}
	loc := rc.ch.Languages[0]
	p, v := s.newPage(rc, loc, nil)
	p.loadNav = s.navLoader(rc, p)
	v.Items = l.Items
	for _, it := range l.Items {
		if d := s.detail(it.Type); d != nil {
			if path, ok := d.build(it.ID, it.Fields); ok {
				it.Path, _ = s.pages.SiteURL(rc.ch, path)
			}
		}
	}
	here := rt.Path
	if page > 1 {
		here = fmt.Sprintf("%s/page/%d", rt.Path, page)
	}
	v.Path, p.canon = s.pages.SiteURL(rc.ch, here)
	if page > 1 || l.HasMore {
		pg := &Pager{Page: page}
		if page > 1 {
			prev := rt.Path
			if page > 2 {
				prev = fmt.Sprintf("%s/page/%d", rt.Path, page-1)
			}
			pg.Prev, _ = s.pages.SiteURL(rc.ch, prev)
		}
		if l.HasMore {
			pg.Next, _ = s.pages.SiteURL(rc.ch, fmt.Sprintf("%s/page/%d", rt.Path, page+1))
		}
		v.Pager = pg
	}
	p.crumbs = []Crumb{{Name: p.tFn("home"), URL: v.Channel.Home}}
	v.Title = p.tFn(rt.Template)
	body, err := rc.eng.render(rt.templateName(), p, v)
	if err != nil {
		return s.internal(rc, err)
	}
	return &result{status: http.StatusOK, body: s.finish(rc, body), tags: rc.f.tagList(), store: !rc.preview()}
}

// findItem looks an item up by the route's parameter: an id, or a field matched through the listing's filter
// (the field must be indexed) or, when it is not, by scanning the listing.
func (s *server) findItem(rc *reqCtx, rt *route, val string) (*Item, error) {
	if rt.param == "id" {
		if !isUUID(val) {
			return nil, nil
		}
		it, err := rc.f.item(rc.ctx, val, rc.preview(), "")
		if err != nil || it == nil || it.Type != rt.Type {
			return nil, err
		}
		return it, nil
	}
	id, err := s.idByField(rc, rt, val)
	if err != nil || id == "" {
		return nil, err
	}
	return rc.f.item(rc.ctx, id, rc.preview(), "")
}

const scanPage, scanPages = 100, 20

func (s *server) idByField(rc *reqCtx, rt *route, val string) (string, error) {
	q := url.Values{"where[" + rt.param + "][eq]": {val}, "limit": {"1"}}
	l, err := rc.f.list(rc.ctx, rt.Type, q, rc.preview())
	var bad *badRequest
	switch {
	case err == nil:
		if len(l.Items) > 0 {
			return l.Items[0].ID, nil
		}
		return "", nil
	case !errors.As(err, &bad):
		return "", err
	}
	// The field is not indexed: scan the listing.
	for page := range scanPages {
		l, err := rc.f.list(rc.ctx, rt.Type, url.Values{"limit": {fmt.Sprint(scanPage)}, "offset": {fmt.Sprint(page * scanPage)}}, rc.preview())
		if err != nil {
			return "", err
		}
		for _, it := range l.Items {
			if v, _ := it.Fields[rt.param].(string); v == val {
				return it.ID, nil
			}
		}
		if !l.HasMore {
			break
		}
	}
	return "", nil
}

// collectionAlternates lists the hreflang set of a collection item: the same item in the other rendered
// channels of its group that can show it.
func (s *server) collectionAlternates(rc *reqCtx, rt *route, it *Item) []pages.Alternate {
	if rc.ch.Group == "" {
		return nil
	}
	var out []pages.Alternate
	seen := map[string]bool{it.Locale: true}
	for _, other := range s.svc.Channels.Group(rc.ch.Group) {
		if other == rc.ch || !renders(other) || !other.AllowsCollection(it.Type) {
			continue
		}
		f := s.fetcher(rc.r, other.Name, "")
		o, err := f.item(rc.ctx, it.ID, false, "")
		if err != nil || o == nil || seen[o.Locale] || !slices.Contains(other.Languages, o.Locale) {
			continue
		}
		path, ok := rt.build(o.ID, o.Fields)
		if !ok {
			continue
		}
		seen[o.Locale] = true
		_, abs := s.pages.SiteURL(other, path)
		out = append(out, pages.Alternate{Channel: other.Name, Locale: o.Locale, URL: abs})
		for t := range f.tags {
			rc.f.tags[t] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	_, self := s.pages.SiteURL(rc.ch, mustBuild(rt, it))
	out = append(out, pages.Alternate{Channel: rc.ch.Name, Locale: it.Locale, URL: self})
	slices.SortFunc(out, func(a, b pages.Alternate) int { return strings.Compare(a.Channel, b.Channel) })
	return out
}

func mustBuild(rt *route, it *Item) string {
	p, _ := rt.build(it.ID, it.Fields)
	return p
}

// servePreviewItem shows one item from the URL a channel's preview pattern produces for items without a page
// yet: /preview/{locale}/{collection}/{id} (or without the locale).
func (s *server) servePreviewItem(w http.ResponseWriter, r *http.Request, eng *engine, rest, tok string, fromCookie bool) {
	token, done := s.acceptToken(w, r, tok, fromCookie, "")
	if done {
		return
	}
	name, scope, _, _ := s.svc.Delivery.PreviewTokenInfo(token)
	ch, ok := s.svc.Channels.Get(name)
	parts := strings.Split(rest, "/")
	if token == "" || !ok || !renders(ch) || len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	id, collection, locale := parts[len(parts)-1], parts[len(parts)-2], ch.Languages[0]
	if len(parts) == 3 {
		locale = parts[0]
	}
	if !slices.Contains(ch.Languages, locale) || !isUUID(id) || (scope != "" && scope != id) {
		http.NotFound(w, r)
		return
	}
	rc := &reqCtx{ctx: r.Context(), r: r, ch: ch, raw: s.schemeOf(r) + "://" + r.Host + r.URL.EscapedPath(),
		f: s.fetcher(r, ch.Name, token), token: token, eng: eng}
	it, err := rc.f.item(rc.ctx, id, true, locale)
	var res *result
	switch {
	case err != nil:
		res = s.internal(rc, err)
	case it == nil || it.Type != collection:
		res = s.notFound(rc)
	default:
		res = s.previewPage(rc, it)
	}
	s.respond(w, r, ch, res, true, "", 0)
}

// previewPage picks the template of an item that has no route: its page template, else its collection route.
func (s *server) previewPage(rc *reqCtx, it *Item) *result {
	if t, ok := s.svc.Schema.Type(it.Type); ok && !t.Page {
		if rt := s.detail(it.Type); rt != nil {
			return s.page(rc, rt.templateName(), it, nil, rt)
		}
	}
	name, ok := rc.eng.itemTemplate(it.Type)
	if !ok {
		return s.internal(rc, fmt.Errorf("no template for type %s", it.Type))
	}
	return s.page(rc, name, it, nil, nil)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}
