package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// runner is what pgx.Tx and *pgxpool.Pool share.
type runner interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// sqlTx is the store.SQL of a transaction, or of the pool (autocommit) when readOnly.
type sqlTx struct {
	tx       runner
	rewrite  *rewriteCache
	readOnly bool
}

func (t *tx) SQL() store.SQL { return sqlTx{tx: t.tx, rewrite: t.rewrite} }

// Read runs statements on the pool without a transaction.
func (db *DB) Read() store.SQL { return sqlTx{tx: db.pool, rewrite: db.rewrite, readOnly: true} }

func (s sqlTx) Exec(ctx context.Context, query string, args ...any) (int64, error) {
	if s.readOnly {
		return 0, store.ErrReadOnly
	}
	tag, err := s.tx.Exec(ctx, s.rewrite.rewrite(query), bindArgs(args)...)
	if err != nil {
		return 0, mapErr(err)
	}
	return tag.RowsAffected(), nil
}

func (s sqlTx) Query(ctx context.Context, query string, args ...any) (store.Rows, error) {
	rows, err := s.tx.Query(ctx, s.rewrite.rewrite(query), bindArgs(args)...)
	if err != nil {
		return nil, mapErr(err)
	}
	return sqlRows{rows}, nil
}

func (s sqlTx) QueryRow(ctx context.Context, query string, args ...any) store.Row {
	return sqlRow{s.tx.QueryRow(ctx, s.rewrite.rewrite(query), bindArgs(args)...)}
}

type sqlRows struct{ r pgx.Rows }

func (r sqlRows) Next() bool { return r.r.Next() }
func (r sqlRows) Err() error { return mapErr(r.r.Err()) }

// Close releases the connection; iteration errors are reported by Err.
func (r sqlRows) Close() error { r.r.Close(); return nil }

func (r sqlRows) Scan(dest ...any) error { return mapErr(r.r.Scan(scanDests(dest)...)) }

type sqlRow struct{ r pgx.Row }

func (r sqlRow) Scan(dest ...any) error {
	err := r.r.Scan(scanDests(dest)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return mapErr(err)
}

// mapErr turns constraint violations into store errors that still carry the driver's message.
func mapErr(err error) error {
	var pg *pgconn.PgError
	if err == nil || !errors.As(err, &pg) {
		return err
	}
	switch pg.Code {
	case "23505":
		return fmt.Errorf("%w: %w", store.ErrDuplicateKey, err)
	case "23503", "23514", "23502":
		return fmt.Errorf("%w: %w", store.ErrConstraint, err)
	}
	return err
}

// bindArgs converts arguments to what pgx encodes into the column's type. The slice is copied on first change.
func bindArgs(args []any) []any {
	out, copied := args, false
	for i, a := range args {
		v, ok := bindArg(a)
		if !ok {
			continue
		}
		if !copied {
			out, copied = append([]any(nil), args...), true
		}
		out[i] = v
	}
	return out
}

// bindArg reports whether a needed conversion.
func bindArg(a any) (any, bool) {
	switch v := a.(type) {
	case ids.UUID:
		return [16]byte(v), true
	case time.Time:
		return v.UTC().Truncate(time.Millisecond), true
	case json.RawMessage:
		return []byte(v), true
	case *ids.UUID:
		return deref(v)
	case *time.Time:
		return deref(v)
	case *json.RawMessage:
		return deref(v)
	case *bool:
		return deref(v)
	case *[]byte:
		return deref(v)
	case *string:
		return deref(v)
	case *int:
		return deref(v)
	case *int64:
		return deref(v)
	case *float64:
		return deref(v)
	}
	return a, false
}

func deref[T any](p *T) (any, bool) {
	if p == nil {
		return nil, true
	}
	v, _ := bindArg(*p)
	return v, true
}

// scanDests swaps destinations pgx does not read as we want (UTC times, ids.UUID behind a pointer).
func scanDests(dest []any) []any {
	out, copied := dest, false
	for i, d := range dest {
		var s any
		switch p := d.(type) {
		case *ids.UUID:
			s = (*[16]byte)(p)
		case **ids.UUID:
			s = optUUID{p}
		case *time.Time:
			s = utcTime{p}
		case **time.Time:
			s = optTime{p}
		case *json.RawMessage:
			s = (*[]byte)(p)
		case **json.RawMessage:
			s = optRaw{p}
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

type optUUID struct{ p **ids.UUID }

func (o optUUID) ScanUUID(v pgtype.UUID) error {
	if !v.Valid {
		*o.p = nil
		return nil
	}
	u := ids.UUID(v.Bytes)
	*o.p = &u
	return nil
}

type utcTime struct{ p *time.Time }

func (o utcTime) ScanTimestamptz(v pgtype.Timestamptz) error {
	if !v.Valid {
		return errors.New("postgres: NULL into time.Time")
	}
	*o.p = v.Time.UTC()
	return nil
}

type optTime struct{ p **time.Time }

func (o optTime) ScanTimestamptz(v pgtype.Timestamptz) error {
	if !v.Valid {
		*o.p = nil
		return nil
	}
	t := v.Time.UTC()
	*o.p = &t
	return nil
}

type optRaw struct{ p **json.RawMessage }

// ScanBytes receives pgx's buffer, valid only until the next row, or nil for NULL; the value is copied.
func (o optRaw) ScanBytes(v []byte) error {
	if v == nil {
		*o.p = nil
		return nil
	}
	r := json.RawMessage(bytes.Clone(v))
	*o.p = &r
	return nil
}
