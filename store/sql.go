package store

import (
	"context"
	"errors"
)

// ErrConstraint is returned when a statement violates a foreign key, check or not-null constraint. Unique
// violations return ErrDuplicateKey instead. The returned error also wraps the driver's, so its message names
// the constraint.
var ErrConstraint = errors.New("store: constraint violation")

// SQL runs a module's own statements inside a transaction (Tx.SQL), so modules keep tables of their own without
// a driver dependency.
//
//   - Placeholders are always "?"; the Postgres adapter rewrites them to $1, $2 … outside quoted strings,
//     identifiers, comments and dollar-quoted strings. Postgres operators that contain "?" (jsonb ?, ?|, ?&) are
//     therefore unavailable; use jsonb_exists, jsonb_exists_any and jsonb_exists_all.
//   - Arguments of type ids.UUID, time.Time, bool, string, []byte, json.RawMessage, int, int64, float64 and nil,
//     and pointers to them (a nil pointer is NULL), are converted to the dialect's storage: SQLite uuid → BLOB(16),
//     time → INTEGER Unix milliseconds UTC, bool → 0/1, json.RawMessage → TEXT; Postgres uuid → uuid, time →
//     timestamptz (millisecond precision), json.RawMessage → the column's json/jsonb/text. A nil []byte or
//     json.RawMessage is NULL.
//   - Scan destinations of the same types, and pointers to them for NULLable columns (**ids.UUID, **string …),
//     are converted back; times come back in UTC. Scanning NULL into a non-pointer destination is an error.
//   - Everything else (DDL, functions, ON CONFLICT, RETURNING …) is dialect SQL: branch on DB.Dialect().
//
// Unique violations return ErrDuplicateKey; foreign key, check and not-null violations return ErrConstraint.
// On Postgres a failed statement aborts the transaction (as always): do not continue after an error.
type SQL interface {
	// Exec runs a statement and returns the number of rows it changed.
	Exec(ctx context.Context, query string, args ...any) (rowsAffected int64, err error)
	// Query runs a statement that returns rows. The caller must Close the rows, and must not run another
	// statement on the same transaction before that.
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	// QueryRow runs a statement and keeps its first row; the statement's error is returned by Scan.
	QueryRow(ctx context.Context, query string, args ...any) Row
}

// ErrReadOnly is returned by the statements of DB.Read when asked to write.
var ErrReadOnly = errors.New("store: read-only statements cannot write")

// Rows iterates a result. Next advances (false at the end or on error; check Err), Scan reads the current row.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
	Close() error
}

// Row is a single-row result.
type Row interface {
	// Scan reads the row, or returns ErrNotFound when the statement returned none.
	Scan(dest ...any) error
}
