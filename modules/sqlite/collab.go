package sqlite

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

type collabStore struct{ authStore }

func (t *tx) Collab() store.CollabStore { return collabStore{authStore{t.tx}} }

func splitInit(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func (c collabStore) AppendUpdate(ctx context.Context, u store.CollabUpdate) (int64, error) {
	data := u.Data
	if data == nil {
		data = []byte{}
	}
	var seq int64
	err := c.tx.QueryRowContext(ctx, `INSERT INTO collab_update (item_id, seq, client, data, init, created_at)
		SELECT ?1, (SELECT COALESCE(MAX(seq), 0) + 1 FROM collab_update WHERE item_id = ?1), ?2, ?3, ?4, ?5
		WHERE EXISTS (SELECT 1 FROM item WHERE id = ?1) RETURNING seq`,
		uid(u.Item), u.Client, data, strings.Join(u.Init, ","), ms(u.At)).Scan(&seq)
	return seq, wrapAuth("collab append", err)
}

func (c collabStore) UpdatesSince(ctx context.Context, item ids.UUID, after int64, limit int) ([]store.CollabUpdate, error) {
	if limit <= 0 {
		limit = -1
	}
	rows, err := c.tx.QueryContext(ctx, `SELECT seq, client, data, init, created_at FROM collab_update
		WHERE item_id = ? AND seq > ? ORDER BY seq LIMIT ?`, uid(item), after, limit)
	if err != nil {
		return nil, wrapAuth("collab updates", err)
	}
	defer rows.Close()
	var out []store.CollabUpdate
	for rows.Next() {
		u := store.CollabUpdate{Item: item}
		var init string
		var at int64
		if err := rows.Scan(&u.Seq, &u.Client, &u.Data, &init, &at); err != nil {
			return nil, wrapAuth("collab updates", err)
		}
		u.Init, u.At = splitInit(init), fromMs(at)
		out = append(out, u)
	}
	return out, wrapAuth("collab updates", rows.Err())
}

func (c collabStore) LastSeq(ctx context.Context, item ids.UUID) (int64, error) {
	var seq sql.NullInt64
	err := c.tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM collab_update WHERE item_id = ?`, uid(item)).Scan(&seq)
	return seq.Int64, wrapAuth("collab last seq", err)
}

func (c collabStore) Compact(ctx context.Context, item ids.UUID, upto int64, data []byte, client string, at time.Time) (int64, error) {
	last, err := c.LastSeq(ctx, item)
	if err != nil || upto <= 0 || last == 0 {
		return 0, err
	}
	upto = min(upto, last)
	rows, err := c.tx.QueryContext(ctx, `SELECT init FROM collab_update WHERE item_id = ? AND seq <= ?`, uid(item), upto)
	if err != nil {
		return 0, wrapAuth("collab compact", err)
	}
	var init []string
	n := 0
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return 0, wrapAuth("collab compact", err)
		}
		n++
		for _, p := range splitInit(s) {
			if !slices.Contains(init, p) {
				init = append(init, p)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil || n == 0 {
		return 0, wrapAuth("collab compact", err)
	}
	slices.Sort(init)
	if _, err := c.exec(ctx, "collab compact", `DELETE FROM collab_update WHERE item_id = ? AND seq <= ?`, uid(item), upto); err != nil {
		return 0, err
	}
	if data == nil {
		data = []byte{}
	}
	_, err = c.exec(ctx, "collab compact", `INSERT INTO collab_update (seq, item_id, client, data, init, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		upto, uid(item), client, data, strings.Join(init, ","), ms(at))
	if err != nil {
		return 0, err
	}
	return upto, nil
}

// LockSession does nothing: SQLite has a single writer.
func (c collabStore) LockSession(context.Context, ids.UUID) error { return nil }

func (c collabStore) EnsureSession(ctx context.Context, item ids.UUID, etag string, at time.Time) (store.CollabSession, bool, error) {
	n, err := c.exec(ctx, "collab ensure session", `INSERT INTO collab_session (item_id, etag, started_at)
		SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM item WHERE id = ?) ON CONFLICT (item_id) DO NOTHING`, uid(item), etag, ms(at), uid(item))
	if err != nil {
		return store.CollabSession{}, false, err
	}
	s, err := c.GetSession(ctx, item)
	return s, n > 0, err
}

func (c collabStore) GetSession(ctx context.Context, item ids.UUID) (store.CollabSession, error) {
	s := store.CollabSession{Item: item}
	var started, asked int64
	err := c.tx.QueryRowContext(ctx, `SELECT etag, started_at, compact_client, compact_upto, compact_at FROM collab_session WHERE item_id = ?`,
		uid(item)).Scan(&s.Etag, &started, &s.CompactClient, &s.CompactUpto, &asked)
	s.StartedAt = fromMs(started)
	if asked != 0 {
		s.CompactAt = fromMs(asked)
	}
	return s, wrapAuth("collab get session", err)
}

func (c collabStore) UpdateSession(ctx context.Context, s store.CollabSession) error {
	var asked int64
	if !s.CompactAt.IsZero() {
		asked = ms(s.CompactAt)
	}
	return c.one(ctx, "collab update session", `UPDATE collab_session SET etag = ?, compact_client = ?, compact_upto = ?, compact_at = ?
		WHERE item_id = ?`, s.Etag, s.CompactClient, s.CompactUpto, asked, uid(s.Item))
}

func (c collabStore) DeleteSession(ctx context.Context, item ids.UUID) error {
	for _, t := range []string{"collab_update", "collab_part", "collab_client", "collab_session"} {
		if _, err := c.exec(ctx, "collab delete session", `DELETE FROM `+t+` WHERE item_id = ?`, uid(item)); err != nil {
			return err
		}
	}
	return nil
}

func (c collabStore) LogSize(ctx context.Context, item ids.UUID) (int, int64, error) {
	var n int
	var b int64
	err := c.tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(length(data)), 0) FROM collab_update WHERE item_id = ?`, uid(item)).Scan(&n, &b)
	return n, b, wrapAuth("collab log size", err)
}

func (c collabStore) ClaimParts(ctx context.Context, item ids.UUID, parts []string, client string, now time.Time, expiry time.Duration) (store.CollabClaim, error) {
	var out store.CollabClaim
	cutoff := ms(now.Add(-expiry))
	for _, part := range parts {
		var got string
		err := c.tx.QueryRowContext(ctx, `INSERT INTO collab_part (item_id, part, claimed_by, claimed_at, seeded)
			SELECT ?, ?, ?, ?, 0 WHERE EXISTS (SELECT 1 FROM item WHERE id = ?)
			ON CONFLICT (item_id, part) DO UPDATE SET claimed_by = excluded.claimed_by,
				claimed_at = CASE WHEN collab_part.claimed_by = excluded.claimed_by AND collab_part.claimed_at > ?
					THEN collab_part.claimed_at ELSE excluded.claimed_at END
			WHERE collab_part.seeded = 0 AND (collab_part.claimed_by = excluded.claimed_by OR collab_part.claimed_at <= ?)
			RETURNING part`, uid(item), part, client, ms(now), uid(item), cutoff, cutoff).Scan(&got)
		if err == nil {
			out.Claimed = append(out.Claimed, part)
			continue
		}
		if err != sql.ErrNoRows {
			return out, wrapAuth("collab claim", err)
		}
		var seeded bool
		if err := c.tx.QueryRowContext(ctx, `SELECT seeded FROM collab_part WHERE item_id = ? AND part = ?`, uid(item), part).Scan(&seeded); err != nil {
			return out, wrapAuth("collab claim", err) // no row: the item does not exist
		}
		if seeded {
			out.Seeded = append(out.Seeded, part)
		} else {
			out.Pending = append(out.Pending, part)
		}
	}
	return out, nil
}

func (c collabStore) MarkSeeded(ctx context.Context, item ids.UUID, parts []string, client string) ([]string, error) {
	var out []string
	for _, part := range parts {
		n, err := c.exec(ctx, "collab seeded", `UPDATE collab_part SET seeded = 1
			WHERE item_id = ? AND part = ? AND claimed_by = ? AND seeded = 0`, uid(item), part, client)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			out = append(out, part)
		}
	}
	return out, nil
}

func (c collabStore) Parts(ctx context.Context, item ids.UUID) ([]store.CollabPart, error) {
	rows, err := c.tx.QueryContext(ctx, `SELECT part, claimed_by, claimed_at, seeded FROM collab_part WHERE item_id = ? ORDER BY part`, uid(item))
	if err != nil {
		return nil, wrapAuth("collab parts", err)
	}
	defer rows.Close()
	var out []store.CollabPart
	for rows.Next() {
		p := store.CollabPart{Item: item}
		var at int64
		if err := rows.Scan(&p.Part, &p.ClaimedBy, &at, &p.Seeded); err != nil {
			return nil, wrapAuth("collab parts", err)
		}
		p.ClaimedAt = fromMs(at)
		out = append(out, p)
	}
	return out, wrapAuth("collab parts", rows.Err())
}

func (c collabStore) ReleaseClaims(ctx context.Context, item ids.UUID, client string) error {
	_, err := c.exec(ctx, "collab release", `DELETE FROM collab_part WHERE item_id = ? AND claimed_by = ? AND seeded = 0`, uid(item), client)
	return err
}

func (c collabStore) ReleaseExpiredClaims(ctx context.Context, before time.Time) ([]ids.UUID, error) {
	return c.returningItems(ctx, "collab release expired", `DELETE FROM collab_part WHERE seeded = 0 AND claimed_at <= ? RETURNING item_id`, ms(before))
}

// returningItems runs a statement that returns item ids and gives them back de-duplicated in first-seen order.
func (c collabStore) returningItems(ctx context.Context, op, query string, args ...any) ([]ids.UUID, error) {
	rows, err := c.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapAuth(op, err)
	}
	defer rows.Close()
	var out []ids.UUID
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, wrapAuth(op, err)
		}
		u, err := toUUID(b)
		if err != nil {
			return nil, wrapAuth(op, err)
		}
		if !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out, wrapAuth(op, rows.Err())
}

const collabClientColumns = `client, item_id, principal, name, locale, focus, idle, edit, color, joined_at, seen_at`

func scanCollabClient(r scanner) (store.CollabClient, error) {
	var (
		c            store.CollabClient
		item         []byte
		joined, seen int64
	)
	if err := r.Scan(&c.Client, &item, &c.Principal, &c.Name, &c.Locale, &c.Focus, &c.Idle, &c.Edit, &c.Color, &joined, &seen); err != nil {
		return c, err
	}
	var err error
	c.Item, err = toUUID(item)
	c.JoinedAt, c.SeenAt = fromMs(joined), fromMs(seen)
	return c, err
}

func (c collabStore) PutClient(ctx context.Context, cl store.CollabClient) error {
	n, err := c.exec(ctx, "collab put client", `INSERT INTO collab_client (`+collabClientColumns+`)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ? WHERE EXISTS (SELECT 1 FROM item WHERE id = ?)
		ON CONFLICT (client) DO UPDATE SET item_id = excluded.item_id, principal = excluded.principal, name = excluded.name,
			locale = excluded.locale, focus = excluded.focus, idle = excluded.idle, edit = excluded.edit,
			color = excluded.color, seen_at = excluded.seen_at`,
		cl.Client, uid(cl.Item), cl.Principal, cl.Name, cl.Locale, cl.Focus, cl.Idle, cl.Edit, cl.Color, ms(cl.JoinedAt), ms(cl.SeenAt), uid(cl.Item))
	if err == nil && n == 0 {
		return store.ErrNotFound
	}
	return err
}

func (c collabStore) GetClient(ctx context.Context, client string) (store.CollabClient, error) {
	cl, err := scanCollabClient(c.tx.QueryRowContext(ctx, `SELECT `+collabClientColumns+` FROM collab_client WHERE client = ?`, client))
	return cl, wrapAuth("collab get client", err)
}

func (c collabStore) SetClientEdit(ctx context.Context, client string, edit bool) error {
	return c.one(ctx, "collab set edit", `UPDATE collab_client SET edit = ? WHERE client = ?`, edit, client)
}

func (c collabStore) TouchClient(ctx context.Context, client, locale, focus string, idle bool, at time.Time) error {
	return c.one(ctx, "collab touch", `UPDATE collab_client SET locale = ?, focus = ?, idle = ?, seen_at = ? WHERE client = ?`,
		locale, focus, idle, ms(at), client)
}

func (c collabStore) RemoveClient(ctx context.Context, item ids.UUID, client string) error {
	if _, err := c.exec(ctx, "collab remove client", `DELETE FROM collab_client WHERE client = ? AND item_id = ?`, client, uid(item)); err != nil {
		return err
	}
	return c.ReleaseClaims(ctx, item, client)
}

func (c collabStore) Clients(ctx context.Context, item ids.UUID) ([]store.CollabClient, error) {
	rows, err := c.tx.QueryContext(ctx, `SELECT `+collabClientColumns+` FROM collab_client WHERE item_id = ? ORDER BY joined_at, client`, uid(item))
	if err != nil {
		return nil, wrapAuth("collab clients", err)
	}
	defer rows.Close()
	var out []store.CollabClient
	for rows.Next() {
		cl, err := scanCollabClient(rows)
		if err != nil {
			return nil, wrapAuth("collab clients", err)
		}
		out = append(out, cl)
	}
	return out, wrapAuth("collab clients", rows.Err())
}

func (c collabStore) RemoveStaleClients(ctx context.Context, before time.Time) ([]ids.UUID, error) {
	rows, err := c.tx.QueryContext(ctx, `DELETE FROM collab_client WHERE seen_at < ? RETURNING client, item_id`, ms(before))
	if err != nil {
		return nil, wrapAuth("collab stale", err)
	}
	type gone struct {
		client string
		item   ids.UUID
	}
	var list []gone
	var items []ids.UUID
	for rows.Next() {
		var g gone
		var b []byte
		if err := rows.Scan(&g.client, &b); err != nil {
			rows.Close()
			return nil, wrapAuth("collab stale", err)
		}
		if g.item, err = toUUID(b); err != nil {
			rows.Close()
			return nil, wrapAuth("collab stale", err)
		}
		list = append(list, g)
		if !slices.Contains(items, g.item) {
			items = append(items, g.item)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, wrapAuth("collab stale", err)
	}
	for _, g := range list {
		if err := c.ReleaseClaims(ctx, g.item, g.client); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (c collabStore) DropIdleSessions(ctx context.Context, before time.Time) ([]ids.UUID, error) {
	items, err := c.returningItems(ctx, "collab idle", `SELECT s.item_id FROM (
			SELECT item_id, MAX(created_at) a FROM collab_update GROUP BY item_id
			UNION ALL SELECT item_id, MAX(claimed_at) FROM collab_part GROUP BY item_id
			UNION ALL SELECT item_id, started_at FROM collab_session) s
		WHERE NOT EXISTS (SELECT 1 FROM collab_client c WHERE c.item_id = s.item_id)
		GROUP BY s.item_id HAVING MAX(s.a) < ?`, ms(before))
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		if err := c.DeleteSession(ctx, it); err != nil {
			return nil, err
		}
	}
	return items, nil
}
