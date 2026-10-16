package content

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// draft is a stored draft part with its document decoded on first use.
type draft struct {
	store.DraftPart
	doc map[string]any
}

func (d *draft) document() (map[string]any, error) {
	if d.doc == nil {
		doc, err := schema.DecodeDoc(d.Data)
		if err != nil {
			return nil, fmt.Errorf("content: draft part %q of %s: %w", d.Part, d.ItemID, err)
		}
		d.doc = doc
	}
	return d.doc, nil
}

// pubVersion is a published version with its documents decoded on first use.
type pubVersion struct {
	store.Version
	shared map[string]any
	locs   map[string]map[string]any
	idx    map[string]int
}

func newPubVersion(v store.Version) *pubVersion {
	slices.SortFunc(v.Locales, func(a, b store.VersionLocale) int { return strings.Compare(a.Locale, b.Locale) })
	idx := make(map[string]int, len(v.Locales))
	for i, l := range v.Locales {
		idx[l.Locale] = i
	}
	return &pubVersion{Version: v, locs: map[string]map[string]any{}, idx: idx}
}

func (p *pubVersion) sharedDoc() (map[string]any, error) {
	if p.shared == nil {
		doc, err := decodeOrEmpty(p.Shared)
		if err != nil {
			return nil, fmt.Errorf("content: shared document of %s v%d: %w", p.ItemID, p.Version.Version, err)
		}
		p.shared = doc
	}
	return p.shared, nil
}

// localeDoc returns the locale's document; ok is false when the version lacks the locale.
func (p *pubVersion) localeDoc(locale string) (doc map[string]any, ok bool, err error) {
	i, found := p.idx[locale]
	if !found {
		return nil, false, nil
	}
	if doc, cached := p.locs[locale]; cached {
		return doc, true, nil
	}
	doc, err = decodeOrEmpty(p.Locales[i].Data)
	if err != nil {
		return nil, false, fmt.Errorf("content: %s document of %s v%d: %w", locale, p.ItemID, p.Version.Version, err)
	}
	p.locs[locale] = doc
	return doc, true, nil
}

func (p *pubVersion) codes() []string {
	out := make([]string, len(p.Locales))
	for i, l := range p.Locales {
		out[i] = l.Locale
	}
	return out
}

func decodeOrEmpty(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	return schema.DecodeDoc(data)
}

// itemState is an item with what an operation loaded of it: its draft parts and published version.
type itemState struct {
	item           store.Item
	typ            *schema.Type
	drafts         map[string]*draft
	pub            *pubVersion
	latest         *pubVersion // newest version of an unpublished item with history; set by loadLatest for publish
	reviewBypassed bool        // published without the approval the type's review needs
	prev           *prevRows   // rows the projections held before the operation, see rememberRows
}

// prevRows are the remembered rows; published is nil for a never-published item.
type prevRows struct{ working, published *rowset }

func (s *Service) newState(it store.Item) (*itemState, error) {
	t, err := s.typeOf(it.Type)
	if err != nil {
		return nil, fmt.Errorf("item %s: %w", it.ID, err)
	}
	return &itemState{item: it, typ: t, drafts: map[string]*draft{}}, nil
}

// load reads the draft parts and, when published, the current version.
func (st *itemState) load(ctx context.Context, c store.ContentStore) error {
	if err := st.loadDrafts(ctx, c); err != nil {
		return err
	}
	return st.loadPub(ctx, c)
}

func (st *itemState) loadDrafts(ctx context.Context, c store.ContentStore) error {
	parts, err := c.GetDraftParts(ctx, st.item.ID)
	if err != nil {
		return fmt.Errorf("content: load drafts of %s: %w", st.item.ID, err)
	}
	st.drafts = make(map[string]*draft, len(parts))
	for _, p := range parts {
		st.drafts[p.Part] = &draft{DraftPart: p}
	}
	return nil
}

