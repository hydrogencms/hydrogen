package site

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
)

// Exporter is the site renderer without HTTP, for modules that write the site out (static export). The site
// module provides it: a module that Requires "site" calls kernel.Lookup[*site.Exporter](r) in Register and
// uses it from Boot on (before that the site has no templates and every method fails).
//
// An exporter sees the site as a visitor does: published content only, no preview, no cookies. It renders
// through the very code paths live requests take (routing, redirects, templates, helpers, custom routes, 404),
// except that the in-process page cache is bypassed so the output is never older than the data. The delivery
// API's own response cache is purged by the hydrogen_purge notification (cachetag.PurgeChannel); a consumer that
// renders on a publish event should call Purge with the event's tags first, so it never reads a stale answer.
//
// Typical use:
//
//	ex, _ := kernel.Lookup[*site.Exporter](r)           // Register
//	for _, ch := range ex.Channels() {                   // Run
//	    targets, _ := ex.Targets(ctx, ch.Name)
//	    for _, t := range targets {
//	        out, _ := ex.Render(ctx, ch.Name, t.URL)    // out.Body, out.Status, out.Header, out.Tags
//	    }
//	}
//
// All methods are safe for concurrent use.
type Exporter struct{ m *module }

// ExportChannel describes a rendered channel.
type ExportChannel struct {
	Name string
	// Host is the host the channel matches ("" when it matches any host) and Prefix its path prefix ("" when
	// none), as `match` declares them: a static export is written under <Host or Name>/<URL path>.
	Host   string
	Prefix string
	// Origin is the channel's public origin (its `frontend` option, scheme://host[:port]) or "".
	Origin    string
	Languages []string
}

// Target kinds.
const (
	TargetPage     = "page"      // a page of the page tree
	TargetItem     = "item"      // a mounted or routed collection item
	TargetListing  = "listing"   // a collection listing (modules.site.routes), page 1 and every further page
	TargetRoute    = "route"     // a custom route with static params
	TargetNotFound = "not_found" // the channel's 404 page (rendered with status 404)
)

// Target is one renderable URL of a channel.
type Target struct {
	Channel string
	// URL is the path and query as a visitor requests it, including the channel's prefix and without a host
	// ("/en/about", "/en" for the home page, "/en/blog/page/2"). Pass it to Render.
	URL    string
	Kind   string
	Locale string // the content's locale ("" for listings, routes and the 404 page)
	Item   string // the item id, for pages and items
}

// Rendered is one URL as the visitor receives it.
type Rendered struct {
	Status int
	// Header holds what the server would send: Content-Type, Location (redirects), Cache-Control, ETag…
	Header http.Header
	Body   []byte
	// Tags are the cache tags the answer depends on, sorted: the very tags of its Surrogate-Key header, which
	// the live page cache purges by. An event whose tags intersect them makes the file stale.
	Tags []string
}

// Location is the redirect target of a 3xx answer, else "".
func (r *Rendered) Location() string { return r.Header.Get("Location") }

// AssetFile is a fingerprinted theme or project asset the pages link to.
type AssetFile struct {
	URL  string // the absolute path pages use: /__hydrogen/assets/site.1a2b3c.css
	Name string // the file name inside assets/: site.css
	Data []byte
}

func (e *Exporter) server() (*server, error) {
	if e == nil || e.m == nil || e.m.srv == nil {
		return nil, errors.New("site: the site module is not booted")
	}
	return e.m.srv, nil
}

type matchOpt struct {
	Host   string `json:"host"`
	Prefix string `json:"prefix"`
	Header *struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"header"`
}

func matchOf(ch *channel.Channel) matchOpt {
	var m matchOpt
	if v, ok := ch.Options["match"]; ok {
		if b, err := json.Marshal(v); err == nil {
			_ = json.Unmarshal(b, &m)
		}
	}
	return m
}

// Channels lists the channels the site renders, by name.
func (e *Exporter) Channels() []ExportChannel {
	s, err := e.server()
	if err != nil {
		return nil
	}
	var out []ExportChannel
	for _, ch := range s.svc.Channels.All() {
		if !renders(ch) {
			continue
		}
		m := matchOf(ch)
		out = append(out, ExportChannel{Name: ch.Name, Host: m.Host, Prefix: m.Prefix, Origin: strings.TrimRight(ch.Frontend, "/"),
			Languages: slices.Clone(ch.Languages)})
	}
	slices.SortFunc(out, func(a, b ExportChannel) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (e *Exporter) channel(s *server, name string) (*channel.Channel, error) {
	ch, ok := s.svc.Channels.Get(name)
	if !ok || !renders(ch) {
		return nil, fmt.Errorf("site: channel %q does not render (render: go)", name)
	}
	return ch, nil
}

// request builds the visitor's GET for a URL of a channel.
func (s *server) exportRequest(ctx context.Context, ch *channel.Channel, uri string) (*http.Request, error) {
	u, err := url.Parse(uri)
	if err != nil || !strings.HasPrefix(uri, "/") || strings.HasPrefix(uri, "//") || u.Host != "" || u.Scheme != "" {
		return nil, fmt.Errorf("site: %q is not a path of the site (such as /en/about)", uri)
	}
	if u.Query().Has("preview") {
		return nil, errors.New("site: exports render the published site: no preview parameter")
	}
	m := matchOf(ch)
	host := m.Host
	if host == "" {
		host = "localhost"
	}
	req, err := http.NewRequestWithContext(context.WithValue(ctx, noCacheKey{}, true), http.MethodGet, "http://"+host+uri, nil)
	if err != nil {
		return nil, err
	}
	if m.Header != nil {
		req.Header.Set(m.Header.Name, m.Header.Value)
	}
	if got, _, ok := s.pages.Match("http://"+host+u.EscapedPath(), req.Header); !ok || got != ch {
		return nil, fmt.Errorf("site: %s is not a URL of channel %s", uri, ch.Name)
	}
	return req, nil
}

// Render renders one URL of a channel (see Target.URL) as a visitor would see it. A redirect, a 404 and any
// other answer below 500 are results, not errors; a 5xx answer (a template or content problem) is an error,
// returned with the Rendered it produced.
func (e *Exporter) Render(ctx context.Context, channelName, uri string) (*Rendered, error) {
	s, err := e.server()
	if err != nil {
		return nil, err
	}
	ch, err := e.channel(s, channelName)
	if err != nil {
		return nil, err
	}
	req, err := s.exportRequest(ctx, ch, uri)
	if err != nil {
		return nil, err
	}
	rec := &recorder{h: http.Header{}}
	s.serveSite(rec, req)
	if rec.code == 0 {
		rec.code = http.StatusOK
	}
	out := &Rendered{Status: rec.code, Header: rec.h, Body: rec.body.Bytes(), Tags: strings.Fields(rec.h.Get("Surrogate-Key"))}
	slices.Sort(out.Tags)
	if rec.code >= 500 {
		return out, fmt.Errorf("site: %s answered %d: %s", uri, rec.code, truncate(string(out.Body), 200))
	}
	return out, nil
}

// Purge drops the cached delivery answers (and rendered pages) that carry any of the tags, so the next Render
// reads current data. Call it with the tags of a change before re-rendering what they touch; cachetag.All drops
// everything.
func (e *Exporter) Purge(tags ...string) {
	s, err := e.server()
	if err != nil {
		return
	}
	s.svc.Delivery.Purge(tags...)
	if s.cache != nil {
		s.cache.purge(tags...)
	}
}

// Assets lists the fingerprinted assets of the site (the chosen theme chain under the project's assets/).
func (e *Exporter) Assets() ([]AssetFile, error) {
	s, err := e.server()
	if err != nil {
		return nil, err
	}
	eng := s.engine()
	if eng == nil {
		return nil, fmt.Errorf("site: templates are not loaded: %v", s.loadErr)
	}
	var out []AssetFile
	list, err := eng.assets.all()
	if err != nil {
		return nil, fmt.Errorf("site: hash assets: %w", err)
	}
	for _, en := range list {
		r, _, done, err := eng.assets.body(en.name)
		if err != nil {
			return nil, fmt.Errorf("site: read asset %s: %w", eng.assets.files[en.name].where, err)
		}
		b, err := io.ReadAll(r)
		done()
		if err != nil {
			return nil, fmt.Errorf("site: read asset %s: %w", eng.assets.files[en.name].where, err)
		}
		out = append(out, AssetFile{URL: assetsURL + en.fp, Name: en.name, Data: b})
	}
	slices.SortFunc(out, func(a, b AssetFile) int { return strings.Compare(a.URL, b.URL) })
	return out, nil
}

// Targets enumerates every URL the site renders for a channel, sorted by URL: the pages of the tree and the
// items mounted below them, the items and listing pages of modules.site.routes (listings walk every page),
// custom routes that declare static params, and the 404 page. Redirects are not targets: ask the route table
// (or Render a retired URL) for those.
func (e *Exporter) Targets(ctx context.Context, channelName string) ([]Target, error) {
	s, err := e.server()
	if err != nil {
		return nil, err
	}
	ch, err := e.channel(s, channelName)
	if err != nil {
		return nil, err
	}
	probe, err := s.exportRequest(ctx, ch, strings.TrimRight(matchOf(ch).Prefix, "/")+notFoundPath)
	if err != nil {
		return nil, err
	}
	f := s.fetcher(probe, ch.Name, "")
	client := &Client{ctx: ctx, f: f}
	seen := map[string]bool{}
	var out []Target
	add := func(t Target) {
		if !seen[t.URL] {
			seen[t.URL] = true
			t.Channel = ch.Name
			out = append(out, t)
		}
	}
	for _, t := range s.svc.Schema.Types() {
		if t.Singleton || !ch.AllowsCollection(t.Name) {
			continue
		}
		rt := s.detail(t.Name)
		for offset := 0; ; offset += 100 {
			l, err := f.list(ctx, t.Name, url.Values{"limit": {"100"}, "offset": {fmt.Sprint(offset)}}, false)
			if err != nil {
				return nil, fmt.Errorf("site: list %s for %s: %w", t.Name, ch.Name, err)
			}
			for _, it := range l.Items {
				if p := s.itemPath(ch, rt, it); p != "" {
					kind := TargetItem
					if t.Page {
						kind = TargetPage
					}
					if s.belongs(ch, p) {
						add(Target{URL: p, Kind: kind, Locale: it.Locale, Item: it.ID})
					}
				}
			}
			if !l.HasMore {
				break
			}
		}
	}
	for _, rt := range s.routes {
		if rt.param != "" || !ch.AllowsCollection(rt.Type) {
			continue
		}
		for pg := 1; ; pg++ {
			here := rt.Path
			if pg > 1 {
				here = fmt.Sprintf("%s/page/%d", rt.Path, pg)
			}
			rel, _ := s.pages.SiteURL(ch, here)
			add(Target{URL: rel, Kind: TargetListing})
			l, err := f.list(ctx, rt.Type, url.Values{"limit": {fmt.Sprint(rt.Limit)}, "offset": {fmt.Sprint(pg * rt.Limit)}}, false)
			if err != nil {
				return nil, fmt.Errorf("site: list %s for %s: %w", rt.Type, ch.Name, err)
			}
			if len(l.Items) == 0 {
				break
			}
		}
	}
	for _, cr := range s.ext.routes {
		if !cr.applies(ch) || !cr.exportable() {
			continue
		}
		sets, err := cr.static(context.WithValue(ctx, clientKey{}, client))
		if err != nil {
			return nil, fmt.Errorf("site: static params of %s: %w", cr.pattern, err)
		}
		for _, params := range sets {
			p, err := cr.fill(params)
			if err != nil {
				return nil, fmt.Errorf("site: %w", err)
			}
			rel, _ := s.pages.SiteURL(ch, p)
			add(Target{URL: rel, Kind: TargetRoute})
		}
	}
	rel, _ := s.pages.SiteURL(ch, notFoundPath)
	add(Target{URL: rel, Kind: TargetNotFound})
	slices.SortStableFunc(out, func(a, b Target) int { return strings.Compare(a.URL, b.URL) })
	return out, nil
}

// itemPath is the URL path of a listed item: the pages module's, else its collection route's.
func (s *server) itemPath(ch *channel.Channel, rt *route, it *Item) string {
	if it.URL != "" {
		if u, err := url.Parse(it.URL); err == nil && u.EscapedPath() != "" {
			return u.EscapedPath()
		}
	}
	if rt != nil {
		if p, ok := rt.build(it.ID, it.Fields); ok {
			rel, _ := s.pages.SiteURL(ch, p)
			return rel
		}
	}
	return ""
}

// belongs reports whether a path is served by the channel (an item in a fallback language is listed with the
// URL of another channel).
func (s *server) belongs(ch *channel.Channel, path string) bool {
	m := matchOf(ch)
	host := m.Host
	if host == "" {
		host = "localhost"
	}
	h := http.Header{}
	if m.Header != nil {
		h.Set(m.Header.Name, m.Header.Value)
	}
	got, _, ok := s.pages.Match("http://"+host+path, h)
	return ok && got == ch
}
