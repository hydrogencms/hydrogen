package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

type reviewStore struct{ authStore }

func (t *tx) Reviews() store.ReviewStore { return reviewStore{authStore{t.tx}} }

const reviewColumns = `item_id, state, draft_hash, submitted_by, submitted_at, reviewers, decided_by, decided_at, updated_at`

type reviewRow struct {
	store.ItemReview
	typ string
}

func scanReview(r pgx.CollectableRow) (store.ItemReview, error) {
	x, err := scanReviewRow(r, false)
	return x.ItemReview, err
}

func scanReviewRow(r pgx.CollectableRow, withType bool) (reviewRow, error) {
	var x reviewRow
	var id [16]byte
	var rv []byte
	dest := []any{&id, &x.State, &x.DraftHash, &x.SubmittedBy, &x.SubmittedAt, &rv, &x.DecidedBy, &x.DecidedAt, &x.UpdatedAt}
	if withType {
		dest = append(dest, &x.typ)
	}
	if err := r.Scan(dest...); err != nil {
		return x, err
	}
	x.Item = id
	if err := json.Unmarshal(rv, &x.Reviewers); err != nil {
		return x, fmt.Errorf("review reviewers: %w", err)
	}
	if len(x.Reviewers) == 0 {
		x.Reviewers = nil
	}
	x.SubmittedAt, x.DecidedAt, x.UpdatedAt = optUTC(x.SubmittedAt), optUTC(x.DecidedAt), utc(x.UpdatedAt)
	return x, nil
}

func reviewersJSON(l []string) (string, error) {
	b, err := json.Marshal(append([]string{}, l...))
	return string(b), err
}

func (r reviewStore) Get(ctx context.Context, item ids.UUID) (store.ItemReview, error) {
	rows, _ := r.tx.Query(ctx, `SELECT `+reviewColumns+` FROM item_review WHERE item_id = $1`, uid(item))
	x, err := pgx.CollectExactlyOneRow(rows, scanReview)
	return x, wrapAuth("get review", err)
}

// Put inserts only when the item exists (a foreign key violation would abort the transaction).
func (r reviewStore) Put(ctx context.Context, x store.ItemReview) error {
	rv, err := reviewersJSON(x.Reviewers)
	if err != nil {
		return err
	}
	n, err := r.exec(ctx, "put review", `INSERT INTO item_review (`+reviewColumns+`)
		SELECT $1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9 WHERE EXISTS (SELECT 1 FROM item WHERE id = $1)
		ON CONFLICT (item_id) DO UPDATE SET state = excluded.state, draft_hash = excluded.draft_hash,
			submitted_by = excluded.submitted_by, submitted_at = excluded.submitted_at, reviewers = excluded.reviewers,
			decided_by = excluded.decided_by, decided_at = excluded.decided_at, updated_at = excluded.updated_at`,
		uid(x.Item), x.State, x.DraftHash, x.SubmittedBy, optTz(x.SubmittedAt), rv, x.DecidedBy, optTz(x.DecidedAt), tz(x.UpdatedAt))
	if err == nil && n == 0 {
		return fmt.Errorf("postgres: put review: item missing: %w", store.ErrConstraint)
	}
	return err
}

