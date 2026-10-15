package exportfmt

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// Exporter exports and imports the data of one module. Export writes records with w inside the export's single
// read transaction; Import restores them as they were, inside one write transaction, after the content has been
// imported. Records are the module's own JSON-encodable values.
type Exporter interface {
	Export(ctx context.Context, tx store.Tx, w *ModuleWriter) error
	Import(ctx context.Context, tx store.Tx, r *ModuleReader) error
}

// NamedExporter is a module's exporter; the name is the module's, and the file is modules/<name>.ndjson.
type NamedExporter struct {
	Name     string
	Exporter Exporter
}

const defaultBatch = 100

// ExportOptions configure Export.
type ExportOptions struct {
	DB    store.DB
	Dir   string // must not exist or be empty
	Clock clock.Clock
	// Version is the hydrogen version recorded in the manifest.
	Version string
	// SchemaHash and Schema (the canonical compiled JSON) identify the project's current schema.
	SchemaHash string
	Schema     []byte
	Modules    []NamedExporter
	// PublishedOnly exports only the published version of each published item: no history, no drafts, and
	// items that were never published are left out.
	PublishedOnly bool
	// Progress receives one line per step (may be nil).
	Progress io.Writer
	// Batch is the number of items read per query (default 100).
	Batch int
}

func progressf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", args...)
	}
}

