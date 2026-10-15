package content

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// Publish makes the selected draft parts the next published version.
func (s *Service) Publish(ctx context.Context, id ids.UUID, o PublishOptions) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, true)
		if err != nil {
			return err
		}
		v, err = s.publish(ctx, tx, it, o)
		return err
	})
	return v, err
}

// PublishSingleton is Publish for a singleton type.
func (s *Service) PublishSingleton(ctx context.Context, typ string, o PublishOptions) (*View, error) {
	if _, err := s.singletonType(typ); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getSingletonItem(ctx, tx, typ)
		if err != nil {
			return err
		}
		if it, err = getItem(ctx, tx, it.ID, true); err != nil {
			return err
		}
		v, err = s.publish(ctx, tx, it, o)
		return err
	})
	return v, err
}

// selection is the set of draft parts a publish takes.
type selection struct {
	shared  bool
	locales []string // sorted
}

// changedLocales are the locales whose published content changes: all of them when the shared part goes.
func (sel selection) changedLocales(all []string) []string {
	if sel.shared {
		return all
	}
	return sel.locales
}

func (s *Service) publish(ctx context.Context, tx store.Tx, it store.Item, o PublishOptions) (*View, error) {
	if _, err := s.currentRev(); err != nil {
		return nil, err
	}
	st, err := s.newState(it)
	if err != nil {
		return nil, err
	}
	c := tx.Content()
	if err := st.load(ctx, c); err != nil {
		return nil, err
	}
	if err := s.check(ctx, access.PolicyContentPublish, st, nil); err != nil {
		return nil, err
	}
	if err := s.checkState(ctx, it); err != nil {
		return nil, err
	}
	sel, err := st.selectParts(o)
	if err != nil {
		return nil, fmt.Errorf("content: publish %s: %w", it.ID, err)
	}
	if err := s.check(ctx, access.PolicyContentPublish, st, st.publishLocales(sel)); err != nil {
		return nil, err
	}
	if !o.Force {
		if err := st.checkConflicts(ctx, c, sel); err != nil {
			return nil, fmt.Errorf("content: publish %s: %w", it.ID, err)
		}
	}
	v, err := s.publishParts(ctx, tx, st, sel)
	if err != nil {
		return nil, fmt.Errorf("content: publish %s: %w", it.ID, err)
	}
	return v, nil
}

// publishLocales are the locales a publish changes: the selected ones, or every locale of the new version when
// the shared part goes.
func (st *itemState) publishLocales(sel selection) []string {
	all := slices.Clone(sel.locales)
	if st.pub != nil {
		all = append(all, st.pub.codes()...)
	}
	slices.Sort(all)
	return sel.changedLocales(slices.Compact(all))
}

func (st *itemState) selectParts(o PublishOptions) (selection, error) {
	var sel selection
	sel.shared = st.drafts[""] != nil
	if len(o.Locales) == 0 {
		for part := range st.drafts {
			if part != "" {
				sel.locales = append(sel.locales, part)
			}
		}
	}
	for _, l := range o.Locales {
		if err := validateLocale(l); err != nil {
			return sel, err
		}
		if st.drafts[l] == nil {
			return sel, badRequest("no draft for locale %q", l)
		}
		sel.locales = append(sel.locales, l)
	}
	slices.Sort(sel.locales)
	sel.locales = slices.Compact(sel.locales)
	switch {
	case !sel.shared && len(sel.locales) == 0:
		return sel, badRequest("nothing to publish")
	case st.item.PublishedVersion == 0 && !slices.Contains(sel.locales, st.item.MainLocale):
		return sel, badRequest("the first publish must include the main locale %q", st.item.MainLocale)
	}
	return sel, nil
}

// checkConflicts fails when a selected part changed between the version it started from and the current one.
func (st *itemState) checkConflicts(ctx context.Context, c store.ContentStore, sel selection) error {
	bases := map[int]*pubVersion{}
	var conflicts []string
	check := func(part string) error {
		d := st.drafts[part]
		if d.BaseVersion >= st.item.PublishedVersion {
			return nil
		}
		base, ok := bases[d.BaseVersion]
		if !ok {
			var err error
			if base, err = loadBase(ctx, c, st.item.ID, d.BaseVersion); err != nil {
				return err
			}
			bases[d.BaseVersion] = base
		}
		differs, err := partDiffers(base, st.pub, part)
		if differs {
			conflicts = append(conflicts, partLabel(part))
		}
		return err
	}
	if sel.shared {
		if err := check(""); err != nil {
			return err
		}
	}
	for _, l := range sel.locales {
		if err := check(l); err != nil {
			return err
		}
	}
	if len(conflicts) > 0 {
		return &ConflictError{Parts: conflicts}
	}
	return nil
}

// loadBase reads a draft's base version; version 0 and versions that no longer exist read as empty.
func loadBase(ctx context.Context, c store.ContentStore, id ids.UUID, version int) (*pubVersion, error) {
	if version == 0 {
		return nil, nil
	}
	v, err := c.GetVersion(ctx, id, version)
	switch {
	case err == nil:
		return newPubVersion(v), nil
	case isStoreNotFound(err):
		return nil, nil
	}
	return nil, fmt.Errorf("content: load version %d of %s: %w", version, id, err)
}

