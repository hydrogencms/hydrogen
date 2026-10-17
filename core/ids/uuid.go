// Package ids generates UUIDv7 identifiers (RFC 9562): time-ordered, random below the millisecond.
package ids

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"sync"

	"github.com/hydrogencms/hydrogen/core/clock"
)

type UUID [16]byte

var Nil UUID

var errFormat = errors.New("ids: invalid UUID format")

func (u UUID) IsZero() bool { return u == Nil }

func (u UUID) String() string {
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:], u[10:])
	return string(b[:])
}

func (u UUID) MarshalText() ([]byte, error) { return []byte(u.String()), nil }

func (u *UUID) UnmarshalText(b []byte) error {
	p, err := Parse(string(b))
	if err != nil {
		return err
	}
	*u = p
	return nil
}

// Parse accepts the canonical 36-character form.
func Parse(s string) (UUID, error) {
	var u UUID
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return u, errFormat
	}
	src := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	if _, err := hex.Decode(u[:], []byte(src)); err != nil {
		return Nil, errFormat
	}
	return u, nil
}

// Generator produces UUIDv7 values that are strictly increasing within one generator: the 12-bit rand_a field
// is a counter seeded randomly each millisecond, and the timestamp moves forward if the counter overflows.
type Generator struct {
	clock clock.Clock
	rand  io.Reader

	mu     sync.Mutex
	lastMs int64
	seq    uint16
}

// NewGenerator uses crypto/rand when r is nil.
func NewGenerator(c clock.Clock, r io.Reader) *Generator {
	if r == nil {
		r = rand.Reader
	}
	return &Generator{clock: c, rand: r}
}

func (g *Generator) New() UUID {
	var u UUID
	if _, err := io.ReadFull(g.rand, u[6:]); err != nil {
		panic("ids: random source failed: " + err.Error())
	}

	g.mu.Lock()
	ms := g.clock.Now().UnixMilli()
	switch {
	case ms > g.lastMs:
		g.lastMs = ms
		g.seq = binary.BigEndian.Uint16(u[6:8]) & 0x07ff // leave headroom before overflow
	case g.seq < 0x0fff:
		g.seq++
	default:
		g.lastMs++
		g.seq = 0
	}
	ms, seq := g.lastMs, g.seq
	g.mu.Unlock()

	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)
	u[6] = 0x70 | byte(seq>>8)
	u[7] = byte(seq)
	u[8] = u[8]&0x3f | 0x80
	return u
}
