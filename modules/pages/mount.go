package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
)

// checkMountSpec validates a mount spec and returns its parsed route pattern.
func (s *service) checkMountSpec(spec *MountSpec) (*route.Pattern, error) {
	if spec.Segment == "" || strings.Contains(spec.Segment, "/") {
		return nil, badRequest("mount segment %q must be a single path segment", spec.Segment)
	}
	if _, err := route.Clean("/" + spec.Segment); err != nil {
		return nil, badRequest("mount segment %q must be a slug (lowercase letters, digits and single dashes)", spec.Segment)
	}
	typ, ok := s.schema.Type(spec.Collection)
	switch {
	case !ok:
		return nil, badRequest("unknown collection %q", spec.Collection)
	case typ.Page:
		return nil, badRequest("%q is a page type; only collections can be mounted", spec.Collection)
	case typ.Name == redirectType:
		return nil, badRequest("redirects cannot be mounted")
	}
	pat, err := route.Parse(spec.Route)
	if err != nil {
		return nil, badRequest("mount route: %v", err)
	}
	for _, f := range pat.Fields() {
		if _, ok := typ.Field(f); !ok {
			return nil, badRequest("mount route: %q has no field %q", spec.Collection, f)
		}
	}
	if len(spec.Filter) > maxFilterKeys {
		return nil, badRequest("a mount filter takes at most %d fields", maxFilterKeys)
	}
	if err := checkLabels(spec.Label); err != nil {
		return nil, err
	}
	if err := checkRedirectField(typ, spec); err != nil {
		return nil, err
	}
	for _, field := range slices.Sorted(maps.Keys(spec.Filter)) {
		if _, ok := typ.Field(field); !ok {
			return nil, badRequest("mount filter: %q has no top-level field %q", spec.Collection, field)
		}
		if err := checkFilterValue(spec.Filter[field]); err != nil {
			return nil, badRequest("mount filter %q: %v", field, err)
		}
	}
	return pat, nil
}

// checkRedirectField validates redirectField (a top-level string or url field of the collection) and
// redirectStatus.
func checkRedirectField(typ *schema.Type, spec *MountSpec) error {
	if spec.RedirectField == "" {
		if spec.RedirectStatus != 0 {
			return badRequest("redirectStatus needs a redirectField")
		}
		return nil
	}
	f, ok := typ.Field(spec.RedirectField)
	switch {
	case !ok:
		return badRequest("mount redirectField: %q has no top-level field %q", spec.Collection, spec.RedirectField)
	case f.Type != "string" && f.Type != "url":
		return badRequest("mount redirectField: %q is a %s field; it must be a string or url field", spec.RedirectField, f.Type)
	case spec.RedirectStatus != 0 && !redirectStatuses[spec.RedirectStatus]:
		return badRequest("mount redirectStatus must be 301, 302, 307 or 308")
	}
	return nil
}

// defaultMountRedirect is the status of a mounted item's redirect unless the mount sets one.
const defaultMountRedirect = 302

func mountRedirectStatus(spec *MountSpec) int {
	if spec.RedirectStatus != 0 {
		return spec.RedirectStatus
	}
	return defaultMountRedirect
}

// redirectOfDoc is where a mounted item sends visitors: its redirect field when it holds an absolute http(s) URL
// or a site path, else "" (the item shows itself).
func redirectOfDoc(spec *MountSpec, doc map[string]any) string {
	if spec.RedirectField == "" {
		return ""
	}
	v, _ := doc[spec.RedirectField].(string)
	if v == "" || strings.HasPrefix(v, "item:") || checkTarget(v) != nil {
		return ""
	}
	return v
}

// Bounds on what one node may carry.
const (
	maxFilterKeys  = 16
	maxLabelLocale = 50
	maxLabelRunes  = 200
)

func checkLabels(labels map[string]string) error {
	if len(labels) > maxLabelLocale {
		return badRequest("a label takes at most %d locales", maxLabelLocale)
	}
	for _, loc := range slices.Sorted(maps.Keys(labels)) {
		if utf8.RuneCountInString(labels[loc]) > maxLabelRunes || utf8.RuneCountInString(loc) > maxLabelRunes {
			return badRequest("the %q label is longer than %d characters", loc, maxLabelRunes)
		}
	}
	return nil
}

