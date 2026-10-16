package pages

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

const (
	codeSlugTaken    = "slug_taken"
	codeInvalidPath  = "invalid_path"
	codePathTooLong  = "path_too_long"
	codeRedirectLoop = "redirect_loop"
)

func str(doc map[string]any, key string) string {
	s, _ := doc[key].(string)
	return s
}

func (s *service) slugRowOf(typ string, doc map[string]any) slugRow {
	name := ""
	if t, ok := s.schema.Type(typ); ok {
		name = s.schema.Name(t, doc)
	}
	return slugRow{Slug: str(doc, "slug"), Name: name, Vanity: str(doc, "vanityPath")}
}

// itemClass tells the hooks what a content type is to the module.
type itemClass int

const (
	classCollection itemClass = iota // only matters when something is mounted on the type
	classPage
	classRedirect
)

func (s *service) classOf(typ string) itemClass {
	switch {
	case s.isPageType(typ):
		return classPage
	case typ == redirectType:
		return classRedirect
	}
	return classCollection
}

// beforePublish blocks a publish whose paths another item already holds.
func (s *service) beforePublish(hc *hooks.Context) error {
	switch s.classOf(hc.Item.Type) {
	case classPage:
		return s.checkPage(hc)
	case classRedirect:
		return s.checkRedirect(hc)
	}
	if err := s.checkMembership(hc); err != nil {
		return err
	}
	clashes, err := s.previewMounted(hc, hc.Tx, hc.Item.ID, hc.Item.Type, hc.Locale, hc.Doc)
	if err != nil {
		return fmt.Errorf("pages: check routes of %s: %w", hc.Item.ID, err)
	}
	field := s.pathField(hc.Item.Type)
	for _, c := range clashes {
		hc.Errors.Add(field, codeSlugTaken, "%s", c)
	}
	return nil
}

// pathField is the field blamed when a mounted item's path is taken: slug when the type has one.
func (s *service) pathField(typ string) schema.Path {
	if t, ok := s.schema.Type(typ); ok {
		if _, has := t.Field("slug"); !has && len(t.Fields) > 0 {
			return schema.Path(t.Fields[0].Name)
		}
	}
	return "slug"
}

// maxClashErrors bounds the field errors one publish reports for a subtree.
const maxClashErrors = 5

func (s *service) checkPage(hc *hooks.Context) error {
	q := hc.Tx.SQL()
	row := s.slugRowOf(hc.Item.Type, hc.Doc)
	if row.Vanity != "" {
		if _, err := route.Clean(row.Vanity); err != nil {
			hc.Errors.Add("vanityPath", codeInvalidPath, "must be a site path such as /promo: %v", err)
			return nil
		}
	}
	n, ok, err := s.lockedNodeByItem(hc, hc.Tx, hc.Item.ID)
	if err != nil || !ok {
		return err
	}
	unchanged, err := slugUnchanged(hc, q, n, hc.Locale, hc.Item.MainLocale, row)
	if err != nil || unchanged {
		return err
	}
	n.MainLocale = hc.Item.MainLocale
	g := newGraph(q)
	nodes, err := g.loadSubtree(hc, n)
	if err != nil {
		return err
	}
	g.nodes[n.ID].MainLocale = hc.Item.MainLocale
	g.setSlug(n.ID, hc.Locale, row)
	locales := []string{hc.Locale}
	if hc.Locale == hc.Item.MainLocale {
		locales = nil
	}
	existing, err := pageRoutesIn(hc, q, n.Path, locales)
	if err != nil {
		return err
	}
	plan, err := diffRoutes(hc, q, existing, g.pageWants(nodes, locales))
	if err != nil {
		return err
	}
	reportClashes(hc, plan.clashes)
	for i, w := range g.tooLong {
		if i == maxClashErrors {
			break
		}
		hc.Errors.Add("slug", codePathTooLong, "the path of %s in %q would be %d bytes; paths are at most %d",
			pageLabel(w, hc.Item.ID), w.Locale, len(w.Path), route.MaxPath)
	}
	return nil
}

