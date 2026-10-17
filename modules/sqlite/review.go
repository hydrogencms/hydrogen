package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

type reviewStore struct{ authStore }

func (t *tx) Reviews() store.ReviewStore { return reviewStore{authStore{t.tx}} }

const reviewColumns = `item_id, state, draft_hash, submitted_by, submitted_at, reviewers, decided_by, decided_at, updated_at`

func scanReview(r scanner) (store.ItemReview, error) {
	var (
		x         store.ItemReview
		id        []byte
		sub, dec  sql.NullInt64
		reviewers string
		updated   int64
	)
	if err := r.Scan(&id, &x.State, &x.DraftHash, &x.SubmittedBy, &sub, &reviewers, &x.DecidedBy, &dec, &updated); err != nil {
		return x, err
	}
	var err error
	if x.Item, err = toUUID(id); err != nil {
		return x, err
	}
	if err := json.Unmarshal([]byte(reviewers), &x.Reviewers); err != nil {
		return x, fmt.Errorf("sqlite: review reviewers: %w", err)
	}
	if len(x.Reviewers) == 0 {
		x.Reviewers = nil
	}
	x.SubmittedAt, x.DecidedAt, x.UpdatedAt = fromOptMs(sub), fromOptMs(dec), fromMs(updated)
	return x, nil
}

func reviewersJSON(l []string) (string, error) {
	b, err := json.Marshal(append([]string{}, l...))
	return string(b), err
}

func (r reviewStore) Get(ctx context.Context, item ids.UUID) (store.ItemReview, error) {
	x, err := scanReview(r.tx.QueryRowContext(ctx, `SELECT `+reviewColumns+` FROM item_review WHERE item_id = ?`, uid(item)))
	return x, wrapAuth("get review", err)
}

func (r reviewStore) Put(ctx context.Context, x store.ItemReview) error {
	rv, err := reviewersJSON(x.Reviewers)
	if err != nil {
		return err
	}
	_, err = r.exec(ctx, "put review", `INSERT INTO item_review (`+reviewColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET state = excluded.state, draft_hash = excluded.draft_hash,
			submitted_by = excluded.submitted_by, submitted_at = excluded.submitted_at, reviewers = excluded.reviewers,
			decided_by = excluded.decided_by, decided_at = excluded.decided_at, updated_at = excluded.updated_at`,
		uid(x.Item), x.State, x.DraftHash, x.SubmittedBy, optMs(x.SubmittedAt), rv, x.DecidedBy, optMs(x.DecidedAt), ms(x.UpdatedAt))
	return err
}

func (r reviewStore) Transition(ctx context.Context, x store.ItemReview, from []string) error {
	rv, err := reviewersJSON(x.Reviewers)
	if err != nil {
		return err
	}
	args := []any{x.State, x.DraftHash, x.SubmittedBy, optMs(x.SubmittedAt), rv, x.DecidedBy, optMs(x.DecidedAt), ms(x.UpdatedAt), uid(x.Item)}
	for _, f := range from {
		args = append(args, f)
	}
	n, err := r.exec(ctx, "transition review", `UPDATE item_review SET state = ?, draft_hash = ?, submitted_by = ?,
			submitted_at = ?, reviewers = ?, decided_by = ?, decided_at = ?, updated_at = ?
		WHERE item_id = ? AND state IN (`+placeholders(len(from))+`)`, args...)
	if err != nil || n > 0 {
		return err
	}
	var one int
	if err := r.tx.QueryRowContext(ctx, `SELECT 1 FROM item_review WHERE item_id = ?`, uid(x.Item)).Scan(&one); err != nil {
		return wrapAuth("transition review", err)
	}
	return store.ErrStateChanged
}

func (r reviewStore) Delete(ctx context.Context, item ids.UUID) error {
	_, err := r.exec(ctx, "delete review", `DELETE FROM item_review WHERE item_id = ?`, uid(item))
	return err
}

func (r reviewStore) AddEvent(ctx context.Context, e store.ReviewEvent) error {
	var data any
	if len(e.Data) > 0 {
		data = string(e.Data)
	}
	_, err := r.exec(ctx, "add review event", `INSERT INTO review_event (id, item_id, kind, comment, actor, at, data)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, uid(e.ID), uid(e.Item), e.Kind, e.Comment, e.Actor, ms(e.At), data)
	return err
}

