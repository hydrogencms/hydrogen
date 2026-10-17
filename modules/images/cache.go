package images

import (
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
)

// ttlCache is a small bounded cache whose entries expire; when full it drops the entry that expires first.
type ttlCache[V any] struct {
	mu  sync.Mutex
	clk clock.Clock
	max int
	m   map[string]ttlEntry[V]
}

type ttlEntry[V any] struct {
	v   V
	exp time.Time
}

func newTTLCache[V any](clk clock.Clock, max int) *ttlCache[V] {
	return &ttlCache[V]{clk: clk, max: max, m: map[string]ttlEntry[V]{}}
}

func (c *ttlCache[V]) get(key string) (v V, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, found := c.m[key]
	if !found || !c.clk.Now().Before(e.exp) {
		delete(c.m, key)
		return v, false
	}
	return e.v, true
}

func (c *ttlCache[V]) put(key string, v V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.m[key]; !ok && len(c.m) >= c.max {
		now := c.clk.Now()
		var oldest string
		var at time.Time
		for k, e := range c.m {
			if !now.Before(e.exp) {
				delete(c.m, k)
				continue
			}
			if oldest == "" || e.exp.Before(at) {
				oldest, at = k, e.exp
			}
		}
		if len(c.m) >= c.max {
			delete(c.m, oldest)
		}
	}
	c.m[key] = ttlEntry[V]{v, c.clk.Now().Add(ttl)}
}
