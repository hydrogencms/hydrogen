package content

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// Create makes an item with a draft in one locale. Defaults are applied to the input.
func (s *Service) Create(ctx context.Context, in CreateInput) (*View, error) {
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) (err error) {
		ctx := store.WithTx(ctx, tx)
		v, err = s.create(ctx, tx, in)
		return err
	})
	return v, err
}

func (s *Service) create(ctx context.Context, tx store.Tx, in CreateInput) (*View, error) {
	t, err := s.typeOf(in.Type)
	if err != nil {
		return nil, err
	}
	locale := cmp.Or(in.Locale, s.o.DefaultLocale)
	if err := validateLocale(locale); err != nil {
		return nil, err
	}
	key, err := s.createKey(t, in.Key)
	if err != nil {
		return nil, err
	}
	if s.guarded(ctx) {
		if err := s.o.Access.Screen(ctx, access.PolicyContentCreate, access.Target{Type: t.Name, Locales: []string{locale}}); err != nil {
			return nil, err
		}
	}
	now, actor := s.now(), audit.ActorFrom(ctx)
	st := &itemState{typ: t, drafts: map[string]*draft{}, item: store.Item{
		ID: s.o.IDs.New(), Key: key, Type: t.Name, MainLocale: locale, AlwaysAvailable: in.AlwaysAvailable,
		CreatedAt: now, UpdatedAt: now, CreatedBy: actor.String(), UpdatedBy: actor.String(),
	}}
	v, err := s.writeDraft(ctx, tx, st, writeReq{locale: locale, patch: in.Fields, create: true})
	if err != nil {
		return nil, fmt.Errorf("content: create %s: %w", t.Name, err)
	}
	return v, nil
}

// createKey checks the key of a new item. Keys are unique across types by design, so a taken key answers 409
// duplicate_key: that is a known oracle for the existence of a key, not of an item. A key equal to a singleton
// type's name is reserved for that singleton, which would otherwise be blocked by a squatter.
func (s *Service) createKey(t *schema.Type, key string) (string, error) {
	if !t.Singleton && s.singletonName(key) {
		return "", badRequest("key %q is reserved for the singleton of that name", key)
	}
	switch {
	case t.Singleton && key != "" && key != t.Name:
		return "", badRequest("the key of singleton %q is %q", t.Name, t.Name)
	case t.Singleton:
		return t.Name, nil
	case key == "":
		return "", nil
	}
	return key, validateKey(key)
}

// Update applies a JSON merge patch to the draft of one locale.
func (s *Service) Update(ctx context.Context, id ids.UUID, in UpdateInput) (*View, error) {
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
		v, err = s.update(ctx, tx, it, in)
		return err
	})
	return v, err
}

func (s *Service) singletonName(key string) bool {
	t, ok := s.o.Schema.Type(key)
	return ok && t.Singleton
}

func (s *Service) update(ctx context.Context, tx store.Tx, it store.Item, in UpdateInput) (*View, error) {
	locale := cmp.Or(in.Locale, it.MainLocale)
	if err := validateLocale(locale); err != nil {
		return nil, err
	}
	st, err := s.newState(it)
	if err != nil {
		return nil, err
	}
	// Authorize before anything that depends on the item's state can answer: an unreadable item is not found.
	if err := st.load(ctx, tx.Content()); err != nil {
		return nil, err
	}
	if err := s.check(ctx, access.PolicyContentEdit, st, []string{locale}); err != nil {
		return nil, err
	}
	if err := st.applyMeta(in); err != nil {
		return nil, err
	}
	if in.Key != nil && st.item.Key != it.Key && !st.typ.Singleton && s.singletonName(st.item.Key) {
		return nil, badRequest("key %q is reserved for the singleton of that name", st.item.Key)
	}
	v, err := s.writeDraft(ctx, tx, st, writeReq{locale: locale, patch: in.Fields, loaded: true,
		meta: st.item.Key != it.Key || st.item.AlwaysAvailable != it.AlwaysAvailable})
	if err != nil {
		return nil, fmt.Errorf("content: update %s: %w", it.ID, err)
	}
	if it.PublishedVersion > 0 && (st.item.Key != it.Key || st.item.AlwaysAvailable != it.AlwaysAvailable) {
		// The key and availability apply to the published version at once, and the snapshots carry them.
		if err := s.rebuildSnapshots(ctx, tx.Content(), st.item); err != nil {
			return nil, fmt.Errorf("content: update %s: %w", it.ID, err)
		}
	}
	return v, nil
}

