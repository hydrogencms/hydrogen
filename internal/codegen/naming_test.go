package codegen

import (
	"slices"
	"strings"
	"testing"
)

func TestGoName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"article", "Article"},
		{"blogPost", "BlogPost"},
		{"publishDate", "PublishDate"},
		{"id", "ID"},
		{"userId", "UserID"},
		{"sourceUrl", "SourceURL"},
		{"html_body", "HTMLBody"},
		{"HTMLBody", "HTMLBody"},
		{"seo-title", "SeoTitle"},
		{"seo", "Seo"},
		{"apiKey2", "APIKey2"},
		{"2fa", "X2fa"},
		{"long-read", "LongRead"},
		{"type", "Type"},
		{"func", "Func"},
		{"naïve", "NaVe"},
	}
	for _, tc := range tests {
		got, err := goName(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("goName(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "-", "__", "é"} {
		if _, err := goName(bad); err == nil {
			t.Errorf("goName(%q) succeeded", bad)
		}
	}
}

func TestFileBase(t *testing.T) {
	for in, want := range map[string]string{"article": "article", "blogPost": "blog_post", "HTMLBody": "html_body"} {
		if got := fileBase(in); got != want {
			t.Errorf("fileBase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWrap(t *testing.T) {
	lines := wrap(strings.Repeat("word ", 40)+"\n\nsecond", 30)
	for _, l := range lines {
		if len(l) > 30 {
			t.Errorf("line too long: %q", l)
		}
	}
	if !slices.Contains(lines, "") || lines[len(lines)-1] != "second" {
		t.Errorf("paragraphs lost: %q", lines)
	}
}

func TestTSKey(t *testing.T) {
	for in, want := range map[string]string{"title": "title", "a-b": `"a-b"`, "2x": `"2x"`, "a_b": "a_b"} {
		if got := tsKey(in); got != want {
			t.Errorf("tsKey(%q) = %q, want %q", in, got, want)
		}
	}
}
