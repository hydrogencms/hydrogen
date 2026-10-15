package delivery

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fillCache puts entries with a few tags each until the cache is full, and returns the keys stored.
func fillCache(c *cache, body int) []string {
	var keys []string
	for i := 0; c.used+int64(body)+1024 < c.max; i++ {
		e := &entry{key: fmt.Sprintf("web\x00GET\x00/items/%036d\x00include=1", i), body: make([]byte, body),
			etag: `"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`,
			tags: []string{"c:article", fmt.Sprintf("i:%036d", i), fmt.Sprintf("i:%036d", i+1<<20)}}
		if !c.put(e, c.generation()) {
			break
		}
		keys = append(keys, e.key)
	}
	return keys
}

// BenchmarkPurgeUnderLoad purges c:article from a full 32 MiB cache while readers hit it, and reports the
// purge duration and the readers' latency while it ran.
func BenchmarkPurgeUnderLoad(b *testing.B) {
	for _, body := range []int{512, 4096} {
		b.Run(fmt.Sprintf("body%d", body), func(b *testing.B) {
			var purgeNs, p99, worst float64
			for range b.N {
				c := newCache(DefaultCacheBytes)
				keys := fillCache(c, body)
				var stop, purging atomic.Bool
				var mu sync.Mutex
				var lat []time.Duration
				var wg sync.WaitGroup
				for g := range 4 {
					wg.Add(1)
					go func() {
						defer wg.Done()
						var mine []time.Duration
						for i := g; !stop.Load(); i += 7 {
							k := []byte(keys[i%len(keys)])
							during := purging.Load()
							t0 := time.Now()
							c.get(k)
							if d := time.Since(t0); during {
								mine = append(mine, d)
							}
						}
						mu.Lock()
						lat = append(lat, mine...)
						mu.Unlock()
					}()
				}
				time.Sleep(20 * time.Millisecond)
				purging.Store(true)
				t0 := time.Now()
				c.purge("c:article")
				purgeNs += float64(time.Since(t0))
				purging.Store(false)
				stop.Store(true)
				wg.Wait()
				slices.Sort(lat)
				if len(lat) > 0 {
					p99 += float64(lat[len(lat)*99/100])
					worst += float64(lat[len(lat)-1])
				}
			}
			n := float64(b.N)
			b.ReportMetric(purgeNs/n/1e6, "purge_ms")
			b.ReportMetric(p99/n/1e3, "get_p99_us")
			b.ReportMetric(worst/n/1e3, "get_max_us")
		})
	}
}
