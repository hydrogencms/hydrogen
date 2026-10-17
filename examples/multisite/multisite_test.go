package main

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

func configureWithProbe(h *hydrogen.Builder) {
	configure(h)
	h.Use(&probeModule{})
}

type view struct {
	ID     string `json:"id"`
	Locale string `json:"locale"`
}

type node struct {
	ID string `json:"id"`
}

type located struct {
	Channel    string `json:"channel"`
	Kind       string `json:"kind"`
	Locale     string `json:"locale"`
	Canonical  string `json:"canonical"`
	Resolution struct {
		Kind     string `json:"kind"`
		Item     string `json:"item"`
		Path     string `json:"path"`
		Status   int    `json:"status"`
		Location string `json:"location"`
	} `json:"resolution"`
	Alternates []struct {
		Channel string `json:"channel"`
		Locale  string `json:"locale"`
		URL     string `json:"url"`
	} `json:"alternates"`
}

// site is the fixture: ids of the items the tests ask about.
type site struct {
	s                                *server
	home, about, contact, team, cart string
	roles, jobs                      string
	productEN, productDE, productFR  string
	productFRHidden                  string
	article                          string
}

// item creates an item and publishes it; extra locales are added as translations and published.
func (f *site) item(typ, locale string, fields map[string]any, always bool, more map[string]map[string]any) string {
	f.s.t.Helper()
	var v view
	f.s.must(f.s.do("POST", "/collections/"+typ+"/items", map[string]any{"locale": locale, "fields": fields,
		"alwaysAvailable": always}, &v), 201, "create "+typ)
	f.s.must(f.s.do("POST", "/items/"+v.ID+"/publish", map[string]any{}, nil), 200, "publish "+typ)
	for loc, fl := range more {
		f.s.must(f.s.do("PATCH", "/items/"+v.ID, map[string]any{"locale": loc, "fields": fl}, nil), 200, "translate "+typ)
		f.s.must(f.s.do("POST", "/items/"+v.ID+"/publish", map[string]any{"locales": []string{loc}}, nil), 200, "publish "+loc)
	}
	return v.ID
}

func (f *site) place(siteName, item, parent string) string {
	f.s.t.Helper()
	body := map[string]any{"kind": "page", "item": item}
	if parent != "" {
		body["parent"] = parent
	}
	var n node
	f.s.must(f.s.do("POST", "/pages/sites/"+siteName+"/nodes", body, &n), 201, "place in "+siteName)
	return n.ID
}

func title(t string) map[string]any { return map[string]any{"title": t} }

func buildSites(s *server) *site {
	f := &site{s: s}
	f.home = f.item("page", "en", title("Home"), false, map[string]map[string]any{"de": title("Startseite")})
	homeNode := f.place("corporate", f.home, "")
	f.about = f.item("page", "en", title("About"), false, map[string]map[string]any{
		"de": title("Uber uns"), "de-CH": map[string]any{"title": "Uber eus", "slug": "uber-eus"}})
	aboutNode := f.place("corporate", f.about, homeNode)
	f.contact = f.item("page", "en", title("Contact"), false, map[string]map[string]any{"de": title("Kontakt")})
	f.place("corporate", f.contact, homeNode)
	f.team = f.item("page", "en", title("Team"), false, nil)
	f.place("corporate", f.team, aboutNode)
	f.cart = f.item("page", "en", title("Cart"), false, nil)
	f.place("corporate", f.cart, homeNode)

	var mount node
	f.s.must(s.do("POST", "/pages/sites/corporate/nodes", map[string]any{"kind": "mount", "parent": homeNode, "segment": "news",
		"collection": "article", "route": "{slug}"}, &mount), 201, "mount news")
	f.article = f.item("article", "en", title("Hello"), false, nil)

	f.jobs = f.item("page", "en", title("Jobs"), false, nil)
	jobsNode := f.place("careers", f.jobs, "")
	f.roles = f.item("page", "en", title("Open roles"), false, nil)
	f.place("careers", f.roles, jobsNode)

	f.productEN = f.item("product", "en", map[string]any{"title": "Kettle", "sku": "K1"}, false, nil)
	f.productDE = f.item("product", "de", map[string]any{"title": "Wasserkocher", "sku": "K2"}, false, nil)
	f.productFR = f.item("product", "fr", map[string]any{"title": "Bouilloire", "sku": "K3"}, true, nil)
	f.productFRHidden = f.item("product", "fr", map[string]any{"title": "Theiere", "sku": "K4"}, false, nil)
	return f
}

