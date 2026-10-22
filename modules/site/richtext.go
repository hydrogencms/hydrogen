package site

import (
	"html/template"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/core/schema"
)

var (
	langRE   = regexp.MustCompile(`^[A-Za-z0-9_+-]+$`)
	schemeRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	anchorRE = regexp.MustCompile(`^[A-Za-z][\w-]{0,63}$`)
)

// RichTextOptions are the per-call options of the richtext helper. Classes maps a node or mark type to class
// tokens added to its elements (custom ones after rt-NAME; anything but [A-Za-z0-9_ -] is dropped). Wrap
// (div, section or article) puts one element, with WrapClass, around non-empty output.
type RichTextOptions struct {
	Classes   map[string]string
	Wrap      string
	WrapClass string
}

// RichElement is what a richtext/<type>.html override template receives: the node or mark being rendered.
// Attrs has the declared defaults applied; Content is its children, already rendered; Class is the class
// string the options gave; Node is the raw document node.
type RichElement struct {
	Type    string
	Attrs   map[string]any
	Content template.HTML
	Class   string
	Node    map[string]any
	View    *View
}

type richConfig struct {
	link func(id string) string
	rt   *richVocab
	opts RichTextOptions
	over map[string]bool // the types that have a richtext/<type>.html template
	// exec runs an override; ok is false when the template is missing (the default element is written).
	exec func(typ string, d *RichElement) (out string, ok bool, err error)
	view *View
}

// renderRichText turns a stored ProseMirror document into HTML. Text is escaped, unknown nodes and marks
// render their children only, and links keep http(s), mailto and relative targets; item:<id> links resolve
// through link (an empty answer drops the link and keeps its text).
func renderRichText(doc any, link func(id string) string) string {
	out, _ := renderRich(doc, richConfig{link: link})
	return out
}

// renderRich renders a document with the declared custom vocabulary, class options and overrides.
func renderRich(doc any, c richConfig) (string, error) {
	m, ok := doc.(map[string]any)
	if !ok {
		return "", nil
	}
	var sb strings.Builder
	w := &richWriter{sb: &sb, cfg: &c}
	if len(c.opts.Classes) > 0 {
		w.classes = make(map[string]string, len(c.opts.Classes))
		for k, v := range c.opts.Classes {
			if classTokens(v) {
				w.classes[k] = v
			}
		}
	}
	w.kids(m, false, false, false)
	if w.err != nil {
		return "", w.err
	}
	out := sb.String()
	switch c.opts.Wrap {
	case "div", "section", "article":
		if out != "" {
			cls := ""
			if classTokens(c.opts.WrapClass) {
				cls = ` class="` + c.opts.WrapClass + `"`
			}
			out = "<" + c.opts.Wrap + cls + ">" + out + "</" + c.opts.Wrap + ">"
		}
	}
	return out, nil
}

func classTokens(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != ' ' && c != '-' {
			return false
		}
	}
	return true
}

type richWriter struct {
	sb      *strings.Builder
	stack   []*strings.Builder
	cfg     *richConfig
	classes map[string]string
	err     error
}

// kids renders the children of n. tight drops the paragraph wrapper of n's direct paragraphs; childTight is
// the same flag for the children themselves.
func (w *richWriter) kids(n map[string]any, inl, tight, childTight bool) {
	list, _ := n["content"].([]any)
	for _, c := range list {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if tight && cm["type"] == "paragraph" {
			w.kids(cm, true, false, false)
			continue
		}
		w.node(cm, inl, childTight)
	}
}

func hasText(n map[string]any) bool {
	list, _ := n["content"].([]any)
	for _, c := range list {
		if cm, ok := c.(map[string]any); ok && cm["type"] == "text" {
			return true
		}
	}
	return false
}

// open writes the start tag in the canonical attribute order: id, class, style, the element's own.
func (w *richWriter) open(typ, tag, id, style, own string) string {
	cls := ""
	if c := w.classes[typ]; c != "" {
		cls = ` class="` + c + `"`
	}
	return "<" + tag + id + cls + style + own + ">"
}

