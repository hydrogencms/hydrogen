package channel

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/config"
)

var (
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	localeRE = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
)

// coreKeys are the spec keys core understands; modules add theirs as options.
var coreKeys = []string{"access", "collections", "frontend", "group", "languages", "preview", "settings", "tokens"}

type specKind int

const (
	kindDefaults specKind = iota
	kindGroup
	kindChannel
	kindTemplate
)

func (k specKind) String() string {
	return [...]string{"defaults", "group", "channel", "template"}[k]
}

// field is a spec value with the node it came from (for error positions).
type field[T any] struct {
	v  T
	ok bool
	n  *config.Node
}

// spec is one parsed, validated defaults/group/channel block. Cascading happens in resolve.
type spec struct {
	n           *config.Node
	group       field[string]
	languages   field[[]string]
	collections field[[]string]
	settings    field[map[string]any]
	frontend    field[string]
	preview     field[map[string]string]
	access      field[string]
	tokens      field[map[string]string]
	opts        map[string]*config.Node
}

func first[T any](chain []*spec, get func(*spec) field[T]) field[T] {
	for _, s := range chain {
		if f := get(s); f.ok {
			return f
		}
	}
	return field[T]{}
}

func (cx *compiler) parseSpec(n *config.Node, kind specKind) *spec {
	if n.Kind() != "map" {
		cx.fail(n, "%s must be a mapping", kind)
		return nil
	}
	sp := &spec{n: n, opts: map[string]*config.Node{}}
	for _, key := range n.Keys() {
		v := n.Get(key)
		switch key {
		case "group":
			if kind == kindDefaults || kind == kindGroup {
				cx.fail(v, "group can't be set on %s", kind)
			} else {
				sp.group = cx.parseGroupName(v)
			}
		case "tokens":
			if kind == kindDefaults || kind == kindGroup {
				cx.fail(v, "tokens belong to one channel and are not inherited; set them on the channel")
			} else {
				sp.tokens = cx.parseTokens(v)
			}
		case "name", "for":
			if kind != kindTemplate {
				cx.fail(v, "%s is only valid in templates", key)
			}
		case "languages":
			sp.languages = cx.parseLanguages(v)
		case "collections":
			sp.collections = cx.parseCollections(v)
		case "settings":
			sp.settings = cx.parseSettings(v)
		case "frontend":
			sp.frontend = cx.parseFrontend(v)
		case "preview":
			sp.preview = cx.parsePreview(v)
		case "access":
			sp.access = cx.parseAccess(v)
		default:
			if _, ok := cx.opts[key]; !ok {
				cx.fail(v, "unknown key %q in %s (known: %s)", key, kind, strings.Join(cx.knownKeys(kind), ", "))
				continue
			}
			sp.opts[key] = v
		}
	}
	return sp
}

func (cx *compiler) knownKeys(kind specKind) []string {
	keys := slices.Clone(coreKeys)
	if kind == kindDefaults || kind == kindGroup {
		keys = slices.DeleteFunc(keys, func(k string) bool { return k == "group" || k == "tokens" })
	}
	for k := range cx.opts {
		keys = append(keys, k)
	}
	if kind == kindTemplate {
		keys = append(keys, "for", "name")
	}
	slices.Sort(keys)
	return keys
}

func (cx *compiler) parseGroupName(v *config.Node) field[string] {
	s, err := v.String()
	if err != nil {
		cx.add(err)
		return field[string]{}
	}
	if !nameRE.MatchString(s) {
		cx.fail(v, "group name %q: use lowercase letters, digits and underscores, starting with a letter", s)
		return field[string]{}
	}
	return field[string]{s, true, v}
}

func (cx *compiler) stringList(v *config.Node, what string) ([]string, bool) {
	if v.Kind() != "list" {
		cx.fail(v, "%s must be a list", what)
		return nil, false
	}
	var out []string
	ok := true
	for _, it := range v.Items() {
		s, err := it.String()
		if err != nil {
			cx.add(err)
			ok = false
			continue
		}
		out = append(out, s)
	}
	if ok && len(out) == 0 {
		cx.fail(v, "%s is empty", what)
		return nil, false
	}
	return out, ok
}

func (cx *compiler) parseLanguages(v *config.Node) field[[]string] {
	list, ok := cx.stringList(v, "languages")
	if !ok {
		return field[[]string]{}
	}
	seen := map[string]bool{}
	for i, l := range list {
		switch {
		case !localeRE.MatchString(l):
			cx.fail(v.Items()[i], "language %q is not a locale code (like en or de-CH)", l)
			ok = false
		case seen[l]:
			cx.fail(v.Items()[i], "language %q is listed twice", l)
			ok = false
		case len(cx.langs) > 0 && !cx.hasLang(l):
			cx.fail(v.Items()[i], "language %q is not in the languages registry (have: %s)", l, strings.Join(cx.langNames(), ", "))
			ok = false
		}
		seen[l] = true
	}
	return field[[]string]{list, ok, v}
}

