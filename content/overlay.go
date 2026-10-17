package content

import (
	"encoding/json"
	"slices"
	"sort"
)

// Overlays tells the service which locales inherit from which (market overlays: de-CH inherits de). An
// overlay locale's stored document holds only the translatable fields it overrides; every place that builds
// a per-locale document resolves the rest from its ancestors, field by field. A nil provider (Options.Overlays)
// makes every locale independent.
type Overlays interface {
	// Chain lists the locale first, then its ancestors, nearest first. A locale that inherits from nothing
	// (or is unknown) is a chain of one.
	Chain(locale string) []string
	// Descendants lists every locale that inherits from the locale, directly or not, sorted.
	Descendants(locale string) []string
}

// chainOf is the locale's chain when it is an overlay locale (it has an ancestor), else nil.
func (s *Service) chainOf(locale string) []string {
	if s == nil || s.o.Overlays == nil {
		return nil
	}
	c := s.o.Overlays.Chain(locale)
	if len(c) < 2 || c[0] != locale {
		return nil
	}
	return c
}

// withDerived returns outs plus the overlay locales that have no document in outs but inherit from one that
// has, sorted by locale. Derived outs share the document of their nearest ancestor in outs. outs must hold
// every stored locale of the version.
func (s *Service) withDerived(outs []*localeOut) []*localeOut {
	if s.o.Overlays == nil || len(outs) == 0 {
		return outs
	}
	have := make(map[string]*localeOut, len(outs))
	for _, o := range outs {
		have[o.code] = o
	}
	var extra []string
	for _, o := range outs {
		for _, d := range s.o.Overlays.Descendants(o.code) {
			if have[d] == nil && !slices.Contains(extra, d) {
				extra = append(extra, d)
			}
		}
	}
	if len(extra) == 0 {
		return outs
	}
	all := slices.Clone(outs)
	for _, d := range extra {
		for _, a := range s.o.Overlays.Chain(d)[1:] {
			if src := have[a]; src != nil {
				all = append(all, &localeOut{code: d, doc: src.doc, name: src.name, refs: src.refs, final: src.final, derived: true})
				break
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].code < all[j].code })
	return all
}

