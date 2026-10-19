package siteproxy

import (
	"errors"
	"fmt"
	"net/netip"
	"path"
	"strings"
	"time"
)

// Config is modules.site-proxy. Exactly one of static (or an embedded file system passed to WithFS) and ssr is
// used.
type Config struct {
	Static StaticConfig `yaml:"static"`
	SSR    SSRConfig    `yaml:"ssr"`
	// Timeout bounds the wait for the frontend's response headers (default 60s); streaming bodies are not cut.
	Timeout time.Duration `yaml:"timeout"`
}

// StaticConfig serves a directory (a static export of the frontend).
type StaticConfig struct {
	// Dir is the directory to serve, relative to the working directory. Not used with WithFS.
	Dir string `yaml:"dir"`
	// SPAFallback answers a path that matches no file and looks like a page (no file extension) with
	// index.html and status 200.
	SPAFallback bool `yaml:"spa_fallback"`
	// Immutable lists the request paths whose files are fingerprinted and cached for a year. A pattern is a
	// path.Match pattern (`*` stays inside one segment) or ends in `/**` for a whole subtree. Default:
	// /_next/static/**, /_astro/**, /_nuxt/**.
	Immutable []string `yaml:"immutable"`
}

// SSRConfig runs a server-rendering frontend as a child process and proxies to it.
type SSRConfig struct {
	// Command is the argv of the process (no shell); `{port}` and `{host}` in an argument are replaced by the
	// port and the host.
	Command []string `yaml:"command"`
	// Host is the loopback host the process listens on and Hydrogen connects to: an address such as 127.0.0.1
	// (default) or the name localhost. Frameworks that pick their own bind address from it (Next.js reads
	// HOSTNAME) are told with {host}.
	Host string `yaml:"host"`
	// Dir is the working directory of the process (default: the working directory).
	Dir string `yaml:"dir"`
	// InheritEnv passes Hydrogen's whole environment to the process. By default it gets only a safe subset (PATH,
	// HOME, USER, TMPDIR, TZ, LANG, LC_*, NODE_*, NPM_CONFIG_*, SystemRoot on Windows) plus Env, so the CMS's
	// secrets (database URL, storage keys, tokens) never reach frontend code.
	InheritEnv bool `yaml:"inherit_env"`
	// Env adds environment variables; `{port}` and `{host}` are replaced.
	Env map[string]string `yaml:"env"`
	// Health is the path polled until the process answers (any status below 500). Default "/".
	Health string `yaml:"health"`
	// StartTimeout is how long the process may take to answer the health check (default 30s).
	StartTimeout time.Duration `yaml:"start_timeout"`
	// RestartBackoff is the wait before the first restart; it doubles up to RestartBackoffMax (defaults 1s, 30s).
	RestartBackoff    time.Duration `yaml:"restart_backoff"`
	RestartBackoffMax time.Duration `yaml:"restart_backoff_max"`
	// StopTimeout is how long the process gets to exit after SIGTERM before it is killed (default 10s).
	StopTimeout time.Duration `yaml:"stop_timeout"`
}

var defaultImmutable = []string{"/_next/static/**", "/_astro/**", "/_nuxt/**"}

type mode int

const (
	modeStatic mode = iota + 1
	modeSSR
)

func (c *Config) validate(haveFS bool) (mode, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("site-proxy: modules.site-proxy."+format, args...))
	}
	static := c.Static.Dir != "" || haveFS
	ssr := len(c.SSR.Command) > 0
	var m mode
	switch {
	case static && ssr:
		fail("static and ssr are both set: use one")
	case !static && !ssr:
		fail("set static.dir or ssr.command (or pass a file system with siteproxy.WithFS)")
	case static:
		m = modeStatic
	default:
		m = modeSSR
	}
	if haveFS && c.Static.Dir != "" {
		fail("static.dir is set but the project passed an embedded file system: remove one")
	}
	if m == modeStatic {
		if c.Static.Immutable == nil {
			c.Static.Immutable = defaultImmutable
		}
		for i, p := range c.Static.Immutable {
			if _, err := path.Match(strings.TrimSuffix(p, "**"), ""); err != nil || !strings.HasPrefix(p, "/") {
				fail("static.immutable[%d] %q must be a path pattern starting with /", i, p)
			}
		}
	}
	if m == modeSSR {
		s := &c.SSR
		if s.Command[0] == "" {
			fail("ssr.command[0] must name the program")
		}
		if s.Host == "" {
			s.Host = "127.0.0.1"
		}
		if a, err := netip.ParseAddr(s.Host); s.Host != "localhost" && (err != nil || !a.IsLoopback()) {
			fail("ssr.host %q must be a loopback address or localhost: the frontend is reached only through Hydrogen", s.Host)
		}
		if s.Health == "" {
			s.Health = "/"
		}
		if !strings.HasPrefix(s.Health, "/") {
			fail("ssr.health %q must be a path starting with /", s.Health)
		}
		def := func(d *time.Duration, v time.Duration, name string) {
			switch {
			case *d < 0:
				fail("ssr.%s must not be negative", name)
			case *d == 0:
				*d = v
			}
		}
		def(&s.StartTimeout, 30*time.Second, "start_timeout")
		def(&s.RestartBackoff, time.Second, "restart_backoff")
		def(&s.RestartBackoffMax, 30*time.Second, "restart_backoff_max")
		def(&s.StopTimeout, 10*time.Second, "stop_timeout")
		if s.RestartBackoffMax < s.RestartBackoff {
			fail("ssr.restart_backoff_max must not be below restart_backoff")
		}
	}
	if c.Timeout < 0 {
		fail("timeout must not be negative")
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	return m, errors.Join(errs...)
}
