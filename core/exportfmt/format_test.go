package exportfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type rec struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

func writeSample(t *testing.T, dir string, n int) {
	t.Helper()
	w, err := NewWriter(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteFile("schema/compiled.json", []byte(`{"types":[]}`)); err != nil {
		t.Fatal(err)
	}
	f, err := w.Create("items.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := f.WriteRecord(rec{i, "line <&>\n  " + strings.Repeat("x", i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Finish(Manifest{Hydrogen: "test", Dialect: "sqlite", SchemaHash: "abc"}); err != nil {
		t.Fatal(err)
	}
}

func TestRoundTripAndChecksums(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	writeSample(t, dir, 50)
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Verify(); err != nil {
		t.Fatal(err)
	}
	m := r.Manifest()
	if m.Format != FormatVersion || m.SchemaHash != "abc" || len(m.Files) != 2 || m.Files[0].Name != "items.ndjson" || m.Files[0].Records != 50 {
		t.Fatalf("manifest = %+v", m)
	}
	rr, err := r.Records("items.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rr.Close() }()
	n := 0
	for {
		var x rec
		ok, err := rr.Next(&x)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		if x.N != n || !strings.HasPrefix(x.Text, "line <&>\n  ") {
			t.Fatalf("record %d = %+v", n, x)
		}
		n++
	}
	if n != 50 {
		t.Fatalf("read %d records", n)
	}
	if _, err := r.ReadFile("nope"); err == nil {
		t.Error("ReadFile of an unlisted file succeeded")
	}
}

func TestTamperDetection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"flipped byte", func(t *testing.T, dir string) {
			p := filepath.Join(dir, "items.ndjson")
			b, _ := os.ReadFile(p)
			b[10] ^= 1
			must(t, os.WriteFile(p, b, 0o644))
		}, "checksum mismatch"},
		{"appended line", func(t *testing.T, dir string) {
			f, err := os.OpenFile(filepath.Join(dir, "items.ndjson"), os.O_APPEND|os.O_WRONLY, 0)
			must(t, err)
			_, _ = f.WriteString("{}\n")
			must(t, f.Close())
		}, "bytes"},
		{"missing file", func(t *testing.T, dir string) {
			must(t, os.Remove(filepath.Join(dir, "schema", "compiled.json")))
		}, "compiled.json"},
		{"truncated", func(t *testing.T, dir string) {
			must(t, os.Truncate(filepath.Join(dir, "items.ndjson"), 5))
		}, "bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "out")
			writeSample(t, dir, 10)
			tc.mutate(t, dir)
			r, err := Open(dir)
			must(t, err)
			err = r.Verify()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Verify = %v, want %q", err, tc.want)
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnsupportedFormatAndNames(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	writeSample(t, dir, 1)
	b, _ := os.ReadFile(filepath.Join(dir, "manifest.json"))
	must(t, os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(strings.Replace(string(b), `"format": 2`, `"format": 3`, 1)), 0o644))
	if _, err := Open(dir); err == nil || !strings.Contains(err.Error(), "format 3") {
		t.Fatalf("Open = %v", err)
	}

	w, err := NewWriter(filepath.Join(t.TempDir(), "x"))
	must(t, err)
	for _, bad := range []string{"../evil", "/abs", "a//b", "manifest.json", "a/../b", "", "sp ace"} {
		if _, err := w.Create(bad); err == nil {
			t.Errorf("Create(%q) succeeded", bad)
		}
	}
	f, err := w.Create("a.ndjson")
	must(t, err)
	if _, err := w.Create("b.ndjson"); err == nil {
		t.Error("second open file accepted")
	}
	if _, err := w.Finish(Manifest{}); err == nil {
		t.Error("Finish with an open file succeeded")
	}
	must(t, f.Close())
	if _, err := w.Create("a.ndjson"); err == nil {
		t.Error("duplicate file accepted")
	}
}

func TestNewWriterRefusesNonEmptyDir(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "x"), nil, 0o644))
	if _, err := NewWriter(dir); err == nil {
		t.Fatal("NewWriter accepted a non-empty directory")
	}
}

func TestManifestPathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "manifest.json"),
		[]byte(`{"format":1,"files":[{"name":"../../etc/passwd","sha256":"","bytes":0}]}`), 0o644))
	if _, err := Open(dir); err == nil {
		t.Fatal("Open accepted a path outside the export")
	}
}

// A large export streams: the reader never holds more than a line, so a megabyte-long line is fine and many
// lines cost nothing.
func TestStreamsLargeFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	writeSample(t, dir, 3000)
	r, err := Open(dir)
	must(t, err)
	must(t, r.Verify())
	rr, _ := r.Records("items.ndjson")
	defer func() { _ = rr.Close() }()
	if rr.Total() != 3000 {
		t.Fatalf("total %d", rr.Total())
	}
}

func TestFingerprintIgnoresWhenAndWhereAnExportWasMade(t *testing.T) {
	m := Manifest{Format: 2, Hydrogen: "0.1.0", Dialect: "sqlite", SchemaHash: "s", CreatedAt: time.Now(),
		Counts: Counts{Items: 1, Modules: map[string]int{"a": 1, "b": 2}}, Files: []File{{Name: "items.ndjson", SHA256: "x", Bytes: 3, Records: 1}}}
	other := m
	other.Hydrogen, other.Dialect, other.CreatedAt = "0.2.0", "postgres", m.CreatedAt.Add(time.Hour)
	if m.Fingerprint() != other.Fingerprint() {
		t.Fatal("fingerprint depends on the export time, version or dialect")
	}
	other.Files = []File{{Name: "items.ndjson", SHA256: "y", Bytes: 3, Records: 1}}
	if m.Fingerprint() == other.Fingerprint() {
		t.Fatal("fingerprint ignores file checksums")
	}
}

func TestFingerprintIgnoresTheItemLayout(t *testing.T) {
	old := Manifest{Format: 2, SchemaHash: "s", Counts: Counts{Items: 2}, Files: []File{
		{Name: "content_migrations.ndjson", SHA256: "m", Records: 0},
		{Name: "items.ndjson", SHA256: "x", Bytes: 30, Records: 2},
		{Name: "schema/revisions/a.json", SHA256: "r", Bytes: 9}}}
	split := old
	split.ItemStream = &Stream{SHA256: "x", Bytes: 30}
	split.Files = []File{old.Files[0], {Name: "items/note/a.json", SHA256: "1", Bytes: 20}, {Name: "items/note/b.json", SHA256: "2", Bytes: 20}, old.Files[2]}
	if old.Fingerprint() != split.Fingerprint() {
		t.Fatal("the item layout changes the fingerprint")
	}
	split.ItemStream.SHA256 = "y"
	if old.Fingerprint() == split.Fingerprint() {
		t.Fatal("the fingerprint ignores the item records")
	}
}