func pageLabel(w want, self ids.UUID) string {
	if w.Item != nil && *w.Item != self {
		return "a page below this one"
	}
	return "this page"
}

// slugUnchanged reports whether a publish leaves the node's stored slug, vanity path and main locale as they
// are, so no route can change.
func slugUnchanged(ctx context.Context, q store.SQL, n nodeRow, locale, mainLocale string, row slugRow) (bool, error) {
	var old slugRow
	err := q.QueryRow(ctx, "SELECT slug, name, vanity FROM page_slug WHERE node_id = ? AND locale = ?", n.ID, locale).
		Scan(&old.Slug, &old.Name, &old.Vanity)
	switch {
	case err == nil:
		return old.Slug == row.Slug && old.Vanity == row.Vanity && n.MainLocale == mainLocale, nil
	case isNoRows(err):
		return false, nil
	}
	return false, err
}

func reportClashes(hc *hooks.Context, clashes []clash) {
	for i, c := range clashes {
		if i == maxClashErrors {
			break
		}
		field := schema.Path("slug")
		if c.want.State == StateAlias && c.want.Item != nil && *c.want.Item == hc.Item.ID {
			field = "vanityPath"
		}
		msg := c.String()
		if c.want.Item == nil || *c.want.Item != hc.Item.ID {
			msg += " by a page below this one"
		}
		hc.Errors.Add(field, codeSlugTaken, "%s", msg)
	}
}

func (s *service) afterPublish(hc *hooks.Context) error {
	var changes []change
	var err error
	switch s.classOf(hc.Item.Type) {
	case classPage:
		changes, err = s.syncPage(hc, hc.Tx, hc.Item.ID, hc.Item.MainLocale, hc.Locale, s.slugRowOf(hc.Item.Type, hc.Doc))
	case classRedirect:
		changes, err = s.syncRedirect(hc, hc.Tx.SQL(), hc.Item.ID, hc.Doc)
	default:
		changes, err = s.syncMountedItem(hc, hc.Tx, hc.Item.ID, hc.Item.Type, hc.Locale, hc.Doc)
	}
	if err != nil {
		return fmt.Errorf("pages: update routes of %s: %w", hc.Item.ID, err)
	}
	return s.emitChanges(hc, hc.Tx, changes)
}

func (s *service) beforeDelete(hc *hooks.Context) error {
	return s.refuseChildren(hc, "move or delete its children first")
}

// beforeUnpublish refuses to take a page that has children offline (unpublishing it or moving it to the
// trash): their paths hang below it.
func (s *service) beforeUnpublish(hc *hooks.Context) error {
	return s.refuseChildren(hc, "move or delete its children first, or unpublish it later")
}

func (s *service) refuseChildren(hc *hooks.Context, advice string) error {
	if !s.isPageType(hc.Item.Type) {
		return nil
	}
	q := hc.Tx.SQL()
	n, ok, err := loadNodeByItem(hc, q, hc.Item.ID)
	if err != nil || !ok {
		return err
	}
	children, err := countChildren(hc, q, n.ID)
	if err != nil {
		return err
	}
	if children > 0 {
		return hasChildren("this page has %d child node(s): %s", children, advice)
	}
	return nil
}

// afterUnpublish drops the routes and memberships of an item that is no longer published. A page keeps its
// node, so restoring and publishing it again puts it back where it was.
func (s *service) afterUnpublish(hc *hooks.Context) error {
	changes, err := s.dropRoutes(hc, hc.Tx, hc.Item.ID)
	if err != nil {
		return fmt.Errorf("pages: remove routes of %s: %w", hc.Item.ID, err)
	}
	return s.emitChanges(hc, hc.Tx, changes)
}

