package ids

import (
	"bytes"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
)

func TestGeneratorOrderAndFormat(t *testing.T) {
	c := clock.NewManual(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	g := NewGenerator(c, nil)

	prev := g.New()
	for i := range 10000 {
		if i%3000 == 0 {
			c.Advance(time.Millisecond)
		}
		u := g.New()
		if bytes.Compare(u[:], prev[:]) <= 0 {
			t.Fatalf("not increasing: %s then %s", prev, u)
		}
		if u[6]>>4 != 7 || u[8]>>6 != 2 {
			t.Fatalf("bad version or variant: %s", u)
		}
		prev = u
	}
}

func TestTimestamp(t *testing.T) {
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	u := NewGenerator(clock.NewManual(at), nil).New()
	ms := int64(u[0])<<40 | int64(u[1])<<32 | int64(u[2])<<24 | int64(u[3])<<16 | int64(u[4])<<8 | int64(u[5])
	if ms != at.UnixMilli() {
		t.Fatalf("timestamp %d, want %d", ms, at.UnixMilli())
	}
}

func TestParseRoundTrip(t *testing.T) {
	u := NewGenerator(clock.System{}, nil).New()
	p, err := Parse(u.String())
	if err != nil || p != u {
		t.Fatalf("round trip: %v %s %s", err, p, u)
	}
	for _, bad := range []string{"", "not-a-uuid", "0190f0e0-0000-7000-8000-00000000000g"} {
		if _, err := Parse(bad); err == nil {
			t.Fatalf("Parse(%q) succeeded", bad)
		}
	}
}
