package meilisearch

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Config is modules.meilisearch.
type Config struct {
	// URL is the Meilisearch server, e.g. http://127.0.0.1:7700.
	URL string `yaml:"url"`
	// Key is an API key with access to the index (the master key works). Required outside dev.
	Key string `yaml:"key" config:"secret"`
	// Index is the uid of the index; <index>_meta and <index>_next are used too.
	Index string `yaml:"index"`
	// Timeout bounds every HTTP request.
	Timeout time.Duration `yaml:"timeout"`
	// TaskTimeout bounds the wait for one Meilisearch task (a document batch, a settings change, a swap).
	TaskTimeout time.Duration `yaml:"task_timeout"`
}

var indexUID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

func defaults() Config {
	return Config{Index: "hydrogen", Timeout: 10 * time.Second, TaskTimeout: 10 * time.Minute}
}

// validate checks the config; dev is true in the dev environment, where the key may be empty.
func (c *Config) validate(dev bool) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("modules.meilisearch."+format, args...))
	}
	if u, err := url.Parse(c.URL); c.URL == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		fail("url must be an http(s) URL, got %q", c.URL)
	}
	if c.Key == "" && !dev {
		fail("key is required outside dev")
	}
	if !indexUID.MatchString(c.Index) {
		fail("index %q must be 1-100 letters, digits, - or _", c.Index)
	}
	if c.Timeout <= 0 {
		fail("timeout must be positive")
	}
	if c.TaskTimeout <= 0 {
		fail("task_timeout must be positive")
	}
	return errors.Join(errs...)
}

func (c *Config) base() string { return strings.TrimRight(c.URL, "/") }
