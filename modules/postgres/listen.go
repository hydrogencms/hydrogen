package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"

	"github.com/hydrogencms/hydrogen/core/store"
)

// listener holds one connection that LISTENs on every channel any subscriber asked for and fans notifications
// out. Listen returns only once the channel is LISTENed, so a commit right after Listen is not missed. After a
// reconnect every subscriber gets an empty-payload notification, because messages sent meanwhile are lost. A
// subscriber whose buffer is full is marked and gets an empty-payload notification as soon as there is room
// (later notifications are covered by it, so they are not queued meanwhile): notifications are never
// dropped silently.
type subscriber struct {
	ch       chan store.Notification
	channel  string
	overflow bool // an empty-payload notification is owed
	closed   bool
}

type listener struct {
	ctx context.Context
	cfg *pgx.ConnConfig
	log *slog.Logger
	wg  *sync.WaitGroup

	mu      sync.Mutex
	subs    map[string]map[*subscriber]struct{}
	waiters []chan struct{}
	wake    context.CancelFunc
	running bool
}

func newListener(ctx context.Context, cfg *pgx.ConnConfig, log *slog.Logger, wg *sync.WaitGroup) *listener {
	cfg = cfg.Copy()
	// Interrupt waits with a read deadline instead of a cancel request, so waking the loop keeps the connection.
	cfg.BuildContextWatcherHandler = func(pc *pgconn.PgConn) ctxwatch.Handler {
		return &pgconn.DeadlineContextWatcherHandler{Conn: pc.Conn()}
	}
	return &listener{ctx: ctx, cfg: cfg, log: log, wg: wg, subs: map[string]map[*subscriber]struct{}{}}
}

func (l *listener) Listen(ctx context.Context, channel string) (<-chan store.Notification, error) {
	sub := &subscriber{ch: make(chan store.Notification, 64), channel: channel}
	ack := make(chan struct{})
	l.mu.Lock()
	if l.subs[channel] == nil {
		l.subs[channel] = map[*subscriber]struct{}{}
	}
	l.subs[channel][sub] = struct{}{}
	l.waiters = append(l.waiters, ack)
	if !l.running {
		l.running = true
		l.wg.Go(l.run)
	} else if l.wake != nil {
		l.wake()
	}
	l.mu.Unlock()

	remove := func() {
		l.mu.Lock()
		delete(l.subs[channel], sub)
		sub.closed = true
		close(sub.ch)
		l.mu.Unlock()
	}
	select {
	case <-ack:
	case <-ctx.Done():
		remove()
		return nil, ctx.Err()
	case <-l.ctx.Done():
		remove()
		return nil, fmt.Errorf("postgres: database closed")
	}
	l.wg.Go(func() {
		select {
		case <-ctx.Done():
		case <-l.ctx.Done():
		}
		remove()
	})
	return sub.ch, nil
}

func (l *listener) run() {
	backoff := 100 * time.Millisecond
	reconnect := false
	for l.ctx.Err() == nil {
		conn, err := pgx.ConnectConfig(l.ctx, l.cfg)
		if err != nil {
			l.log.Warn("postgres: listener connect failed", "err", err, "retry", backoff)
			select {
			case <-time.After(backoff):
			case <-l.ctx.Done():
				return
			}
			backoff = min(backoff*2, 5*time.Second)
			continue
		}
		backoff = 100 * time.Millisecond
		err = l.serve(conn, reconnect)
		_ = conn.Close(context.Background())
		if l.ctx.Err() != nil {
			return
		}
		l.log.Warn("postgres: listener connection lost; reconnecting", "err", err)
		reconnect = true
	}
}

func (l *listener) serve(conn *pgx.Conn, reconnected bool) error {
	listening := map[string]bool{}
	for {
		l.mu.Lock()
		channels := slices.Sorted(maps.Keys(l.subs))
		waiters := l.waiters
		l.waiters = nil
		l.mu.Unlock()

		for _, c := range channels {
			if listening[c] {
				continue
			}
			if _, err := conn.Exec(l.ctx, "LISTEN "+pgx.Identifier{c}.Sanitize()); err != nil {
				l.requeue(waiters)
				return err
			}
			listening[c] = true
		}
		for _, w := range waiters {
			close(w)
		}
		if reconnected {
			reconnected = false
			l.broadcastReconnect()
		}

		wctx, cancel := context.WithCancel(l.ctx)
		l.mu.Lock()
		l.wake = cancel
		pending := len(l.waiters) > 0
		l.mu.Unlock()
		if pending {
			cancel()
		}
		n, err := conn.WaitForNotification(wctx)
		cancel()
		if err != nil {
			if l.ctx.Err() != nil {
				return l.ctx.Err()
			}
			if wctx.Err() != nil && !conn.IsClosed() {
				continue // woken to LISTEN on a new channel
			}
			return err
		}
		l.dispatch(store.Notification{Channel: n.Channel, Payload: n.Payload})
	}
}

func (l *listener) requeue(ws []chan struct{}) {
	l.mu.Lock()
	l.waiters = append(ws, l.waiters...)
	l.mu.Unlock()
}

func (l *listener) dispatch(n store.Notification) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for sub := range l.subs[n.Channel] {
		l.deliver(sub, n)
	}
}

func (l *listener) broadcastReconnect() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for c, subs := range l.subs {
		for sub := range subs {
			l.deliver(sub, store.Notification{Channel: c})
		}
	}
}

// deliver (with l.mu held) hands n to the subscriber without blocking. A full buffer marks the subscriber, and
// flush owes it an empty-payload notification; until that arrives, further notifications are covered by it.
func (l *listener) deliver(sub *subscriber, n store.Notification) {
	if sub.closed || sub.overflow {
		return
	}
	select {
	case sub.ch <- n:
	default:
		sub.overflow = true
		l.wg.Go(func() { l.flush(sub) })
	}
}

func (l *listener) flush(sub *subscriber) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		l.mu.Lock()
		if sub.closed {
			l.mu.Unlock()
			return
		}
		select {
		case sub.ch <- store.Notification{Channel: sub.channel}:
			sub.overflow = false
			l.mu.Unlock()
			return
		default:
		}
		l.mu.Unlock()
		select {
		case <-t.C:
		case <-l.ctx.Done():
			return
		}
	}
}
