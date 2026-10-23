package site

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
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
	clock      clock.Clock
	checking   sync.Mutex   // one dev check of the site directory at a time
	nextCheck  atomic.Int64 // unix nanoseconds before which dev does not check the site directory again
	log        *slog.Logger
	singletons []string // singleton types, sorted
	proxies    []netip.Prefix
	loadErr    error // the templates could not be loaded, or some do not parse outside dev; fatal outside dev
	ext        *Extensions
	src        *sources
	custom     *customSet
	guard      *formGuard
}

func newServer(cfg *Config, ext *Extensions, s *kernel.Services, ps *pages.Service) (*server, error) {
	routes, err := parseRoutes(cfg.Routes)
	if err != nil {
		return nil, fmt.Errorf("site: modules.site.routes: %w", err)
	}
	if err := checkRoutes(routes, s.Schema); err != nil {
		return nil, err
	}
	for _, name := range cfg.Submissions {
		if t, ok := s.Schema.Type(name); !ok || t.Singleton {
			return nil, fmt.Errorf("site: modules.site.submissions: %q is not a collection type", name)
		}
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
	srv := &server{proxies: proxies, cfg: cfg, svc: s, pages: ps, routes: routes, log: s.Log, dev: s.Env == config.DevEnv, clock: s.Clock,
		ext: ext, custom: custom, src: &sources{chain: chain, dir: cfg.Dir, comps: ext.comps, funcs: ext.funcs}}
	secret := s.Core.Delivery.PreviewSecret
	if secret == "" {
		secret = s.Core.Manage.Token
	}
	srv.guard = newFormGuard(secret, cfg.Forms, s.Clock)
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
	e, err := loadEngine(srv.src, false, nil)
	if e == nil || (err != nil && !srv.dev) {
		srv.loadErr = err
		return srv, nil
	}
	srv.logParseErrors(e)
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

// devCheckEvery bounds how often dev walks the site directory for changes: once per request burst, not per
// request.
const devCheckEvery = 250 * time.Millisecond

// engine returns the current templates; in dev it reloads them when files under the site directory change,
// checking at most every devCheckEvery. A file that no longer parses keeps its last good version.
func (s *server) engine() *engine {
	e := s.eng.Load()
	if !s.dev {
		return e
	}
	now := s.clock.Now().UnixNano()
	if now < s.nextCheck.Load() || !s.checking.TryLock() {
		return e
	}
	defer s.checking.Unlock()
	if now < s.nextCheck.Load() {
		return s.eng.Load()
	}
	defer func() { s.nextCheck.Store(s.clock.Now().Add(devCheckEvery).UnixNano()) }()
	e = s.eng.Load()
	if e == nil && s.loadErr != nil {
		ne, err := loadEngine(s.src, false, nil)
		if ne == nil {
			s.loadErr = err
			return nil
		}
		s.loadErr = nil
		s.logParseErrors(ne)
		s.eng.Store(ne)
		return ne
	}
	if e == nil {
		return nil
	}
	sig := dirSignature(s.cfg.Dir)
	if sig == e.sig {
		return e
	}
	ne, err := loadEngine(s.src, false, e)
	if ne == nil {
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
	if !s.logParseErrors(ne) {
		s.log.Info("site: templates reloaded")
	}
	return ne
}

// logParseErrors logs each template that does not parse; true when there is one.
func (s *server) logParseErrors(e *engine) bool {
	for _, n := range sortedKeys(e.errs) {
		kept := "pages that use it fail"
		if _, ok := e.good[n]; ok {
			kept = "serving its last good version"
		}
		s.log.Error("site: template does not parse; "+kept, "err", e.errs[n])
	}
	return len(e.errs) > 0
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

// linkFields lists the fields the detail route of typ builds its URLs from, or of every detail route of the
// channel's collections for "".
func (s *server) linkFields(ch *channel.Channel, typ string) []string {
	var out []string
	for _, rt := range s.routes {
		if rt.param != "" && rt.param != "id" && (typ == "" || rt.Type == typ) && ch.AllowsCollection(rt.Type) && !slices.Contains(out, rt.param) {
			out = append(out, rt.param)
		}
	}
	slices.Sort(out)
	return out
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