// withDescendants adds the locales that inherit from any of codes (limited to the known set when known is
// non-nil), sorted.
func (s *Service) withDescendants(codes, known []string) []string {
	if s.o.Overlays == nil {
		return codes
	}
	out := slices.Clone(codes)
	for _, c := range codes {
		for _, d := range s.o.Overlays.Descendants(c) {
			if (known == nil || slices.Contains(known, d)) && !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
	}
	slices.Sort(out)
	return out
}

// resolution is the translatable document of an overlay locale with where each field comes from.
type resolution struct {
	doc    map[string]any    // resolved fields
	own    map[string]any    // the locale's stored (sparse) document; nil when it has none
	from   map[string]string // inherited field → the locale it comes from
	stored bool
	avail  bool // the locale or an ancestor has a document
}

// rawLoc is the stored (sparse) document and name of one locale: its draft part when working and it has one,
// else its published document.
func (st *itemState) rawLoc(working bool, locale string) (doc map[string]any, name string, ok bool, err error) {
	if working {
		if d, has := st.drafts[locale]; has {
			doc, err = d.document()
			return doc, d.Name, true, err
		}
	}
	if st.pub == nil {
		return nil, "", false, nil
	}
	doc, ok, err = st.pub.localeDoc(locale)
	if ok {
		name = st.pub.Locales[st.pub.idx[locale]].Name
	}
	return doc, name, ok, err
}

func (st *itemState) chain(locale string) []string { return st.svc.chainOf(locale) }

// resolve unions the stored documents along the chain, the locale's own fields first.
func (st *itemState) resolve(working bool, chain []string) (resolution, error) {
	res := resolution{doc: map[string]any{}, from: map[string]string{}}
	for i, code := range chain {
		doc, _, ok, err := st.rawLoc(working, code)
		if err != nil {
			return res, err
		}
		if !ok {
			continue
		}
		res.avail = true
		if i == 0 {
			res.stored, res.own = true, doc
		}
		for k, v := range doc {
			if _, has := res.doc[k]; !has {
				res.doc[k] = v
				if i > 0 {
					res.from[k] = code
				}
			}
		}
	}
	return res, nil
}

// unionChain resolves a locale over documents held in memory (the locales of a version being written):
// ancestors without a document contribute nothing.
func unionChain(chain []string, docs map[string]map[string]any) map[string]any {
	out := map[string]any{}
	for _, code := range chain {
		for k, v := range docs[code] {
			if _, has := out[k]; !has {
				out[k] = v
			}
		}
	}
	return out
}

// parentOf returns the resolved translatable document of the nearest ancestor of chain that has one in docs
// (documents that are already resolved), or nil.
func parentOf(chain []string, resolved map[string]map[string]any) map[string]any {
	for _, a := range chain[1:] {
		if d, ok := resolved[a]; ok {
			return d
		}
	}
	return nil
}

// sparsify reduces a resolved translatable document to what the locale stores: the fields it stored before
// (own), the ones in force, and any that differ from what the parent resolves to.
func sparsify(resolved, own, parent map[string]any, force map[string]bool) map[string]any {
	out := make(map[string]any, len(own))
	for k, v := range resolved {
		pv, inParent := parent[k]
		if _, stored := own[k]; stored || force[k] || !inParent || !equalJSON(v, pv) {
			out[k] = v
		}
	}
	return out
}

// availableLocales lists the locales the item has documents for, plus the overlay locales that inherit from
// them.
func (st *itemState) availableLocales(withDrafts bool) []string {
	out := st.locales(withDrafts)
	if st.svc == nil || st.svc.o.Overlays == nil {
		return out
	}
	return st.svc.withDescendants(out, nil)
}

// nameOf is the display name of a resolved locale document.
func (st *itemState) nameOf(shared, loc map[string]any) string {
	doc := mergeParts(cloneDoc(shared), cloneDoc(loc))
	st.svc.o.Schema.Normalize(st.typ, doc)
	st.svc.o.Schema.Prune(st.typ, doc)
	return st.svc.o.Schema.Name(st.typ, doc)
}

// decorate fills the overlay fields of a view.
func (st *itemState) decorate(v *View, locale string, working bool) {
	v.AvailableLocales = st.availableLocales(working)
	chain := st.chain(locale)
	if chain == nil {
		return
	}
	res, err := st.resolve(working, chain)
	if err != nil {
		return
	}
	v.Inherits = chain[1]
	v.Overrides = make([]string, 0, len(res.own))
	for k := range res.own {
		v.Overrides = append(v.Overrides, k)
	}
	slices.Sort(v.Overrides)
	v.InheritedFrom = res.from
}

// MarshalJSON adds overrides and inheritedFrom (always present, possibly empty) to the views of overlay
// locales.
func (v View) MarshalJSON() ([]byte, error) {
	type plain View
	if v.Inherits == "" {
		return json.Marshal(plain(v))
	}
	if v.InheritedFrom == nil {
		v.InheritedFrom = map[string]string{}
	}
	return json.Marshal(struct {
		plain
		Overrides     []string          `json:"overrides"`
		InheritedFrom map[string]string `json:"inheritedFrom"`
	}{plain(v), v.Overrides, v.InheritedFrom})
}

// overlayWrite is a draft write to an overlay locale in progress.
type overlayWrite struct {
	resolved map[string]any // shared fields and the locale's resolved fields: what hooks and validation see
	own      map[string]any // the fields the locale stored before the write
	parent   map[string]any // what the ancestors resolve to
	force    map[string]bool
	chain    []string
}

// startOverlayWrite applies a merge patch to the resolved document of an overlay locale. base is the stored
// (sparse) document with the shared fields. A patched field becomes an override; null removes the override, so
// the field takes the ancestors' value again.
func (s *Service) startOverlayWrite(st *itemState, locale string, chain []string, base, patch map[string]any) (*overlayWrite, error) {
	shared, loc, _, err := st.workingParts(locale)
	if err != nil {
		return nil, err
	}
	pres, err := st.resolve(true, chain[1:])
	if err != nil {
		return nil, err
	}
	ov := &overlayWrite{resolved: mergeParts(cloneDoc(shared), cloneDoc(loc)), own: map[string]any{}, parent: pres.doc,
		force: map[string]bool{}, chain: chain}
	_, own := splitDoc(st.typ, base)
	ov.own = own
	applyPatch(ov.resolved, patch)
	for k, pv := range patch {
		f, ok := st.typ.Field(k)
		if !ok || !f.Translatable {
			continue
		}
		if pv != nil {
			ov.force[k] = true
			continue
		}
		delete(ov.own, k)
		if inherited, has := pres.doc[k]; has {
			ov.resolved[k] = cloneValue(inherited)
		}
	}
	return ov, nil
}

// sparse is the document to store for doc (the finished resolved document): shared fields and the overrides.
func (ov *overlayWrite) sparse(st *itemState, doc map[string]any) map[string]any {
	shared, loc := splitDoc(st.typ, doc)
	return mergeParts(shared, sparsify(loc, ov.own, ov.parent, ov.force))
}