func (w *richWriter) node(n map[string]any, inl, tight bool) {
	if w.err != nil {
		return
	}
	typ, _ := n["type"].(string)
	if typ == "text" {
		w.text(n)
		return
	}
	attrs, _ := n["attrs"].(map[string]any)
	ci := typ == "paragraph" || typ == "heading" || hasText(n)
	lt := (typ == "bulletList" || typ == "orderedList") && attrs["tight"] == true
	switch typ {
	case "paragraph", "heading":
		tag := "p"
		if typ == "heading" {
			tag = "h" + strconv.Itoa(headingLevel(attrs["level"]))
		}
		id, style := "", ""
		if s, _ := attrs["id"].(string); s != "" && anchorRE.MatchString(s) {
			id = ` id="` + s + `"`
		}
		switch a, _ := attrs["align"].(string); a {
		case "left", "center", "right", "justify":
			style = ` style="text-align:` + a + `"`
		}
		w.element(typ, tag, w.open(typ, tag, id, style, ""), n, ci, tight, lt)
	case "bulletList":
		w.element(typ, "ul", w.open(typ, "ul", "", "", ""), n, ci, tight, lt)
	case "orderedList":
		own := ""
		if f, ok := attrs["start"].(float64); ok && f != 1 && math.Abs(f) <= 1e6 {
			own = ` start="` + strconv.Itoa(int(f)) + `"`
		}
		w.element(typ, "ol", w.open(typ, "ol", "", "", own), n, ci, tight, lt)
	case "listItem":
		w.element(typ, "li", w.open(typ, "li", "", "", ""), n, ci, tight, lt)
	case "blockquote":
		w.element(typ, "blockquote", w.open(typ, "blockquote", "", "", ""), n, ci, tight, lt)
	case "tableRow":
		w.element(typ, "tr", w.open(typ, "tr", "", "", ""), n, ci, tight, lt)
	case "table":
		o := w.enter(typ)
		open := w.open(typ, "table", "", "", "") + "<tbody>"
		if !o {
			w.sb.WriteString(open)
		}
		w.kids(n, ci, tight, lt)
		w.leave(o, typ, n, false, open, "</tbody></table>")
	case "tableCell", "tableHeader":
		tag := "td"
		if typ == "tableHeader" {
			tag = "th"
		}
		w.element(typ, tag, w.open(typ, tag, "", "", cellAttrs(attrs)), n, ci, tight, lt)
	case "codeBlock":
		code := "<code>"
		if l, _ := attrs["language"].(string); l != "" && langRE.MatchString(l) {
			code = `<code class="language-` + l + `">`
		}
		o := w.enter(typ)
		open := w.open(typ, "pre", "", "", "") + code
		if !o {
			w.sb.WriteString(open)
		}
		w.kids(n, ci, tight, lt)
		w.leave(o, typ, n, false, open, "</code></pre>")
	case "image":
		open := w.imageTag(attrs)
		if open == "" {
			return
		}
		w.void(typ, n, open)
	case "horizontalRule":
		w.void(typ, n, w.open(typ, "hr", "", "", ""))
	case "hardBreak":
		w.void(typ, n, w.open(typ, "br", "", "", ""))
	default:
		if !isName(typ) {
			w.kids(n, ci, tight, lt)
			return
		}
		tag, open := w.customOpen(typ, attrs, false, inl)
		w.element(typ, tag, open, n, ci, tight, lt)
	}
}

func headingLevel(v any) int {
	f, _ := v.(float64)
	switch {
	case f < 1:
		return 1
	case f > 6:
		return 6
	}
	return int(f)
}

func (w *richWriter) element(typ, tag, open string, n map[string]any, ci, tight, lt bool) {
	o := w.enter(typ)
	if !o {
		w.sb.WriteString(open)
	}
	w.kids(n, ci, tight, lt)
	w.leave(o, typ, n, false, open, "</"+tag+">")
}

func (w *richWriter) void(typ string, n map[string]any, open string) {
	o := w.enter(typ)
	if !o {
		w.sb.WriteString(open)
	}
	w.leave(o, typ, n, false, open, "")
}

// enter starts capturing the children of an element whose type has an override template.
func (w *richWriter) enter(typ string) bool {
	if w.cfg.over == nil || !w.cfg.over[typ] {
		return false
	}
	w.stack = append(w.stack, w.sb)
	w.sb = &strings.Builder{}
	return true
}

// leave closes an element with its end tag (close, "" for void elements) or, with an override, writes the
// template's output in place of the element.
func (w *richWriter) leave(o bool, typ string, n map[string]any, mark bool, open, close string) {
	if !o {
		w.sb.WriteString(close)
		return
	}
	body := w.sb.String()
	w.sb = w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	d := &RichElement{Type: typ, Attrs: w.attrsOf(typ, n, mark), Content: template.HTML(body), Class: w.classes[typ], Node: n, View: w.cfg.view} //nolint:gosec // the children are rendered by this writer, escaped
	out, ok, err := w.cfg.exec(typ, d)
	switch {
	case err != nil:
		w.err = err
	case ok:
		w.sb.WriteString(out)
	default:
		w.sb.WriteString(open + body + close)
	}
}

