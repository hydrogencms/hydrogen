package wasmhooks

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/hooks"
)

const (
	DefaultDir     = "hooks"
	DefaultTimeout = 200 * time.Millisecond
	MaxTimeout     = 10 * time.Second
	DefaultMemory  = 32 << 20
	MaxMemory      = 512 << 20
	minMemory      = 1 << 20
	pageSize       = 64 << 10
)

// Config is modules.wasmhooks.
type Config struct {
	// Dir holds the hook files, relative to the project directory.
	Dir string `yaml:"dir"`
	// Timeout is the deadline of one call (default 200ms, at most 10s); a hook may set its own.
	Timeout time.Duration `yaml:"timeout"`
	// Memory limits one instance (default 32MB, at most 512MB).
	Memory config.Size `yaml:"memory"`
	// Concurrency bounds simultaneous calls; 0 is GOMAXPROCS.
	Concurrency int          `yaml:"concurrency"`
	Hooks       []HookConfig `yaml:"hooks"`
}

// HookConfig is one entry of modules.wasmhooks.hooks.
type HookConfig struct {
	File  string   `yaml:"file"`
	Event string   `yaml:"event"`
	Types []string `yaml:"types"`
	// Async runs the hook after commit from the outbox (afterPublish and afterDelete only; needs Name).
	Async bool   `yaml:"async"`
	Name  string `yaml:"name"`
	// Timeout overrides the module's for this hook.
	Timeout time.Duration `yaml:"timeout"`
}

// setup is a validated Config.
type setup struct {
	dir         string
	timeout     time.Duration
	pages       uint32
	concurrency int
	hooks       []hookSpec
}

type hookSpec struct {
	file    string // path as configured, relative to dir
	event   hooks.Event
	types   []string
	async   bool
	name    string
	timeout time.Duration
}

var events = []hooks.Event{hooks.BeforeValidate, hooks.Validate, hooks.BeforeSave, hooks.BeforePublish, hooks.AfterPublish,
	hooks.BeforeDelete, hooks.AfterDelete, hooks.BeforeUnpublish, hooks.AfterUnpublish}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func defaultConfig() Config {
	return Config{Dir: DefaultDir, Timeout: DefaultTimeout, Memory: DefaultMemory}
}

func (c *Config) build() (*setup, error) {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("modules.wasmhooks."+format, args...))
	}
	s := &setup{dir: c.Dir, timeout: c.Timeout, concurrency: c.Concurrency}
	if s.dir == "" {
		s.dir = DefaultDir
	}
	if s.timeout == 0 {
		s.timeout = DefaultTimeout
	}
	if s.timeout < 0 || s.timeout > MaxTimeout {
		fail("timeout %s must be between 1ms and %s", c.Timeout, MaxTimeout)
	}
	mem := int64(c.Memory)
	if mem == 0 {
		mem = DefaultMemory
	}
	if mem < minMemory || mem > MaxMemory {
		fail("memory must be between 1MB and 512MB")
	}
	s.pages = uint32((mem + pageSize - 1) / pageSize)
	if s.concurrency < 0 {
		fail("concurrency must not be negative")
	}
	if s.concurrency == 0 {
		s.concurrency = runtime.GOMAXPROCS(0)
	}
	names := map[string]int{}
	for i, h := range c.Hooks {
		hfail := func(format string, args ...any) { fail("hooks[%d]: "+format, append([]any{i}, args...)...) }
		spec := hookSpec{file: h.File, event: hooks.Event(h.Event), types: slices.Clone(h.Types), async: h.Async, name: h.Name, timeout: h.Timeout}
		switch {
		case h.File == "":
			hfail("file is required")
		case !filepath.IsLocal(filepath.FromSlash(h.File)):
			hfail("file %q must be a relative path inside %s", h.File, s.dir)
		}
		if !slices.Contains(events, spec.event) {
			hfail("event %q is not one of %s", h.Event, joinEvents())
		} else if h.Async && spec.event != hooks.AfterPublish && spec.event != hooks.AfterDelete {
			hfail("%s hooks cannot be async (only afterPublish and afterDelete)", h.Event)
		}
		if h.Async && h.Name == "" {
			hfail("async hooks need a name (it becomes the outbox consumer's name)")
		}
		if h.Name != "" && !nameRe.MatchString(h.Name) {
			hfail("name %q must be letters, digits, '.', '_' or '-'", h.Name)
		}
		if h.Async && h.Name != "" {
			final := []string{h.Name}
			if len(h.Types) > 1 {
				final = nil
				for _, t := range h.Types {
					final = append(final, h.Name+"."+t)
				}
			}
			for _, n := range final {
				if j, dup := names[n]; dup {
					hfail("consumer name %q is already used by hooks[%d]", n, j)
				}
				names[n] = i
			}
		}
		for _, t := range h.Types {
			if t == "" {
				hfail("types must not hold an empty name")
			}
		}
		if len(spec.types) > 1 {
			slices.Sort(spec.types)
			if len(slices.Compact(slices.Clone(spec.types))) != len(spec.types) {
				hfail("types lists a type twice")
			}
		}
		if h.Timeout < 0 || h.Timeout > MaxTimeout {
			hfail("timeout %s must be between 1ms and %s", h.Timeout, MaxTimeout)
		}
		if spec.timeout == 0 {
			spec.timeout = s.timeout
		}
		s.hooks = append(s.hooks, spec)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

func joinEvents() string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = string(e)
	}
	return strings.Join(out, ", ")
}
