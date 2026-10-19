// Package contentfiles holds the file-level parts of "content in git" (content.files): the directory
// fingerprint, the sync marker next to the SQLite file, database backups and the atomic directory swap.
package contentfiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/exportfmt"
)

// Marker records the content the database and the directory last agreed on, and the newest audit event seen
// then, which tells a later start whether the database can have changed since.
type Marker struct {
	Fingerprint string `json:"fingerprint"`
	Audit       int64  `json:"audit"`
	// DirtyAudit, when set, is the newest audit event seen when the database was found to differ from
	// Fingerprint, so a restart need not export it again until something else changes.
	DirtyAudit int64 `json:"dirtyAudit,omitempty"`
}

// SQLitePath returns the database file of a sqlite: URL, relative to the working directory like the adapter.
func SQLitePath(url string) (string, error) {
	path, ok := strings.CutPrefix(url, "sqlite:")
	if !ok {
		return "", fmt.Errorf("content.files needs a SQLite database (database.url sqlite:…)")
	}
	return strings.TrimPrefix(path, "//"), nil
}

// MarkerPath is where the marker of a database lives.
func MarkerPath(db string) string { return db + ".content-sync" }

// ReadMarker returns nil when there is no marker (or it is unreadable: the next start rebuilds it).
func ReadMarker(db string) *Marker {
	data, err := os.ReadFile(MarkerPath(db))
	if err != nil {
		return nil
	}
	var m Marker
	if json.Unmarshal(data, &m) != nil || m.Fingerprint == "" {
		return nil
	}
	return &m
}

// WriteMarker replaces the marker atomically.
func WriteMarker(db string, m Marker) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	path := MarkerPath(db)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Fingerprint identifies the export in dir without reading its files: the manifest says what they hold, and
// their sizes must match it (import verifies the checksums). found is false when dir holds no export.
func Fingerprint(dir string) (fp string, found bool, err error) {
	r, err := exportfmt.Open(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("content directory %s: %w", dir, err)
	}
	if err := r.CheckSizes(); err != nil {
		return "", false, fmt.Errorf("content directory %s: %w", dir, err)
	}
	return r.Manifest().Fingerprint(), true, nil
}

// Backup moves the SQLite files of db (database, -wal, -shm) into backups/<timestamp> next to it and returns
// that directory; "" when there is no database. The database must be closed. Nothing is ever deleted.
func Backup(db string, now time.Time) (string, error) {
	var present []string
	for _, p := range []string{db, db + "-wal", db + "-shm"} {
		if _, err := os.Stat(p); err == nil {
			present = append(present, p)
		}
	}
	if len(present) == 0 {
		return "", nil
	}
	base := filepath.Join(filepath.Dir(db), "backups", now.UTC().Format("20060102-150405"))
	if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
		return "", err
	}
	to := base
	for n := 2; ; n++ {
		err := os.Mkdir(to, 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		to = fmt.Sprintf("%s-%d", base, n)
	}
	for _, p := range present {
		if err := os.Rename(p, filepath.Join(to, filepath.Base(p))); err != nil {
			return "", err
		}
	}
	return to, nil
}

// Swap replaces dir with fresh (a sibling directory) by two renames, so an interrupted save leaves the old or the
// new content, never a mix of both. The old directory is removed once the new one is in place.
func Swap(dir, fresh string) error {
	old := fresh + ".old"
	_ = os.RemoveAll(old)
	hadOld := true
	if err := os.Rename(dir, old); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		hadOld = false
	}
	if err := os.Rename(fresh, dir); err != nil {
		if hadOld {
			_ = os.Rename(old, dir)
		}
		return err
	}
	if hadOld {
		return os.RemoveAll(old)
	}
	return nil
}
