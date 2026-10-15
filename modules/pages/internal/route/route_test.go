package route

import (
	"slices"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		pattern string
		fields  []string
		err     string
	}{
		{"{slug}", []string{"slug"}, ""},
		{"news/{slug}", []string{"slug"}, ""},
		{"{yyyy}/{mm}/{slug}", []string{"mm", "slug", "yyyy"}, ""},
		{"{published:yyyy}/{published:mm}/{slug}", []string{"published", "slug"}, ""},
		{"{slug}-{id}", []string{"slug"}, ""},
		{"{id}", nil, ""},
		{"blog/{published:yyyy}-{published:mm}-{published:dd}/{title}", []string{"published", "title"}, ""},
		{"größe/{slug}", []string{"slug"}, ""},
		{"", nil, "empty pattern"},
		{"news", nil, "no placeholder"},
		{"/news/{slug}", nil, "empty segment"},
		{"news/{slug}/", nil, "empty segment"},
		{"news//{slug}", nil, "empty segment"},
		{"{a}{b}", nil, "between them"},
		{"{a:yyyy}{a:mm}", nil, "between them"},
		{"News/{slug}", nil, "invalid literal"},
		{"new_s/{slug}", nil, "invalid literal"},
		{"{slug", nil, "unclosed"},
		{"slug}", nil, "unexpected"},
		{"{Slug}", nil, "invalid placeholder"},
		{"{}", nil, "invalid placeholder"},
		{"{my-field}", nil, "invalid placeholder"},
		{"{d:yy}", nil, "unknown format"},
		{"{id:yyyy}", nil, "no format"},
		{"-{slug}", nil, "valid slug"},
		{"{slug}-", nil, "valid slug"},
		{"a--{slug}", nil, "valid slug"},
		{"{slug}/..", nil, "invalid literal"},
	}
	for _, tt := range tests {
		p, err := Parse(tt.pattern)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("Parse(%q) error = %v, want containing %q", tt.pattern, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v", tt.pattern, err)
			continue
		}
		if got := p.Fields(); len(got)+len(tt.fields) > 0 && !slices.Equal(got, tt.fields) {
			t.Errorf("Parse(%q).Fields() = %v, want %v", tt.pattern, got, tt.fields)
		}
		if p.String() != tt.pattern {
			t.Errorf("String() = %q", p.String())
		}
	}
}

func TestFill(t *testing.T) {
	vals := map[string]string{
		"slug":  "hello-world",
		"title": "Hello, Wörld & Co!",
		"id":    "0190f1a2-3b4c",
		"d":     "2026-10-04",
		"dt":    "2026-12-31T23:30:00-05:00",
		"bad":   "not a date",
		"blank": "!!!",
		"empty": "",
		"cjk":   "日本語 ページ",
	}
	get := func(f string) (string, bool) { v, ok := vals[f]; return v, ok }
	tests := []struct {
		pattern string
		want    string
		err     string
	}{
		{"news/{slug}", "news/hello-world", ""},
		{"{title}", "hello-world-co", ""},
		{"x/{title}-{id}", "x/hello-world-co-0190f1a2-3b4c", ""},
		{"{d:yyyy}/{d:mm}/{d:dd}/{slug}", "2026/10/04/hello-world", ""},
		{"{dt:yyyy}-{dt:mm}-{dt:dd}", "2027-01-01", ""},
		{"{cjk}", "日本語-ページ", ""},
		{"a/{missing}", "", `"missing"`},
		{"{bad:yyyy}", "", `"bad"`},
		{"{blank}", "", `"blank"`},
		{"{empty}", "", `"empty"`},
		{"{d:yyyy}/{unknown:mm}", "", `"unknown"`},
	}
	for _, tt := range tests {
		p, err := Parse(tt.pattern)
		if err != nil {
			t.Fatalf("Parse(%q): %v", tt.pattern, err)
		}
		got, err := p.Fill(get)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("Fill(%q) error = %v, want containing %s", tt.pattern, err, tt.err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Fill(%q): %v", tt.pattern, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Fill(%q) = %q, want %q", tt.pattern, got, tt.want)
		}
		if _, err := Clean("/" + got); err != nil {
			t.Errorf("Fill(%q) = %q is not a clean path: %v", tt.pattern, got, err)
		}
	}
}

