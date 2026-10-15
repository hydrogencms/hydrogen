package pages

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

func (s *service) matchURL(rawURL string, h http.Header) (*webChannel, string, bool) {
	host, path, _, err := splitURL(rawURL)
	if err != nil {
		return nil, "", false
	}
	return s.web.match(host, path, h)
}

func notFoundLocated(w *webChannel) *Located {
	return &Located{Channel: w.ch.Name, Kind: ResolvedNotFound, Resolution: notFoundResolution()}
}

func (s *service) locate(ctx context.Context, rawURL string, h http.Header) (*Located, error) {
	host, path, bad, err := splitURL(rawURL)
	if err != nil {
		return nil, badRequest("url %q: %v", rawURL, err)
	}
	w, rest, ok := s.web.match(host, path, h)
	if !ok {
		return &Located{Kind: LocatedNoChannel}, nil
	}
	if bad {
		return notFoundLocated(w), nil
	}
	rest, slash := trimSlash(rest)
	if w.isReserved(rest) {
		return &Located{Channel: w.ch.Name, Kind: LocatedReserved}, nil
	}
	locale, res, hit, err := s.resolveInChannel(ctx, w, rest)
	if err != nil {
		return nil, err
	}
	if res.Kind == ResolvedNotFound {
		return notFoundLocated(w), nil
	}
	out := &Located{Channel: w.ch.Name, Kind: res.Kind, Locale: locale, Resolution: res}
	if slash {
		// The path with a trailing slash is not a site path; send the client to the one that is.
		out.Kind = ResolvedRedirect
		out.Resolution = &Resolution{Kind: ResolvedRedirect, Item: res.Item, Path: rest, Status: http.StatusPermanentRedirect,
			Location: w.rel(rest)}
		return out, nil
	}
	if res.Kind == ResolvedPage || res.Kind == ResolvedItem {
		var crumbErr error
		if res.Breadcrumbs, crumbErr = s.breadcrumbs(ctx, s.db.Read(), &hit.routeHit, locale, res.Path); crumbErr != nil {
			return nil, crumbErr
		}
		s.addAlternates(out, w, hit)
	}
	return out, nil
}

// locHit is a route found for a path, with the item's type and every current route of the item (for alias and
// history targets, alternates and canonical), all read by the one lookup query.
type locHit struct {
	routeHit
	typ string
	cur itemRoutes
}

// resolveInChannel tries the path in each language of the channel, in order; the first hit decides the locale.
// A hit costs the lookup query, plus the ancestor query for breadcrumbs that Locate adds; a miss costs one.
func (s *service) resolveInChannel(ctx context.Context, w *webChannel, path string) (string, *Resolution, *locHit, error) {
	if err := s.checkSite(w.site); err != nil {
		return "", nil, nil, err
	}
	p, ok := resolvablePath(path)
	if !ok || len(w.ch.Languages) == 0 {
		return "", notFoundResolution(), nil, nil
	}
	rd := s.db.Read()
	hits, err := lookupLocate(ctx, rd, w.site, p, w.ch.Languages)
	if err != nil {
		return "", nil, nil, err
	}
	for _, l := range w.ch.Languages {
		h := pickHit(hits, l)
		if h == nil {
			continue
		}
		res, err := s.hitResolution(ctx, rd, w, l, h)
		if err != nil {
			return "", nil, nil, err
		}
		if res.Kind == ResolvedNotFound {
			continue
		}
		if res.Kind == ResolvedRedirect {
			loc, ok := w.siteLink(res.Location)
			if !ok {
				continue
			}
			res.Location = loc
		}
		return l, res, h, nil
	}
	return "", notFoundResolution(), nil, nil
}

// pickHit chooses the route that serves a path in a locale: the locale's own row or the every-locale (”)
// row, the higher-ranked state first and the locale's own row on a tie.
func pickHit(hits map[string]*locHit, locale string) *locHit {
	own, every := hits[locale], hits[""]
	switch {
	case own == nil:
		return every
	case every == nil:
		return own
	case rank(every.State) > rank(own.State):
		return every
	}
	return own
}

