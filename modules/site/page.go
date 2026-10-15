package site

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/channel"
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
}

func (p *page) funcs() template.FuncMap {
	return template.FuncMap{
		"blocks":      p.blocksFn,
		"richtext":    p.richtextFn,
		"link":        p.linkFn,
		"t":           p.tFn,
		"meta":        p.metaFn,
		"nav":         p.navFn,
		"breadcrumbs": p.breadcrumbsFn,
		"asset":       p.assetFn,
		"edit":        p.editFn,
		"setting":     p.settingFn,
	}
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

// blocksFn renders a blocks field of the current item by name, a slot, or a single block.
func (p *page) blocksFn(v any) (template.HTML, error) {
	var list []*BlockView
	switch x := v.(type) {
	case nil:
		return "", nil
	case string:
		if p.view.Item != nil {
			list = p.parseBlocks(p.view.Item.Fields[x], "fields/"+x, false)
		}
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
		b := &BlockView{Slots: map[string][]*BlockView{}}
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

func (p *page) richtextFn(v any) template.HTML {
	return template.HTML(renderRichText(v, func(id string) string { return p.linkFn(id) })) //nolint:gosec // renderRichText escapes
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
