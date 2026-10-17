package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

type releaseStore struct{ authStore }

func (t *tx) Releases() store.ReleaseStore { return releaseStore{authStore{t.tx}} }

const releaseColumns = `id, name, description, kind, status, scheduled_at, published_at, errors,
	created_by, updated_by, scheduled_by, created_at, updated_at`

func scanRelease(r pgx.CollectableRow) (store.Release, error) {
	var x store.Release
	var id [16]byte
	var errs []byte
	err := r.Scan(&id, &x.Name, &x.Description, &x.Kind, &x.Status, &x.ScheduledAt, &x.PublishedAt, &errs,
		&x.CreatedBy, &x.UpdatedBy, &x.ScheduledBy, &x.CreatedAt, &x.UpdatedAt)
	x.ID = id
	if len(errs) > 0 {
		x.Errors = json.RawMessage(errs)
	}
	x.ScheduledAt, x.PublishedAt = optUTC(x.ScheduledAt), optUTC(x.PublishedAt)
	x.CreatedAt, x.UpdatedAt = utc(x.CreatedAt), utc(x.UpdatedAt)
	return x, err
}

// releaseErrors binds the errors document; nil binds NULL.
func releaseErrors(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func kindOf(k string) string {
	if k == "" {
		return "release"
	}
	return k
}

func (r releaseStore) CreateRelease(ctx context.Context, x store.Release) error {
	return r.insert(ctx, "create release", `INSERT INTO release (`+releaseColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10, $11, $12, $13)`,
		uid(x.ID), x.Name, x.Description, kindOf(x.Kind), x.Status, optTz(x.ScheduledAt), optTz(x.PublishedAt),
		releaseErrors(x.Errors), x.CreatedBy, x.UpdatedBy, x.ScheduledBy, tz(x.CreatedAt), tz(x.UpdatedAt))
}

func (r releaseStore) UpdateRelease(ctx context.Context, x store.Release) error {
	return r.one(ctx, "update release", `UPDATE release SET name = $2, description = $3, kind = $4, status = $5,
			scheduled_at = $6, published_at = $7, errors = $8::jsonb, updated_by = $9, scheduled_by = $10, updated_at = $11
		WHERE id = $1`, uid(x.ID), x.Name, x.Description, kindOf(x.Kind), x.Status, optTz(x.ScheduledAt),
		optTz(x.PublishedAt), releaseErrors(x.Errors), x.UpdatedBy, x.ScheduledBy, tz(x.UpdatedAt))
}

func (r releaseStore) TransitionRelease(ctx context.Context, x store.Release, from []string) error {
	n, err := r.exec(ctx, "transition release", `UPDATE release SET name = $2, description = $3, kind = $4, status = $5,
			scheduled_at = $6, published_at = $7, errors = $8::jsonb, updated_by = $9, scheduled_by = $10, updated_at = $11
		WHERE id = $1 AND status = ANY($12)`, uid(x.ID), x.Name, x.Description, kindOf(x.Kind), x.Status, optTz(x.ScheduledAt),
		optTz(x.PublishedAt), releaseErrors(x.Errors), x.UpdatedBy, x.ScheduledBy, tz(x.UpdatedAt), from)
	if err != nil || n > 0 {
		return err
	}
	var exists bool
	if err := r.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM release WHERE id = $1)`, uid(x.ID)).Scan(&exists); err != nil {
		return wrapAuth("transition release", err)
	}
	if !exists {
		return store.ErrNotFound
	}
	return store.ErrStateChanged
}

func (r releaseStore) ReleaseItemCounts(ctx context.Context, list []ids.UUID) (map[ids.UUID]int, error) {
	out := map[ids.UUID]int{}
	if len(list) == 0 {
		return out, nil
	}
	rows, err := r.tx.Query(ctx, `SELECT release_id, count(*) FROM release_item WHERE release_id = ANY($1) GROUP BY release_id`, idArgs(list))
	if err != nil {
		return nil, wrapAuth("release item counts", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id [16]byte
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, wrapAuth("release item counts", err)
		}
		out[ids.UUID(id)] = n
	}
	return out, wrapAuth("release item counts", rows.Err())
}

func (r releaseStore) ReleaseItemsOfItem(ctx context.Context, item ids.UUID) ([]store.ReleaseItem, error) {
	return r.releaseItems(ctx, "release items of item", `WHERE item_id = $1`, uid(item))
}