func (f *site) locate(raw string, headers ...string) located {
	f.s.t.Helper()
	q := "/pages/locate?url=" + url.QueryEscape(raw)
	for i := 0; i+1 < len(headers); i += 2 {
		q += "&header." + headers[i] + "=" + url.QueryEscape(headers[i+1])
	}
	var l located
	f.s.must(f.s.do("GET", q, nil, &l), 200, "locate "+raw)
	return l
}

func (f *site) wantLocated(raw, channel, kind, locale, item string) located {
	f.s.t.Helper()
	l := f.locate(raw)
	if l.Channel != channel || l.Kind != kind || l.Locale != locale || (item != "" && l.Resolution.Item != item) {
		f.s.t.Errorf("%s: channel %q kind %q locale %q item %q; want %q %q %q %q", raw, l.Channel, l.Kind, l.Locale,
			l.Resolution.Item, channel, kind, locale, item)
	}
	return l
}

func (f *site) probe(path string, out any, want int, what string) {
	f.s.t.Helper()
	f.s.must(f.s.do("GET", path, nil, out), want, what)
}

func scenario(t *testing.T, s *server) {
	f := buildSites(s)
	t.Run("locate", f.locateURLs)
	t.Run("alternates", f.alternatesAndCanonical)
	t.Run("links", f.links)
	t.Run("app channel", f.appChannel)
	t.Run("redirects", f.redirects)
}

func (f *site) locateURLs(t *testing.T) {
	f.wantLocated("https://www.acme.com/en/about", "corporate_en", "page", "en", f.about)
	f.wantLocated("https://www.acme.com/en", "corporate_en", "page", "en", f.home)
	f.wantLocated("https://www.acme.com/en/", "corporate_en", "page", "en", f.home)
	f.wantLocated("https://www.acme.com/de/uber-uns", "corporate_de", "page", "de", f.about)
	f.wantLocated("https://www.acme.com/de-ch/uber-eus", "corporate_dech", "page", "de-CH", f.about)
	f.wantLocated("https://WWW.acme.com:8443/en/about/team", "corporate_en", "page", "en", f.team)
	f.wantLocated("https://www.acme.com/en/news/hello", "corporate_en", "item", "en", f.article)
	f.wantLocated("https://www.acme.sg/about", "corporate_sg_en", "page", "en-SG", f.about) // en-SG inherits en: available without a document
	f.wantLocated("https://jobs.acme.com/open-roles", "careers_en", "page", "en", f.roles)
	f.wantLocated("https://jobs.acme.com/", "careers_en", "page", "en", f.jobs)

	// de-CH inherits de: the contact page has no de-CH document, yet is served as de-CH (with de's slug).
	// No de or de-CH team, so en answers there.
	f.wantLocated("https://www.acme.com/de-ch/kontakt", "corporate_dech", "page", "de-CH", f.contact)
	f.wantLocated("https://www.acme.com/de-ch/about/team", "corporate_dech", "page", "en", f.team)
	// A path of another locale's route does not resolve in this channel.
	f.wantLocated("https://www.acme.com/de/about", "corporate_de", "not_found", "", "")

	// Sites are separate trees.
	f.wantLocated("https://jobs.acme.com/about", "careers_en", "not_found", "", "")

	// Prefixes match on segment boundaries; no matcher fits other hosts and prefixes.
	f.wantLocated("https://www.acme.com/english/about", "", "no_channel", "", "")
	f.wantLocated("https://www.acme.com/", "", "no_channel", "", "")
	f.wantLocated("https://other.test/en/about", "", "no_channel", "", "")

	// The frontend owns its reserved paths, even when a page has that path.
	f.wantLocated("https://www.acme.com/en/cart", "corporate_en", "reserved", "", "")
	f.wantLocated("https://www.acme.com/de/account", "corporate_de", "reserved", "", "")
	f.wantLocated("https://www.acme.com/de/account/orders/7", "corporate_de", "reserved", "", "")
	f.wantLocated("https://www.acme.com/en/accounts", "corporate_en", "not_found", "", "")

	f.s.must(f.s.do("GET", "/pages/locate", nil, nil), 400, "locate without url")
}

