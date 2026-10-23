package site

import (
	"fmt"
	"strings"
	"testing"
)

func TestPageCacheVariants(t *testing.T) {
	c := newPageCache(1 << 20)
	put := func(body string, read []string, rawQuery string, tags ...string) {
		c.store(&cached{body: []byte(body), tags: tags}, "p", read, rawQuery, c.generation())
	}
	body := func(rawQuery string) string {
		if e := c.lookup("p", rawQuery); e != nil {
			return string(e.body)
		}
		return ""
	}

	put("plain", nil, "a=1", "t")
	if body("a=2&utm=x") != "plain" {
		t.Fatal("a page that reads nothing is one entry")
	}

	put("cat-x", []string{"cat"}, "cat=x&utm=1", "t")
	if body("a=2") != "" || body("cat=x&utm=9") != "cat-x" || body("cat=y") != "" {
		t.Fatal("variants are keyed by the parameters read")
	}
	put("cat-y", []string{"cat"}, "cat=y", "t")
	if body("cat=y") != "cat-y" || body("cat=x") != "cat-x" {
		t.Fatal("two variants")
	}

	// a page that reads more than known re-keys the path
	put("both", []string{"cat", "page"}, "cat=x&page=2", "t")
	if body("cat=x&page=2") != "both" || body("cat=x") != "" || body("cat=y") != "" {
		t.Fatal("growing the read set drops the coarser variants")
	}
	// a render that reads fewer parameters is stored under the known ones
	put("only-cat", []string{"cat"}, "cat=z", "t")
	if body("cat=z") != "only-cat" || body("cat=z&page=3") != "" {
		t.Fatal("subset reads")
	}

	c.purge("t")
	if body("cat=x&page=2") != "" || body("cat=z") != "" || len(c.vary) != 0 || c.used != 0 {
		t.Fatalf("purge left variants: vary=%d used=%d", len(c.vary), c.used)
	}

	// bounds: the oldest variant makes room, long and many values are never stored
	for i := range maxVariants + 8 {
		put(fmt.Sprint(i), []string{"q"}, fmt.Sprintf("q=%d", i))
	}
	if n := len(c.vary["p"].keys); n != maxVariants || len(c.m) != maxVariants {
		t.Fatalf("variants = %d, entries = %d", n, len(c.m))
	}
	if body("q=0") != "" || body("q=39") != "39" {
		t.Fatal("the oldest variant should have made room")
	}
	put("long", []string{"q"}, "q="+strings.Repeat("x", maxQueryValueLn+1))
	put("many", []string{"q"}, strings.Repeat("q=1&", maxQueryValues+1))
	if len(c.m) != maxVariants {
		t.Fatal("unbounded values were stored")
	}
	if body("q="+strings.Repeat("x", maxQueryValueLn+1)) != "" {
		t.Fatal("a long value is never served from the cache")
	}
}

func TestPageCacheVariantAfterPurge(t *testing.T) {
	c := newPageCache(1 << 20)
	gen := c.generation()
	c.purge("x")
	c.store(&cached{body: []byte("old")}, "p", []string{"q"}, "q=1", gen)
	if c.lookup("p", "q=1") != nil || len(c.vary) != 0 {
		t.Fatal("a variant rendered before a purge was stored")
	}
}

func BenchmarkPageCacheVariantLookup(b *testing.B) {
	c := newPageCache(1 << 20)
	c.store(&cached{body: []byte("x")}, "p", []string{"category", "page"}, "category=news&page=2", c.generation())
	b.ReportAllocs()
	for b.Loop() {
		if c.lookup("p", "category=news&page=2&utm_source=a&utm_medium=b") == nil {
			b.Fatal("miss")
		}
	}
}

func BenchmarkPageCachePlainLookup(b *testing.B) {
	c := newPageCache(1 << 20)
	c.store(&cached{body: []byte("x")}, "p", nil, "", c.generation())
	b.ReportAllocs()
	for b.Loop() {
		if c.lookup("p", "utm_source=a") == nil {
			b.Fatal("miss")
		}
	}
}
