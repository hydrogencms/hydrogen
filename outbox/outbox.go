// Package outbox publishes events in the same transaction as the change that caused them and delivers them to
// consumers after commit, at least once. Consumers must be idempotent.
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/store"
)

// NotifyChannel wakes outbox workers after a commit that published events.
const NotifyChannel = "hydrogen_outbox"

type Event struct {
	ID        int64
	Topic     string
	Payload   json.RawMessage
	CreatedAt time.Time
	Attempt   int
}

type Consumer struct {
	Name string
	// Topics: exact names ("item.published"), prefixes ("item.*"), or "*".
	Topics      []string
	Handle      func(ctx context.Context, e Event) error
	MaxAttempts int // default 10, then the delivery fails for good
}

type Options struct {
	Poll  time.Duration // fallback polling interval, default 1s
	Lease time.Duration // how long a claimed delivery stays hidden from other workers, default 1m
	Batch int           // default 50
}

type Outbox struct {
	db        store.DB
	clock     clock.Clock
	log       *slog.Logger
	consumers []Consumer
	opts      Options
}

func New(db store.DB, c clock.Clock, log *slog.Logger, consumers []Consumer, o Options) (*Outbox, error) {
	if o.Poll <= 0 {
		o.Poll = time.Second
	}
	if o.Lease <= 0 {
		o.Lease = time.Minute
	}
	if o.Batch <= 0 {
		o.Batch = 50
	}
	consumers = slices.Clone(consumers)
	slices.SortFunc(consumers, func(a, b Consumer) int { return strings.Compare(a.Name, b.Name) })
	for i, cn := range consumers {
		if cn.Name == "" || cn.Handle == nil || len(cn.Topics) == 0 {
			return nil, fmt.Errorf("outbox: consumer %q needs a name, topics and a handler", cn.Name)
		}
		if i > 0 && consumers[i-1].Name == cn.Name {
			return nil, fmt.Errorf("outbox: duplicate consumer %q", cn.Name)
		}
		if cn.MaxAttempts <= 0 {
			consumers[i].MaxAttempts = 10
		}
	}
	return &Outbox{db: db, clock: c, log: log, consumers: consumers, opts: o}, nil
}

func matches(topics []string, topic string) bool {
	for _, t := range topics {
		if t == "*" || t == topic || (strings.HasSuffix(t, ".*") && strings.HasPrefix(topic, t[:len(t)-1])) {
			return true
		}
	}
	return false
}

// Publish records an event inside tx. payload is encoded as JSON.
func (o *Outbox) Publish(ctx context.Context, tx store.Tx, topic string, payload any) (int64, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("outbox: encode %s: %w", topic, err)
	}
	var names []string
	for _, cn := range o.consumers {
		if matches(cn.Topics, topic) {
			names = append(names, cn.Name)
		}
	}
	id, err := tx.Outbox().Append(ctx, topic, b, names, o.clock.Now())
	if err != nil {
		return 0, err
	}
	if len(names) > 0 {
		if err := tx.Notify(NotifyChannel, ""); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// Run delivers events until ctx ends. Every consumer runs its own loop (claim, deliver, wake on notification
// or poll), so a slow consumer, such as a webhook timing out, never delays the others.
func (o *Outbox) Run(ctx context.Context) error {
	if len(o.consumers) == 0 {
		<-ctx.Done()
		return nil
	}
	wakes := make([]<-chan store.Notification, len(o.consumers))
	for i := range o.consumers {
		w, err := o.db.Notifier().Listen(ctx, NotifyChannel)
		if err != nil {
			return err
		}
		wakes[i] = w
	}
	var wg sync.WaitGroup
	for i, cn := range o.consumers {
		wg.Go(func() { o.runConsumer(ctx, cn, wakes[i]) })
	}
	wg.Wait()
	return nil
}

func (o *Outbox) runConsumer(ctx context.Context, cn Consumer, wake <-chan store.Notification) {
	tick := time.NewTicker(o.opts.Poll)
	defer tick.Stop()
	for {
		if err := o.drainConsumer(ctx, cn, new(int)); err != nil && ctx.Err() == nil {
			o.log.Error("outbox: delivery round failed", "consumer", cn.Name, "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-wake:
		case <-tick.C:
		}
	}
}

// Drain delivers every due event once and returns how many deliveries ran.
func (o *Outbox) Drain(ctx context.Context) (int, error) {
	total := 0
	for _, cn := range o.consumers {
		if err := o.drainConsumer(ctx, cn, &total); err != nil {
			return total, err
		}
	}
	return total, nil
}

// drainConsumer delivers the consumer's due events until a round comes back short, adding to *total.
func (o *Outbox) drainConsumer(ctx context.Context, cn Consumer, total *int) error {
	for {
		n, err := o.round(ctx, cn)
		*total += n
		if err != nil {
			return err
		}
		if n < o.opts.Batch {
			return nil
		}
	}
}

func (o *Outbox) round(ctx context.Context, cn Consumer) (int, error) {
	var ds []store.Delivery
	err := o.db.Tx(ctx, func(tx store.Tx) error {
		var err error
		ds, err = tx.Outbox().Claim(ctx, cn.Name, o.clock.Now(), o.opts.Lease, o.opts.Batch)
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, d := range ds {
		herr := o.handle(ctx, cn, d)
		err := o.db.Tx(ctx, func(tx store.Tx) error {
			now := o.clock.Now()
			switch {
			case herr == nil:
				return tx.Outbox().Complete(ctx, cn.Name, d.Event.ID, now)
			case d.Attempts >= cn.MaxAttempts:
				o.log.Error("outbox: giving up", "consumer", cn.Name, "event", d.Event.ID, "topic", d.Event.Topic, "err", herr)
				return tx.Outbox().Fail(ctx, cn.Name, d.Event.ID, now, herr.Error())
			default:
				o.log.Warn("outbox: will retry", "consumer", cn.Name, "event", d.Event.ID, "attempt", d.Attempts, "err", herr)
				return tx.Outbox().Retry(ctx, cn.Name, d.Event.ID, now.Add(Backoff(d.Attempts)), herr.Error())
			}
		})
		if err != nil {
			return 0, err
		}
	}
	return len(ds), nil
}

func (o *Outbox) handle(ctx context.Context, cn Consumer, d store.Delivery) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	err = cn.Handle(ctx, Event{ID: d.Event.ID, Topic: d.Event.Topic, Payload: d.Event.Payload,
		CreatedAt: d.Event.CreatedAt, Attempt: d.Attempts})
	if err == nil && ctx.Err() != nil {
		err = errors.New("canceled")
	}
	return err
}

// Backoff is the delay before retry n (1-based): 1s, 2s, 4s… capped at 5 minutes.
func Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 9 {
		return 5 * time.Minute
	}
	return min(time.Second<<(attempt-1), 5*time.Minute)
}