func (r reviewStore) Events(ctx context.Context, item ids.UUID, limit int, before ids.UUID) ([]store.ReviewEvent, error) {
	limit = eventLimit(limit)
	q := `SELECT id, item_id, kind, comment, actor, at, data FROM review_event WHERE item_id = ?`
	args := []any{uid(item)}
	if before != (ids.UUID{}) {
		var at int64
		if err := r.tx.QueryRowContext(ctx, `SELECT at FROM review_event WHERE id = ? AND item_id = ?`, uid(before), uid(item)).Scan(&at); err != nil {
			return nil, wrapAuth("review events", err)
		}
		q += ` AND (at < ? OR (at = ? AND id < ?))`
		args = append(args, at, at, uid(before))
	}
	rows, err := r.tx.QueryContext(ctx, q+` ORDER BY at DESC, id DESC LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, wrapAuth("review events", err)
	}
	defer rows.Close()
	var out []store.ReviewEvent
	for rows.Next() {
		var (
			e      store.ReviewEvent
			id, it []byte
			at     int64
			data   sql.NullString
		)
		if err := rows.Scan(&id, &it, &e.Kind, &e.Comment, &e.Actor, &at, &data); err != nil {
			return nil, wrapAuth("review events", err)
		}
		if e.ID, err = toUUID(id); err != nil {
			return nil, err
		}
		if e.Item, err = toUUID(it); err != nil {
			return nil, err
		}
		e.At = fromMs(at)
		if data.Valid {
			e.Data = json.RawMessage(data.String)
		}
		out = append(out, e)
	}
	return out, wrapAuth("review events", rows.Err())
}

func eventLimit(n int) int {
	switch {
	case n <= 0:
		return 50
	case n > 500:
		return 500
	}
	return n
}

func (r reviewStore) List(ctx context.Context, f store.ReviewFilter, p store.Page) ([]store.ReviewRow, int, error) {
	conds := []string{"item.deleted_at IS NULL"}
	var args []any
	strs := func(col string, l []string) {
		conds = append(conds, col+" IN ("+placeholders(len(l))+")")
		for _, s := range l {
			args = append(args, s)
		}
	}
	if len(f.States) > 0 {
		strs("item_review.state", f.States)
	}
	if len(f.Types) > 0 {
		strs("item.type", f.Types)
	}
	if len(f.Reviewers) > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM json_each(item_review.reviewers) WHERE value IN ("+placeholders(len(f.Reviewers))+"))")
		for _, s := range f.Reviewers {
			args = append(args, s)
		}
	}
	from := ` FROM item_review JOIN item ON item.id = item_review.item_id WHERE ` + strings.Join(conds, " AND ")
	var out []store.ReviewRow
	total, err := r.list(ctx, "list reviews", `SELECT count(*)`+from,
		`SELECT `+qualified("item_review", reviewColumns)+`, item.type`+from+` ORDER BY item_review.submitted_at DESC NULLS LAST, item_review.item_id DESC`,
		storeutil.NormalizePage(p), args, func(s scanner) error {
			var typ string
			var x store.ItemReview
			var err error
			x, err = scanReviewWith(s, &typ)
			out = append(out, store.ReviewRow{ItemReview: x, Type: typ})
			return err
		})
	return out, total, err
}

func qualified(table, cols string) string {
	parts := strings.Split(cols, ",")
	for i, c := range parts {
		parts[i] = table + "." + strings.TrimSpace(c)
	}
	return strings.Join(parts, ", ")
}

// scanReviewWith scans a review row followed by one extra string column.
func scanReviewWith(s scanner, extra *string) (store.ItemReview, error) {
	return scanReview(extraScanner{s, extra})
}

type extraScanner struct {
	scanner
	extra *string
}

func (e extraScanner) Scan(dest ...any) error { return e.scanner.Scan(append(dest, e.extra)...) }