func checkFilterValue(v any) error {
	if list, ok := v.([]any); ok {
		if len(list) == 0 {
			return fmt.Errorf("an empty list matches nothing")
		}
		for _, e := range list {
			if _, ok := scalarKey(e); !ok {
				return fmt.Errorf("values must be strings, numbers or booleans")
			}
		}
		return nil
	}
	if _, ok := scalarKey(v); !ok {
		return fmt.Errorf("the value must be a string, number, boolean or a list of them")
	}
	return nil
}

// scalarKey makes strings, numbers and booleans comparable across JSON decodings.
func scalarKey(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return "s" + x, true
	case bool:
		return "b" + strconv.FormatBool(x), true
	case json.Number:
		f, err := x.Float64()
		return "n" + strconv.FormatFloat(f, 'g', -1, 64), err == nil && !math.IsNaN(f)
	case float64:
		return "n" + strconv.FormatFloat(x, 'g', -1, 64), !math.IsNaN(x)
	case int:
		return "n" + strconv.Itoa(x), true
	case int64:
		return "n" + strconv.FormatInt(x, 10), true
	}
	return "", false
}

// matches reports whether doc passes the filter: every field equals its value, or one of its values; a list
// field matches when it contains the value.
func matches(filter map[string]any, doc map[string]any) bool {
	for field, want := range filter {
		have, ok := doc[field]
		if !ok {
			return false
		}
		if list, isList := want.([]any); isList {
			if !slices.ContainsFunc(list, func(w any) bool { return valueIs(have, w) }) {
				return false
			}
		} else if !valueIs(have, want) {
			return false
		}
	}
	return true
}

func valueIs(have, want any) bool {
	wk, ok := scalarKey(want)
	if !ok {
		return false
	}
	if list, isList := have.([]any); isList {
		return slices.ContainsFunc(list, func(h any) bool { k, ok := scalarKey(h); return ok && k == wk })
	}
	hk, ok := scalarKey(have)
	return ok && hk == wk
}

// relPath fills the mount's route pattern from an item's document.
func relPath(pat *route.Pattern, item ids.UUID, doc map[string]any, allow func(field string) string) (string, bool) {
	rel, err := pat.FillAllow(func(field string) (string, bool) {
		if field == "id" {
			return item.String(), true
		}
		switch v := doc[field].(type) {
		case string:
			return v, v != ""
		case json.Number:
			return v.String(), true
		}
		return "", false
	}, allow)
	return rel, err == nil
}

const mountItemCols = "node_id, item_id, locale, rel, redirect"

// mountSet is a collection's mounts with their parsed specs.
type mountSet struct {
	rows []nodeRow
	spec map[ids.UUID]*MountSpec
	pat  map[ids.UUID]*route.Pattern
	// allow gives the extra joining characters of a slug field of the collection.
	allow func(field string) string
}

func newMountSet(rows []nodeRow) (*mountSet, error) {
	m := &mountSet{rows: rows, spec: map[ids.UUID]*MountSpec{}, pat: map[ids.UUID]*route.Pattern{}}
	for _, r := range rows {
		var spec MountSpec
		if err := json.Unmarshal(r.Data, &spec); err != nil {
			return nil, fmt.Errorf("pages: mount %s: bad data: %w", r.ID, err)
		}
		pat, err := route.Parse(spec.Route)
		if err != nil {
			return nil, fmt.Errorf("pages: mount %s: %w", r.ID, err)
		}
		m.spec[r.ID], m.pat[r.ID] = &spec, pat
	}
	return m, nil
}

func (m *mountSet) nodeIDs() []ids.UUID { return nodeIDs(m.rows) }

