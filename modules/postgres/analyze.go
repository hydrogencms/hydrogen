package postgres

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/store"
)

// Analyze refreshes the planner statistics of every table in the connection's schema (store.Analyzer).
func (db *DB) Analyze(ctx context.Context) error {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	tables, err := schemaTables(ctx, conn.Conn())
	if err != nil {
		return err
	}
	return analyzeTables(ctx, conn.Conn(), tables)
}

func schemaTables(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = current_schema() ORDER BY tablename`)
	if err != nil {
		return nil, fmt.Errorf("postgres: list tables: %w", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("postgres: list tables: %w", err)
	}
	return tables, nil
}

// analyzeTouched analyzes the existing tables the migrations mention.
func analyzeTouched(ctx context.Context, conn *pgx.Conn, ms []store.Migration) error {
	if len(ms) == 0 {
		return nil
	}
	have, err := schemaTables(ctx, conn)
	if err != nil {
		return err
	}
	var tables []string
	for _, t := range touchedTables(ms) {
		if slices.Contains(have, t) {
			tables = append(tables, t)
		}
	}
	return analyzeTables(ctx, conn, tables)
}

func analyzeTables(ctx context.Context, conn *pgx.Conn, tables []string) error {
	for _, t := range tables {
		if _, err := conn.Exec(ctx, "ANALYZE "+pgx.Identifier{t}.Sanitize()); err != nil {
			return fmt.Errorf("postgres: analyze %s: %w", t, err)
		}
	}
	return nil
}

var touchedTable = regexp.MustCompile(`(?i)\b(?:CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|ALTER\s+TABLE(?:\s+IF\s+EXISTS)?(?:\s+ONLY)?|INSERT\s+INTO|UPDATE|CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?\S+\s+ON(?:\s+ONLY)?)\s+"?([a-z_][a-z0-9_]*)"?`)

// touchedTables lists the names after the statements that create, alter, index or write a table (a superset: the
// caller keeps those that exist), sorted.
// the migrations create, alter, index or write to, sorted.
func touchedTables(ms []store.Migration) []string {
	var out []string
	for _, m := range ms {
		for _, sub := range touchedTable.FindAllStringSubmatch(m.SQL, -1) {
			if t := strings.ToLower(sub[1]); t != "hydrogen_migration" {
				out = append(out, t)
			}
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
