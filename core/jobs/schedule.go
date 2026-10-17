// Package jobs parses job schedules and runs scheduled jobs, one run per tick across all nodes sharing a
// database.
package jobs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule computes when a job is due.
type Schedule interface {
	// Next returns the first time strictly after t at which the job is due.
	Next(t time.Time) time.Time
}

// MinEvery is the shortest "@every" interval.
const MinEvery = time.Second

// Parse reads a schedule: a 5-field cron expression ("m h dom mon dow", UTC) or "@every <duration>".
//
// Fields accept "*", numbers, ranges "a-b", lists "a,b", and steps "*/n" or "a-b/n" (also "a/n" meaning
// a-max). Day of week is 0-7 (0 and 7 are Sunday). As in classic cron, when both day-of-month and day-of-week
// are restricted, a day matches if either does. "@hourly", "@daily" and "@weekly" are accepted.
func Parse(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)
	switch spec {
	case "@hourly":
		spec = "0 * * * *"
	case "@daily", "@midnight":
		spec = "0 0 * * *"
	case "@weekly":
		spec = "0 0 * * 0"
	}
	if rest, ok := strings.CutPrefix(spec, "@every"); ok {
		d, err := time.ParseDuration(strings.TrimSpace(rest))
		if err != nil {
			return nil, fmt.Errorf("schedule %q: %v", spec, err)
		}
		if d < MinEvery {
			return nil, fmt.Errorf("schedule %q: interval must be at least %s", spec, MinEvery)
		}
		return every(d), nil
	}
	if strings.HasPrefix(spec, "@") {
		return nil, fmt.Errorf("schedule %q: unknown shortcut", spec)
	}
	f := strings.Fields(spec)
	if len(f) != 5 {
		return nil, fmt.Errorf("schedule %q: want 5 fields (minute hour day-of-month month day-of-week) or @every <duration>", spec)
	}
	var c cron
	var err error
	for i, p := range []struct {
		name     string
		min, max int
		dst      *uint64
	}{{"minute", 0, 59, &c.min}, {"hour", 0, 23, &c.hour}, {"day-of-month", 1, 31, &c.dom},
		{"month", 1, 12, &c.mon}, {"day-of-week", 0, 7, &c.dow}} {
		if *p.dst, err = parseField(f[i], p.min, p.max); err != nil {
			return nil, fmt.Errorf("schedule %q: %s: %v", spec, p.name, err)
		}
	}
	if c.dow&(1<<7) != 0 {
		c.dow = c.dow&^(1<<7) | 1
	}
	c.domStar, c.dowStar = f[2][0] == '*', f[4][0] == '*'
	if c.Next(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return nil, fmt.Errorf("schedule %q: no date ever matches", spec)
	}
	return c, nil
}

func parseField(s string, lo, hi int) (uint64, error) {
	var set uint64
	for part := range strings.SplitSeq(s, ",") {
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepS)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("bad step %q", stepS)
			}
			step = n
		}
		a, b := lo, hi
		switch rng {
		case "*":
		case "":
			return 0, fmt.Errorf("empty list element")
		default:
			from, to, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = atoi(from, lo, hi); err != nil {
				return 0, err
			}
			switch {
			case isRange:
				if b, err = atoi(to, lo, hi); err != nil {
					return 0, err
				}
				if b < a {
					return 0, fmt.Errorf("range %q is backwards", rng)
				}
			case hasStep:
			default:
				b = a
			}
		}
		for v := a; v <= b; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

func atoi(s string, lo, hi int) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || s != strconv.Itoa(n) {
		return 0, fmt.Errorf("%q is not a number", s)
	}
	if n < lo || n > hi {
		return 0, fmt.Errorf("%d is outside %d-%d", n, lo, hi)
	}
	return n, nil
}

// every fires at multiples of the interval since the zero time, so nodes agree on the ticks.
type every time.Duration

func (e every) Next(t time.Time) time.Time {
	return t.UTC().Truncate(time.Duration(e)).Add(time.Duration(e))
}

type cron struct {
	min, hour, dom, mon, dow uint64
	domStar, dowStar         bool
}

func (c cron) dayMatches(t time.Time) bool {
	dom := c.dom&(1<<uint(t.Day())) != 0
	dow := c.dow&(1<<uint(t.Weekday())) != 0
	switch {
	case c.domStar || c.dowStar:
		return dom && dow
	default:
		return dom || dow
	}
}

// Next finds the first matching minute after t, searching up to 8 years ahead (enough for Feb 29).
func (c cron) Next(t time.Time) time.Time {
	t = t.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(8, 0, 0)
	for t.Before(limit) {
		if c.mon&(1<<uint(t.Month())) == 0 {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
			continue
		}
		if c.hour&(1<<uint(t.Hour())) == 0 {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
			continue
		}
		if c.min&(1<<uint(t.Minute())) == 0 {
			t = t.Add(time.Minute)
			continue
		}
		return t
	}
	return time.Time{}
}
