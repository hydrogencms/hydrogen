package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

const maxSlug = 120

// Slugify makes a slug: lower-case, common Latin accents folded to ASCII, letters and digits of other
// scripts kept, other runs replaced by single dashes, at most 120 characters.
func Slugify(s string) string {
	var out []rune
	dash := true
	for _, r := range s {
		r = unicode.ToLower(r)
		switch {
		case r < unicode.MaxASCII && slugRune(r):
			out = append(out, r)
			dash = false
		case unicode.Is(unicode.Mn, r):
		default:
			if t, ok := foldRune(r); ok {
				out = append(out, []rune(t)...)
				dash = false
			} else if slugRune(r) {
				out = append(out, r)
				dash = false
			} else if !dash {
				out = append(out, '-')
				dash = true
			}
		}
		if len(out) > maxSlug {
			break
		}
	}
	if len(out) > maxSlug {
		out = out[:maxSlug]
	}
	return strings.Trim(string(out), "-")
}

func foldRune(r rune) (string, bool) {
	switch r {
	case 'à', 'á', 'â', 'ã', 'ä', 'å', 'ā', 'ą', 'ă':
		return "a", true
	case 'æ':
		return "ae", true
	case 'ç', 'ć', 'č', 'ĉ':
		return "c", true
	case 'ď', 'đ', 'ð':
		return "d", true
	case 'è', 'é', 'ê', 'ë', 'ē', 'ę', 'ě':
		return "e", true
	case 'ğ', 'ĝ':
		return "g", true
	case 'ì', 'í', 'î', 'ï', 'ī', 'ı', 'į':
		return "i", true
	case 'ł', 'ľ':
		return "l", true
	case 'ñ', 'ń', 'ň':
		return "n", true
	case 'ò', 'ó', 'ô', 'õ', 'ö', 'ø', 'ō', 'ő':
		return "o", true
	case 'œ':
		return "oe", true
	case 'ř':
		return "r", true
	case 'ś', 'š', 'ş':
		return "s", true
	case 'ß':
		return "ss", true
	case 'ť', 'ţ':
		return "t", true
	case 'þ':
		return "th", true
	case 'ù', 'ú', 'û', 'ü', 'ū', 'ů', 'ű', 'ų':
		return "u", true
	case 'ý', 'ÿ':
		return "y", true
	case 'ź', 'ž', 'ż':
		return "z", true
	}
	return "", false
}

// DecodeDoc parses a JSON object with numbers kept exact (json.Number) and rejects trailing data.
func DecodeDoc(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("schema: decode document: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("schema: decode document: unexpected data after the JSON value")
	}
	doc, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("schema: decode document: the document must be a JSON object")
	}
	return doc, nil
}
