package site

import (
	"container/list"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
)

// pageCache is a byte-bounded LRU of rendered pages with a tag index, purged by the same cache tags the
// delivery API uses.
//
// A page whose templates read the query string is stored per variant: the path's key plus the values of the
// parameters its renders read. The names are learned from renders and only grow; a request's other parameters
// never split the cache.
type pageCache struct {
	mu    sync.Mutex
	max   int64
	used  int64
	gen   atomic.Uint64 // written under mu
	order *list.List    // front = most recently used
	m     map[string]*list.Element
	byTag map[string]map[*cached]struct{}
	vary  map[string]*varyInfo // path key → the parameters its pages read
}

// Bounds of a path's variants: how many, how many values a parameter may hold and how long a value may be. A
// request beyond them is rendered and not stored.
const (
	maxVariants     = 32
	maxQueryValues  = 8
	maxQueryValueLn = 128
)

type varyInfo struct {
	names []string // sorted
	keys  []string // variant keys, oldest first
}

type cached struct {
	key  string
	body []byte
	etag string
	ttl  time.Duration
	tags []string
	size int64
	elem *list.Element
	base string // the path key, for a variant
}

const entryOverhead = 400

func newPageCache(max int64) *pageCache {
	return &pageCache{max: max, order: list.New(), m: map[string]*list.Element{}, byTag: map[string]map[*cached]struct{}{}, vary: map[string]*varyInfo{}}
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

// variantKey is the cache key of a path for the values a request holds of the parameters; false when there
// are too many or too long ones.
func variantKey(base string, names []string, q url.Values) (string, bool) {
	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\x00?")
	for _, n := range names {
		vals := q[n]
		if len(vals) > maxQueryValues {
			return "", false
		}
		sb.WriteString(url.QueryEscape(n))
		for _, v := range vals {
			if len(v) > maxQueryValueLn {
				return "", false
			}
			sb.WriteByte('=')
			sb.WriteString(url.QueryEscape(v))
		}
		sb.WriteByte('&')
	}
	return sb.String(), true
}

// lookup finds the page of a path for a request's query; the query is only parsed for paths whose pages read it.
func (c *pageCache) lookup(base, rawQuery string) *cached {
	c.mu.Lock()
	var names []string
	if info := c.vary[base]; info != nil {
		names = info.names
	}
	c.mu.Unlock()
	if names == nil {
		return c.get(base)
	}
	q, _ := url.ParseQuery(rawQuery)
	key, ok := variantKey(base, names, q)
	if !ok {
		return nil
	}
	return c.get(key)
}

// generation counts purges: a page rendered before a purge must not be stored after it.
func (c *pageCache) generation() uint64 { return c.gen.Load() }

func (c *pageCache) put(e *cached, gen uint64) { c.store(e, e.key, nil, "", gen) }

// store keeps a rendered page of the path; read are the query parameters the render read.
func (c *pageCache) store(e *cached, base string, read []string, rawQuery string, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info := c.vary[base]
	if len(read) == 0 && info == nil {
		e.key = base
		c.insertLocked(e, gen)
		return
	}
	if gen != c.gen.Load() {
		return
	}
	names := read
	if info != nil {
		names = info.names
		if merged := mergeNames(names, read); len(merged) != len(names) {
			// The page reads more than the variants stored so far were keyed by.
			old := info.keys
			info.keys = nil
			for _, k := range old {
				if el := c.m[k]; el != nil {
					c.remove(el.Value.(*cached))
				}
			}
			names = merged
		}
	}
	q, _ := url.ParseQuery(rawQuery)
	key, ok := variantKey(base, names, q)
	if !ok {
		return
	}
	e.key, e.base = key, base
	e.size = sizeOf(e)
	if e.size > c.max/4 {
		return
	}
	if c.vary[base] == nil {
		if info == nil {
			info = &varyInfo{}
		}
		if plain := c.m[base]; plain != nil {
			c.remove(plain.Value.(*cached))
		}
		c.vary[base] = info
	}
	info.names = names
	if old := c.m[key]; old != nil {
		c.remove(old.Value.(*cached))
	}
	if len(info.keys) >= maxVariants {
		if el := c.m[info.keys[0]]; el != nil {
			c.remove(el.Value.(*cached))
		} else {
			info.keys = info.keys[1:]
		}
	}
	info.keys = append(info.keys, key)
	c.insertLocked(e, gen)
}

func mergeNames(a, b []string) []string {
	out := slices.Clone(a)
	for _, n := range b {
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

func sizeOf(e *cached) int64 {
	size := int64(len(e.body)+len(e.key)+len(e.etag)) + entryOverhead
	for _, t := range e.tags {
		size += int64(len(t)) + 120
	}
	return size
}

func (c *pageCache) insertLocked(e *cached, gen uint64) {
	e.size = sizeOf(e)
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
	if e.base != "" {
		if info := c.vary[e.base]; info != nil {
			if i := slices.Index(info.keys, e.key); i >= 0 {
				info.keys = slices.Delete(info.keys, i, i+1)
			}
			if len(info.keys) == 0 {
				delete(c.vary, e.base)
			}
		}
	}
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
	c.vary = map[string]*varyInfo{}
	c.used = 0
}
