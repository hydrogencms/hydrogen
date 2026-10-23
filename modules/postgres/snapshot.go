package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

const snapshotColumns = `item_id, locale, version, type, key, main_locale, always_available, payload, etag, refs, published_at`

func (s contentStore) PutSnapshots(ctx context.Context, item ids.UUID, snaps []store.Snapshot) error {
	if len(snaps) == 0 {
		return s.DeleteSnapshots(ctx, item)
	}
	var b strings.Builder
	b.WriteString(`INSERT INTO published_snapshot (` + snapshotColumns + `) VALUES `)
	args := make([]any, 0, len(snaps)*11)
	for i, sn := range snaps {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := range 11 {
			if c > 0 {
				b.WriteByte(',')
			}
			b.WriteString("$" + strconv.Itoa(i*11+c+1))
		}
		b.WriteByte(')')
		args = append(args, uid(item), sn.Locale, sn.Version, sn.Type, nullable(sn.Key), sn.MainLocale,
			sn.AlwaysAvailable, string(sn.Payload), sn.ETag, idArgs(sn.Refs), tz(sn.PublishedAt))
	}
	// One round trip for the delete and the insert.
	var batch pgx.Batch
	batch.Queue(`DELETE FROM published_snapshot WHERE item_id = $1`, uid(item))
	batch.Queue(b.String(), args...)
	res := s.tx.SendBatch(ctx, &batch)
	_, err := res.Exec()
	if err == nil {
		_, err = res.Exec()
	}
	if err := errors.Join(err, res.Close()); err != nil {
		return fmt.Errorf("postgres: put snapshots of %s: %w", item, err)
	}
	return nil
}

func (s contentStore) DeleteSnapshots(ctx context.Context, item ids.UUID) error {
	if _, err := s.tx.Exec(ctx, `DELETE FROM published_snapshot WHERE item_id = $1`, uid(item)); err != nil {
		return fmt.Errorf("postgres: delete snapshots of %s: %w", item, err)
	}
	return nil
}

// reader serves delivery reads straight from the pool, one statement per call.
type reader struct{ db *DB }

func (db *DB) Reader() store.Reader { return reader{db} }

func (r reader) Query(ctx context.Context, q store.Query) (store.QueryResult, error) {
	return runQuery(ctx, r.db.pool, q)
}

func (r reader) Facet(ctx context.Context, q store.Query, field string, limit int) ([]store.FacetCount, error) {
	return runFacet(ctx, r.db.pool, q, field, limit)
}

func (r reader) Snapshots(ctx context.Context, items []ids.UUID, locales []string) ([]store.Snapshot, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if len(locales) == 0 {
		return r.scan(ctx, `SELECT `+snapshotColumns+` FROM published_snapshot WHERE item_id = ANY($1)`, idArgs(items))
	}
	return r.scan(ctx, `SELECT `+snapshotColumns+` FROM published_snapshot WHERE item_id = ANY($1) AND locale = ANY($2)`,
		idArgs(items), locales)
}

func (r reader) SnapshotsByKey(ctx context.Context, key string, locales []string) ([]store.Snapshot, error) {
	if key == "" {
		return nil, nil
	}
	if len(locales) == 0 {
		return r.scan(ctx, `SELECT `+snapshotColumns+` FROM published_snapshot WHERE key = $1`, key)
	}
	return r.scan(ctx, `SELECT `+snapshotColumns+` FROM published_snapshot WHERE key = $1 AND locale = ANY($2)`, key, locales)
}

func (r reader) scan(ctx context.Context, q string, args ...any) ([]store.Snapshot, error) {
	rows, err := r.db.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: snapshots: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.Snapshot, error) {
		var (
			sn   store.Snapshot
			id   [16]byte
			key  *string
			refs [][16]byte
		)
		err := row.Scan(&id, &sn.Locale, &sn.Version, &sn.Type, &key, &sn.MainLocale, &sn.AlwaysAvailable,
			&sn.Payload, &sn.ETag, &refs, &sn.PublishedAt)
		sn.ItemID = id
		if key != nil {
			sn.Key = *key
		}
		if len(refs) > 0 {
			sn.Refs = make([]ids.UUID, len(refs))
			for i, r := range refs {
				sn.Refs[i] = r
			}
		}
		sn.PublishedAt = sn.PublishedAt.UTC()
		return sn, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: snapshots: %w", err)
	}
	return out, nil
}
