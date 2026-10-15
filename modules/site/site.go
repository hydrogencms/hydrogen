// Package site is the fullstack module: it renders a website with Go templates for the web channels that set
// `render: go`, from the same routing, delivery and preview layers any external frontend uses. A project's
// templates (site/templates) override the embedded starter theme by file name.
package site

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

const (
	optRender  = "render"
	bridgeURL  = "/__hydrogen/bridge.js"
	assetsURL  = "/__hydrogen/assets/"
	cookieName = "hydrogen-preview"
)

//go:embed all:themes
var themeFS embed.FS

// bridgeJS is packages/bridge/dist/bridge.iife.js; `make site-bridge` refreshes it.
//
//go:embed bridge.iife.js
var bridgeJS []byte

// Config is modules.site.
type Config struct {
	// Dir holds templates/, assets/ and translations/ (default "site", relative to the working directory).
	Dir string `yaml:"dir"`
	// AdminURL is the admin's origin (scheme://host[:port]) when it is not the site's own.
	AdminURL string `yaml:"admin_url"`
	// PreviewCSP sends a strict Content-Security-Policy with preview pages (default true); turn it off for
	// themes with inline scripts.
	PreviewCSP bool `yaml:"preview_csp"`
	// CacheBytes bounds the rendered page cache; 0 turns it off (default 16 MiB).
	CacheBytes int64 `yaml:"cache_bytes"`
	// Root names the channel that "/" redirects to when no channel serves it (302).
	Root string `yaml:"root"`
	// Routes render collection items that are not placed in the page tree.
	Routes []Route `yaml:"routes"`
}

// Route maps a path inside a channel to a collection type. A path with one {param} (a field of the type, or
// id) shows one item with collections/<template>.html (default: the type name); a path without a parameter
// lists the type with the template named by `template`.
type Route struct {
	Path     string `yaml:"path"`
	Type     string `yaml:"type"`
	Template string `yaml:"template"`
	Sort     string `yaml:"sort"`
	Limit    int    `yaml:"limit"`
}

// Module returns the site module.
func Module() kernel.Module {
	return &module{cfg: Config{Dir: "site", CacheBytes: 16 << 20, PreviewCSP: true}}
}

type module struct {
	cfg   Config
	pages *pages.Service
	srv   *server
}

func (*module) Name() string       { return "site" }
func (*module) Requires() []string { return []string{"pages"} }
func (m *module) Config() any      { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	if err := m.cfg.validate(); err != nil {
		return err
	}
	if m.cfg.AdminURL == "" && r.Env() != config.DevEnv {
		return errors.New("site: modules.site.admin_url is required outside dev (the origin of the admin that previews the site, e.g. https://cms.example.com)")
	}
	svc, ok := kernel.Lookup[*pages.Service](r)
	if !ok {
		return errors.New("the pages module did not provide its service")
	}
	m.pages = svc
	r.ChannelOption(channel.Option{Name: optRender, Decode: decodeRender})
	r.ChannelCheck(m.checkChannels)
	r.Handle("GET "+bridgeURL, http.HandlerFunc(serveBridge))
	r.Handle("GET "+assetsURL, m.handler(func(s *server) http.HandlerFunc { return s.serveAsset }))
	r.Handle("/", m.handler(func(s *server) http.HandlerFunc { return s.serveSite }))
	return r.Command(kernel.Command{Name: "site:check", Summary: "render every page type, collection route and block type against sample data (-db: and published content)",
		Run: m.runCheck})
}

func (m *module) handler(pick func(*server) http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.srv == nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		pick(m.srv)(w, r)
	})
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	srv, err := newServer(&m.cfg, s, m.pages)
	if err != nil {
		return err
	}
	m.srv = srv
	return nil
}

func (m *module) Start(ctx context.Context) error {
	if m.srv == nil {
		return nil
	}
	return m.srv.start(ctx)
}

var (
	routeParam  = regexp.MustCompile(`^\{([A-Za-z][A-Za-z0-9_]*)\}$`)
	templateRE  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	typeNameRE  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	segmentChar = regexp.MustCompile(`^[A-Za-z0-9._~%-]+$`)
)

func (c *Config) validate() error {
	if c.Dir == "" {
		return errors.New("site: modules.site.dir must not be empty")
	}
	if c.AdminURL != "" {
		o, err := normalizeOrigin(c.AdminURL)
		if err != nil {
			return fmt.Errorf("site: modules.site.admin_url %q must be an origin such as https://cms.example.com", c.AdminURL)
		}
		c.AdminURL = o
	}
	if c.CacheBytes < 0 {
		return errors.New("site: modules.site.cache_bytes must not be negative")
	}
	var errs []error
	seen := map[string]bool{}
	for i, rt := range c.Routes {
		if _, err := parseRoute(rt); err != nil {
			errs = append(errs, fmt.Errorf("site: modules.site.routes[%d]: %w", i, err))
			continue
		}
		if seen[rt.Path] {
			errs = append(errs, fmt.Errorf("site: modules.site.routes[%d]: path %s is listed twice", i, rt.Path))
		}
		seen[rt.Path] = true
	}
	return errors.Join(errs...)
}

func decodeRender(n *config.Node) (any, error) {
	s, err := n.String()
	if err != nil || s != "go" {
		return nil, n.Errorf(`render must be "go" (Go templates, the only renderer)`)
	}
	return s, nil
}

// renders reports whether the channel is served by this module.
func renders(ch *channel.Channel) bool {
	v, _ := ch.Options[optRender].(string)
	return v == "go"
}

// checkChannels rejects a rendered channel the module could not serve: it needs the pages module's match and
// public access.
func (m *module) checkChannels(s *channel.Set) error {
	var errs []error
	if root := m.cfg.Root; root != "" {
		if ch, ok := s.Get(root); !ok || !renders(ch) {
			errs = append(errs, fmt.Errorf("modules.site.root %q is not a channel with render: go", root))
		}
	}
	for _, ch := range s.All() {
		if !renders(ch) {
			continue
		}
		if _, ok := ch.Options["match"]; !ok {
			errs = append(errs, fmt.Errorf("channel %s: render: go needs a match (host, prefix or header) to tell its URLs apart", ch.Name))
		}
		if ch.Access != channel.AccessPublic {
			errs = append(errs, fmt.Errorf("channel %s: render: go needs public access", ch.Name))
		}
	}
	return errors.Join(errs...)
}

func serveBridge(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/javascript; charset=utf-8")
	h.Set("Cache-Control", "public, max-age=300")
	h.Set("X-Content-Type-Options", "nosniff")
	etag := etagOf(bridgeJS)
	h.Set("ETag", etag)
	if matchesETag(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	_, _ = w.Write(bridgeJS)
}

var originRE = regexp.MustCompile(`^https?://[A-Za-z0-9.-]+(:[0-9]+)?$`)

// normalizeOrigin lower-cases the host and drops a default port, so the string equals what a browser reports
// as event.origin.
func normalizeOrigin(s string) (string, error) {
	if !originRE.MatchString(s) {
		return "", errors.New("not an origin")
	}
	scheme, host, _ := strings.Cut(s, "://")
	host = strings.ToLower(host)
	if (scheme == "http" && strings.HasSuffix(host, ":80")) || (scheme == "https" && strings.HasSuffix(host, ":443")) {
		host = host[:strings.LastIndex(host, ":")]
	}
	return scheme + "://" + host, nil
}
