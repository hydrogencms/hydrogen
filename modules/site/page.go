package site

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

// View is the data root of page templates.
type View struct {
	Item       *Item   // the page or collection item; nil on listings and the 404 page
	Items      []*Item // listings
	Fields     map[string]any
	Title      string
	Locale     string
	Path       string // the page's URL on this site (channel-relative)
	Channel    ChannelView
	Params     map[string]string
	Settings   map[string]map[string]any // singleton type → fields
	Alternates []pages.Alternate         // the page in the other languages of its group, when there are any
	Preview    bool
	Status     int
	// Data is what a custom route's Response carried.
	Data any
	// Pager is set on a paged collection listing.
	Pager *Pager
	// Block is the block a block component renders.
	Block *BlockView

	p *page // the render the helper methods below read
}

// Pager links the pages of a collection listing (/blog, /blog/page/2…). Prev and Next are empty at the ends.
type Pager struct {
	Page int
	Prev string
	Next string
}

// ChannelView describes the channel being served.
type ChannelView struct {
	Name      string
	Languages []string
	Home      string // URL of the channel's home page
}

// NavItem is a navigation entry. Active marks the current page, Open also its ancestors.
type NavItem struct {
	Name     string
	URL      string
	Active   bool
	Open     bool
	Children []*NavItem
}

// Crumb is a breadcrumb.
type Crumb struct {
	Name string
	URL  string
}

// BlockView is a block as block templates see it. Attrs goes on the block's root element.
type BlockView struct {
	ID     string
	Type   string
	Props  map[string]any
	Slots  map[string][]*BlockView
	Blocks []*BlockView // shared blocks: the shared item's blocks, read only
	Path   string       // data-h-path; empty inside shared blocks
	Attrs  template.HTMLAttr
	Page   *View // the page being rendered: .Page.Path, .Page.Query "name"
}

// page is one render: the helpers read from it.
type page struct {
	eng      *engine
	ch       *channel.Channel
	locale   string
	def      string
	preview  bool
	strict   bool
	log      *slog.Logger
	view     *View
	links    map[string]string
	canon    string
	alts     []pages.Alternate
	crumbs   []Crumb
	loadNav  func() ([]*NavItem, error)
	navList  []*NavItem
	navDone  bool
	t        *template.Template
	siteName string
	ctx      context.Context
	f        *fetcher // nil in site:check
	itemPath func(*Item) string
	// linkFields names the fields a type's detail route needs in its URLs ("" = every type).
	linkFields func(typ string) []string
	memo       map[string]any
	rt         *schema.RichText // the project's custom rich text vocabulary; nil renders custom types by convention
	rtv        *richVocab
	depth      int
	rawQuery   string
	query      url.Values
	qread      map[string]bool
}

func (p *page) context() context.Context {
	if p.ctx == nil {
		return context.Background()
	}
	return p.ctx
}

func (p *page) funcs() template.FuncMap {
	m := template.FuncMap{
		"blocks":      p.blocksFn,
		"partial":     p.partialFn,
		"hasTemplate": p.hasTemplateFn,
		"items":       p.itemsFn,
		"item":        p.itemFn,
		"query":       p.queryFn,
		"richtext":    p.richtextFn,
		"link":        p.linkFn,
		"t":           p.tFn,
		"meta":        p.metaFn,
		"nav":         p.navFn,
		"breadcrumbs": p.breadcrumbsFn,
		"asset":       p.assetFn,
		"edit":        p.editFn,
		"setting":     p.settingFn,
		"component":   p.componentFn,
	}
	if p.eng != nil {
		for _, f := range p.eng.viewFuncs {
			for k, fn := range f(p.view) {
				m[k] = fn
			}
		}
	}
	return m
}

// queryValues are the request's values of a query parameter. Reading one marks the render as depending on it,
// which the page cache keys on; "preview" (the preview token) is never readable.
func (p *page) queryValues(name string) []string {
	if name == "preview" {
		return nil
	}
	if p.qread == nil {
		p.qread = map[string]bool{}
	}
	p.qread[name] = true
	if p.query == nil {
		p.query, _ = url.ParseQuery(p.rawQuery)
		if p.query == nil {
			p.query = url.Values{}
		}
	}
	return p.query[name]
}