func (st *itemState) applyMeta(in UpdateInput) error {
	if in.Key != nil && *in.Key != st.item.Key {
		if st.typ.Singleton {
			return badRequest("the key of singleton %q cannot change", st.typ.Name)
		}
		if *in.Key != "" {
			if err := validateKey(*in.Key); err != nil {
				return err
			}
		}
		st.item.Key = *in.Key
	}
	if in.AlwaysAvailable != nil {
		st.item.AlwaysAvailable = *in.AlwaysAvailable
	}
	return nil
}

// UpdateSingleton is Update for a singleton type; the first write creates the item.
func (s *Service) UpdateSingleton(ctx context.Context, typ string, in UpdateInput) (*View, error) {
	if _, err := s.singletonType(typ); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) (err error) {
		ctx := store.WithTx(ctx, tx)
		it, err := getSingletonItem(ctx, tx, typ)
		switch {
		case err == nil:
			v, err = s.update(ctx, tx, it, in)
		case isNotFound(err):
			v, err = s.create(ctx, tx, CreateInput{Type: typ, Locale: in.Locale, Key: typ,
				AlwaysAvailable: in.AlwaysAvailable != nil && *in.AlwaysAvailable, Fields: in.Fields})
		}
		return err
	})
	return v, err
}

type writeReq struct {
	locale string
	patch  map[string]any
	create bool
	loaded bool // the item's state is loaded and the edit already authorized
	meta   bool // item metadata (key, alwaysAvailable) changed
}

// writeDraft is the draft write path shared by create and update: merge, hooks, validate, split, store.
func (s *Service) writeDraft(ctx context.Context, tx store.Tx, st *itemState, req writeReq) (*View, error) {
	c := tx.Content()
	if !req.create {
		if !req.loaded {
			if err := st.load(ctx, c); err != nil {
				return nil, err
			}
		}
		s.rememberRows(st)
	}
	doc, err := s.baseDoc(st, req.locale)
	if err != nil {
		return nil, err
	}
	applyPatch(doc, req.patch)
	if req.create {
		s.o.Schema.ApplyDefaults(st.typ, doc)
	}
	if err := s.prepareDraft(ctx, tx, st, req.locale, doc); err != nil {
		return nil, err
	}
	if err := s.authorizeDraft(ctx, st, req, doc); err != nil {
		return nil, err
	}
	now, actor := s.now(), audit.ActorFrom(ctx)
	if req.create {
		if err := c.InsertItem(ctx, st.item); err != nil {
			return nil, itemErr("key "+st.item.Key, err)
		}
	}
	name := s.o.Schema.Name(st.typ, doc)
	changed, err := s.storeDraftParts(ctx, c, st, req.locale, doc, name, now, actor)
	if err != nil {
		return nil, err
	}
	if changed || req.create {
		if err := s.projectWorking(ctx, c, st); err != nil {
			return nil, err
		}
	}
	if changed || req.meta || req.create {
		if err := s.finishWrite(ctx, tx, st, req, now, actor); err != nil {
			return nil, err
		}
	}
	v := st.viewFrom(req.locale, name, doc, true)
	s.withActions(ctx, st, v)
	return v, nil
}

