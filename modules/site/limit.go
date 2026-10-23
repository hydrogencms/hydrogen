package site

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Limit bounds how often a route answers non-GET requests, counted per fixed window in the database, so servers
// that share it share the count. A request over a limit is answered 429 with Retry-After. A window is fixed,
// not sliding: a client can send up to twice the limit around a window boundary.
type Limit struct {
	// PerClient is the most requests one client address makes in a window (an IPv6 client counts by /64). The
	// address is the peer's, or the one http.trusted_proxies resolves from X-Forwarded-For.
	PerClient int
	// Total is the most requests all clients together make in a window.
	Total int
	// Window is the length of a window, in whole seconds (at least one).
	Window time.Duration
}

func (l Limit) validate() error {
	switch {
	case l.PerClient < 0 || l.Total < 0:
		return fmt.Errorf("limits must not be negative")
	case l.PerClient == 0 && l.Total == 0:
		return fmt.Errorf("set PerClient or Total")
	case l.Window < time.Second:
		return fmt.Errorf("the window must be at least one second")
	}
	return nil
}

// Limit rate-limits the route's non-GET requests.
func (r *CustomRoute) Limit(l Limit) *CustomRoute {
	if err := l.validate(); err != nil && r.err == nil {
		r.err = fmt.Errorf("site: custom route %q: Limit: %w", r.pattern, err)
	}
	r.limit = &l
	return r
}

const limitJob = "site.limits"

// pruneLimits deletes the counters of windows that ended.
func pruneLimits(ctx context.Context, s *kernel.Services) error {
	now := s.Clock.Now().Unix()
	return s.DB.Tx(ctx, func(tx store.Tx) error {
		_, err := tx.SQL().Exec(ctx, `DELETE FROM site_limit WHERE expires_at < ?`, now)
		return err
	})
}

// clientBucket is the address a limit counts a client by.
func clientBucket(ip string) string {
	if a, err := netip.ParseAddr(ip); err == nil && a.Is6() {
		if p, err := a.Prefix(64); err == nil {
			return p.String()
		}
	}
	return ip
}

// limited counts the request against the route's limit and returns how long the client must wait when it is over.
func (s *server) limited(ctx context.Context, r *http.Request, rt *CustomRoute, channel string) (time.Duration, error) {
	l := rt.limit
	secs := int64(l.Window / time.Second)
	now := s.clock.Now().Unix()
	start := now / secs * secs
	base := channel + "|" + rt.pattern
	type bucket struct {
		key string
		max int
	}
	var buckets []bucket
	if l.PerClient > 0 {
		buckets = append(buckets, bucket{base + "|c|" + clientBucket(s.svc.Auth.ClientIP(r)), l.PerClient})
	}
	if l.Total > 0 {
		buckets = append(buckets, bucket{base + "|*", l.Total})
	}
	over := false
	err := s.svc.DB.Tx(ctx, func(tx store.Tx) error {
		for _, b := range buckets {
			var hits int64
			err := tx.SQL().QueryRow(ctx, `INSERT INTO site_limit (bucket, window_start, hits, expires_at) VALUES (?, ?, 1, ?)
ON CONFLICT (bucket, window_start) DO UPDATE SET hits = site_limit.hits + 1 RETURNING hits`, b.key, start, start+secs).Scan(&hits)
			if err != nil {
				return err
			}
			over = over || hits > int64(b.max)
		}
		return nil
	})
	if err != nil || !over {
		return 0, err
	}
	return time.Duration(start+secs-now) * time.Second, nil
}
