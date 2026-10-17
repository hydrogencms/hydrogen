package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	msqlite "modernc.org/sqlite"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// runner is what *sql.Tx and *sql.DB share.
type runner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// sqlTx is the store.SQL of a transaction, or of the reader pool when readOnly.
type sqlTx struct {
	tx       runner
	readOnly bool
}

func (t *tx) SQL() store.SQL { return sqlTx{tx: t.tx} }

// Read runs statements on the read-only pool without a transaction.
func (db *DB) Read() store.SQL { return sqlTx{tx: db.reader, readOnly: true} }

func (s sqlTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	if s.readOnly {
		return 0, store.ErrReadOnly
	}
	res, err := s.tx.ExecContext(ctx, query, bindArgs(args)...)
	if err != nil {
		return 0, mapErr(err)
	}
	return res.RowsAffected()
}

func (s sqlTx) Query(ctx context.Context, query string, args ...any) (store.Rows, error) {
	rows, err := s.tx.QueryContext(ctx, query, bindArgs(args)...)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqlRows{rows}, nil
}

func (s sqlTx) QueryRow(ctx context.Context, query string, args ...any) store.Row {
	return sqlRow{s.tx.QueryRowContext(ctx, query, bindArgs(args)...)}
}

type sqlRows struct{ r *sql.Rows }

func (r sqlRows) Next() bool   { return r.r.Next() }
func (r sqlRows) Err() error   { return mapErr(r.r.Err()) }
func (r sqlRows) Close() error { return r.r.Close() }
func (r sqlRows) Scan(dest ...any) error {
	return mapErr(r.r.Scan(scanDests(dest)...))
}

type sqlRow struct{ r *sql.Row }

func (r sqlRow) Scan(dest ...any) error {
	err := r.r.Scan(scanDests(dest)...)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	return mapErr(err)
}

// SQLITE_CONSTRAINT and the extended codes that mean a duplicate key; every other extended constraint code
// (check, foreign key incl. ON DELETE RESTRICT, not null, trigger, …) shares the primary code 19.
const (
	codeConstraint = 19
	codePrimaryKey = 1555
	codeUnique     = 2067
)

// mapErr turns constraint violations into store errors that still carry the driver's message.
func mapErr(err error) error {
	var se *msqlite.Error
	if err == nil || !errors.As(err, &se) {
		return err
	}
	switch code := se.Code(); {
	case code == codeUnique || code == codePrimaryKey:
		return fmt.Errorf("%w: %w", store.ErrDuplicateKey, err)
	case code&0xff == codeConstraint:
		return fmt.Errorf("%w: %w", store.ErrConstraint, err)
	}
	return err
}

// bindArgs converts arguments to SQLite storage.
func bindArgs(args []any) []any {
	if len(args) == 0 {
		return nil
	}
	out := make([]any, len(args))
	for i, a := range args {
		out[i] = bindArg(a)
	}
	return out
}

func bindArg(a any) any {
	switch v := a.(type) {
	case ids.UUID:
		return v[:]
	case time.Time:
		return ms(v)
	case bool:
		return boolInt(v)
	case json.RawMessage:
		if v == nil {
			return nil
		}
		return string(v)
	case []byte:
		if v == nil {
			return nil
		}
		return v
	case *ids.UUID:
		return deref(v, bindArg)
	case *time.Time:
		return deref(v, bindArg)
	case *bool:
		return deref(v, bindArg)
	case *json.RawMessage:
		return deref(v, bindArg)
	case *[]byte:
		return deref(v, bindArg)
	case *string:
		return deref(v, bindArg)
	case *int:
		return deref(v, bindArg)
	case *int64:
		return deref(v, bindArg)
	case *float64:
		return deref(v, bindArg)
	}
	return a
}

func deref[T any](p *T, conv func(any) any) any {
	if p == nil {
		return nil
	}
	return conv(*p)
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// scanFunc adapts a function to sql.Scanner.
type scanFunc func(src any) error

func (f scanFunc) Scan(src any) error { return f(src) }

// scanDests swaps destinations the database/sql conversions do not cover. The slice is copied on first change.
func scanDests(dest []any) []any {
	out, copied := dest, false
	for i, d := range dest {
		var s any
		switch p := d.(type) {
		case *ids.UUID:
			s = required(p, uuidOf)
		case **ids.UUID:
			s = optional(p, uuidOf)
		case *time.Time:
			s = required(p, timeOf)
		case **time.Time:
			s = optional(p, timeOf)
		case *json.RawMessage:
			s = required(p, rawOf)
		case **json.RawMessage:
			s = optional(p, rawOf)
		default:
			continue
		}
		if !copied {
			out, copied = append([]any(nil), dest...), true
		}
		out[i] = s
	}
	return out
}

func required[T any](dst *T, conv func(any) (T, error)) scanFunc {
	return func(src any) error {
		if src == nil {
			return fmt.Errorf("sqlite: NULL into %T", dst)
		}
		v, err := conv(src)
		if err == nil {
			*dst = v
		}
		return err
	}
}

func optional[T any](dst **T, conv func(any) (T, error)) scanFunc {
	return func(src any) error {
		if src == nil {
			*dst = nil
			return nil
		}
		v, err := conv(src)
		if err == nil {
			*dst = &v
		}
		return err
	}
}

func uuidOf(src any) (ids.UUID, error) {
	var u ids.UUID
	b, ok := src.([]byte)
	if !ok || len(b) != len(u) {
		return u, fmt.Errorf("sqlite: cannot read %T as a uuid", src)
	}
	copy(u[:], b)
	return u, nil
}

func timeOf(src any) (time.Time, error) {
	switch v := src.(type) {
	case int64:
		return fromMs(v), nil
	case time.Time:
		return v.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("sqlite: cannot read %T as a time", src)
}

func rawOf(src any) (json.RawMessage, error) {
	switch v := src.(type) {
	case string:
		return json.RawMessage(v), nil
	case []byte:
		return json.RawMessage(append([]byte(nil), v...)), nil
	}
	return nil, fmt.Errorf("sqlite: cannot read %T as JSON", src)
}