// Export writes the whole content database and every module's data to o.Dir, from one read snapshot.
func Export(ctx context.Context, o ExportOptions) (*Manifest, error) {
	if o.Batch <= 0 {
		o.Batch = defaultBatch
	}
	w, err := NewWriter(o.Dir)
	if err != nil {
		return nil, err
	}
	m := Manifest{Hydrogen: o.Version, Dialect: o.DB.Dialect(), SchemaHash: o.SchemaHash,
		CreatedAt: o.Clock.Now().UTC(), PublishedOnly: o.PublishedOnly, Counts: Counts{Modules: map[string]int{}}}
	if err := w.WriteFile("schema/compiled.json", o.Schema); err != nil {
		return nil, err
	}
	err = store.ViewConsistent(ctx, o.DB, func(tx store.Tx) error {
		es, err := store.ExportOf(tx)
		if err != nil {
			return err
		}
		revs, err := es.SchemaRevisions(ctx)
		if err != nil {
			return err
		}
		used, err := exportItems(ctx, es, w, o, revs, &m)
		if err != nil {
			return err
		}
		if err := exportRevisions(w, revs, used, &m); err != nil {
			return err
		}
		if err := exportMigrations(ctx, es, w, &m); err != nil {
			return err
		}
		for _, mod := range SortedModules(o.Modules) {
			f, err := w.Create("modules/" + mod.Name + ".ndjson")
			if err != nil {
				return err
			}
			if err := mod.Exporter.Export(ctx, tx, &ModuleWriter{f}); err != nil {
				_ = f.Close()
				return fmt.Errorf("export module %s: %w", mod.Name, err)
			}
			n := f.records
			if err := f.Close(); err != nil {
				return err
			}
			m.Counts.Modules[mod.Name] = n
			progressf(o.Progress, "module %s: %d records", mod.Name, n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(m.Counts.Modules) == 0 {
		m.Counts.Modules = nil
	}
	m, err = w.Finish(m)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func exportItems(ctx context.Context, es store.ExportStore, w *Writer, o ExportOptions, revs []store.SchemaRevision, m *Manifest) (map[int]bool, error) {
	hashOf := make(map[int]string, len(revs))
	for _, r := range revs {
		hashOf[r.Rev] = r.Hash
	}
	used := map[int]bool{}
	f, err := w.Create("items.ndjson")
	if err != nil {
		return nil, err
	}
	var after ids.UUID
	for {
		items, err := es.ItemsAfter(ctx, after, o.Batch)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if len(items) == 0 {
			break
		}
		after = items[len(items)-1].ID
		list := make([]ids.UUID, len(items))
		for i, it := range items {
			list[i] = it.ID
		}
		versions, err := es.VersionsOf(ctx, list, o.PublishedOnly)
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		var drafts []store.DraftPart
		if !o.PublishedOnly {
			if drafts, err = es.DraftPartsFor(ctx, list); err != nil {
				_ = f.Close()
				return nil, err
			}
		}
		vi, di := 0, 0
		for _, it := range items {
			rec := ItemRecord{ID: it.ID, Key: it.Key, Type: it.Type, MainLocale: it.MainLocale, AlwaysAvailable: it.AlwaysAvailable,
				PublishedVersion: it.PublishedVersion, CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
				CreatedBy: it.CreatedBy, UpdatedBy: it.UpdatedBy, Versions: []VersionRecord{}}
			for ; vi < len(versions) && versions[vi].ItemID == it.ID; vi++ {
				v := versions[vi]
				hash, ok := hashOf[v.SchemaRev]
				if !ok {
					_ = f.Close()
					return nil, fmt.Errorf("export: version %d of %s refers to unknown schema revision %d", v.Version, it.ID, v.SchemaRev)
				}
				used[v.SchemaRev] = true
				rec.Versions = append(rec.Versions, versionRecord(v, hash))
			}
			for ; di < len(drafts) && drafts[di].ItemID == it.ID; di++ {
				p := drafts[di]
				rec.Drafts = append(rec.Drafts, DraftRecord{Part: p.Part, BaseVersion: p.BaseVersion, Name: p.Name,
					Data: doc(p.Data), UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy})
			}
			if o.PublishedOnly && it.PublishedVersion == 0 {
				continue
			}
			if err := f.WriteRecord(rec); err != nil {
				_ = f.Close()
				return nil, err
			}
			m.Counts.Items++
			m.Counts.Versions += len(rec.Versions)
			m.Counts.Drafts += len(rec.Drafts)
		}
		if m.Counts.Items/1000 != (m.Counts.Items-len(items))/1000 {
			progressf(o.Progress, "items: %d", m.Counts.Items)
		}
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	progressf(o.Progress, "items: %d (%d versions, %d drafts)", m.Counts.Items, m.Counts.Versions, m.Counts.Drafts)
	return used, nil
}

func doc(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

func versionRecord(v store.Version, hash string) VersionRecord {
	rec := VersionRecord{Version: v.Version, Schema: hash, CreatedAt: v.CreatedAt, CreatedBy: v.CreatedBy,
		Shared: doc(v.Shared), Locales: make([]LocaleRecord, len(v.Locales))}
	for i, l := range v.Locales {
		rec.Locales[i] = LocaleRecord{Locale: l.Locale, Name: l.Name, Data: doc(l.Data)}
	}
	return rec
}

func exportRevisions(w *Writer, revs []store.SchemaRevision, used map[int]bool, m *Manifest) error {
	for _, r := range revs {
		if !used[r.Rev] {
			continue
		}
		rf := RevisionFile{Hash: r.Hash, Rev: r.Rev, CreatedAt: r.CreatedAt, Compiled: doc(r.Compiled)}
		f, err := w.Create("schema/revisions/" + r.Hash + ".json")
		if err != nil {
			return err
		}
		if err := f.WriteRecord(rf); err != nil {
			_ = f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		m.Counts.SchemaRevisions++
	}
	return nil
}

func exportMigrations(ctx context.Context, es store.ExportStore, w *Writer, m *Manifest) error {
	log, err := es.ContentMigrationLog(ctx)
	if err != nil {
		return err
	}
	f, err := w.Create("content_migrations.ndjson")
	if err != nil {
		return err
	}
	for _, mig := range log {
		if err := f.WriteRecord(MigrationRecord{Name: mig.Name, AppliedAt: mig.AppliedAt}); err != nil {
			_ = f.Close()
			return err
		}
	}
	m.Counts.ContentMigrations = len(log)
	return f.Close()
}

// SortedModules returns exporters ordered by name.
func SortedModules(mods []NamedExporter) []NamedExporter {
	out := slices.Clone(mods)
	slices.SortFunc(out, func(a, b NamedExporter) int { return cmp.Compare(a.Name, b.Name) })
	return out
}
