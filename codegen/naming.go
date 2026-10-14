package codegen

import (
	"fmt"
	"strings"
	"unicode"
)

// isInitialism reports whether the upper-cased word is one Go style keeps upper-case in identifiers.
func isInitialism(upper string) bool {
	switch upper {
	case "ID", "URL", "HTML", "JSON", "API", "HTTP":
		return true
	}
	return false
}

// words splits a schema name into words at separators, lower-to-upper transitions and acronym ends:
// "blogPost" -> [blog Post], "seo_title" -> [seo title], "HTMLBody" -> [HTML Body]. Only ASCII letters
// and digits make words; everything else separates.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, string(cur))
			cur = cur[:0]
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			flush()
			continue
		}
		if unicode.IsUpper(r) && len(cur) > 0 {
			prev := cur[len(cur)-1]
			nextLower := i+1 < len(rs) && unicode.IsLower(rs[i+1])
			if !unicode.IsUpper(prev) || nextLower {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return out
}

// goName converts a schema name into an exported Go identifier: "blogPost" -> "BlogPost",
// "userId" -> "UserID". Names starting with a digit get an "X" prefix.
func goName(s string) (string, error) {
	ws := words(s)
	if len(ws) == 0 {
		return "", fmt.Errorf("codegen: %q has no letters or digits to build an identifier from", s)
	}
	var b strings.Builder
	for _, w := range ws {
		if up := strings.ToUpper(w); isInitialism(up) {
			b.WriteString(up)
			continue
		}
		b.WriteString(strings.ToUpper(w[:1]))
		b.WriteString(strings.ToLower(w[1:]))
	}
	name := b.String()
	if name[0] >= '0' && name[0] <= '9' {
		name = "X" + name
	}
	return name, nil
}

// fileBase converts a schema name into a snake-case file name stem: "blogPost" -> "blog_post".
func fileBase(s string) string {
	ws := words(s)
	for i, w := range ws {
		ws[i] = strings.ToLower(w)
	}
	return strings.Join(ws, "_")
}

// validSchemaName reports whether s is safe to embed in a struct tag, string literal and TS key.
func validSchemaName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-') {
			return false
		}
	}
	return true
}

// wrap breaks text into lines of at most width runes (words longer than width stay whole). Blank lines in
// text are kept as empty lines.
func wrap(text string, width int) []string {
	var lines []string
	for para := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
		fields := strings.Fields(para)
		if len(fields) == 0 {
			lines = append(lines, "")
			continue
		}
		cur := fields[0]
		for _, w := range fields[1:] {
			if len([]rune(cur))+1+len([]rune(w)) > width {
				lines = append(lines, cur)
				cur = w
				continue
			}
			cur += " " + w
		}
		lines = append(lines, cur)
	}
	return lines
}

// lineComment renders text as Go line comments with the given indent.
func lineComment(indent, text string) string {
	var b strings.Builder
	for _, l := range wrap(text, 100-len(indent)*4-3) {
		b.WriteString(indent)
		if l == "" {
			b.WriteString("//\n")
			continue
		}
		b.WriteString("// " + l + "\n")
	}
	return b.String()
}

// jsDoc renders text as a JSDoc block with the given indent.
func jsDoc(indent, text string) string {
	text = strings.ReplaceAll(text, "*/", "*\\/")
	lines := wrap(text, 100-len(indent)*2-3)
	if len(lines) == 1 {
		return indent + "/** " + lines[0] + " */\n"
	}
	var b strings.Builder
	b.WriteString(indent + "/**\n")
	for _, l := range lines {
		if l == "" {
			b.WriteString(indent + " *\n")
			continue
		}
		b.WriteString(indent + " * " + l + "\n")
	}
	b.WriteString(indent + " */\n")
	return b.String()
}

// tsKey renders an object key, quoting it unless it is a plain identifier.
func tsKey(s string) string {
	plain := s != "" && (s[0] < '0' || s[0] > '9')
	for _, r := range s {
		if r != '_' && r != '$' && (r >= unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r))) {
			plain = false
		}
	}
	if plain {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
