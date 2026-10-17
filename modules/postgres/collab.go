package postgres

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

type collabStore struct{ authStore }

func (t *tx) Collab() store.CollabStore { return collabStore{authStore{t.tx}} }

func splitInit(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// lock serializes the transactions that write one item's log until they commit. It is taken before a seq is
// drawn, so within an item a later seq always commits after an earlier one and readers never see a gap fill.
func (c collabStore) lock(ctx context.Context, item ids.UUID) error {
	_, err := c.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('collab:' || $1::uuid::text, 0))`, uid(item))
	return wrapAuth("collab lock", err)
}

func (c collabStore) AppendUpdate(ctx context.Context, u store.CollabUpdate) (int64, error) {
	if err := c.lock(ctx, u.Item); err != nil {
		return 0, err
	}
	data := u.Data
	if data == nil {
		data = []byte{}
	}
	var seq int64
	err := c.tx.QueryRow(ctx, `INSERT INTO collab_update (item_id, seq, client, data, init, created_at)
		SELECT $1::uuid, (SELECT COALESCE(MAX(seq), 0) + 1 FROM collab_update WHERE item_id = $1::uuid), $2, $3, $4, $5 WHERE EXISTS (SELECT 1 FROM item WHERE id = $1) RETURNING seq`,
		uid(u.Item), u.Client, data, strings.Join(u.Init, ","), tz(u.At)).Scan(&seq)
	return seq, wrapAuth("collab append", err)
}

func (c collabStore) UpdatesSince(ctx context.Context, item ids.UUID, after int64, limit int) ([]store.CollabUpdate, error) {
	var lim *int
	if limit > 0 {
		lim = &limit
	}
	rows, err := c.tx.Query(ctx, `SELECT seq, client, data, init, created_at FROM collab_update
		WHERE item_id = $1 AND seq > $2 ORDER BY seq LIMIT $3`, uid(item), after, lim)
	if err != nil {
		return nil, wrapAuth("collab updates", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.CollabUpdate, error) {
		u := store.CollabUpdate{Item: item}
		var init string
		err := r.Scan(&u.Seq, &u.Client, &u.Data, &init, &u.At)
		u.Init, u.At = splitInit(init), utc(u.At)
		return u, err
	})
	return out, wrapAuth("collab updates", err)
}

func (c collabStore) LastSeq(ctx context.Context, item ids.UUID) (int64, error) {
	var seq *int64
	err := c.tx.QueryRow(ctx, `SELECT MAX(seq) FROM collab_update WHERE item_id = $1`, uid(item)).Scan(&seq)
	if seq == nil {
		return 0, wrapAuth("collab last seq", err)
	}
	return *seq, wrapAuth("collab last seq", err)
}

func (c collabStore) Compact(ctx context.Context, item ids.UUID, upto int64, data []byte, client string, at time.Time) (int64, error) {
	if err := c.lock(ctx, item); err != nil {
		return 0, err
	}
	last, err := c.LastSeq(ctx, item)
	if err != nil || upto <= 0 || last == 0 {
		return 0, err
	}
	upto = min(upto, last)
	rows, err := c.tx.Query(ctx, `SELECT init FROM collab_update WHERE item_id = $1 AND seq <= $2`, uid(item), upto)
	if err != nil {
		return 0, wrapAuth("collab compact", err)
	}
	inits, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(inits) == 0 {
		return 0, wrapAuth("collab compact", err)
	}
	var init []string
	for _, s := range inits {
		for _, p := range splitInit(s) {
			if !slices.Contains(init, p) {
				init = append(init, p)
			}
		}
	}
	slices.Sort(init)
	if _, err := c.exec(ctx, "collab compact", `DELETE FROM collab_update WHERE item_id = $1 AND seq <= $2`, uid(item), upto); err != nil {
		return 0, err
	}
	if data == nil {
		data = []byte{}
	}
	_, err = c.exec(ctx, "collab compact", `INSERT INTO collab_update (seq, item_id, client, data, init, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
		upto, uid(item), client, data, strings.Join(init, ","), tz(at))
	if err != nil {
		return 0, err
	}
	return upto, nil
}

func (c collabStore) LockSession(ctx context.Context, item ids.UUID) error { return c.lock(ctx, item) }

func (c collabStore) EnsureSession(ctx context.Context, item ids.UUID, etag string, at time.Time) (store.CollabSession, bool, error) {
	n, err := c.exec(ctx, "collab ensure session", `INSERT INTO collab_session (item_id, etag, started_at)
		SELECT $1, $2, $3 WHERE EXISTS (SELECT 1 FROM item WHERE id = $1) ON CONFLICT (item_id) DO NOTHING`, uid(item), etag, tz(at))
	if err != nil {
		return store.CollabSession{}, false, err
	}
	s, err := c.GetSession(ctx, item)
	return s, n > 0, err
}

