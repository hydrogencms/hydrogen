package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

type previewStore struct{ authStore }

func (t *tx) Preview() store.PreviewStore { return previewStore{authStore{t.tx}} }

const shareColumns = `id, token_hash, item, channel, locale, created_by, created_at, expires_at, revoked_at`

func scanShare(r pgx.CollectableRow) (store.ShareSession, error) {
	var s store.ShareSession
	var id, item [16]byte
	err := r.Scan(&id, &s.TokenHash, &item, &s.Channel, &s.Locale, &s.CreatedBy, &s.CreatedAt, &s.ExpiresAt, &s.RevokedAt)
	s.ID, s.Item = id, item
	s.CreatedAt, s.ExpiresAt, s.RevokedAt = utc(s.CreatedAt), utc(s.ExpiresAt), optUTC(s.RevokedAt)
	return s, err
}

func (p previewStore) one1(ctx context.Context, op, query string, args ...any) (store.ShareSession, error) {
	rows, _ := p.tx.Query(ctx, query, args...)
	s, err := pgx.CollectExactlyOneRow(rows, scanShare)
	return s, wrapAuth(op, err)
}

func (p previewStore) CreateShare(ctx context.Context, s store.ShareSession) error {
	return p.insert(ctx, "create share", `INSERT INTO preview_share (`+shareColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uid(s.ID), s.TokenHash, uid(s.Item), s.Channel, s.Locale, s.CreatedBy, tz(s.CreatedAt), tz(s.ExpiresAt), optTz(s.RevokedAt))
}

func (p previewStore) ShareByHash(ctx context.Context, hash []byte) (store.ShareSession, error) {
	return p.one1(ctx, "share by hash", `SELECT `+shareColumns+` FROM preview_share WHERE token_hash = $1`, hash)
}

func (p previewStore) GetShare(ctx context.Context, id ids.UUID) (store.ShareSession, error) {
	return p.one1(ctx, "get share", `SELECT `+shareColumns+` FROM preview_share WHERE id = $1`, uid(id))
}

func (p previewStore) ListShares(ctx context.Context, item ids.UUID) ([]store.ShareSession, error) {
	rows, err := p.tx.Query(ctx, `SELECT `+shareColumns+` FROM preview_share WHERE item = $1 ORDER BY created_at DESC, id DESC`, uid(item))
	if err != nil {
		return nil, wrapAuth("list shares", err)
	}
	out, err := pgx.CollectRows(rows, scanShare)
	return out, wrapAuth("list shares", err)
}

func (p previewStore) RevokeShare(ctx context.Context, id ids.UUID, at time.Time) error {
	return p.one(ctx, "revoke share", `UPDATE preview_share SET revoked_at = COALESCE(revoked_at, $2) WHERE id = $1`, uid(id), tz(at))
}

func (p previewStore) DeleteExpiredShares(ctx context.Context, before time.Time) (int, error) {
	n, err := p.exec(ctx, "delete expired shares", `DELETE FROM preview_share WHERE expires_at < $1 OR revoked_at < $1`, tz(before))
	return int(n), err
}

func (p previewStore) PutRelay(ctx context.Context, d store.RelayDraft) (int64, error) {
	fields := d.Fields
	if len(fields) == 0 {
		fields = json.RawMessage("{}")
	}
	var rev int64
	err := p.tx.QueryRow(ctx, `INSERT INTO preview_relay (item, locale, fields, rev, updated_by, updated_at)
		VALUES ($1, $2, $3, 1, $4, $5)
		ON CONFLICT (item, locale) DO UPDATE SET fields = excluded.fields, rev = preview_relay.rev + 1,
			updated_by = excluded.updated_by, updated_at = excluded.updated_at
		RETURNING rev`, uid(d.Item), d.Locale, []byte(fields), d.UpdatedBy, tz(d.UpdatedAt)).Scan(&rev)
	return rev, wrapAuth("put relay", err)
}

func (p previewStore) GetRelay(ctx context.Context, item ids.UUID, locale string) (store.RelayDraft, error) {
	var d store.RelayDraft
	var id [16]byte
	var fields []byte
	err := p.tx.QueryRow(ctx, `SELECT item, locale, fields, rev, updated_by, updated_at FROM preview_relay WHERE item = $1 AND locale = $2`,
		uid(item), locale).Scan(&id, &d.Locale, &fields, &d.Rev, &d.UpdatedBy, &d.UpdatedAt)
	if err != nil {
		return d, wrapAuth("get relay", err)
	}
	d.Item, d.Fields, d.UpdatedAt = id, json.RawMessage(fields), utc(d.UpdatedAt)
	return d, nil
}

func (p previewStore) DeleteRelay(ctx context.Context, item ids.UUID, locale string) error {
	_, err := p.exec(ctx, "delete relay", `DELETE FROM preview_relay WHERE item = $1 AND locale = $2`, uid(item), locale)
	return err
}

func (p previewStore) DeleteStaleRelays(ctx context.Context, before time.Time) (int, error) {
	n, err := p.exec(ctx, "delete stale relays", `DELETE FROM preview_relay WHERE updated_at < $1`, tz(before))
	return int(n), err
}

func (p previewStore) GetRelays(ctx context.Context, items []ids.UUID) ([]store.RelayDraft, error) {
	if len(items) == 0 {
		return nil, nil
	}
	rows, err := p.tx.Query(ctx, `SELECT item, locale, fields, rev, updated_by, updated_at FROM preview_relay WHERE item = ANY($1)`, idArgs(items))
	if err != nil {
		return nil, wrapAuth("get relays", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.RelayDraft, error) {
		var (
			d      store.RelayDraft
			id     [16]byte
			fields []byte
		)
		err := r.Scan(&id, &d.Locale, &fields, &d.Rev, &d.UpdatedBy, &d.UpdatedAt)
		d.Item, d.Fields, d.UpdatedAt = id, json.RawMessage(fields), utc(d.UpdatedAt)
		return d, err
	})
	return out, wrapAuth("get relays", err)
}