func nodeIDs(rows []nodeRow) []ids.UUID {
	out := make([]ids.UUID, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

// memberships lists the mounts an item belongs to in a locale.
func (m *mountSet) memberships(item ids.UUID, locale string, doc map[string]any) []membership {
	var out []membership
	for _, r := range m.rows {
		if !matches(m.spec[r.ID].Filter, doc) {
			continue
		}
		if rel, ok := relPath(m.pat[r.ID], item, doc, m.allow); ok {
			out = append(out, membership{r.ID, item, locale, rel, redirectOfDoc(m.spec[r.ID], doc)})
		}
	}
	return out
}

// publishedDoc is one locale of a published version: merged shared and locale fields.
type publishedDoc struct {
	Locale string
	Name   string
	Doc    map[string]any
}

// publishedDocs reads the current published version of an item through the transaction.
func publishedDocs(ctx context.Context, c store.ContentStore, it store.Item) ([]publishedDoc, error) {
	if it.PublishedVersion == 0 {
		return nil, nil
	}
	v, err := c.GetVersion(ctx, it.ID, it.PublishedVersion)
	if err != nil {
		return nil, fmt.Errorf("pages: read version %d of %s: %w", it.PublishedVersion, it.ID, err)
	}
	shared := map[string]any{}
	if len(v.Shared) > 0 {
		if shared, err = schema.DecodeDoc(v.Shared); err != nil {
			return nil, fmt.Errorf("pages: item %s: %w", it.ID, err)
		}
	}
	out := make([]publishedDoc, 0, len(v.Locales))
	for _, l := range v.Locales {
		doc := make(map[string]any, len(shared)+8)
		for k, x := range shared {
			doc[k] = x
		}
		if len(l.Data) > 0 {
			loc, err := schema.DecodeDoc(l.Data)
			if err != nil {
				return nil, fmt.Errorf("pages: item %s: %w", it.ID, err)
			}
			for k, x := range loc {
				doc[k] = x
			}
		}
		out = append(out, publishedDoc{l.Locale, l.Name, doc})
	}
	slices.SortFunc(out, func(a, b publishedDoc) int { return strings.Compare(a.Locale, b.Locale) })
	return out, nil
}

func loadMemberships(ctx context.Context, q store.SQL, mounts []ids.UUID, item *ids.UUID, locales []string) ([]membership, error) {
	var out []membership
	err := chunks(mounts, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		query := "SELECT node_id, item_id, locale, rel, redirect FROM page_mount_item WHERE node_id IN " + ph
		if item != nil {
			query += " AND item_id = ?"
			args = append(args, *item)
		}
		if locales != nil {
			lph, largs := inArgs(locales)
			query += " AND locale IN " + lph
			args = append(args, largs...)
		}
		return each(ctx, q, query, args, func(r store.Rows) error {
			var m membership
			err := r.Scan(&m.Node, &m.Item, &m.Locale, &m.Rel, &m.Redirect)
			out = append(out, m)
			return err
		})
	})
	return out, err
}

// recomputeCollection reconciles the routes of every item mounted below the collection's mounts of a site.
// item and locales narrow it to what changed.
func (s *service) recomputeCollection(ctx context.Context, q store.SQL, site, collection string, item *ids.UUID, locales []string) error {
	rows, err := loadMounts(ctx, q, site, collection)
	if err != nil || len(rows) == 0 {
		return err
	}
	return s.recomputeMounts(ctx, q, rows, item, locales)
}

func (s *service) recomputeMounts(ctx context.Context, q store.SQL, rows []nodeRow, item *ids.UUID, locales []string) error {
	g := newGraph(q)
	if err := g.loadNodesWithAncestors(ctx, rows); err != nil {
		return err
	}
	list := nodeIDs(rows)
	ms, err := loadMemberships(ctx, q, list, item, locales)
	if err != nil {
		return err
	}
	existing, err := mountRoutes(ctx, q, list, item, locales)
	if err != nil {
		return err
	}
	plan, err := diffRoutes(ctx, q, existing, g.mountWants(ms, locales))
	if err != nil {
		return err
	}
	return s.apply(ctx, q, plan, false)
}

// rebuildMembership recomputes which published items belong to a mount, from the content store.
func (s *service) rebuildMembership(ctx context.Context, tx store.Tx, node nodeRow, spec *MountSpec) error {
	q := tx.SQL()
	if _, err := q.Exec(ctx, "DELETE FROM page_mount_item WHERE node_id = ?", node.ID); err != nil {
		return err
	}
	rows, err := s.membershipRows(ctx, tx, node.ID, spec)
	if err != nil {
		return err
	}
	_, err = insertRows(ctx, q, "page_mount_item", mountItemCols, "", rows)
	return err
}

// membershipRows lists the page_mount_item rows of the published items that belong to a mount.
func (s *service) membershipRows(ctx context.Context, tx store.Tx, node ids.UUID, spec *MountSpec) ([][]any, error) {
	pat, err := route.Parse(spec.Route)
	if err != nil {
		return nil, err
	}
	c := tx.Content()
	var rows [][]any
	allow := s.slugAllowOf(spec.Collection)
	after := ids.Nil
	for {
		batch, err := c.ItemIDsOfType(ctx, spec.Collection, after, 500)
		if err != nil {
			return nil, fmt.Errorf("pages: list %s: %w", spec.Collection, err)
		}
		if len(batch) == 0 {
			break
		}
		after = batch[len(batch)-1]
		items, err := c.GetItems(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("pages: read %s items: %w", spec.Collection, err)
		}
		slices.SortFunc(items, func(a, b store.Item) int { return slices.Compare(a.ID[:], b.ID[:]) })
		for _, it := range items {
			docs, err := s.publishedDocs(ctx, tx, it)
			if err != nil {
				return nil, err
			}
			for _, d := range docs {
				if !matches(spec.Filter, d.Doc) {
					continue
				}
				if rel, ok := relPath(pat, it.ID, d.Doc, allow); ok {
					rows = append(rows, []any{node, it.ID, d.Locale, rel, redirectOfDoc(spec, d.Doc)})
				}
			}
		}
	}
	return rows, nil
}

// membershipItems lists the items a mount with this spec would route.
func (s *service) membershipItems(ctx context.Context, tx store.Tx, spec *MountSpec) ([]ids.UUID, error) {
	rows, err := s.membershipRows(ctx, tx, ids.Nil, spec)
	if err != nil {
		return nil, err
	}
	out := make([]ids.UUID, len(rows))
	for i, r := range rows {
		out[i] = r[1].(ids.UUID)
	}
	return out, nil
}

// mountsOf returns the mounts of a collection in every site, from the short-lived cache when possible.
func (s *service) mountsOf(ctx context.Context, q store.SQL, collection string) (*mountSet, error) {
	set, gen, ok := s.mounts.get(collection, s.clock.Now())
	if ok {
		return set, nil
	}
	rows, err := loadMounts(ctx, q, "", collection)
	if err != nil {
		return nil, err
	}
	set, err = newMountSet(rows)
	if err != nil {
		return nil, err
	}
	set.allow = s.slugAllowOf(collection)
	s.mounts.put(collection, set, gen, s.clock.Now().Add(mountCacheTTL))
	return set, nil
}

// membershipChange is what publishing a collection item in a locale changes in its mount memberships.
type membershipChange struct {
	set     *mountSet
	desired []membership
}

// membershipChange finds the mounts a published collection item belongs to in a locale and compares them with
// the stored ones. It returns nil when the collection has no mounts or nothing changes. The sites of the
// mounts are locked first, so the comparison holds until the transaction ends.
func (s *service) membershipChange(ctx context.Context, tx store.Tx, item ids.UUID, typ, locale string, doc map[string]any) (*membershipChange, error) {
	q := tx.SQL()
	set, err := s.mountsOf(ctx, q, typ)
	if err != nil || len(set.rows) == 0 {
		return nil, err
	}
	if err := s.lockSites(ctx, tx, distinctSites(set.rows)); err != nil {
		return nil, err
	}
	desired := set.memberships(item, locale, doc)
	have, err := loadMemberships(ctx, q, set.nodeIDs(), &item, []string{locale})
	if err != nil {
		return nil, err
	}
	if sameMemberships(desired, have) {
		return nil, nil
	}
	return &membershipChange{set, desired}, nil
}

// syncMountedItem refreshes the mount memberships and routes of a collection item in one locale. It reports
// where routes may have changed.
func (s *service) syncMountedItem(ctx context.Context, tx store.Tx, item ids.UUID, typ, locale string, doc map[string]any) ([]change, error) {
	q := tx.SQL()
	mc, err := s.membershipChange(ctx, tx, item, typ, locale, doc)
	if err != nil || mc == nil {
		return nil, err
	}
	ph, args := inArgs(mc.set.nodeIDs(), item, locale)
	if _, err := q.Exec(ctx, "DELETE FROM page_mount_item WHERE item_id = ? AND locale = ? AND node_id IN "+ph, args...); err != nil {
		return nil, err
	}
	insert := make([][]any, len(mc.desired))
	for i, m := range mc.desired {
		insert[i] = []any{m.Node, m.Item, m.Locale, m.Rel, m.Redirect}
	}
	if _, err := insertRows(ctx, q, "page_mount_item", mountItemCols, "", insert); err != nil {
		return nil, err
	}
	if err := s.reindexItems(ctx, tx, []ids.UUID{item}); err != nil {
		return nil, err
	}
	var out []change
	for _, site := range distinctSites(mc.set.rows) {
		if err := s.recomputeMounts(ctx, q, rowsOfSite(mc.set.rows, site), &item, []string{locale}); err != nil {
			return nil, err
		}
		out = append(out, change{Site: site})
	}
	return out, nil
}

func rowsOfSite(rows []nodeRow, site string) []nodeRow {
	var out []nodeRow
	for _, r := range rows {
		if r.Site == site {
			out = append(out, r)
		}
	}
	return out
}

func sameMemberships(a, b []membership) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(m membership) string { return m.Node.String() + "|" + m.Rel + "|" + m.Redirect }
	ka := make([]string, len(a))
	kb := make([]string, len(b))
	for i := range a {
		ka[i], kb[i] = key(a[i]), key(b[i])
	}
	slices.Sort(ka)
	slices.Sort(kb)
	return slices.Equal(ka, kb)
}

