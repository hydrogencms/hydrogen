package tests

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/exportfmt"
	"github.com/hydrogencms/hydrogen/internal/contentfiles"
)

const noteWithBody = "types:\n  note:\n    name: \"{title}\"\n    fields:\n      title: { type: string, required: true }\n      body: { type: string }\n"

// production switches the project to a production start, as a CI job runs it.
func (p *gitProject) production() {
	p.t.Helper()
	cfg, err := os.ReadFile(filepath.Join(p.root, "config", "app.yaml"))
	if err != nil {
		p.t.Fatal(err)
	}
	p.write("config/app.yaml", string(cfg)+"manage: { token: \"${env:MANAGE_TOKEN}\" }\ndelivery: { preview_secret: \"${env:PREVIEW_SECRET}\" }\n")
	p.env = "production"
}

// freshCheckout forgets the working copy, as a new checkout has none.
func (p *gitProject) freshCheckout() {
	for _, f := range []string{p.db, p.db + "-wal", p.db + "-shm", p.db + ".content-sync"} {
		_ = os.Remove(f)
	}
}

func (p *gitProject) contentBytes() map[string]string {
	p.t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(p.content, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		rel, _ := filepath.Rel(p.content, path)
		out[rel] = string(data)
		return err
	})
	if err != nil {
		p.t.Fatal(err)
	}
	return out
}

func sameFiles(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("the content directory changed: %d files, now %d", len(before), len(after))
	}
	for name, data := range before {
		if after[name] != data {
			t.Fatalf("%s changed", name)
		}
	}
}

func TestContentFilesAdditiveSchemaChangeStartsFresh(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("test:publish")
	p.must("content:save")
	before := p.contentBytes()

	p.write("schema/note.yaml", noteWithBody) // committed without content:save
	p.freshCheckout()
	p.production()
	p.start()
	if got := p.titles(); got != "A" {
		t.Fatalf("titles = %q\n%s", got, p.logs.String())
	}
	p.must("test:add-body", "B", "new field") // the new schema is live
	sameFiles(t, before, p.contentBytes())
	if len(p.backups()) != 0 {
		t.Fatalf("backups %v", p.backups())
	}

	// content:load takes the same path.
	p.must("content:load", "-force")
	if got := p.titles(); got != "A" {
		t.Fatalf("titles after content:load = %q", got)
	}
}

func TestContentFilesBreakingSchemaChangeNeedsAMigration(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("test:publish")
	p.must("content:save")
	before := p.contentBytes()

	p.write("schema/note.yaml", "types:\n  note:\n    name: \"{heading}\"\n    fields:\n      heading: { type: string, required: true }\n")
	p.freshCheckout()
	p.production()
	_, err := p.serve()
	if err == nil {
		t.Fatal("serve accepted a breaking schema change without a migration")
	}
	for _, want := range []string{"schema changes need a content migration", "note.title: removed", "content:save"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "allow-schema-change") {
		t.Errorf("error names a flag of another command: %v", err)
	}
	if _, err := p.run("content:load", "-force"); err == nil || strings.Contains(err.Error(), "allow-schema-change") {
		t.Errorf("content:load: %v", err)
	}
	sameFiles(t, before, p.contentBytes())

	// With a migration the start applies it, in production too, and leaves the files alone.
	p.migrations = []content.Migration{{Name: "0001_heading", Type: "note", Changes: []string{"note.title"},
		Up: func(d *content.MigrationDoc) error {
			d.Fields["heading"] = d.Fields["title"]
			delete(d.Fields, "title")
			return nil
		}}}
	p.freshCheckout()
	p.start()
	if got := p.titles(); got != "A" {
		t.Fatalf("titles = %q\n%s", got, p.logs.String())
	}
	sameFiles(t, before, p.contentBytes())
	p.start() // the migrated copy differs from the files: kept, with a reminder
	if got := p.titles(); got != "A" {
		t.Fatalf("titles after restart = %q", got)
	}
}

func TestContentFilesFailedLoadKeepsTheDatabase(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	old := p.snapshot()
	p.must("test:add", "B")
	p.must("content:save")

	// The directory went back (a pull); loading it fails after the cheap checks passed.
	p.checkout(old)
	p.failImport.Store(true)
	_, err := p.serve()
	if err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("serve: %v", err)
	}
	if got := p.titles(); got != "A,B" {
		t.Fatalf("titles after a failed reload = %q", got)
	}
	if len(p.backups()) != 0 {
		t.Fatalf("backups %v", p.backups())
	}
	if _, err := p.run("content:load", "-force"); err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("content:load: %v", err)
	}
	if got := p.titles(); got != "A,B" {
		t.Fatalf("titles after a failed load = %q", got)
	}

	p.failImport.Store(false)
	p.start()
	if got := p.titles(); got != "A" {
		t.Fatalf("titles after the retry = %q", got)
	}
	if len(p.backups()) != 1 {
		t.Fatalf("backups %v", p.backups())
	}
}

