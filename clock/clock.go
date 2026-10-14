// Package clock is the only source of time in Hydrogen. Code takes a Clock instead of calling time.Now, so
// tests and snapshots stay deterministic.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

// System is the real clock, in UTC.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Manual is a clock for tests: it only moves when told to.
type Manual struct {
	mu  sync.Mutex
	now time.Time
}

func NewManual(t time.Time) *Manual { return &Manual{now: t.UTC()} }

func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.now
}

func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	m.now = t.UTC()
	m.mu.Unlock()
}

func (m *Manual) Advance(d time.Duration) {
	m.mu.Lock()
	m.now = m.now.Add(d)
	m.mu.Unlock()
}
