package schema

import (
	"net/url"
	"slices"
	"strings"
)

type richtextType struct{}

func (richtextType) Name() string { return "richtext" }

func richMarkNames() []string {
	return []string{"bold", "code", "italic", "link", "strike", "subscript", "superscript", "underline"}
}

func richNodeNames() []string {
	return []string{"blockquote", "bulletList", "codeBlock", "hardBreak", "heading", "horizontalRule",
		"image", "orderedList", "paragraph", "table"}
}

func (richtextType) Compile(o *Options) (Shape, Config, error) {
	cfg := &richtextConfig{rt: o.c.rt}
	cfg.marks = allowList(o, "marks", append(richMarkNames(), customNames(o.c.rt.Marks, func(m *RichMark) (string, bool) { return m.Name, true })...))
	cfg.nodes = allowList(o, "nodes", append(richNodeNames(), customNames(o.c.rt.Nodes, func(n *RichNode) (string, bool) { return n.Name, !n.Part })...))
	if cfg.nodes != nil {
		for _, name := range cfg.nodes {
			if n, ok := o.c.rt.Node(name); ok && n.Part {
				o.Errorf("nodes", "nodes names %q, a part node: it is allowed where its parent is", name)
			}
		}
	}
	return Shape{Base: BaseRichText}, cfg, nil
}

func customNames[T any](list []T, name func(T) (string, bool)) []string {
	var out []string
	for _, e := range list {
		if n, ok := name(e); ok {
			out = append(out, n)
		}
	}
	return out
}

func allowList(o *Options, key string, known []string) []string {
	list, ok := o.Strings(key)
	if !ok {
		return nil
	}
	for _, name := range list {
		if !slices.Contains(known, name) {
			o.Errorf(key, "%s names unknown %q (known: %s)", key, name, strings.Join(known, ", "))
		}
	}
	return uniqueSorted(list)
}

type richtextConfig struct {
	rt           *RichText
	marks, nodes []string // allow-lists; nil allows everything
}

func (c *richtextConfig) allowsNode(t string, cn *RichNode) bool {
	if c.nodes == nil {
		return true
	}
	if cn != nil {
		return cn.Part || slices.Contains(c.nodes, t)
	}
	switch t {
	case "listItem":
		return slices.Contains(c.nodes, "bulletList") || slices.Contains(c.nodes, "orderedList")
	case "tableRow", "tableCell", "tableHeader":
		return slices.Contains(c.nodes, "table")
	case "text":
		return true
	}
	return slices.Contains(c.nodes, t)
}

func (c *richtextConfig) allowsMark(t string) bool {
	return c.marks == nil || slices.Contains(c.marks, t)
}

// rtSeg is one step from the field to a node: into content[idx], or into marks[idx] when mark is set.
type rtSeg struct {
	mark bool
	idx  int
}

// rtCheck walks a document; the path of a node is only built when an error is reported.
type rtCheck struct {
	c    *richtextConfig
	r    *Report
	segs []rtSeg
	ids  map[string]struct{}
}

func (k *rtCheck) fail(format string, args ...any) {
	k.failCode("richtext", format, args...)
}

func (k *rtCheck) failCode(code, format string, args ...any) {
	if k.r.full() {
		return
	}
	p := k.r.here()
	for _, sg := range k.segs {
		if sg.mark {
			p = p.Key("marks").Index(sg.idx)
		} else {
			p = p.Key("content").Index(sg.idx)
		}
	}
	k.r.Add(p, code, format, args...)
}

func (c *richtextConfig) Validate(v any, _ Mode, r *Report) {
	doc, ok := v.(map[string]any)
	if !ok {
		typeErr(r, "a rich text document")
		return
	}
	k := &rtCheck{c: c, r: r}
	if t, _ := doc["type"].(string); t != "doc" {
		k.fail(`the root node must have type "doc"`)
		return
	}
	known := 1
	if _, ok := doc["content"]; ok {
		known++
	}
	if len(doc) > known {
		k.fail("the document may only have type and content")
		return
	}
	k.children(doc, "doc", nil)
}

