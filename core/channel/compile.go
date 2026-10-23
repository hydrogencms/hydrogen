package channel

import (
	"cmp"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/hydrogencms/hydrogen/core/config"
)

type compiler struct {
	o     CompileOptions
	opts  map[string]Option
	langs map[string]Language
	errs  []error
}

func (cx *compiler) add(err error) { cx.errs = append(cx.errs, err) }

var topKeys = []string{"channels", "defaults", "groups", "languages", "templates"}

// Compile builds the channel set from the "channels" config node (nil: not configured → one default channel).
// All problems are reported together, sorted by position.
func Compile(n *config.Node, o CompileOptions) (*Set, error) {
	cx := &compiler{o: o, opts: map[string]Option{}, langs: map[string]Language{}}
	cx.registerOptions()
	var s *Set
	if n == nil {
		s = cx.build(nil, nil, nil, nil)
	} else {
		s = cx.compileNode(n)
	}
	if len(cx.errs) == 0 {
		cx.runChecks(s)
	}
	if len(cx.errs) > 0 {
		return nil, joinSorted(cx.errs)
	}
	return s, nil
}

func (cx *compiler) registerOptions() {
	for _, op := range cx.o.Options {
		switch {
		case op.Name == "" || op.Decode == nil:
			cx.add(errors.New("channel: option needs a name and a Decode function"))
		case slices.Contains(coreKeys, op.Name) || slices.Contains(topKeys, op.Name) || op.Name == "name" || op.Name == "for":
			cx.add(fmt.Errorf("channel: option %q clashes with a core key", op.Name))
		default:
			if _, dup := cx.opts[op.Name]; dup {
				cx.add(fmt.Errorf("channel: option %q registered twice", op.Name))
			}
			cx.opts[op.Name] = op
		}
	}
}

func (cx *compiler) runChecks(s *Set) {
	for _, chk := range cx.o.Checks {
		if err := chk(s); err != nil {
			cx.add(err)
		}
	}
}

func (cx *compiler) compileNode(n *config.Node) *Set {
	if n.Kind() != "map" {
		cx.fail(n, "channels must be a mapping")
		return nil
	}
	for _, k := range n.Keys() {
		if !slices.Contains(topKeys, k) {
			cx.fail(n.Get(k), "unknown key %q in channels (known: %s)", k, strings.Join(topKeys, ", "))
		}
	}
	if ln := n.Get("languages"); ln != nil {
		cx.parseRegistry(ln)
	}
	var defaults *spec
	if dn := n.Get("defaults"); dn != nil {
		defaults = cx.parseSpec(dn, kindDefaults)
	}
	groups := cx.parseGroups(n.Get("groups"))
	chans := cx.parseChannels(n.Get("channels"))
	chans = append(chans, cx.parseTemplates(n.Get("templates"))...)
	return cx.build(defaults, groups, chans, n)
}

type namedSpec struct {
	name string
	spec *spec
}

func (cx *compiler) parseGroups(gn *config.Node) map[string]*spec {
	out := map[string]*spec{}
	if gn == nil {
		return out
	}
	if gn.Kind() != "map" {
		cx.fail(gn, "groups must be a mapping of name to settings")
		return out
	}
	for _, name := range gn.Keys() {
		v := gn.Get(name)
		if !idRE.MatchString(name) {
			cx.fail(v, "group name %q: use lowercase letters, digits, underscores and single dashes, starting with a letter", name)
		}
		if sp := cx.parseSpec(v, kindGroup); sp != nil {
			out[name] = sp
		}
	}
	return out
}

func (cx *compiler) parseChannels(cn *config.Node) []namedSpec {
	if cn == nil {
		return nil
	}
	if cn.Kind() != "map" {
		cx.fail(cn, "channels must be a mapping of name to settings")
		return nil
	}
	var out []namedSpec
	for _, name := range cn.Keys() {
		v := cn.Get(name)
		if sp := cx.parseSpec(v, kindChannel); sp != nil {
			out = append(out, namedSpec{name, sp})
		}
	}
	return out
}

func (cx *compiler) parseRegistry(ln *config.Node) {
	if ln.Kind() != "map" {
		cx.fail(ln, "languages must be a mapping of locale code to {}")
		return
	}
	for _, code := range ln.Keys() {
		v := ln.Get(code)
		if !localeRE.MatchString(code) {
			cx.fail(v, "language %q is not a locale code (like en or de-CH)", code)
			continue
		}
		l := Language{Code: code}
		if v.Kind() == "map" {
			for _, k := range v.Keys() {
				if k != "inherits" {
					cx.fail(v.Get(k), "unknown key %q in language %s (known: inherits)", k, code)
					continue
				}
				l.Inherits, _ = v.Get(k).String()
			}
		}
		cx.langs[code] = l
	}
	cx.checkInherits(ln)
}

