package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

var _ store.ExportStore = contentStore{}

func (s contentStore) SchemaRevisions(ctx context.Context) ([]store.SchemaRevision, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT rev, hash, compiled, created_at FROM schema_rev ORDER BY rev`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: schema revisions: %w", err)
	}
	defer rows.Close()
	var out []store.SchemaRevision
	for rows.Next() {
		var x store.SchemaRevision
		var created int64
		if err := rows.Scan(&x.Rev, &x.Hash, &x.Compiled, &created); err != nil {
			return nil, fmt.Errorf("sqlite: schema revisions: %w", err)
		}
		x.CreatedAt = fromMs(created)
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s contentStore) ItemsAfter(ctx context.Context, after ids.UUID, limit int) ([]store.Item, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT `+itemColumns+` FROM item WHERE id > ? AND deleted_at IS NULL ORDER BY id LIMIT ?`, uid(after), limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: items after: %w", err)
	}
	defer rows.Close()
	var out []store.Item
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("sqlite: items after: %w", err)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

func (s contentStore) VersionsOf(ctx context.Context, list []ids.UUID, publishedOnly bool) ([]store.Version, error) {
	var out []store.Version
	for chunk := range slices.Chunk(list, maxVars) {
		only, onlyLoc := "", ""
		if publishedOnly {
			only, onlyLoc = ` AND v.version = i.published_version`, ` AND l.version = i.published_version`
		}
		in := placeholders(len(chunk))
		start := len(out)
		rows, err := s.tx.QueryContext(ctx, `SELECT v.item_id, v.version, v.schema_rev, v.shared, v.created_at, v.created_by
			FROM item_version v JOIN item i ON i.id = v.item_id
			WHERE v.item_id IN (`+in+`)`+only+` ORDER BY v.item_id, v.version`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: versions of: %w", err)
		}
		for rows.Next() {
			var v store.Version
			var id []byte
			var created int64
			if err := rows.Scan(&id, &v.Version, &v.SchemaRev, &v.Shared, &created, &v.CreatedBy); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: versions of: %w", err)
			}
			if v.ItemID, err = toUUID(id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			v.CreatedAt = fromMs(created)
			out = append(out, v)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: versions of: %w", err)
		}
		rows, err = s.tx.QueryContext(ctx, `SELECT l.item_id, l.version, l.locale, l.name, l.data
			FROM item_version_locale l JOIN item i ON i.id = l.item_id
			WHERE l.item_id IN (`+in+`)`+onlyLoc+` ORDER BY l.item_id, l.version, l.locale`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: version locales of: %w", err)
		}
		i := start
		for rows.Next() {
			var raw []byte
			var version int
			var l store.VersionLocale
			if err := rows.Scan(&raw, &version, &l.Locale, &l.Name, &l.Data); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: version locales of: %w", err)
			}
			var id ids.UUID
			copy(id[:], raw)
			for i < len(out) && (out[i].ItemID != id || out[i].Version != version) {
				i++
			}
			if i == len(out) {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: version locales of: orphan locale %s/%d/%s", id, version, l.Locale)
			}
			out[i].Locales = append(out[i].Locales, l)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: version locales of: %w", err)
		}
	}
	return out, nil
}

func (s contentStore) DraftPartsFor(ctx context.Context, list []ids.UUID) ([]store.DraftPart, error) {
	var out []store.DraftPart
	for chunk := range slices.Chunk(list, maxVars) {
		rows, err := s.tx.QueryContext(ctx, `SELECT item_id, part, base_version, name, data, updated_at, updated_by FROM draft_part
			WHERE item_id IN (`+placeholders(len(chunk))+`) ORDER BY item_id, part`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: draft parts for: %w", err)
		}
		for rows.Next() {
			var p store.DraftPart
			var raw []byte
			var updated int64
			if err := rows.Scan(&raw, &p.Part, &p.BaseVersion, &p.Name, &p.Data, &updated, &p.UpdatedBy); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: draft parts for: %w", err)
			}
			copy(p.ItemID[:], raw)
			p.UpdatedAt = fromMs(updated)
			out = append(out, p)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: draft parts for: %w", err)
		}
	}
	return out, nil
}

func (s contentStore) ContentMigrationLog(ctx context.Context) ([]store.AppliedContentMigration, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT name, applied_at FROM content_migration ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: content migration log: %w", err)
	}
	defer rows.Close()
	var out []store.AppliedContentMigration
	for rows.Next() {
		var m store.AppliedContentMigration
		var at int64
		if err := rows.Scan(&m.Name, &at); err != nil {
			return nil, err
		}
		m.AppliedAt = fromMs(at)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s contentStore) ItemTypes(ctx context.Context) ([]string, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT DISTINCT type FROM item ORDER BY type`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: item types: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s contentStore) CountItems(ctx context.Context) (int, error) {
	var n int
	err := s.tx.QueryRowContext(ctx, `SELECT count(*) FROM item WHERE deleted_at IS NULL`).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("sqlite: count items: %w", err)
	}
	return n, nil
}
