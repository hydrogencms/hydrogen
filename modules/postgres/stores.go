package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/store"
)

type outboxStore struct{ tx pgx.Tx }

func (o outboxStore) Append(ctx context.Context, topic string, payload []byte, consumers []string, at time.Time) (int64, error) {
	var id int64
	err := o.tx.QueryRow(ctx, `INSERT INTO outbox_event (topic, payload, created_at) VALUES ($1, $2, $3) RETURNING id`,
		topic, payload, at).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: outbox append: %w", err)
	}
	if len(consumers) > 0 {
		if _, err := o.tx.Exec(ctx, `INSERT INTO outbox_delivery (event_id, consumer, available_at)
			SELECT $1, c, $2 FROM unnest($3::text[]) AS c`, id, at, consumers); err != nil {
			return 0, fmt.Errorf("postgres: outbox delivery: %w", err)
		}
	}
	return id, nil
}

func (o outboxStore) Claim(ctx context.Context, consumer string, now time.Time, lease time.Duration, limit int) ([]store.Delivery, error) {
	rows, err := o.tx.Query(ctx, `
		WITH due AS (
			SELECT event_id FROM outbox_delivery
			WHERE consumer = $1 AND done_at IS NULL AND failed_at IS NULL AND available_at <= $2
			ORDER BY event_id LIMIT $3
			FOR UPDATE SKIP LOCKED
		), claimed AS (
			UPDATE outbox_delivery d SET attempts = d.attempts + 1, available_at = $4
			FROM due WHERE d.consumer = $1 AND d.event_id = due.event_id
			RETURNING d.event_id, d.attempts
		)
		SELECT e.id, e.topic, e.payload, e.created_at, claimed.attempts
		FROM claimed JOIN outbox_event e ON e.id = claimed.event_id
		ORDER BY e.id`, consumer, now, limit, now.Add(lease))
	if err != nil {
		return nil, fmt.Errorf("postgres: outbox claim: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Delivery, error) {
		d := store.Delivery{Consumer: consumer}
		err := r.Scan(&d.Event.ID, &d.Event.Topic, &d.Event.Payload, &d.Event.CreatedAt, &d.Attempts)
		d.Event.CreatedAt = d.Event.CreatedAt.UTC()
		return d, err
	})
}

func (o outboxStore) update(ctx context.Context, query string, args ...any) error {
	tag, err := o.tx.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("postgres: outbox: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("postgres: outbox: delivery not found")
	}
	return nil
}

func (o outboxStore) Complete(ctx context.Context, consumer string, eventID int64, at time.Time) error {
	return o.update(ctx, `UPDATE outbox_delivery SET done_at = $1 WHERE consumer = $2 AND event_id = $3`, at, consumer, eventID)
}

func (o outboxStore) Retry(ctx context.Context, consumer string, eventID int64, retryAt time.Time, reason string) error {
	return o.update(ctx, `UPDATE outbox_delivery SET available_at = $1, last_error = $2 WHERE consumer = $3 AND event_id = $4`,
		retryAt, reason, consumer, eventID)
}

func (o outboxStore) Fail(ctx context.Context, consumer string, eventID int64, at time.Time, reason string) error {
	return o.update(ctx, `UPDATE outbox_delivery SET failed_at = $1, last_error = $2 WHERE consumer = $3 AND event_id = $4`,
		at, reason, consumer, eventID)
}

func (o outboxStore) Stats(ctx context.Context, consumer string) (store.OutboxStats, error) {
	var s store.OutboxStats
	err := o.tx.QueryRow(ctx, `SELECT
			count(*) FILTER (WHERE done_at IS NULL AND failed_at IS NULL),
			count(*) FILTER (WHERE done_at IS NOT NULL),
			count(*) FILTER (WHERE failed_at IS NOT NULL)
		FROM outbox_delivery WHERE consumer = $1`, consumer).Scan(&s.Pending, &s.Done, &s.Failed)
	return s, err
}

type auditStore struct{ tx pgx.Tx }

func (a auditStore) Write(ctx context.Context, e store.AuditEvent) (int64, error) {
	var id int64
	err := a.tx.QueryRow(ctx, `INSERT INTO audit_event (at, actor_kind, actor_id, action, subject, data)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, e.At, e.ActorKind, e.ActorID, e.Action, e.Subject, e.Data).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("postgres: audit write: %w", err)
	}
	return id, nil
}

func (a auditStore) List(ctx context.Context, q store.AuditQuery) ([]store.AuditEvent, error) {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	rows, err := a.tx.Query(ctx, `SELECT id, at, actor_kind, actor_id, action, subject, data FROM audit_event
		WHERE ($1 = '' OR subject = $1) AND ($2 = '' OR action = $2) ORDER BY id DESC LIMIT $3`,
		q.Subject, q.Action, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: audit list: %w", err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.AuditEvent, error) {
		var e store.AuditEvent
		err := r.Scan(&e.ID, &e.At, &e.ActorKind, &e.ActorID, &e.Action, &e.Subject, &e.Data)
		e.At = e.At.UTC()
		return e, err
	})
}
