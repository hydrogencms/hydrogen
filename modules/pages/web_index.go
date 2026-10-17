package pages

import (
	"cmp"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/channel"
)

// Channel option names the pages module registers.
const (
	optSite     = "site"
	optMatch    = "match"
	optReserved = "reserved"
)

// matcher is the decoded `match` option of a web channel.
type matcher struct {
	Host   string       `json:"host,omitempty"`
	Prefix string       `json:"prefix,omitempty"`
	Header *headerMatch `json:"header,omitempty"`
}

type headerMatch struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// parts counts the matcher's conditions; more parts are more specific.
func (m matcher) parts() int {
	n := 0
	if m.Host != "" {
		n++
	}
	if m.Prefix != "" {
		n++
	}
	if m.Header != nil {
		n++
	}
	return n
}

// key identifies the matcher; two web channels with the same key can never be told apart.
func (m matcher) key() string {
	k := "host=" + m.Host + " prefix=" + m.Prefix
	if m.Header != nil {
		k += " header=" + m.Header.Name + ":" + m.Header.Value
	}
	return k
}

// overlaps reports whether some request can fit both matchers: their hosts agree or one is open, one prefix
// contains the other, and their headers do not demand different values of the same header.
func (m matcher) overlaps(o matcher) bool {
	if m.Host != "" && o.Host != "" && m.Host != o.Host {
		return false
	}
	if m.Prefix != "" && o.Prefix != "" {
		if _, ok := cutPrefix(m.Prefix, o.Prefix); !ok {
			if _, ok := cutPrefix(o.Prefix, m.Prefix); !ok {
				return false
			}
		}
	}
	if m.Header != nil && o.Header != nil && m.Header.Name == o.Header.Name && m.Header.Value != o.Header.Value {
		return false
	}
	return true
}

// matches reports the site path (after the prefix) when the request fits the matcher.
func (m matcher) matches(host, path string, h http.Header) (string, bool) {
	if m.Host != "" && m.Host != host {
		return "", false
	}
	if m.Header != nil && h.Get(m.Header.Name) != m.Header.Value {
		return "", false
	}
	if m.Prefix == "" {
		return path, true
	}
	return cutPrefix(path, m.Prefix)
}

// cutPrefix strips a path prefix on a segment boundary; the site path of the prefix itself is "/".
func cutPrefix(path, prefix string) (string, bool) {
	if path == prefix {
		return "/", true
	}
	if rest, ok := strings.CutPrefix(path, prefix); ok && strings.HasPrefix(rest, "/") {
		return rest, true
	}
	return "", false
}

// trimSlash drops trailing slashes from a site path; slash reports whether a non-root path had any.
func trimSlash(path string) (trimmed string, slash bool) {
	t := strings.TrimRight(path, "/")
	if t == "" {
		return "/", false
	}
	return t, t != path
}

// reservedPath is a path the frontend owns: exact, or with a trailing /* the path and everything below it.
type reservedPath struct {
	base     string
	wildcard bool
}

func parseReserved(pattern string) reservedPath {
	if base, ok := strings.CutSuffix(pattern, "/*"); ok {
		return reservedPath{base: base, wildcard: true}
	}
	return reservedPath{base: pattern}
}

// matches compares case-sensitively; callers trim a trailing slash first.
func (r reservedPath) matches(path string) bool {
	if path == r.base {
		return true
	}
	if !r.wildcard {
		return false
	}
	rest, ok := strings.CutPrefix(path, r.base)
	return ok && strings.HasPrefix(rest, "/")
}

// webChannel is a channel with its pages options decoded.
type webChannel struct {
	ch       *channel.Channel
	site     string
	m        matcher
	reserved []reservedPath
}

func newWebChannel(ch *channel.Channel, defaultSite string) *webChannel {
	w := &webChannel{ch: ch, site: defaultSite}
	if s, ok := ch.Options[optSite].(string); ok && s != "" {
		w.site = s
	}
	w.m, _ = ch.Options[optMatch].(matcher)
	if list, ok := ch.Options[optReserved].([]string); ok {
		for _, p := range list {
			w.reserved = append(w.reserved, parseReserved(p))
		}
	}
	return w
}

func (w *webChannel) isReserved(path string) bool {
	return slices.ContainsFunc(w.reserved, func(r reservedPath) bool { return r.matches(path) })
}

// rel is the channel-relative URL of a site path: the prefix plus the path (the prefix alone for the root).
func (w *webChannel) rel(path string) string {
	if path == "/" && w.m.Prefix != "" {
		return w.m.Prefix
	}
	return w.m.Prefix + path
}