func (s *service) afterDelete(hc *hooks.Context) error {
	changes, err := s.removeItem(hc, hc.Tx, hc.Item.ID)
	if err != nil {
		return fmt.Errorf("pages: remove routes of %s: %w", hc.Item.ID, err)
	}
	return s.emitChanges(hc, hc.Tx, changes)
}

// checkRedirect validates a manual redirect and its collisions.
func (s *service) checkRedirect(hc *hooks.Context) error {
	q := hc.Tx.SQL()
	r, problems := s.redirectOf(hc.Doc)
	for _, p := range problems {
		hc.Errors.Add(p.field, p.code, "%s", p.msg)
	}
	if len(problems) > 0 {
		return nil
	}
	if err := s.lockSite(hc, hc.Tx, r.Site); err != nil {
		return err
	}
	held, err := loadRoutesAt(hc, q, []routeKey{r.key()})
	if err != nil {
		return err
	}
	rows := held[r.key()]
	if r.Locale == "" {
		var anyLocale []routeRow
		anyLocale, err = routesAtAnyLocale(hc, q, r.Site, r.From)
		if err != nil {
			return err
		}
		rows = append(rows, anyLocale...)
	}
	for _, h := range rows {
		if h.State == StateRedirect || (h.State == StateManual && sameOwner(h.Item, &hc.Item.ID)) {
			continue
		}
		by := "a page or mounted item"
		if h.State == StateManual {
			by = "another redirect"
		}
		hc.Errors.Add("from", codeSlugTaken, "the path %s is already used by %s", r.From, by)
		return nil
	}
	loop, err := s.redirectLoops(hc, q, r, hc.Item.ID)
	if err != nil {
		return err
	}
	if loop != "" {
		hc.Errors.Add("to", codeRedirectLoop, "%s", loop)
	}
	return nil
}

type problem struct {
	field schema.Path
	code  string
	msg   string
}

// redirectDef is a redirect item's document, normalised.
type redirectDef struct {
	Site   string
	Locale string
	From   string
	To     string
	Status int
}

func (r redirectDef) key() routeKey { return routeKey{r.Site, r.Locale, r.From} }

// redirectStatuses are the statuses a redirect may use.
var redirectStatuses = map[int]bool{301: true, 302: true, 307: true, 308: true}

func (s *service) redirectOf(doc map[string]any) (redirectDef, []problem) {
	var r redirectDef
	var ps []problem
	r.Site = str(doc, "site")
	if r.Site == "" {
		r.Site = s.sites[0]
	}
	if err := s.checkSite(r.Site); err != nil {
		ps = append(ps, problem{"site", "unknown_site", fmt.Sprintf("unknown site %q (sites: %s)", r.Site, strings.Join(s.sites, ", "))})
	}
	r.Locale = str(doc, "forLocale")
	r.From = str(doc, "from")
	if err := checkLegacyPath(r.From); err != nil {
		ps = append(ps, problem{"from", codeInvalidPath, "must be a path starting with / such as /old-page: " + err.Error()})
	}
	r.To = str(doc, "to")
	if err := checkTarget(r.To); err != nil {
		ps = append(ps, problem{"to", "invalid_target", err.Error()})
	}
	r.Status = 301
	if v, ok := doc["statusCode"]; ok {
		n, err := strconv.Atoi(fmt.Sprint(v))
		if err != nil || !redirectStatuses[n] {
			ps = append(ps, problem{"statusCode", "invalid_status", "must be 301, 302, 307 or 308"})
		} else {
			r.Status = n
		}
	}
	return r, ps
}

// unsafeText reports a backslash or a control character, which browsers read as other characters in a URL
// (`/\host` is a link to another site).
func unsafeText(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r == '\\' || r < 0x20 || r == 0x7f })
}

