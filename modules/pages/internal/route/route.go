// Package route parses route patterns such as "{yyyy}/{mm}/{slug}" and builds and validates site paths.
package route

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// MaxPath is the longest site path in bytes.
const MaxPath = 1024

type partKind uint8

const (
	literal partKind = iota
	placeholder
	year
	month
	day
)

type part struct {
	kind  partKind
	text  string // literal text or field name
	date  bool   // placeholder has a date format
	width int
}

// Pattern is a parsed route pattern: slash-separated segments made of literals and placeholders.
type Pattern struct {
	segs   [][]part
	fields []string
	src    string
}

// Parse parses a route pattern. Segments are separated by "/" (no leading or trailing slash), each made of
// slug literals and placeholders {field} or {field:fmt}, where fmt is yyyy, mm or dd. The field "id" is
// the item id. At least one placeholder is required and placeholders must be separated by a literal.
func Parse(pattern string) (*Pattern, error) {
	if pattern == "" {
		return nil, errors.New("route: empty pattern")
	}
	p := &Pattern{src: pattern}
	placeholders := 0
	for seg := range strings.SplitSeq(pattern, "/") {
		if seg == "" {
			return nil, fmt.Errorf("route: pattern %q has an empty segment", pattern)
		}
		parts, n, err := parseSegment(seg)
		if err != nil {
			return nil, fmt.Errorf("route: pattern %q: %w", pattern, err)
		}
		placeholders += n
		p.segs = append(p.segs, parts)
	}
	if placeholders == 0 {
		return nil, fmt.Errorf("route: pattern %q has no placeholder", pattern)
	}
	for _, s := range p.segs {
		for _, pt := range s {
			if pt.kind != literal && pt.text != "id" {
				p.fields = append(p.fields, pt.text)
			}
		}
	}
	slices.Sort(p.fields)
	p.fields = slices.Compact(p.fields)
	return p, nil
}

func parseSegment(seg string) ([]part, int, error) {
	var parts []part
	var sample strings.Builder
	n := 0
	for rest := seg; rest != ""; {
		if rest[0] == '{' {
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				return nil, 0, fmt.Errorf("segment %q: unclosed placeholder", seg)
			}
			if len(parts) > 0 && parts[len(parts)-1].kind != literal {
				return nil, 0, fmt.Errorf("segment %q: placeholders need a literal between them", seg)
			}
			pt, err := parsePlaceholder(rest[1:end])
			if err != nil {
				return nil, 0, fmt.Errorf("segment %q: %w", seg, err)
			}
			parts = append(parts, pt)
			sample.WriteByte('x')
			n++
			rest = rest[end+1:]
			continue
		}
		end := strings.IndexAny(rest, "{}")
		if end < 0 {
			end = len(rest)
		}
		if end < len(rest) && rest[end] == '}' {
			return nil, 0, fmt.Errorf("segment %q: unexpected '}'", seg)
		}
		lit := rest[:end]
		for _, r := range lit {
			if r != '-' && !slugRune(r) {
				return nil, 0, fmt.Errorf("segment %q: invalid literal character %q", seg, r)
			}
		}
		parts = append(parts, part{kind: literal, text: lit})
		sample.WriteString(lit)
		rest = rest[end:]
	}
	if !validSlug(sample.String()) {
		return nil, 0, fmt.Errorf("segment %q cannot produce a valid slug (stray or doubled dashes)", seg)
	}
	return parts, n, nil
}

func parsePlaceholder(s string) (part, error) {
	name, format, hasFmt := strings.Cut(s, ":")
	if !validField(name) {
		return part{}, fmt.Errorf("invalid placeholder field %q", name)
	}
	pt := part{kind: placeholder, text: name}
	if !hasFmt {
		return pt, nil
	}
	if name == "id" {
		return part{}, errors.New("placeholder id takes no format")
	}
	switch format {
	case "yyyy":
		pt.kind, pt.width = year, 4
	case "mm":
		pt.kind, pt.width = month, 2
	case "dd":
		pt.kind, pt.width = day, 2
	default:
		return part{}, fmt.Errorf("unknown format %q in {%s} (want yyyy, mm or dd)", format, s)
	}
	pt.date = true
	return pt, nil
}