func (w *richWriter) text(n map[string]any) {
	if s, _ := n["text"].(string); s != "" {
		marks, _ := n["marks"].([]any)
		w.mark(marks, s)
	}
}

// mark writes s inside the marks, the first being the outermost.
func (w *richWriter) mark(marks []any, s string) {
	if w.err != nil {
		return
	}
	if len(marks) == 0 {
		escape(w.sb, s, false)
		return
	}
	m, _ := marks[0].(map[string]any)
	typ, _ := m["type"].(string)
	attrs, _ := m["attrs"].(map[string]any)
	var tag, open string
	switch typ {
	case "bold":
		tag = "strong"
	case "italic":
		tag = "em"
	case "code":
		tag = "code"
	case "underline":
		tag = "u"
	case "strike":
		tag = "s"
	case "superscript":
		tag = "sup"
	case "subscript":
		tag = "sub"
	case "link":
		href := w.href(attrs)
		if href == "" {
			w.mark(marks[1:], s)
			return
		}
		own := ` href="` + attrEscape(href) + `"`
		if attrs["target"] == "_blank" {
			own += ` target="_blank" rel="noopener noreferrer"`
		}
		tag, open = "a", w.open(typ, "a", "", "", own)
	default:
		if !isName(typ) {
			w.mark(marks[1:], s)
			return
		}
		tag, open = w.customOpen(typ, attrs, true, false)
	}
	if open == "" {
		open = w.open(typ, tag, "", "", "")
	}
	o := w.enter(typ)
	if !o {
		w.sb.WriteString(open)
	}
	w.mark(marks[1:], s)
	w.leave(o, typ, m, true, open, "</"+tag+">")
}

// customOpen is the start tag of a custom node or mark: its declared tag (div or span otherwise), the
// rt-NAME class and one data attribute per declared attribute (per stored attribute, sorted, when undeclared).
func (w *richWriter) customOpen(typ string, attrs map[string]any, mark, inl bool) (tag, open string) {
	decl, tagged, declared := w.declOf(typ, mark)
	switch {
	case tagged != "":
		tag = tagged
	case mark || (declared && decl) || (!declared && inl):
		tag = "span"
	default:
		tag = "div"
	}
	var sb strings.Builder
	sb.WriteString("<" + tag + ` class="rt-` + typ)
	if c := w.classes[typ]; c != "" {
		sb.WriteString(" " + c)
	}
	sb.WriteByte('"')
	list := w.declAttrs(typ, mark)
	if declared {
		for _, a := range list {
			v := attrs[a.Name]
			if v == nil {
				v = a.Default
			}
			dataAttr(&sb, a.Name, v, a.Type == "url")
		}
	} else {
		for _, k := range sortedKeys(attrs) {
			dataAttr(&sb, k, attrs[k], false)
		}
	}
	sb.WriteByte('>')
	return tag, sb.String()
}

// richVocab indexes the declared custom nodes and marks; nil when there are none.
type richVocab struct {
	nodes map[string]*schema.RichNode
	marks map[string]*schema.RichMark
}

func newRichVocab(rt *schema.RichText) *richVocab {
	if rt == nil || len(rt.Nodes)+len(rt.Marks) == 0 {
		return nil
	}
	v := &richVocab{nodes: make(map[string]*schema.RichNode, len(rt.Nodes)), marks: make(map[string]*schema.RichMark, len(rt.Marks))}
	for _, n := range rt.Nodes {
		v.nodes[n.Name] = n
	}
	for _, m := range rt.Marks {
		v.marks[m.Name] = m
	}
	return v
}

// declOf is the declared inline flag and tag of a custom node or mark, and whether it is declared.
func (w *richWriter) declOf(typ string, mark bool) (inline bool, tag string, declared bool) {
	v := w.cfg.rt
	if v == nil {
		return false, "", false
	}
	if mark {
		if m, ok := v.marks[typ]; ok {
			return false, m.Tag, true
		}
		return false, "", false
	}
	if n, ok := v.nodes[typ]; ok {
		return n.Inline, n.Tag, true
	}
	return false, "", false
}

func (w *richWriter) declAttrs(typ string, mark bool) []*schema.RichAttr {
	v := w.cfg.rt
	if v == nil {
		return nil
	}
	if mark {
		if m, ok := v.marks[typ]; ok {
			return m.Attrs
		}
	} else if n, ok := v.nodes[typ]; ok {
		return n.Attrs
	}
	return nil
}

