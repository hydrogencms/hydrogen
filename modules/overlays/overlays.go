// Package overlays switches on locale inheritance (market overlays): a language that declares
// `inherits: de` in channels.yaml (`languages: { de-CH: { inherits: de } }`) stores only the translatable
// fields it overrides and resolves the rest from its ancestors, field by field. Without this module the
// declaration is ignored and every locale is independent. See docs/locales.md.
package overlays

import (
	"slices"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

// Module returns the overlays module.
func Module() kernel.Module { return module{} }

type module struct{}

func (module) Name() string { return "overlays" }

func (module) Register(r *kernel.Registry) error {
	return r.Overlays(func(set *channel.Set) content.Overlays { return New(set.Languages()) })
}

// Provider answers content.Overlays from a language registry.
type Provider struct {
	parent   map[string]string
	children map[string][]string
}

// New builds the provider of a language registry (channel.Set.Languages).
func New(langs map[string]channel.Language) *Provider {
	p := &Provider{parent: map[string]string{}, children: map[string][]string{}}
	for code, l := range langs {
		if l.Inherits != "" {
			p.parent[code] = l.Inherits
			p.children[l.Inherits] = append(p.children[l.Inherits], code)
		}
	}
	for _, c := range p.children {
		slices.Sort(c)
	}
	return p
}

// Chain lists the locale and then its ancestors, nearest first.
func (p *Provider) Chain(locale string) []string {
	out := []string{locale}
	for len(out) <= len(p.parent) {
		parent, ok := p.parent[out[len(out)-1]]
		if !ok {
			break
		}
		out = append(out, parent)
	}
	return out
}

// Descendants lists the locales that inherit from the locale, directly or not, sorted.
func (p *Provider) Descendants(locale string) []string {
	var out []string
	queue := slices.Clone(p.children[locale])
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if slices.Contains(out, c) {
			continue
		}
		out = append(out, c)
		queue = append(queue, p.children[c]...)
	}
	slices.Sort(out)
	return out
}