// authorizeDraft checks the finished document of a draft write: a new item may be created, an existing one
// may still be edited after the change (so an item cannot be moved out of or into the principal's scope). A
// change to the shared part reaches every locale of the item, and the principal must be allowed to write
// them all.
func (s *Service) authorizeDraft(ctx context.Context, st *itemState, req writeReq, doc map[string]any) error {
	if !s.guarded(ctx) {
		return nil
	}
	if req.create {
		return s.o.Access.Can(ctx, access.PolicyContentCreate, access.Target{Type: st.typ.Name, Locales: []string{req.locale}, Doc: doc})
	}
	locales := []string{req.locale}
	shared, _ := splitDoc(st.typ, doc)
	cur, _, _, err := st.workingParts(req.locale)
	if err != nil {
		return err
	}
	if !equalDocs(shared, cur) || req.meta {
		locales = allLocales(st, req.locale)
	}
	main := doc
	if req.locale != st.item.MainLocale {
		_, loc, _, err := st.workingParts(st.item.MainLocale)
		if err != nil {
			return err
		}
		main = mergeParts(cloneDoc(shared), cloneDoc(loc))
		s.o.Schema.Normalize(st.typ, main)
		s.o.Schema.Prune(st.typ, main)
	}
	if err := s.o.Access.Can(ctx, access.PolicyContentEdit, s.targetOf(st, locales, main)); err != nil {
		return err
	}
	if req.meta && st.item.PublishedVersion > 0 {
		// The key and availability go live at once: that is publishing.
		return s.o.Access.Can(ctx, access.PolicyContentPublish, s.targetOf(st, locales, main))
	}
	return nil
}

// baseDoc is the working document of a locale (a private copy: draft parts over published parts).
func (s *Service) baseDoc(st *itemState, locale string) (map[string]any, error) {
	shared, loc, _, err := st.workingParts(locale)
	if err != nil {
		return nil, err
	}
	return mergeParts(cloneDoc(shared), cloneDoc(loc)), nil
}

// prepareDraft runs the pre-save pipeline on doc: hooks, normalisation, validation.
func (s *Service) prepareDraft(ctx context.Context, tx store.Tx, st *itemState, locale string, doc map[string]any) error {
	if _, err := s.runHooks(ctx, tx, st.item, hooks.BeforeValidate, locale, doc); err != nil {
		return err
	}
	s.o.Schema.Normalize(st.typ, doc)
	s.fillSlugs(st.typ, doc)
	s.o.Schema.Prune(st.typ, doc)
	errs := s.o.Schema.Validate(st.typ, doc, schema.ModeDraft)
	rep, err := s.runHooks(ctx, tx, st.item, hooks.Validate, locale, doc)
	if err != nil {
		return err
	}
	if errs = append(errs, rep.Errors()...); len(errs) > 0 {
		return &ValidationError{Locale: locale, Errors: errs}
	}
	_, err = s.runHooks(ctx, tx, st.item, hooks.BeforeSave, locale, doc)
	return err
}

// storeDraftParts writes the shared and locale parts of doc where they differ from what is published and
// from the current draft. It reports whether anything changed.
func (s *Service) storeDraftParts(ctx context.Context, c store.ContentStore, st *itemState, locale string,
	doc map[string]any, name string, now time.Time, actor audit.Actor) (bool, error) {
	pubShared, pubLoc, _, err := st.publishedParts(locale)
	if err != nil {
		return false, err
	}
	shared, loc := splitDoc(st.typ, doc)
	by := actor.String()
	a, err := st.syncPart(ctx, c, "", shared, pubShared, "", now, by)
	if err != nil {
		return false, err
	}
	b, err := st.syncPart(ctx, c, locale, loc, pubLoc, name, now, by)
	return a || b, err
}

