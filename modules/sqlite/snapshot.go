package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

const snapshotColumns = `item_id, locale, version, type, key, main_locale, always_available, payload, etag, refs, published_at`

func (s contentStore) PutSnapshots(ctx context.Context, item ids.UUID, snaps []store.Snapshot) error {
	if err := s.DeleteSnapshots(ctx, item); err != nil || len(snaps) == 0 {
		return err
	}
	args := make([]any, 0, len(snaps)*11)
	for _, sn := range snaps {
		refs := make([]byte, 0, len(sn.Refs)*16)
		for _, r := range sn.Refs {
			refs = append(refs, r[:]...)
		}
		args = append(args, uid(item), sn.Locale, sn.Version, sn.Type, nullable(sn.Key), sn.MainLocale,
			sn.AlwaysAvailable, string(sn.Payload), sn.ETag, refs, ms(sn.PublishedAt))
	}
	if err := s.insertRows(ctx, `INSERT INTO published_snapshot (`+snapshotColumns+`) VALUES`, 11, args); err != nil {
		return fmt.Errorf("sqlite: put snapshots of %s: %w", item, err)
	}
	return nil
}

func (s contentStore) DeleteSnapshots(ctx context.Context, item ids.UUID) error {
	if _, err := s.tx.ExecContext(ctx, `DELETE FROM published_snapshot WHERE item_id = ?`, uid(item)); err != nil {
		return fmt.Errorf("sqlite: delete snapshots of %s: %w", item, err)
	}
	return nil
}

// reader serves delivery reads from the read-only pool, one statement per call.
type reader struct{ db *DB }

func (db *DB) Reader() store.Reader { return reader{db} }

func (r reader) Query(ctx context.Context, q store.Query) (store.QueryResult, error) {
	return runQuery(ctx, r.db.reader, q)
}

func (r reader) Snapshots(ctx context.Context, items []ids.UUID, locales []string) ([]store.Snapshot, error) {
	if len(items) == 0 {
		return nil, nil
	}
	var out []store.Snapshot
	chunk := max(maxVars-len(locales), 1)
	for len(items) > 0 {
		n := min(len(items), chunk)
		args := make([]any, 0, n+len(locales))
		args = append(args, idArgs(items[:n])...)
		q := `SELECT ` + snapshotColumns + ` FROM published_snapshot WHERE item_id IN (` + placeholders(n) + `)`
		q, args = localeFilter(q, args, locales)
		var err error
		if out, err = r.scan(ctx, out, q, args); err != nil {
			return nil, err
		}
		items = items[n:]
	}
	return out, nil
}

func (r reader) SnapshotsByKey(ctx context.Context, key string, locales []string) ([]store.Snapshot, error) {
	if key == "" {
		return nil, nil
	}
	q, args := localeFilter(`SELECT `+snapshotColumns+` FROM published_snapshot WHERE key = ?`, []any{key}, locales)
	return r.scan(ctx, nil, q, args)
}

// localeFilter narrows a query to locales; no locales means all of them.
func localeFilter(q string, args []any, locales []string) (string, []any) {
	if len(locales) == 0 {
		return q, args
	}
	for _, l := range locales {
		args = append(args, l)
	}
	return q + ` AND locale IN (` + strings.TrimSuffix(strings.Repeat("?,", len(locales)), ",") + `)`, args
}

func (r reader) scan(ctx context.Context, out []store.Snapshot, q string, args []any) ([]store.Snapshot, error) {
	rows, err := r.db.reader.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: snapshots: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			sn   store.Snapshot
			id   []byte
			key  *string
			refs []byte
			pub  int64
		)
		if err := rows.Scan(&id, &sn.Locale, &sn.Version, &sn.Type, &key, &sn.MainLocale, &sn.AlwaysAvailable,
			&sn.Payload, &sn.ETag, &refs, &pub); err != nil {
			return nil, fmt.Errorf("sqlite: snapshots: %w", err)
		}
		if sn.ItemID, err = toUUID(id); err != nil {
			return nil, err
		}
		if key != nil {
			sn.Key = *key
		}
		if len(refs)%16 != 0 {
			return nil, fmt.Errorf("sqlite: snapshot refs of %s: %d bytes", sn.ItemID, len(refs))
		}
		if len(refs) > 0 {
			sn.Refs = make([]ids.UUID, len(refs)/16)
			for i := range sn.Refs {
				copy(sn.Refs[i][:], refs[i*16:])
			}
		}
		sn.PublishedAt = fromMs(pub)
		out = append(out, sn)
	}
	return out, rows.Err()
}