func (c collabStore) GetSession(ctx context.Context, item ids.UUID) (store.CollabSession, error) {
	s := store.CollabSession{Item: item}
	var asked *time.Time
	err := c.tx.QueryRow(ctx, `SELECT etag, started_at, compact_client, compact_upto, compact_at FROM collab_session WHERE item_id = $1`,
		uid(item)).Scan(&s.Etag, &s.StartedAt, &s.CompactClient, &s.CompactUpto, &asked)
	s.StartedAt = utc(s.StartedAt)
	if asked != nil {
		s.CompactAt = utc(*asked)
	}
	return s, wrapAuth("collab get session", err)
}

func (c collabStore) UpdateSession(ctx context.Context, s store.CollabSession) error {
	var asked *time.Time
	if !s.CompactAt.IsZero() {
		t := tz(s.CompactAt)
		asked = &t
	}
	return c.one(ctx, "collab update session", `UPDATE collab_session SET etag = $2, compact_client = $3, compact_upto = $4, compact_at = $5
		WHERE item_id = $1`, uid(s.Item), s.Etag, s.CompactClient, s.CompactUpto, asked)
}

func (c collabStore) DeleteSession(ctx context.Context, item ids.UUID) error {
	if err := c.lock(ctx, item); err != nil {
		return err
	}
	for _, t := range []string{"collab_update", "collab_part", "collab_client", "collab_session"} {
		if _, err := c.exec(ctx, "collab delete session", `DELETE FROM `+t+` WHERE item_id = $1`, uid(item)); err != nil {
			return err
		}
	}
	return nil
}

