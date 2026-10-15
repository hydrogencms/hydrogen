package channel_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/schema"
)

const testSchema = `
types:
  article:
    fields:
      title: { type: string, translatable: true, required: true }
      slug:  { type: string }
      cat:   { type: group, fields: { slug: { type: string } } }
  product:
    fields:
      sku: { type: string }
  home:
    singleton: true
    fields:
      title: { type: string }
`

func testSchemaOf(tb testing.TB) *schema.Schema {
	tb.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": &fstest.MapFile{Data: []byte(testSchema)}}, "schema"); err != nil {
		tb.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func node(tb testing.TB, yaml string, vars map[string]string) *config.Node {
	tb.Helper()
	dir := tb.TempDir()
	tb.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "channels.yaml"), []byte(yaml), 0o644); err != nil {
		tb.Fatal(err)
	}
	c, err := config.Load(config.Options{Dir: ".", LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }})
	if err != nil {
		tb.Fatal(err)
	}
	n, err := c.Node("channels")
	if err != nil {
		tb.Fatal(err)
	}
	return n
}

func compile(tb testing.TB, yaml string, o channel.CompileOptions) (*channel.Set, error) {
	tb.Helper()
	if o.Schema == nil {
		o.Schema = testSchemaOf(tb)
	}
	if o.DefaultLocale == "" {
		o.DefaultLocale = "en"
	}
	return channel.Compile(node(tb, yaml, map[string]string{"IOS": "ios-secret-0123456", "KIOSK": "kiosk-secret-012345", "TOKEN_SG": "sg-token-0123456789", "TOKEN_MY": "my-token-0123456789", "BRACES": "abcdefghijklmnop{market}", "SHORT": "short", "SPACED": "has a space in the token"}), o)
}

const full = `
languages:
  en: {}
  de: {}
  de-CH: { inherits: de }
  en-SG: {}
defaults:
  settings: { cache_ttl: 300, image: { preset: web, quality: 80 } }
groups:
  corporate_web:
    frontend: https://www.acme.com/
    collections: [article, product]
    preview: { article: "/news/{slug}", "*": "/preview/{collection}/{id}" }
    settings: { image: { quality: 90 }, theme: dark }
    site: corporate
channels:
  corporate_en:   { group: corporate_web, languages: [en] }
  corporate_dech: { group: corporate_web, languages: [de-CH, de, en], collections: [article], settings: { cache_ttl: 60 } }
  ios_app:
    languages: [en, de]
    collections: [product]
    access: token
    tokens: { v1: "${env:IOS}" }
    site: mobile
templates:
  - for: { market: [sg], tier: [a, b] }
    name: "corporate_{market}_{tier}"
    group: corporate_web
    languages: ["en-{MARKET}", en]
    site: "corp-{market}"
    settings: { label: "{MARKET}" }
`

func siteOption() channel.Option {
	return channel.Option{Name: "site", Decode: func(n *config.Node) (any, error) {
		s, err := n.String()
		if err != nil {
			return nil, err
		}
		if s == "bad" {
			return nil, n.Errorf("unknown site %q", s)
		}
		return s, nil
	}}
}

