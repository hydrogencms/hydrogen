package siteexport

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Config is modules.site-export.
type Config struct {
	// Out is the working tree: the export is written to <out>/<host or channel name>/<path>/index.html with
	// manifest.json at its root. Publishers deliver this tree. Relative to the working directory (default
	// "site-export"); `site:export -out` overrides it for one run.
	Out string `yaml:"out"`
	// Redirects lists the redirect formats written next to each host's files: netlify (_redirects, also read by
	// Cloudflare Pages), nginx (redirects.nginx.conf), s3 (s3-routing-rules.json) and html (a refresh page at
	// the old path, the portable fallback). Default [html].
	Redirects []string `yaml:"redirects"`
	// Auto re-exports incrementally whenever published content changes (an outbox consumer in the serving
	// process); the work tree is created by the first event when no export exists yet.
	Auto bool `yaml:"auto"`
	// Concurrency bounds parallel rendering, writing and uploading (default 8).
	Concurrency int `yaml:"concurrency"`
	// Publisher delivers the tree after each export; at most one kind is set. Without one the tree in Out
	// is the result.
	Publisher PublisherConfig `yaml:"publisher"`
}

// PublisherConfig holds exactly one publisher.
type PublisherConfig struct {
	Dir     DirConfig     `yaml:"dir"`
	S3      S3Config      `yaml:"s3"`
	Command CommandConfig `yaml:"command"`
}

// DirConfig publishes into versioned directories <path>/releases/<name> and points the symlink <path>/current at
// the newest one (an atomic rename), so a web server whose root is <path>/current never sees a half-written site.
type DirConfig struct {
	Path string `yaml:"path"`
	// Keep is how many releases stay (default 3, at least 1); older ones are removed.
	Keep int `yaml:"keep"`
}

// S3Config publishes to an S3-compatible bucket (AWS S3, MinIO, Cloudflare R2, ...): changed files are
// uploaded, files that left the export are deleted, then PurgeCommand runs. The state of the bucket is a
// file in it (.hydrogen-export-state.json), so any machine can publish.
type S3Config struct {
	Bucket    string `yaml:"bucket"`
	Region    string `yaml:"region"`
	Endpoint  string `yaml:"endpoint"`
	PathStyle bool   `yaml:"path_style"`
	// Prefix is prepended to every key ("site/").
	Prefix       string `yaml:"prefix"`
	AccessKey    string `yaml:"access_key" config:"secret"`
	SecretKey    string `yaml:"secret_key" config:"secret"`
	SessionToken string `yaml:"session_token" config:"secret"`
	// PurgeCommand is an argv (no shell) run after the upload, to purge a CDN. The environment carries
	// HYDROGEN_EXPORT_DIR and HYDROGEN_EXPORT_CHANGED, a file listing the keys that changed or were removed.
	PurgeCommand []string      `yaml:"purge_command"`
	Timeout      time.Duration `yaml:"timeout"`
}

// CommandConfig runs a program after each export. The output directory is appended as the last argument; an
// argument containing {dir} gets it substituted instead. The environment carries HYDROGEN_EXPORT_DIR.
type CommandConfig struct {
	Argv    []string      `yaml:"argv"`
	Timeout time.Duration `yaml:"timeout"`
}

var redirectFormats = []string{"netlify", "nginx", "s3", "html"}

func (c *Config) validate() error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("site-export: modules.site-export."+format, args...))
	}
	if c.Out == "" {
		fail("out must not be empty")
	}
	if c.Concurrency < 0 || c.Concurrency > 64 {
		fail("concurrency %d must be between 1 and 64", c.Concurrency)
	}
	if c.Concurrency == 0 {
		c.Concurrency = 8
	}
	seen := map[string]bool{}
	for _, f := range c.Redirects {
		if !slices.Contains(redirectFormats, f) {
			fail("redirects: unknown format %q (%s)", f, strings.Join(redirectFormats, ", "))
		}
		if seen[f] {
			fail("redirects: %q listed twice", f)
		}
		seen[f] = true
	}
	p := &c.Publisher
	set := 0
	if p.Dir.Path != "" || p.Dir.Keep != 0 {
		set++
		if p.Dir.Path == "" {
			fail("publisher.dir.path is required")
		}
		if p.Dir.Keep < 0 {
			fail("publisher.dir.keep must not be negative")
		}
		if p.Dir.Keep == 0 {
			p.Dir.Keep = 3
		}
	}
	if s := &p.S3; s.Bucket != "" || s.AccessKey != "" || s.SecretKey != "" || s.Endpoint != "" {
		set++
		if s.Bucket == "" {
			fail("publisher.s3.bucket is required")
		}
		if s.AccessKey == "" || s.SecretKey == "" {
			fail("publisher.s3.access_key and secret_key are required (use ${env:…})")
		}
		if s.Endpoint != "" {
			if u, err := url.Parse(s.Endpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				fail("publisher.s3.endpoint %q must be an http(s) URL", s.Endpoint)
			}
		}
		if len(s.PurgeCommand) > 0 && s.PurgeCommand[0] == "" {
			fail("publisher.s3.purge_command[0] must name the program")
		}
		if s.Timeout < 0 {
			fail("publisher.s3.timeout must not be negative")
		}
		if s.Timeout == 0 {
			s.Timeout = 10 * time.Minute
		}
	}
	if cm := &p.Command; len(cm.Argv) > 0 || cm.Timeout != 0 {
		set++
		if len(cm.Argv) == 0 || cm.Argv[0] == "" {
			fail("publisher.command.argv must name the program")
		}
		if cm.Timeout < 0 {
			fail("publisher.command.timeout must not be negative")
		}
		if cm.Timeout == 0 {
			cm.Timeout = 10 * time.Minute
		}
	}
	if set > 1 {
		fail("publisher: set one of dir, s3 and command")
	}
	return errors.Join(errs...)
}
