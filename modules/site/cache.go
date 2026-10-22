package site

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
)

// pageCache is a byte-bounded LRU of rendered pages with a tag index, purged by the same cache tags the
// delivery API uses.
type pageCache struct {
	mu    sync.Mutex
	max   int64
	used  int64
	gen   atomic.Uint64 // written under mu
	order *list.List    // front = most recently used
	m     map[string]*list.Element
	byTag map[string]map[*cached]struct{}
}

type cached struct {
	key  string
	body []byte
	etag string
	ttl  time.Duration
	tags []string
	size int64
	elem *list.Element
}

const entryOverhead = 400

func newPageCache(max int64) *pageCache {
	return &pageCache{max: max, order: list.New(), m: map[string]*list.Element{}, byTag: map[string]map[*cached]struct{}{}}
}

func (c *pageCache) get(key string) *cached {
	c.mu.Lock()
	defer c.mu.Unlock()
	el := c.m[key]
	if el == nil {
		return nil
	}
	c.order.MoveToFront(el)
	return el.Value.(*cached)
}

// generation counts purges: a page rendered before a purge must not be stored after it.
func (c *pageCache) generation() uint64 { return c.gen.Load() }

func (c *pageCache) put(e *cached, gen uint64) {
	e.size = int64(len(e.body)+len(e.key)+len(e.etag)) + entryOverhead
	for _, t := range e.tags {
		e.size += int64(len(t)) + 120
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen.Load() || e.size > c.max/4 {
		return
	}
	if old := c.m[e.key]; old != nil {
		c.remove(old.Value.(*cached))
	}
	e.elem = c.order.PushFront(e)
	c.m[e.key] = e.elem
	c.used += e.size
	for _, t := range e.tags {
		set := c.byTag[t]
		if set == nil {
			set = map[*cached]struct{}{}
			c.byTag[t] = set
		}
		set[e] = struct{}{}
	}
	for c.used > c.max && c.order.Len() > 0 {
		c.remove(c.order.Back().Value.(*cached))
	}
}

func (c *pageCache) remove(e *cached) {
	c.order.Remove(e.elem)
	delete(c.m, e.key)
	c.used -= e.size
	for _, t := range e.tags {
		if set := c.byTag[t]; set != nil {
			delete(set, e)
			if len(set) == 0 {
				delete(c.byTag, t)
			}
		}
	}
}

// purge drops every page carrying one of the tags; cachetag.All drops everything.
func (c *pageCache) purge(tags ...string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen.Add(1)
	for _, t := range tags {
		if t == cachetag.All {
			c.clearLocked()
			return
		}
		for e := range c.byTag[t] {
			c.remove(e)
		}
	}
}

func (c *pageCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen.Add(1)
	c.clearLocked()
}

func (c *pageCache) clearLocked() {
	c.order.Init()
	c.m = map[string]*list.Element{}
	c.byTag = map[string]map[*cached]struct{}{}
	c.used = 0
}
