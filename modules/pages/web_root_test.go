package pages

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
)

func TestRootReservedMatch(t *testing.T) {
	roots := map[string]any{optRootReserved: []string{"/legacy", "/ajax/*"}, optReserved: []string{"/cart"}}
	x := newWebIndex([]*channel.Channel{
		web("global-en", "", []string{"en"}, matcher{Host: "www.acme.com", Prefix: "/en"}, roots),
		web("global-de", "", []string{"de"}, matcher{Host: "www.acme.com", Prefix: "/de"}, nil),
		web("rest", "", []string{"en"}, matcher{Host: "www.acme.com"}, nil),
		web("other-en", "", []string{"en"}, matcher{Host: "other.test", Prefix: "/en"}, nil),
	}, nil)
	for _, c := range []struct {
		url, name, path string
	}{
		{"https://www.acme.com/legacy", "global-en", "/legacy"},
		{"https://www.acme.com/legacy/", "global-en", "/legacy/"},
		{"https://www.acme.com/ajax/a/b", "global-en", "/ajax/a/b"},
		{"https://www.acme.com/ajax", "global-en", "/ajax"},
		{"https://www.acme.com/legacy/x", "rest", "/legacy/x"}, // an exact entry covers only itself
		{"https://www.acme.com/ajaxy", "rest", "/ajaxy"},
		{"https://www.acme.com/en/about", "global-en", "/about"},
		{"https://www.acme.com/de/about", "global-de", "/about"},
		{"https://other.test/legacy", "", ""}, // another host
	} {
		host, p, _, err := splitURL(c.url)
		if err != nil {
			t.Fatal(err)
		}
		w, path, ok := x.match(host, p, nil)
		if c.name == "" {
			if ok {
				t.Errorf("%s: matched %s", c.url, w.ch.Name)
			}
			continue
		}
		if !ok || w.ch.Name != c.name || path != c.path {
			t.Errorf("%s: got %v %q, want %s %q", c.url, ok, path, c.name, c.path)
		}
	}
	en := x.of(x.byName[1].ch)
	if !en.isReserved("/legacy") || !en.isReserved("/ajax/x") || !en.isReserved("/cart") || en.isReserved("/about") {
		t.Error("root paths are reserved inside the prefix too")
	}
	// A header condition still applies.
	h := newWebIndex([]*channel.Channel{
		web("app", "", []string{"en"}, matcher{Prefix: "/m", Header: &headerMatch{Name: "X-App", Value: "1"}}, roots),
	}, nil)
	if _, _, ok := h.match("", "/legacy", nil); ok {
		t.Error("matched without the header")
	}
	if _, _, ok := h.match("", "/legacy", http.Header{"X-App": {"1"}}); !ok {
		t.Error("did not match with the header")
	}
}

func TestRootConflicts(t *testing.T) {
	root := func(paths ...string) map[string]any { return map[string]any{optRootReserved: paths} }
	for _, c := range []struct {
		name  string
		chans []*channel.Channel
		want  string
	}{
		{"ok", []*channel.Channel{
			web("a", "", nil, matcher{Host: "x.test", Prefix: "/en"}, root("/legacy")),
			web("b", "", nil, matcher{Host: "x.test", Prefix: "/de"}, root("/other")),
			web("c", "", nil, matcher{Host: "y.test", Prefix: "/en"}, root("/legacy")),
		}, ""},
		{"same path", []*channel.Channel{
			web("a", "", nil, matcher{Host: "x.test", Prefix: "/en"}, root("/legacy")),
			web("b", "", nil, matcher{Host: "x.test", Prefix: "/de"}, root("/legacy")),
		}, "channels a and b both claim the root path /legacy"},
		{"wildcard covers", []*channel.Channel{
			web("a", "", nil, matcher{Prefix: "/en"}, root("/shop/*")),
			web("b", "", nil, matcher{Prefix: "/de"}, root("/shop/cart")),
		}, "both claim the root path /shop/*"},
		{"no prefix", []*channel.Channel{
			web("a", "", nil, matcher{Host: "x.test"}, root("/legacy")),
		}, "root_reserved needs match.prefix"},
		{"own prefix", []*channel.Channel{
			web("a", "", nil, matcher{Prefix: "/en"}, root("/en/legacy")),
		}, "lies under its own prefix /en"},
		{"other prefix", []*channel.Channel{
			web("a", "", nil, matcher{Prefix: "/en"}, root("/de/x")),
			web("b", "", nil, matcher{Prefix: "/de"}, nil),
		}, "overlaps the prefix /de of channel b"},
		{"wildcard over prefix", []*channel.Channel{
			web("a", "", nil, matcher{Prefix: "/en"}, root("/shop/*")),
			web("b", "", nil, matcher{Prefix: "/shop/eu"}, nil),
		}, "overlaps the prefix /shop/eu of channel b"},
		{"header apart", []*channel.Channel{
			web("a", "", nil, matcher{Prefix: "/en", Header: &headerMatch{Name: "X", Value: "1"}}, root("/legacy")),
			web("b", "", nil, matcher{Prefix: "/de", Header: &headerMatch{Name: "X", Value: "2"}}, root("/legacy")),
		}, ""},
	} {
		err := rootConflicts(c.chans)
		switch {
		case c.want == "" && err != nil, c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

func TestOwnedRoot(t *testing.T) {
	for base, want := range map[string]bool{"/api": true, "/api/cart": true, "/healthz": true, "/apix": false, "/legacy": false, "/en/api": false} {
		if got := ownedRoot(base); got != want {
			t.Errorf("ownedRoot(%s) = %v", base, got)
		}
	}
}
