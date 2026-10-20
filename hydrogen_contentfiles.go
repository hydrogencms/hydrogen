package hydrogen

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/exportfmt"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/internal/contentfiles"
)

// Content in git (content.files): the directory is the source of truth, the SQLite database a working copy.
// Both change independently, so a marker next to the database records the content they last agreed on.

type verdict int

const (
	vNothing   verdict = iota // no directory and an empty database
	vImport                   // empty database: import the directory in place
	vInSync                   // same content: record it
	vUnchanged                // neither changed since the marker
	vReload                   // only the directory changed: replace the database
	vKeep                     // only the database changed: unsaved edits
	vConflict                 // both changed, or no marker and they differ
)

// contentFiles is the setup of one project.
type contentFiles struct {
	dir, db string
}

// contentFilesOf returns nil when the project does not keep its content in files.
func contentFilesOf(app *kernel.App) (*contentFiles, error) {
	s := app.Services()
	if s.Core.Content.Files == "" {
		return nil, nil
	}
	db, err := contentfiles.SQLitePath(s.Core.Database.URL)
	if err != nil {
		return nil, err
	}
	return &contentFiles{dir: filepath.Clean(s.Core.Content.Files), db: db}, nil
}

func requireContentFiles(app *kernel.App, command string) (*contentFiles, error) {
	cf, err := contentFilesOf(app)
	if err == nil && cf == nil {
		err = fmt.Errorf("%s needs content.files in the configuration", command)
	}
	return cf, err
}

// assessment is what the database and the directory hold relative to the marker.
type assessment struct {
	verdict verdict
	marker  *contentfiles.Marker
	dirFP   string
	dbFP    string // set when the database was exported
	newest  int64  // the newest audit event before the check
}

// assess compares the database with the directory. The database is exported only when it can differ from
// the marker: its audit log moved by something other than sign-ins and access changes.
func (c *contentFiles) assess(ctx context.Context, app *kernel.App) (assessment, error) {
	a := assessment{marker: contentfiles.ReadMarker(c.db)}
	var hasDir bool
	var err error
	if a.dirFP, hasDir, err = contentfiles.Fingerprint(c.dir); err != nil {
		return a, err
	}
	if a.newest, err = newestAudit(ctx, app); err != nil {
		return a, err
	}
	if a.marker == nil {
		m, err := c.exportManifest(ctx, app)
		if err != nil {
			return a, err
		}
		a.dbFP = m.Fingerprint()
		switch {
		case emptyExport(m):
			a.verdict = vNothing
			if hasDir {
				a.verdict = vImport
			}
		case hasDir && a.dbFP == a.dirFP:
			a.verdict = vInSync
		default:
			a.verdict = vConflict
		}
		return a, nil
	}
	dirChanged := !hasDir || a.dirFP != a.marker.Fingerprint
	// Once the database is known to differ, later starts compare against that point instead.
	ref, dbChanged := a.marker.Audit, a.marker.DirtyAudit > 0
	if dbChanged {
		ref = a.marker.DirtyAudit
	}
	moved, err := auditMoved(ctx, app, ref)
	if err != nil {
		return a, err
	}
	if moved {
		m, err := c.exportManifest(ctx, app)
		if err != nil {
			return a, err
		}
		a.dbFP = m.Fingerprint()
		dbChanged = a.dbFP != a.marker.Fingerprint
	}
	switch {
	case a.dbFP != "" && hasDir && a.dbFP == a.dirFP:
		a.verdict = vInSync
	case !dirChanged && !dbChanged:
		a.verdict = vUnchanged
	case dirChanged && !dbChanged:
		if !hasDir {
			return a, fmt.Errorf("content directory %s is missing; restore it from git, or run content:save to write the database's content there", c.dir)
		}
		a.verdict = vReload
	case !dirChanged:
		a.verdict = vKeep
	default:
		a.verdict = vConflict
	}
	return a, nil
}

func emptyExport(m *exportfmt.Manifest) bool {
	n := m.Counts.Items + m.Counts.Uploads
	for _, c := range m.Counts.Modules {
		n += c
	}
	return n == 0
}

