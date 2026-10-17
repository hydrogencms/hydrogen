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
	var errs []error
	for _, k := range n.Keys() {
		v := n.Get(k)
		var err error
		switch k {
		case "host":
			m.Host, err = decodeHost(v)
		case "prefix":
			m.Prefix, err = decodePrefix(v)
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
	if m.parts() == 0 {
		return nil, n.Errorf("match needs at least one of host, prefix, header")
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
	if _, err := route.Clean(s); err != nil || s == "/" {
		return "", n.Errorf("prefix %q must be a clean path such as /en, not /", s)
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
	if n.Kind() != "list" {
		return nil, n.Errorf("reserved must be a list of paths")
	}
	var out []string
	var errs []error
	for _, item := range n.Items() {
		s, err := scalar(item)
		if err == nil {
			err = checkReserved(item, s)
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
func checkMatchers(s *channel.Set) error { return duplicateMatchers(s.All()) }

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
