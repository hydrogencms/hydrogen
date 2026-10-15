package delivery

import (
	"strconv"
	"sync"

	"github.com/hydrogencms/hydrogen/cachetag"
)

// entry is a cached 200 response: the body and the header values to send with it. The header slices are
// shared by every response served from the entry and never modified.
type entry struct {
	key  string
	body []byte
	etag string // quoted

	hETag, hCache, hSurrogate, hTag, hLength, hVary []string

	tags       []string
	size       int64
	dead       bool // purged: no longer served, waiting to be evicted
	prev, next *entry
}

// cache is a byte-bounded LRU of responses with a tag index for purges.
type cache struct {
	mu    sync.Mutex
	max   int64
	used  int64
	m     map[string]*entry
	byTag map[string]map[*entry]struct{}
	head  entry // sentinel: head.next is the most recently used
	gen   uint64
}

func newCache(maxBytes int64) *cache {
	c := &cache{max: maxBytes, m: map[string]*entry{}, byTag: map[string]map[*entry]struct{}{}}
	c.head.next, c.head.prev = &c.head, &c.head
	return c
}

func (c *cache) unlink(e *entry) {
	e.prev.next, e.next.prev = e.next, e.prev
	e.prev, e.next = nil, nil
}

func (c *cache) pushFront(e *entry) {
	e.next, e.prev = c.head.next, &c.head
	c.head.next.prev = e
	c.head.next = e
}

// get returns the entry for key and marks it recently used. The conversion in the map index does not allocate.
func (c *cache) get(key []byte) *entry {
	c.mu.Lock()
	e := c.m[string(key)]
	if e != nil && e.dead {
		e = nil
	} else if e != nil && c.head.next != e {
		c.unlink(e)
		c.pushFront(e)
	}
	c.mu.Unlock()
	return e
}

// generation counts purges. A response computed before a purge must not be stored after it.
func (c *cache) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// Memory accounting. The cache counts what an entry really pins on the heap, so cache_bytes bounds memory and
// not just payload bytes. The constants come from a heap profile of a full cache (TestEntrySizeMatchesTheHeap
// repeats the measurement): beyond body, key and ETag, an entry costs about 320 bytes (the entry, its map slot,
// list links, header slices) and every tag about 272 bytes (its slot in the tag index with a set of its own in
// the worst case, a string header in the entry) plus three copies of its text (the entry, the Surrogate-Key
// header and the Cache-Tag header).
const (
	entryOverhead = 320
	tagOverhead   = 272
)

func entrySize(e *entry) int64 {
	n := int64(len(e.body)+len(e.key)+len(e.etag)) + entryOverhead
	for _, t := range e.tags {
		n += 3*int64(len(t)) + tagOverhead
	}
	return n
}

// put stores e unless a purge ran since gen was read or the entry is too big to be worth keeping.
func (c *cache) put(e *entry, gen uint64) bool {
	e.size = entrySize(e)
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen || e.size > c.max/4 {
		return false
	}
	if old := c.m[e.key]; old != nil {
		c.remove(old)
	}
	c.m[e.key] = e
	c.pushFront(e)
	c.used += e.size
	for _, t := range e.tags {
		set := c.byTag[t]
		if set == nil {
			set = map[*entry]struct{}{}
			c.byTag[t] = set
		}
		set[e] = struct{}{}
	}
	for c.used > c.max && c.head.prev != &c.head {
		c.remove(c.head.prev)
	}
	return true
}

func (c *cache) remove(e *entry) {
	c.unlink(e)
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

// purgeSlice is how many entries a purge evicts per lock hold.
const purgeSlice = 256

// purge drops every entry carrying one of the tags; cachetag.All clears the cache.
//
// The lock is never held for the whole purge. Under it, the victims are collected and marked dead, which is
// what invalidates them: get stops serving a dead entry at once, so a purge is as immediate as one done in a
// single critical section. The expensive part, unlinking victims and their tag index slots, then runs in
// slices of purgeSlice with the lock released between them, so readers wait for at most one slice. The tag
// index is walked once under the lock instead of keeping per-tag generations, which would have to be
// pruned and cost every get a lookup per tag.
func (c *cache) purge(tags ...string) {
	c.mu.Lock()
	c.gen++
	var victims []*entry
	for _, t := range tags {
		if t == cachetag.All {
			c.clearLocked()
			c.mu.Unlock()
			return
		}
		for e := range c.byTag[t] {
			if !e.dead {
				e.dead = true
				victims = append(victims, e)
			}
		}
	}
	c.mu.Unlock()
	for len(victims) > 0 {
		n := min(len(victims), purgeSlice)
		c.mu.Lock()
		for _, e := range victims[:n] {
			if c.m[e.key] == e {
				c.remove(e)
			}
		}
		c.mu.Unlock()
		victims = victims[n:]
	}
}

func (c *cache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.clearLocked()
}

// clearLocked drops everything in constant time; the old maps and entries are left to the garbage collector.
func (c *cache) clearLocked() {
	c.m = map[string]*entry{}
	c.byTag = map[string]map[*entry]struct{}{}
	c.head.next, c.head.prev = &c.head, &c.head
	c.used = 0
}

func (c *cache) stats() (entries int, bytes int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m), c.used
}

func lengthHeader(n int) []string { return []string{strconv.Itoa(n)} }