func TestCompileFull(t *testing.T) {
	s, err := compile(t, full, channel.CompileOptions{Options: []channel.Option{siteOption()}})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range s.All() {
		names = append(names, c.Name)
	}
	want := []string{"corporate_dech", "corporate_en", "corporate_sg_a", "corporate_sg_b", "ios_app"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("channels %v", names)
	}
	en, _ := s.Get("corporate_en")
	if en.Group != "corporate_web" || en.Frontend != "https://www.acme.com" || !reflect.DeepEqual(en.Collections, []string{"article", "product"}) {
		t.Fatalf("%+v", en)
	}
	if en.Options["site"] != "corporate" || en.Access != channel.AccessPublic {
		t.Fatalf("%+v", en)
	}
	if v, _ := en.Setting("image.quality"); v != int64(90) {
		t.Fatalf("deep merge: %v", v)
	}
	if v, _ := en.Setting("image.preset"); v != "web" {
		t.Fatalf("deep merge: %v", v)
	}
	if v, _ := en.Setting("cache_ttl"); v != int64(300) {
		t.Fatal(v)
	}
	dech, _ := s.Get("corporate_dech")
	if v, _ := dech.Setting("cache_ttl"); v != int64(60) || !reflect.DeepEqual(dech.Collections, []string{"article"}) {
		t.Fatalf("%+v", dech)
	}
	sg, _ := s.Get("corporate_sg_b")
	if !reflect.DeepEqual(sg.Languages, []string{"en-SG", "en"}) || sg.Options["site"] != "corp-sg" {
		t.Fatalf("%+v", sg)
	}
	if v, _ := sg.Setting("label"); v != "SG" {
		t.Fatal(v)
	}
	if got := s.Group("corporate_web"); len(got) != 4 || got[0].Name != "corporate_dech" {
		t.Fatalf("group %v", got)
	}
	if got := s.Group("nope"); len(got) != 0 {
		t.Fatal(got)
	}
	ios, ok := s.ByToken("ios-secret-0123456")
	if !ok || ios.Name != "ios_app" || ios.Access != channel.AccessToken {
		t.Fatalf("%v %v", ios, ok)
	}
	if _, ok := s.ByToken("ios-secret-0123457"); ok {
		t.Fatal("wrong token accepted")
	}
	if _, ok := s.ByToken(""); ok {
		t.Fatal("empty token accepted")
	}
	if l := s.Languages(); len(l) != 4 || l["de-CH"].Inherits != "de" {
		t.Fatalf("%v", l)
	}
	if ios.Options["site"] != "mobile" || ios.Group != "" || ios.Frontend != "" {
		t.Fatalf("%+v", ios)
	}
}

func TestOptionsInheritedWholesaleAndTokensNot(t *testing.T) {
	s, err := compile(t, `
defaults: { site: d, languages: [en] }
groups: { g: { site: g } }
channels:
  a: {}
  b: { group: g }
  c: { group: g, site: c }
`, channel.CompileOptions{Options: []channel.Option{siteOption()}})
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"a": "d", "b": "g", "c": "c"} {
		c, _ := s.Get(name)
		if c.Options["site"] != want {
			t.Errorf("%s: %v", name, c.Options)
		}
	}
	_, err = compile(t, "defaults: { languages: [en], access: token, tokens: { a: \"${env:IOS}\" } }\nchannels: { a: {} }\n", channel.CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "tokens belong to one channel") {
		t.Fatalf("got %v", err)
	}
}