// checkLegacyPath accepts the paths of old sites that a redirect may match: anything starting with "/" that
// has no whitespace, control character or backslash, within the path length limit. Matching is exact.
func checkLegacyPath(p string) error {
	switch {
	case !strings.HasPrefix(p, "/"):
		return fmt.Errorf("a path starts with /")
	case len(p) > route.MaxPath:
		return fmt.Errorf("a path is at most %d bytes", route.MaxPath)
	case unsafeText(p) || strings.ContainsFunc(p, unicode.IsSpace):
		return fmt.Errorf("a path has no whitespace, control characters or backslashes")
	}
	return nil
}

// checkTarget accepts an absolute http(s) URL, a site path starting with "/", or item:<uuid>.
func checkTarget(to string) error {
	if unsafeText(to) {
		return fmt.Errorf("a target cannot contain control characters or backslashes")
	}
	switch {
	case strings.HasPrefix(to, "item:"):
		if _, err := ids.Parse(strings.TrimPrefix(to, "item:")); err != nil {
			return fmt.Errorf("item: targets need an item id")
		}
	case strings.HasPrefix(to, "/") && !strings.HasPrefix(to, "//"):
		if strings.ContainsFunc(to, unicode.IsSpace) {
			return fmt.Errorf("a site path cannot contain spaces")
		}
	default:
		u, err := url.Parse(to)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("must be an absolute http(s) URL, a site path starting with /, or item:<id>")
		}
	}
	return nil
}

// maxRedirectHops bounds the manual redirects a request may follow.
const maxRedirectHops = 10

// redirectLoops follows the manual redirects that start at the target of r (the redirect item `self` being
// published replaces its own earlier row) and describes a loop or a chain longer than maxRedirectHops; it
// returns "" when the chain ends.
func (s *service) redirectLoops(ctx context.Context, q store.SQL, r redirectDef, self ids.UUID) (string, error) {
	frontier := []string{}
	if path, ok := sitePathOf(r.To); ok {
		frontier = append(frontier, path)
	}
	for hop := 1; len(frontier) > 0; hop++ {
		if slices.Contains(frontier, r.From) {
			return fmt.Sprintf("this redirect would loop back to %s", r.From), nil
		}
		if hop > maxRedirectHops {
			return fmt.Sprintf("this redirect starts a chain of more than %d redirects", maxRedirectHops), nil
		}
		var next []string
		for _, p := range frontier {
			query := "SELECT target FROM route WHERE site = ? AND path = ? AND state = 'manual' AND item_id <> ?"
			args := []any{r.Site, p, self}
			if r.Locale != "" {
				query += " AND (locale = ? OR locale = '')"
				args = append(args, r.Locale)
			}
			err := each(ctx, q, query, args, func(rows store.Rows) error {
				var target *string
				if err := rows.Scan(&target); err != nil {
					return err
				}
				if target != nil {
					if np, ok := sitePathOf(*target); ok && !slices.Contains(next, np) {
						next = append(next, np)
					}
				}
				return nil
			})
			if err != nil {
				return "", err
			}
		}
		frontier = next
	}
	return "", nil
}

// sitePathOf is the path a redirect target names on the same site, without query and fragment.
func sitePathOf(target string) (string, bool) {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "", false
	}
	if i := strings.IndexAny(target, "?#"); i >= 0 {
		target = target[:i]
	}
	return target, true
}

// checkMembership refuses a publish whose document joins a mount outside the publisher's pages limits: mount
// membership follows the document, so changing a filter field would otherwise move the item into another
// subtree. The content service checks the item's existing placements; this adds the mounts it would join.
func (s *service) checkMembership(hc *hooks.Context) error {
	if !s.restricted(hc) {
		return nil
	}
	ctx := store.WithTx(hc.Context, hc.Tx)
	it := store.Item{ID: hc.Item.ID, Type: hc.Item.Type}
	return s.svcs.Access.Can(ctx, access.PolicyContentPublish, access.Target{Type: it.Type, Item: &it,
		Locales: []string{hc.Locale}, Doc: hc.Doc})
}
