package exportfmt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

const defaultParallel = 4

// ImportedTopic is the outbox event an import ends with, so caches purge everything.
const ImportedTopic = "content.imported"

// ErrNotEmpty is returned when the target database already has content.
var ErrNotEmpty = errors.New("exportfmt: the content database is not empty; import needs an empty one")

// ErrSchemaMismatch is returned when the export was made under another schema.
var ErrSchemaMismatch = errors.New("exportfmt: schema mismatch")

// ImportOptions configure Import.
type ImportOptions struct {
	DB      store.DB
	Content *content.Service
	Outbox  *outbox.Outbox
	Clock   clock.Clock
	Dir     string
	// AllowSchemaChange accepts an export made under a different schema than the project's current one.
	AllowSchemaChange bool
	Modules           []NamedExporter
	// Files restores the uploaded files of the export (files/<sha> with their upload records). An export that
	// holds files cannot be imported without it.
	Files    FileStorage
	Progress io.Writer
	// Batch is the number of items written per transaction (default 100).
	Batch int
	// Parallel is the number of batches written at once (default: 1 on a single-node database, 4 on a
	// networked one, where it overlaps round trips).
	Parallel int
}

// ImportResult are the numbers of records written.
type ImportResult struct {
	Items, Versions, Drafts, ContentMigrations, Uploads int
	Modules                                             map[string]int
}

type importedEvent struct {
	Items    int `json:"items"`
	Versions int `json:"versions"`
}

// Import restores an export into an empty content database, keeping ids. It verifies the checksums first and
// writes nothing if they fail. Items are written in batches, one transaction each; a failure part way leaves the
// batches already committed, so import into a fresh database and recreate it to retry. Derived data (references,
// field index, search text, snapshots) is rebuilt from the published versions, without hooks or events; the one
// event emitted is content.imported.
func Import(ctx context.Context, o ImportOptions) (*ImportResult, error) {
	if o.Batch <= 0 {
		o.Batch = defaultBatch
	}
	r, err := Open(o.Dir)
	if err != nil {
		return nil, err
	}
	progressf(o.Progress, "verifying %d files", len(r.Manifest().Files))
	if err := r.Verify(); err != nil {
		return nil, err
	}
	man := r.Manifest()
	if want := o.Content.Schema().Hash(); man.SchemaHash != want && !o.AllowSchemaChange {
		return nil, fmt.Errorf("%w: the export was made under schema %s, this project's is %s (use -allow-schema-change to import anyway)",
			ErrSchemaMismatch, short(man.SchemaHash), short(want))
	}
	mods, err := matchModules(r, o.Modules)
	if err != nil {
		return nil, err
	}
	err = store.ViewConsistent(ctx, o.DB, func(tx store.Tx) error {
		es, err := store.ExportOf(tx)
		if err != nil {
			return err
		}
		n, err := es.CountItems(ctx)
		if err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%w (%d items)", ErrNotEmpty, n)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if r.Has("uploads.ndjson") && o.Files == nil {
		return nil, errors.New("exportfmt: the export holds uploaded files, but this project has no media storage")
	}
	res := &ImportResult{Modules: map[string]int{}}
	if err := importUploads(ctx, o, r, res); err != nil {
		return nil, err
	}
	revs, err := importRevisions(ctx, o, r)
	if err != nil {
		return nil, err
	}
	if err := importItems(ctx, o, r, revs, res); err != nil {
		return nil, err
	}
	if res.Items != man.Counts.Items || res.Versions != man.Counts.Versions || res.Drafts != man.Counts.Drafts {
		return nil, fmt.Errorf("exportfmt: imported %d items, %d versions, %d drafts; manifest lists %d, %d, %d",
			res.Items, res.Versions, res.Drafts, man.Counts.Items, man.Counts.Versions, man.Counts.Drafts)
	}
	if err := importMigrations(ctx, o, r, res); err != nil {
		return nil, err
	}
	for _, mod := range mods {
		rr, err := r.Records("modules/" + mod.Name + ".ndjson")
		if err != nil {
			return nil, err
		}
		err = o.DB.Tx(ctx, func(tx store.Tx) error { return mod.Exporter.Import(ctx, tx, &ModuleReader{rr}) })
		_ = rr.Close()
		if err != nil {
			return nil, fmt.Errorf("import module %s: %w", mod.Name, err)
		}
		res.Modules[mod.Name] = rr.Total()
		progressf(o.Progress, "module %s: %d records", mod.Name, rr.Total())
	}
	// With the project's own schema the data is current. Otherwise the imported revision stays the latest, so
	// serve reports the changes that need a content migration before it records the project's schema.
	if man.SchemaHash == o.Content.Schema().Hash() {
		if _, err := o.Content.EnsureSchemaRev(ctx); err != nil {
			return nil, err
		}
	}
	err = o.DB.Tx(ctx, func(tx store.Tx) error {
		_, err := o.Outbox.Publish(ctx, tx, ImportedTopic, importedEvent{Items: res.Items, Versions: res.Versions})
		return err
	})
	if err != nil {
		return nil, err
	}
	progressf(o.Progress, "imported %d items (%d versions, %d drafts), %d content migrations", res.Items, res.Versions, res.Drafts, res.ContentMigrations)
	return res, nil
}

func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}

