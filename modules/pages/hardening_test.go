package pages

import (
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
)

func TestDedupeChanges(t *testing.T) {
	a, b := ids.UUID{1}, ids.UUID{2}
	in := []change{{"main", &a}, {"main", nil}, {"main", &a}, {"shop", &a}, {"main", &b}, {"main", nil}}
	got := dedupeChanges(in)
	if len(got) != 4 {
		t.Fatalf("dedupeChanges = %+v, want 4 distinct", got)
	}
	for i, want := range []change{{"main", &a}, {"main", nil}, {"shop", &a}, {"main", &b}} {
		if got[i].Site != want.Site || !sameOwner(got[i].Node, want.Node) {
			t.Errorf("change %d = %+v, want %+v", i, got[i], want)
		}
	}
}

func TestMountCache(t *testing.T) {
	var c mountCache
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	set := &mountSet{}
	if _, _, ok := c.get("article", now); ok {
		t.Fatal("empty cache hit")
	}
	_, gen, _ := c.get("article", now)
	c.put("article", set, gen, now.Add(mountCacheTTL))
	if got, _, ok := c.get("article", now.Add(mountCacheTTL-time.Millisecond)); !ok || got != set {
		t.Errorf("fresh entry: %v %v", got, ok)
	}
	if _, _, ok := c.get("article", now.Add(mountCacheTTL)); ok {
		t.Error("expired entry served")
	}
	c.put("article", set, gen, now.Add(mountCacheTTL))
	c.invalidate()
	if _, _, ok := c.get("article", now); ok {
		t.Error("entry survived invalidation")
	}
	// A read that started before an invalidation must not be stored after it.
	c.put("article", set, gen, now.Add(mountCacheTTL))
	if _, _, ok := c.get("article", now); ok {
		t.Error("stale read stored")
	}
}

func TestRedirectOfValidates(t *testing.T) {
	s := &service{sites: []string{"main"}}
	probs := func(doc map[string]any) []string {
		_, ps := s.redirectOf(doc)
		var out []string
		for _, p := range ps {
			out = append(out, string(p.field)+":"+p.code)
		}
		return out
	}
	if got := probs(map[string]any{"from": "/a.html", "to": "/b"}); len(got) != 0 {
		t.Errorf("legacy from: %v", got)
	}
	if got := probs(map[string]any{"from": "/a", "to": "/b", "statusCode": "999"}); len(got) != 1 || got[0] != "statusCode:invalid_status" {
		t.Errorf("status 999: %v", got)
	}
	if got := probs(map[string]any{"from": "/a", "to": "/b", "statusCode": "x"}); len(got) != 1 {
		t.Errorf("status x: %v", got)
	}
	for _, st := range []string{"301", "302", "307", "308"} {
		if got := probs(map[string]any{"from": "/a", "to": "/b", "statusCode": st}); len(got) != 0 {
			t.Errorf("status %s: %v", st, got)
		}
	}
	if got := probs(map[string]any{"from": `/a\b`, "to": `/\evil`}); len(got) != 2 {
		t.Errorf("backslashes: %v", got)
	}
}

func TestSitePathOf(t *testing.T) {
	for in, want := range map[string]string{"/a?x=1": "/a", "/a#f": "/a", "/a/b": "/a/b"} {
		if got, ok := sitePathOf(in); !ok || got != want {
			t.Errorf("sitePathOf(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"//host/x", "https://x/y", "item:abc", ""} {
		if _, ok := sitePathOf(in); ok {
			t.Errorf("sitePathOf(%q) is a site path", in)
		}
	}
}
