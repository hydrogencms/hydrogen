package postgres

import (
	"strconv"
	"strings"
	"sync"
)

// maxRewrites bounds the rewrite cache. Statements built from varying text (IN lists …) can exceed it; the
// cache is then emptied and refills with the statements in use.
const maxRewrites = 512

// rewriteCache memoizes rewritePlaceholders per query string. A hit takes a read lock and allocates nothing.
type rewriteCache struct {
	mu sync.RWMutex
	m  map[string]string
}

func newRewriteCache() *rewriteCache { return &rewriteCache{m: make(map[string]string, 64)} }

func (c *rewriteCache) rewrite(q string) string {
	if strings.IndexByte(q, '?') < 0 {
		return q
	}
	c.mu.RLock()
	r, ok := c.m[q]
	c.mu.RUnlock()
	if ok {
		return r
	}
	r = rewritePlaceholders(q)
	c.mu.Lock()
	if len(c.m) >= maxRewrites {
		clear(c.m)
	}
	c.m[q] = r
	c.mu.Unlock()
	return r
}

// rewritePlaceholders replaces each "?" with $1, $2 … It leaves single-quoted strings (” escapes, and
// backslash escapes in E'…'), double-quoted identifiers, "--" comments, nested "/* */" comments and
// dollar-quoted strings untouched.
func rewritePlaceholders(q string) string {
	var b strings.Builder
	var num [20]byte
	n, last := 0, 0
	for i := 0; i < len(q); {
		switch c := q[i]; c {
		case '?':
			if n == 0 {
				b.Grow(len(q) + 8)
			}
			n++
			b.WriteString(q[last:i])
			b.WriteByte('$')
			b.Write(strconv.AppendInt(num[:0], int64(n), 10))
			i++
			last = i
		case '\'':
			i = skipQuoted(q, i, c, i > 0 && (q[i-1] == 'e' || q[i-1] == 'E') && (i == 1 || !isIdent(q[i-2])))
		case '"':
			i = skipQuoted(q, i, c, false)
		case '-':
			if i+1 < len(q) && q[i+1] == '-' {
				i = skipTo(q, i, "\n")
			} else {
				i++
			}
		case '/':
			if i+1 < len(q) && q[i+1] == '*' {
				i = skipBlockComment(q, i)
			} else {
				i++
			}
		case '$':
			if tag, ok := dollarTag(q, i); ok && (i == 0 || !isIdent(q[i-1])) {
				i = skipTo(q, i+len(tag), tag)
			} else {
				i++
			}
		default:
			i++
		}
	}
	if n == 0 {
		return q
	}
	b.WriteString(q[last:])
	return b.String()
}

// skipQuoted returns the index after the quoted run that opens at q[i]; a doubled quote is an escape, and so is
// a backslash when backslash is set. An unterminated run extends to the end.
func skipQuoted(q string, i int, quote byte, backslash bool) int {
	for i++; i < len(q); i++ {
		switch {
		case backslash && q[i] == '\\':
			i++
		case q[i] == quote:
			if i+1 < len(q) && q[i+1] == quote {
				i++
				continue
			}
			return i + 1
		}
	}
	return len(q)
}

// skipTo returns the index after the next occurrence of end at or after i (the end of q if none).
func skipTo(q string, i int, end string) int {
	j := strings.Index(q[i:], end)
	if j < 0 {
		return len(q)
	}
	return i + j + len(end)
}

func skipBlockComment(q string, i int) int {
	depth := 0
	for i < len(q)-1 {
		switch {
		case q[i] == '/' && q[i+1] == '*':
			depth++
			i += 2
		case q[i] == '*' && q[i+1] == '/':
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(q)
}

// dollarTag returns the opening tag ($$ or $name$) at q[i].
func dollarTag(q string, i int) (string, bool) {
	for j := i + 1; j < len(q); j++ {
		switch c := q[j]; {
		case c == '$':
			return q[i : j+1], true
		case isIdent(c) && (j > i+1 || c < '0' || c > '9'):
		default:
			return "", false
		}
	}
	return "", false
}

func isIdent(c byte) bool {
	return c == '_' || c >= 0x80 || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
