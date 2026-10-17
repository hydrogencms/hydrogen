package delivery

import (
	"slices"

	"github.com/hydrogencms/hydrogen/core/channel"
)

// parents maps a language to the language it inherits from (the registry's `inherits`).
func parentsOf(s *channel.Set) map[string]string {
	var out map[string]string
	for code, l := range s.Languages() {
		if l.Inherits != "" {
			if out == nil {
				out = map[string]string{}
			}
			out[code] = l.Inherits
		}
	}
	return out
}

// hasOverlays reports whether a channel serves a language that inherits from another.
func (a *API) hasOverlays(ch *channel.Channel) bool {
	if len(a.parents) == 0 {
		return false
	}
	return slices.ContainsFunc(ch.Languages, func(l string) bool { _, ok := a.parents[l]; return ok })
}

// withDerived adds to the locales an item is stored in the channel's overlay languages whose chain reaches
// one of them: an overlay locale is available whenever a locale it inherits from is.
func (a *API) withDerived(ch *channel.Channel, avail []string) []string {
	out := avail
	for _, l := range ch.Languages {
		if slices.Contains(out, l) {
			continue
		}
		seen := 0
		for p, ok := a.parents[l]; ok && seen <= len(a.parents); p, ok = a.parents[p] {
			seen++
			if slices.Contains(avail, p) {
				out = append(slices.Clone(out), l)
				break
			}
		}
	}
	return out
}