// syncPart makes the draft part match doc: deleted when doc equals the published part (pub nil: no such
// published part), kept when it already holds doc, written otherwise.
func (st *itemState) syncPart(ctx context.Context, c store.ContentStore, part string, doc, pub map[string]any,
	name string, now time.Time, by string) (bool, error) {
	d := st.drafts[part]
	if pub != nil && equalDocs(doc, pub) {
		if d == nil {
			return false, nil
		}
		return true, st.dropDrafts(ctx, c, []string{part})
	}
	base := st.item.PublishedVersion
	if d != nil {
		cur, err := d.document()
		if err != nil {
			return false, err
		}
		if d.Name == name && equalDocs(doc, cur) {
			return false, nil
		}
		base = d.BaseVersion
	}
	data, err := encodeDoc(doc)
	if err != nil {
		return false, fmt.Errorf("content: encode %s part: %w", partLabel(part), err)
	}
	p := store.DraftPart{ItemID: st.item.ID, Part: part, BaseVersion: base, Name: name, Data: data, UpdatedAt: now, UpdatedBy: by}
	return true, st.setDraft(ctx, c, p, doc)
}

func partLabel(part string) string {
	if part == "" {
		return SharedPart
	}
	return part
}

func (s *Service) finishWrite(ctx context.Context, tx store.Tx, st *itemState, req writeReq, now time.Time, actor audit.Actor) error {
	c := tx.Content()
	action, topic := "item.update", "item.updated"
	if req.create {
		action, topic = "item.create", "item.created"
	} else {
		st.item.UpdatedAt, st.item.UpdatedBy = now, actor.String()
		if err := c.UpdateItem(ctx, st.item); err != nil {
			return itemErr(st.item.ID.String(), err)
		}
	}
	data := map[string]any{"type": st.typ.Name, "locale": req.locale}
	if err := s.record(ctx, tx, actor, action, st.item.ID, data); err != nil {
		return err
	}
	// Only a key or availability change of a published item alters what delivery serves.
	draft := !req.meta || st.item.PublishedVersion == 0
	return s.emitEvent(ctx, tx, topic, eventPayload{ID: st.item.ID, Type: st.item.Type, Version: st.item.PublishedVersion,
		Locales: []string{req.locale}, Draft: draft})
}

// Discard drops draft parts ("shared" or locale codes); nil drops all.
func (s *Service) Discard(ctx context.Context, id ids.UUID, parts []string) error {
	if err := s.prepare(ctx); err != nil {
		return err
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, true)
		if err != nil {
			return err
		}
		st, err := s.newState(it)
		if err != nil {
			return err
		}
		c := tx.Content()
		if err := st.load(ctx, c); err != nil {
			return err
		}
		if err := s.check(ctx, access.PolicyContentEdit, st, nil); err != nil {
			return err
		}
		drop, err := st.discardTargets(parts)
		if err != nil || len(drop) == 0 {
			return err
		}
		if err := s.check(ctx, access.PolicyContentEdit, st, discarded(st, drop)); err != nil {
			return err
		}
		s.rememberRows(st)
		if err := st.dropDrafts(ctx, c, drop); err != nil {
			return err
		}
		if err := s.projectWorking(ctx, c, st); err != nil {
			return err
		}
		actor := audit.ActorFrom(ctx)
		st.item.UpdatedAt, st.item.UpdatedBy = s.now(), actor.String()
		if err := c.UpdateItem(ctx, st.item); err != nil {
			return itemErr(id.String(), err)
		}
		if err := s.record(ctx, tx, actor, "item.discard", id, map[string]any{"parts": partNames(drop)}); err != nil {
			return err
		}
		return s.emitEvent(ctx, tx, "item.updated", eventPayload{ID: id, Type: it.Type, Version: it.PublishedVersion, Draft: true})
	})
}

// discarded are the locales dropping draft parts changes: dropping the shared part reaches every locale.
func discarded(st *itemState, drop []string) []string {
	if slices.Contains(drop, "") {
		return allLocales(st)
	}
	return drop
}

// discardTargets resolves the requested parts to existing draft parts, sorted.
func (st *itemState) discardTargets(parts []string) ([]string, error) {
	if len(parts) == 0 {
		return st.draftNames(), nil
	}
	var out []string
	for _, p := range parts {
		p = storagePart(p)
		if p != "" {
			if err := validateLocale(p); err != nil {
				return nil, err
			}
		}
		if _, ok := st.drafts[p]; ok && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out, nil
}