func (cx *compiler) parseCollections(v *config.Node) field[[]string] {
	list, ok := cx.stringList(v, "collections (omit the key to allow every collection)")
	if !ok {
		return field[[]string]{}
	}
	seen := map[string]bool{}
	for i, name := range list {
		switch {
		case seen[name]:
			cx.fail(v.Items()[i], "collection %q is listed twice", name)
			ok = false
		case cx.o.Schema != nil && !cx.hasType(name):
			cx.fail(v.Items()[i], "unknown collection %q (types: %s)", name, strings.Join(cx.typeNames(), ", "))
			ok = false
		}
		seen[name] = true
	}
	return field[[]string]{list, ok, v}
}

func (cx *compiler) parseSettings(v *config.Node) field[map[string]any] {
	if v.Kind() != "map" {
		cx.fail(v, "settings must be a mapping")
		return field[map[string]any]{}
	}
	a, err := v.Any()
	if err != nil {
		cx.add(err)
		return field[map[string]any]{}
	}
	return field[map[string]any]{a.(map[string]any), true, v}
}

func (cx *compiler) parseFrontend(v *config.Node) field[string] {
	s, err := v.String()
	if err != nil {
		cx.add(err)
		return field[string]{}
	}
	u, err := url.Parse(s)
	switch {
	case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "":
		cx.fail(v, "frontend %q must be an absolute http(s) URL", s)
	case u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Contains(s, "?") || strings.Contains(s, "#"):
		cx.fail(v, "frontend %q must have no credentials, query or fragment", s)
	default:
		return field[string]{strings.TrimRight(s, "/"), true, v}
	}
	return field[string]{}
}

func (cx *compiler) parseAccess(v *config.Node) field[string] {
	s, err := v.String()
	if err != nil {
		cx.add(err)
		return field[string]{}
	}
	if s != AccessPublic && s != AccessToken {
		cx.fail(v, "access %q is not %s or %s", s, AccessPublic, AccessToken)
		return field[string]{}
	}
	return field[string]{s, true, v}
}

func (cx *compiler) parseTokens(v *config.Node) field[map[string]string] {
	var m map[string]string
	if err := v.DecodeSecret(&m); err != nil {
		cx.add(err)
		return field[map[string]string]{n: v}
	}
	ok := true
	for _, name := range sortedKeys(m) {
		if !nameRE.MatchString(name) {
			cx.fail(v.Get(name), "token name %q: use lowercase letters, digits and underscores, starting with a letter", name)
			ok = false
		}
		if m[name] == "" {
			cx.fail(v.Get(name), "token %q is empty", name)
			ok = false
		}
	}
	return field[map[string]string]{m, ok, v}
}

func (cx *compiler) parsePreview(v *config.Node) field[map[string]string] {
	if v.Kind() != "map" {
		cx.fail(v, "preview must be a mapping of collection to URL pattern")
		return field[map[string]string]{}
	}
	out := map[string]string{}
	ok := true
	for _, coll := range v.Keys() {
		pn := v.Get(coll)
		pattern, err := pn.String()
		if err != nil {
			cx.add(err)
			ok = false
			continue
		}
		if !cx.checkPattern(pn, coll, pattern) {
			ok = false
		}
		out[coll] = pattern
	}
	return field[map[string]string]{out, ok, v}
}

func (cx *compiler) checkPattern(pn *config.Node, coll, pattern string) bool {
	good := true
	if coll != "*" && cx.o.Schema != nil && !cx.hasType(coll) {
		cx.fail(pn, "preview for unknown collection %q (types: %s)", coll, strings.Join(cx.typeNames(), ", "))
		good = false
	}
	if !strings.HasPrefix(pattern, "/") || strings.HasPrefix(pattern, "//") {
		cx.fail(pn, "preview pattern %q must start with a single /", pattern)
		good = false
	}
	if strings.ContainsAny(placeholder.ReplaceAllString(pattern, ""), "{}") {
		cx.fail(pn, "preview pattern %q has unbalanced braces", pattern)
		return false
	}
	for _, m := range placeholder.FindAllStringSubmatch(pattern, -1) {
		if !cx.checkPlaceholder(pn, coll, m[1]) {
			good = false
		}
	}
	return good
}

func (cx *compiler) checkPlaceholder(pn *config.Node, coll, name string) bool {
	switch {
	case name == "":
		cx.fail(pn, "preview pattern has an empty placeholder {}")
		return false
	case name == "id" || name == "locale" || name == "collection" || coll == "*" || cx.o.Schema == nil:
		return true
	}
	t, ok := cx.o.Schema.Type(coll)
	if !ok {
		return true
	}
	root, _, _ := strings.Cut(name, ".")
	if _, ok := t.Field(root); !ok {
		cx.fail(pn, "preview placeholder {%s}: %s has no field %q", name, coll, root)
		return false
	}
	return true
}

func (cx *compiler) hasType(name string) bool {
	_, ok := cx.o.Schema.Type(name)
	return ok
}

func (cx *compiler) typeNames() []string {
	var out []string
	for _, t := range cx.o.Schema.Types() {
		out = append(out, t.Name)
	}
	return out
}

func (cx *compiler) hasLang(code string) bool {
	_, ok := cx.langs[code]
	return ok
}

func (cx *compiler) langNames() []string { return sortedKeys(cx.langs) }

func (cx *compiler) fail(n *config.Node, format string, args ...any) {
	if n == nil {
		cx.add(fmt.Errorf(format, args...))
		return
	}
	cx.add(n.Errorf(format, args...))
}
