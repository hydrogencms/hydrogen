// Package channel compiles delivery channels — websites, apps, integrations — from config/channels.yaml:
// ordered languages, allowed collections, settings, preview URL patterns, access tokens, templates and module
// options (the pages module adds site, match and reserved). Contract: planning/design/channels-h3.md.
package channel

import (
	"crypto/sha256"
	"crypto/subtle"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/schema"
)

// Access values.
const (
	AccessPublic = "public"
	AccessToken  = "token"
)

// DefaultName is the implicit channel when channels.yaml is absent.
const DefaultName = "default"

// Channel is a compiled channel. A Channel, the values Setting and Options return, and the contents of a Set are
// shared by every caller and read-only: never modify them.
type Channel struct {
	Name        string
	Group       string
	Languages   []string // ordered fallback
	Collections []string // nil = all
	Settings    map[string]any
	Frontend    string
	Preview     map[string]string
	Access      string
	Options     map[string]any // module option values by key, as their Decode returned them
}

// AllowsCollection reports whether the channel may read a content type (nil Collections allows all).
func (c *Channel) AllowsCollection(typ string) bool {
	return c.Collections == nil || slices.Contains(c.Collections, typ)
}

// Setting looks up a dotted path in Settings. The result may be a shared map or slice: do not modify it.
func (c *Channel) Setting(path string) (any, bool) {
	var cur any = c.Settings
	for {
		key, rest, more := strings.Cut(path, ".")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[key]; !ok {
			return nil, false
		}
		if !more {
			return cur, true
		}
		path = rest
	}
}

type Language struct {
	Code     string
	Inherits string
}

type tokenEntry struct {
	sum [sha256.Size]byte
	ch  *Channel
}

// Set is a compiled set of channels. It is immutable and shared: the channels it hands out are read-only.
type Set struct {
	channels []*Channel // sorted by name
	byName   map[string]*Channel
	groups   map[string][]*Channel
	langs    map[string]Language
	tokens   []tokenEntry // SHA-256 digests only; no plaintext is kept
}

func (s *Set) All() []*Channel { return slices.Clone(s.channels) }

func (s *Set) Get(name string) (*Channel, bool) {
	c, ok := s.byName[name]
	return c, ok
}

// Group lists the channels of a group, sorted by name.
func (s *Set) Group(name string) []*Channel { return slices.Clone(s.groups[name]) }

// ByToken finds the channel a delivery token belongs to. It compares SHA-256 digests with every stored digest
// in constant time, so neither the token's content nor which channel matched shows in the timing.
func (s *Set) ByToken(token string) (*Channel, bool) {
	if token == "" {
		return nil, false
	}
	sum := sha256.Sum256([]byte(token))
	var found *Channel
	for i := range s.tokens {
		if subtle.ConstantTimeCompare(sum[:], s.tokens[i].sum[:]) == 1 {
			found = s.tokens[i].ch
		}
	}
	return found, found != nil
}

// Languages is the configured language registry (empty when not configured).
func (s *Set) Languages() map[string]Language {
	out := make(map[string]Language, len(s.langs))
	for k, v := range s.langs {
		out[k] = v
	}
	return out
}

// PickLocale chooses the locale to show: the first of the channel's languages the item has, else its main
// locale when it is always available (and present). fallback reports a locale other than the channel's first
// language.
func PickLocale(c *Channel, available []string, main string, alwaysAvailable bool) (locale string, fallback, ok bool) {
	for i, l := range c.Languages {
		if slices.Contains(available, l) {
			return l, i > 0, true
		}
	}
	if alwaysAvailable && slices.Contains(available, main) {
		return main, len(c.Languages) == 0 || main != c.Languages[0], true
	}
	return "", false, false
}

// Option is a channel key a module adds; Decode validates the YAML value and returns what Channel.Options holds.
type Option struct {
	Name   string
	Decode func(n *config.Node) (any, error)
}

// Check validates the compiled set as a whole (e.g. overlapping matchers). It runs only when every channel
// compiled cleanly.
type Check func(s *Set) error

type CompileOptions struct {
	Options       []Option
	Checks        []Check
	Schema        *schema.Schema
	DefaultLocale string
}