func (cx *compiler) checkInherits(ln *config.Node) {
	for _, code := range sortedKeys(cx.langs) {
		l := cx.langs[code]
		if l.Inherits == "" {
			continue
		}
		at := ln.Get(code).Get("inherits")
		if _, ok := cx.langs[l.Inherits]; !ok {
			cx.fail(at, "language %s inherits %q, which is not in the registry", code, l.Inherits)
			continue
		}
		seen := map[string]bool{code: true}
		for p := l.Inherits; p != ""; p = cx.langs[p].Inherits {
			if seen[p] {
				cx.fail(at, "language %s inherits in a cycle", code)
				break
			}
			seen[p] = true
		}
	}
}

// build resolves every channel through the cascade, validates the result and assembles the Set. With no
// channels configured it makes the implicit default channel.
func (cx *compiler) build(defaults *spec, groups map[string]*spec, chans []namedSpec, n *config.Node) *Set {
	implicit := len(chans) == 0
	if implicit {
		chans = []namedSpec{{DefaultName, nil}}
	}
	s := &Set{byName: map[string]*Channel{}, groups: map[string][]*Channel{}, langs: maps.Clone(cx.langs)}
	if s.langs == nil {
		s.langs = map[string]Language{}
	}
	where := map[string]string{}
	tokenOwner := map[[sha256.Size]byte]string{}
	for _, ns := range chans {
		pos := "channels"
		if ns.spec != nil {
			pos = ns.spec.n.Pos()
		} else if n != nil {
			pos = n.Pos()
		}
		if prev, dup := where[ns.name]; dup {
			cx.add(fmt.Errorf("%s: channel %q is already defined at %s", pos, ns.name, prev))
			continue
		}
		where[ns.name] = pos
		ch, toks := cx.resolve(ns, defaults, groups, implicit)
		if ch == nil {
			continue
		}
		for _, t := range toks {
			if other, dup := tokenOwner[t]; dup {
				cx.add(fmt.Errorf("%s: channel %q uses a token that channel %q also has", pos, ns.name, other))
				continue
			}
			tokenOwner[t] = ns.name
			s.tokens = append(s.tokens, tokenEntry{t, ch})
		}
		s.channels = append(s.channels, ch)
		s.byName[ch.Name] = ch
	}
	slices.SortFunc(s.channels, func(a, b *Channel) int { return cmp.Compare(a.Name, b.Name) })
	for _, ch := range s.channels {
		if ch.Group != "" {
			s.groups[ch.Group] = append(s.groups[ch.Group], ch)
		}
	}
	return s
}

func (cx *compiler) resolve(ns namedSpec, defaults *spec, groups map[string]*spec, implicit bool) (*Channel, [][sha256.Size]byte) {
	pos := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		if ns.spec != nil {
			cx.fail(ns.spec.n, "channel %q: %s", ns.name, msg)
		} else {
			cx.add(fmt.Errorf("channel %q: %s", ns.name, msg))
		}
	}
	if !idRE.MatchString(ns.name) {
		pos("name: use lowercase letters, digits, underscores and single dashes, starting with a letter")
		return nil, nil
	}
	chain := cx.chain(ns, defaults, groups)
	c := &Channel{Name: ns.name, Options: map[string]any{}}
	if g := first(chain, func(s *spec) field[string] { return s.group }); g.ok {
		c.Group = g.v
	}
	c.Languages = cx.pickLanguages(chain, implicit, pos)
	if f := first(chain, func(s *spec) field[[]string] { return s.collections }); f.ok {
		c.Collections = slices.Clone(f.v)
	}
	c.Frontend = first(chain, func(s *spec) field[string] { return s.frontend }).v
	c.Preview = map[string]string{}
	if f := first(chain, func(s *spec) field[map[string]string] { return s.preview }); f.ok {
		c.Preview = maps.Clone(f.v)
	}
	c.Links = map[string]string{}
	if f := first(chain, func(s *spec) field[map[string]string] { return s.links }); f.ok {
		c.Links = maps.Clone(f.v)
	}
	c.Settings = mergeSettings(chain)
	c.Access = AccessPublic
	if f := first(chain, func(s *spec) field[string] { return s.access }); f.ok {
		c.Access = f.v
	}
	toks := cx.channelTokens(ns, c, pos)
	cx.decodeOptions(c, chain)
	if c.Languages == nil {
		return nil, nil
	}
	return c, toks
}