// lookupLocate is the first query of Locate: every route at a path in the channel's languages (and the
// every-locale manual redirects), with its page node, the item's type and the item's current routes.
func lookupLocate(ctx context.Context, q store.SQL, site, p string, langs []string) (map[string]*locHit, error) {
	locales := make([]string, 0, len(langs)+1)
	for _, l := range langs {
		if !slices.Contains(locales, l) && l != "" {
			locales = append(locales, l)
		}
	}
	locales = append(locales, "")
	args := []any{site, p}
	for _, l := range locales {
		args = append(args, l)
	}
	hits := map[string]*locHit{}
	err := each(ctx, q, `SELECT r.locale, r.state, r.item_id, r.node_id, r.target, r.status, n.path, n.kind, i.type,
CASE WHEN n.kind = 'mount' THEN (SELECT l.name FROM item_version_locale l
    WHERE l.item_id = r.item_id AND l.version = i.published_version AND (l.locale = r.locale OR l.locale = i.main_locale)
    ORDER BY CASE WHEN l.locale = r.locale THEN 0 ELSE 1 END LIMIT 1) END,
c.site, c.locale, c.path
FROM route r
LEFT JOIN page_node n ON n.id = r.node_id
LEFT JOIN item i ON i.id = r.item_id
LEFT JOIN route c ON c.item_id = r.item_id AND c.state = 'current'
WHERE r.site = ? AND r.path = ? AND r.locale IN (`+placeholders(len(locales))+")", args,
		func(r store.Rows) error {
			var loc, state string
			var h routeHit
			var target, nodePath, kind, typ, name, cSite, cLocale, cPath *string
			err := r.Scan(&loc, &state, &h.Item, &h.Node, &target, &h.Status, &nodePath, &kind, &typ, &name,
				&cSite, &cLocale, &cPath)
			if err != nil {
				return err
			}
			hit := hits[loc]
			if hit == nil {
				h.Site, h.Locale, h.Path, h.State = site, loc, p, state
				if target != nil {
					h.Target = *target
				}
				if nodePath != nil {
					h.hasNode, h.nodePath, h.nodeKind = true, *nodePath, *kind
				}
				if name != nil {
					h.itemName = *name
				}
				hit = &locHit{routeHit: h, cur: itemRoutes{}}
				if typ != nil {
					hit.typ = *typ
				}
				hits[loc] = hit
			}
			if cSite != nil {
				if hit.cur[*cSite] == nil {
					hit.cur[*cSite] = map[string]string{}
				}
				hit.cur[*cSite][*cLocale] = *cPath
			}
			return nil
		})
	return hits, err
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// hitResolution turns the route found for language l into a resolution, without breadcrumbs. Items outside the
// channel's collections never resolve, and neither do history or alias targets that are.
func (s *service) hitResolution(ctx context.Context, q store.SQL, w *webChannel, l string, h *locHit) (*Resolution, error) {
	allowed := w.ch.AllowsCollection(h.typ)
	switch h.State {
	case StateCurrent, StateAlias:
		if h.Node == nil || h.Item == nil || !h.hasNode || !allowed {
			return notFoundResolution(), nil
		}
		res := &Resolution{Kind: ResolvedPage, Item: h.Item, Node: h.Node, Path: h.Path, Status: 200}
		if cur, ok := h.cur[w.site][l]; ok && h.State == StateAlias {
			res.Path = cur
		}
		if h.nodeKind == KindMount {
			res.Kind = ResolvedItem
		}
		return res, nil
	case StateRedirect:
		cur, ok := h.cur[w.site][l]
		if h.Item == nil || !allowed || !ok {
			return notFoundResolution(), nil
		}
		return &Resolution{Kind: ResolvedRedirect, Item: h.Item, Path: cur, Status: statusOr(h.Status), Location: cur}, nil
	}
	res := &Resolution{Kind: ResolvedRedirect, Status: statusOr(h.Status), Location: h.Target}
	rest, ok := strings.CutPrefix(h.Target, "item:")
	if !ok {
		return res, nil
	}
	id, err := ids.Parse(rest)
	if err != nil {
		return notFoundResolution(), nil
	}
	// A manual redirect to an item is rare enough to cost one more query.
	var typ, cur string
	err = q.QueryRow(ctx, `SELECT i.type, r.path FROM item i JOIN route r ON r.item_id = i.id AND r.site = ?
AND r.locale = ? AND r.state = 'current' WHERE i.id = ?`, w.site, l, id).Scan(&typ, &cur)
	if isNoRows(err) || (err == nil && !w.ch.AllowsCollection(typ)) {
		return notFoundResolution(), nil
	}
	if err != nil {
		return nil, err
	}
	res.Item, res.Location, res.Path = &id, cur, cur
	return res, nil
}

// addAlternates fills the canonical URL and the hreflang set of a page or item from the routes the lookup read.
func (s *service) addAlternates(out *Located, w *webChannel, h *locHit) {
	peers := s.web.peers(w)
	out.Alternates = alternates(w, peers, h.cur, h.typ, out.Locale, out.Resolution.Path)
	out.Canonical = canonicalURL(w, peers, h.cur, h.typ, out.Locale, out.Resolution.Path)
}

// itemRoutes maps site → locale → current path of one item.
type itemRoutes map[string]map[string]string

// currentRoutes reads every current route of an item, across sites and locales, with the item's type, in one
// query. The type is empty when the item has no route.
func (s *service) currentRoutes(ctx context.Context, q store.SQL, item ids.UUID) (itemRoutes, string, error) {
	out := itemRoutes{}
	var typ string
	err := each(ctx, q, `SELECT r.site, r.locale, r.path, i.type FROM route r JOIN item i ON i.id = r.item_id
WHERE r.item_id = ? AND r.state = 'current'`, []any{item},
		func(r store.Rows) error {
			var site, locale, path string
			if err := r.Scan(&site, &locale, &path, &typ); err != nil {
				return err
			}
			if out[site] == nil {
				out[site] = map[string]string{}
			}
			out[site][locale] = path
			return nil
		})
	return out, typ, err
}

// firstRoute is the first of the channel's languages in which the item has a route in the channel's site.
func (r itemRoutes) firstRoute(w *webChannel) (locale, path string, ok bool) {
	for _, l := range w.ch.Languages {
		if p, found := r[w.site][l]; found {
			return l, p, true
		}
	}
	return "", "", false
}

// alternates lists the hreflang set in channel-name order: the current channel with the locale it shows, and
// every other peer that allows the item's type and where the item has a route in the peer's first language.
// A locale is listed once; the current channel owns its own, and the first peer by name owns the rest.
func alternates(cur *webChannel, peers []*webChannel, r itemRoutes, typ, shown, curPath string) []Alternate {
	var out []Alternate
	taken := map[string]bool{shown: true}
	for _, p := range peers {
		if p.ch == cur.ch {
			out = append(out, Alternate{Channel: p.ch.Name, Locale: shown, URL: p.abs(curPath)})
			continue
		}
		if !p.ch.AllowsCollection(typ) || len(p.ch.Languages) == 0 {
			continue
		}
		l := p.ch.Languages[0]
		if path, ok := r[p.site][l]; ok && !taken[l] {
			taken[l] = true
			out = append(out, Alternate{Channel: p.ch.Name, Locale: l, URL: p.abs(path)})
		}
	}
	return out
}

// canonicalURL is the item's URL in the peer whose first language is the shown locale (the current channel
// preferred), else the current URL.
func canonicalURL(cur *webChannel, peers []*webChannel, r itemRoutes, typ, locale, curPath string) string {
	own := func(p *webChannel) (string, bool) {
		path, ok := r[p.site][locale]
		return path, ok && p.ch.AllowsCollection(typ) && len(p.ch.Languages) > 0 && p.ch.Languages[0] == locale
	}
	if path, ok := own(cur); ok {
		return cur.abs(path)
	}
	for _, p := range peers {
		if path, ok := own(p); ok {
			return p.abs(path)
		}
	}
	return cur.abs(curPath)
}

func (s *service) itemURL(ctx context.Context, ch *channel.Channel, item ids.UUID, locale string) (string, bool, error) {
	if !slices.Contains(ch.Languages, locale) {
		return "", false, nil
	}
	w := s.web.of(ch)
	var path, typ string
	err := s.db.Read().QueryRow(ctx, `SELECT r.path, i.type FROM route r JOIN item i ON i.id = r.item_id
WHERE r.site = ? AND r.locale = ? AND r.item_id = ? AND r.state = 'current'`, w.site, locale, item).Scan(&path, &typ)
	if isNoRows(err) || (err == nil && !ch.AllowsCollection(typ)) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return w.abs(path), true, nil
}

func (s *service) linkURL(ctx context.Context, from *channel.Channel, item ids.UUID) (string, error) {
	routes, typ, err := s.currentRoutes(ctx, s.db.Read(), item)
	if err != nil {
		return "", err
	}
	if u, ok := s.web.link(from, typ, routes); ok {
		return u, nil
	}
	return "", notFound("item %s has no route in any web channel", item)
}

// link builds the URL to an item from a channel: relative in the channel itself, else absolute in the best
// channel (same group, then same site, then any) that allows the item's type and can produce an absolute URL,
// preferring a language of the origin.
func (x *webIndex) link(from *channel.Channel, typ string, r itemRoutes) (string, bool) {
	w := x.of(from)
	if _, ok := from.Options[optMatch].(matcher); ok && from.AllowsCollection(typ) {
		if _, path, found := r.firstRoute(w); found {
			return w.rel(path), true
		}
	}
	tiers := [](func(*webChannel) bool){
		func(p *webChannel) bool { return w.ch.Group != "" && p.ch.Group == w.ch.Group },
		func(p *webChannel) bool { return p.site == w.site },
		func(*webChannel) bool { return true },
	}
	for _, in := range tiers {
		if u, ok := x.bestIn(in, w, typ, r); ok {
			return u, true
		}
	}
	return "", false
}

// bestIn picks, among the web channels the filter accepts, the hit in the earliest language of from (else the
// first by name). Channels that cannot form an absolute URL are skipped, from itself excepted.
func (x *webIndex) bestIn(in func(*webChannel) bool, from *webChannel, typ string, r itemRoutes) (string, bool) {
	prefer := from.ch.Languages
	best, bestRank, bestURL := false, 0, ""
	for _, p := range x.byName {
		if !in(p) || !p.ch.AllowsCollection(typ) || (p.origin() == "" && p.ch != from.ch) {
			continue
		}
		l, path, ok := r.firstRoute(p)
		if !ok {
			continue
		}
		rank := slices.Index(prefer, l)
		if rank < 0 {
			rank = len(prefer)
		}
		if !best || rank < bestRank {
			best, bestRank, bestURL = true, rank, p.abs(path)
		}
	}
	return bestURL, best
}