func (c collabStore) LogSize(ctx context.Context, item ids.UUID) (int, int64, error) {
	var n int
	var b int64
	err := c.tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(length(data)), 0)::bigint FROM collab_update WHERE item_id = $1`, uid(item)).Scan(&n, &b)
	return n, b, wrapAuth("collab log size", err)
}

func (c collabStore) ClaimParts(ctx context.Context, item ids.UUID, parts []string, client string, now time.Time, expiry time.Duration) (store.CollabClaim, error) {
	var out store.CollabClaim
	cutoff := tz(now.Add(-expiry))
	for _, part := range parts {
		var got string
		err := c.tx.QueryRow(ctx, `INSERT INTO collab_part (item_id, part, claimed_by, claimed_at, seeded)
			SELECT $1, $2, $3, $4, false WHERE EXISTS (SELECT 1 FROM item WHERE id = $1)
			ON CONFLICT (item_id, part) DO UPDATE SET claimed_by = excluded.claimed_by,
				claimed_at = CASE WHEN collab_part.claimed_by = excluded.claimed_by AND collab_part.claimed_at > $5
					THEN collab_part.claimed_at ELSE excluded.claimed_at END
			WHERE NOT collab_part.seeded AND (collab_part.claimed_by = excluded.claimed_by OR collab_part.claimed_at <= $5)
			RETURNING part`, uid(item), part, client, tz(now), cutoff).Scan(&got)
		if err == nil {
			out.Claimed = append(out.Claimed, part)
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return out, wrapAuth("collab claim", err)
		}
		var seeded bool
		if err := c.tx.QueryRow(ctx, `SELECT seeded FROM collab_part WHERE item_id = $1 AND part = $2`, uid(item), part).Scan(&seeded); err != nil {
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
		n, err := c.exec(ctx, "collab seeded", `UPDATE collab_part SET seeded = true
			WHERE item_id = $1 AND part = $2 AND claimed_by = $3 AND NOT seeded`, uid(item), part, client)
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
	rows, err := c.tx.Query(ctx, `SELECT part, claimed_by, claimed_at, seeded FROM collab_part WHERE item_id = $1 ORDER BY part`, uid(item))
	if err != nil {
		return nil, wrapAuth("collab parts", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.CollabPart, error) {
		p := store.CollabPart{Item: item}
		err := r.Scan(&p.Part, &p.ClaimedBy, &p.ClaimedAt, &p.Seeded)
		p.ClaimedAt = utc(p.ClaimedAt)
		return p, err
	})
	return out, wrapAuth("collab parts", err)
}

func (c collabStore) ReleaseClaims(ctx context.Context, item ids.UUID, client string) error {
	_, err := c.exec(ctx, "collab release", `DELETE FROM collab_part WHERE item_id = $1 AND claimed_by = $2 AND NOT seeded`, uid(item), client)
	return err
}

func (c collabStore) ReleaseExpiredClaims(ctx context.Context, before time.Time) ([]ids.UUID, error) {
	return c.returningItems(ctx, "collab release expired", `DELETE FROM collab_part WHERE NOT seeded AND claimed_at <= $1 RETURNING item_id`, tz(before))
}

// returningItems runs a statement that returns item ids and gives them back de-duplicated in first-seen order.
func (c collabStore) returningItems(ctx context.Context, op, query string, args ...any) ([]ids.UUID, error) {
	rows, err := c.tx.Query(ctx, query, args...)
	if err != nil {
		return nil, wrapAuth(op, err)
	}
	all, err := pgx.CollectRows(rows, pgx.RowTo[[16]byte])
	if err != nil {
		return nil, wrapAuth(op, err)
	}
	var out []ids.UUID
	for _, u := range all {
		if !slices.Contains(out, u) {
			out = append(out, u)
		}
	}
	return out, nil
}

const collabClientColumns = `client, item_id, principal, name, locale, focus, idle, edit, color, joined_at, seen_at`

func scanCollabClient(r pgx.CollectableRow) (store.CollabClient, error) {
	var c store.CollabClient
	var item [16]byte
	err := r.Scan(&c.Client, &item, &c.Principal, &c.Name, &c.Locale, &c.Focus, &c.Idle, &c.Edit, &c.Color, &c.JoinedAt, &c.SeenAt)
	c.Item, c.JoinedAt, c.SeenAt = item, utc(c.JoinedAt), utc(c.SeenAt)
	return c, err
}

func (c collabStore) PutClient(ctx context.Context, cl store.CollabClient) error {
	n, err := c.exec(ctx, "collab put client", `INSERT INTO collab_client (`+collabClientColumns+`)
		SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11 WHERE EXISTS (SELECT 1 FROM item WHERE id = $2)
		ON CONFLICT (client) DO UPDATE SET item_id = excluded.item_id, principal = excluded.principal, name = excluded.name,
			locale = excluded.locale, focus = excluded.focus, idle = excluded.idle, edit = excluded.edit,
			color = excluded.color, seen_at = excluded.seen_at`,
		cl.Client, uid(cl.Item), cl.Principal, cl.Name, cl.Locale, cl.Focus, cl.Idle, cl.Edit, cl.Color, tz(cl.JoinedAt), tz(cl.SeenAt))
	if err == nil && n == 0 {
		return store.ErrNotFound
	}
	return err
}

func (c collabStore) GetClient(ctx context.Context, client string) (store.CollabClient, error) {
	rows, _ := c.tx.Query(ctx, `SELECT `+collabClientColumns+` FROM collab_client WHERE client = $1`, client)
	cl, err := pgx.CollectExactlyOneRow(rows, scanCollabClient)
	return cl, wrapAuth("collab get client", err)
}

func (c collabStore) SetClientEdit(ctx context.Context, client string, edit bool) error {
	return c.one(ctx, "collab set edit", `UPDATE collab_client SET edit = $2 WHERE client = $1`, client, edit)
}

func (c collabStore) TouchClient(ctx context.Context, client, locale, focus string, idle bool, at time.Time) error {
	return c.one(ctx, "collab touch", `UPDATE collab_client SET locale = $2, focus = $3, idle = $4, seen_at = $5 WHERE client = $1`,
		client, locale, focus, idle, tz(at))
}

func (c collabStore) RemoveClient(ctx context.Context, item ids.UUID, client string) error {
	if _, err := c.exec(ctx, "collab remove client", `DELETE FROM collab_client WHERE client = $1 AND item_id = $2`, client, uid(item)); err != nil {
		return err
	}
	return c.ReleaseClaims(ctx, item, client)
}

func (c collabStore) Clients(ctx context.Context, item ids.UUID) ([]store.CollabClient, error) {
	rows, err := c.tx.Query(ctx, `SELECT `+collabClientColumns+` FROM collab_client WHERE item_id = $1 ORDER BY joined_at, client`, uid(item))
	if err != nil {
		return nil, wrapAuth("collab clients", err)
	}
	out, err := pgx.CollectRows(rows, scanCollabClient)
	return out, wrapAuth("collab clients", err)
}

func (c collabStore) RemoveStaleClients(ctx context.Context, before time.Time) ([]ids.UUID, error) {
	rows, err := c.tx.Query(ctx, `DELETE FROM collab_client WHERE seen_at < $1 RETURNING client, item_id`, tz(before))
	if err != nil {
		return nil, wrapAuth("collab stale", err)
	}
	type gone struct {
		client string
		item   [16]byte
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (g gone, err error) {
		err = r.Scan(&g.client, &g.item)
		return
	})
	if err != nil {
		return nil, wrapAuth("collab stale", err)
	}
	var items []ids.UUID
	for _, g := range list {
		if err := c.ReleaseClaims(ctx, g.item, g.client); err != nil {
			return nil, err
		}
		if !slices.Contains(items, g.item) {
			items = append(items, g.item)
		}
	}
	return items, nil
}

func (c collabStore) DropIdleSessions(ctx context.Context, before time.Time) ([]ids.UUID, error) {
	items, err := c.returningItems(ctx, "collab idle", `SELECT s.item_id FROM (
			SELECT item_id, MAX(created_at) AS a FROM collab_update GROUP BY item_id
			UNION ALL SELECT item_id, MAX(claimed_at) FROM collab_part GROUP BY item_id
			UNION ALL SELECT item_id, started_at FROM collab_session) s
		WHERE NOT EXISTS (SELECT 1 FROM collab_client c WHERE c.item_id = s.item_id)
		GROUP BY s.item_id HAVING MAX(s.a) < $1`, tz(before))
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
