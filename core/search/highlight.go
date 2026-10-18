package search

import (
	"html"
	"strings"
	"unicode"
)

// Markers wrap a match in a raw snippet. Engines return them in place of tags so that the text can be
// HTML-escaped before the tags are put in.
const (
	MarkOpen  = "\x02"
	MarkClose = "\x03"
)

// Marked turns a raw snippet with MarkOpen/MarkClose markers into HTML: the text is escaped, the markers
// become <mark> and </mark>.
func Marked(raw string) string {
	s := html.EscapeString(raw)
	s = strings.ReplaceAll(s, MarkOpen, "<mark>")
	return strings.ReplaceAll(s, MarkClose, "</mark>")
}

// MarkTerms marks the words of text that start with one of terms (case-insensitive), as raw text with
// markers; Marked makes it HTML. It returns text unchanged when nothing matches.
func MarkTerms(text string, terms []string) string {
	text = stripMarks(text)
	if len(terms) == 0 {
		return text
	}
	runes := []rune(text)
	var sb strings.Builder
	for i := 0; i < len(runes); {
		n := 0
		if i == 0 || !wordRune(runes[i-1]) {
			for _, t := range terms {
				if m := prefixLen(runes[i:], []rune(t)); m > n {
					n = m
				}
			}
		}
		if n == 0 {
			sb.WriteRune(runes[i])
			i++
			continue
		}
		sb.WriteString(MarkOpen)
		sb.WriteString(string(runes[i : i+n]))
		sb.WriteString(MarkClose)
		i += n
	}
	return sb.String()
}

func stripMarks(s string) string {
	if !strings.ContainsAny(s, MarkOpen+MarkClose) {
		return s
	}
	return strings.NewReplacer(MarkOpen, "", MarkClose, "").Replace(s)
}

func wordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// prefixLen is len(term) when s starts with term ignoring case, else 0.
func prefixLen(s, term []rune) int {
	if len(term) == 0 || len(s) < len(term) {
		return 0
	}
	for i, r := range term {
		if unicode.ToLower(s[i]) != unicode.ToLower(r) {
			return 0
		}
	}
	return len(term)
}