// attrsOf is what an override template sees as .Attrs: the stored attributes of a built-in, the declared
// ones with their defaults for a custom type.
func (w *richWriter) attrsOf(typ string, n map[string]any, mark bool) map[string]any {
	stored, _ := n["attrs"].(map[string]any)
	out := make(map[string]any, len(stored))
	if _, _, declared := w.declOf(typ, mark); declared && !builtinType(typ) {
		for _, a := range w.declAttrs(typ, mark) {
			v := stored[a.Name]
			if v == nil {
				v = a.Default
			}
			if v != nil {
				out[a.Name] = v
			}
		}
		return out
	}
	for k, v := range stored {
		out[k] = v
	}
	return out
}

func builtinType(typ string) bool {
	switch typ {
	case "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock", "table", "tableRow",
		"tableCell", "tableHeader", "image", "horizontalRule", "hardBreak", "bold", "italic", "code", "underline",
		"strike", "superscript", "subscript", "link":
		return true
	}
	return false
}

// dataAttr writes data-NAME (camelCase turned to kebab-case) for a scalar value; false and nil are left out,
// true is an empty attribute, urls must pass the image source check.
func dataAttr(sb *strings.Builder, name string, v any, url bool) {
	if !isName(name) {
		return
	}
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool:
		if !x {
			return
		}
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		s = strconv.FormatInt(x, 10)
	case int:
		s = strconv.Itoa(x)
	default:
		return
	}
	if url {
		if s = imageSrc(s); s == "" {
			return
		}
	}
	sb.WriteString(" data-")
	for i := 0; i < len(name); i++ {
		if c := name[i]; c >= 'A' && c <= 'Z' {
			sb.WriteByte('-')
			sb.WriteByte(c + 32)
		} else {
			sb.WriteByte(c)
		}
	}
	sb.WriteString(`="`)
	escape(sb, s, true)
	sb.WriteByte('"')
}

// isName reports whether s can name a custom node, mark or attribute: [a-z][a-zA-Z0-9]*.
func isName(s string) bool {
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

// escape writes s with & < > escaped, and " as well inside attribute values.
func escape(sb *strings.Builder, s string, quotes bool) {
	set := "&<>"
	if quotes {
		set = `&<>"`
	}
	if !strings.ContainsAny(s, set) {
		sb.WriteString(s)
		return
	}
	last := 0
	for i := 0; i < len(s); i++ {
		var r string
		switch s[i] {
		case '&':
			r = "&amp;"
		case '<':
			r = "&lt;"
		case '>':
			r = "&gt;"
		case '"':
			if !quotes {
				continue
			}
			r = "&quot;"
		default:
			continue
		}
		sb.WriteString(s[last:i])
		sb.WriteString(r)
		last = i + 1
	}
	sb.WriteString(s[last:])
}

func attrEscape(s string) string {
	var sb strings.Builder
	escape(&sb, s, true)
	return sb.String()
}

func cellAttrs(attrs map[string]any) string {
	out := ""
	for _, k := range [...]string{"colspan", "rowspan"} {
		if f, ok := attrs[k].(float64); ok && f > 1 && f <= 1000 {
			out += " " + k + `="` + strconv.Itoa(int(f)) + `"`
		}
	}
	return out
}

// imageSrc keeps http(s) and root-relative sources (not protocol-relative); anything else is empty.
func imageSrc(u string) string {
	s := strings.TrimSpace(u)
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return ""
		}
	}
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") {
		return s
	}
	if l := strings.ToLower(s); strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
		return s
	}
	return ""
}

// imageTag is the start tag of an image, empty when its source is unsafe.
func (w *richWriter) imageTag(attrs map[string]any) string {
	src, _ := attrs["src"].(string)
	if src = imageSrc(src); src == "" {
		return ""
	}
	alt, _ := attrs["alt"].(string)
	own := ` src="` + attrEscape(src) + `" alt="` + attrEscape(alt) + `"`
	if t, _ := attrs["title"].(string); t != "" {
		own += ` title="` + attrEscape(t) + `"`
	}
	for _, k := range [...]string{"width", "height"} {
		if f, ok := attrs[k].(float64); ok && f >= 1 && f <= 100000 {
			own += " " + k + `="` + strconv.Itoa(int(f)) + `"`
		}
	}
	return w.open("image", "img", "", "", own+` loading="lazy"`)
}

// href is the checked URL of a link mark. item:<id>[#frag] resolves the bare id and appends #frag; an
// unresolved or unsafe target is empty.
func (w *richWriter) href(attrs map[string]any) string {
	h, _ := attrs["href"].(string)
	h = strings.TrimSpace(h)
	if ref, ok := strings.CutPrefix(h, "item:"); ok {
		id, frag, _ := strings.Cut(ref, "#")
		if frag != "" {
			frag = "#" + frag
		}
		if w.cfg.link == nil {
			return ""
		}
		u := safeHref(w.cfg.link(id))
		if u == "" {
			return ""
		}
		return u + frag
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