// partDiffers compares one part between two versions; a nil version has an empty shared part and no locales.
func partDiffers(a, b *pubVersion, part string) (bool, error) {
	if part == "" {
		da, err := sharedOf(a)
		if err != nil {
			return false, err
		}
		db, err := sharedOf(b)
		return !equalDocs(da, db), err
	}
	da, oka, err := localeOf(a, part)
	if err != nil {
		return false, err
	}
	db, okb, err := localeOf(b, part)
	return oka != okb || (oka && !equalDocs(da, db)), err
}

func sharedOf(v *pubVersion) (map[string]any, error) {
	if v == nil {
		return nil, nil
	}
	return v.sharedDoc()
}

func localeOf(v *pubVersion, locale string) (map[string]any, bool, error) {
	if v == nil {
		return nil, false, nil
	}
	return v.localeDoc(locale)
}

// localeOut is one locale of the version being written.
type localeOut struct {
	code string
	data []byte
	doc  map[string]any // merged, normalised document
	name string
	refs []schema.RefValue
	errs []schema.FieldError

	final bool // doc went through Normalize and Prune (prepareOuts)
}

func (s *Service) publishParts(ctx context.Context, tx store.Tx, st *itemState, sel selection) (*View, error) {
	s.rememberRows(st)
	shared, sharedData, outs, err := st.nextVersion(sel)
	if err != nil {
		return nil, err
	}
	c := tx.Content()
	sharedData, err = s.prepareOuts(ctx, tx, st, shared, sharedData, outs)
	if err != nil {
		return nil, err
	}
	if err := s.checkRefs(ctx, c, st.typ, outs); err != nil {
		return nil, err
	}
	if err := reportFirstFailure(outs); err != nil {
		return nil, err
	}
	actor := audit.ActorFrom(ctx)
	if err := s.commitVersion(ctx, c, st, sharedData, outs, actor.String()); err != nil {
		return nil, err
	}
	drop := slices.Clone(sel.locales)
	if sel.shared {
		drop = append(drop, "")
	}
	if err := st.dropDrafts(ctx, c, drop); err != nil {
		return nil, err
	}
	if err := s.projectWorking(ctx, c, st); err != nil {
		return nil, err
	}
	changed := sel.changedLocales(codesOf(outs))
	data := map[string]any{"type": st.typ.Name, "version": st.item.PublishedVersion, "locales": changed}
	if err := s.record(ctx, tx, actor, "item.publish", st.item.ID, data); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, tx, "item.published", st.item, st.item.PublishedVersion, changed); err != nil {
		return nil, err
	}
	if err := s.afterPublish(ctx, tx, st, outs, changed); err != nil {
		return nil, err
	}
	v := st.publishedResult(outs)
	s.withActions(ctx, st, v)
	return v, nil
}

// nextVersion assembles the documents of the version after the current one: the current version with the
// selected draft parts replacing theirs.
func (st *itemState) nextVersion(sel selection) (shared map[string]any, sharedData []byte, outs []*localeOut, err error) {
	shared, sharedData = map[string]any{}, []byte("{}")
	if sel.shared {
		d := st.drafts[""]
		if shared, err = d.document(); err != nil {
			return nil, nil, nil, err
		}
		sharedData = d.Data
	} else if st.pub != nil {
		if shared, err = st.pub.sharedDoc(); err != nil {
			return nil, nil, nil, err
		}
		sharedData = st.pub.Shared
	}
	codes := sel.locales
	if st.pub != nil {
		codes = slices.Concat(st.pub.codes(), sel.locales)
		slices.Sort(codes)
		codes = slices.Compact(codes)
	}
	for _, code := range codes {
		out, err := st.localeOut(code, sel)
		if err != nil {
			return nil, nil, nil, err
		}
		outs = append(outs, out)
	}
	return shared, sharedData, outs, nil
}

func (st *itemState) localeOut(code string, sel selection) (*localeOut, error) {
	if slices.Contains(sel.locales, code) {
		d := st.drafts[code]
		doc, err := d.document()
		return &localeOut{code: code, data: d.Data, doc: doc}, err
	}
	doc, _, err := st.pub.localeDoc(code)
	if err != nil {
		return nil, err
	}
	return &localeOut{code: code, data: st.pub.Locales[st.pub.idx[code]].Data, doc: doc}, nil
}

