package tests

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

func TestPostgresAnalyzeAfterMigrateAndOnDemand(t *testing.T) {
	url := pgtest.Scratch(t)
	db, err := postgres.Open(ctx, url, store.OpenOptions{Clock: newClock()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	analyzed := func(table string) bool {
		var ok bool
		err := conn.QueryRow(ctx, `SELECT coalesce((SELECT last_analyze IS NOT NULL FROM pg_stat_user_tables WHERE relname = $1), false)`, table).Scan(&ok)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	for _, table := range []string{"item", "outbox_event", "published_snapshot"} {
		if !analyzed(table) {
			t.Errorf("%s not analyzed after migrate", table)
		}
	}
	if _, err := conn.Exec(ctx, `SELECT pg_stat_reset()`); err != nil {
		t.Fatal(err)
	}
	if analyzed("item") {
		t.Skip("cannot reset statistics here")
	}
	if err := store.Analyze(ctx, db); err != nil {
		t.Fatal(err)
	}
	if !analyzed("item") {
		t.Error("store.Analyze did not analyze item")
	}
}