func (st *itemState) loadPub(ctx context.Context, c store.ContentStore) error {
	if st.item.PublishedVersion == 0 {
		return nil
	}
	v, err := c.GetVersion(ctx, st.item.ID, st.item.PublishedVersion)
	if err != nil {
		return itemErr(fmt.Sprintf("%s version %d", st.item.ID, st.item.PublishedVersion), err)
	}
	st.pub = newPubVersion(v)
	return nil
}

// loadLatest reads the newest version of an unpublished item, which a publish builds the parts it does not
// select from (the item has no published version, but its history is still the best base).
func (st *itemState) loadLatest(ctx context.Context, c store.ContentStore) error {
	if st.item.PublishedVersion != 0 {
		return nil
	}
	metas, err := c.ListVersions(ctx, st.item.ID)
	if err != nil {
		return fmt.Errorf("content: list versions of %s: %w", st.item.ID, err)
	}
	if len(metas) == 0 {
		return nil
	}
	v, err := c.GetVersion(ctx, st.item.ID, metas[0].Version)
	if err != nil {
		return itemErr(fmt.Sprintf("%s version %d", st.item.ID, metas[0].Version), err)
	}
	st.latest = newPubVersion(v)
	return nil
}

// current is the version a publish builds on: the published one, else the newest of an unpublished item.
func (st *itemState) current() *pubVersion {
	if st.pub != nil {
		return st.pub
	}
	return st.latest
}

// draftNames lists the draft part names ("" = shared), sorted.
func (st *itemState) draftNames() []string {
	return slices.Sorted(maps.Keys(st.drafts))
}

func (st *itemState) setDraft(ctx context.Context, c store.ContentStore, p store.DraftPart, doc map[string]any) error {
	if err := c.PutDraftPart(ctx, p); err != nil {
		return fmt.Errorf("content: save draft part %q of %s: %w", p.Part, p.ItemID, err)
	}
	st.drafts[p.Part] = &draft{DraftPart: p, doc: doc}
	return nil
}

func (st *itemState) dropDrafts(ctx context.Context, c store.ContentStore, parts []string) error {
	if len(parts) == 0 {
		return nil
	}
	if err := c.DeleteDraftParts(ctx, st.item.ID, parts); err != nil {
		return fmt.Errorf("content: delete draft parts of %s: %w", st.item.ID, err)
	}
	for _, p := range parts {
		delete(st.drafts, p)
	}
	return nil
}

// publishedParts returns the published shared document ({} when never published) and the locale document
// (ok false when the locale is not published).
func (st *itemState) publishedParts(locale string) (shared, loc map[string]any, ok bool, err error) {
	if st.pub == nil {
		return map[string]any{}, nil, false, nil
	}
	if shared, err = st.pub.sharedDoc(); err != nil {
		return nil, nil, false, err
	}
	loc, ok, err = st.pub.localeDoc(locale)
	return shared, loc, ok, err
}

// workingParts returns the documents a draft view of locale shows: each draft part replaces the published
// part it was based on. name is "" when the locale exists nowhere.
func (st *itemState) workingParts(locale string) (shared, loc map[string]any, name string, err error) {
	pubShared, pubLoc, pubOK, err := st.publishedParts(locale)
	if err != nil {
		return nil, nil, "", err
	}
	shared, loc = pubShared, pubLoc
	if pubOK {
		name = st.pub.Locales[st.pub.idx[locale]].Name
	}
	if d, ok := st.drafts[""]; ok {
		if shared, err = d.document(); err != nil {
			return nil, nil, "", err
		}
	}
	if d, ok := st.drafts[locale]; ok {
		if loc, err = d.document(); err != nil {
			return nil, nil, "", err
		}
		name = d.Name
	}
	if loc == nil {
		loc = map[string]any{}
	}
	return shared, loc, name, nil
}

// locales lists the locales an item has: published ones, plus draft-only ones when withDrafts.
func (st *itemState) locales(withDrafts bool) []string {
	var out []string
	if st.pub != nil {
		out = st.pub.codes()
	}
	if withDrafts {
		for part := range st.drafts {
			if part != "" && !slices.Contains(out, part) {
				out = append(out, part)
			}
		}
		sort.Strings(out)
	}
	if out == nil {
		out = []string{}
	}
	return out
}