func (f *site) alternatesAndCanonical(t *testing.T) {
	type alt struct{ Channel, Locale, URL string }
	wantAlts := func(l located, want []alt) {
		t.Helper()
		var got []alt
		for _, a := range l.Alternates {
			got = append(got, alt{a.Channel, a.Locale, a.URL})
		}
		if len(got) != len(want) {
			t.Errorf("alternates %v, want %v", got, want)
			return
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("alternate %d: %v, want %v", i, got[i], want[i])
			}
		}
	}
	about := []alt{
		{"corporate_de", "de", "https://www.acme.com/de/uber-uns"},
		{"corporate_dech", "de-CH", "https://www.acme.com/de-ch/uber-eus"},
		{"corporate_en", "en", "https://www.acme.com/en/about"},
		{"corporate_sg_en", "en-SG", "https://www.acme.sg/about"},
	}
	// corporate_sg_en is an alternate: en-SG inherits en, so the page has an en-SG route.
	l := f.locate("https://www.acme.com/en/about")
	wantAlts(l, about)
	if l.Canonical != "https://www.acme.com/en/about" {
		t.Errorf("canonical: %s", l.Canonical)
	}
	l = f.locate("https://www.acme.com/de-ch/uber-eus")
	wantAlts(l, about)
	if l.Canonical != "https://www.acme.com/de-ch/uber-eus" {
		t.Errorf("canonical de-CH: %s", l.Canonical)
	}
	// Served as de-CH through inheritance: it is an alternate of its own, and the canonical is this channel's.
	l = f.locate("https://www.acme.com/de-ch/kontakt")
	wantAlts(l, []alt{
		{"corporate_de", "de", "https://www.acme.com/de/kontakt"},
		{"corporate_dech", "de-CH", "https://www.acme.com/de-ch/kontakt"},
		{"corporate_en", "en", "https://www.acme.com/en/contact"},
		{"corporate_sg_en", "en-SG", "https://www.acme.sg/contact"},
	})
	if l.Canonical != "https://www.acme.com/de-ch/kontakt" {
		t.Errorf("canonical fallback: %s", l.Canonical)
	}
	// The market channel serves the inherited en-SG; it is its own canonical.
	l = f.locate("https://www.acme.sg/about")
	if l.Canonical != "https://www.acme.sg/about" {
		t.Errorf("canonical sg: %s", l.Canonical)
	}
	// An item mounted under the tree has alternates too; the second site has no group, so only its own site.
	l = f.locate("https://www.acme.com/en/news/hello")
	if len(l.Alternates) == 0 || l.Canonical != "https://www.acme.com/en/news/hello" {
		t.Errorf("article: %+v", l)
	}
	l = f.locate("https://jobs.acme.com/open-roles")
	wantAlts(l, []alt{{"careers_en", "en", "https://jobs.acme.com/open-roles"}})
	if l.Canonical != "https://jobs.acme.com/open-roles" {
		t.Errorf("canonical careers: %s", l.Canonical)
	}
}

func (f *site) links(t *testing.T) {
	var out struct {
		URL   string `json:"url"`
		Found bool   `json:"found"`
	}
	link := func(from, item string) string {
		t.Helper()
		f.probe("/probe/link?from="+from+"&item="+item, &out, 200, "link")
		return out.URL
	}
	// Same channel: relative. Another site: absolute.
	if got := link("corporate_en", f.about); got != "/en/about" {
		t.Errorf("same channel: %s", got)
	}
	if got := link("corporate_dech", f.contact); got != "/de-ch/kontakt" {
		t.Errorf("same channel, fallback language: %s", got)
	}
	if got := link("corporate_en", f.roles); got != "https://jobs.acme.com/open-roles" {
		t.Errorf("cross site: %s", got)
	}
	if got := link("careers_en", f.about); got != "https://www.acme.com/en/about" {
		t.Errorf("cross site, language preferred: %s", got)
	}
	// An app channel has no URLs of its own: the best web channel in its languages (en first).
	if got := link("ios_app", f.about); got != "https://www.acme.com/en/about" {
		t.Errorf("app: %s", got)
	}
	f.probe("/probe/link?from=corporate_en&item="+f.productEN, &out, 404, "link to an item without route")

	f.probe("/probe/url?channel=corporate_dech&locale=de&item="+f.about, &out, 200, "url")
	if !out.Found || out.URL != "https://www.acme.com/de-ch/uber-uns" {
		t.Errorf("url: %+v", out)
	}
	f.probe("/probe/url?channel=corporate_sg_en&locale=en&item="+f.about, &out, 200, "url sg")
	if out.URL != "https://www.acme.sg/about" {
		t.Errorf("url sg: %+v", out)
	}
	f.probe("/probe/url?channel=careers_en&locale=en&item="+f.about, &out, 200, "url other site")
	if out.Found {
		t.Errorf("about has no route in careers: %+v", out)
	}
}

