package kernel

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// AuthConfig is the auth section of app.yaml: management sessions.
type AuthConfig struct {
	// SessionIdle ends a session after this long without a request (sliding). Default 12h.
	SessionIdle time.Duration `yaml:"session_idle"`
	// SessionMax ends a session this long after login, however active. Default 7 days.
	SessionMax time.Duration `yaml:"session_max"`
	// InsecureCookies drops the Secure flag of the session cookie, for plain-HTTP development. Only allowed in dev.
	InsecureCookies bool `yaml:"insecure_cookies"`
}

func (c *AuthConfig) check(dev bool) error {
	switch {
	case c.SessionIdle < 0:
		return fmt.Errorf("auth.session_idle must not be negative")
	case c.SessionMax < 0:
		return fmt.Errorf("auth.session_max must not be negative")
	case c.InsecureCookies && !dev:
		return fmt.Errorf("auth.insecure_cookies is only allowed in the %s environment", config.DevEnv)
	}
	return nil
}

// trustedProxies parses http.trusted_proxies: CIDRs or single addresses.
func trustedProxies(list []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(list))
	for _, s := range list {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, aerr := netip.ParseAddr(s)
			if aerr != nil {
				return nil, fmt.Errorf("http.trusted_proxies: %q is not a CIDR or an address", s)
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

// addCoreLimitations registers collection, locale, owner and channel next to the modules' limitations.
func addCoreLimitations(reg *Registry, sc *schema.Schema, chans *channel.Set) error {
	for _, l := range access.CoreLimitations(sc, chans) {
		if err := reg.accessCatalog().AddLimitation(l); err != nil {
			return fmt.Errorf("limitation %q: %w", l.Name(), err)
		}
	}
	return nil
}

// cleanupSessions is the auth.cleanup job: it deletes expired sessions.
func cleanupSessions(ctx context.Context, s *Services) error {
	return s.DB.Tx(ctx, func(tx store.Tx) error {
		n, err := tx.Auth().DeleteExpiredSessions(ctx, s.Clock.Now())
		if err == nil && n > 0 {
			s.Log.Info("expired sessions removed", "count", n)
		}
		return err
	})
}
