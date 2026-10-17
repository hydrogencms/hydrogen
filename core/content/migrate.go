package content

import (
	"context"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

const migrateBatch = 100

const migrateLock = "content:migrate"

// PendingMigrations lists registered content migrations not yet applied, in the order they will run.
func (s *Service) PendingMigrations(ctx context.Context) ([]string, error) {
	pending, err := s.pending(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(pending))
	for i, m := range pending {
		names[i] = m.Name
	}
	return names, nil
}

func (s *Service) pending(ctx context.Context) ([]Migration, error) {
	var applied []string
	err := s.o.DB.View(ctx, func(tx store.Tx) (err error) {
		applied, err = tx.Content().AppliedContentMigrations(ctx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("content: applied migrations: %w", err)
	}
	var out []Migration
	for _, m := range s.migrations {
		if !slices.Contains(applied, m.Name) {
			out = append(out, m)
		}
	}
	return out, nil
}

// Migrate applies pending content migrations and returns their names. For each item of a migration's type
// (one transaction per item) the published version becomes a new version with the migrated documents,
// stamped with the current schema revision, and draft parts are migrated in place. Up may run again on an
// item if a run is interrupted, so it must be idempotent. The schema revision is recorded afterwards.
func (s *Service) Migrate(ctx context.Context) ([]string, error) {
	lock, ok, err := s.o.DB.Locker().TryLock(ctx, migrateLock)
	if err != nil {
		return nil, fmt.Errorf("content: migrate: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("content: migrate: another process is migrating")
	}
	defer func() { _ = lock.Release(context.WithoutCancel(ctx)) }()
	pending, err := s.pending(ctx)
	if err != nil {
		return nil, err
	}
	if len(pending) > 0 {
		if _, err := s.EnsureSchemaRev(ctx); err != nil {
			return nil, err
		}
	}
	var done []string
	for _, m := range pending {
		if err := s.runMigration(ctx, m); err != nil {
			return done, fmt.Errorf("content: migration %s: %w", m.Name, err)
		}
		done = append(done, m.Name)
	}
	if _, err := s.EnsureSchemaRev(ctx); err != nil {
		return done, err
	}
	return done, nil
}

func (s *Service) runMigration(ctx context.Context, m Migration) error {
	if _, err := s.typeOf(m.Type); err != nil {
		return err
	}
	var after ids.UUID
	for {
		var batch []ids.UUID
		err := s.o.DB.View(ctx, func(tx store.Tx) (err error) {
			batch, err = tx.Content().ItemIDsOfType(ctx, m.Type, after, migrateBatch)
			return err
		})
		if err != nil {
			return err
		}
		for _, id := range batch {
			err := s.o.DB.Tx(ctx, func(tx store.Tx) error { return s.migrateItem(ctx, tx, m, id) })
			if err != nil {
				return fmt.Errorf("item %s: %w", id, err)
			}
		}
		if len(batch) < migrateBatch {
			break
		}
		after = batch[len(batch)-1]
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		return tx.Content().RecordContentMigration(ctx, m.Name, s.now())
	})
}

// draftPlan is a draft part's migrated content, computed from the pre-migration state.
type draftPlan struct {
	part string
	doc  map[string]any
	name string
}

func (s *Service) migrateItem(ctx context.Context, tx store.Tx, m Migration, id ids.UUID) error {
	it, err := getItemAny(ctx, tx, id, true)
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
	plans, err := s.planDrafts(m, st)
	if err != nil {
		return err
	}
	oldVersion := st.item.PublishedVersion
	if st.pub != nil {
		sharedData, outs, err := s.migratePublished(m, st)
		if err != nil {
			return err
		}
		if err := s.commitVersion(ctx, c, st, sharedData, outs, audit.System.String()); err != nil {
			return err
		}
		if err := s.emit(ctx, tx, "item.published", st.item, st.item.PublishedVersion, codesOf(s.withDerived(outs))); err != nil {
			return err
		}
	}
	if err := s.writePlans(ctx, c, st, plans, oldVersion); err != nil {
		return err
	}
	if len(plans) > 0 {
		if err := s.draftChanged(ctx, tx, st); err != nil {
			return err
		}
	}
	if err := s.projectWorking(ctx, c, st); err != nil {
		return err
	}
	data := map[string]any{"migration": m.Name, "type": st.typ.Name, "version": st.item.PublishedVersion}
	return s.record(ctx, tx, audit.System, "item.migrate", id, data)
}

// runUp runs the migration on a private copy of one locale's merged document and returns the result.
func (s *Service) runUp(m Migration, st *itemState, locale string, shared, loc map[string]any) (map[string]any, error) {
	md := &MigrationDoc{ItemID: st.item.ID, Locale: locale, Fields: mergeParts(cloneDoc(shared), cloneDoc(loc))}
	if err := m.Up(md); err != nil {
		return nil, fmt.Errorf("locale %q: %w", locale, err)
	}
	if md.Fields == nil {
		md.Fields = map[string]any{}
	}
	return md.Fields, nil
}

// migratePublished migrates every locale of the published version. The shared document comes from the main
// locale's result (or the first locale's).
func (s *Service) migratePublished(m Migration, st *itemState) (sharedData []byte, outs []*localeOut, err error) {
	shared, err := st.pub.sharedDoc()
	if err != nil {
		return nil, nil, err
	}
	results := make(map[string]map[string]any, len(st.pub.Locales))
	sparse := make(map[string]map[string]any, len(st.pub.Locales))
	for _, code := range st.pub.codes() {
		loc, _, err := st.pub.localeDoc(code)
		if err != nil {
			return nil, nil, err
		}
		sparse[code] = loc
	}
	for _, code := range st.pub.codes() {
		loc := sparse[code]
		if chain := st.chain(code); chain != nil {
			loc = unionChain(chain, sparse) // migrations see the resolved document
		}
		var err error
		if results[code], err = s.runUp(m, st, code, shared, loc); err != nil {
			return nil, nil, err
		}
	}
	primary := primaryOf(st.pub.codes(), st.item.MainLocale)
	newShared, _ := splitDoc(st.typ, results[primary])
	if sharedData, err = encodeDoc(newShared); err != nil {
		return nil, nil, err
	}
	resolvedLocs := make(map[string]map[string]any, len(results))
	for code, r := range results {
		_, resolvedLocs[code] = splitDoc(st.typ, r)
	}
	for _, code := range st.pub.codes() {
		loc := resolvedLocs[code]
		keep := loc
		if chain := st.chain(code); chain != nil {
			keep = sparsify(loc, sparse[code], parentOf(chain, resolvedLocs), nil)
		}
		data, err := encodeDoc(keep)
		if err != nil {
			return nil, nil, err
		}
		doc := mergeParts(newShared, loc)
		outs = append(outs, &localeOut{code: code, data: data, doc: doc,
			name: s.o.Schema.Name(st.typ, doc), refs: s.o.Schema.Refs(st.typ, doc)})
	}
	return sharedData, outs, nil
}

func primaryOf(codes []string, main string) string {
	if slices.Contains(codes, main) {
		return main
	}
	return codes[0]
}

// planDrafts migrates the draft parts from the item's working documents.
func (s *Service) planDrafts(m Migration, st *itemState) ([]draftPlan, error) {
	var locales []string
	for _, part := range st.draftNames() {
		if part != "" {
			locales = append(locales, part)
		}
	}
	_, hasShared := st.drafts[""]
	if len(locales) == 0 && !hasShared {
		return nil, nil
	}
	carriers := locales
	if len(carriers) == 0 {
		carriers = []string{st.item.MainLocale}
	}
	results := make(map[string]map[string]any, len(carriers))
	names := make(map[string]string, len(carriers))
	run := func(code string) error {
		if _, done := results[code]; done {
			return nil
		}
		shared, loc, _, err := st.workingParts(code)
		if err != nil {
			return err
		}
		if results[code], err = s.runUp(m, st, code, shared, loc); err != nil {
			return err
		}
		names[code] = s.o.Schema.Name(st.typ, results[code])
		return nil
	}
	for _, code := range carriers {
		if err := run(code); err != nil {
			return nil, err
		}
		// Overlay locales are stored sparse against their ancestors' migrated documents.
		if chain := st.chain(code); chain != nil {
			for _, a := range chain[1:] {
				if res, err := st.resolve(true, []string{a}); err != nil {
					return nil, err
				} else if res.avail {
					if err := run(a); err != nil {
						return nil, err
					}
				}
			}
		}
	}
	resolvedLocs := make(map[string]map[string]any, len(results))
	for code, r := range results {
		_, resolvedLocs[code] = splitDoc(st.typ, r)
	}
	var plans []draftPlan
	if hasShared {
		sh, _ := splitDoc(st.typ, results[primaryOf(carriers, st.item.MainLocale)])
		plans = append(plans, draftPlan{part: "", doc: sh})
	}
	for _, code := range locales {
		loc := resolvedLocs[code]
		if chain := st.chain(code); chain != nil {
			own, _, _, err := st.rawLoc(true, code)
			if err != nil {
				return nil, err
			}
			loc = sparsify(loc, own, parentOf(chain, resolvedLocs), nil)
		}
		plans = append(plans, draftPlan{part: code, doc: loc, name: names[code]})
	}
	return plans, nil
}

// writePlans stores migrated draft parts; parts that started from the replaced version are rebased on the
// new one so the migration is not reported as a conflict.
func (s *Service) writePlans(ctx context.Context, c store.ContentStore, st *itemState, plans []draftPlan, oldVersion int) error {
	now := s.now()
	for _, p := range plans {
		d := st.drafts[p.part]
		data, err := encodeDoc(p.doc)
		if err != nil {
			return err
		}
		part := d.DraftPart
		part.Data, part.Name, part.UpdatedAt, part.UpdatedBy = data, p.name, now, audit.System.String()
		if part.BaseVersion == oldVersion {
			part.BaseVersion = st.item.PublishedVersion
		}
		if err := st.setDraft(ctx, c, part, p.doc); err != nil {
			return err
		}
	}
	return nil
}
