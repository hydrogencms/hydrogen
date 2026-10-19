package tests

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/exportfmt"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// countingExporter counts exports, which is how the tests tell the fast path (no export) from the slow one.
type countingExporter struct{ n *atomic.Int32 }

func (c countingExporter) Export(ctx context.Context, _ store.Tx, _ *exportfmt.ModuleWriter) error {
	c.n.Add(1)
	return nil
}

func (countingExporter) Import(context.Context, store.Tx, *exportfmt.ModuleReader) error { return nil }

type countingModule struct{ n *atomic.Int32 }

func (countingModule) Name() string { return "exportcount" }
func (m countingModule) Register(r *kernel.Registry) error {
	return r.Exporter("exportcount", countingExporter(m))
}

// gitProject is a project whose content lives in a directory (content.files) over a SQLite working copy.
type gitProject struct {
	t       *testing.T
	root    string
	db      string
	url     string
	content string
	addr    string
	clk     *clock.Manual
	exports atomic.Int32
	logs    lockedBuf
	listed  string
}

func newGitProject(t *testing.T, databaseURL string) *gitProject {
	t.Helper()
	root := t.TempDir()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	p := &gitProject{t: t, root: root, db: filepath.Join(root, ".work", "hydrogen.db"), content: filepath.Join(root, "content"),
		addr: addr, clk: clock.NewManual(time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC))}
	if databaseURL == "" {
		databaseURL = "sqlite:" + p.db
	}
	p.url = databaseURL
	p.write("schema/note.yaml", "types:\n  note:\n    name: \"{title}\"\n    fields:\n      title: { type: string, required: true }\n")
	p.write("config/app.yaml", fmt.Sprintf("http: { addr: %q }\ndatabase: { url: \"${env:DATABASE_URL}\" }\ncontent: { schema_dir: %q, files: %q }\n",
		addr, filepath.Join(root, "schema"), p.content))
	return p
}

