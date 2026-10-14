package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

// MigrationSet is one source's migrations for the open database's dialect: files named NNNN_name.sql,
// numbered from 0001 without gaps.
type MigrationSet struct {
	Source string
	FS     fs.FS
}

type Migration struct {
	Source   string
	Version  int
	Name     string
	SQL      string
	Checksum string
}

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// ParseMigrations reads and validates a set. Adapters call it from Migrate and Pending.
func ParseMigrations(set MigrationSet) ([]Migration, error) {
	entries, err := fs.ReadDir(set.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("migrations %s: %w", set.Source, err)
	}
	var out []Migration
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("migrations %s: %s does not match NNNN_name.sql", set.Source, e.Name())
		}
		b, err := fs.ReadFile(set.FS, e.Name())
		if err != nil {
			return nil, err
		}
		v, _ := strconv.Atoi(m[1])
		sum := sha256.Sum256(b)
		out = append(out, Migration{Source: set.Source, Version: v, Name: m[2], SQL: string(b),
			Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("migrations %s: expected version %04d, found %04d_%s", set.Source, i+1, m.Version, m.Name)
		}
	}
	return out, nil
}

// CoreSource names the migration set adapters apply for core tables.
const CoreSource = "core"

// Applied is a migration recorded in the database.
type Applied struct {
	Source   string
	Version  int
	Name     string
	Checksum string
}

// PlanMigrations compares what the database has applied with the sets (core first) and returns what is
// pending, in order. It fails when an applied migration's file changed or disappeared, which means the code
// and the database disagree.
func PlanMigrations(applied []Applied, sets []MigrationSet) ([]Migration, error) {
	have := map[string]map[int]Applied{}
	for _, a := range applied {
		if have[a.Source] == nil {
			have[a.Source] = map[int]Applied{}
		}
		have[a.Source][a.Version] = a
	}
	seen := map[string]bool{}
	var pending []Migration
	for _, set := range sets {
		if seen[set.Source] {
			return nil, fmt.Errorf("migrations: source %q given twice", set.Source)
		}
		seen[set.Source] = true
		ms, err := ParseMigrations(set)
		if err != nil {
			return nil, err
		}
		for _, m := range ms {
			a, ok := have[m.Source][m.Version]
			if !ok {
				pending = append(pending, m)
				continue
			}
			if a.Checksum != m.Checksum {
				return nil, fmt.Errorf("migration %s/%04d_%s changed after it was applied; add a new migration instead",
					m.Source, m.Version, m.Name)
			}
		}
		if n := len(have[set.Source]); n > len(ms) {
			return nil, fmt.Errorf("database has %d %s migrations but this build has %d; is the binary older than the database?",
				n, set.Source, len(ms))
		}
	}
	return pending, nil
}