// matchModules pairs the module files of the export with the project's exporters, in name order. A file without
// an exporter is an error: its data would be dropped silently.
func matchModules(r *Reader, have []NamedExporter) ([]NamedExporter, error) {
	var out []NamedExporter
	for _, name := range r.Names("modules/") {
		mod := strings.TrimSuffix(strings.TrimPrefix(name, "modules/"), ".ndjson")
		i := slices.IndexFunc(have, func(e NamedExporter) bool { return e.Name == mod })
		if i < 0 || !strings.HasSuffix(name, ".ndjson") {
			return nil, fmt.Errorf("exportfmt: the export holds data of module %q, which this project does not have", mod)
		}
		out = append(out, have[i])
	}
	return out, nil
}

// importRevisions re-creates the schema revisions in their original order and returns the new revision
// number of each hash.
func importRevisions(ctx context.Context, o ImportOptions, r *Reader) (map[string]int, error) {
	var files []RevisionFile
	for _, name := range r.Names("schema/revisions/") {
		data, err := r.ReadFile(name)
		if err != nil {
			return nil, err
		}
		var rf RevisionFile
		if err := json.Unmarshal(data, &rf); err != nil {
			return nil, fmt.Errorf("exportfmt: %s: %w", name, err)
		}
		if name != "schema/revisions/"+rf.Hash+".json" {
			return nil, fmt.Errorf("exportfmt: %s holds revision %s", name, rf.Hash)
		}
		files = append(files, rf)
	}
	slices.SortFunc(files, func(a, b RevisionFile) int { return a.Rev - b.Rev })
	revs := make(map[string]int, len(files))
	err := o.DB.Tx(ctx, func(tx store.Tx) error {
		for _, rf := range files {
			rev, err := tx.Content().AddSchemaRev(ctx, rf.Hash, rf.Compiled, rf.CreatedAt)
			if err != nil {
				return err
			}
			revs[rf.Hash] = rev
		}
		return nil
	})
	return revs, err
}

func importItems(ctx context.Context, o ImportOptions, r *Reader, revs map[string]int, res *ImportResult) error {
	rr, err := r.Records("items.ndjson")
	if err != nil {
		return err
	}
	defer func() { _ = rr.Close() }()
	workers := o.Parallel
	if workers <= 0 {
		workers = 1
		if o.DB.Capabilities().MultiNode {
			workers = defaultParallel // a networked database: overlap the round trips
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
		work     = make(chan []ItemRecord)
	)
	fail := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}
	for range workers {
		wg.Go(func() {
			for batch := range work {
				var n, nv, nd int
				err := o.DB.Tx(ctx, func(tx store.Tx) error {
					n, nv, nd = 0, 0, 0
					for _, rec := range batch {
						v, err := writeItem(ctx, tx.Content(), o.Content, rec, revs)
						if err != nil {
							return fmt.Errorf("item %s: %w", rec.ID, err)
						}
						n++
						nv += v
						nd += len(rec.Drafts)
					}
					return nil
				})
				if err != nil {
					fail(fmt.Errorf("exportfmt: import items: %w", err))
					continue
				}
				mu.Lock()
				before := res.Items
				res.Items, res.Versions, res.Drafts = res.Items+n, res.Versions+nv, res.Drafts+nd
				if res.Items/1000 != before/1000 {
					progressf(o.Progress, "items: %d/%d", res.Items, rr.Total())
				}
				mu.Unlock()
			}
		})
	}
	readErr := func() error {
		defer close(work)
		for {
			batch := make([]ItemRecord, 0, o.Batch)
			for len(batch) < o.Batch {
				var rec ItemRecord
				ok, err := rr.Next(&rec)
				if err != nil {
					return err
				}
				if !ok {
					break
				}
				batch = append(batch, rec)
			}
			if len(batch) == 0 {
				return nil
			}
			select {
			case work <- batch:
			case <-ctx.Done():
				return nil
			}
		}
	}()
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	if readErr != nil {
		return readErr
	}
	progressf(o.Progress, "items: %d", res.Items)
	return nil
}