// exportManifest exports the database into a scratch directory beside it and returns the manifest.
func (c *contentFiles) exportManifest(ctx context.Context, app *kernel.App) (*exportfmt.Manifest, error) {
	tmp, err := os.MkdirTemp(filepath.Dir(c.db), "content-export-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	return app.Export(ctx, tmp, kernel.ExportOptions{Version: Version()})
}

// contentAction reports whether an audit action can change exported content: sign-ins and access management
// never do.
func contentAction(action string) bool {
	kind, _, _ := strings.Cut(action, ".")
	switch kind {
	case "auth", "user", "token", "role", "group", "assignment":
		return false
	}
	return true
}

const auditPage = 100

func newestAudit(ctx context.Context, app *kernel.App) (int64, error) {
	var id int64
	err := app.Services().DB.View(ctx, func(tx store.Tx) error {
		evs, err := tx.Audit().List(ctx, store.AuditQuery{Limit: 1})
		if len(evs) > 0 {
			id = evs[0].ID
		}
		return err
	})
	return id, err
}

// auditMoved reports whether an audit event newer than after could have changed content. It reads only the
// events since the marker, newest first, and stops at the first that might have.
func auditMoved(ctx context.Context, app *kernel.App, after int64) (bool, error) {
	var before int64
	first := true
	for {
		var evs []store.AuditEvent
		err := app.Services().DB.View(ctx, func(tx store.Tx) error {
			var err error
			evs, err = tx.Audit().List(ctx, store.AuditQuery{Limit: auditPage, Before: before})
			return err
		})
		if err != nil {
			return false, err
		}
		if first {
			first = false
			// The log is shorter than at the last sync (pruned or replaced): nothing can be said.
			if (len(evs) == 0 && after > 0) || (len(evs) > 0 && evs[0].ID < after) {
				return true, nil
			}
		}
		for _, e := range evs {
			if e.ID <= after {
				return false, nil
			}
			if contentAction(e.Action) {
				return true, nil
			}
		}
		if len(evs) < auditPage {
			return false, nil
		}
		before = evs[len(evs)-1].ID
	}
}

func (c *contentFiles) record(fp string, audit int64) error {
	return contentfiles.WriteMarker(c.db, contentfiles.Marker{Fingerprint: fp, Audit: audit})
}

// conflictError names the two ways out.
func (c *contentFiles) conflictError() error {
	return fmt.Errorf("the database and %[1]s both changed since they last agreed (or were never synced and differ).\n"+
		"  keep the database:  hydrogen content:save   (writes it to %[1]s; review with git diff)\n"+
		"  keep %[1]s:  hydrogen content:load -force   (the database is moved to %[2]s)",
		c.dir, filepath.Join(filepath.Dir(c.db), "backups"))
}

// ensureMigrated migrates a database that is new (in any environment) or, in dev, behind; it reports whether
// the database is ready to be read.
func ensureMigrated(ctx context.Context, app *kernel.App) (bool, error) {
	pending, err := app.Pending(ctx)
	if err != nil {
		return false, err
	}
	if len(pending) == 0 {
		return true, nil
	}
	fresh := pending[0].Source == "core" && pending[0].Version == 1
	if !fresh && app.Services().Env != config.DevEnv {
		return false, nil
	}
	return true, app.Migrate(ctx)
}

// reconcileContentFiles runs at the start of serve, before the server accepts requests. It may replace the app
// (the database is closed, backed up and rebuilt); the returned app is the one to serve.
// rebuilt reports that the database was (re)built from the directory by this start.
func reconcileContentFiles(ctx context.Context, b *Builder, o RunOptions, app *kernel.App) (_ *kernel.App, rebuilt bool, _ error) {
	cf, err := contentFilesOf(app)
	if cf == nil || err != nil {
		return app, false, err
	}
	log := app.Services().Log
	if ok, err := ensureMigrated(ctx, app); !ok || err != nil {
		return app, false, err
	}
	a, err := cf.assess(ctx, app)
	if err != nil {
		return app, false, err
	}
	switch a.verdict {
	case vNothing:
	case vImport:
		if err := cf.importInto(ctx, app); err != nil {
			return app, false, err
		}
		log.Info("content loaded from "+cf.dir, "dir", cf.dir)
		return app, true, nil
	case vInSync:
		return app, false, cf.record(a.dirFP, a.newest)
	case vUnchanged:
		if a.newest != a.marker.Audit {
			return app, false, cf.record(a.marker.Fingerprint, a.newest)
		}
	case vReload:
		log.Info(cf.dir + " changed; loading it into the database")
		fresh, err := cf.replace(ctx, b, o, app)
		return fresh, err == nil, err
	case vKeep:
		log.Warn("keeping your edits; they are not in "+cf.dir+" yet (run hydrogen content:save, then commit)", "dir", cf.dir)
		if a.newest != a.marker.DirtyAudit {
			m := *a.marker
			m.DirtyAudit = a.newest
			return app, false, contentfiles.WriteMarker(cf.db, m)
		}
	case vConflict:
		return app, false, cf.conflictError()
	}
	return app, false, nil
}

// importInto imports the directory into the (empty, migrated) database of app and records the marker. The
// directory and the schema come from the same checkout, so a schema that differs from the one the content was
// saved under is not an error here: serve reports the changes that need a content migration and applies the
// migrations that exist.
func (c *contentFiles) importInto(ctx context.Context, app *kernel.App) error {
	if _, err := app.Import(ctx, c.dir, kernel.ImportOptions{AllowSchemaChange: true}); err != nil {
		return fmt.Errorf("load %s: %w", c.dir, err)
	}
	fp, _, err := contentfiles.Fingerprint(c.dir)
	if err != nil {
		return err
	}
	audit, err := newestAudit(ctx, app)
	if err != nil {
		return err
	}
	return c.record(fp, audit)
}

// replace rebuilds the database from the directory: it checks the directory, moves the database to
// backups/<timestamp> and imports into a new one. When the import fails the previous database is moved back.
func (c *contentFiles) replace(ctx context.Context, b *Builder, o RunOptions, app *kernel.App) (*kernel.App, error) {
	if err := app.CheckImport(c.dir, kernel.ImportOptions{AllowSchemaChange: true}); err != nil {
		return app, fmt.Errorf("load %s: %w (the database is unchanged)", c.dir, err)
	}
	s := app.Services()
	clk, log := s.Clock, s.Log
	if err := app.Close(); err != nil {
		return nil, err
	}
	backup, err := contentfiles.Backup(c.db, clk.Now())
	if err != nil {
		return nil, fmt.Errorf("back up the database: %w", err)
	}
	fresh, err := c.rebuild(ctx, b, o)
	if err != nil {
		if backup == "" {
			return nil, err
		}
		if rerr := contentfiles.Restore(c.db, backup); rerr != nil {
			return nil, fmt.Errorf("%w; restoring the previous database from %s also failed: %v", err, backup, rerr)
		}
		return nil, fmt.Errorf("%w (the previous database was restored)", err)
	}
	if backup != "" {
		log.Info("the previous database is kept", "path", backup)
	}
	log.Info("content loaded from "+c.dir, "dir", c.dir)
	return fresh, nil
}

// rebuild imports the directory into a new database.
func (c *contentFiles) rebuild(ctx context.Context, b *Builder, o RunOptions) (*kernel.App, error) {
	fresh, err := newApp(ctx, b, o)
	if err != nil {
		return nil, err
	}
	if err := fresh.Migrate(ctx); err != nil {
		_ = fresh.Close()
		return nil, err
	}
	if err := c.importInto(ctx, fresh); err != nil {
		_ = fresh.Close()
		return nil, err
	}
	return fresh, nil
}

// contentSave writes the database's content to content.files and records the sync marker. Identical content
// leaves the files alone.
func contentSave(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	if len(args) > 0 {
		return errors.New("content:save takes no arguments")
	}
	cf, err := requireContentFiles(app, "content:save")
	if err != nil {
		return err
	}
	if err := requireMigrated(ctx, app); err != nil {
		return err
	}
	newest, err := newestAudit(ctx, app)
	if err != nil {
		return err
	}
	parent := filepath.Dir(cf.dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "."+filepath.Base(cf.dir)+"-save-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	m, err := app.Export(ctx, tmp, kernel.ExportOptions{Version: Version(), Layout: exportfmt.LayoutItems})
	if err != nil {
		return err
	}
	fp := m.Fingerprint()
	if cur, found, err := contentfiles.Fingerprint(cf.dir); err == nil && found && cur == fp && !oldLayout(cf.dir) {
		if err := cf.record(fp, newest); err != nil {
			return err
		}
		_, err = fmt.Fprintf(o.Stdout, "%s is up to date\n", cf.dir)
		return err
	}
	if err := contentfiles.Swap(cf.dir, tmp); err != nil {
		return err
	}
	if err := cf.record(fp, newest); err != nil {
		return err
	}
	_, err = fmt.Fprintf(o.Stdout, "saved %d items (%d versions, %d drafts) to %s; commit it to publish\n", m.Counts.Items, m.Counts.Versions, m.Counts.Drafts, cf.dir)
	return err
}

// oldLayout reports whether dir keeps its items in items.ndjson (written before the per-item layout), which
// content:save converts.
func oldLayout(dir string) bool {
	r, err := exportfmt.Open(dir)
	return err == nil && r.Has("items.ndjson")
}

// contentLoad replaces the database with the directory's content. It may replace the app.
func contentLoad(ctx context.Context, b *Builder, o RunOptions, app *kernel.App, args []string) (*kernel.App, error) {
	fs := flag.NewFlagSet("content:load", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	force := fs.Bool("force", false, "replace the database even when it holds changes that are not saved to the directory")
	if err := fs.Parse(args); err != nil {
		return app, err
	}
	if fs.NArg() > 0 {
		return app, errors.New("content:load takes no arguments")
	}
	cf, err := requireContentFiles(app, "content:load")
	if err != nil {
		return app, err
	}
	if running(ctx, app.Services().Core.HTTP.Addr) {
		return app, fmt.Errorf("a server answers on %s; stop it before loading content", app.Services().Core.HTTP.Addr)
	}
	_, hasDir, err := contentfiles.Fingerprint(cf.dir)
	if err != nil {
		return app, err
	}
	if !hasDir {
		return app, fmt.Errorf("%s holds no content (no manifest.json)", cf.dir)
	}
	ready, err := ensureMigrated(ctx, app)
	if err != nil {
		return app, err
	}
	if !*force {
		if !ready {
			return app, errors.New("pending database migrations; run migrate first (or pass -force)")
		}
		a, err := cf.assess(ctx, app)
		if err != nil {
			return app, err
		}
		switch a.verdict {
		case vKeep, vConflict:
			return app, fmt.Errorf("the database has changes that are not in %s; run content:save to keep them, or content:load -force to discard them (the database is kept in %s)",
				cf.dir, filepath.Join(filepath.Dir(cf.db), "backups"))
		case vInSync, vUnchanged:
			_, err := fmt.Fprintf(o.Stdout, "the database already holds the content of %s\n", cf.dir)
			return app, err
		case vImport:
			if err := cf.importInto(ctx, app); err != nil {
				return app, err
			}
			if err := readyContent(ctx, app, true); err != nil {
				return app, err
			}
			_, err := fmt.Fprintf(o.Stdout, "loaded %s\n", cf.dir)
			return app, err
		}
	}
	app, err = cf.replace(ctx, b, o, app)
	if err != nil {
		return app, err
	}
	if err := readyContent(ctx, app, true); err != nil {
		return app, err
	}
	_, err = fmt.Fprintf(o.Stdout, "loaded %s\n", cf.dir)
	return app, err
}

// running reports whether a Hydrogen server answers its health check on addr (a listen address).
func running(ctx context.Context, addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "0" {
		return false
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