// previewMounted computes the routes a collection item would get in a locale and returns the clashes with
// routes of other items. Nothing is written.
func (s *service) previewMounted(ctx context.Context, tx store.Tx, item ids.UUID, typ, locale string, doc map[string]any) ([]clash, error) {
	q := tx.SQL()
	mc, err := s.membershipChange(ctx, tx, item, typ, locale, doc)
	if err != nil || mc == nil {
		return nil, err
	}
	var out []clash
	for _, site := range distinctSites(mc.set.rows) {
		inSite := rowsOfSite(mc.set.rows, site)
		g := newGraph(q)
		if err := g.loadNodesWithAncestors(ctx, inSite); err != nil {
			return nil, err
		}
		var ms []membership
		for _, m := range mc.desired {
			if g.nodes[m.Node] != nil {
				ms = append(ms, m)
			}
		}
		existing, err := mountRoutes(ctx, q, nodeIDs(inSite), &item, []string{locale})
		if err != nil {
			return nil, err
		}
		plan, err := diffRoutes(ctx, q, existing, g.mountWants(ms, []string{locale}))
		if err != nil {
			return nil, err
		}
		out = append(out, plan.clashes...)
	}
	return out, nil
}

func distinctSites(rows []nodeRow) []string {
	var out []string
	for _, r := range rows {
		if !slices.Contains(out, r.Site) {
			out = append(out, r.Site)
		}
	}
	return out
}

// reindexMount rewrites the module index rows of a mount's items and of the earlier ones (after its membership
// or collection changed).
func (s *service) reindexMount(ctx context.Context, tx store.Tx, node ids.UUID, earlier []ids.UUID) error {
	now, err := mountItemsOf(ctx, tx.SQL(), node)
	if err != nil {
		return err
	}
	return s.reindexItems(ctx, tx, append(now, earlier...))
}

// slugAllowOf gives the extra joining characters of the slug fields of a type, "" for other fields.
func (s *service) slugAllowOf(typ string) func(field string) string {
	t, ok := s.schema.Type(typ)
	if !ok {
		return nil
	}
	return func(field string) string {
		if f, ok := t.Field(field); ok && f.Shape.Format == "slug" {
			return strings.Join(f.Shape.Allow, "")
		}
		return ""
	}
}