// queryNames are the query parameters the render read, sorted.
func (p *page) queryNames() []string {
	if len(p.qread) == 0 {
		return nil
	}
	return sortedKeys(p.qread)
}

func (p *page) problem(format string, args ...any) error {
	err := fmt.Errorf(format, args...)
	if p.strict {
		return err
	}
	if p.log != nil {
		p.log.Warn("site: "+err.Error(), "channel", p.ch.Name)
	}
	return nil
}

// blocksFn renders a blocks field of the current item by name, a slot, a single block, or a blocks value read
// from another item (rendered read only, without editing paths).
func (p *page) blocksFn(v any) (template.HTML, error) {
	var list []*BlockView
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		if p.view.Item != nil {
			list = p.parseBlocks(p.view.Item.Fields[x], "fields/"+x, false)
		}
	case []any:
		list = p.parseBlocks(x, "", true)
	case []*BlockView:
		list = x
	case *BlockView:
		list = []*BlockView{x}
	default:
		return "", fmt.Errorf("blocks: expected a field name, a slot or a block, got %T", v)
	}
	var buf bytes.Buffer
	for _, b := range list {
		if err := p.blockInto(&buf, b); err != nil {
			return "", err
		}
	}
	return template.HTML(buf.String()), nil
}

func (p *page) blockInto(buf *bytes.Buffer, b *BlockView) error {
	name := "blocks/" + b.Type + ".html"
	if f := p.eng.comps[name]; f != nil {
		cv := *p.view
		cv.Block = b
		out, err := p.eng.renderComponent(name, f, p, &cv)
		buf.Write(out)
		return err
	}
	if err := p.ensureSet(); err != nil {
		return err
	}
	if p.t.Lookup(name) == nil {
		if err := p.problem("no template %s for block type %s", name, b.Type); err != nil {
			return err
		}
		buf.WriteString("<!-- no template for block " + html.EscapeString(b.Type) + " -->")
		return nil
	}
	return p.t.ExecuteTemplate(buf, name, b)
}

