package site

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync/atomic"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

// server is the booted module: templates, routes and the page cache.
type server struct {
	cfg        *Config
	svc        *kernel.Services
	pages      *pages.Service
	routes     []*route
	eng        atomic.Pointer[engine]
	cache      *pageCache
	dev        bool
	log        *slog.Logger
	singletons []string // singleton types, sorted
	proxies    []netip.Prefix
	loadErr    error // the templates did not parse; fatal outside dev
	ext        *Extensions
	src        *sources
	custom     *customSet
}

func newServer(cfg *Config, ext *Extensions, s *kernel.Services, ps *pages.Service) (*server, error) {
	routes, err := parseRoutes(cfg.Routes)
	if err != nil {
		return nil, fmt.Errorf("site: modules.site.routes: %w", err)
	}
	if err := checkRoutes(routes, s.Schema); err != nil {
		return nil, err
	}
	var proxies []netip.Prefix
	for _, p := range s.Core.HTTP.TrustedProxies {
		if pf, err := netip.ParsePrefix(p); err == nil {
			proxies = append(proxies, pf.Masked())
		} else if a, err := netip.ParseAddr(p); err == nil {
			proxies = append(proxies, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
		}
	}
	chain, err := ext.chain(cfg.Theme)
	if err != nil {
		return nil, err
	}
	var rendered []*channel.Channel
	for _, ch := range s.Channels.All() {
		if renders(ch) {
			rendered = append(rendered, ch)
		}
	}
	custom, err := buildCustom(ext.routes, rendered)
	if err != nil {
		return nil, err
	}
	srv := &server{proxies: proxies, cfg: cfg, svc: s, pages: ps, routes: routes, log: s.Log, dev: s.Env == config.DevEnv,
		ext: ext, custom: custom, src: &sources{chain: chain, dir: cfg.Dir, comps: ext.comps}}
	if cfg.CacheBytes > 0 {
		srv.cache = newPageCache(cfg.CacheBytes)
	}
	for _, t := range s.Schema.Types() {
		if t.Singleton {
			srv.singletons = append(srv.singletons, t.Name)
		}
	}
	served := false
	for _, ch := range s.Channels.All() {
		served = served || renders(ch)
	}
	if !served {
		return srv, nil
	}
	e, err := loadEngine(srv.src, false)
	if err != nil {
		srv.loadErr = err
		return srv, nil
	}
	srv.eng.Store(e)
	return srv, nil
}

// checkRoutes validates routes against the schema: the type must be a collection and a {param} one of its
// scalar fields (or id).
func checkRoutes(routes []*route, s *schema.Schema) error {
	var errs []error
	for _, rt := range routes {
		t, ok := s.Type(rt.Type)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("site: route %s: unknown type %q", rt.Path, rt.Type))
			continue
		case t.Singleton:
			errs = append(errs, fmt.Errorf("site: route %s: %s is a singleton; its fields are in .Settings", rt.Path, rt.Type))
			continue
		}
		if rt.param == "" || rt.param == "id" {
			continue
		}
		f, ok := t.Field(rt.param)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("site: route %s: type %s has no field %q", rt.Path, rt.Type, rt.param))
		case f.Shape.Base != schema.BaseString:
			errs = append(errs, fmt.Errorf("site: route %s: field %s.%s is not a string", rt.Path, rt.Type, rt.param))
		}
	}
	return errors.Join(errs...)
}

// engine returns the current templates; in dev it reloads them when files under the site directory change.
func (s *server) engine() *engine {
	e := s.eng.Load()
	if e == nil && s.dev && s.loadErr != nil {
		ne, err := loadEngine(s.src, false)
		if err != nil {
			s.loadErr = err
			return nil
		}
		s.loadErr = nil
		s.eng.Store(ne)
		return ne
	}
	if e == nil || !s.dev {
		return e
	}
	sig := dirSignature(s.cfg.Dir)
	if sig == e.sig {
		return e
	}
	ne, err := loadEngine(s.src, false)
	if err != nil {
		s.log.Error("site: templates not reloaded", "err", err)
		kept := *e
		kept.sig = sig
		s.eng.Store(&kept)
		return &kept
	}
	s.eng.Store(ne)
	if s.cache != nil {
		s.cache.clear()
	}
	s.log.Info("site: templates reloaded")
	return ne
}

// start applies cache purges until ctx ends. The delivery cache is purged first: a page must not be rebuilt
// from a stale delivery answer after its own entry is gone.
func (s *server) start(ctx context.Context) error {
	s.warnSameOrigin()
	if s.loadErr != nil && !s.dev {
		return fmt.Errorf("site: %w", s.loadErr)
	}
	if s.cache == nil || (s.eng.Load() == nil && s.loadErr == nil) {
		return nil
	}
	sub, err := s.svc.DB.Notifier().Listen(ctx, cachetag.PurgeChannel)
	if err != nil {
		return err
	}
	go func() {
		for n := range sub {
			tags := strings.Fields(n.Payload)
			if len(tags) == 0 {
				tags = []string{cachetag.All}
			}
			s.svc.Delivery.Purge(tags...)
			s.cache.purge(tags...)
		}
	}()
	return nil
}

// detail is the route that shows one item of a type, preferring the earliest configured.
func (s *server) detail(typ string) *route {
	for _, rt := range s.routes {
		if rt.Type == typ && rt.param != "" {
			return rt
		}
	}
	return nil
}

// warnSameOrigin tells when a previewed page shares its origin with the admin: the page's scripts could then
// reach the admin's session.
func (s *server) warnSameOrigin() {
	for _, ch := range s.svc.Channels.All() {
		if !renders(ch) {
			continue
		}
		fe, err := normalizeOrigin(strings.TrimRight(ch.Frontend, "/"))
		if ch.Frontend == "" || err != nil {
			continue
		}
		if s.cfg.AdminURL == "" || fe == s.cfg.AdminURL {
			s.log.Warn("site: the website shares the admin's origin; its scripts are trusted like project code and the "+
				"admin locks its preview frame. For production, serve the website from a separate host or port and set "+
				"modules.site.admin_url", "channel", ch.Name)
			return
		}
	}
}
