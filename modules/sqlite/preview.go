package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

type previewStore struct{ authStore }

func (t *tx) Preview() store.PreviewStore { return previewStore{authStore{t.tx}} }

const shareColumns = `id, token_hash, item, channel, locale, created_by, created_at, expires_at, revoked_at`

func scanShare(r scanner) (store.ShareSession, error) {
	var (
		s            store.ShareSession
		id, item     []byte
		created, exp int64
		revoked      sql.NullInt64
	)
	if err := r.Scan(&id, &s.TokenHash, &item, &s.Channel, &s.Locale, &s.CreatedBy, &created, &exp, &revoked); err != nil {
		return s, err
	}
	var err error
	if s.ID, err = toUUID(id); err != nil {
		return s, err
	}
	if s.Item, err = toUUID(item); err != nil {
		return s, err
	}
	s.TokenHash = slices.Clone(s.TokenHash)
	s.CreatedAt, s.ExpiresAt, s.RevokedAt = fromMs(created), fromMs(exp), fromOptMs(revoked)
	return s, nil
}

func (p previewStore) CreateShare(ctx context.Context, s store.ShareSession) error {
	_, err := p.exec(ctx, "create share", `INSERT INTO preview_share (`+shareColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid(s.ID), s.TokenHash, uid(s.Item), s.Channel, s.Locale, s.CreatedBy, ms(s.CreatedAt), ms(s.ExpiresAt), optMs(s.RevokedAt))
	return err
}

func (p previewStore) ShareByHash(ctx context.Context, hash []byte) (store.ShareSession, error) {
	s, err := scanShare(p.tx.QueryRowContext(ctx, `SELECT `+shareColumns+` FROM preview_share WHERE token_hash = ?`, hash))
	return s, wrapAuth("share by hash", err)
}

func (p previewStore) GetShare(ctx context.Context, id ids.UUID) (store.ShareSession, error) {
	s, err := scanShare(p.tx.QueryRowContext(ctx, `SELECT `+shareColumns+` FROM preview_share WHERE id = ?`, uid(id)))
	return s, wrapAuth("get share", err)
}

func (p previewStore) ListShares(ctx context.Context, item ids.UUID) ([]store.ShareSession, error) {
	rows, err := p.tx.QueryContext(ctx, `SELECT `+shareColumns+` FROM preview_share WHERE item = ? ORDER BY created_at DESC, id DESC`, uid(item))
	if err != nil {
		return nil, wrapAuth("list shares", err)
	}
	defer rows.Close()
	var out []store.ShareSession
	for rows.Next() {
		s, err := scanShare(rows)
		if err != nil {
			return nil, wrapAuth("list shares", err)
		}
		out = append(out, s)
	}
	return out, wrapAuth("list shares", rows.Err())
}

func (p previewStore) RevokeShare(ctx context.Context, id ids.UUID, at time.Time) error {
	return p.one(ctx, "revoke share", `UPDATE preview_share SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, ms(at), uid(id))
}

func (p previewStore) DeleteExpiredShares(ctx context.Context, before time.Time) (int, error) {
	n, err := p.exec(ctx, "delete expired shares", `DELETE FROM preview_share WHERE expires_at < ? OR revoked_at < ?`, ms(before), ms(before))
	return int(n), err
}

func (p previewStore) PutRelay(ctx context.Context, d store.RelayDraft) (int64, error) {
	var rev int64
	err := p.tx.QueryRowContext(ctx, `INSERT INTO preview_relay (item, locale, fields, rev, updated_by, updated_at)
		VALUES (?, ?, ?, 1, ?, ?)
		ON CONFLICT (item, locale) DO UPDATE SET fields = excluded.fields, rev = preview_relay.rev + 1,
			updated_by = excluded.updated_by, updated_at = excluded.updated_at
		RETURNING rev`, uid(d.Item), d.Locale, jsonOr(d.Fields, "{}"), d.UpdatedBy, ms(d.UpdatedAt)).Scan(&rev)
	return rev, wrapAuth("put relay", err)
}

func (p previewStore) GetRelay(ctx context.Context, item ids.UUID, locale string) (store.RelayDraft, error) {
	var (
		d       store.RelayDraft
		id      []byte
		fields  string
		updated int64
	)
	err := p.tx.QueryRowContext(ctx, `SELECT item, locale, fields, rev, updated_by, updated_at FROM preview_relay WHERE item = ? AND locale = ?`,
		uid(item), locale).Scan(&id, &d.Locale, &fields, &d.Rev, &d.UpdatedBy, &updated)
	if err != nil {
		return d, wrapAuth("get relay", err)
	}
	if d.Item, err = toUUID(id); err != nil {
		return d, err
	}
	d.Fields, d.UpdatedAt = json.RawMessage(fields), fromMs(updated)
	return d, nil
}

func (p previewStore) DeleteRelay(ctx context.Context, item ids.UUID, locale string) error {
	_, err := p.exec(ctx, "delete relay", `DELETE FROM preview_relay WHERE item = ? AND locale = ?`, uid(item), locale)
	return err
}

func (p previewStore) DeleteStaleRelays(ctx context.Context, before time.Time) (int, error) {
	n, err := p.exec(ctx, "delete stale relays", `DELETE FROM preview_relay WHERE updated_at < ?`, ms(before))
	return int(n), err
}

func (p previewStore) GetRelays(ctx context.Context, items []ids.UUID) ([]store.RelayDraft, error) {
	var out []store.RelayDraft
	for len(items) > 0 {
		chunk := items[:min(len(items), 500)]
		items = items[len(chunk):]
		rows, err := p.tx.QueryContext(ctx, `SELECT item, locale, fields, rev, updated_by, updated_at FROM preview_relay
			WHERE item IN (`+placeholders(len(chunk))+`)`, idArgs(chunk)...)
		if err != nil {
			return nil, wrapAuth("get relays", err)
		}
		for rows.Next() {
			var (
				d       store.RelayDraft
				id      []byte
				fields  string
				updated int64
			)
			if err := rows.Scan(&id, &d.Locale, &fields, &d.Rev, &d.UpdatedBy, &updated); err != nil {
				rows.Close()
				return nil, wrapAuth("get relays", err)
			}
			if d.Item, err = toUUID(id); err != nil {
				rows.Close()
				return nil, err
			}
			d.Fields, d.UpdatedAt = json.RawMessage(fields), fromMs(updated)
			out = append(out, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, wrapAuth("get relays", err)
		}
	}
	return out, nil
}
