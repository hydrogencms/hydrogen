package delivery

import (
	"fmt"
	"runtime"
	"testing"
)

// heapPerEntry fills a cache with n entries of the given body size and tag count and returns the live heap
// bytes per entry, beyond the body, and the cache's own accounting of it.
func heapPerEntry(t testing.TB, n, body, tags int) (heap, counted float64) {
	m := &chanMeta{hCC: []string{"public, max-age=0, s-maxage=60, stale-while-revalidate=60"}, hVary: []string{"Authorization"}}
	c := newCache(1 << 40)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for i := range n {
		tl := make([]string, tags)
		for j := range tags {
			tl[j] = fmt.Sprintf("i:%08x-0000-4000-8000-%012x", j, i)
		}
		key := fmt.Sprintf("web\x00GET\x00/items/%08x-0000-4000-8000-%012x\x00include=1&locale=de", 0, i)
		e := newEntry(m, []byte(key), make([]byte, body), `"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`, tl)
		c.put(e, c.generation())
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(c)
	return float64(after.HeapAlloc-before.HeapAlloc)/float64(n) - float64(body), float64(c.used)/float64(n) - float64(body)
}

// TestEntrySizeMatchesTheHeap checks the cache's accounting against the live heap, for entries with a shared
// tag set and with tags nobody else has (the worst case).
func TestEntrySizeMatchesTheHeap(t *testing.T) {
	if testing.Short() {
		t.Skip("measures the heap")
	}
	for _, tags := range []int{1, 2, 5, 20} {
		heap, counted := heapPerEntry(t, 5000, 1000, tags)
		if r := counted / heap; r < 0.75 || r > 1.5 {
			t.Errorf("%d tags: the heap holds %.0f bytes per entry beyond its body, the cache counts %.0f", tags, heap, counted)
		}
		t.Logf("tags=%d heap/entry beyond body=%.0f counted=%.0f", tags, heap, counted)
	}
}