// writeItem inserts the item, its versions and drafts, then rebuilds its derived data from the published
// version. It returns the number of versions written.
func writeItem(ctx context.Context, c store.ContentStore, svc *content.Service, rec ItemRecord, revs map[string]int) (int, error) {
	it := store.Item{ID: rec.ID, Key: rec.Key, Type: rec.Type, MainLocale: rec.MainLocale, AlwaysAvailable: rec.AlwaysAvailable,
		PublishedVersion: rec.PublishedVersion, CreatedAt: rec.CreatedAt, UpdatedAt: rec.UpdatedAt,
		CreatedBy: rec.CreatedBy, UpdatedBy: rec.UpdatedBy}
	if err := c.InsertItem(ctx, it); err != nil {
		return 0, err
	}
	var published *store.Version
	for _, vr := range rec.Versions {
		rev, ok := revs[vr.Schema]
		if !ok {
			return 0, fmt.Errorf("version %d refers to schema revision %s, which the export does not hold", vr.Version, vr.Schema)
		}
		v := store.Version{ItemID: rec.ID, Version: vr.Version, SchemaRev: rev, Shared: vr.Shared,
			Locales: make([]store.VersionLocale, len(vr.Locales)), CreatedAt: vr.CreatedAt, CreatedBy: vr.CreatedBy}
		for i, l := range vr.Locales {
			v.Locales[i] = store.VersionLocale{Locale: l.Locale, Name: l.Name, Data: l.Data}
		}
		if err := c.InsertVersion(ctx, v); err != nil {
			return 0, err
		}
		if v.Version == rec.PublishedVersion {
			published = &v
		}
	}
	if rec.PublishedVersion > 0 && published == nil {
		return 0, fmt.Errorf("published version %d is not in the export", rec.PublishedVersion)
	}
	for _, d := range rec.Drafts {
		err := c.PutDraftPart(ctx, store.DraftPart{ItemID: rec.ID, Part: d.Part, BaseVersion: d.BaseVersion, Name: d.Name,
			Data: d.Data, UpdatedAt: d.UpdatedAt, UpdatedBy: d.UpdatedBy})
		if err != nil {
			return 0, err
		}
	}
	return len(rec.Versions), svc.RebuildProjections(ctx, c, it, published)
}

func importMigrations(ctx context.Context, o ImportOptions, r *Reader, res *ImportResult) error {
	rr, err := r.Records("content_migrations.ndjson")
	if err != nil {
		return err
	}
	defer func() { _ = rr.Close() }()
	return o.DB.Tx(ctx, func(tx store.Tx) error {
		for {
			var m MigrationRecord
			ok, err := rr.Next(&m)
			if err != nil || !ok {
				return err
			}
			if err := tx.Content().RecordContentMigration(ctx, m.Name, m.AppliedAt); err != nil {
				return err
			}
			res.ContentMigrations++
		}
	})
}

// importUploads stores the exported files and records them as uploads. They are fresh as far as garbage
// collection goes, so a file whose item is imported later is not collected in between.
func importUploads(ctx context.Context, o ImportOptions, r *Reader, res *ImportResult) error {
	if !r.Has("uploads.ndjson") {
		return nil
	}
	rr, err := r.Records("uploads.ndjson")
	if err != nil {
		return err
	}
	defer func() { _ = rr.Close() }()
	for {
		var rec UploadRecord
		ok, err := rr.Next(&rec)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if err := importUpload(ctx, o, r, rec); err != nil {
			return fmt.Errorf("exportfmt: import upload %s: %w", rec.SHA, err)
		}
		res.Uploads++
	}
	progressf(o.Progress, "uploads: %d files", res.Uploads)
	return nil
}

func importUpload(ctx context.Context, o ImportOptions, r *Reader, rec UploadRecord) error {
	if err := checkName("files/" + rec.SHA); err != nil || len(rec.SHA) != 64 {
		return fmt.Errorf("invalid sha")
	}
	f, err := r.OpenFile("files/" + rec.SHA)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return o.Files.Restore(ctx, store.Upload{SHA: rec.SHA, MIME: rec.MIME, Size: rec.Size, Width: rec.Width,
		Height: rec.Height, CreatedBy: rec.CreatedBy, CreatedAt: rec.CreatedAt}, f)
}
