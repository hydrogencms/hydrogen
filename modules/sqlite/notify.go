package sqlite

import (
	"context"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/store"
)

// hub delivers notifications within the process; SQLite runs on one node.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[*subscriber]struct{}
}

type subscriber struct {
	ch       chan store.Notification
	channel  string
	overflow bool // an empty-payload notification is owed
	closed   bool
}

func newHub() *hub { return &hub{subs: map[string]map[*subscriber]struct{}{}} }

func (h *hub) Listen(ctx context.Context, channel string) (<-chan store.Notification, error) {
	sub := &subscriber{ch: make(chan store.Notification, 64), channel: channel}
	h.mu.Lock()
	if h.subs[channel] == nil {
		h.subs[channel] = map[*subscriber]struct{}{}
	}
	h.subs[channel][sub] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.subs[channel], sub)
		sub.closed = true
		close(sub.ch)
		h.mu.Unlock()
	}()
	return sub.ch, nil
}

// send never blocks. A subscriber with a full buffer is marked and gets an empty-payload notification as soon
// as there is room; notifications sent meanwhile are covered by it, so none is dropped silently.
func (h *hub) send(n store.Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[n.Channel] {
		if sub.closed || sub.overflow {
			continue
		}
		select {
		case sub.ch <- n:
		default:
			sub.overflow = true
			go h.flush(sub)
		}
	}
}

func (h *hub) flush(sub *subscriber) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		h.mu.Lock()
		if sub.closed {
			h.mu.Unlock()
			return
		}
		select {
		case sub.ch <- store.Notification{Channel: sub.channel}:
			sub.overflow = false
			h.mu.Unlock()
			return
		default:
		}
		h.mu.Unlock()
		<-t.C
	}
}

type locker struct {
	mu   sync.Mutex
	held map[string]bool
}

func newLocker() *locker { return &locker{held: map[string]bool{}} }

func (l *locker) TryLock(_ context.Context, name string) (store.Lock, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[name] {
		return nil, false, nil
	}
	l.held[name] = true
	return &lock{l: l, name: name}, true, nil
}

type lock struct {
	l    *locker
	name string
	once sync.Once
}

func (k *lock) Release(context.Context) error {
	k.once.Do(func() {
		k.l.mu.Lock()
		delete(k.l.held, k.name)
		k.l.mu.Unlock()
	})
	return nil
}
