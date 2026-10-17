package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

var (
	_ store.ExportStore      = contentStore{}
	_ store.ConsistentViewer = (*DB)(nil)
)

// ViewConsistent runs fn in a read-only repeatable-read transaction, so every statement sees one snapshot.
func (db *DB) ViewConsistent(ctx context.Context, fn func(store.Tx) error) error {
	return db.run(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
}

func (s contentStore) SchemaRevisions(ctx context.Context) ([]store.SchemaRevision, error) {
	rows, err := s.tx.Query(ctx, `SELECT rev, hash, compiled, created_at FROM schema_rev ORDER BY rev`)
	if err != nil {
		return nil, fmt.Errorf("postgres: schema revisions: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.SchemaRevision, error) {
		var x store.SchemaRevision
		err := r.Scan(&x.Rev, &x.Hash, &x.Compiled, &x.CreatedAt)
		x.CreatedAt = utc(x.CreatedAt)
		return x, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: schema revisions: %w", err)
	}
	return out, nil
}

func (s contentStore) ItemsAfter(ctx context.Context, after ids.UUID, limit int) ([]store.Item, error) {
	rows, err := s.tx.Query(ctx, `SELECT `+itemColumns+` FROM item WHERE id > $1 AND deleted_at IS NULL ORDER BY id LIMIT $2`, uid(after), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: items after: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Item, error) { return scanItem(r) })
	if err != nil {
		return nil, fmt.Errorf("postgres: items after: %w", err)
	}
	return out, nil
}

func (s contentStore) VersionsOf(ctx context.Context, list []ids.UUID, publishedOnly bool) ([]store.Version, error) {
	if len(list) == 0 {
		return nil, nil
	}
	only, onlyLoc := "", ""
	if publishedOnly {
		only, onlyLoc = ` AND v.version = i.published_version`, ` AND l.version = i.published_version`
	}
	rows, err := s.tx.Query(ctx, `SELECT v.item_id, v.version, v.schema_rev, v.shared, v.created_at, v.created_by
		FROM item_version v JOIN item i ON i.id = v.item_id
		WHERE v.item_id = ANY($1)`+only+` ORDER BY v.item_id, v.version`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: versions of: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Version, error) {
		var v store.Version
		var id [16]byte
		err := r.Scan(&id, &v.Version, &v.SchemaRev, &v.Shared, &v.CreatedAt, &v.CreatedBy)
		v.ItemID, v.CreatedAt = id, utc(v.CreatedAt)
		return v, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: versions of: %w", err)
	}
	rows, err = s.tx.Query(ctx, `SELECT l.item_id, l.version, l.locale, l.name, l.data
		FROM item_version_locale l JOIN item i ON i.id = l.item_id
		WHERE l.item_id = ANY($1)`+onlyLoc+` ORDER BY l.item_id, l.version, l.locale COLLATE "C"`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: version locales of: %w", err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var id [16]byte
		var version int
		var l store.VersionLocale
		if err := rows.Scan(&id, &version, &l.Locale, &l.Name, &l.Data); err != nil {
			return nil, fmt.Errorf("postgres: version locales of: %w", err)
		}
		for i < len(out) && (out[i].ItemID != id || out[i].Version != version) {
			i++
		}
		if i == len(out) {
			return nil, fmt.Errorf("postgres: version locales of: orphan locale %s/%d/%s", ids.UUID(id), version, l.Locale)
		}
		out[i].Locales = append(out[i].Locales, l)
	}
	return out, rows.Err()
}

func (s contentStore) DraftPartsFor(ctx context.Context, list []ids.UUID) ([]store.DraftPart, error) {
	if len(list) == 0 {
		return nil, nil
	}
	rows, err := s.tx.Query(ctx, `SELECT item_id, part, base_version, name, data, updated_at, updated_by FROM draft_part
		WHERE item_id = ANY($1) ORDER BY item_id, part COLLATE "C"`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: draft parts for: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.DraftPart, error) {
		var p store.DraftPart
		var id [16]byte
		err := r.Scan(&id, &p.Part, &p.BaseVersion, &p.Name, &p.Data, &p.UpdatedAt, &p.UpdatedBy)
		p.ItemID, p.UpdatedAt = id, utc(p.UpdatedAt)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: draft parts for: %w", err)
	}
	return out, nil
}

func (s contentStore) ContentMigrationLog(ctx context.Context) ([]store.AppliedContentMigration, error) {
	rows, err := s.tx.Query(ctx, `SELECT name, applied_at FROM content_migration ORDER BY name COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("postgres: content migration log: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.AppliedContentMigration, error) {
		var m store.AppliedContentMigration
		err := r.Scan(&m.Name, &m.AppliedAt)
		m.AppliedAt = utc(m.AppliedAt)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: content migration log: %w", err)
	}
	return out, nil
}

func (s contentStore) ItemTypes(ctx context.Context) ([]string, error) {
	rows, err := s.tx.Query(ctx, `SELECT DISTINCT type FROM item ORDER BY type`)
	if err != nil {
		return nil, fmt.Errorf("postgres: item types: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("postgres: item types: %w", err)
	}
	return out, nil
}

func (s contentStore) CountItems(ctx context.Context) (int, error) {
	var n int
	if err := s.tx.QueryRow(ctx, `SELECT count(*) FROM item WHERE deleted_at IS NULL`).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: count items: %w", err)
	}
	return n, nil
}