func (p *page) parseBlocks(raw any, path string, readOnly bool) []*BlockView {
	list, _ := raw.([]any)
	out := make([]*BlockView, 0, len(list))
	for i, it := range list {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		b := &BlockView{Slots: map[string][]*BlockView{}, Page: p.view}
		b.ID, _ = m["id"].(string)
		b.Type, _ = m["type"].(string)
		if b.Props, _ = m["props"].(map[string]any); b.Props == nil {
			b.Props = map[string]any{}
		}
		attrs := `data-h-block="` + html.EscapeString(b.Type) + `"`
		here := path + "/" + strconv.Itoa(i)
		if !readOnly && p.preview {
			b.Path = here
			attrs += ` data-h-path="` + html.EscapeString(here) + `"`
		}
		b.Attrs = template.HTMLAttr(attrs)
		slots, _ := m["slots"].(map[string]any)
		for _, name := range sortedKeys(slots) {
			b.Slots[name] = p.parseBlocks(slots[name], here+"/slots/"+name, readOnly)
		}
		if inl, ok := m["blocks"]; ok {
			b.Blocks = p.parseBlocks(inl, "", true)
			b.Slots["blocks"] = b.Blocks
		}
		out = append(out, b)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// richtextFn renders a rich text document; the options are a dict of classes, wrap and wrapClass.
func (p *page) richtextFn(v any, opts ...map[string]any) (template.HTML, error) {
	var o RichTextOptions
	for _, m := range opts {
		for k, x := range m {
			s, _ := x.(string)
			switch k {
			case "wrap":
				o.Wrap = s
			case "wrapClass":
				o.WrapClass = s
			case "classes":
				switch c := x.(type) {
				case map[string]string:
					o.Classes = c
				case map[string]any:
					o.Classes = make(map[string]string, len(c))
					for t, cv := range c {
						o.Classes[t], _ = cv.(string)
					}
				default:
					return "", fmt.Errorf("richtext: classes must be a dict, got %T", x)
				}
			default:
				return "", fmt.Errorf("richtext: unknown option %q (classes, wrap, wrapClass)", k)
			}
		}
	}
	return p.richText(v, o)
}

func (p *page) richText(v any, o RichTextOptions) (template.HTML, error) {
	c := richConfig{link: func(id string) string { return p.linkFn(id) }, rt: p.vocab(), opts: o, exec: p.richExec, view: p.view}
	if p.eng != nil {
		c.over = p.eng.rich
	}
	out, err := renderRich(v, c)
	return template.HTML(out), err //nolint:gosec // renderRich escapes; override output is html/template's
}

func (p *page) vocab() *richVocab {
	if p.rtv == nil && p.rt != nil {
		p.rtv = newRichVocab(p.rt)
	}
	return p.rtv
}

// richExec runs the richtext/<type>.html override of a node or mark type.
func (p *page) richExec(typ string, d *RichElement) (string, bool, error) {
	name := "richtext/" + typ + ".html"
	if err := p.ensureSet(); err != nil {
		return "", false, err
	}
	if p.t.Lookup(name) == nil {
		return "", false, p.problem("no template %s", name)
	}
	if p.depth >= maxPartialDepth {
		return "", false, fmt.Errorf("%s: nested more than %d levels deep", name, maxPartialDepth)
	}
	p.depth++
	defer func() { p.depth-- }()
	var buf bytes.Buffer
	if err := p.t.ExecuteTemplate(&buf, name, d); err != nil {
		return "", false, err
	}
	return buf.String(), true, nil
}

// linkFn is the URL of a referenced item: an item id (with or without item:), or an item from a listing.
// It is empty when the item has no page on this site.
func (p *page) linkFn(v any) string {
	switch x := v.(type) {
	case string:
		return p.links[strings.TrimPrefix(x, "item:")]
	case *Item:
		if x == nil {
			return ""
		}
		if x.Path != "" {
			return x.Path
		}
		return p.links[x.ID]
	}
	return ""
}

func (p *page) tFn(key string, pairs ...any) string {
	return p.eng.trans.text(p.locale, p.def, key, pairs...)
}

func (p *page) editFn(path string) template.HTMLAttr {
	if !p.preview {
		return ""
	}
	return template.HTMLAttr(`data-h-path="` + html.EscapeString(path) + `"`)
}

func (p *page) assetFn(name string) (string, error) {
	u, ok := p.eng.assets.url(name)
	if !ok {
		if err := p.problem("asset %q does not exist in site/assets or the theme", name); err != nil {
			return "", err
		}
		return "", nil
	}
	return u, nil
}

func (p *page) navFn() ([]*NavItem, error) {
	if !p.navDone {
		p.navDone = true
		if p.loadNav != nil {
			var err error
			if p.navList, err = p.loadNav(); err != nil {
				return nil, err
			}
		}
	}
	return p.navList, nil
}

func (p *page) breadcrumbsFn() []Crumb { return p.crumbs }

// metaFn renders the head tags: title, description, canonical, hreflang alternates and Open Graph basics.
func (p *page) metaFn() template.HTML {
	var sb strings.Builder
	title := p.view.Title
	switch {
	case title == "":
		title = p.siteName
	case p.siteName != "" && p.view.Path != p.view.Channel.Home && title != p.siteName:
		title += " | " + p.siteName
	}
	esc := html.EscapeString
	if title != "" {
		sb.WriteString("<title>" + esc(title) + "</title>\n")
		sb.WriteString(`<meta property="og:title" content="` + esc(p.view.Title) + `">` + "\n")
	}
	if d := p.description(); d != "" {
		sb.WriteString(`<meta name="description" content="` + esc(d) + `">` + "\n")
		sb.WriteString(`<meta property="og:description" content="` + esc(d) + `">` + "\n")
	}
	if p.siteName != "" {
		sb.WriteString(`<meta property="og:site_name" content="` + esc(p.siteName) + `">` + "\n")
	}
	if p.canon != "" {
		sb.WriteString(`<link rel="canonical" href="` + esc(p.canon) + `">` + "\n")
		sb.WriteString(`<meta property="og:url" content="` + esc(p.canon) + `">` + "\n")
	}
	for _, a := range p.alts {
		sb.WriteString(`<link rel="alternate" hreflang="` + esc(a.Locale) + `" href="` + esc(a.URL) + `">` + "\n")
	}
	return template.HTML(sb.String()) //nolint:gosec // every value is escaped
}

func (p *page) description() string {
	for _, k := range []string{"description", "excerpt", "summary"} {
		if s, _ := p.view.Fields[k].(string); strings.TrimSpace(s) != "" {
			s = strings.Join(strings.Fields(s), " ")
			if r := []rune(s); len(r) > 200 {
				s = string(r[:197]) + "..."
			}
			return s
		}
	}
	return ""
}

// settingFn reads a field of the singleton types (siteSettings and the like): the first type, by name, that
// has it.
func (p *page) settingFn(key string) any {
	for _, t := range sortedKeys(p.view.Settings) {
		if v, ok := p.view.Settings[t][key]; ok {
			return v
		}
	}
	return nil
}

// ensureSet gives a render that did not start from a template (a component) a template set for blocks.
func (p *page) ensureSet() error {
	if p.t != nil {
		return nil
	}
	t, err := p.eng.blockSet(p)
	if err != nil {
		return err
	}
	p.t = t
	return nil
}

// componentFn renders a registered component with the page's view.
func (p *page) componentFn(name string) (template.HTML, error) {
	f := p.eng.comps[name]
	if f == nil {
		if err := p.problem("no component %s", name); err != nil {
			return "", err
		}
		return "", nil
	}
	out, err := p.eng.renderComponent(name, f, p, p.view)
	return template.HTML(out), err //nolint:gosec // component output is HTML by contract
}

// Partial executes a template of the set by name, like {{partial}}; data defaults to the view.
func (v *View) Partial(name string, data ...any) (template.HTML, error) {
	return v.p.partialFn(name, data...)
}

// The methods below give components (which are not templates) the helpers templates have.

// T translates a key in the page's locale; pairs replace {name} placeholders.
func (v *View) T(key string, pairs ...any) string { return v.p.tFn(key, pairs...) }

// Meta renders the head tags: title, description, canonical, hreflang and Open Graph basics.
func (v *View) Meta() template.HTML { return v.p.metaFn() }

// Nav is the site navigation (two levels).
func (v *View) Nav() ([]*NavItem, error) { return v.p.navFn() }

// Breadcrumbs are the current page's ancestors.
func (v *View) Breadcrumbs() []Crumb { return v.p.breadcrumbsFn() }

// Asset is the fingerprinted URL of a file of the theme's or project's assets.
func (v *View) Asset(name string) (string, error) { return v.p.assetFn(name) }

// Query is the first value of a query parameter of the request, or "". Reading a parameter makes the cached page
// depend on it; parameters a template does not read never split the cache. Static exports have no query.
func (v *View) Query(name string) string {
	if vals := v.p.queryValues(name); len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// QueryValues are all values of a query parameter, in request order.
func (v *View) QueryValues(name string) []string { return slices.Clone(v.p.queryValues(name)) }

// List reads a collection like {{items}}; params are its dict (limit, sort, where…). The page is tagged with
// what it reads.
func (v *View) List(typ string, params map[string]any) ([]*Item, error) {
	return v.p.itemsFn(typ, dictArgs(params)...)
}

// Get reads one item by id like {{item}}; params may hold fields and locale. It is nil when the item is not
// available.
func (v *View) Get(id string, params map[string]any) (*Item, error) {
	return v.p.itemFn(id, dictArgs(params)...)
}

func dictArgs(m map[string]any) []any {
	if m == nil {
		return nil
	}
	return []any{m}
}

// Link is the URL of a referenced item (id, or an item of a listing).
func (v *View) Link(item any) string { return v.p.linkFn(item) }

// Setting reads a field of the singleton types.
func (v *View) Setting(key string) any { return v.p.settingFn(key) }

// Edit marks an element for visual editing in previews.
func (v *View) Edit(path string) template.HTMLAttr { return v.p.editFn(path) }

// Blocks renders a blocks field of the item, a slot or a block.
func (v *View) Blocks(x any) (template.HTML, error) { return v.p.blocksFn(x) }

// RichText renders a rich text document; a failing richtext/<type>.html override is logged and renders nothing.
func (v *View) RichText(doc any, opts ...RichTextOptions) template.HTML {
	var o RichTextOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	out, err := v.p.richText(doc, o)
	if err != nil && v.p.log != nil {
		v.p.log.Warn("site: "+err.Error(), "channel", v.p.ch.Name)
	}
	return out
}