// prepareOuts merges every locale with the shared document and runs normalisation, validation and the
// publish hooks on it. Problems are collected per locale; a hook returning an error aborts.
//
// Changes a BeforePublish hook makes to a document are persisted into the version, so snapshots, projection
// rebuilds and content migrations all see what publish saw: translatable fields go to that locale's data,
// shared fields come from the main locale's result (the first locale's when the main one is not in the
// version) and replace the shared data for every locale. It returns the shared data to store.
func (s *Service) prepareOuts(ctx context.Context, tx store.Tx, st *itemState, shared map[string]any, sharedData []byte, outs []*localeOut) ([]byte, error) {
	sc := s.o.Schema
	pre := make([]map[string]any, len(outs))
	for i, o := range outs {
		doc := mergeParts(cloneDoc(shared), cloneDoc(o.doc))
		sc.Normalize(st.typ, doc)
		sc.Prune(st.typ, doc)
		o.errs = sc.Validate(st.typ, doc, schema.ModePublish)
		for _, ev := range []hooks.Event{hooks.Validate, hooks.BeforePublish} {
			if ev == hooks.BeforePublish {
				pre[i] = cloneDoc(doc)
			}
			rep, err := s.runHooks(ctx, tx, st.item, ev, o.code, doc)
			if err != nil {
				return nil, err
			}
			o.errs = append(o.errs, rep.Errors()...)
		}
		o.doc, o.name, o.refs, o.final = doc, sc.Name(st.typ, doc), sc.Refs(st.typ, doc), true
	}
	return s.persistHookChanges(st, sharedData, pre, outs)
}

func (s *Service) persistHookChanges(st *itemState, sharedData []byte, pre []map[string]any, outs []*localeOut) ([]byte, error) {
	changed := false
	locChanged := make([]bool, len(outs))
	for i, o := range outs {
		if equalDocs(pre[i], o.doc) {
			continue
		}
		changed = true
		_, a := splitDoc(st.typ, pre[i])
		_, b := splitDoc(st.typ, o.doc)
		locChanged[i] = !equalDocs(a, b)
	}
	if !changed || len(outs) == 0 {
		return sharedData, nil
	}
	primary := outs[0]
	for _, o := range outs {
		if o.code == st.item.MainLocale {
			primary = o
		}
	}
	finalShared, _ := splitDoc(st.typ, primary.doc)
	data, err := encodeDoc(finalShared)
	if err != nil {
		return nil, fmt.Errorf("content: encode shared document of %s: %w", st.item.ID, err)
	}
	sc := s.o.Schema
	for i, o := range outs {
		_, loc := splitDoc(st.typ, o.doc)
		if locChanged[i] {
			if o.data, err = encodeDoc(loc); err != nil {
				return nil, fmt.Errorf("content: encode %s document of %s: %w", o.code, st.item.ID, err)
			}
		}
		o.doc = mergeParts(cloneDoc(finalShared), loc)
		o.name, o.refs = sc.Name(st.typ, o.doc), sc.Refs(st.typ, o.doc)
	}
	return data, nil
}

func reportFirstFailure(outs []*localeOut) error {
	for _, o := range outs {
		if len(o.errs) > 0 {
			return &ValidationError{Locale: o.code, Errors: o.errs}
		}
	}
	return nil
}

func codesOf(outs []*localeOut) []string {
	out := make([]string, len(outs))
	for i, o := range outs {
		out[i] = o.code
	}
	return out
}

// commitVersion writes the next version, moves the item to it and rebuilds the projections.
func (s *Service) commitVersion(ctx context.Context, c store.ContentStore, st *itemState, sharedData []byte,
	outs []*localeOut, by string) error {
	rev, err := s.currentRev()
	if err != nil {
		return err
	}
	now := s.now()
	v := store.Version{ItemID: st.item.ID, Version: st.item.PublishedVersion + 1, SchemaRev: rev,
		Shared: sharedData, Locales: make([]store.VersionLocale, len(outs)), CreatedAt: now, CreatedBy: by}
	for i, o := range outs {
		v.Locales[i] = store.VersionLocale{Locale: o.code, Name: o.name, Data: o.data}
	}
	if err := c.InsertVersion(ctx, v); err != nil {
		return fmt.Errorf("content: insert version %d of %s: %w", v.Version, st.item.ID, err)
	}
	st.item.PublishedVersion, st.item.UpdatedAt, st.item.UpdatedBy = v.Version, now, by
	if err := c.UpdateItem(ctx, st.item); err != nil {
		return itemErr(st.item.ID.String(), err)
	}
	st.pub = newPubVersion(v)
	if err := s.project(ctx, c, st, outs); err != nil {
		return err
	}
	return s.putSnapshots(ctx, c, st, outs, v.Version, now)
}

func (s *Service) afterPublish(ctx context.Context, tx store.Tx, st *itemState, outs []*localeOut, changed []string) error {
	for _, o := range outs {
		if !slices.Contains(changed, o.code) {
			continue
		}
		if _, err := s.runHooks(ctx, tx, st.item, hooks.AfterPublish, o.code, o.doc); err != nil {
			return err
		}
	}
	return nil
}

// publishedResult is the published view of the main locale (or the first one) of the new version.
func (st *itemState) publishedResult(outs []*localeOut) *View {
	pick := outs[0]
	for _, o := range outs {
		if o.code == st.item.MainLocale {
			pick = o
		}
	}
	return st.viewFrom(pick.code, pick.name, pick.doc, false)
}

func isStoreNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }
