package pages

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
)

// maxLinkTags is how many linked items a response tags one by one; beyond it the response is tagged with the
// collections of the linked items instead, so listings of many items keep their tag headers small.
const maxLinkTags = 64

// registerDelivery adds the pages module's parts of the delivery API: /route and /tree, the URL decorator, the
// cache tags of page.changed, and the public channel options.
func registerDelivery(r *kernel.Registry, svc func() *Service) {
	d := &deliverer{svc: svc}
	r.DeliveryRoute(delivery.Route{Method: http.MethodGet, Path: "/route", Handler: d.route,
		Summary: "Locate a URL: the page or item it shows, a redirect, or not found", OpenAPI: opRouteDelivery()})
	r.DeliveryRoute(delivery.Route{Method: http.MethodGet, Path: "/tree", Handler: d.tree,
		Summary: "Navigation of a site", OpenAPI: opTreeDelivery()})
	r.Decorator(d)
	r.PreviewURL(d.previewURL)
	r.PatternURL(d.patternURL)
	r.FrameOrigin(d.frameOrigin)
	r.TagMapper(pageTags)
	for _, n := range []string{optSite, optMatch, optReserved, optRootReserved} {
		r.PublicChannelOption(n)
	}
	r.DeliveryVary(d.vary)
}

// vary lists the header names of every web channel that can match the same requests as ch: which channel a
// URL belongs to depends on those headers, so an answer cached for one channel must not be served to a
// request that the header of another would have claimed.
func (d *deliverer) vary(ch *channel.Channel) []string {
	m, ok := ch.Options[optMatch].(matcher)
	if !ok {
		return nil
	}
	var names []string
	if m.Header != nil {
		names = append(names, m.Header.Name)
	}
	if s, err := d.impl(); err == nil {
		for _, w := range s.web.byName {
			if w.ch != ch && w.m.Header != nil && m.overlaps(w.m) {
				names = append(names, w.m.Header.Name)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// pageTags maps page.changed to the routes and navigation of its site. Every change to routes, slugs and
// the tree (hooks, reconcile, node writes) queues one, so item events need no mapping of their own.
func pageTags(topic string, payload json.RawMessage) []string {
	if topic != "page.changed" {
		return nil
	}
	var p struct {
		Site string `json:"site"`
	}
	if json.Unmarshal(payload, &p) != nil || p.Site == "" {
		return nil
	}
	return []string{cachetag.Routes(p.Site), cachetag.Nav(p.Site)}
}

type deliverer struct{ svc func() *Service }

// previewURL is the item's route on the channel. Routes exist from publish on, so a never-published page has
// none and falls through to the channel's preview patterns.
func (d *deliverer) previewURL(ctx context.Context, ch *channel.Channel, item ids.UUID, _, locale string) (string, bool, error) {
	s, err := d.impl()
	if err != nil {
		return "", false, err
	}
	return s.itemURL(ctx, ch, item, locale)
}

// patternURL prefixes the path of a link or preview pattern like a page route: the channel's prefix, and its
// origin (frontend, else the matched host) for the absolute form.
func (d *deliverer) patternURL(ch *channel.Channel, path string) (rel, abs string, ok bool) {
	s, err := d.impl()
	if err != nil {
		return "", "", false
	}
	w := s.web.of(ch)
	return w.rel(path), w.abs(path), true
}

// frameOrigin is the origin the admin may frame for the channel: the one its URLs are built with.
func (d *deliverer) frameOrigin(ch *channel.Channel) string {
	s, err := d.impl()
	if err != nil {
		return ""
	}
	return s.web.of(ch).origin()
}

func (d *deliverer) impl() (*service, error) { return d.svc().get() }

// maxURLParam bounds the url parameter of /route; longer URLs can never be site paths.
const maxURLParam = 4096

// checkParams rejects unknown query parameters and repeated ones: every parameter of these routes has one value.
func checkParams(r *http.Request, allowed ...string) error {
	for k, v := range r.URL.Query() {
		switch k {
		case "channel", "locale", "preview":
		default:
			if !slices.Contains(allowed, k) {
				return manage.BadRequest("unknown query parameter %s", strconv.QuoteToASCII(truncate(k, 40)))
			}
		}
		if len(v) > 1 {
			return manage.BadRequest("query parameter %s may appear only once", strconv.QuoteToASCII(truncate(k, 40)))
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// route answers GET /route?url=: where a frontend's URL leads, with the item it shows.
func (d *deliverer) route(w http.ResponseWriter, r *http.Request) {
	if err := checkParams(r, "url"); err != nil {
		delivery.Fail(w, r, err)
		return
	}
	raw := r.URL.Query().Get("url")
	if raw == "" {
		delivery.Fail(w, r, manage.BadRequest("url is required"))
		return
	}
	if len(raw) > maxURLParam || !utf8.ValidString(raw) || strings.ContainsFunc(raw, func(c rune) bool { return c < 0x20 || c == 0x7f }) {
		delivery.Fail(w, r, manage.BadRequest("url must be valid text of at most %d bytes without control characters", maxURLParam))
		return
	}
	s, err := d.impl()
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	ch := delivery.Channel(r)
	loc, err := s.locate(r.Context(), raw, r.Header)
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	if loc.Kind == LocatedNoChannel {
		delivery.Fail(w, r, manage.NotFound("no channel's match fits the url (a channel that serves every URL has match: { prefix: / })"))
		return
	}
	if loc.Channel != ch.Name {
		delivery.Fail(w, r, manage.NotFound("the url is not served by this channel"))
		return
	}
	delivery.Tag(r, cachetag.Routes(s.web.of(ch).site))
	// Alternates and the canonical URL read the routes of the peers' sites.
	for _, p := range s.web.peers(s.web.of(ch)) {
		delivery.Tag(r, cachetag.Routes(p.site))
	}
	if loc.Resolution != nil && loc.Resolution.Item != nil {
		// Tagged before the lookup: an item that is not available yet purges the answer when it is published.
		delivery.Tag(r, cachetag.Item(loc.Resolution.Item.String()))
	}
	out := struct {
		Located *Located        `json:"located"`
		Item    json.RawMessage `json:"item,omitempty"`
	}{Located: loc}
	if (loc.Kind == ResolvedPage || loc.Kind == ResolvedItem) && loc.Resolution != nil && loc.Resolution.Item != nil {
		got, err := delivery.Items(r, loc.Locale, *loc.Resolution.Item)
		if err != nil {
			delivery.Fail(w, r, err)
			return
		}
		if got[0] == nil {
			delivery.Fail(w, r, manage.NotFound("not available"))
			return
		}
		out.Item = got[0]
	}
	delivery.WriteJSON(w, r, http.StatusOK, out)
}

// navNode is a node of the delivery navigation.
type navNode struct {
	ID     ids.UUID        `json:"id"`
	Kind   string          `json:"kind"`
	Item   *ids.UUID       `json:"item,omitempty"`
	Type   string          `json:"type,omitempty"`
	Name   string          `json:"name"`
	Path   string          `json:"path,omitempty"`
	URL    string          `json:"url,omitempty"`
	Fields json.RawMessage `json:"fields,omitempty"`
	Nodes  []navNode       `json:"nodes,omitempty"`
}

// tree answers GET /tree?site=&locale=&depth=: published, visible navigation in the channel's locale.
func (d *deliverer) tree(w http.ResponseWriter, r *http.Request) {
	if err := checkParams(r, "site", "depth", "fields"); err != nil {
		delivery.Fail(w, r, err)
		return
	}
	fields, err := delivery.FieldsParam(r)
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	s, err := d.impl()
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	ch := delivery.Channel(r)
	wc := s.web.of(ch)
	q := r.URL.Query()
	site := wc.site
	if v := q.Get("site"); v != "" {
		if !siteName.MatchString(v) || len(v) > 64 {
			delivery.Fail(w, r, manage.BadRequest("site must be a site name"))
			return
		}
		site = v
	}
	depth := 1
	if v := q.Get("depth"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxTreeDepth {
			delivery.Fail(w, r, manage.BadRequest("depth must be a whole number from 1 to %d", maxTreeDepth))
			return
		}
		depth = n
	}
	locale := delivery.Languages(r)[0]
	ctx := r.Context()
	nodes, err := s.tree(ctx, site, locale, nil, depth, nil)
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	delivery.Tag(r, cachetag.Nav(site), cachetag.Routes(site))
	var linked []ids.UUID
	collectLinkItems(nodes, &linked)
	urls, tags, err := s.linkURLs(ctx, ch, linked)
	if err != nil {
		delivery.Fail(w, r, err)
		return
	}
	delivery.Tag(r, tags...)
	nav := navNodes(wc, nodes, urls)
	if fields != nil {
		var pageItems []ids.UUID
		collectPageItems(nav, &pageItems)
		if err := attachFields(r, locale, fields, nav, pageItems); err != nil {
			delivery.Fail(w, r, err)
			return
		}
	}
	if nav == nil {
		nav = []navNode{}
	}
	delivery.WriteJSON(w, r, http.StatusOK, map[string]any{"site": site, "locale": locale, "nodes": nav})
}

func collectPageItems(nodes []navNode, out *[]ids.UUID) {
	for _, n := range nodes {
		if n.Kind == KindPage && n.Item != nil {
			*out = append(*out, *n.Item)
		}
		collectPageItems(n.Nodes, out)
	}
}

// attachFields loads the page items in one batch, projected, and puts each one's fields on its node.
func attachFields(r *http.Request, locale string, f *delivery.Fields, nodes []navNode, list []ids.UUID) error {
	if len(list) == 0 {
		return nil
	}
	payloads, err := delivery.ItemsFields(r, locale, f, list...)
	if err != nil {
		return err
	}
	byItem := make(map[ids.UUID]json.RawMessage, len(list))
	for i, raw := range payloads {
		if raw == nil {
			continue
		}
		var it struct {
			Fields json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(raw, &it); err != nil {
			return err
		}
		if len(it.Fields) > 0 {
			byItem[list[i]] = it.Fields
		}
	}
	setFields(nodes, byItem)
	return nil
}

func setFields(nodes []navNode, byItem map[ids.UUID]json.RawMessage) {
	for i := range nodes {
		if nodes[i].Kind == KindPage && nodes[i].Item != nil {
			nodes[i].Fields = byItem[*nodes[i].Item]
		}
		setFields(nodes[i].Nodes, byItem)
	}
}

func collectLinkItems(nodes []TreeNode, out *[]ids.UUID) {
	for _, n := range nodes {
		if n.Kind == KindLink && n.Link != nil && n.Link.Item != nil {
			*out = append(*out, *n.Link.Item)
		}
		collectLinkItems(n.Nodes, out)
	}
}

// navNodes keeps what a visitor sees: published pages with a path in the locale, mounts and links that lead
// somewhere, of the collections the channel allows; hidden nodes and everything below them drop out.
func navNodes(wc *webChannel, nodes []TreeNode, itemURLs map[ids.UUID]string) []navNode {
	var out []navNode
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		nn := navNode{ID: n.ID, Kind: n.Kind, Name: n.Name}
		switch n.Kind {
		case KindPage:
			if n.Status == "" || n.Status == "draft" || n.Path == "" || !wc.ch.AllowsCollection(n.Type) {
				continue
			}
			nn.Item, nn.Type, nn.Path, nn.URL = n.Item, n.Type, n.Path, wc.abs(n.Path)
		case KindMount:
			if n.Mount == nil || n.Path == "" || !wc.ch.AllowsCollection(n.Mount.Collection) {
				continue
			}
			nn.Path, nn.URL = n.Path, wc.abs(n.Path)
		case KindLink:
			if n.Broken || n.Link == nil {
				continue
			}
			switch {
			case n.Link.Item != nil:
				nn.URL = itemURLs[*n.Link.Item]
			case n.Target != "" && n.Target[0] == '/':
				nn.Path, nn.URL = n.Target, wc.abs(n.Target)
			default:
				nn.URL = n.Target
			}
			if nn.URL == "" {
				continue
			}
		}
		nn.Nodes = navNodes(wc, n.Nodes, itemURLs)
		out = append(out, nn)
	}
	return out
}

// routesOf reads the current routes of items across sites and locales in one query, with the item types.
func (s *service) routesOf(ctx context.Context, q store.SQL, list []ids.UUID) (map[ids.UUID]itemRoutes, map[ids.UUID]string, error) {
	routes := make(map[ids.UUID]itemRoutes, len(list))
	types := make(map[ids.UUID]string, len(list))
	err := chunks(list, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		return each(ctx, q, `SELECT r.item_id, r.site, r.locale, r.path, i.type FROM route r JOIN item i ON i.id = r.item_id
WHERE r.state = 'current' AND r.item_id IN `+ph, args, func(rows store.Rows) error {
			var id ids.UUID
			var site, locale, path, typ string
			if err := rows.Scan(&id, &site, &locale, &path, &typ); err != nil {
				return err
			}
			rt := routes[id]
			if rt == nil {
				rt = itemRoutes{}
				routes[id] = rt
			}
			if rt[site] == nil {
				rt[site] = map[string]string{}
			}
			rt[site][locale] = path
			types[id] = typ
			return nil
		})
	})
	return routes, types, err
}

// linkURLs is LinkURL for many items in one query: items with no route in any web channel are left out. The
// tags purge the answer when a route of a linked item appears or changes: the routes of the sites where the
// items have one, every site when some item has none, and the items themselves up to maxLinkTags.
func (s *service) linkURLs(ctx context.Context, from *channel.Channel, list []ids.UUID) (map[ids.UUID]string, []string, error) {
	out := map[ids.UUID]string{}
	if len(list) == 0 {
		return out, nil, nil
	}
	routes, types, err := s.routesOf(ctx, s.db.Read(), list)
	if err != nil {
		return nil, nil, err
	}
	tagged := map[string]bool{}
	for _, id := range list {
		rt := routes[id]
		u, ok := s.web.link(from, types[id], rt)
		if ok {
			out[id] = u
		} else {
			for _, site := range s.sites {
				tagged[cachetag.Routes(site)] = true
			}
		}
		for site := range rt {
			tagged[cachetag.Routes(site)] = true
		}
		if len(list) <= maxLinkTags {
			tagged[cachetag.Item(id.String())] = true
		}
	}
	return out, slices.Sorted(maps.Keys(tagged)), nil
}

// Decorate adds url (the item's route in the request's channel) and links (the URLs of referenced items) to
// the items of a response, from one query for all of them.
func (d *deliverer) Decorate(ctx context.Context, ch *channel.Channel, items []*delivery.Item) error {
	if len(items) == 0 {
		return nil
	}
	s, err := d.impl()
	if err != nil {
		return err
	}
	wc := s.web.of(ch)
	seen := map[ids.UUID]bool{}
	var want []ids.UUID
	add := func(id ids.UUID) {
		if !seen[id] {
			seen[id] = true
			want = append(want, id)
		}
	}
	parsed := make([]ids.UUID, len(items))
	for i, it := range items {
		if id, err := ids.Parse(it.ID); err == nil {
			parsed[i] = id
			add(id)
		}
		for _, ref := range it.Refs {
			add(ref)
		}
	}
	routes, types, err := s.routesOf(ctx, s.db.Read(), want)
	if err != nil {
		return err
	}
	tagged := map[string]bool{cachetag.Routes(wc.site): true}
	unresolved := false
	linkTypes := map[string]bool{}
	linkIDs := map[ids.UUID]bool{}
	refIDs := map[ids.UUID]bool{}
	for i, it := range items {
		extra := map[string]any{}
		if path, ok := routes[parsed[i]][wc.site][it.Locale]; ok && slices.Contains(ch.Languages, it.Locale) && ch.AllowsCollection(it.Type) {
			extra["url"] = wc.abs(path)
		}
		links := map[string]string{}
		for _, ref := range it.Refs {
			rt := routes[ref]
			refIDs[ref] = true
			if u, ok := s.web.link(ch, types[ref], rt); ok {
				links[ref.String()] = u
				linkIDs[ref] = true
				linkTypes[types[ref]] = true
				for site := range rt {
					tagged[cachetag.Routes(site)] = true
				}
			} else {
				unresolved = true
			}
		}
		if len(links) > 0 {
			extra["links"] = links
		}
		if len(extra) > 0 {
			if it.Extra == nil {
				it.Extra = map[string]any{}
			}
			for k, v := range extra {
				it.Extra[k] = v
			}
		}
	}
	if unresolved {
		for _, site := range s.sites {
			tagged[cachetag.Routes(site)] = true
		}
	}
	var tags []string
	for t := range tagged {
		tags = append(tags, t)
	}
	// Every referenced item is tagged, linked or not: one that is unavailable now purges the response when it
	// is published. Beyond the bound the collections of the linked ones and the routes of every site (set
	// above when a reference has no link) stand in.
	if len(refIDs) <= maxLinkTags {
		for id := range refIDs {
			tags = append(tags, cachetag.Item(id.String()))
		}
	} else {
		for typ := range linkTypes {
			tags = append(tags, cachetag.Collection(typ))
		}
	}
	slices.Sort(tags)
	delivery.TagContext(ctx, tags...)
	return nil
}
