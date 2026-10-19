// Package siteproxy serves a JavaScript frontend behind the Hydrogen binary: a static export from a directory
// or an embedded file system, or a server-rendering process (Next.js standalone, Nuxt, Astro, ...) that the
// module starts, supervises and reverse-proxies to. The kernel's own routes (admin, /api/manage, /api/deliver,
// /api/x, /assets, /images, /healthz, other modules' routes) answer first; the frontend gets every other
// request. See docs/site-proxy.md.
package siteproxy

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

// Option customizes the module.
type Option func(*module)

// WithFS serves the static export from fsys (typically an embed.FS reduced with fs.Sub) instead of
// modules.site-proxy.static.dir. spa_fallback and immutable still come from the configuration.
func WithFS(fsys fs.FS) Option { return func(m *module) { m.fsys = fsys } }

// Module returns the site-proxy module.
func Module(opts ...Option) kernel.Module {
	m := &module{}
	for _, o := range opts {
		o(m)
	}
	return m
}

type module struct {
	cfg  Config
	fsys fs.FS
	mode mode
	h    http.Handler
	sup  *supervisor
	prx  *proxy
}

func (*module) Name() string  { return "site-proxy" }
func (m *module) Config() any { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	md, err := m.cfg.validate(m.fsys != nil)
	if err != nil {
		return err
	}
	m.mode = md
	r.ChannelCheck(checkChannels)
	// "/" is the least specific pattern of the kernel's mux: it only receives what nothing else claims.
	r.Handle("/", http.HandlerFunc(m.serve))
	return nil
}

func (m *module) serve(w http.ResponseWriter, r *http.Request) {
	h := m.h
	if h == nil {
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	h.ServeHTTP(w, r)
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	switch m.mode {
	case modeStatic:
		fsys := m.fsys
		if fsys == nil {
			fi, err := os.Stat(m.cfg.Static.Dir)
			if err != nil || !fi.IsDir() {
				return fmt.Errorf("site-proxy: modules.site-proxy.static.dir %q is not a directory (build the frontend's static export first)", m.cfg.Static.Dir)
			}
			fsys = os.DirFS(m.cfg.Static.Dir)
		}
		m.h = &staticHandler{fsys: fsys, spa: m.cfg.Static.SPAFallback, immutable: m.cfg.Static.Immutable}
	case modeSSR:
		m.sup = newSupervisor(m.cfg.SSR, s.Log)
		m.prx = newProxy(m.sup.Target, m.cfg.Timeout, parsePrefixes(s.Core.HTTP.TrustedProxies), s.Log)
		m.sup.onChange = m.prx.transport.CloseIdleConnections
		m.h = m.prx
	}
	return nil
}

func (m *module) Start(ctx context.Context) error {
	if m.sup != nil {
		m.sup.Start(ctx)
	}
	return nil
}

func (m *module) Stop(ctx context.Context) error {
	if m.sup == nil {
		return nil
	}
	// New requests get 503, running ones finish (bounded by stop_timeout and the shutdown deadline), then the
	// process is stopped.
	dctx, cancel := context.WithTimeout(ctx, m.cfg.SSR.StopTimeout)
	defer cancel()
	m.prx.drain(dctx)
	return m.sup.Stop(ctx)
}

// checkChannels rejects channels the site module renders: the frontend and the site module would both claim
// the website's URLs.
func checkChannels(s *channel.Set) error {
	var errs []error
	for _, ch := range s.All() {
		if v, _ := ch.Options["render"].(string); v == "go" {
			errs = append(errs, fmt.Errorf("channel %s: render: go (the site module) cannot be combined with the site-proxy module; the frontend would serve the same URLs", ch.Name))
		}
	}
	return errors.Join(errs...)
}