func (f *site) appChannel(t *testing.T) {
	var got struct{ Locale, Type string }
	get := func(item string) int {
		return f.s.do("GET", "/probe/get?channel=ios_app&item="+item, nil, &got)
	}
	if st := get(f.about); st != 404 {
		t.Errorf("page type on the app channel: %d", st)
	}
	if st := get(f.article); st != 404 {
		t.Errorf("article on the app channel: %d", st)
	}
	if st := get(f.productEN); st != 200 || got.Locale != "en" {
		t.Errorf("product en: %d %+v", st, got)
	}
	if st := get(f.productDE); st != 200 || got.Locale != "de" {
		t.Errorf("product de (second language): %d %+v", st, got)
	}
	if st := get(f.productFR); st != 200 || got.Locale != "fr" {
		t.Errorf("always available product falls back to its main locale: %d %+v", st, got)
	}
	if st := get(f.productFRHidden); st != 404 {
		t.Errorf("product without a usable locale: %d", st)
	}
	// A web channel that lists the collection reads it too.
	if st := f.s.do("GET", "/probe/get?channel=corporate_en&item="+f.productEN, nil, &got); st != 200 || got.Locale != "en" {
		t.Errorf("product on a web channel: %d %+v", st, got)
	}
}

func (f *site) redirects(t *testing.T) {
	f.s.must(f.s.do("PATCH", "/items/"+f.contact, map[string]any{"locale": "en", "fields": map[string]any{"slug": "reach-us"}}, nil), 200, "rename")
	f.s.must(f.s.do("POST", "/items/"+f.contact+"/publish", map[string]any{}, nil), 200, "publish rename")
	l := f.locate("https://www.acme.com/en/contact")
	if l.Kind != "redirect" || l.Resolution.Status != 301 || l.Resolution.Location != "/en/reach-us" || l.Channel != "corporate_en" {
		t.Errorf("history redirect keeps the channel prefix: %+v", l)
	}
	f.wantLocated("https://www.acme.com/en/reach-us", "corporate_en", "page", "en", f.contact)
}

func TestMultisiteSQLite(t *testing.T) {
	scenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configureWithProbe))
}

func TestMultisitePostgres(t *testing.T) {
	scenario(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configureWithProbe))
}

// checkConfig runs config:check on a copy of the example's config with channels.yaml replaced.
func checkConfig(t *testing.T, channels string) error {
	t.Helper()
	dir := t.TempDir()
	app, err := os.ReadFile(filepath.Join("config", "app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"app.yaml": app, "channels.yaml": []byte(channels)} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	opts := &config.Options{Dir: dir, Env: config.DevEnv, LookupEnv: func(k string) (string, bool) {
		return map[string]string{"SCHEMA_DIR": "schema", "DATABASE_URL": "sqlite:" + filepath.Join(dir, "h.db")}[k], k == "SCHEMA_DIR" || k == "DATABASE_URL"
	}}
	return hydrogen.Run(context.Background(), []string{"config:check"}, configure, hydrogen.RunOptions{Config: opts, Stdout: io.Discard, Stderr: io.Discard})
}