func validField(s string) bool {
	if s == "id" {
		return true
	}
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// Fields returns the sorted, unique field names the pattern references, without "id".
func (p *Pattern) Fields() []string { return slices.Clone(p.fields) }

// String returns the pattern source.
func (p *Pattern) String() string { return p.src }

// Fill builds the relative path (no leading slash) for the values value returns. Text values that are not
// already slugs are slugified; date placeholders accept 2006-01-02 or RFC 3339 values (the UTC date is
// used). A missing, empty or unparsable value is an error naming the field.
func (p *Pattern) Fill(value func(field string) (string, bool)) (string, error) {
	var sb strings.Builder
	sb.Grow(len(p.src) + 16)
	for i, seg := range p.segs {
		if i > 0 {
			sb.WriteByte('/')
		}
		for _, pt := range seg {
			if pt.kind == literal {
				sb.WriteString(pt.text)
				continue
			}
			v, ok := value(pt.text)
			if !ok {
				return "", fmt.Errorf("route: no value for field %q", pt.text)
			}
			if pt.date {
				if err := writeDate(&sb, pt, v); err != nil {
					return "", fmt.Errorf("route: field %q: %w", pt.text, err)
				}
				continue
			}
			if !validSlug(v) {
				v = schema.Slugify(v)
			}
			if v == "" {
				return "", fmt.Errorf("route: field %q has no usable value for a path", pt.text)
			}
			sb.WriteString(v)
		}
	}
	return sb.String(), nil
}

func writeDate(sb *strings.Builder, pt part, v string) error {
	t, err := time.Parse(time.DateOnly, v)
	if err != nil {
		if t, err = time.Parse(time.RFC3339, v); err != nil {
			return fmt.Errorf("%q is not a date (want 2006-01-02 or RFC 3339)", v)
		}
		t = t.UTC()
	}
	var n int
	switch pt.kind {
	case year:
		n = t.Year()
	case month:
		n = int(t.Month())
	default:
		n = t.Day()
	}
	var buf [8]byte
	i := len(buf)
	for ; n > 0 || i > len(buf)-pt.width; n /= 10 {
		i--
		buf[i] = byte('0' + n%10)
	}
	sb.Write(buf[i:])
	return nil
}

// Clean validates a site path and returns it. A path starts with "/", the root is "/", there is no trailing
// slash otherwise, no empty, "." or ".." segments, every segment is a slug, and it is at most MaxPath bytes.
func Clean(path string) (string, error) {
	if len(path) > MaxPath {
		return "", fmt.Errorf("route: path longer than %d bytes", MaxPath)
	}
	if path == "/" {
		return path, nil
	}
	if path == "" || path[0] != '/' {
		return "", fmt.Errorf("route: path %q must start with /", path)
	}
	for seg := range strings.SplitSeq(path[1:], "/") {
		switch {
		case seg == "":
			return "", fmt.Errorf("route: path %q has an empty segment or trailing slash", path)
		case seg == "." || seg == "..":
			return "", fmt.Errorf("route: path %q has a %q segment", path, seg)
		case !validSlug(seg):
			return "", fmt.Errorf("route: path %q: segment %q is not a slug", path, seg)
		}
	}
	return path, nil
}

// Join appends the relative path rel to the cleaned path parent. Surrounding slashes of rel are dropped.
func Join(parent, rel string) string {
	rel = strings.Trim(rel, "/")
	switch {
	case rel == "":
		return parent
	case parent == "/" || parent == "":
		return "/" + rel
	}
	return parent + "/" + rel
}

func slugRune(r rune) bool {
	if r < utf8.RuneSelf {
		return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
	}
	return unicode.In(r, unicode.Ll, unicode.Lo, unicode.Lm, unicode.Nd)
}

// validSlug reports whether s is runs of slug characters separated by single dashes.
func validSlug(s string) bool {
	if s == "" {
		return false
	}
	dash := true
	for _, r := range s {
		switch {
		case r == '-':
			if dash {
				return false
			}
			dash = true
		case slugRune(r):
			dash = false
		default:
			return false
		}
	}
	return !dash
}
