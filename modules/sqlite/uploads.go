package sqlite

import (
	"context"
	"time"

	"github.com/hydrogencms/hydrogen/core/store"
)

type uploadStore struct{ authStore }

func (t *tx) Uploads() store.UploadStore { return uploadStore{authStore{t.tx}} }

const uploadColumns = `sha, mime, size, width, height, created_by, created_at, touched_at`

func scanUpload(r scanner) (store.Upload, error) {
	var (
		u              store.Upload
		created, touch int64
	)
	if err := r.Scan(&u.SHA, &u.MIME, &u.Size, &u.Width, &u.Height, &u.CreatedBy, &created, &touch); err != nil {
		return u, err
	}
	u.CreatedAt, u.TouchedAt = fromMs(created), fromMs(touch)
	return u, nil
}

func (s uploadStore) Put(ctx context.Context, u store.Upload) (bool, error) {
	n, err := s.exec(ctx, "put upload", `INSERT INTO media_upload (`+uploadColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (sha) DO NOTHING`,
		u.SHA, u.MIME, u.Size, u.Width, u.Height, u.CreatedBy, ms(u.CreatedAt), ms(u.TouchedAt))
	if err != nil {
		return false, err
	}
	if n == 1 {
		return true, nil
	}
	_, err = s.exec(ctx, "touch upload", `UPDATE media_upload SET touched_at = max(touched_at, ?) WHERE sha = ?`, ms(u.TouchedAt), u.SHA)
	return false, err
}

func (s uploadStore) Get(ctx context.Context, sha string) (store.Upload, error) {
	u, err := scanUpload(s.tx.QueryRowContext(ctx, `SELECT `+uploadColumns+` FROM media_upload WHERE sha = ?`, sha))
	return u, wrapAuth("get upload", err)
}

func (s uploadStore) list(ctx context.Context, where string, args []any, limit int) ([]store.Upload, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT `+uploadColumns+` FROM media_upload WHERE `+where+` ORDER BY sha LIMIT ?`,
		append(args, max(limit, 1))...)
	if err != nil {
		return nil, wrapAuth("list uploads", err)
	}
	defer rows.Close()
	var out []store.Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, wrapAuth("list uploads", err)
		}
		out = append(out, u)
	}
	return out, wrapAuth("list uploads", rows.Err())
}

func (s uploadStore) List(ctx context.Context, after string, limit int) ([]store.Upload, error) {
	return s.list(ctx, `sha > ?`, []any{after}, limit)
}

func (s uploadStore) ListStale(ctx context.Context, before time.Time, after string, limit int) ([]store.Upload, error) {
	return s.list(ctx, `sha > ? AND touched_at < ?`, []any{after, ms(before)}, limit)
}

func (s uploadStore) Touch(ctx context.Context, sha string, at time.Time) (bool, error) {
	n, err := s.exec(ctx, "touch upload", `UPDATE media_upload SET touched_at = max(touched_at, ?) WHERE sha = ?`, ms(at), sha)
	return n > 0, err
}

func (s uploadStore) DeleteStale(ctx context.Context, sha string, before time.Time) (bool, error) {
	n, err := s.exec(ctx, "delete stale upload", `DELETE FROM media_upload WHERE sha = ? AND touched_at < ?`, sha, ms(before))
	return n > 0, err
}

func (s uploadStore) Delete(ctx context.Context, sha string) (bool, error) {
	n, err := s.exec(ctx, "delete upload", `DELETE FROM media_upload WHERE sha = ?`, sha)
	return n > 0, err
}
