package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/store"
)

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04:05", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestParseInvalid(t *testing.T) {
	for _, tc := range []struct{ spec, want string }{
		{"", "want 5 fields"},
		{"* * * *", "want 5 fields"},
		{"* * * * * *", "want 5 fields"},
		{"60 * * * *", "outside 0-59"},
		{"* 24 * * *", "outside 0-23"},
		{"* * 0 * *", "outside 1-31"},
		{"* * * 13 *", "outside 1-12"},
		{"* * * * 8", "outside 0-7"},
		{"5-1 * * * *", "backwards"},
		{"*/0 * * * *", "bad step"},
		{"*/x * * * *", "bad step"},
		{"a * * * *", "not a number"},
		{"1,,2 * * * *", "empty list"},
		{"+1 * * * *", "not a number"},
		{"0 0 30 2 *", "no date ever matches"},
		{"@every", "invalid duration"},
		{"@every 10ms", "at least"},
		{"@every soon", "invalid duration"},
		{"@yearly", "unknown shortcut"},
	} {
		if _, err := Parse(tc.spec); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) = %v, want %q", tc.spec, err, tc.want)
		}
	}
}

func TestNext(t *testing.T) {
	for _, tc := range []struct{ spec, after, want string }{
		{"* * * * *", "2026-10-14 09:00:00", "2026-10-14 09:01:00"},
		{"* * * * *", "2026-10-14 09:00:59", "2026-10-14 09:01:00"},
		{"*/15 * * * *", "2026-10-14 09:15:00", "2026-10-14 09:30:00"},
		{"*/15 * * * *", "2026-10-14 09:46:00", "2026-10-14 10:00:00"},
		{"0 3 * * *", "2026-10-14 03:00:00", "2026-10-15 03:00:00"},
		{"30 2 * * *", "2026-10-14 02:29:59", "2026-10-14 02:30:00"},
		{"0 0 1 * *", "2026-01-31 12:00:00", "2026-02-01 00:00:00"},
		{"0 0 31 * *", "2026-04-30 00:00:00", "2026-05-31 00:00:00"},
		{"0 0 31 * *", "2026-02-01 00:00:00", "2026-03-31 00:00:00"},
		{"0 0 29 2 *", "2026-03-01 00:00:00", "2028-02-29 00:00:00"},
		{"59 23 31 12 *", "2026-12-31 23:59:00", "2027-12-31 23:59:00"},
		{"0 9-17/4 * * *", "2026-10-14 09:00:00", "2026-10-14 13:00:00"},
		{"0 9-17/4 * * *", "2026-10-14 17:00:00", "2026-10-15 09:00:00"},
		{"0,30 8,20 * * *", "2026-10-14 08:30:00", "2026-10-14 20:00:00"},
		{"10/20 * * * *", "2026-10-14 09:10:00", "2026-10-14 09:30:00"},
		{"0 0 * * 1", "2026-10-14 00:00:00", "2026-10-19 00:00:00"},
		{"0 0 * * 7", "2026-10-14 00:00:00", "2026-10-18 00:00:00"},
		{"0 0 * * 0", "2026-10-14 00:00:00", "2026-10-18 00:00:00"},
		{"0 0 * * 1-5", "2026-10-16 00:00:00", "2026-10-19 00:00:00"},
		{"0 0 13 * 5", "2026-10-14 00:00:00", "2026-10-16 00:00:00"}, // dom or dow
		{"0 0 13 * 5", "2026-10-16 00:00:00", "2026-10-23 00:00:00"},
		{"0 0 * 10 *", "2026-10-31 00:00:00", "2027-10-01 00:00:00"},
		{"@hourly", "2026-10-14 09:30:00", "2026-10-14 10:00:00"},
		{"@daily", "2026-10-14 09:30:00", "2026-10-15 00:00:00"},
		{"@weekly", "2026-10-14 09:30:00", "2026-10-18 00:00:00"},
		{"@every 30s", "2026-10-14 09:00:10", "2026-10-14 09:00:30"},
		{"@every 30s", "2026-10-14 09:00:30", "2026-10-14 09:01:00"},
		{"@every 1h", "2026-10-14 09:30:00", "2026-10-14 10:00:00"},
	} {
		s, err := Parse(tc.spec)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.spec, err)
			continue
		}
		if got := s.Next(at(tc.after)); !got.Equal(at(tc.want)) {
			t.Errorf("%q after %s = %s, want %s", tc.spec, tc.after, got.Format(time.DateTime), tc.want)
		}
	}
}

type memLocker struct {
	mu   sync.Mutex
	held map[string]bool
}

