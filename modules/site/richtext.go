package site

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	langRE   = regexp.MustCompile(`^[A-Za-z0-9_+-]+$`)
	schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// renderRichText turns a stored ProseMirror document into HTML. Text is escaped, unknown nodes and marks
// render their children only, and links keep http(s), mailto and relative targets; item:<id> links resolve
// through link (an empty answer drops the link and keeps its text).
func renderRichText(doc any, link func(id string) string) string {
	m, ok := doc.(map[string]any)
	if !ok {
		return ""
	}
	var sb strings.Builder
	rt := richWriter{sb: &sb, link: link}
	rt.children(m)
	return sb.String()
}

type richWriter struct {
	sb   *strings.Builder
	link func(string) string
}

func (w richWriter) children(n map[string]any) {
	list, _ := n["content"].([]any)
	for _, c := range list {
		if cm, ok := c.(map[string]any); ok {
			w.node(cm)
		}
	}
}

func (w richWriter) node(n map[string]any) {
	typ, _ := n["type"].(string)
	attrs, _ := n["attrs"].(map[string]any)
	switch typ {
	case "text":
		w.text(n)
	case "paragraph":
		w.wrap("p", "", n)
	case "heading":
		lvl := 1
		if f, ok := attrs["level"].(float64); ok && f >= 1 && f <= 6 {
			lvl = int(f)
		}
		w.wrap("h"+strconv.Itoa(lvl), "", n)
	case "bulletList":
		w.wrap("ul", "", n)
	case "orderedList":
		attr := ""
		if f, ok := attrs["order"].(float64); ok && f > 1 {
			attr = ` start="` + strconv.Itoa(int(f)) + `"`
		}
		w.wrap("ol", attr, n)
	case "listItem":
		w.wrap("li", "", n)
	case "blockquote":
		w.wrap("blockquote", "", n)
	case "codeBlock":
		class := ""
		if l, _ := attrs["language"].(string); l != "" && langRE.MatchString(l) {
			class = ` class="language-` + l + `"`
		}
		w.sb.WriteString("<pre><code" + class + ">")
		w.children(n)
		w.sb.WriteString("</code></pre>")
	case "horizontalRule":
		w.sb.WriteString("<hr>")
	case "hardBreak":
		w.sb.WriteString("<br>")
	default:
		w.children(n)
	}
}

func (w richWriter) wrap(tag, attr string, n map[string]any) {
	w.sb.WriteString("<" + tag + attr + ">")
	w.children(n)
	w.sb.WriteString("</" + tag + ">")
}

func (w richWriter) text(n map[string]any) {
	s, _ := n["text"].(string)
	if s == "" {
		return
	}
	marks, _ := n["marks"].([]any)
	var closers []string
	for _, mk := range marks {
		m, _ := mk.(map[string]any)
		typ, _ := m["type"].(string)
		switch typ {
		case "bold":
			w.sb.WriteString("<strong>")
			closers = append(closers, "</strong>")
		case "italic":
			w.sb.WriteString("<em>")
			closers = append(closers, "</em>")
		case "code":
			w.sb.WriteString("<code>")
			closers = append(closers, "</code>")
		case "link":
			attrs, _ := m["attrs"].(map[string]any)
			href := w.href(attrs)
			if href == "" {
				continue
			}
			w.sb.WriteString(`<a href="` + html.EscapeString(href) + `">`)
			closers = append(closers, "</a>")
		}
	}
	w.sb.WriteString(html.EscapeString(s))
	for i := len(closers) - 1; i >= 0; i-- {
		w.sb.WriteString(closers[i])
	}
}

func (w richWriter) href(attrs map[string]any) string {
	h, _ := attrs["href"].(string)
	h = strings.TrimSpace(h)
	if id, ok := strings.CutPrefix(h, "item:"); ok {
		if w.link == nil {
			return ""
		}
		return w.link(id)
	}
	return safeHref(h)
}

// safeHref keeps http(s), mailto and scheme-less (relative) targets.
func safeHref(h string) string {
	clean := strings.Map(func(r rune) rune {
		if r <= ' ' || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, h)
	if clean == "" || strings.HasPrefix(clean, "//") || strings.Contains(clean, `\`) {
		return ""
	}
	if schemeRE.MatchString(clean) {
		l := strings.ToLower(clean)
		if !strings.HasPrefix(l, "http:") && !strings.HasPrefix(l, "https:") && !strings.HasPrefix(l, "mailto:") {
			return ""
		}
	}
	return h
}
