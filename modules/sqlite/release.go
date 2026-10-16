package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

type releaseStore struct{ authStore }

func (t *tx) Releases() store.ReleaseStore { return releaseStore{authStore{t.tx}} }

const releaseColumns = `id, name, description, kind, status, scheduled_at, published_at, errors,
	created_by, updated_by, scheduled_by, created_at, updated_at`

func scanRelease(r scanner) (store.Release, error) {
	var (
		x                store.Release
		id               []byte
		sched, pub       sql.NullInt64
		errs             sql.NullString
		created, updated int64
	)
	if err := r.Scan(&id, &x.Name, &x.Description, &x.Kind, &x.Status, &sched, &pub, &errs,
		&x.CreatedBy, &x.UpdatedBy, &x.ScheduledBy, &created, &updated); err != nil {
		return x, err
	}
	var err error
	if x.ID, err = toUUID(id); err != nil {
		return x, err
	}
	x.ScheduledAt, x.PublishedAt = fromOptMs(sched), fromOptMs(pub)
	if errs.Valid {
		x.Errors = json.RawMessage(errs.String)
	}
	x.CreatedAt, x.UpdatedAt = fromMs(created), fromMs(updated)
	return x, nil
}

func releaseErrors(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func (r releaseStore) CreateRelease(ctx context.Context, x store.Release) error {
	_, err := r.exec(ctx, "create release", `INSERT INTO release (`+releaseColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid(x.ID), x.Name, x.Description, kindOf(x.Kind), x.Status, optMs(x.ScheduledAt), optMs(x.PublishedAt),
		releaseErrors(x.Errors), x.CreatedBy, x.UpdatedBy, x.ScheduledBy, ms(x.CreatedAt), ms(x.UpdatedAt))
	return err
}

func kindOf(k string) string {
	if k == "" {
		return "release"
	}
	return k
}

func (r releaseStore) UpdateRelease(ctx context.Context, x store.Release) error {
	return r.one(ctx, "update release", `UPDATE release SET name = ?, description = ?, kind = ?, status = ?,
			scheduled_at = ?, published_at = ?, errors = ?, updated_by = ?, scheduled_by = ?, updated_at = ?
		WHERE id = ?`, x.Name, x.Description, kindOf(x.Kind), x.Status, optMs(x.ScheduledAt), optMs(x.PublishedAt),
		releaseErrors(x.Errors), x.UpdatedBy, x.ScheduledBy, ms(x.UpdatedAt), uid(x.ID))
}

func (r releaseStore) TransitionRelease(ctx context.Context, x store.Release, from []string) error {
	args := []any{x.Name, x.Description, kindOf(x.Kind), x.Status, optMs(x.ScheduledAt), optMs(x.PublishedAt),
		releaseErrors(x.Errors), x.UpdatedBy, x.ScheduledBy, ms(x.UpdatedAt), uid(x.ID)}
	for _, f := range from {
		args = append(args, f)
	}
	n, err := r.exec(ctx, "transition release", `UPDATE release SET name = ?, description = ?, kind = ?, status = ?,
			scheduled_at = ?, published_at = ?, errors = ?, updated_by = ?, scheduled_by = ?, updated_at = ?
		WHERE id = ? AND status IN (`+placeholders(len(from))+`)`, args...)
	if err != nil || n > 0 {
		return err
	}
	var one int
	if err := r.tx.QueryRowContext(ctx, `SELECT 1 FROM release WHERE id = ?`, uid(x.ID)).Scan(&one); err != nil {
		return wrapAuth("transition release", err)
	}
	return store.ErrStateChanged
}

func (r releaseStore) ReleaseItemCounts(ctx context.Context, list []ids.UUID) (map[ids.UUID]int, error) {
	out := map[ids.UUID]int{}
	for chunk := range slices.Chunk(list, maxVars) {
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = uid(id)
		}
		rows, err := r.tx.QueryContext(ctx, `SELECT release_id, count(*) FROM release_item
			WHERE release_id IN (`+placeholders(len(chunk))+`) GROUP BY release_id`, args...)
		if err != nil {
			return nil, wrapAuth("release item counts", err)
		}
		for rows.Next() {
			var raw []byte
			var n int
			if err := rows.Scan(&raw, &n); err != nil {
				_ = rows.Close()
				return nil, wrapAuth("release item counts", err)
			}
			id, err := toUUID(raw)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = n
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, wrapAuth("release item counts", err)
		}
	}
	return out, nil
}

func (r releaseStore) ReleaseItemsOfItem(ctx context.Context, item ids.UUID) ([]store.ReleaseItem, error) {
	return r.releaseItems(ctx, "release items of item", `WHERE item_id = ?`, uid(item))
}

