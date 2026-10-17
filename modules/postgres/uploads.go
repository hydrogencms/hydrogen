package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/core/store"
)

type uploadStore struct{ authStore }

func (t *tx) Uploads() store.UploadStore { return uploadStore{authStore{t.tx}} }

const uploadColumns = `sha, mime, size, width, height, created_by, created_at, touched_at`

func scanUpload(r pgx.CollectableRow) (store.Upload, error) {
	var u store.Upload
	var w, h int32
	err := r.Scan(&u.SHA, &u.MIME, &u.Size, &w, &h, &u.CreatedBy, &u.CreatedAt, &u.TouchedAt)
	u.Width, u.Height = int(w), int(h)
	u.CreatedAt, u.TouchedAt = utc(u.CreatedAt), utc(u.TouchedAt)
	return u, err
}

func (s uploadStore) Put(ctx context.Context, u store.Upload) (bool, error) {
	n, err := s.exec(ctx, "put upload", `INSERT INTO media_upload (`+uploadColumns+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (sha) DO NOTHING`,
		u.SHA, u.MIME, u.Size, u.Width, u.Height, u.CreatedBy, tz(u.CreatedAt), tz(u.TouchedAt))
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}
	_, err = s.exec(ctx, "touch upload", `UPDATE media_upload SET touched_at = greatest(touched_at, $2) WHERE sha = $1`, u.SHA, tz(u.TouchedAt))
	return false, err
}

func (s uploadStore) Get(ctx context.Context, sha string) (store.Upload, error) {
	rows, _ := s.tx.Query(ctx, `SELECT `+uploadColumns+` FROM media_upload WHERE sha = $1`, sha)
	u, err := pgx.CollectExactlyOneRow(rows, scanUpload)
	return u, wrapAuth("get upload", err)
}

func (s uploadStore) list(ctx context.Context, query string, args ...any) ([]store.Upload, error) {
	rows, err := s.tx.Query(ctx, query, args...)
	if err != nil {
		return nil, wrapAuth("list uploads", err)
	}
	out, err := pgx.CollectRows(rows, scanUpload)
	return out, wrapAuth("list uploads", err)
}

func (s uploadStore) List(ctx context.Context, after string, limit int) ([]store.Upload, error) {
	return s.list(ctx, `SELECT `+uploadColumns+` FROM media_upload WHERE sha > $1 ORDER BY sha LIMIT $2`, after, max(limit, 1))
}

func (s uploadStore) ListStale(ctx context.Context, before time.Time, after string, limit int) ([]store.Upload, error) {
	return s.list(ctx, `SELECT `+uploadColumns+` FROM media_upload WHERE sha > $1 AND touched_at < $2 ORDER BY sha LIMIT $3`,
		after, tz(before), max(limit, 1))
}

func (s uploadStore) Touch(ctx context.Context, sha string, at time.Time) (bool, error) {
	n, err := s.exec(ctx, "touch upload", `UPDATE media_upload SET touched_at = greatest(touched_at, $2) WHERE sha = $1`, sha, tz(at))
	return n > 0, err
}

func (s uploadStore) DeleteStale(ctx context.Context, sha string, before time.Time) (bool, error) {
	n, err := s.exec(ctx, "delete stale upload", `DELETE FROM media_upload WHERE sha = $1 AND touched_at < $2`, sha, tz(before))
	return n > 0, err
}

func (s uploadStore) Delete(ctx context.Context, sha string) (bool, error) {
	n, err := s.exec(ctx, "delete upload", `DELETE FROM media_upload WHERE sha = $1`, sha)
	return n > 0, err
}