func (p *gitProject) write(rel, data string) {
	p.t.Helper()
	full := filepath.Join(p.root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *gitProject) configure(b *hydrogen.Builder) {
	b.Use(sqlite.Module(), postgres.Module(), countingModule{&p.exports})
	b.Command(kernel.Command{Name: "test:add", Run: func(ctx context.Context, app *kernel.App, args []string) error {
		_, err := app.Services().Content.Create(ctx, content.CreateInput{Type: "note", Fields: map[string]any{"title": args[0]}})
		return err
	}})
	b.Command(kernel.Command{Name: "test:titles", Run: func(ctx context.Context, app *kernel.App, args []string) error {
		res, err := app.Services().Content.List(ctx, content.ListOptions{Type: "note", Limit: 100})
		if err != nil {
			return err
		}
		var names []string
		for _, it := range res.Items {
			names = append(names, it.Name)
		}
		slices.Sort(names)
		p.listed = strings.Join(names, ",")
		return nil
	}})
	b.Command(kernel.Command{Name: "test:signin", Run: func(ctx context.Context, app *kernel.App, args []string) error {
		return app.Services().DB.Tx(ctx, func(tx store.Tx) error {
			_, err := tx.Audit().Write(ctx, store.AuditEvent{At: app.Services().Clock.Now(), ActorKind: "user", ActorID: "u1", Action: "auth.login"})
			return err
		})
	}})
}

func (p *gitProject) options(stdout *bytes.Buffer) hydrogen.RunOptions {
	p.clk.Advance(time.Second)
	return hydrogen.RunOptions{Config: &config.Options{Dir: filepath.Join(p.root, "config"), Env: config.DevEnv, LookupEnv: func(k string) (string, bool) { return p.url, k == "DATABASE_URL" }},
		Stdout: stdout, Stderr: &p.logs, Clock: p.clk, PasswordIterations: 1000}
}

func (p *gitProject) run(args ...string) (string, error) {
	var out bytes.Buffer
	err := hydrogen.Run(ctx, args, p.configure, p.options(&out))
	return out.String(), err
}

func (p *gitProject) must(args ...string) string {
	p.t.Helper()
	out, err := p.run(args...)
	if err != nil {
		p.t.Fatalf("%v: %v\n%s", args, err, p.logs.String())
	}
	return out
}

func (p *gitProject) titles() string { p.t.Helper(); p.must("test:titles"); return p.listed }

// serve starts the server and returns once it listens, with a function that stops it.
func (p *gitProject) serve() (stop func(), err error) {
	c, cancel := context.WithCancel(ctx)
	listening := make(chan string, 1)
	done := make(chan error, 1)
	opts := p.options(&bytes.Buffer{})
	opts.Listening = func(a string) { listening <- a }
	go func() { done <- hydrogen.Run(c, []string{"serve"}, p.configure, opts) }()
	select {
	case <-listening:
	case err := <-done:
		cancel()
		return nil, err
	}
	return func() {
		cancel()
		if err := <-done; err != nil {
			p.t.Errorf("serve stopped with %v", err)
		}
	}, nil
}

func (p *gitProject) start() {
	p.t.Helper()
	stop, err := p.serve()
	if err != nil {
		p.t.Fatalf("serve: %v\n%s", err, p.logs.String())
	}
	stop()
}

func (p *gitProject) backups() []string {
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(p.db), "backups"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// checkout replaces the content directory with a copy of snapshot (a git pull or checkout).
func (p *gitProject) checkout(snapshot string) {
	p.t.Helper()
	if err := os.RemoveAll(p.content); err != nil {
		p.t.Fatal(err)
	}
	copyDir(p.t, snapshot, p.content)
}

func (p *gitProject) snapshot() string {
	p.t.Helper()
	dst := filepath.Join(p.t.TempDir(), "snap")
	copyDir(p.t, p.content, dst)
	return dst
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		dest := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (p *gitProject) exportCount() int32 { return p.exports.Load() }

func TestContentFilesFirstRun(t *testing.T) {
	p := newGitProject(t, "")
	p.start() // no database, no directory: nothing to load
	if got := p.titles(); got != "" {
		t.Fatalf("titles = %q", got)
	}
	p.must("test:add", "A")
	if _, err := os.Stat(p.content); !os.IsNotExist(err) {
		t.Fatalf("content directory exists before content:save: %v", err)
	}
	p.must("content:save")
	if _, err := os.Stat(filepath.Join(p.content, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	// A new checkout: no database yet, only the committed directory.
	for _, f := range []string{p.db, p.db + "-wal", p.db + "-shm", p.db + ".content-sync"} {
		_ = os.Remove(f)
	}
	p.start()
	if got := p.titles(); got != "A" {
		t.Fatalf("titles after first run from the directory = %q\n%s", got, p.logs.String())
	}
	if !strings.Contains(p.logs.String(), "content loaded") {
		t.Fatalf("no load message:\n%s", p.logs.String())
	}
	if len(p.backups()) != 0 {
		t.Fatalf("a first run must not back anything up: %v", p.backups())
	}
}

func TestContentFilesFastPathAndEdits(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	before := p.exportCount()

	p.start()
	p.start()
	if n := p.exportCount() - before; n != 0 {
		t.Fatalf("unchanged restarts exported %d times, want 0", n)
	}

	// A sign-in is not a content change.
	p.must("test:signin")
	before = p.exportCount()
	p.start()
	p.start()
	if n := p.exportCount() - before; n != 0 {
		t.Fatalf("restarts after a sign-in exported %d times, want 0", n)
	}

	// An edit survives a restart, is reported, and costs one export; restarting again is cheap again.
	p.must("test:add", "B")
	before = p.exportCount()
	p.logs = lockedBuf{}
	p.start()
	if n := p.exportCount() - before; n != 1 {
		t.Fatalf("restart after an edit exported %d times, want 1", n)
	}
	if !strings.Contains(p.logs.String(), "content:save") {
		t.Fatalf("no reminder about unsaved edits:\n%s", p.logs.String())
	}
	if got := p.titles(); got != "A,B" {
		t.Fatalf("titles = %q", got)
	}
	before = p.exportCount()
	p.start()
	if n := p.exportCount() - before; n != 0 {
		t.Fatalf("second restart with the same unsaved edit exported %d times, want 0", n)
	}

	// Saving records the new state; restarts stay on the fast path.
	p.must("content:save")
	before = p.exportCount()
	p.start()
	if n := p.exportCount() - before; n != 0 {
		t.Fatalf("restart after save exported %d times, want 0", n)
	}
	if len(p.backups()) != 0 {
		t.Fatalf("unexpected backups %v", p.backups())
	}
}

func TestContentFilesSaveClearsUnsavedNotice(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	p.must("test:add", "B")
	p.start()
	// Saving the same state again is a no-op for the files, and the marker follows.
	p.must("content:save")
	p.logs = lockedBuf{}
	p.start()
	if strings.Contains(p.logs.String(), "content:save") {
		t.Fatalf("still reports unsaved edits:\n%s", p.logs.String())
	}
}

func TestContentFilesDirectoryChangedLoadsIt(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	snapA := p.snapshot()
	p.must("test:add", "B")
	p.must("content:save")
	if got := p.titles(); got != "A,B" {
		t.Fatalf("titles = %q", got)
	}

	p.checkout(snapA) // git checkout of an older commit, or a pull
	p.start()
	if got := p.titles(); got != "A" {
		t.Fatalf("titles after the directory changed = %q\n%s", got, p.logs.String())
	}
	backups := p.backups()
	if len(backups) != 1 {
		t.Fatalf("backups = %v", backups)
	}
	if backups[0] != "20261019-090000" && !strings.HasPrefix(backups[0], "20261019-0900") {
		t.Fatalf("backup name %q does not come from the clock", backups[0])
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(p.db), "backups", backups[0], "hydrogen.db")); err != nil {
		t.Fatalf("the old database is not in the backup: %v", err)
	}
	if !strings.Contains(p.logs.String(), "loading it") {
		t.Fatalf("no load message:\n%s", p.logs.String())
	}
	// The imported database exports to the same fingerprint as the directory.
	assertDBMatchesDir(t, p)
	// And the next start is on the fast path.
	before := p.exportCount()
	p.start()
	if n := p.exportCount() - before; n != 0 {
		t.Fatalf("restart after a load exported %d times, want 0", n)
	}
}

func assertDBMatchesDir(t *testing.T, p *gitProject) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "check")
	p.must("export", out)
	a, err := exportfmt.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	b, err := exportfmt.Open(p.content)
	if err != nil {
		t.Fatal(err)
	}
	if a.Manifest().Fingerprint() != b.Manifest().Fingerprint() {
		t.Fatalf("an import followed by an export does not reproduce the directory's fingerprint")
	}
}

func TestContentFilesBothChangedRefusesAndLoadForce(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	snapA := p.snapshot()
	p.must("test:add", "B")
	p.must("content:save")

	p.checkout(snapA)
	p.must("test:add", "C") // database edited too
	_, err := p.serve()
	if err == nil {
		t.Fatal("serve started although both changed")
	}
	for _, want := range []string{"content:save", "content:load -force"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name %q", err, want)
		}
	}
	if got := p.titles(); got != "A,B,C" {
		t.Fatalf("the refused start changed the database: %q", got)
	}

	if _, err := p.run("content:load"); err == nil || !strings.Contains(err.Error(), "not in") {
		t.Fatalf("content:load without -force over unsaved changes: %v", err)
	}
	p.must("content:load", "-force")
	if got := p.titles(); got != "A" {
		t.Fatalf("titles after load -force = %q", got)
	}
	if len(p.backups()) != 1 {
		t.Fatalf("backups = %v", p.backups())
	}
	p.start() // consistent again
}

func TestContentFilesNoMarkerAndDifferentConflicts(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	_ = os.Remove(p.db + ".content-sync")
	p.start() // same content: the marker is recreated
	p.must("test:add", "B")
	_ = os.Remove(p.db + ".content-sync")
	if _, err := p.serve(); err == nil || !strings.Contains(err.Error(), "content:save") {
		t.Fatalf("differing content without a marker started: %v", err)
	}
	// content:load without -force refuses too, then -force resolves it.
	if _, err := p.run("content:load"); err == nil {
		t.Fatal("content:load replaced a database that differs, without -force")
	}
	p.must("content:load", "-force")
	if got := p.titles(); got != "A" {
		t.Fatalf("titles = %q", got)
	}
}

func TestContentSaveLeavesUnchangedFilesAlone(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	manifest := filepath.Join(p.content, "manifest.json")
	first, _ := os.ReadFile(manifest)
	st, _ := os.Stat(manifest)

	p.clk.Advance(time.Hour)
	out := p.must("content:save")
	if !strings.Contains(out, "up to date") {
		t.Fatalf("output = %q", out)
	}
	second, _ := os.ReadFile(manifest)
	st2, _ := os.Stat(manifest)
	if !bytes.Equal(first, second) || !st.ModTime().Equal(st2.ModTime()) {
		t.Fatal("content:save rewrote an unchanged directory")
	}
	entries, _ := os.ReadDir(p.root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".content-save") {
			t.Fatalf("temporary directory left behind: %s", e.Name())
		}
	}
	p.must("test:add", "B")
	if out := p.must("content:save"); !strings.Contains(out, "saved") {
		t.Fatalf("output = %q", out)
	}
	if third, _ := os.ReadFile(manifest); bytes.Equal(first, third) {
		t.Fatal("content:save did not write a change")
	}
}