// origin is where the channel's pages are served from: its frontend, else https plus the matched host, else
// nothing (path-only URLs).
func (w *webChannel) origin() string {
	switch {
	case w.ch.Frontend != "":
		return strings.TrimRight(w.ch.Frontend, "/")
	case w.m.Host != "":
		return "https://" + w.m.Host
	}
	return ""
}

func (w *webChannel) abs(path string) string { return w.origin() + w.rel(path) }

// siteLink makes a redirect target safe to hand out: a site path becomes relative to the channel, an absolute
// http(s) URL stays as it is, and everything else (protocol-relative, other schemes, backslashes, control
// characters, whitespace in a path) is refused.
func (w *webChannel) siteLink(target string) (string, bool) {
	if unsafeText(target) {
		return "", false
	}
	if strings.HasPrefix(target, "/") {
		if strings.HasPrefix(target, "//") || strings.ContainsFunc(target, unicode.IsSpace) {
			return "", false
		}
		return w.rel(target), true
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return target, true
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// moreSpecific orders web channels by comparing (has host, has prefix, has header) lexicographically, then
// the longer prefix, then the name. A host-only matcher therefore beats a prefix-only one, and beats a
// prefix-plus-header one.
func moreSpecific(a, b *webChannel) int {
	return cmp.Or(
		cmp.Compare(b2i(b.m.Host != ""), b2i(a.m.Host != "")),
		cmp.Compare(b2i(b.m.Prefix != ""), b2i(a.m.Prefix != "")),
		cmp.Compare(b2i(b.m.Header != nil), b2i(a.m.Header != nil)),
		cmp.Compare(len(b.m.Prefix), len(a.m.Prefix)),
		cmp.Compare(a.ch.Name, b.ch.Name),
	)
}

// webIndex holds the web channels of a project: by specificity for matching, by name for everything else.
type webIndex struct {
	defaultSite string
	bySpecific  []*webChannel
	byName      []*webChannel
}

func newWebIndex(chans []*channel.Channel, sites []string) *webIndex {
	x := &webIndex{}
	if len(sites) > 0 {
		x.defaultSite = sites[0]
	}
	for _, ch := range chans {
		if _, ok := ch.Options[optMatch].(matcher); ok {
			x.byName = append(x.byName, newWebChannel(ch, x.defaultSite))
		}
	}
	slices.SortFunc(x.byName, func(a, b *webChannel) int { return cmp.Compare(a.ch.Name, b.ch.Name) })
	x.bySpecific = slices.Clone(x.byName)
	slices.SortFunc(x.bySpecific, moreSpecific)
	return x
}

// splitURL reads the lower-case host (no port, no trailing dot) and the decoded path of a request URL or bare
// path. bad reports a path that can never be a site path: invalid UTF-8, or an encoded slash (%2F) in the
// escaped path, which would decode into a separator.
func splitURL(raw string) (host, path string, bad bool, err error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false, err
	}
	path = u.Path
	if path == "" {
		path = "/"
	}
	bad = !utf8.ValidString(path) || hasEncodedSlash(u.EscapedPath())
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), path, bad, nil
}

func hasEncodedSlash(escaped string) bool { return strings.Contains(strings.ToLower(escaped), "%2f") }

// match picks the most specific web channel the request fits.
func (x *webIndex) match(host, path string, h http.Header) (*webChannel, string, bool) {
	for _, w := range x.bySpecific {
		if rest, ok := w.m.matches(host, path, h); ok {
			return w, rest, true
		}
	}
	return nil, "", false
}

// of returns the indexed web channel, or derives one for a channel without a matcher.
func (x *webIndex) of(ch *channel.Channel) *webChannel {
	if i, ok := slices.BinarySearchFunc(x.byName, ch.Name, func(w *webChannel, name string) int {
		return cmp.Compare(w.ch.Name, name)
	}); ok && x.byName[i].ch == ch {
		return x.byName[i]
	}
	return newWebChannel(ch, x.defaultSite)
}

// peers are the web channels that share w's group, or its site when it has no group; w itself is included
// when it is a web channel.
func (x *webIndex) peers(w *webChannel) []*webChannel {
	var out []*webChannel
	for _, p := range x.byName {
		if (w.ch.Group != "" && p.ch.Group == w.ch.Group) || (w.ch.Group == "" && p.site == w.site) {
			out = append(out, p)
		}
	}
	return out
}
