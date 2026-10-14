// Package sqlite is Hydrogen's SQLite adapter: a single file, nothing to install, one node.
//
// Database URLs look like sqlite:.work/hydrogen.db (relative to the working directory) or sqlite:/abs/path.db.
// Writes go through one connection (SQLite has a single writer); reads use a separate read-only pool, which
// WAL mode lets run alongside writes.
package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/store"

	_ "modernc.org/sqlite"
)

const Dialect = "sqlite"

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the adapter for sqlite: URLs.
func Module() kernel.Module { return module{} }

type module struct{}

func (module) Name() string { return "sqlite" }

func (module) Register(r *kernel.Registry) error { return r.Database("sqlite", Open) }

type DB struct {
	writer *sql.DB
	reader *sql.DB
	clock  clock.Clock
	hub    *hub
	locks  *locker
}

func Open(ctx context.Context, url string, o store.OpenOptions) (store.DB, error) {
	path, ok := strings.CutPrefix(url, "sqlite:")
	if !ok {
		return nil, fmt.Errorf("sqlite: URL %q must start with %q", url, "sqlite:")
	}
	path = strings.TrimPrefix(path, "//")
	if path == "" || strings.ContainsAny(path, "?#") {
		return nil, fmt.Errorf("sqlite: invalid database path %q", path)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("sqlite: %w", err)
		}
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	pragmas := "_pragma=busy_timeout(10000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	writer, err := sql.Open("sqlite", "file:"+path+"?"+pragmas+"&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(1)
	writer.SetConnMaxIdleTime(0)
	if err := writer.PingContext(ctx); err != nil {
		_ = writer.Close()
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}
	reader, err := sql.Open("sqlite", "file:"+path+"?"+pragmas+"&_pragma=query_only(1)")
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(4)
	return &DB{writer: writer, reader: reader, clock: o.Clock, hub: newHub(), locks: newLocker()}, nil
}

func (db *DB) Dialect() string                  { return Dialect }
func (db *DB) Capabilities() store.Capabilities { return store.Capabilities{MultiNode: false} }
func (db *DB) Notifier() store.Notifier         { return db.hub }
func (db *DB) Locker() store.Locker             { return db.locks }
func (db *DB) Ping(ctx context.Context) error   { return db.writer.PingContext(ctx) }
func (db *DB) Close() error                     { return errors.Join(db.reader.Close(), db.writer.Close()) }

func (db *DB) Tx(ctx context.Context, fn func(store.Tx) error) error {
	return db.run(ctx, db.writer, false, fn)
}

func (db *DB) View(ctx context.Context, fn func(store.Tx) error) error {
	return db.run(ctx, db.reader, true, fn)
}

func (db *DB) run(ctx context.Context, pool *sql.DB, readOnly bool, fn func(store.Tx) error) (err error) {
	sqlTx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin: %w", err)
	}
	t := &tx{tx: sqlTx, readOnly: readOnly}
	defer func() {
		if r := recover(); r != nil {
			_ = sqlTx.Rollback()
			panic(r)
		}
	}()
	if err := fn(t); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit: %w", err)
	}
	for _, n := range t.notes {
		db.hub.send(n)
	}
	return nil
}

type tx struct {
	tx       *sql.Tx
	readOnly bool
	notes    []store.Notification
}

func (t *tx) Outbox() store.OutboxStore   { return outboxStore{t.tx} }
func (t *tx) Audit() store.AuditStore     { return auditStore{t.tx} }
func (t *tx) Content() store.ContentStore { return contentStore{t.tx} }

func (t *tx) Notify(channel, payload string) error {
	if t.readOnly {
		return errors.New("sqlite: Notify in a read-only transaction")
	}
	t.notes = append(t.notes, store.Notification{Channel: channel, Payload: payload})
	return nil
}

func ms(t time.Time) int64     { return t.UnixMilli() }
func fromMs(v int64) time.Time { return time.UnixMilli(v).UTC() }

// Migrations

func coreSet() store.MigrationSet {
	sub, _ := fs.Sub(migrations, "migrations")
	return store.MigrationSet{Source: store.CoreSource, FS: sub}
}

const migrationTable = `CREATE TABLE IF NOT EXISTS hydrogen_migration (
    source     TEXT    NOT NULL,
    version    INTEGER NOT NULL,
    name       TEXT    NOT NULL,
    checksum   TEXT    NOT NULL,
    applied_at INTEGER NOT NULL,
    PRIMARY KEY (source, version)
)`

func (db *DB) applied(ctx context.Context) ([]store.Applied, error) {
	if _, err := db.writer.ExecContext(ctx, migrationTable); err != nil {
		return nil, fmt.Errorf("sqlite: migration table: %w", err)
	}
	rows, err := db.writer.QueryContext(ctx, `SELECT source, version, name, checksum FROM hydrogen_migration`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Applied
	for rows.Next() {
		var a store.Applied
		if err := rows.Scan(&a.Source, &a.Version, &a.Name, &a.Checksum); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (db *DB) Pending(ctx context.Context, sets []store.MigrationSet) ([]store.Migration, error) {
	applied, err := db.applied(ctx)
	if err != nil {
		return nil, err
	}
	return store.PlanMigrations(applied, append([]store.MigrationSet{coreSet()}, sets...))
}

func (db *DB) Migrate(ctx context.Context, sets []store.MigrationSet) error {
	pending, err := db.Pending(ctx, sets)
	if err != nil {
		return err
	}
	for _, m := range pending {
		err := db.Tx(ctx, func(t store.Tx) error {
			x := t.(*tx).tx
			if _, err := x.ExecContext(ctx, m.SQL); err != nil {
				return err
			}
			_, err := x.ExecContext(ctx, `INSERT INTO hydrogen_migration (source, version, name, checksum, applied_at)
				VALUES (?, ?, ?, ?, ?)`, m.Source, m.Version, m.Name, m.Checksum, ms(db.clock.Now()))
			return err
		})
		if err != nil {
			return fmt.Errorf("sqlite: migration %s/%04d_%s: %w", m.Source, m.Version, m.Name, err)
		}
	}
	return nil
}
