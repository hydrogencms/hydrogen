package content

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// maxNamedReferrers bounds the per-item version reads that give referrers their names.
const maxNamedReferrers = 100

// Get returns the published view of an item, or with Draft the published content overlaid with its draft
// parts. A locale the item lacks yields a view with only the shared fields and an empty Name, so a client can
// start a translation from it. A never-published item without Draft is ErrNotFound.
func (s *Service) Get(ctx context.Context, id ids.UUID, o GetOptions) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		get := getItem
		if o.Trashed {
			get = getItemAny
		}
		it, err := get(ctx, tx, id, false)
		if err != nil {
			return err
		}
		v, err = s.viewOf(ctx, tx, it, o)
		return err
	})
	return v, err
}

// GetByKey is Get for an item addressed by its key.
func (s *Service) GetByKey(ctx context.Context, key string, o GetOptions) (*View, error) {
	return s.getByKey(ctx, key, "", o)
}

// getByKey reads the item with the key; with singleton set it must be of that type.
func (s *Service) getByKey(ctx context.Context, key, singleton string, o GetOptions) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItemByKey(ctx, tx, key)
		if err == nil && singleton != "" && it.Type != singleton {
			err = fmt.Errorf("%w: key %s", ErrNotFound, key)
		}
		if err != nil {
			return err
		}
		v, err = s.viewOf(ctx, tx, it, o)
		return err
	})
	return v, err
}

// Singleton returns the item of a singleton type (ErrNotFound until its first write).
func (s *Service) Singleton(ctx context.Context, typ string, o GetOptions) (*View, error) {
	if _, err := s.singletonType(typ); err != nil {
		return nil, err
	}
	return s.getByKey(ctx, typ, typ, o)
}

// getSingletonItem finds the item of a singleton type by its key. An item of another type holding the key
// (created before keys named after singletons were refused) is not the singleton.
func getSingletonItem(ctx context.Context, tx store.Tx, typ string) (store.Item, error) {
	it, err := getItemByKey(ctx, tx, typ)
	if err == nil && it.Type != typ {
		return it, fmt.Errorf("%w: key %s", ErrNotFound, typ)
	}
	return it, err
}

func (s *Service) singletonType(name string) (singleton bool, err error) {
	t, err := s.typeOf(name)
	if err != nil {
		return false, err
	}
	if !t.Singleton {
		return false, badRequest("type %q is not a singleton", name)
	}
	return true, nil
}

func (s *Service) viewOf(ctx context.Context, tx store.Tx, it store.Item, o GetOptions) (*View, error) {
	locale := o.Locale
	if locale == "" {
		locale = it.MainLocale
	} else if err := validateLocale(locale); err != nil {
		return nil, err
	}
	st, err := s.newState(it)
	if err != nil {
		return nil, err
	}
	if err := st.load(ctx, tx.Content()); err != nil {
		return nil, err
	}
	if it.DeletedAt != nil {
		o.Draft = true
	}
	if err := s.checkRead(ctx, st, o.Draft); err != nil {
		return nil, err
	}
	if it.PublishedVersion == 0 && !o.Draft {
		return nil, fmt.Errorf("%w: %s is not published", ErrNotFound, it.ID)
	}
	v, err := st.view(locale, o.Draft)
	if err != nil {
		return nil, err
	}
	if o.Draft && it.PublishedVersion == 0 {
		metas, err := tx.Content().ListVersions(ctx, it.ID)
		if err != nil {
			return nil, fmt.Errorf("content: list versions of %s: %w", it.ID, err)
		}
		if len(metas) > 0 {
			v.Status = StatusUnpublished
		}
	}
	if it.DeletedAt != nil {
		v.DeletedAt, v.DeletedBy = it.DeletedAt, it.DeletedBy
		v.DeletedByName = s.actorNames(ctx, tx, []string{it.DeletedBy})[it.DeletedBy]
	}
	s.withActions(ctx, st, v)
	return v, nil
}

// view builds the view of one locale from the loaded state.
func (st *itemState) view(locale string, withDraft bool) (*View, error) {
	var (
		shared, loc map[string]any
		name        string
		err         error
	)
	if withDraft {
		shared, loc, name, err = st.workingParts(locale)
	} else {
		shared, loc, name, err = st.publishedView(locale)
	}
	if err != nil {
		return nil, err
	}
	return st.viewFrom(locale, name, mergeParts(shared, loc), withDraft), nil
}

func (st *itemState) publishedView(locale string) (shared, loc map[string]any, name string, err error) {
	shared, loc, ok, err := st.publishedParts(locale)
	if ok {
		name = st.pub.Locales[st.pub.idx[locale]].Name
	}
	return shared, loc, name, err
}

func (st *itemState) viewFrom(locale, name string, fields map[string]any, withDraft bool) *View {
	it := st.item
	v := &View{
		ID: it.ID, Type: it.Type, Key: it.Key, Locale: locale, MainLocale: it.MainLocale,
		AlwaysAvailable: it.AlwaysAvailable, Status: statusOf(it.PublishedVersion, len(st.drafts)),
		Version: it.PublishedVersion, Draft: withDraft, Name: name, Fields: fields,
		Locales: st.locales(withDraft), DraftParts: partNames(st.draftNames()),
		CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
	if st.pub != nil {
		at := st.pub.CreatedAt
		v.PublishedAt = &at
	}
	return v
}

func statusOf(publishedVersion, drafts int) string {
	switch {
	case publishedVersion == 0:
		return StatusDraft
	case drafts > 0:
		return StatusChanged
	}
	return StatusPublished
}

// partNames converts storage part names ("" = shared) to API names.
func partNames(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if p == "" {
			p = SharedPart
		}
		out[i] = p
	}
	return out
}