func (m *memLocker) TryLock(_ context.Context, name string) (store.Lock, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held == nil {
		m.held = map[string]bool{}
	}
	if m.held[name] {
		return nil, false, nil
	}
	m.held[name] = true
	return memLock{m, name}, true, nil
}

type memLock struct {
	m    *memLocker
	name string
}

func (l memLock) Release(context.Context) error {
	l.m.mu.Lock()
	defer l.m.mu.Unlock()
	delete(l.m.held, l.name)
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRunnerTick(t *testing.T) {
	clk := clock.NewManual(at("2026-10-14 09:00:30"))
	var a, b, bad atomic.Int32
	r, err := NewRunner(Options{Clock: clk, Locker: &memLocker{}, Log: quiet(), Hold: time.Millisecond}, []Job{
		{Name: "a", Schedule: "* * * * *", Run: func(context.Context) error { a.Add(1); return nil }},
		{Name: "b", Schedule: "*/5 * * * *", Run: func(context.Context) error { b.Add(1); return errors.New("boom") }},
		{Name: "bad", Schedule: "@every 1m", Run: func(context.Context) error { bad.Add(1); panic("kaboom") }},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r.Tick(ctx)
	if a.Load() != 0 {
		t.Fatal("ran before due")
	}
	clk.Set(at("2026-10-14 09:01:00"))
	r.Tick(ctx)
	r.Tick(ctx)
	if a.Load() != 1 || b.Load() != 0 || bad.Load() != 1 {
		t.Fatalf("a=%d b=%d bad=%d", a.Load(), b.Load(), bad.Load())
	}
	// a late wake-up runs once for several missed ticks
	time.Sleep(20 * time.Millisecond) // lock hold
	clk.Set(at("2026-10-14 09:10:20"))
	r.Tick(ctx)
	if a.Load() != 2 || b.Load() != 1 {
		t.Fatalf("a=%d b=%d", a.Load(), b.Load())
	}
}

func TestRunnerSkipsHeldLock(t *testing.T) {
	clk := clock.NewManual(at("2026-10-14 09:00:30"))
	lk := &memLocker{}
	var n atomic.Int32
	r, err := NewRunner(Options{Clock: clk, Locker: lk, Log: quiet()}, []Job{
		{Name: "a", Schedule: "* * * * *", Run: func(context.Context) error { n.Add(1); return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	l, _, _ := lk.TryLock(context.Background(), "job:a")
	clk.Advance(time.Minute)
	r.Tick(context.Background())
	if n.Load() != 0 {
		t.Fatal("ran while locked")
	}
	_ = l.Release(context.Background())
	if err := r.RunNow(context.Background(), "a"); err != nil || n.Load() != 1 {
		t.Fatalf("RunNow: %v %d", err, n.Load())
	}
	if err := r.RunNow(context.Background(), "zzz"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	l, _, _ = lk.TryLock(context.Background(), "job:a")
	defer func() { _ = l.Release(context.Background()) }()
	if err := r.RunNow(context.Background(), "a"); !errors.Is(err, ErrLocked) {
		t.Fatalf("locked: %v", err)
	}
}

func TestRunnerRunLoop(t *testing.T) {
	clk := clock.NewManual(at("2026-10-14 09:00:30"))
	ran := make(chan struct{}, 8)
	r, err := NewRunner(Options{Clock: clk, Locker: &memLocker{}, Log: quiet(), Poll: 5 * time.Millisecond, Hold: time.Millisecond}, []Job{
		{Name: "a", Schedule: "* * * * *", Run: func(context.Context) error { ran <- struct{}{}; return nil }}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- r.Run(ctx) }()
	select {
	case <-ran:
		t.Fatal("ran before due")
	case <-time.After(30 * time.Millisecond):
	}
	clk.Advance(time.Minute)
	select {
	case <-ran:
	case <-time.After(5 * time.Second):
		t.Fatal("did not run")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestNewRunnerErrors(t *testing.T) {
	ok := func(context.Context) error { return nil }
	for _, tc := range []struct {
		jobs []Job
		want string
	}{
		{[]Job{{Name: "A", Schedule: "@daily", Run: ok}}, "lowercase"},
		{[]Job{{Name: "", Schedule: "@daily", Run: ok}}, "empty"},
		{[]Job{{Name: "a", Schedule: "@daily", Run: ok}, {Name: "a", Schedule: "@daily", Run: ok}}, "twice"},
		{[]Job{{Name: "a", Schedule: "nope", Run: ok}}, "want 5 fields"},
		{[]Job{{Name: "a", Schedule: "@daily"}}, "no function"},
	} {
		if _, err := NewRunner(Options{}, tc.jobs); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: %v, want %q", tc.jobs, err, tc.want)
		}
	}
}
