package images

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
)

const (
	driverBuiltin  = "builtin"
	driverImgproxy = "imgproxy"

	// AdminPreset is the built-in preset of the admin's thumbnails; it is never delivered.
	AdminPreset = "admin"

	// DefaultMaxPixels bounds the decoded size of a source.
	DefaultMaxPixels = 50_000_000

	// URLBase is where the module serves variants.
	URLBase = "/images"
)

// Config is modules.images.
type Config struct {
	// Driver is builtin (default, in-process) or imgproxy.
	Driver string `yaml:"driver"`
	// PublicURL is the base of variant URLs when a CDN fronts /images (empty: this server).
	PublicURL string `yaml:"public_url"`
	// MaxPixels is the largest source (width × height) that gets variants.
	MaxPixels int64 `yaml:"max_pixels"`
	// Workers bounds concurrent transforms; 0 is max(1, GOMAXPROCS/2).
	Workers  int                     `yaml:"workers"`
	Presets  map[string]PresetConfig `yaml:"presets"`
	Imgproxy ImgproxyConfig          `yaml:"imgproxy"`
}

// PresetConfig is one entry of modules.images.presets.
type PresetConfig struct {
	Width   int    `yaml:"width"`
	Height  int    `yaml:"height"`
	Fit     string `yaml:"fit"`
	Widths  []int  `yaml:"widths"`
	Format  string `yaml:"format"`
	Quality int    `yaml:"quality"`
	Upscale bool   `yaml:"upscale"`
}

// ImgproxyConfig configures driver imgproxy.
type ImgproxyConfig struct {
	URL string `yaml:"url"`
	// Key and Salt are hex, as imgproxy's IMGPROXY_KEY and IMGPROXY_SALT.
	Key  string `yaml:"key" config:"secret"`
	Salt string `yaml:"salt" config:"secret"`
	// Source is where imgproxy reads originals, with {sha} and {name}.
	Source string `yaml:"source"`
}

// setup is a validated Config.
type setup struct {
	driver    string
	publicURL string
	maxPixels int64
	workers   int
	presets   map[string]Preset
	names     []string // delivered presets (all but admin), sorted
	proxy     Imgproxy
}

func (c *Config) build(procs int) (*setup, error) {
	s := &setup{driver: c.Driver, maxPixels: c.MaxPixels, workers: c.Workers, presets: map[string]Preset{}}
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf("modules.images."+format, args...)) }
	switch s.driver {
	case "":
		s.driver = driverBuiltin
	case driverBuiltin, driverImgproxy:
	default:
		fail("driver %q is not builtin or imgproxy", c.Driver)
		s.driver = driverBuiltin
	}
	if c.PublicURL != "" {
		if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			fail("public_url %q must be an http(s) URL", c.PublicURL)
		}
		s.publicURL = strings.TrimRight(c.PublicURL, "/")
	}
	if s.maxPixels == 0 {
		s.maxPixels = DefaultMaxPixels
	}
	if s.maxPixels < 0 {
		fail("max_pixels must be positive")
	}
	if s.workers < 0 {
		fail("workers must not be negative")
	}
	if s.workers == 0 {
		s.workers = max(1, procs/2)
	}
	s.presets[AdminPreset] = Preset{Name: AdminPreset, Width: 480, Height: 480, Fit: Contain}
	keys := make([]string, 0, len(c.Presets))
	for name := range c.Presets {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		pc := c.Presets[name]
		p := Preset{Name: name, Width: pc.Width, Height: pc.Height, Fit: pc.Fit, Format: pc.Format, Quality: pc.Quality,
			Upscale: pc.Upscale, Widths: slices.Clone(pc.Widths)}
		slices.Sort(p.Widths)
		p.Widths = slices.Compact(p.Widths)
		if len(p.Widths) == 0 {
			p.Widths = nil
		}
		if err := p.Validate(s.driver); err != nil {
			fail("presets.%s: %v", name, err)
			continue
		}
		s.presets[name] = p
		if name != AdminPreset {
			s.names = append(s.names, name)
		}
	}
	errs = append(errs, s.buildImgproxy(c)...)
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *setup) buildImgproxy(c *Config) []error {
	ip := c.Imgproxy
	if s.driver != driverImgproxy {
		if ip != (ImgproxyConfig{}) {
			return []error{errors.New("modules.images.imgproxy belongs to driver imgproxy (driver is builtin)")}
		}
		return nil
	}
	var errs []error
	if u, err := url.Parse(ip.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("modules.images.imgproxy.url %q must be an http(s) URL", ip.URL))
	}
	key, err := hex.DecodeString(ip.Key)
	if err != nil || len(key) == 0 {
		errs = append(errs, errors.New("modules.images.imgproxy.key must be hex (use ${env:IMGPROXY_KEY})"))
	}
	salt, err := hex.DecodeString(ip.Salt)
	if err != nil || len(salt) == 0 {
		errs = append(errs, errors.New("modules.images.imgproxy.salt must be hex (use ${env:IMGPROXY_SALT})"))
	}
	if !strings.Contains(ip.Source, "{sha}") {
		errs = append(errs, errors.New("modules.images.imgproxy.source must contain {sha} (e.g. https://cms.example.com/assets/{sha}/{name})"))
	}
	s.proxy = Imgproxy{Base: ip.URL, Key: key, Salt: salt, Source: ip.Source}
	return errs
}
