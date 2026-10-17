package access

import (
	"container/list"
	"errors"
	"net/netip"
	"runtime"
	"slices"
	"sync"
	"time"
)

const (
	throttleWindow = 15 * time.Minute
	throttleBase   = 30 * time.Second
	throttleMax    = 15 * time.Minute
	throttleKeys   = 10_000

	// Failure ceilings per window: a login at one client, a client across logins, a login across clients.
	limitPair  = 5
	limitIP    = 30
	limitLogin = 100

	knownIPsPerLogin = 5
)

// ErrBusy: too many password checks are running; try again shortly (503). Nothing was hashed or counted.
var ErrBusy = errors.New("access: too many concurrent password checks")

// throttle counts attempts per key in memory (per node). take counts an attempt before the password is checked
// and refuses once limit attempts happened within the window: the limit-th attempt still runs but blocks the key
// for an exponentially growing time (capped at throttleMax), so parallel requests cannot all guess. give undoes
// an attempt that turned out right. Memory is bounded by an LRU.
type throttle struct {
	mu      sync.Mutex
	limit   int
	max     int
	entries map[string]*list.Element
	order   *list.List // front = most recently used
}

type throttleEntry struct {
	key   string
	first time.Time
	count int
	until time.Time
}

func newThrottle(max, limit int) *throttle {
	return &throttle{limit: limit, max: max, entries: map[string]*list.Element{}, order: list.New()}
}

// take counts an attempt. A non-zero result is how long the key stays blocked; the attempt is then not counted.
func (t *throttle) take(key string, now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	el, ok := t.entries[key]
	if !ok {
		el = t.order.PushFront(&throttleEntry{key: key, first: now})
		t.entries[key] = el
		for t.order.Len() > t.max {
			old := t.order.Back()
			t.order.Remove(old)
			delete(t.entries, old.Value.(*throttleEntry).key)
		}
	} else {
		t.order.MoveToFront(el)
	}
	e := el.Value.(*throttleEntry)
	if now.Before(e.until) {
		return e.until.Sub(now)
	}
	if now.Sub(e.first) > throttleWindow {
		e.first, e.count = now, 0
	}
	e.count++
	if e.count >= t.limit {
		d := throttleBase
		for i := t.limit; i < e.count && d < throttleMax; i++ {
			d *= 2
		}
		e.until = now.Add(min(d, throttleMax))
	}
	return 0
}

// give undoes one counted attempt, lifting a block it caused.
func (t *throttle) give(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if el, ok := t.entries[key]; ok {
		e := el.Value.(*throttleEntry)
		e.count = max(e.count-1, 0)
		if e.count < t.limit {
			e.until = time.Time{}
		}
	}
}

func (t *throttle) clear(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if el, ok := t.entries[key]; ok {
		t.order.Remove(el)
		delete(t.entries, key)
	}
}

// verifyGate caps concurrent password hashing so a flood cannot exhaust CPU; full means refuse, not queue.
type verifyGate chan struct{}

func newVerifyGate(n int) verifyGate {
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	return make(verifyGate, n)
}

func (g verifyGate) tryAcquire() bool {
	select {
	case g <- struct{}{}:
		return true
	default:
		return false
	}
}

func (g verifyGate) release() { <-g }

// loginGuard throttles logins by three keys, each in its own LRU so junk logins cannot evict client entries:
// (login, client) pairs, clients across logins, and logins across clients. The last one blocks only clients that
// have not logged in as that login lately, so an attacker cannot lock a user out of their usual places.
type loginGuard struct {
	pair, ip, login *throttle

	mu    sync.Mutex
	known map[string]*list.Element // login → *knownEntry
	order *list.List
	max   int
}

type knownEntry struct {
	login string
	ips   []string
}

func newLoginGuard(max int) *loginGuard {
	return &loginGuard{pair: newThrottle(max, limitPair), ip: newThrottle(max, limitIP), login: newThrottle(max, limitLogin),
		known: map[string]*list.Element{}, order: list.New(), max: max}
}

// reservation is what attempt counted; undo it on success.
type reservation struct {
	pairKey, ipKey, loginKey string
	login                    bool
}

// clientKey is the client's address; IPv6 clients count by /64 so rotating addresses in a prefix don't evade.
func clientKey(ip string) string {
	if addr, err := netip.ParseAddr(ip); err == nil && addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return ip
}

// attempt counts a login attempt, or returns how long to wait.
func (g *loginGuard) attempt(login, ip string, now time.Time) (reservation, time.Duration) {
	ck := clientKey(ip)
	r := reservation{pairKey: login + "|" + ck, ipKey: ck, loginKey: login}
	if d := g.pair.take(r.pairKey, now); d > 0 {
		return reservation{}, d
	}
	if d := g.ip.take(r.ipKey, now); d > 0 {
		g.pair.give(r.pairKey)
		return reservation{}, d
	}
	if !g.knows(login, ck) {
		if d := g.login.take(r.loginKey, now); d > 0 {
			g.pair.give(r.pairKey)
			g.ip.give(r.ipKey)
			return reservation{}, d
		}
		r.login = true
	}
	return r, 0
}

// release undoes the reservation after a successful login and remembers the client for that login.
func (g *loginGuard) release(r reservation) {
	g.pair.give(r.pairKey)
	g.ip.give(r.ipKey)
	if r.login {
		g.login.give(r.loginKey)
	}
	g.remember(r.loginKey, r.ipKey)
}

// cancel undoes a reservation whose password was never checked.
func (g *loginGuard) cancel(r reservation) {
	g.pair.give(r.pairKey)
	g.ip.give(r.ipKey)
	if r.login {
		g.login.give(r.loginKey)
	}
}

func (g *loginGuard) knows(login, ck string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	el, ok := g.known[login]
	return ok && slices.Contains(el.Value.(*knownEntry).ips, ck)
}

func (g *loginGuard) remember(login, ck string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	el, ok := g.known[login]
	if !ok {
		el = g.order.PushFront(&knownEntry{login: login})
		g.known[login] = el
		for g.order.Len() > g.max {
			old := g.order.Back()
			g.order.Remove(old)
			delete(g.known, old.Value.(*knownEntry).login)
		}
	} else {
		g.order.MoveToFront(el)
	}
	e := el.Value.(*knownEntry)
	if i := slices.Index(e.ips, ck); i >= 0 {
		e.ips = slices.Delete(e.ips, i, i+1)
	}
	e.ips = append(e.ips, ck)
	if len(e.ips) > knownIPsPerLogin {
		e.ips = e.ips[1:]
	}
}
