package collab

import (
	"testing"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

func upd(seq int64, size int) store.CollabUpdate {
	return store.CollabUpdate{Seq: seq, Data: make([]byte, size)}
}

// The feeds of all items share one byte budget: the oldest frames of the largest feeds go first, and a stream
// behind the floor is told to read from the store.
func TestFeedBudget(t *testing.T) {
	h := newHub(&Service{o: Options{FeedBudget: 100}})
	var a, b ids.UUID
	a[0], b[0] = 1, 2
	fa, fb := &feed{}, &feed{}
	h.feeds[a], h.feeds[b] = fa, fb
	add := func(f *feed, ups ...store.CollabUpdate) {
		h.feedBytes.Add(int64(f.add(ups)))
		h.enforceBudget()
	}
	add(fa, upd(1, 30), upd(2, 30))
	add(fb, upd(3, 30))
	if got := h.feedBytes.Load(); got != 90 {
		t.Fatalf("bytes = %d, want 90", got)
	}
	add(fb, upd(4, 30)) // 120 > 100: the largest feed (a or b, 60 each; the first found) loses its oldest frame
	if got := h.feedBytes.Load(); got > 100 {
		t.Fatalf("bytes = %d, over the budget", got)
	}
	if want := int64(fa.size() + fb.size()); h.feedBytes.Load() != want {
		t.Errorf("accounted %d, feeds hold %d", h.feedBytes.Load(), want)
	}
	// Whoever lost frames reports that a stream behind them must read from the store.
	var lost *feed
	for _, f := range []*feed{fa, fb} {
		if _, ok := f.since(0, 10); !ok {
			lost = f
		}
	}
	if lost == nil {
		t.Fatal("no feed evicted a frame")
	}
	// Dropping a feed gives its bytes back, and a late add to it counts nothing.
	h.mu.Lock()
	h.dropFeed(a)
	h.mu.Unlock()
	if d := fa.add([]store.CollabUpdate{upd(9, 50)}); d != 0 {
		t.Errorf("add to a dropped feed changed the size by %d", d)
	}
	if want := int64(fb.size()); h.feedBytes.Load() != want {
		t.Errorf("after the drop: accounted %d, b holds %d", h.feedBytes.Load(), want)
	}
}