func TestClean(t *testing.T) {
	long := "/" + strings.Repeat("a", MaxPath)
	tests := []struct {
		path string
		ok   bool
	}{
		{"/", true}, {"/a", true}, {"/a/b-c/d1", true}, {"/größe/日本", true},
		{"", false}, {"a", false}, {"a/b", false}, {"/a/", false}, {"//", false}, {"/a//b", false},
		{"/.", false}, {"/a/..", false}, {"/A", false}, {"/a_b", false}, {"/-a", false}, {"/a-", false},
		{"/a--b", false}, {"/a b", false}, {long, false}, {long[:MaxPath], true},
	}
	for _, tt := range tests {
		got, err := Clean(tt.path)
		if (err == nil) != tt.ok {
			t.Errorf("Clean(%q) error = %v, want ok=%v", tt.path, err, tt.ok)
		}
		if err == nil && got != tt.path {
			t.Errorf("Clean(%q) = %q", tt.path, got)
		}
	}
}

func TestJoin(t *testing.T) {
	tests := []struct{ parent, rel, want string }{
		{"/", "a/b", "/a/b"},
		{"/a", "b", "/a/b"},
		{"/a/b", "c/d", "/a/b/c/d"},
		{"/a", "", "/a"},
		{"/", "", "/"},
		{"/a", "/b/", "/a/b"},
	}
	for _, tt := range tests {
		if got := Join(tt.parent, tt.rel); got != tt.want {
			t.Errorf("Join(%q,%q) = %q, want %q", tt.parent, tt.rel, got, tt.want)
		}
	}
}

func FuzzRoute(f *testing.F) {
	for _, s := range []string{"{slug}", "a/{b}-{c:yyyy}", "{", "}{", "a//b", "/", "/a/b", "/a/../b", "ß/{x}"} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, pattern, path string) {
		if p, err := Parse(pattern); err == nil {
			_ = p.Fields()
			out, err := p.Fill(func(f string) (string, bool) { return path, true })
			if err == nil && len(p.Fields()) <= 1 {
				if _, err := Clean("/" + out); err != nil && !strings.Contains(pattern, ":") && len(out) <= MaxPath-1 {
					t.Fatalf("Fill(%q) = %q not clean: %v", pattern, out, err)
				}
			}
		}
		clean, err := Clean(path)
		if err != nil {
			return
		}
		rel := strings.TrimPrefix(clean, "/")
		j := Join("/", rel)
		if j != clean {
			t.Fatalf("Join(/, %q) = %q, want %q", rel, j, clean)
		}
		if i := strings.LastIndexByte(clean, '/'); i > 0 {
			if got := Join(clean[:i], clean[i+1:]); got != clean {
				t.Fatalf("Join(%q,%q) = %q", clean[:i], clean[i+1:], got)
			}
		}
		if c2, err := Clean(Join("/", rel)); err != nil || c2 != clean {
			t.Fatalf("round trip of %q failed: %q %v", clean, c2, err)
		}
	})
}

func BenchmarkFill(b *testing.B) {
	p, err := Parse("news/{published:yyyy}/{published:mm}/{title}-{id}")
	if err != nil {
		b.Fatal(err)
	}
	vals := map[string]string{"published": "2026-10-14", "title": "hello-world", "id": "abc123"}
	get := func(f string) (string, bool) { v, ok := vals[f]; return v, ok }
	b.ReportAllocs()
	for range b.N {
		if _, err := p.Fill(get); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFillSlugify(b *testing.B) {
	p, _ := Parse("news/{title}")
	get := func(string) (string, bool) { return "Hello, Wörld & Co!", true }
	b.ReportAllocs()
	for range b.N {
		_, _ = p.Fill(get)
	}
}

func BenchmarkClean(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, _ = Clean("/news/2026/10/hello-world")
	}
}