func TestContentFilesDamagedDirectoryIsRefusedBeforeTheDatabaseMoves(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("content:save")
	old := p.snapshot()
	p.must("test:add", "B")
	p.must("content:save")

	p.checkout(old)
	files, _ := filepath.Glob(filepath.Join(p.content, "items", "note", "*.json"))
	if len(files) != 1 {
		t.Fatalf("item files = %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if err := os.WriteFile(files[0], bytes.Replace(data, []byte(`"A"`), []byte(`"Z"`), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := p.serve()
	if err == nil || !strings.Contains(err.Error(), "damaged") || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("serve: %v", err)
	}
	if got := p.titles(); got != "A,B" {
		t.Fatalf("titles = %q", got)
	}
	if len(p.backups()) != 0 {
		t.Fatalf("backups %v", p.backups())
	}
}

func TestContentFilesItemFilesDiffAndTrack(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("test:add", "B")
	p.must("content:save")
	if _, err := os.Stat(filepath.Join(p.content, "items.ndjson")); err == nil {
		t.Fatal("content:save wrote items.ndjson")
	}
	files, _ := filepath.Glob(filepath.Join(p.content, "items", "note", "*.json"))
	if len(files) != 2 {
		t.Fatalf("item files = %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !bytes.Contains(data, []byte("\n    \"title\"")) && !bytes.Contains(data, []byte("\n        \"title\"")) {
		t.Fatalf("item file is not indented line by line:\n%s", data)
	}

	// A deleted item's file goes with it.
	p.must("test:rm", "B")
	p.must("content:save")
	if files, _ = filepath.Glob(filepath.Join(p.content, "items", "note", "*.json")); len(files) != 1 {
		t.Fatalf("item files after a delete = %v", files)
	}
	p.freshCheckout()
	p.start()
	if got := p.titles(); got != "A" {
		t.Fatalf("titles = %q", got)
	}

	// An empty database leaves no items directory and still loads.
	p.must("test:rm", "A")
	p.must("content:save")
	if _, err := os.Stat(filepath.Join(p.content, "items")); err == nil {
		t.Fatal("empty content keeps an items directory")
	}
	p.freshCheckout()
	p.start()
	if got := p.titles(); got != "" {
		t.Fatalf("titles = %q\n%s", got, p.logs.String())
	}
}

// A project saved before the per-item layout keeps its verdicts; content:save converts the directory once.
func TestContentFilesUpgradeFromItemsNDJSON(t *testing.T) {
	p := newGitProject(t, "")
	p.start()
	p.must("test:add", "A")
	p.must("export", p.content)
	// Manifests of earlier versions have no itemStream.
	manifest := filepath.Join(p.content, "manifest.json")
	data, _ := os.ReadFile(manifest)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "itemStream")
	data, _ = json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(manifest, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	old := p.contentBytes()

	p.start() // database and directory agree: the marker is recorded
	marker := contentfiles.ReadMarker(p.db)
	if marker == nil {
		t.Fatal("no marker")
	}
	before := p.exportCount()
	p.start()
	if n := p.exportCount() - before; n != 0 {
		t.Fatalf("restart exported %d times", n)
	}
	sameFiles(t, old, p.contentBytes())

	out := p.must("content:save")
	if !strings.Contains(out, "saved") {
		t.Fatalf("content:save did not convert the old layout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(p.content, "items.ndjson")); err == nil {
		t.Fatal("items.ndjson survived the conversion")
	}
	r, err := exportfmt.Open(p.content)
	if err != nil {
		t.Fatal(err)
	}
	if r.Manifest().Fingerprint() != marker.Fingerprint {
		t.Fatal("the layout changed the fingerprint")
	}
	converted := p.contentBytes()
	if out := p.must("content:save"); !strings.Contains(out, "up to date") {
		t.Fatalf("second content:save: %q", out)
	}
	sameFiles(t, converted, p.contentBytes())
	p.start()
	if len(p.backups()) != 0 {
		t.Fatalf("backups %v", p.backups())
	}

	// The converted directory (a pull of the conversion) and the old one both load into a fresh database.
	for _, dir := range []map[string]string{converted, old} {
		if err := os.RemoveAll(p.content); err != nil {
			t.Fatal(err)
		}
		for name, data := range dir {
			p.write(filepath.Join("content", name), data)
		}
		p.freshCheckout()
		p.start()
		if got := p.titles(); got != "A" {
			t.Fatalf("titles = %q\n%s", got, p.logs.String())
		}
	}
}
