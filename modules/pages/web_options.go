package pages

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/pages/internal/route"
)

var (
	hostPattern   = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
	headerPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
)

// channelOptions are the channel keys the pages module adds.
func channelOptions(sites []string) []channel.Option {
	return []channel.Option{
		{Name: optSite, Decode: func(n *config.Node) (any, error) { return decodeSite(n, sites) }},
		{Name: optMatch, Decode: func(n *config.Node) (any, error) { return decodeMatch(n) }},
		{Name: optReserved, Decode: func(n *config.Node) (any, error) { return decodeReserved(n) }},
		{Name: optRootReserved, Decode: func(n *config.Node) (any, error) { return decodeRootReserved(n) }},
	}
}

func scalar(n *config.Node) (string, error) {
	if n.Kind() != "scalar" {
		return "", n.Errorf("expected a string")
	}
	s, err := n.String()
	if err != nil {
		return "", n.Errorf("expected a string")
	}
	return s, nil
}

func decodeSite(n *config.Node, sites []string) (any, error) {
	s, err := scalar(n)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(sites, s) {
		return nil, n.Errorf("unknown site %q (modules.pages.sites: %s)", s, strings.Join(sites, ", "))
	}
	return s, nil
}

func decodeMatch(n *config.Node) (any, error) {
	if n.Kind() != "map" {
		return nil, n.Errorf("match must be a map with host, prefix and/or header")
	}
	var m matcher
	all := false // prefix: / serves every path; with nothing else the channel serves every URL
	var errs []error
	for _, k := range n.Keys() {
		v := n.Get(k)
		var err error
		switch k {
		case "host":
			m.Host, err = decodeHost(v)
		case "prefix":
			m.Prefix, err = decodePrefix(v)
			all = all || (err == nil && m.Prefix == "")
		case "header":
			m.Header, err = decodeHeader(v)
		default:
			err = v.Errorf("unknown match key %q (host, prefix, header)", k)
		}
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if m.parts() == 0 && !all {
		return nil, n.Errorf("match needs at least one of host, prefix, header (prefix: / serves every URL)")
	}
	return m, nil
}

func decodeHost(n *config.Node) (string, error) {
	s, err := scalar(n)
	if err != nil {
		return "", err
	}
	if i := strings.IndexFunc(s, func(r rune) bool { return r >= 0x80 }); i >= 0 {
		return "", n.Errorf("host %q must be ASCII; write an internationalised name in punycode (xn--…)", s)
	}
	if s != strings.ToLower(s) {
		return "", n.Errorf("host %q must be lower-case", s)
	}
	if !hostPattern.MatchString(s) {
		return "", n.Errorf("host %q must be a bare host name: no scheme, port or path", s)
	}
	return s, nil
}

func decodePrefix(n *config.Node) (string, error) {
	s, err := scalar(n)
	if err != nil {
		return "", err
	}
	if s == "/" {
		return "", nil
	}
	if _, err := route.Clean(s); err != nil {
		return "", n.Errorf("prefix %q must be a clean path such as /en, or / for every path", s)
	}
	return s, nil
}

func decodeHeader(n *config.Node) (*headerMatch, error) {
	if n.Kind() != "map" {
		return nil, n.Errorf("header must be a map with name and value")
	}
	var h headerMatch
	var errs []error
	for _, k := range n.Keys() {
		v := n.Get(k)
		s, err := scalar(v)
		errs = append(errs, err)
		switch k {
		case "name":
			h.Name = http.CanonicalHeaderKey(s)
		case "value":
			h.Value = s
		default:
			errs = append(errs, v.Errorf("unknown header key %q (name, value)", k))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	if !headerPattern.MatchString(h.Name) || h.Value == "" {
		return nil, n.Errorf("header needs a name (letters, digits, dashes) and a non-empty value")
	}
	return &h, nil
}

func decodeReserved(n *config.Node) (any, error) {
	return decodePaths(n, "reserved", checkReserved)
}

// decodeRootReserved reads root_reserved: reserved paths outside the channel's prefix.
func decodeRootReserved(n *config.Node) (any, error) {
	return decodePaths(n, optRootReserved, func(item *config.Node, pattern string) error {
		if err := checkReserved(item, pattern); err != nil {
			return err
		}
		if base := strings.TrimSuffix(pattern, "/*"); ownedRoot(base) {
			return item.Errorf("root path %q belongs to the binary (%s); pick another path", pattern, strings.Join(ownedRoots, ", "))
		}
		return nil
	})
}

// ownedRoots are the root paths the binary serves itself; no channel can claim them.
var ownedRoots = []string{"/api", "/healthz", "/readyz"}

func ownedRoot(base string) bool {
	return slices.ContainsFunc(ownedRoots, func(o string) bool { return base == o || strings.HasPrefix(base, o+"/") })
}

func decodePaths(n *config.Node, what string, check func(*config.Node, string) error) (any, error) {
	if n.Kind() != "list" {
		return nil, n.Errorf("%s must be a list of paths", what)
	}
	var out []string
	var errs []error
	for _, item := range n.Items() {
		s, err := scalar(item)
		if err == nil {
			err = check(item, s)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		out = append(out, s)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return out, nil
}

func checkReserved(n *config.Node, pattern string) error {
	base := strings.TrimSuffix(pattern, "/*")
	if _, err := route.Clean(base); err != nil || base == "/" || base == "" {
		return n.Errorf("reserved path %q must be a clean path such as /cart or /account/*", pattern)
	}
	return nil
}

// checkMatchers fails when two web channels have the same matcher: the second could never be reached.
func checkMatchers(s *channel.Set) error {
	return errors.Join(duplicateMatchers(s.All()), rootConflicts(s.All()))
}

// rootConflicts checks the root_reserved paths of prefixed web channels: they need a prefix, must lie outside
// it, and no other channel that can see the same requests may claim them, by the same root path or by a prefix
// they fall under.
func rootConflicts(chans []*channel.Channel) error {
	var web []*webChannel
	for _, ch := range chans {
		if _, ok := ch.Options[optMatch].(matcher); ok {
			web = append(web, newWebChannel(ch, ""))
		}
	}
	var errs []error
	for _, a := range web {
		if len(a.roots) == 0 {
			continue
		}
		if a.m.Prefix == "" {
			errs = append(errs, fmt.Errorf("channel %s: %s needs match.prefix; a channel without one already serves every path", a.ch.Name, optRootReserved))
			continue
		}
		for _, p := range a.roots {
			if underPrefix(p.base, a.m.Prefix) {
				errs = append(errs, fmt.Errorf("channel %s: root path %s lies under its own prefix %s; list it in reserved", a.ch.Name, p, a.m.Prefix))
			}
		}
		for _, b := range web {
			if a == b || !a.m.sameRequests(b.m) {
				continue
			}
			for _, p := range a.roots {
				if b.m.Prefix != "" && (underPrefix(p.base, b.m.Prefix) || p.wildcard && underPrefix(b.m.Prefix, p.base)) {
					errs = append(errs, fmt.Errorf("channel %s: root path %s overlaps the prefix %s of channel %s", a.ch.Name, p, b.m.Prefix, b.ch.Name))
				}
				if a.ch.Name < b.ch.Name {
					for _, q := range b.roots {
						if p.matches(q.base) || q.matches(p.base) {
							errs = append(errs, fmt.Errorf("channels %s and %s both claim the root path %s", a.ch.Name, b.ch.Name, p))
						}
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}

func underPrefix(path, prefix string) bool {
	_, ok := cutPrefix(path, prefix)
	return ok
}

func duplicateMatchers(chans []*channel.Channel) error {
	seen := map[string]string{}
	var errs []error
	for _, ch := range chans {
		m, ok := ch.Options[optMatch].(matcher)
		if !ok {
			continue
		}
		if first, dup := seen[m.key()]; dup {
			errs = append(errs, fmt.Errorf("channels %s and %s both match %s", first, ch.Name, m.key()))
			continue
		}
		seen[m.key()] = ch.Name
	}
	return errors.Join(errs...)
}