// children checks n's content. pn is n's declaration when it is a custom node.
func (k *rtCheck) children(n map[string]any, parent string, pn *RichNode) {
	raw, present := n["content"]
	if !present {
		if pn != nil {
			k.noChildren(pn, 0)
		}
		return
	}
	list, ok := raw.([]any)
	if !ok {
		k.fail("content must be a list")
		return
	}
	if pn != nil {
		k.noChildren(pn, len(list))
	}
	for i, e := range list {
		if k.r.full() {
			return
		}
		k.segs = append(k.segs, rtSeg{idx: i})
		if m, ok := e.(map[string]any); ok {
			k.node(m, parent, pn, i)
		} else {
			k.fail("a node must be an object")
		}
		k.segs = k.segs[:len(k.segs)-1]
	}
}

// noChildren checks how many children a custom node has against its content rule.
func (k *rtCheck) noChildren(pn *RichNode, n int) {
	switch pn.Content {
	case "blocks":
		if n == 0 {
			k.fail("%s needs at least one block", pn.Name)
		}
	case "parts":
		if n != len(pn.Parts) {
			k.fail("%s needs %d parts: %s", pn.Name, len(pn.Parts), strings.Join(pn.Parts, ", "))
		}
	}
}

// node checks one node under parent (pn: its declaration when custom); idx is its position among siblings.
func (k *rtCheck) node(n map[string]any, parent string, pn *RichNode, idx int) {
	if !k.nodeKeys(n) {
		return
	}
	t, _ := n["type"].(string)
	var cn *RichNode
	if t != "" {
		cn = k.c.rt.nodes[t]
	}
	switch {
	case t == "":
		k.fail("a node needs a type")
		return
	case cn == nil && !validRichNode(t):
		k.fail("unknown node type %q", t)
		return
	case pn != nil && pn.Content == "parts":
		if idx >= len(pn.Parts) || pn.Parts[idx] != t {
			k.fail("%s needs %s at position %d", parent, partName(pn, idx), idx+1)
			return
		}
	case !childAllowed(parent, pn, t, cn):
		k.fail("%s cannot contain %s", parent, t)
		return
	case !k.c.allowsNode(t, cn):
		k.fail("%s nodes are not allowed here", t)
		return
	}
	if t != "text" && n["marks"] != nil {
		k.fail("only text can have marks")
	}
	if cn != nil {
		k.customAttrs(n["attrs"], cn.Attrs, cn.Name)
		if cn.Content == "none" {
			if raw := n["content"]; raw != nil {
				if l, ok := raw.([]any); !ok || len(l) > 0 {
					k.fail("%s cannot have content", t)
				}
			}
			return
		}
		k.children(n, t, cn)
		return
	}
	k.attrs(n, t)
	if t == "text" {
		k.text(n)
		return
	}
	if len(childTypes(t)) == 0 {
		if raw := n["content"]; raw != nil {
			if l, ok := raw.([]any); !ok || len(l) > 0 {
				k.fail("%s cannot have content", t)
			}
		}
		return
	}
	k.children(n, t, nil)
}

func partName(pn *RichNode, idx int) string {
	if idx < len(pn.Parts) {
		return pn.Parts[idx]
	}
	return "no more children"
}

// customAttrs checks the attrs of a custom node or mark against its declaration.
func (k *rtCheck) customAttrs(raw any, decl []*RichAttr, name string) {
	attrs, isMap := raw.(map[string]any)
	if raw != nil && !isMap {
		k.fail("attrs must be an object")
		return
	}
	for key := range attrs {
		if !slices.ContainsFunc(decl, func(a *RichAttr) bool { return a.Name == key }) {
			for _, key := range sortedKeys(attrs) {
				if !slices.ContainsFunc(decl, func(a *RichAttr) bool { return a.Name == key }) {
					k.fail("%s does not take attribute %q", name, key)
				}
			}
			break
		}
	}
	for _, a := range decl {
		v := attrs[a.Name]
		if v == nil {
			if a.Required {
				k.fail("%s needs attrs.%s", name, a.Name)
			}
			continue
		}
		if p := a.problem(v); p != "" {
			k.fail("%s attribute %s %s", name, a.Name, p)
		}
	}
}