func TestConfigCheck(t *testing.T) {
	if err := checkConfig(t, "channels:\n  a: { languages: [en], match: { host: a.test } }\n"); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	const head = "channels:\n"
	for _, c := range []struct {
		name, yaml string
		want       []string
	}{
		{"duplicate matcher", head + "  a: { languages: [en], match: { host: a.test, prefix: /en } }\n  b: { languages: [en], match: { host: a.test, prefix: /en } }\n",
			[]string{"a and b both match"}},
		{"unknown site", head + "  a: { languages: [en], site: nowhere, match: { host: a.test } }\n",
			[]string{"channels.yaml:2", `unknown site "nowhere"`}},
		{"unknown collection", head + "  a: { languages: [en], collections: [ghost], match: { host: a.test } }\n",
			[]string{"channels.yaml:2", "ghost"}},
		{"unknown language", "languages:\n  en: {}\nchannels:\n  a: { languages: [en, de], match: { host: a.test } }\n",
			[]string{"channels.yaml:4", "de"}},
		{"bad prefix", head + "  a: { languages: [en], match: { prefix: en } }\n",
			[]string{"channels.yaml:2", "prefix"}},
		{"upper-case host", head + "  a: { languages: [en], match: { host: A.test } }\n",
			[]string{"channels.yaml:2", "lower-case"}},
		{"non-ASCII host", head + "  a: { languages: [en], match: { host: bücher.test } }\n",
			[]string{"channels.yaml:2", "ASCII", "punycode"}},
		{"empty match", head + "  a: { languages: [en], match: {} }\n",
			[]string{"channels.yaml:2", "at least one"}},
		{"unknown match key", head + "  a: { languages: [en], match: { hots: a.test } }\n",
			[]string{"channels.yaml:2", "hots"}},
		{"bad reserved", head + "  a: { languages: [en], match: { host: a.test }, reserved: [cart] }\n",
			[]string{"channels.yaml:2", "reserved"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := checkConfig(t, c.yaml)
			if err == nil {
				t.Fatal("config:check passed")
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

// tokenConfigCheck runs config:check on the example's own config with the given environment.
func tokenConfigCheck(env string, vars map[string]string) error {
	vars["SCHEMA_DIR"] = "schema"
	vars["DATABASE_URL"] = "sqlite:" + filepath.Join(os.TempDir(), "multisite-token-check.db")
	vars["MANAGE_TOKEN"] = token
	vars["PREVIEW_SECRET"] = "test-preview-secret-0123456789abcdef"
	opts := &config.Options{Dir: "config", Env: env, LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	return hydrogen.Run(context.Background(), []string{"config:check"}, configure, hydrogen.RunOptions{Config: opts, Stdout: io.Discard, Stderr: io.Discard})
}

// The app token's placeholder default is for development only, and it is long enough to be a valid token.
func TestTokenDefaultIsDevOnly(t *testing.T) {
	if err := tokenConfigCheck(config.DevEnv, map[string]string{}); err != nil {
		t.Errorf("dev without IOS_TOKEN: %v", err)
	}
	if err := tokenConfigCheck("prod", map[string]string{}); err == nil {
		t.Error("prod without IOS_TOKEN passed: the dev default must not apply")
	}
	if err := tokenConfigCheck("prod", map[string]string{"IOS_TOKEN": iosToken}); err != nil {
		t.Errorf("prod with IOS_TOKEN: %v", err)
	}
	if err := tokenConfigCheck("prod", map[string]string{"IOS_TOKEN": "short"}); err == nil {
		t.Error("a short token passed")
	}
}

// TestGeneratedCodeIsCurrent regenerates into a temp dir and compares with gen/.
func TestGeneratedCodeIsCurrent(t *testing.T) {
	dir := t.TempDir()
	opts := &config.Options{Dir: "config", LookupEnv: func(string) (string, bool) { return "", false }}
	args := []string{"generate", "-go", filepath.Join(dir, "content"), "-ts", filepath.Join(dir, "types.ts")}
	if err := hydrogen.Run(context.Background(), args, configure, hydrogen.RunOptions{Config: opts, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "content", "*.go"))
	files = append(files, filepath.Join(dir, "types.ts"))
	for _, f := range files {
		rel := filepath.Base(f)
		committed := filepath.Join("gen", "content", rel)
		if rel == "types.ts" {
			committed = filepath.Join("gen", rel)
		}
		want, _ := os.ReadFile(f)
		got, err := os.ReadFile(committed)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run hydrogen generate", committed)
		}
	}
	committed, _ := filepath.Glob(filepath.Join("gen", "content", "*.go"))
	if len(committed) != len(files)-1 {
		t.Errorf("gen/content has %d files, generate wrote %d", len(committed), len(files)-1)
	}
}
