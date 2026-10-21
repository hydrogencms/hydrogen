package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// resolved is an item as the channel shows it: one locale's payload, ready to splice.
type resolved struct {
	id       ids.UUID
	typ      string
	locale   string
	payload  []byte
	refs     []ids.UUID
	fallback bool
	draft    bool // built from the draft, not a snapshot
	inlined  bool // shared blocks were inlined (or there are none)
	// assetsDone is set once the asset fields were expanded. Items loaded as assets of another item are not
	// expanded themselves (an asset's own asset fields stay ids), so they never carry it.
	assetsDone bool
	extra      []byte
	etag       string // the store's hex SHA-256 of payload; empty for drafts and projections
	full       []byte // the whole payload of a projected item
}

// source is the item's whole payload, before any projection.
func (r *resolved) source() []byte {
	if r.full != nil {
		return r.full
	}
	return r.payload
}

func cmpUUID(a, b ids.UUID) int { return bytes.Compare(a[:], b[:]) }

// pick chooses the snapshot to show from the rows of one item.
func (st *state) pick(rows []store.Snapshot) *resolved {
	avail := make([]string, len(rows))
	for i := range rows {
		avail[i] = rows[i].Locale
	}
	first := rows[0]
	loc, _, ok := channel.PickLocale(st.eff, avail, first.MainLocale, first.AlwaysAvailable)
	if !ok || !st.m.ch.AllowsCollection(first.Type) {
		return nil
	}
	for i := range rows {
		if rows[i].Locale == loc {
			return &resolved{id: rows[i].ItemID, typ: rows[i].Type, locale: loc, payload: rows[i].Payload,
				refs: rows[i].Refs, fallback: loc != st.eff.Languages[0], etag: rows[i].ETag}
		}
	}
	return nil
}

// look records that the response looked an item up, whether or not the channel can show it: publishing it
// later changes the response either way, so the response is tagged with it. typ is "" while unknown.
func (st *state) look(id ids.UUID, typ string) {
	if st.looked == nil {
		st.looked = map[ids.UUID]string{}
	}
	if typ != "" || st.looked[id] == "" {
		st.looked[id] = typ
	}
}

// pickAll groups rows by item and picks one per item.
func (st *state) pickAll(rows []store.Snapshot, into map[ids.UUID]*resolved) {
	if len(rows) == 1 {
		if r := st.pick(rows); r != nil {
			into[r.id] = r
		}
		return
	}
	slices.SortFunc(rows, func(a, b store.Snapshot) int { return cmpUUID(a.ItemID, b.ItemID) })
	for len(rows) > 0 {
		n := 1
		for n < len(rows) && rows[n].ItemID == rows[0].ItemID {
			n++
		}
		if r := st.pick(rows[:n]); r != nil {
			into[r.id] = r
		}
		rows = rows[n:]
	}
}

