package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/store"
)

type outboxStore struct{ tx *sql.Tx }

func (o outboxStore) Append(ctx context.Context, topic string, payload []byte, consumers []string, at time.Time) (int64, error) {
	var id int64
	err := o.tx.QueryRowContext(ctx, `INSERT INTO outbox_event (topic, payload, created_at) VALUES (?, ?, ?) RETURNING id`,
		topic, payload, ms(at)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("sqlite: outbox append: %w", err)
	}
	for _, c := range consumers {
		if _, err := o.tx.ExecContext(ctx, `INSERT INTO outbox_delivery (event_id, consumer, available_at) VALUES (?, ?, ?)`,
			id, c, ms(at)); err != nil {
			return 0, fmt.Errorf("sqlite: outbox delivery: %w", err)
		}
	}
	return id, nil
}

func (o outboxStore) Claim(ctx context.Context, consumer string, now time.Time, lease time.Duration, limit int) ([]store.Delivery, error) {
	rows, err := o.tx.QueryContext(ctx, `
		UPDATE outbox_delivery SET attempts = attempts + 1, available_at = ?
		WHERE consumer = ? AND event_id IN (
			SELECT event_id FROM outbox_delivery
			WHERE consumer = ? AND done_at IS NULL AND failed_at IS NULL AND available_at <= ?
			ORDER BY event_id LIMIT ?)
		RETURNING event_id, attempts`, ms(now.Add(lease)), consumer, consumer, ms(now), limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: outbox claim: %w", err)
	}
	attempts := map[int64]int{}
	var ids []any
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		attempts[id] = n
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return nil, err
	}
	rows, err = o.tx.QueryContext(ctx, `SELECT id, topic, payload, created_at FROM outbox_event WHERE id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`) ORDER BY id`, ids...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.Delivery
	for rows.Next() {
		var e store.OutboxEvent
		var created int64
		if err := rows.Scan(&e.ID, &e.Topic, &e.Payload, &created); err != nil {
			return nil, err
		}
		e.CreatedAt = fromMs(created)
		out = append(out, store.Delivery{Event: e, Consumer: consumer, Attempts: attempts[e.ID]})
	}
	slices.SortFunc(out, func(a, b store.Delivery) int { return int(a.Event.ID - b.Event.ID) })
	return out, rows.Err()
}

func (o outboxStore) update(ctx context.Context, query string, args ...any) error {
	res, err := o.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sqlite: outbox: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("sqlite: outbox: delivery not found")
	}
	return nil
}

func (o outboxStore) Complete(ctx context.Context, consumer string, eventID int64, at time.Time) error {
	return o.update(ctx, `UPDATE outbox_delivery SET done_at = ? WHERE consumer = ? AND event_id = ?`, ms(at), consumer, eventID)
}

func (o outboxStore) Retry(ctx context.Context, consumer string, eventID int64, retryAt time.Time, reason string) error {
	return o.update(ctx, `UPDATE outbox_delivery SET available_at = ?, last_error = ? WHERE consumer = ? AND event_id = ?`,
		ms(retryAt), reason, consumer, eventID)
}

func (o outboxStore) Fail(ctx context.Context, consumer string, eventID int64, at time.Time, reason string) error {
	return o.update(ctx, `UPDATE outbox_delivery SET failed_at = ?, last_error = ? WHERE consumer = ? AND event_id = ?`,
		ms(at), reason, consumer, eventID)
}

func (o outboxStore) Stats(ctx context.Context, consumer string) (store.OutboxStats, error) {
	var s store.OutboxStats
	err := o.tx.QueryRowContext(ctx, `SELECT
			COALESCE(SUM(CASE WHEN done_at IS NULL AND failed_at IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN done_at IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN failed_at IS NOT NULL THEN 1 ELSE 0 END), 0)
		FROM outbox_delivery WHERE consumer = ?`, consumer).Scan(&s.Pending, &s.Done, &s.Failed)
	return s, err
}

type auditStore struct{ tx *sql.Tx }

func (a auditStore) Write(ctx context.Context, e store.AuditEvent) (int64, error) {
	var id int64
	err := a.tx.QueryRowContext(ctx, `INSERT INTO audit_event (at, actor_kind, actor_id, action, subject, data)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`, ms(e.At), e.ActorKind, e.ActorID, e.Action, e.Subject, e.Data).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("sqlite: audit write: %w", err)
	}
	return id, nil
}

func (a auditStore) List(ctx context.Context, q store.AuditQuery) ([]store.AuditEvent, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	rows, err := a.tx.QueryContext(ctx, `SELECT id, at, actor_kind, actor_id, action, subject, data FROM audit_event
		WHERE (? = '' OR subject = ?) AND (? = '' OR action = ?) ORDER BY id DESC LIMIT ?`,
		q.Subject, q.Subject, q.Action, q.Action, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: audit list: %w", err)
	}
	defer rows.Close()
	var out []store.AuditEvent
	for rows.Next() {
		var e store.AuditEvent
		var at int64
		if err := rows.Scan(&e.ID, &at, &e.ActorKind, &e.ActorID, &e.Action, &e.Subject, &e.Data); err != nil {
			return nil, err
		}
		e.At = fromMs(at)
		out = append(out, e)
	}
	return out, rows.Err()
}