func (r releaseStore) GetRelease(ctx context.Context, id ids.UUID) (store.Release, error) {
	x, err := scanRelease(r.tx.QueryRowContext(ctx, `SELECT `+releaseColumns+` FROM release WHERE id = ?`, uid(id)))
	return x, wrapAuth("get release", err)
}

func (r releaseStore) DeleteRelease(ctx context.Context, id ids.UUID) error {
	return r.one(ctx, "delete release", `DELETE FROM release WHERE id = ?`, uid(id))
}

func (r releaseStore) ListReleases(ctx context.Context, f store.ReleaseFilter, p store.Page) ([]store.Release, int, error) {
	var conds []string
	var args []any
	if len(f.Statuses) > 0 {
		conds = append(conds, "status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.Kind != "" {
		conds = append(conds, "kind = ?")
		args = append(args, f.Kind)
	}
	if f.Item != (ids.UUID{}) {
		conds = append(conds, "id IN (SELECT release_id FROM release_item WHERE item_id = ?)")
		args = append(args, uid(f.Item))
	}
	where := ""
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	var out []store.Release
	total, err := r.list(ctx, "list releases", `SELECT count(*) FROM release`+where,
		`SELECT `+releaseColumns+` FROM release`+where+` ORDER BY created_at DESC, id DESC`, storeutil.NormalizePage(p), args,
		func(s scanner) error {
			x, err := scanRelease(s)
			out = append(out, x)
			return err
		})
	return out, total, err
}

func (r releaseStore) PutReleaseItem(ctx context.Context, it store.ReleaseItem) error {
	locales, err := json.Marshal(append([]string{}, it.Locales...))
	if err != nil {
		return err
	}
	_, err = r.exec(ctx, "put release item", `INSERT INTO release_item (release_id, item_id, action, locales, added_by, added_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (release_id, item_id) DO UPDATE SET action = excluded.action, locales = excluded.locales,
			added_by = excluded.added_by, added_at = excluded.added_at`,
		uid(it.ReleaseID), uid(it.ItemID), it.Action, string(locales), it.AddedBy, ms(it.AddedAt))
	return err
}

func (r releaseStore) RemoveReleaseItem(ctx context.Context, release, item ids.UUID) error {
	_, err := r.exec(ctx, "remove release item", `DELETE FROM release_item WHERE release_id = ? AND item_id = ?`, uid(release), uid(item))
	return err
}

func (r releaseStore) ReleaseItems(ctx context.Context, release ids.UUID) ([]store.ReleaseItem, error) {
	return r.releaseItems(ctx, "release items", `WHERE release_id = ?`, uid(release))
}

func (r releaseStore) releaseItems(ctx context.Context, op, where string, arg any) ([]store.ReleaseItem, error) {
	rows, err := r.tx.QueryContext(ctx, `SELECT release_id, item_id, action, locales, added_by, added_at FROM release_item `+
		where+` ORDER BY added_at, release_id, item_id`, arg)
	if err != nil {
		return nil, wrapAuth(op, err)
	}
	defer rows.Close()
	var out []store.ReleaseItem
	for rows.Next() {
		var (
			it       store.ReleaseItem
			rid, iid []byte
			locales  string
			added    int64
		)
		if err := rows.Scan(&rid, &iid, &it.Action, &locales, &it.AddedBy, &added); err != nil {
			return nil, wrapAuth(op, err)
		}
		if it.ReleaseID, err = toUUID(rid); err != nil {
			return nil, err
		}
		if it.ItemID, err = toUUID(iid); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(locales), &it.Locales); err != nil {
			return nil, fmt.Errorf("sqlite: release item locales: %w", err)
		}
		if len(it.Locales) == 0 {
			it.Locales = nil
		}
		it.AddedAt = fromMs(added)
		out = append(out, it)
	}
	return out, wrapAuth(op, rows.Err())
}

func (r releaseStore) DueReleases(ctx context.Context, now time.Time, limit int) ([]store.Release, error) {
	rows, err := r.tx.QueryContext(ctx, `SELECT `+releaseColumns+` FROM release
		WHERE status = 'scheduled' AND scheduled_at <= ? ORDER BY scheduled_at, id LIMIT ?`, ms(now), max(limit, 1))
	if err != nil {
		return nil, wrapAuth("due releases", err)
	}
	defer rows.Close()
	var out []store.Release
	for rows.Next() {
		x, err := scanRelease(rows)
		if err != nil {
			return nil, wrapAuth("due releases", err)
		}
		out = append(out, x)
	}
	return out, wrapAuth("due releases", rows.Err())
}