// load returns the requested items as the channel shows them, in request order; unavailable items are left
// out. Items a preview token covers come from their drafts, the others from the published snapshots. f projects
// them (nil: every field).
func (st *state) load(ctx context.Context, list []ids.UUID, f *Fields) ([]*resolved, error) {
	got := make(map[ids.UUID]*resolved, len(list))
	rest := make([]ids.UUID, 0, len(list))
	st.prefetchRelays(ctx, list)
	for _, id := range list {
		st.look(id, "")
		if _, dup := got[id]; dup {
			continue
		}
		if st.previews(id) {
			r, err := st.draft(ctx, id)
			if err != nil {
				return nil, err
			}
			if r != nil {
				got[id] = r
			}
			continue
		}
		if !slices.Contains(rest, id) {
			rest = append(rest, id)
		}
	}
	if err := st.published(ctx, rest, got); err != nil {
		return nil, err
	}
	out := make([]*resolved, 0, len(list))
	seen := map[ids.UUID]bool{}
	for _, id := range list {
		if r := got[id]; r != nil && !seen[id] {
			seen[id] = true
			st.look(id, r.typ)
			out = append(out, r)
		}
	}
	if err := st.project(out, f); err != nil {
		return nil, err
	}
	if err := st.inline(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (st *state) published(ctx context.Context, list []ids.UUID, into map[ids.UUID]*resolved) error {
	if len(list) == 0 {
		return nil
	}
	rd := st.a.o.Reader
	rows, err := rd.Snapshots(ctx, list, st.eff.Languages)
	if err != nil {
		return err
	}
	st.pickAll(rows, into)
	// Items with no row in the channel's languages may still be always available in their main locale, which
	// only their rows tell.
	have := make(map[ids.UUID]bool, len(rows))
	for i := range rows {
		have[rows[i].ItemID] = true
	}
	var missing []ids.UUID
	for _, id := range list {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	rows, err = rd.Snapshots(ctx, missing, nil)
	if err != nil {
		return err
	}
	st.pickAll(rows, into)
	return nil
}

// draft returns the draft view of an item as the channel shows it, nil when it isn't available.
func (st *state) draft(ctx context.Context, id ids.UUID) (*resolved, error) {
	v, err := st.a.o.Content.GetForChannel(ctx, id, st.eff, content.ChannelGetOptions{Draft: true})
	if err != nil {
		if errors.Is(err, content.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return st.resolveDraft(ctx, v)
}

func (st *state) resolveDraft(ctx context.Context, v *content.View) (*resolved, error) {
	v = st.relayed(ctx, v)
	payload, refs, err := st.a.o.Content.PreviewPayload(v)
	if err != nil {
		return nil, err
	}
	return &resolved{id: v.ID, typ: v.Type, locale: v.Locale, payload: payload, refs: refs,
		fallback: v.Locale != st.eff.Languages[0], draft: true}, nil
}

// loadKey returns the item with a key as the channel shows it.
func (st *state) loadKey(ctx context.Context, key string, f *Fields) (*resolved, error) {
	if st.preview != nil {
		v, err := st.a.o.Content.GetByKey(ctx, key, content.GetOptions{Draft: true})
		if err != nil {
			if errors.Is(err, content.ErrNotFound) {
				return nil, nil
			}
			return nil, err
		}
		list, err := st.load(ctx, []ids.UUID{v.ID}, f)
		if err != nil || len(list) == 0 {
			return nil, err
		}
		return list[0], nil
	}
	rd := st.a.o.Reader
	rows, err := rd.SnapshotsByKey(ctx, key, st.eff.Languages)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if rows, err = rd.SnapshotsByKey(ctx, key, nil); err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := st.pick(rows)
	if r == nil {
		return nil, nil
	}
	st.look(r.id, r.typ)
	if err := st.project([]*resolved{r}, f); err != nil {
		return nil, err
	}
	return r, st.inline(ctx, []*resolved{r})
}

// loadSummaries loads listed items. Published listings fetch exactly the locale each item shows, one query per
// distinct locale. Items are known to be available (the query filters on the channel's languages), so one
// missing here was unpublished since the query ran.
func (st *state) loadSummaries(ctx context.Context, items []store.ItemSummary, f *Fields) ([]*resolved, error) {
	for _, it := range items {
		st.look(it.ID, it.Type)
	}
	if st.preview != nil && st.preview.Item == "" {
		list := make([]ids.UUID, len(items))
		for i, it := range items {
			list[i] = it.ID
		}
		return st.load(ctx, list, f)
	}
	got, err := st.fetchLocales(ctx, items)
	if err != nil {
		return nil, err
	}
	out := make([]*resolved, 0, len(items))
	for _, it := range items {
		r := got[it.ID]
		if st.previews(it.ID) {
			var err error
			if r, err = st.draft(ctx, it.ID); err != nil {
				return nil, err
			}
		}
		if r != nil {
			out = append(out, r)
		}
	}
	if err := st.project(out, f); err != nil {
		return nil, err
	}
	if err := st.inline(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// fetchLocales loads the snapshot each item shows, one query per locale.
func (st *state) fetchLocales(ctx context.Context, items []store.ItemSummary) (map[ids.UUID]*resolved, error) {
	got := map[ids.UUID]*resolved{}
	if st.a.hasOverlays(st.eff) {
		// An overlay locale has a snapshot whenever its chain reaches a published locale, though the item's
		// version lists only the stored ones. Items whose snapshot is missing after all (inheritance is
		// switched off) are picked again from the version's locales.
		missed, err := st.fetchPicked(ctx, items, true, got)
		if err != nil {
			return nil, err
		}
		items = missed
	}
	_, err := st.fetchPicked(ctx, items, false, got)
	return got, err
}

// fetchPicked fetches the locale PickLocale chooses for each item, widening the locales an item is available
// in with the overlay languages whose chain reaches one of them. It returns the items whose chosen locale had
// no snapshot.
func (st *state) fetchPicked(ctx context.Context, items []store.ItemSummary, widen bool, got map[ids.UUID]*resolved) ([]store.ItemSummary, error) {
	byLocale := map[string][]ids.UUID{}
	picked := map[ids.UUID]store.ItemSummary{}
	for _, it := range items {
		avail := it.Locales
		if widen {
			avail = st.a.withDerived(st.eff, avail)
		}
		if loc, _, ok := channel.PickLocale(st.eff, avail, it.MainLocale, it.AlwaysAvailable); ok {
			byLocale[loc] = append(byLocale[loc], it.ID)
			picked[it.ID] = it
		}
	}
	var missed []store.ItemSummary
	for _, loc := range sortedKeys(byLocale) {
		rows, err := st.a.o.Reader.Snapshots(ctx, byLocale[loc], []string{loc})
		if err != nil {
			return nil, err
		}
		found := make(map[ids.UUID]bool, len(rows))
		for i := range rows {
			found[rows[i].ItemID] = true
			if st.m.ch.AllowsCollection(rows[i].Type) {
				got[rows[i].ItemID] = &resolved{id: rows[i].ItemID, typ: rows[i].Type, locale: loc, payload: rows[i].Payload,
					refs: rows[i].Refs, fallback: loc != st.eff.Languages[0], etag: rows[i].ETag}
			}
		}
		for _, id := range byLocale[loc] {
			if !found[id] {
				missed = append(missed, picked[id])
			}
		}
	}
	return missed, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// include resolves the items the primary ones reference, breadth-first to depth levels, at most maxIncluded in
// all. Items already shown (and ones that were looked up and not available) are never fetched twice, so cycles
// end.
func (st *state) include(ctx context.Context, primary []*resolved, depth int) ([]*resolved, error) {
	seen := map[ids.UUID]bool{}
	for _, p := range primary {
		seen[p.id] = true
	}
	frontier := primary
	var out []*resolved
	for level := 0; level < depth && len(frontier) > 0 && len(out) < maxIncluded; level++ {
		var want []ids.UUID
		for _, f := range frontier {
			for _, ref := range f.refs {
				if !seen[ref] {
					seen[ref] = true
					want = append(want, ref)
				}
			}
		}
		var loaded []*resolved
		for len(want) > 0 && len(out)+len(loaded) < maxIncluded {
			n := min(len(want), maxIncluded-len(out)-len(loaded))
			got, err := st.load(ctx, want[:n], nil)
			if err != nil {
				return nil, err
			}
			loaded = append(loaded, got...)
			want = want[n:]
		}
		out = append(out, loaded...)
		frontier = loaded
	}
	return out, nil
}

// decorate asks the decorators for extra keys and encodes them with the fallback marker.
func (st *state) decorate(ctx context.Context, all []*resolved) error {
	a := st.a
	if len(a.o.Decorators) == 0 && len(st.m.ch.Links) == 0 {
		for _, r := range all {
			r.extra = fallbackExtra(r.fallback)
		}
		return nil
	}
	items := make([]*Item, len(all))
	for i, r := range all {
		items[i] = &Item{ID: r.id.String(), Type: r.typ, Locale: r.locale, Refs: r.refs}
	}
	ctx = context.WithValue(ctx, stateKey{}, st)
	for _, d := range a.o.Decorators {
		if err := d.Decorate(ctx, st.m.ch, items); err != nil {
			return err
		}
	}
	if err := st.patternLinks(ctx, all, items); err != nil {
		return err
	}
	for i, r := range all {
		var err error
		if r.extra, err = extraJSON(items[i].Extra, r.fallback); err != nil {
			return err
		}
	}
	return nil
}

// TagContext adds cache tags to the response of the request a Decorator is running for.
func TagContext(ctx context.Context, tags ...string) {
	if st, _ := ctx.Value(stateKey{}).(*state); st != nil {
		st.tags = append(st.tags, tags...)
	}
}

var fallbackJSON = []byte(`"fallback":true`)

func fallbackExtra(fallback bool) []byte {
	if fallback {
		return fallbackJSON
	}
	return nil
}

// MaxItemTags bounds the item tags of one response (and so the Surrogate-Key and Cache-Tag headers). A
// response that looked up more items is tagged with the listing tag of every type involved instead, which
// any publish of those types purges.
const MaxItemTags = 64

// responseTags returns the response's tags, sorted and unique: the ones handlers added plus one per looked-up
// item (or per involved type beyond MaxItemTags).
func (st *state) responseTags() []string {
	tags := append(st.tags, st.itemTags()...)
	slices.Sort(tags)
	return slices.Compact(tags)
}

func (st *state) itemTags() []string {
	if len(st.looked) <= MaxItemTags {
		tags := make([]string, 0, len(st.looked))
		for id := range st.looked {
			tags = append(tags, cachetag.Item(id.String()))
		}
		return tags
	}
	types := map[string]bool{}
	for _, typ := range st.looked {
		if typ == "" {
			// Its type is unknown, so any type the channel shows could be it.
			for _, t := range st.a.o.Schema.Types() {
				if st.m.ch.AllowsCollection(t.Name) {
					types[t.Name] = true
				}
			}
			continue
		}
		types[typ] = true
	}
	tags := make([]string, 0, len(types))
	for _, typ := range sortedKeys(types) {
		if t, ok := st.a.o.Schema.Type(typ); ok && t.Singleton {
			tags = append(tags, cachetag.Singleton(typ))
		} else {
			tags = append(tags, cachetag.Collection(typ))
		}
	}
	return tags
}

// appendIncluded appends `,"included":{id:payload,…}` sorted by id, or nothing when there are none.
func appendIncluded(dst []byte, inc []*resolved) []byte {
	if len(inc) == 0 {
		return dst
	}
	sorted := slices.Clone(inc)
	slices.SortFunc(sorted, func(a, b *resolved) int { return cmpUUID(a.id, b.id) })
	dst = append(dst, `,"included":{`...)
	for i, r := range sorted {
		if i > 0 {
			dst = append(dst, ',')
		}
		dst = append(dst, '"')
		dst = append(dst, r.id.String()...)
		dst = append(dst, `":`...)
		dst = appendSpliced(dst, r.payload, r.extra)
	}
	return append(dst, '}')
}

// Items loads items for a module route: for each id, the payload as the channel shows it in locale (the
// channel's own order when locale is empty or not one of its languages), with fallback and decorator keys
// spliced in, or nil when the item isn't available on the channel. Every id asked for is tagged on the response, shown or not.
func Items(r *http.Request, locale string, list ...ids.UUID) ([]json.RawMessage, error) {
	return items(r, locale, nil, list)
}

func items(r *http.Request, locale string, f *Fields, list []ids.UUID) ([]json.RawMessage, error) {
	st := stateOf(r)
	if st == nil {
		return nil, errors.New("delivery: Items called outside a delivery route")
	}
	old := st.eff
	defer func() { st.eff = old }()
	if locale != "" && slices.Contains(st.m.ch.Languages, locale) {
		st.eff = narrowTo(st.m.ch, locale)
	}
	res, err := st.load(r.Context(), list, f)
	if err != nil {
		return nil, err
	}
	if err := st.decorate(r.Context(), res); err != nil {
		return nil, err
	}
	byID := make(map[ids.UUID]*resolved, len(res))
	for _, x := range res {
		byID[x.id] = x
	}
	out := make([]json.RawMessage, len(list))
	for i, id := range list {
		if x := byID[id]; x != nil {
			out[i] = appendSpliced(nil, x.payload, x.extra)
			if len(x.extra) == 0 && x.etag != "" {
				st.whole = append(st.whole, stored{x.payload, x.etag})
			}
		}
	}
	return out, nil
}

// narrowTo orders the channel's languages with loc first.
func narrowTo(ch *channel.Channel, loc string) *channel.Channel {
	if ch.Languages[0] == loc {
		return ch
	}
	cp := *ch
	cp.Languages = append([]string{loc}, slices.DeleteFunc(slices.Clone(ch.Languages), func(l string) bool { return l == loc })...)
	return &cp
}