// chain lists the specs a channel inherits from, most specific first.
func (cx *compiler) chain(ns namedSpec, defaults *spec, groups map[string]*spec) []*spec {
	var chain []*spec
	if ns.spec != nil {
		chain = append(chain, ns.spec)
		if g := ns.spec.group; g.ok {
			if gs, ok := groups[g.v]; ok {
				chain = append(chain, gs)
			} else {
				cx.fail(g.n, "channel %q: unknown group %q (have: %s)", ns.name, g.v, strings.Join(sortedKeys(groups), ", "))
			}
		}
	}
	if defaults != nil {
		chain = append(chain, defaults)
	}
	return chain
}

func (cx *compiler) pickLanguages(chain []*spec, implicit bool, fail func(string, ...any)) []string {
	if f := first(chain, func(s *spec) field[[]string] { return s.languages }); f.ok {
		return slices.Clone(f.v)
	}
	if implicit {
		l := cx.o.DefaultLocale
		if l == "" {
			l = "en"
		}
		return []string{l}
	}
	fail("languages are required (set them on the channel, its group or defaults)")
	return nil
}

func (cx *compiler) channelTokens(ns namedSpec, c *Channel, fail func(string, ...any)) [][sha256.Size]byte {
	var tokens map[string]string
	if ns.spec != nil && ns.spec.tokens.ok {
		tokens = ns.spec.tokens.v
	}
	broken := ns.spec != nil && ns.spec.tokens.n != nil && !ns.spec.tokens.ok
	switch {
	case broken:
	case c.Access == AccessToken && len(tokens) == 0:
		fail("access is token but no tokens are set")
	case c.Access == AccessPublic && len(tokens) > 0:
		fail("tokens are set but access is public; set access: token")
	}
	var out [][sha256.Size]byte
	for _, name := range sortedKeys(tokens) {
		if err := checkToken(tokens[name]); err != "" {
			cx.fail(ns.spec.tokens.n.Get(name), "channel %q: token %q %s", ns.name, name, err)
			continue
		}
		out = append(out, sha256.Sum256([]byte(tokens[name])))
	}
	return out
}

// minTokenLen is the shortest delivery token accepted.
const minTokenLen = 16

func checkToken(t string) string {
	switch {
	case len([]rune(t)) < minTokenLen:
		return fmt.Sprintf("must be at least %d characters", minTokenLen)
	case strings.IndexFunc(t, unicode.IsSpace) >= 0:
		return "must not contain whitespace"
	}
	return ""
}

func (cx *compiler) decodeOptions(c *Channel, chain []*spec) {
	names := map[string]bool{}
	for _, s := range chain {
		for k := range s.opts {
			names[k] = true
		}
	}
	for _, name := range sortedKeys(names) {
		var node *config.Node
		for _, s := range chain {
			if v, ok := s.opts[name]; ok {
				node = v
				break
			}
		}
		v, err := cx.opts[name].Decode(node)
		if err != nil {
			cx.add(fmt.Errorf("%w (channel %q, option %s)", err, c.Name, name))
			continue
		}
		c.Options[name] = v
	}
}

func mergeSettings(chain []*spec) map[string]any {
	out := map[string]any{}
	for _, s := range slices.Backward(chain) {
		if s.settings.ok {
			mergeInto(out, s.settings.v)
		}
	}
	return out
}

func mergeInto(dst, src map[string]any) {
	for k, v := range src {
		sm, srcMap := v.(map[string]any)
		dm, dstMap := dst[k].(map[string]any)
		if srcMap && dstMap {
			mergeInto(dm, sm)
			continue
		}
		dst[k] = deepCopy(v)
	}
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		return slices.Collect(func(yield func(any) bool) {
			for _, e := range x {
				if !yield(deepCopy(e)) {
					return
				}
			}
		})
	}
	return v
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}

// joinSorted joins errors ordered by message with digit runs compared as numbers (file:2 before file:10),
// dropping repeats (the same template position fails once per expansion).
func joinSorted(errs []error) error {
	errs = slices.Clone(errs)
	slices.SortStableFunc(errs, func(a, b error) int { return naturalCompare(a.Error(), b.Error()) })
	errs = slices.CompactFunc(errs, func(a, b error) bool { return a.Error() == b.Error() })
	return errors.Join(errs...)
}

func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		if isDigit(a[0]) && isDigit(b[0]) {
			na, ra := digits(a)
			nb, rb := digits(b)
			if c := cmp.Compare(len(strings.TrimLeft(na, "0")), len(strings.TrimLeft(nb, "0"))); c != 0 {
				return c
			}
			if c := strings.Compare(strings.TrimLeft(na, "0"), strings.TrimLeft(nb, "0")); c != 0 {
				return c
			}
			a, b = ra, rb
			continue
		}
		if a[0] != b[0] {
			return cmp.Compare(a[0], b[0])
		}
		a, b = a[1:], b[1:]
	}
	return cmp.Compare(len(a), len(b))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digits(s string) (run, rest string) {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i], s[i:]
}
