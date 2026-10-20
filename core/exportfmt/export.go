package exportfmt

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
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

// FileStorage is the byte store of uploaded files (the media service, keyed by SHA-256 hex).
type FileStorage interface {
	// Open streams a stored file; the caller closes it.
	Open(ctx context.Context, sha string) (io.ReadCloser, error)
	// Restore stores the file and records its upload. The implementation must verify the bytes against the
	// record (SHA-256 of the content, size, type) and refuse a file that does not match.
	Restore(ctx context.Context, rec store.Upload, r io.Reader) error
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
	// Files exports the uploaded files items refer to (files/<sha>, uploads.ndjson); nil exports none.
	Files FileStorage
	// PublishedOnly exports only the published version of each published item: no history, no drafts, and
	// items that were never published are left out.
	PublishedOnly bool
	// Progress receives one line per step (may be nil).
	Progress io.Writer
	// Batch is the number of items read per query (default 100).
	Batch int
	// Layout of the item records: LayoutNDJSON (default) or LayoutItems.
	Layout string
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
	if o.Layout == "" {
		o.Layout = LayoutNDJSON
	}
	if o.Layout != LayoutNDJSON && o.Layout != LayoutItems {
		return nil, fmt.Errorf("exportfmt: unknown layout %q (use %s or %s)", o.Layout, LayoutNDJSON, LayoutItems)
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
		shas := map[string]bool{}
		used, err := exportItems(ctx, es, w, o, revs, &m, shas)
		if err != nil {
			return err
		}
		if err := exportUploads(ctx, tx, w, o, shas, &m); err != nil {
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

func exportItems(ctx context.Context, es store.ExportStore, w *Writer, o ExportOptions, revs []store.SchemaRevision, m *Manifest, shas map[string]bool) (map[int]bool, error) {
	hashOf := make(map[int]string, len(revs))
	for _, r := range revs {
		hashOf[r.Rev] = r.Hash
	}
	used := map[int]bool{}
	var f *FileWriter
	if o.Layout == LayoutNDJSON {
		var err error
		if f, err = w.Create("items.ndjson"); err != nil {
			return nil, err
		}
	}
	closeFile := func() {
		if f != nil {
			_ = f.Close()
		}
	}
	var (
		after  ids.UUID
		line   bytes.Buffer
		pretty bytes.Buffer
		sum    = sha256.New()
		size   int64
	)
	for {
		items, err := es.ItemsAfter(ctx, after, o.Batch)
		if err != nil {
			closeFile()
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
			closeFile()
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
					closeFile()
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
			if o.Files != nil {
				for _, v := range rec.Versions {
					scanShas(v.Shared, shas)
					for _, l := range v.Locales {
						scanShas(l.Data, shas)
					}
				}
				for _, d := range rec.Drafts {
					scanShas(d.Data, shas)
				}
			}
			if err := encodeLine(&line, rec); err != nil {
				closeFile()
				return nil, fmt.Errorf("export: item %s: %w", it.ID, err)
			}
			sum.Write(line.Bytes())
			size += int64(line.Len())
			if f != nil {
				err = f.writeLine(line.Bytes())
			} else {
				err = writeItemFile(w, rec, line.Bytes(), &pretty)
			}
			if err != nil {
				closeFile()
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
	if f != nil {
		if err := f.Close(); err != nil {
			return nil, err
		}
	}
	m.ItemStream = &Stream{SHA256: hex.EncodeToString(sum.Sum(nil)), Bytes: size}
	progressf(o.Progress, "items: %d (%d versions, %d drafts)", m.Counts.Items, m.Counts.Versions, m.Counts.Drafts)
	return used, nil
}

// itemFileName is where an item lives in the items layout.
func itemFileName(typ string, id ids.UUID) string {
	return "items/" + typ + "/" + id.String() + ".json"
}

// writeItemFile writes the record (its compact line) indented by two spaces, ending with a newline.
func writeItemFile(w *Writer, rec ItemRecord, line []byte, buf *bytes.Buffer) error {
	buf.Reset()
	if err := json.Indent(buf, bytes.TrimSuffix(line, []byte{'\n'}), "", "  "); err != nil {
		return fmt.Errorf("export: item %s: %w", rec.ID, err)
	}
	buf.WriteByte('\n')
	return w.WriteFile(itemFileName(rec.Type, rec.ID), buf.Bytes())
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

// scanShas adds every string in doc that looks like a SHA-256 (64 lower-case hex digits between quotes): the
// ids of file values. Whether a file exists is up to the upload records.
func scanShas(doc []byte, into map[string]bool) {
	const n = 64
	for i := 0; i+n+2 <= len(doc); i++ {
		if doc[i] != '"' || doc[i+n+1] != '"' {
			continue
		}
		s := doc[i+1 : i+1+n]
		hex := true
		for _, c := range s {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
				hex = false
				break
			}
		}
		if hex {
			into[string(s)] = true
			i += n + 1
		}
	}
}

// exportUploads writes the files the exported documents refer to and their upload records. Strings that match no
// upload record are not files.
func exportUploads(ctx context.Context, tx store.Tx, w *Writer, o ExportOptions, shas map[string]bool, m *Manifest) error {
	if o.Files == nil {
		return nil
	}
	var recs []UploadRecord
	for _, sha := range slices.Sorted(maps.Keys(shas)) {
		u, err := tx.Uploads().Get(ctx, sha)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return fmt.Errorf("export: upload %s: %w", sha, err)
		}
		if err := copyFile(ctx, w, o.Files, sha); err != nil {
			return err
		}
		recs = append(recs, UploadRecord{SHA: u.SHA, MIME: u.MIME, Size: u.Size, Width: u.Width, Height: u.Height,
			CreatedBy: u.CreatedBy, CreatedAt: u.CreatedAt.UTC()})
	}
	f, err := w.Create("uploads.ndjson")
	if err != nil {
		return err
	}
	for _, r := range recs {
		if err := f.WriteRecord(r); err != nil {
			_ = f.Close()
			return err
		}
	}
	m.Counts.Uploads = len(recs)
	progressf(o.Progress, "uploads: %d files", len(recs))
	return f.Close()
}

func copyFile(ctx context.Context, w *Writer, src FileStorage, sha string) error {
	rc, err := src.Open(ctx, sha)
	if err != nil {
		return fmt.Errorf("export: file %s: %w", sha, err)
	}
	defer func() { _ = rc.Close() }()
	f, err := w.Create("files/" + sha)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, rc); err != nil {
		_ = f.Close()
		return fmt.Errorf("export: file %s: %w", sha, err)
	}
	return f.Close()
}
