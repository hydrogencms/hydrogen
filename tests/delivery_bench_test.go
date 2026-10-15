package tests

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
)

// nopWriter discards the response and reuses its header map, so a benchmark measures the handler.
type nopWriter struct{ h http.Header }

func (w *nopWriter) Header() http.Header         { return w.h }
func (w *nopWriter) WriteHeader(int)             {}
func (w *nopWriter) Write(b []byte) (int, error) { return len(b), nil }

func benchNode(b *testing.B, be backend) (*dnode, ids.UUID) {
	b.Helper()
	clk := newClock()
	n := newDeliveryNode(b, be.open(b, clk), clk)
	ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
	var id ids.UUID
	for i := range 40 {
		id = n.create("article", "", "en", fmt.Sprintf(`{"title":"Article %d with a title of some length","rank":%d,"author":"%s"}`, i, i, ann), true)
	}
	return n, id
}

func BenchmarkDeliveryCachedHit(b *testing.B) {
	n, id := benchNode(b, backends[0])
	req := httptest.NewRequest("GET", "/api/deliver/v1/items/"+id.String()+"?channel=web&include=1", nil)
	req.URL.Path = "/items/" + id.String()
	w := &nopWriter{h: http.Header{}}
	h := n.app.Services().Delivery.Handler()
	h.ServeHTTP(w, req) // fill the cache
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		clear(w.h)
		h.ServeHTTP(w, req)
	}
}

func BenchmarkDeliveryCachedHitParallel(b *testing.B) {
	n, id := benchNode(b, backends[0])
	h := n.app.Services().Delivery.Handler()
	req := httptest.NewRequest("GET", "/items/"+id.String()+"?channel=web", nil)
	h.ServeHTTP(&nopWriter{h: http.Header{}}, req)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		w := &nopWriter{h: http.Header{}}
		req := req.Clone(req.Context())
		for pb.Next() {
			clear(w.h)
			h.ServeHTTP(w, req)
		}
	})
}

// uncached runs the request with the response cache emptied before every iteration and reports percentiles.
func uncached(b *testing.B, be backend, target func(id ids.UUID) string) {
	n, id := benchNode(b, be)
	d := n.app.Services().Delivery
	h := d.Handler()
	req := httptest.NewRequest("GET", target(id), nil)
	w := &nopWriter{h: http.Header{}}
	for range 20 { // warm up the connection pool and statement caches
		d.Purge("*")
		h.ServeHTTP(w, req)
	}
	times := make([]time.Duration, 0, b.N)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		b.StopTimer()
		d.Purge("*")
		clear(w.h)
		b.StartTimer()
		start := time.Now()
		h.ServeHTTP(w, req)
		times = append(times, time.Since(start))
	}
	b.StopTimer()
	slices.Sort(times)
	b.ReportMetric(float64(times[len(times)/2].Microseconds()), "p50-µs")
	b.ReportMetric(float64(times[len(times)*99/100].Microseconds()), "p99-µs")
}

func BenchmarkDeliveryItemUncached(b *testing.B) {
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			uncached(b, be, func(id ids.UUID) string { return "/items/" + id.String() + "?channel=web" })
		})
	}
}

func BenchmarkDeliveryItemIncludeUncached(b *testing.B) {
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			uncached(b, be, func(id ids.UUID) string { return "/items/" + id.String() + "?channel=web&include=2" })
		})
	}
}

func BenchmarkDeliveryListing20Uncached(b *testing.B) {
	for _, be := range backends {
		b.Run(be.name, func(b *testing.B) {
			uncached(b, be, func(ids.UUID) string { return "/collections/article/items?channel=web&limit=20&sort=-rank" })
		})
	}
}