func (k *rtCheck) nodeKeys(n map[string]any) bool {
	known := 0
	for _, key := range [...]string{"type", "content", "attrs", "text", "marks"} {
		if _, ok := n[key]; ok {
			known++
		}
	}
	if len(n) == known {
		return true
	}
	for _, key := range sortedKeys(n) {
		switch key {
		case "type", "content", "attrs", "text", "marks":
		default:
			k.fail("unknown node key %q", key)
		}
	}
	return false
}

func (k *rtCheck) attrs(n map[string]any, t string) {
	raw, present := n["attrs"]
	if !present {
		if t == "heading" {
			k.fail("heading needs attrs.level")
		}
		return
	}
	attrs, ok := raw.(map[string]any)
	if !ok {
		k.fail("attrs must be an object")
		return
	}
	for _, key := range sortedKeys(attrs) {
		if !slices.Contains(nodeAttrNames[t], key) {
			k.fail("%s does not take attribute %q", t, key)
		}
	}
	switch t {
	case "tableCell", "tableHeader":
		k.cellAttrs(attrs)
	case "image":
		k.imageAttrs(attrs)
	case "heading":
		if lv, ok := intOf(attrs["level"]); !ok || lv < 1 || lv > 6 {
			k.fail("heading level must be 1 to 6")
		}
		k.alignID(attrs)
	case "paragraph":
		k.alignID(attrs)
	case "orderedList":
		if v := attrs["start"]; v != nil {
			if n, ok := intOf(v); !ok || n < -1_000_000 || n > 1_000_000 {
				k.fail("orderedList start must be an integer from -1000000 to 1000000")
			}
		}
		k.tight(attrs)
	case "bulletList":
		k.tight(attrs)
	case "codeBlock":
		if lang, ok := attrs["language"]; ok {
			if _, isStr := lang.(string); !isStr {
				k.fail("codeBlock language must be a string")
			}
		}
	}
}

var nodeAttrNames = map[string][]string{
	"paragraph":   {"align", "id"},
	"bulletList":  {"tight"},
	"orderedList": {"start", "tight"},
	"heading":     {"level", "align", "id"},
	"codeBlock":   {"language"},
	"tableCell":   {"colspan", "rowspan", "colwidth"},
	"tableHeader": {"colspan", "rowspan", "colwidth"},
	"image":       {"src", "alt", "title", "width", "height"},
}

func (k *rtCheck) alignID(attrs map[string]any) {
	if v := attrs["align"]; v != nil {
		switch v {
		case "left", "center", "right", "justify":
		default:
			k.fail("align must be left, center, right or justify")
		}
	}
	if v := attrs["id"]; v != nil {
		id, _ := v.(string)
		if !validAnchor(id) {
			k.fail("id must start with a letter and use letters, digits, - and _ (at most 64 characters)")
			return
		}
		if _, dup := k.ids[id]; dup {
			k.fail("id %q is used twice in the document", id)
			return
		}
		if k.ids == nil {
			k.ids = map[string]struct{}{}
		}
		k.ids[id] = struct{}{}
	}
}

func (k *rtCheck) tight(attrs map[string]any) {
	if v := attrs["tight"]; v != nil {
		if _, ok := v.(bool); !ok {
			k.fail("tight must be true or false")
		}
	}
}