func TestImplicitDefault(t *testing.T) {
	for _, n := range []*config.Node{nil, node(t, "defaults: { settings: { a: 1 } }\n", nil)} {
		s, err := channel.Compile(n, channel.CompileOptions{DefaultLocale: "de"})
		if err != nil {
			t.Fatal(err)
		}
		all := s.All()
		if len(all) != 1 || all[0].Name != channel.DefaultName || !reflect.DeepEqual(all[0].Languages, []string{"de"}) ||
			all[0].Collections != nil || all[0].Access != channel.AccessPublic || !all[0].AllowsCollection("anything") {
			t.Fatalf("%+v", all)
		}
	}
	s, _ := channel.Compile(node(t, "defaults: { settings: { a: 1 } }\n", nil), channel.CompileOptions{})
	if v, _ := s.All()[0].Setting("a"); v != int64(1) {
		t.Fatal("defaults must apply to the implicit channel")
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"channel name", "channels: { Bad-Name: { languages: [en] } }", `channels.yaml:1: channel "Bad-Name": name:`},
		{"group name", "groups: { Bad: {} }\n", `group name "Bad"`},
		{"missing languages", "channels:\n  a: {}\n", `channels.yaml:2: channel "a": languages are required`},
		{"bad language", "channels:\n  a: { languages: [EN_us] }\n", `language "EN_us" is not a locale code`},
		{"language not in registry", "languages: { en: {} }\nchannels:\n  a: { languages: [en, fr] }\n", `channels.yaml:3: language "fr" is not in the languages registry (have: en)`},
		{"language twice", "channels:\n  a: { languages: [en, en] }\n", `language "en" is listed twice`},
		{"empty languages", "channels:\n  a: { languages: [] }\n", "languages is empty"},
		{"inherits unknown", "languages:\n  en: { inherits: xx }\n", `inherits "xx", which is not in the registry`},
		{"inherits cycle", "languages:\n  en: { inherits: de }\n  de: { inherits: en }\n", "inherits in a cycle"},
		{"unknown collection", "channels:\n  a: { languages: [en], collections: [article, nope] }\n", `unknown collection "nope" (types: article, home, product)`},
		{"empty collections", "channels:\n  a: { languages: [en], collections: [] }\n", "omit the key to allow every collection"},
		{"unknown key", "channels:\n  a: { languages: [en], colour: red }\n", `channels.yaml:2: unknown key "colour" in channel (known: access, collections, frontend, group, languages, links, preview, settings, site, tokens)`},
		{"unknown group", "channels:\n  a: { languages: [en], group: nope }\n", `unknown group "nope"`},
		{"frontend scheme", "channels:\n  a: { languages: [en], frontend: www.acme.com }\n", "absolute http(s) URL"},
		{"frontend query", "channels:\n  a: { languages: [en], frontend: \"https://a.com/?x=1\" }\n", "no credentials, query or fragment"},
		{"access value", "channels:\n  a: { languages: [en], access: private }\n", `access "private" is not public or token`},
		{"token without tokens", "channels:\n  a: { languages: [en], access: token }\n", "access is token but no tokens are set"},
		{"tokens on public", "channels:\n  a: { languages: [en], tokens: { x: \"${env:IOS}\" } }\n", "tokens are set but access is public"},
		{"literal token", "channels:\n  a: { languages: [en], access: token, tokens: { x: plain } }\n", "is a secret and must come from ${env:NAME}"},
		{"duplicate token", "channels:\n  a: { languages: [en], access: token, tokens: { x: \"${env:IOS}\" } }\n  b: { languages: [en], access: token, tokens: { x: \"${env:IOS}\" } }\n", `channel "b" uses a token that channel "a" also has`},
		{"preview unknown collection", "channels:\n  a: { languages: [en], preview: { nope: \"/x\" } }\n", `preview for unknown collection "nope"`},
		{"preview unknown field", "channels:\n  a: { languages: [en], preview: { article: \"/n/{nope}\" } }\n", `article has no field "nope"`},
		{"preview relative", "channels:\n  a: { languages: [en], preview: { article: \"n/{slug}\" } }\n", "must start with a single /"},
		{"preview braces", "channels:\n  a: { languages: [en], preview: { article: \"/n/{slug\" } }\n", "unbalanced braces"},
		{"links unknown collection", "channels:\n  a: { languages: [en], links: { nope: \"/x\" } }\n", `links for unknown collection "nope"`},
		{"links unknown field", "channels:\n  a: { languages: [en], links: { article: \"/n/{nope}\" } }\n", `links placeholder {nope}: article has no field "nope"`},
		{"links relative", "channels:\n  a: { languages: [en], links: { article: \"n/{slug}\" } }\n", "links pattern \"n/{slug}\" must start with a single /"},
		{"links protocol relative", "channels:\n  a: { languages: [en], links: { article: \"//evil.com/{slug}\" } }\n", "must start with a single /"},
		{"links braces", "channels:\n  a: { languages: [en], links: { article: \"/n/{slug\" } }\n", "unbalanced braces"},
		{"links fallback", "channels:\n  a: { languages: [en], links: { \"*\": \"/{id}\" } }\n", "links has no fallback pattern"},
		{"links not a mapping", "channels:\n  a: { languages: [en], links: x }\n", "links must be a mapping"},
		{"top key", "chanels: {}\n", `unknown key "chanels" in channels`},
		{"duplicate channel", "channels:\n  corp_sg: { languages: [en] }\ntemplates:\n  - for: { m: [sg] }\n    name: corp_{m}\n    languages: [en]\n", `channel "corp_sg" is already defined at`},
		{"template no for", "templates:\n  - name: x\n    languages: [en]\n", "template needs `for`"},
		{"template no name", "templates:\n  - for: { m: [a] }\n    languages: [en]\n", "template needs a name"},
		{"template same name", "templates:\n  - for: { m: [a, b] }\n    name: fixed\n    languages: [en]\n", `channel "fixed" is already defined`},
		{"name outside template", "channels:\n  a: { languages: [en], name: x }\n", "name is only valid in templates"},
		{"option error", "channels:\n  a: { languages: [en], site: bad }\n", `channels.yaml:2: unknown site "bad" (channel "a", option site)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := compile(t, tc.yaml, channel.CompileOptions{Options: []channel.Option{siteOption()}})
			if err == nil || s != nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestErrorsJoinedAndSorted(t *testing.T) {
	yaml := "channels:\n"
	for _, n := range []string{"a", "b"} {
		yaml += "  " + n + ": { languages: [en], collections: [nope] }\n"
	}
	yaml += "  c:\n    colour: red\n    languages: [en]\n"
	_, err := compile(t, yaml, channel.CompileOptions{})
	if err == nil {
		t.Fatal("want errors")
	}
	lines := strings.Split(err.Error(), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "channels.yaml:2:") || !strings.HasPrefix(lines[2], "channels.yaml:5:") {
		t.Fatalf("%q", lines)
	}
	_, again := compile(t, yaml, channel.CompileOptions{})
	if again.Error() != err.Error() {
		t.Fatal("not deterministic")
	}
}

func TestNaturalOrderAcrossTenLines(t *testing.T) {
	yaml := "channels:\n" + strings.Repeat("  # c\n", 7) + "  late: { languages: [en], collections: [nope] }\n  early: { languages: [en], collections: [nope] }\n"
	yaml = strings.Replace(yaml, "  early: {", "  early: {", 1)
	_, err := compile(t, yaml, channel.CompileOptions{})
	lines := strings.Split(err.Error(), "\n")
	if !strings.HasPrefix(lines[0], "channels.yaml:9:") || !strings.HasPrefix(lines[1], "channels.yaml:10:") {
		t.Fatalf("%q", lines)
	}
}

func TestChecksRunLastAndOnlyOnCleanSets(t *testing.T) {
	boom := errors.New("overlap")
	var seen int
	chk := func(s *channel.Set) error { seen = len(s.All()); return boom }
	_, err := compile(t, "channels: { a: { languages: [en] }, b: { languages: [en] } }", channel.CompileOptions{Checks: []channel.Check{chk}})
	if !errors.Is(err, boom) || seen != 2 {
		t.Fatalf("%v %d", err, seen)
	}
	seen = 0
	_, err = compile(t, "channels: { a: {} }", channel.CompileOptions{Checks: []channel.Check{chk}})
	if err == nil || seen != 0 {
		t.Fatalf("checks must not run after errors: %v %d", err, seen)
	}
}

func TestOptionRegistrationErrors(t *testing.T) {
	dec := func(*config.Node) (any, error) { return nil, nil }
	for _, op := range []channel.Option{{Name: "languages", Decode: dec}, {Name: "x"}} {
		if _, err := compile(t, "channels: { a: { languages: [en] } }", channel.CompileOptions{Options: []channel.Option{op}}); err == nil {
			t.Errorf("%v accepted", op.Name)
		}
	}
	both := []channel.Option{{Name: "x", Decode: dec}, {Name: "x", Decode: dec}}
	if _, err := compile(t, "channels: { a: { languages: [en] } }", channel.CompileOptions{Options: both}); err == nil {
		t.Error("duplicate accepted")
	}
}

func TestOptionUnknownKeyListsKnown(t *testing.T) {
	op := channel.Option{Name: "match", Decode: func(n *config.Node) (any, error) {
		var m struct {
			Host string `yaml:"host"`
		}
		return m, n.Decode(&m)
	}}
	_, err := compile(t, "channels:\n  a:\n    languages: [en]\n    match: { hots: x }\n", channel.CompileOptions{Options: []channel.Option{op}})
	if err == nil || !strings.Contains(err.Error(), `channels.yaml:4: unknown key "hots"`) || !strings.Contains(err.Error(), "known: host") {
		t.Fatalf("got %v", err)
	}
}

func TestPickLocale(t *testing.T) {
	c := &channel.Channel{Languages: []string{"de-CH", "de", "en"}}
	cases := []struct {
		name      string
		available []string
		main      string
		always    bool
		want      string
		fallback  bool
		ok        bool
	}{
		{"first", []string{"de-CH", "en"}, "en", false, "de-CH", false, true},
		{"second", []string{"de", "en"}, "en", false, "de", true, true},
		{"order of channel not item", []string{"en", "de"}, "en", false, "de", true, true},
		{"last", []string{"en"}, "en", false, "en", true, true},
		{"main when always available", []string{"fr"}, "fr", true, "fr", true, true},
		{"not always available", []string{"fr"}, "fr", false, "", false, false},
		{"main missing", []string{"fr"}, "it", true, "", false, false},
		{"nothing", nil, "en", true, "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, fb, ok := channel.PickLocale(c, tc.available, tc.main, tc.always)
			if l != tc.want || fb != tc.fallback || ok != tc.ok {
				t.Fatalf("got %q %v %v", l, fb, ok)
			}
		})
	}
	nc := &channel.Channel{}
	if l, fb, ok := channel.PickLocale(nc, []string{"en"}, "en", true); l != "en" || !fb || !ok {
		t.Fatal(l, fb, ok)
	}
}

func TestLinkAndPreviewPatterns(t *testing.T) {
	c := &channel.Channel{Frontend: "https://www.acme.com",
		Preview: map[string]string{"article": "/preview/{slug}", "*": "/p/{collection}/{id}"},
		Links:   map[string]string{"article": "/news/{slug}", "post": "/blog/{slug}"}}
	fields := func(name string) (string, bool) { return map[string]string{"slug": "a b"}[name], name == "slug" }
	if p, ok := c.LinkPath("post", "1", "en", fields); !ok || p != "/blog/a%20b" {
		t.Errorf("link path %q %v", p, ok)
	}
	if _, ok := c.LinkPath("page", "1", "en", fields); ok {
		t.Error("a collection without a link pattern has no link")
	}
	if _, ok := c.LinkPath("post", "1", "en", nil); ok {
		t.Error("a missing field must fail")
	}
	// A preview pattern wins; without one the link pattern is used; then the "*" fallback.
	for coll, want := range map[string]string{"article": "/preview/a%20b", "post": "/blog/a%20b", "page": "/p/page/1"} {
		if p, ok := c.PreviewPath(coll, "1", "en", fields); !ok || p != want {
			t.Errorf("preview path of %s = %q %v, want %q", coll, p, ok, want)
		}
	}
}

func TestPreviewURL(t *testing.T) {
	c := &channel.Channel{Frontend: "https://www.acme.com", Preview: map[string]string{
		"article": "/news/{slug}", "page": "/p/{path}", "x": "/{id}", "*": "/preview/{collection}/{id}?l={locale}"}}
	fields := func(name string) (string, bool) {
		v, ok := map[string]string{"slug": "hello world", "path": "a/b c/d", "cat.slug": "x"}[name]
		return v, ok
	}
	cases := []struct {
		coll, want string
		ok         bool
	}{
		{"article", "https://www.acme.com/news/hello%20world", true},
		{"page", "https://www.acme.com/p/a%2Fb%20c%2Fd", true},
		{"product", "https://www.acme.com/preview/product/42?l=de", true},
	}
	for _, tc := range cases {
		got, ok := c.PreviewURL(tc.coll, "42", "de", fields)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: %q %v", tc.coll, got, ok)
		}
	}
	if _, ok := c.PreviewURL("article", "1", "en", func(string) (string, bool) { return "", false }); ok {
		t.Error("missing field must fail")
	}
	if _, ok := c.PreviewURL("article", "1", "en", nil); ok {
		t.Error("nil field func must fail")
	}
	none := &channel.Channel{Preview: map[string]string{"article": "/x"}}
	if _, ok := none.PreviewURL("product", "1", "en", nil); ok {
		t.Error("no pattern")
	}
	if got, ok := none.PreviewURL("article", "1", "en", nil); !ok || got != "/x" {
		t.Error(got, ok)
	}
	dot := &channel.Channel{Preview: map[string]string{"article": "/c/{cat.slug}"}}
	if got, _ := dot.PreviewURL("article", "1", "en", fields); got != "/c/x" {
		t.Error(got)
	}
}

func TestSettingAndAllows(t *testing.T) {
	c := &channel.Channel{Settings: map[string]any{"a": map[string]any{"b": int64(1)}, "s": "x"}, Collections: []string{"article"}}
	for path, want := range map[string]any{"a.b": int64(1), "s": "x"} {
		if v, ok := c.Setting(path); !ok || v != want {
			t.Errorf("%s: %v", path, v)
		}
	}
	for _, p := range []string{"a.c", "s.x", "zz", "a.b.c"} {
		if _, ok := c.Setting(p); ok {
			t.Errorf("%s found", p)
		}
	}
	if !c.AllowsCollection("article") || c.AllowsCollection("product") {
		t.Error("allows")
	}
}

func TestSetIsIsolated(t *testing.T) {
	s, err := compile(t, "languages: { en: {} }\nchannels: { a: { languages: [en] } }\n", channel.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s.All()[0] = nil
	delete(s.Languages(), "en")
	if _, ok := s.Get("a"); !ok || len(s.Languages()) != 1 || s.All()[0] == nil {
		t.Fatal("set mutated through accessors")
	}
}

func BenchmarkPickLocale(b *testing.B) {
	c := &channel.Channel{Languages: []string{"de-CH", "de", "en"}}
	avail := []string{"en", "fr", "it"}
	for b.Loop() {
		channel.PickLocale(c, avail, "fr", true)
	}
}

func BenchmarkByToken(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("channels:\n")
	vars := map[string]string{}
	for i := range 50 {
		n := string(rune('a'+i/26)) + string(rune('a'+i%26))
		sb.WriteString("  c" + n + ": { languages: [en], access: token, tokens: { t: \"${env:T" + n + "}\" } }\n")
		vars["T"+n] = "secret-token-0123-" + n
	}
	s, err := channel.Compile(node(b, sb.String(), vars), channel.CompileOptions{})
	if err != nil {
		b.Fatal(err)
	}
	for b.Loop() {
		s.ByToken("secret-token-0123-ax")
	}
}

func TestPreviewURLRefusesUnsafeValues(t *testing.T) {
	c := &channel.Channel{Preview: map[string]string{"a": "/{slug}", "b": "/x/{slug}/y"}}
	val := func(v string) func(string) (string, bool) {
		return func(string) (string, bool) { return v, true }
	}
	for _, v := range []string{"", ".", ".."} {
		if got, ok := c.PreviewURL("a", "1", "en", val(v)); ok {
			t.Errorf("%q accepted: %q", v, got)
		}
	}
	if got, ok := c.PreviewURL("a", "1", "en", val("/evil.com/x")); !ok || got != "/%2Fevil.com%2Fx" {
		t.Errorf("slash: %q %v", got, ok)
	}
	if got, ok := c.PreviewURL("b", "1", "en", val("../..")); !ok || strings.Contains(got, "/../") || got != "/x/..%2F../y" {
		t.Errorf("dots: %q %v", got, ok)
	}
	if got, ok := c.PreviewURL("a", "", "en", val("v")); !ok || got != "/v" {
		t.Errorf("%q %v", got, ok)
	}
	idc := &channel.Channel{Preview: map[string]string{"*": "/{id}"}}
	if _, ok := idc.PreviewURL("a", "", "en", nil); ok {
		t.Error("empty id accepted")
	}
}

func TestTemplateEnvPerExpansionAndBraceSafety(t *testing.T) {
	s, err := compile(t, `
templates:
  - for: { market: [sg, my] }
    name: "web_{market}"
    languages: [en]
    access: token
    tokens: { v1: "${env:TOKEN_{MARKET}}" }
    settings: { note: "${env:BRACES}" }
`, channel.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := s.ByToken("sg-token-0123456789"); !ok || c.Name != "web_sg" {
		t.Fatalf("%v %v", c, ok)
	}
	if c, ok := s.ByToken("my-token-0123456789"); !ok || c.Name != "web_my" {
		t.Fatalf("%v %v", c, ok)
	}
	sg, _ := s.Get("web_sg")
	if v, _ := sg.Setting("note"); v != "abcdefghijklmnop{market}" {
		t.Fatalf("env value rewritten: %v", v)
	}
}

func TestTemplateMissingEnvNamesExpandedVariable(t *testing.T) {
	_, err := compile(t, `
templates:
  - for: { market: [nz] }
    name: "web_{market}"
    languages: [en]
    access: token
    tokens: { v1: "${env:TOKEN_{MARKET}}" }
`, channel.CompileOptions{})
	if err == nil || !strings.Contains(err.Error(), "TOKEN_NZ") {
		t.Fatalf("got %v", err)
	}
}

func TestBadTokenIsReportedOnceAndNeverEchoed(t *testing.T) {
	_, err := compile(t, "channels:\n  a: { languages: [en], access: token, tokens: { x: plain-literal-token-1234 } }\n", channel.CompileOptions{})
	if err == nil || strings.Contains(err.Error(), "no tokens are set") || strings.Contains(err.Error(), "plain-literal") {
		t.Fatalf("got %v", err)
	}
	_, err = compile(t, "channels:\n  a: { languages: [en], access: token, tokens: { x: \"${env:SHORT}\" } }\n", channel.CompileOptions{})
	if err == nil || strings.Contains(err.Error(), "no tokens are set") || strings.Contains(err.Error(), "short\"") {
		t.Fatalf("got %v", err)
	}
}

func TestSettingDoesNotAllocate(t *testing.T) {
	c := &channel.Channel{Settings: map[string]any{"a": map[string]any{"b": map[string]any{"c": int64(1)}}}}
	if n := testing.AllocsPerRun(100, func() { c.Setting("a.b.c") }); n != 0 {
		t.Fatalf("%v allocs", n)
	}
}
