package contentfiles

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBackupMovesEveryFileAndNeverReusesADirectory(t *testing.T) {
	db := filepath.Join(t.TempDir(), ".work", "h.db")
	now := time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC)
	if got, err := Backup(db, now); err != nil || got != "" {
		t.Fatalf("Backup without a database = %q, %v", got, err)
	}
	var dirs []string
	for range 2 {
		write(t, db, "db")
		write(t, db+"-wal", "wal")
		write(t, db+"-shm", "shm")
		dir, err := Backup(db, now)
		if err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
		for _, f := range []string{db, db + "-wal", db + "-shm"} {
			if _, err := os.Stat(f); !os.IsNotExist(err) {
				t.Fatalf("%s was not moved", f)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.Base(f))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if dirs[0] == dirs[1] || filepath.Base(dirs[0]) != "20261019-093000" {
		t.Fatalf("backup directories %v", dirs)
	}
}

func TestSwapReplacesTheDirectory(t *testing.T) {
	root := t.TempDir()
	dir, fresh := filepath.Join(root, "content"), filepath.Join(root, ".fresh")
	write(t, filepath.Join(dir, "old"), "1")
	write(t, filepath.Join(fresh, "new"), "2")
	if err := Swap(dir, fresh); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old")); !os.IsNotExist(err) {
		t.Fatal("old content survived")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 {
		t.Fatalf("leftovers: %v", entries)
	}
	// No previous directory.
	_ = os.RemoveAll(dir)
	write(t, filepath.Join(fresh, "x"), "3")
	if err := Swap(dir, fresh); err != nil {
		t.Fatal(err)
	}
}

func TestMarker(t *testing.T) {
	db := filepath.Join(t.TempDir(), "h.db")
	if ReadMarker(db) != nil {
		t.Fatal("marker from nowhere")
	}
	if err := WriteMarker(db, Marker{Fingerprint: "abc", Audit: 7, DirtyAudit: 9}); err != nil {
		t.Fatal(err)
	}
	if m := ReadMarker(db); m == nil || *m != (Marker{Fingerprint: "abc", Audit: 7, DirtyAudit: 9}) {
		t.Fatalf("marker = %+v", m)
	}
	write(t, MarkerPath(db), "{broken")
	if ReadMarker(db) != nil {
		t.Fatal("a damaged marker must count as missing")
	}
}

func TestSQLitePath(t *testing.T) {
	for url, want := range map[string]string{"sqlite:.work/h.db": ".work/h.db", "sqlite:///abs/h.db": "/abs/h.db"} {
		if got, err := SQLitePath(url); err != nil || got != want {
			t.Errorf("SQLitePath(%q) = %q, %v", url, got, err)
		}
	}
	if _, err := SQLitePath("postgres://x"); err == nil {
		t.Error("postgres accepted")
	}
}