func TestContentLoadRefusesWhileServerRuns(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	stop, err := p.serve()
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	if _, err := p.run("content:load", "-force"); err == nil || !strings.Contains(err.Error(), "stop it") {
		t.Fatalf("content:load with a server running: %v", err)
	}
}

func TestContentFilesNeedsSQLite(t *testing.T) {
	p := newGitProject(t, "postgres://u:p@localhost:1/db")
	_, err := p.run("config:check")
	if err == nil || !strings.Contains(err.Error(), "content.files needs a SQLite database") {
		t.Fatalf("config:check = %v", err)
	}
	if _, err := p.run("content:save"); err == nil {
		t.Fatal("content:save accepted a Postgres database")
	}
}

func TestContentFilesOffByDefault(t *testing.T) {
	p := newGitProject(t, "")
	p.write("config/app.yaml", fmt.Sprintf("http: { addr: %q }\ndatabase: { url: \"${env:DATABASE_URL}\" }\ncontent: { schema_dir: %q }\n", p.addr, filepath.Join(p.root, "schema")))
	p.start()
	if _, err := p.run("content:save"); err == nil || !strings.Contains(err.Error(), "content.files") {
		t.Fatalf("content:save without content.files: %v", err)
	}
	if p.exportCount() != 0 {
		t.Fatal("serve exported without content.files")
	}
}
