package content

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// Create makes an item with a draft in one locale. Defaults are applied to the input.
func (s *Service) Create(ctx context.Context, in CreateInput) (*View, error) {
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) (err error) {
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
	key, err := createKey(t, in.Key)
	if err != nil {
		return nil, err
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

func createKey(t *schema.Type, key string) (string, error) {
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
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		it, err := getItem(ctx, tx, id, true)
		if err != nil {
			return err
		}
		v, err = s.update(ctx, tx, it, in)
		return err
	})
	return v, err
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
	if err := st.applyMeta(in); err != nil {
		return nil, err
	}
	v, err := s.writeDraft(ctx, tx, st, writeReq{locale: locale, patch: in.Fields, meta: st.item.Key != it.Key || st.item.AlwaysAvailable != it.AlwaysAvailable})
	if err != nil {
		return nil, fmt.Errorf("content: update %s: %w", it.ID, err)
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
	var v *View
	err := s.o.DB.Tx(ctx, func(tx store.Tx) (err error) {
		it, err := getItemByKey(ctx, tx, typ)
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
	meta   bool // item metadata (key, alwaysAvailable) changed
}

// writeDraft is the draft write path shared by create and update: merge, hooks, validate, split, store.
func (s *Service) writeDraft(ctx context.Context, tx store.Tx, st *itemState, req writeReq) (*View, error) {
	c := tx.Content()
	if !req.create {
		if err := st.load(ctx, c); err != nil {
			return nil, err
		}
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
	if changed || req.meta || req.create {
		if err := s.finishWrite(ctx, tx, st, req, now, actor); err != nil {
			return nil, err
		}
	}
	return st.viewFrom(req.locale, name, doc, true), nil
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
	return s.emit(ctx, tx, topic, st.item, st.item.PublishedVersion, []string{req.locale})
}

// Discard drops draft parts ("shared" or locale codes); nil drops all.
func (s *Service) Discard(ctx context.Context, id ids.UUID, parts []string) error {
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		it, err := getItem(ctx, tx, id, true)
		if err != nil {
			return err
		}
		st, err := s.newState(it)
		if err != nil {
			return err
		}
		c := tx.Content()
		if err := st.loadDrafts(ctx, c); err != nil {
			return err
		}
		drop, err := st.discardTargets(parts)
		if err != nil || len(drop) == 0 {
			return err
		}
		if err := st.dropDrafts(ctx, c, drop); err != nil {
			return err
		}
		actor := audit.ActorFrom(ctx)
		st.item.UpdatedAt, st.item.UpdatedBy = s.now(), actor.String()
		if err := c.UpdateItem(ctx, st.item); err != nil {
			return itemErr(id.String(), err)
		}
		return s.record(ctx, tx, actor, "item.discard", id, map[string]any{"parts": partNames(drop)})
	})
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