func (r releaseStore) GetRelease(ctx context.Context, id ids.UUID) (store.Release, error) {
	rows, _ := r.tx.Query(ctx, `SELECT `+releaseColumns+` FROM release WHERE id = $1`, uid(id))
	x, err := pgx.CollectExactlyOneRow(rows, scanRelease)
	return x, wrapAuth("get release", err)
}

func (r releaseStore) DeleteRelease(ctx context.Context, id ids.UUID) error {
	return r.one(ctx, "delete release", `DELETE FROM release WHERE id = $1`, uid(id))
}

func (r releaseStore) ListReleases(ctx context.Context, f store.ReleaseFilter, p store.Page) ([]store.Release, int, error) {
	var conds []string
	var args []any
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if len(f.Statuses) > 0 {
		conds = append(conds, "status = ANY("+bind(f.Statuses)+")")
	}
	if f.Kind != "" {
		conds = append(conds, "kind = "+bind(f.Kind))
	}
	if f.Item != (ids.UUID{}) {
		conds = append(conds, "id IN (SELECT release_id FROM release_item WHERE item_id = "+bind(uid(f.Item))+")")
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return listPage(ctx, r.authStore, "list releases", `SELECT count(*) FROM release`+where,
		`SELECT `+releaseColumns+` FROM release`+where+` ORDER BY created_at DESC, id DESC`, p, scanRelease, args...)
}

// PutReleaseItem inserts only when the release and the item exist (a foreign key violation would abort the
// transaction); otherwise it returns ErrConstraint.
func (r releaseStore) PutReleaseItem(ctx context.Context, it store.ReleaseItem) error {
	locales, err := json.Marshal(append([]string{}, it.Locales...))
	if err != nil {
		return err
	}
	n, err := r.exec(ctx, "put release item", `INSERT INTO release_item (release_id, item_id, action, locales, added_by, added_at)
		SELECT $1, $2, $3, $4::jsonb, $5, $6
		WHERE EXISTS (SELECT 1 FROM release WHERE id = $1) AND EXISTS (SELECT 1 FROM item WHERE id = $2)
		ON CONFLICT (release_id, item_id) DO UPDATE SET action = excluded.action, locales = excluded.locales,
			added_by = excluded.added_by, added_at = excluded.added_at`,
		uid(it.ReleaseID), uid(it.ItemID), it.Action, string(locales), it.AddedBy, tz(it.AddedAt))
	if err == nil && n == 0 {
		return fmt.Errorf("postgres: put release item: release or item missing: %w", store.ErrConstraint)
	}
	return err
}

func (r releaseStore) RemoveReleaseItem(ctx context.Context, release, item ids.UUID) error {
	_, err := r.exec(ctx, "remove release item", `DELETE FROM release_item WHERE release_id = $1 AND item_id = $2`, uid(release), uid(item))
	return err
}

func (r releaseStore) ReleaseItems(ctx context.Context, release ids.UUID) ([]store.ReleaseItem, error) {
	return r.releaseItems(ctx, "release items", `WHERE release_id = $1`, uid(release))
}

func (r releaseStore) releaseItems(ctx context.Context, op, where string, arg any) ([]store.ReleaseItem, error) {
	rows, err := r.tx.Query(ctx, `SELECT release_id, item_id, action, locales, added_by, added_at FROM release_item `+
		where+` ORDER BY added_at, release_id, item_id`, arg)
	if err != nil {
		return nil, wrapAuth(op, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.ReleaseItem, error) {
		var it store.ReleaseItem
		var rid, iid [16]byte
		var locales []byte
		if err := row.Scan(&rid, &iid, &it.Action, &locales, &it.AddedBy, &it.AddedAt); err != nil {
			return it, err
		}
		it.ReleaseID, it.ItemID, it.AddedAt = rid, iid, utc(it.AddedAt)
		if err := json.Unmarshal(locales, &it.Locales); err != nil {
			return it, fmt.Errorf("release item locales: %w", err)
		}
		if len(it.Locales) == 0 {
			it.Locales = nil
		}
		return it, nil
	})
	return out, wrapAuth(op, err)
}

func (r releaseStore) DueReleases(ctx context.Context, now time.Time, limit int) ([]store.Release, error) {
	rows, err := r.tx.Query(ctx, `SELECT `+releaseColumns+` FROM release
		WHERE status = 'scheduled' AND scheduled_at <= $1 ORDER BY scheduled_at, id LIMIT $2`, tz(now), max(limit, 1))
	if err != nil {
		return nil, wrapAuth("due releases", err)
	}
	out, err := pgx.CollectRows(rows, scanRelease)
	return out, wrapAuth("due releases", err)
}