func (r reviewStore) Transition(ctx context.Context, x store.ItemReview, from []string) error {
	rv, err := reviewersJSON(x.Reviewers)
	if err != nil {
		return err
	}
	n, err := r.exec(ctx, "transition review", `UPDATE item_review SET state = $2, draft_hash = $3, submitted_by = $4,
			submitted_at = $5, reviewers = $6::jsonb, decided_by = $7, decided_at = $8, updated_at = $9
		WHERE item_id = $1 AND state = ANY($10)`, uid(x.Item), x.State, x.DraftHash, x.SubmittedBy, optTz(x.SubmittedAt), rv,
		x.DecidedBy, optTz(x.DecidedAt), tz(x.UpdatedAt), from)
	if err != nil || n > 0 {
		return err
	}
	var exists bool
	if err := r.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM item_review WHERE item_id = $1)`, uid(x.Item)).Scan(&exists); err != nil {
		return wrapAuth("transition review", err)
	}
	if !exists {
		return store.ErrNotFound
	}
	return store.ErrStateChanged
}

func (r reviewStore) Delete(ctx context.Context, item ids.UUID) error {
	_, err := r.exec(ctx, "delete review", `DELETE FROM item_review WHERE item_id = $1`, uid(item))
	return err
}

func (r reviewStore) AddEvent(ctx context.Context, e store.ReviewEvent) error {
	var data any
	if len(e.Data) > 0 {
		data = string(e.Data)
	}
	n, err := r.exec(ctx, "add review event", `INSERT INTO review_event (id, item_id, kind, comment, actor, at, data)
		SELECT $1, $2, $3, $4, $5, $6, $7::jsonb WHERE EXISTS (SELECT 1 FROM item WHERE id = $2)
		ON CONFLICT (id) DO NOTHING`, uid(e.ID), uid(e.Item), e.Kind, e.Comment, e.Actor, tz(e.At), data)
	if err != nil || n > 0 {
		return err
	}
	var taken bool
	if err := r.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM review_event WHERE id = $1)`, uid(e.ID)).Scan(&taken); err != nil {
		return wrapAuth("add review event", err)
	}
	if taken {
		return store.ErrDuplicateKey
	}
	return fmt.Errorf("postgres: add review event: item missing: %w", store.ErrConstraint)
}

func (r reviewStore) Events(ctx context.Context, item ids.UUID, limit int, before ids.UUID) ([]store.ReviewEvent, error) {
	switch {
	case limit <= 0:
		limit = 50
	case limit > 500:
		limit = 500
	}
	q := `SELECT id, item_id, kind, comment, actor, at, data FROM review_event WHERE item_id = $1`
	args := []any{uid(item)}
	if before != (ids.UUID{}) {
		var exists bool
		if err := r.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM review_event WHERE id = $1 AND item_id = $2)`, uid(before), uid(item)).Scan(&exists); err != nil {
			return nil, wrapAuth("review events", err)
		}
		if !exists {
			return nil, store.ErrNotFound
		}
		q += ` AND (at, id) < (SELECT at, id FROM review_event WHERE id = $2)`
		args = append(args, uid(before))
	}
	rows, err := r.tx.Query(ctx, q+` ORDER BY at DESC, id DESC LIMIT $`+strconv.Itoa(len(args)+1), append(args, limit)...)
	if err != nil {
		return nil, wrapAuth("review events", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (store.ReviewEvent, error) {
		var e store.ReviewEvent
		var id, it [16]byte
		var data []byte
		if err := row.Scan(&id, &it, &e.Kind, &e.Comment, &e.Actor, &e.At, &data); err != nil {
			return e, err
		}
		e.ID, e.Item, e.At = id, it, utc(e.At)
		if len(data) > 0 {
			e.Data = json.RawMessage(data)
		}
		return e, nil
	})
	return out, wrapAuth("review events", err)
}

func (r reviewStore) List(ctx context.Context, f store.ReviewFilter, p store.Page) ([]store.ReviewRow, int, error) {
	conds := []string{"item.deleted_at IS NULL"}
	var args []any
	bind := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if len(f.States) > 0 {
		conds = append(conds, "item_review.state = ANY("+bind(f.States)+")")
	}
	if len(f.Types) > 0 {
		conds = append(conds, "item.type = ANY("+bind(f.Types)+")")
	}
	if len(f.Reviewers) > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM jsonb_array_elements_text(item_review.reviewers) v WHERE v = ANY("+bind(f.Reviewers)+"))")
	}
	from := ` FROM item_review JOIN item ON item.id = item_review.item_id WHERE ` + strings.Join(conds, " AND ")
	cols := "item_review." + strings.ReplaceAll(reviewColumns, ", ", ", item_review.")
	rows, total, err := listPage(ctx, r.authStore, "list reviews", `SELECT count(*)`+from,
		`SELECT `+cols+`, item.type`+from+` ORDER BY item_review.submitted_at DESC NULLS LAST, item_review.item_id DESC`, p,
		func(row pgx.CollectableRow) (store.ReviewRow, error) {
			x, err := scanReviewRow(row, true)
			return store.ReviewRow{ItemReview: x.ItemReview, Type: x.typ}, err
		}, args...)
	return rows, total, err
}
