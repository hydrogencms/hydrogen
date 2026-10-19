// Package postgres is Hydrogen's Postgres adapter (16+), for production and multi-node setups.
//
// Database URLs are standard Postgres URLs (postgres://user:pass@host:5432/db). Notifications use
// LISTEN/NOTIFY, locks use advisory locks, and outbox claims use FOR UPDATE SKIP LOCKED, so several servers can
// share one database.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
)

const Dialect = "postgres"

//go:embed migrations/*.sql
var migrations embed.FS

// Module registers the adapter for postgres: and postgresql: URLs.
func Module() kernel.Module { return module{} }

type module struct{}

func (module) Name() string { return "postgres" }

func (module) Register(r *kernel.Registry) error {
	if err := r.Database("postgres", Open); err != nil {
		return err
	}
	return r.Database("postgresql", Open)
}

type DB struct {
	pool     *pgxpool.Pool
	lockPool *pgxpool.Pool
	clock    clock.Clock
	listener *listener
	stop     context.CancelFunc
	wg       sync.WaitGroup
	rewrite  *rewriteCache
}

func Open(ctx context.Context, url string, o store.OpenOptions) (store.DB, error) {
	if !strings.HasPrefix(url, "postgres://") && !strings.HasPrefix(url, "postgresql://") {
		return nil, fmt.Errorf("postgres: URL must start with postgres:// or postgresql://")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	// Advisory locks hold their connection until released. Taken from the shared pool they could use every
	// connection (a pool is only max(4, CPUs) wide) and starve the work the lock holders need to do.
	lcfg := cfg.Copy()
	lcfg.MaxConns = lockPoolSize
	lcfg.MinConns = 0
	lockPool, err := pgxpool.NewWithConfig(ctx, lcfg)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	lctx, stop := context.WithCancel(context.Background())
	db := &DB{pool: pool, lockPool: lockPool, clock: o.Clock, stop: stop, rewrite: newRewriteCache()}
	db.listener = newListener(lctx, cfg.ConnConfig, o.Log, &db.wg)
	return db, nil
}

func (db *DB) Dialect() string                  { return Dialect }
func (db *DB) Capabilities() store.Capabilities { return store.Capabilities{MultiNode: true} }
func (db *DB) Notifier() store.Notifier         { return db.listener }
func (db *DB) Locker() store.Locker             { return locker{db.lockPool} }
func (db *DB) Ping(ctx context.Context) error   { return db.pool.Ping(ctx) }

func (db *DB) Close() error {
	db.stop()
	db.wg.Wait()
	db.lockPool.Close()
	db.pool.Close()
	return nil
}

func (db *DB) Tx(ctx context.Context, fn func(store.Tx) error) error {
	return db.run(ctx, pgx.TxOptions{}, fn)
}

func (db *DB) View(ctx context.Context, fn func(store.Tx) error) error {
	return db.run(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

func (db *DB) run(ctx context.Context, opts pgx.TxOptions, fn func(store.Tx) error) error {
	pgTx, err := db.pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = pgTx.Rollback(context.Background())
			panic(r)
		}
	}()
	if err := fn(&tx{tx: pgTx, rewrite: db.rewrite}); err != nil {
		_ = pgTx.Rollback(context.Background())
		return err
	}
	if err := pgTx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}

type tx struct {
	tx      pgx.Tx
	rewrite *rewriteCache
}

func (t *tx) Outbox() store.OutboxStore { return outboxStore{t.tx} }
func (t *tx) Audit() store.AuditStore   { return auditStore{t.tx} }

// Notify uses pg_notify, which Postgres delivers on commit.
func (t *tx) Notify(channel, payload string) error {
	if _, err := t.tx.Exec(context.Background(), `SELECT pg_notify($1, $2)`, channel, payload); err != nil {
		return fmt.Errorf("postgres: notify: %w", err)
	}
	return nil
}

// Migrations

func coreSet() store.MigrationSet {
	sub, _ := fs.Sub(migrations, "migrations")
	return store.MigrationSet{Source: store.CoreSource, FS: sub}
}

const migrationTable = `CREATE TABLE IF NOT EXISTS hydrogen_migration (
    source     text        NOT NULL,
    version    integer     NOT NULL,
    name       text        NOT NULL,
    checksum   text        NOT NULL,
    applied_at timestamptz NOT NULL,
    PRIMARY KEY (source, version)
)`

func applied(ctx context.Context, q pgx.Tx) ([]store.Applied, error) {
	rows, err := q.Query(ctx, `SELECT source, version, name, checksum FROM hydrogen_migration`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Applied, error) {
		var a store.Applied
		err := r.Scan(&a.Source, &a.Version, &a.Name, &a.Checksum)
		return a, err
	})
}

func (db *DB) Pending(ctx context.Context, sets []store.MigrationSet) ([]store.Migration, error) {
	var pending []store.Migration
	err := db.Tx(ctx, func(t store.Tx) error {
		x := t.(*tx).tx
		if _, err := x.Exec(ctx, migrationTable); err != nil {
			return err
		}
		a, err := applied(ctx, x)
		if err != nil {
			return err
		}
		pending, err = store.PlanMigrations(a, append([]store.MigrationSet{coreSet()}, sets...))
		return err
	})
	return pending, err
}

// Migrate holds an advisory lock for the whole run, so servers starting together apply each migration once.
func (db *DB) Migrate(ctx context.Context, sets []store.MigrationSet) error {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended('hydrogen:migrate', 0))`); err != nil {
		return fmt.Errorf("postgres: migration lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended('hydrogen:migrate', 0))`)
	}()

	pending, err := db.Pending(ctx, sets)
	if err != nil {
		return err
	}
	for _, m := range pending {
		err := pgx.BeginFunc(ctx, conn, func(x pgx.Tx) error {
			if _, err := x.Exec(ctx, m.SQL, pgx.QueryExecModeSimpleProtocol); err != nil {
				return err
			}
			_, err := x.Exec(ctx, `INSERT INTO hydrogen_migration (source, version, name, checksum, applied_at)
				VALUES ($1, $2, $3, $4, $5)`, m.Source, m.Version, m.Name, m.Checksum, db.clock.Now())
			return err
		})
		if err != nil {
			return fmt.Errorf("postgres: migration %s/%04d_%s: %w", m.Source, m.Version, m.Name, err)
		}
	}
	// Tables the migrations just created or reshaped have no statistics yet; refresh them so the first queries
	// are planned on real numbers.
	return analyzeTouched(ctx, conn.Conn(), pending)
}

// Locks

// lockPoolSize bounds the advisory locks one process holds at once.
const lockPoolSize = 32

type locker struct{ pool *pgxpool.Pool }

// TryLock takes a session advisory lock on a connection held until Release.
func (l locker) TryLock(ctx context.Context, name string) (store.Lock, bool, error) {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 0))`, "hydrogen:"+name).Scan(&ok); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !ok {
		conn.Release()
		return nil, false, nil
	}
	return &lock{conn: conn, name: name}, true, nil
}

type lock struct {
	conn *pgxpool.Conn
	name string
	once sync.Once
}

func (k *lock) Release(ctx context.Context) error {
	var err error
	k.once.Do(func() {
		_, err = k.conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, "hydrogen:"+k.name)
		if err != nil {
			// The lock is tied to the session; closing the connection is the only safe way to drop it.
			_ = k.conn.Conn().Close(context.Background())
		}
		k.conn.Release()
	})
	if err != nil {
		return errors.Join(errors.New("postgres: unlock failed, connection closed"), err)
	}
	return nil
}