// validAnchor matches ^[A-Za-z][A-Za-z0-9_-]{0,63}$.
func validAnchor(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

func (k *rtCheck) cellAttrs(attrs map[string]any) {
	for _, key := range [...]string{"colspan", "rowspan"} {
		if v, ok := attrs[key]; ok {
			if n, ok := intOf(v); !ok || n < 1 || n > 1000 {
				k.fail("%s must be an integer from 1 to 1000", key)
			}
		}
	}
	raw, ok := attrs["colwidth"]
	if !ok || raw == nil {
		return
	}
	list, isList := raw.([]any)
	if !isList || len(list) == 0 || len(list) > 1000 {
		k.fail("colwidth must be a list of column widths")
		return
	}
	for _, e := range list {
		if n, ok := intOf(e); !ok || n < 0 || n > 100000 {
			k.fail("colwidth entries must be integers from 0 to 100000")
			return
		}
	}
}

func (k *rtCheck) imageAttrs(attrs map[string]any) {
	if src, _ := attrs["src"].(string); !validImageSrc(src) {
		k.fail("image needs attrs.src: an http(s) URL or a root-relative path")
	}
	for _, key := range [...]string{"alt", "title"} {
		if v, ok := attrs[key]; ok && v != nil {
			if s, isStr := v.(string); !isStr || strings.IndexByte(s, 0) >= 0 {
				k.fail("image %s must be a string", key)
			}
		}
	}
	for _, key := range [...]string{"width", "height"} {
		if v, ok := attrs[key]; ok && v != nil {
			if n, ok := intOf(v); !ok || n < 1 || n > 100000 {
				k.fail("image %s must be an integer from 1 to 100000", key)
			}
		}
	}
}

// validImageSrc accepts absolute http(s) URLs and root-relative paths such as the media library's
// /assets/{sha}/{name}. Protocol-relative ("//host"), backslashes and control characters are refused like in links.
func validImageSrc(s string) bool {
	if s == "" || strings.ContainsFunc(s, func(r rune) bool { return r == '\\' || r < 0x20 || r == 0x7f }) {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return !strings.HasPrefix(s, "//")
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (k *rtCheck) text(n map[string]any) {
	if s, ok := n["text"].(string); !ok || s == "" {
		k.fail("text nodes need non-empty text")
	} else if strings.IndexByte(s, 0) >= 0 {
		k.failCode("format", "must not contain NUL characters")
	}
	if _, has := n["content"]; has {
		k.fail("text nodes cannot have content")
	}
	raw, present := n["marks"]
	if !present {
		return
	}
	marks, ok := raw.([]any)
	if !ok {
		k.fail("marks must be a list")
		return
	}
	for i, e := range marks {
		if k.r.full() {
			return
		}
		k.segs = append(k.segs, rtSeg{mark: true, idx: i})
		if m, ok := e.(map[string]any); ok {
			k.mark(m, marks[:i])
		} else {
			k.fail("a mark must be an object")
		}
		k.segs = k.segs[:len(k.segs)-1]
	}
}

func (k *rtCheck) mark(m map[string]any, before []any) {
	t, _ := m["type"].(string)
	cm := k.c.rt.marks[t]
	if cm == nil {
		switch t {
		case "bold", "italic", "code", "link", "underline", "strike", "superscript", "subscript":
		default:
			k.fail("unknown mark type %q", t)
			return
		}
	}
	if !k.c.allowsMark(t) {
		k.fail("%s marks are not allowed here", t)
	}
	for _, p := range before {
		if pm, ok := p.(map[string]any); ok && pm["type"] == t {
			k.fail("duplicate %s mark", t)
			break
		}
	}
	if cm != nil {
		k.customAttrs(m["attrs"], cm.Attrs, cm.Name)
		return
	}
	if t != "link" {
		if m["attrs"] != nil {
			k.fail("%s marks take no attributes", t)
		}
		return
	}
	attrs, _ := m["attrs"].(map[string]any)
	href, _ := attrs["href"].(string)
	if !validHref(href) {
		k.fail("link needs attrs.href: an http(s) URL, mailto: address, #anchor or item:<id>")
	}
	for key := range attrs {
		if key != "href" && key != "target" {
			k.fail("link does not take attribute %q", key)
			break
		}
	}
	if v := attrs["target"]; v != nil && v != "_blank" {
		k.fail("link target must be _blank")
	}
}

// validHref accepts item: links (with an optional #anchor), #anchors, mailto: addresses and absolute http(s) URLs. Backslashes and control
// characters are refused anywhere: browsers read a backslash as a slash in http(s) URLs, so values such as
// "/\evil.com" would redirect to another host.
func validHref(h string) bool {
	if strings.ContainsFunc(h, func(r rune) bool { return r == '\\' || r < 0x20 || r == 0x7f }) {
		return false
	}
	if rest, ok := strings.CutPrefix(h, "item:"); ok {
		id, frag, has := strings.Cut(rest, "#")
		return isUUID(id) && (!has || validAnchor(frag))
	}
	if frag, ok := strings.CutPrefix(h, "#"); ok {
		return validAnchor(frag)
	}
	if rest, ok := strings.CutPrefix(h, "mailto:"); ok {
		return rest != ""
	}
	u, err := url.Parse(h)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func validRichNode(t string) bool {
	switch t {
	case "paragraph", "heading", "bulletList", "orderedList", "listItem", "blockquote", "codeBlock",
		"horizontalRule", "hardBreak", "text", "image", "table", "tableRow", "tableCell", "tableHeader":
		return true
	}
	return false
}

func childTypes(parent string) []string {
	switch parent {
	case "doc", "blockquote", "listItem", "tableCell", "tableHeader":
		return []string{"paragraph", "heading", "bulletList", "orderedList", "blockquote", "codeBlock", "horizontalRule",
			"image", "table"}
	case "table":
		return []string{"tableRow"}
	case "tableRow":
		return []string{"tableCell", "tableHeader"}
	case "bulletList", "orderedList":
		return []string{"listItem"}
	case "paragraph", "heading":
		return []string{"text", "hardBreak"}
	case "codeBlock":
		return []string{"text"}
	}
	return nil
}

// childAllowed says whether child (cn: its declaration when custom) may stand under parent (pn: its declaration
// when custom). Part nodes never stand anywhere but in their parent's part sequence.
func childAllowed(parent string, pn *RichNode, child string, cn *RichNode) bool {
	if pn != nil {
		switch {
		case cn != nil:
			return pn.Content == "blocks" && !cn.Inline && !cn.Part || pn.Content == "inline" && cn.Inline
		case pn.Content == "inline":
			return child == "text" || child == "hardBreak"
		case pn.Content == "blocks":
			return slices.Contains(childTypes("doc"), child)
		}
		return false
	}
	if cn != nil {
		if cn.Part {
			return false
		}
		if cn.Inline {
			return parent == "paragraph" || parent == "heading"
		}
		switch parent {
		case "doc", "blockquote", "listItem", "tableCell", "tableHeader":
			return true
		}
		return false
	}
	return slices.Contains(childTypes(parent), child)
}

// intOf reads an exact integer from any JSON number representation.
func intOf(v any) (int, bool) {
	f, exact, ok := numberOf(v)
	return int(f), ok && exact
}

func (c *richtextConfig) IsEmpty(v any) bool {
	doc, ok := v.(map[string]any)
	if !ok {
		return false
	}
	list, _ := doc["content"].([]any)
	return !slices.ContainsFunc(list, c.hasContent)
}

func (c *richtextConfig) hasContent(v any) bool {
	n, ok := v.(map[string]any)
	if !ok {
		return false
	}
	t, _ := n["type"].(string)
	switch t {
	case "text":
		s, _ := n["text"].(string)
		return s != ""
	case "horizontalRule", "hardBreak", "image":
		return true
	}
	if cn := c.rt.nodes[t]; cn != nil && cn.Content == "none" {
		return true
	}
	list, _ := n["content"].([]any)
	return slices.ContainsFunc(list, c.hasContent)
}

func (c *richtextConfig) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"type":    map[string]any{"const": "doc"},
			"content": map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		},
		"required":             []string{"type"},
		"additionalProperties": false,
	}
}
