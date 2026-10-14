package sqlite

import (
	"context"
	"sync"

	"github.com/hydrogencms/hydrogen/store"
)

// hub delivers notifications within the process; SQLite runs on one node.
type hub struct {
	mu   sync.Mutex
	subs map[string]map[chan store.Notification]struct{}
}

func newHub() *hub { return &hub{subs: map[string]map[chan store.Notification]struct{}{}} }

func (h *hub) Listen(ctx context.Context, channel string) (<-chan store.Notification, error) {
	ch := make(chan store.Notification, 64)
	h.mu.Lock()
	if h.subs[channel] == nil {
		h.subs[channel] = map[chan store.Notification]struct{}{}
	}
	h.subs[channel][ch] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.subs[channel], ch)
		h.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

// send never blocks: a listener with a full buffer already has a wake-up pending, and listeners re-check
// state rather than counting messages.
func (h *hub) send(n store.Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[n.Channel] {
		select {
		case ch <- n:
		default:
		}
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
