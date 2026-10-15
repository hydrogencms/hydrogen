package postgres

import (
	"fmt"
	"testing"
)

func TestRewritePlaceholders(t *testing.T) {
	tests := []struct{ in, want string }{
		{"SELECT 1", "SELECT 1"},
		{"a = ? AND b = ?", "a = $1 AND b = $2"},
		{"'?' = ?", "'?' = $1"},
		{"'it''s ?' = ? ", "'it''s ?' = $1 "},
		{`"a?b" = ?`, `"a?b" = $1`},
		{`"a""?" = ?`, `"a""?" = $1`},
		{"? -- why?\n AND ?", "$1 -- why?\n AND $2"},
		{"? /* ? */ ?", "$1 /* ? */ $2"},
		{"? /* a /* ? */ ? */ ?", "$1 /* a /* ? */ ? */ $2"},
		{"a - ? / ?", "a - $1 / $2"},
		{"$$ ? $$ ?", "$$ ? $$ $1"},
		{"$f$ ? $$ ? $f$ ?", "$f$ ? $$ ? $f$ $1"},
		{"$1 ?", "$1 $1"},
		{"E'\\'?' = ?", "E'\\'?' = $1"},
		{"'\\' ?", "'\\' $1"},
		{"x'?'", "x'?'"},
		{"'unterminated ?", "'unterminated ?"},
		{"/* unterminated ?", "/* unterminated ?"},
		{"? ? ? ? ? ? ? ? ? ? ?", "$1 $2 $3 $4 $5 $6 $7 $8 $9 $10 $11"},
	}
	for _, tc := range tests {
		if got := rewritePlaceholders(tc.in); got != tc.want {
			t.Errorf("rewritePlaceholders(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRewriteCache(t *testing.T) {
	c := newRewriteCache()
	q := "SELECT a FROM t WHERE b = ? AND c = ?"
	for range 2 {
		if got := c.rewrite(q); got != "SELECT a FROM t WHERE b = $1 AND c = $2" {
			t.Fatalf("got %q", got)
		}
	}
	for i := range 3 * maxRewrites {
		c.rewrite(fmt.Sprintf("SELECT ? -- %d", i))
		if len(c.m) > maxRewrites {
			t.Fatalf("cache holds %d entries", len(c.m))
		}
	}
	if n := testing.AllocsPerRun(100, func() { c.rewrite(q); c.rewrite(q) }); n > 1 {
		t.Errorf("cache hit allocates %v times", n)
	}
}

const benchQuery = `SELECT id, name, parent_id, sort_key FROM pages_page WHERE site = ? AND parent_id = ? AND name <> '?' AND status IN (?, ?) ORDER BY sort_key LIMIT ?`

func BenchmarkRewritePlaceholders(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		rewritePlaceholders(benchQuery)
	}
}

func BenchmarkRewriteCacheHit(b *testing.B) {
	c := newRewriteCache()
	c.rewrite(benchQuery)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		c.rewrite(benchQuery)
	}
}