// storagePart is the inverse of partNames for one name.
func storagePart(p string) string {
	if p == SharedPart {
		return ""
	}
	return p
}

func (s *Service) Versions(ctx context.Context, id ids.UUID) ([]store.VersionMeta, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out []store.VersionMeta
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err := s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentRead, nil); err != nil {
			return err
		}
		out, err = tx.Content().ListVersions(ctx, id)
		return err
	})
	return out, err
}

// VersionContent returns the documents of one stored version (read-only; ErrNotFound for an unknown number).
func (s *Service) VersionContent(ctx context.Context, id ids.UUID, version int) (*VersionView, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var out *VersionView
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err := s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentRead, nil); err != nil {
			return err
		}
		v, err := tx.Content().GetVersion(ctx, id, version)
		if err != nil {
			return itemErr(fmt.Sprintf("%s version %d", id, version), err)
		}
		pv := newPubVersion(v)
		shared, err := pv.sharedDoc()
		if err != nil {
			return err
		}
		out = &VersionView{Version: v.Version, SchemaRev: v.SchemaRev, CreatedAt: v.CreatedAt, CreatedBy: v.CreatedBy,
			Shared: VersionDoc{Fields: shared}, Locales: make(map[string]VersionDoc, len(v.Locales))}
		for _, l := range v.Locales {
			doc, _, err := pv.localeDoc(l.Locale)
			if err != nil {
				return err
			}
			out.Locales[l.Locale] = VersionDoc{Name: l.Name, Fields: doc}
		}
		return nil
	})
	return out, err
}

// References lists items whose published versions point at id.
func (s *Service) References(ctx context.Context, id ids.UUID) ([]Referrer, error) {
	out, _, err := s.ReferencesVisible(ctx, id)
	return out, err
}

// ReferencesVisible is References for the principal in ctx: referrers it may not read are left out and
// counted in hidden.
func (s *Service) ReferencesVisible(ctx context.Context, id ids.UUID) (out []Referrer, hidden int, err error) {
	if err := s.prepare(ctx); err != nil {
		return nil, 0, err
	}
	err = s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err := s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentRead, nil); err != nil {
			return err
		}
		refs, err := tx.Content().IncomingRefs(ctx, id)
		if err != nil {
			return fmt.Errorf("content: references of %s: %w", id, err)
		}
		out, hidden, err = s.referrers(ctx, tx.Content(), refs, len(refs))
		return err
	})
	return out, hidden, err
}

// referrers resolves refs to referrers with the type and name of the referring item. Types come from one
// batched read; names need one version read per distinct item, so only the first nameLimit items get one.
// Referrers the principal in ctx may not read are dropped and counted.
func (s *Service) referrers(ctx context.Context, c store.ContentStore, refs []store.Ref, nameLimit int) ([]Referrer, int, error) {
	if len(refs) == 0 {
		return nil, 0, nil
	}
	slices.SortFunc(refs, func(a, b store.Ref) int {
		if c := slices.Compare(a.From[:], b.From[:]); c != 0 {
			return c
		}
		return compareRefs(a, b)
	})
	items, err := c.GetItems(ctx, distinctFrom(refs))
	if err != nil {
		return nil, 0, fmt.Errorf("content: load referrers: %w", err)
	}
	byID := make(map[ids.UUID]store.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	names := map[ids.UUID]string{}
	readable := map[ids.UUID]bool{}
	hidden := 0
	out := make([]Referrer, 0, len(refs))
	for _, r := range refs {
		it, ok := byID[r.From]
		if !ok {
			continue
		}
		if s.guarded(ctx) {
			vis, seen := readable[r.From]
			if !seen {
				var err error
				if vis, err = s.readable(ctx, c, it); err != nil {
					return nil, 0, err
				}
				readable[r.From] = vis
			}
			if !vis {
				hidden++
				continue
			}
		}
		name, seen := names[r.From]
		if !seen && len(names) < nameLimit {
			if name, err = s.publishedName(ctx, c, it); err != nil {
				return nil, 0, err
			}
			names[r.From] = name
		}
		out = append(out, Referrer{ItemID: r.From, Type: it.Type, Name: name, FieldPath: r.FieldPath, Kind: r.Kind, OnDelete: r.OnDelete})
	}
	return out, hidden, nil
}

func compareRefs(a, b store.Ref) int {
	if c := strings.Compare(a.FieldPath, b.FieldPath); c != 0 {
		return c
	}
	return slices.Compare(a.To[:], b.To[:])
}

func distinctFrom(refs []store.Ref) []ids.UUID {
	out := make([]ids.UUID, 0, len(refs))
	for _, r := range refs {
		if len(out) == 0 || out[len(out)-1] != r.From {
			out = append(out, r.From)
		}
	}
	return slices.Compact(out)
}

func (s *Service) publishedName(ctx context.Context, c store.ContentStore, it store.Item) (string, error) {
	if it.PublishedVersion == 0 {
		return "", nil
	}
	v, err := c.GetVersion(ctx, it.ID, it.PublishedVersion)
	if err != nil {
		return "", itemErr(it.ID.String(), err)
	}
	pv := newPubVersion(v)
	if i, ok := pv.idx[it.MainLocale]; ok {
		return v.Locales[i].Name, nil
	}
	if len(v.Locales) > 0 {
		return v.Locales[0].Name, nil
	}
	return "", nil
}
