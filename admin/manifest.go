package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Manifest declares admin extensions: custom elements the admin loads lazily into its slots. The project
// declares it in config/app.yaml (admin section, files are paths on disk); modules register it in code
// (files are names inside their fs.FS, and theme must be empty).
type Manifest struct {
	Elements  []string          `yaml:"elements" json:"elements"`
	Theme     Theme             `yaml:"theme" json:"theme"`
	Strings   string            `yaml:"strings" json:"strings,omitempty"`
	Widgets   map[string]string `yaml:"widgets" json:"widgets"`
	Cells     map[string]string `yaml:"cells" json:"cells"`
	Panels    []Panel           `yaml:"panels" json:"panels"`
	Actions   []Panel           `yaml:"actions" json:"actions"`
	Dashboard []Panel           `yaml:"dashboard" json:"dashboard"`
	Views     []View            `yaml:"views" json:"views"`
	Slots     []Slot            `yaml:"slots" json:"slots"`
	// Previews maps a block type to the element that previews it (palette thumbnail, collapsed block card).
	Previews map[string]string `yaml:"previews" json:"previews"`
	// ListActions are toolbar and bulk actions of the collection list.
	ListActions []Panel `yaml:"list_actions" json:"listActions"`
}

// Theme in a Manifest holds file paths (Logo, CSS); in config.json it holds URLs.
type Theme struct {
	Name   string `yaml:"name" json:"name"`
	Logo   string `yaml:"logo" json:"logo,omitempty"`
	Accent string `yaml:"accent" json:"accent,omitempty"`
	CSS    string `yaml:"css" json:"css,omitempty"`
}

// Panel is an editor side panel, an editor toolbar action or a dashboard widget. Types limits panels and
// actions to content types (empty: every type); Policy hides the entry from principals lacking it.
type Panel struct {
	Tag    string   `yaml:"tag" json:"tag"`
	Title  string   `yaml:"title" json:"title"`
	Types  []string `yaml:"types" json:"types,omitempty"`
	Policy string   `yaml:"policy" json:"policy,omitempty"`
}

// View is a full screen at Path (admin-relative) with a menu entry grouped under Menu.
type View struct {
	Path   string `yaml:"path" json:"path"`
	Tag    string `yaml:"tag" json:"tag"`
	Menu   string `yaml:"menu" json:"menu,omitempty"`
	Title  string `yaml:"title" json:"title"`
	Policy string `yaml:"policy" json:"policy,omitempty"`
}

// Slot places an element at a known position (see SlotPositions).
type Slot struct {
	Tag    string `yaml:"tag" json:"tag"`
	At     string `yaml:"at" json:"at"`
	Policy string `yaml:"policy" json:"policy,omitempty"`
}

// SlotPositions are the valid values of Slot.At.
var SlotPositions = []string{"login.before", "login.after", "list.before", "list.after",
	"editor.before", "editor.after", "dashboard.before", "dashboard.after",
	"pages.before", "pages.after", "assets.before", "assets.after", "search.before", "search.after",
	"releases.before", "releases.after", "reviews.before", "reviews.after", "trash.before", "trash.after",
	"settings.before", "settings.after"}

// BuiltinRoutes are the admin's own screens; a view path may not equal or sit under one.
var BuiltinRoutes = []string{"/", "/login", "/search", "/account", "/collections", "/items", "/singletons", "/pages", "/settings", "/releases", "/trash", "/reviews", "/assets"}

var (
	tagName    = regexp.MustCompile(`^[a-z][a-z0-9_.]*-[a-z0-9_.-]*$`)
	identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	viewPath   = regexp.MustCompile(`^(/[A-Za-z0-9._~-]+|/:[A-Za-z][A-Za-z0-9_]*)+(/\*)?$`)
	hexColour  = regexp.MustCompile(`^#([0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	fnColour   = regexp.MustCompile(`^(rgb|rgba|hsl|hsla)\([0-9a-zA-Z%., /+-]+\)$`)
	named      = regexp.MustCompile(`^[a-zA-Z]{3,24}$`)
	logoExt    = []string{".svg", ".png", ".jpg", ".jpeg", ".webp", ".gif", ".avif", ".ico"}
)

// CheckOptions says what a manifest is checked against. Nil functions skip that check.
type CheckOptions struct {
	// Where prefixes error messages: "admin" or "module x: admin extension".
	Where string
	// File reports why a declared file can't be served (nil error: fine).
	File   func(name string) error
	Policy func(name string) bool
	Type   func(name string) bool
	// Block reports whether a block type exists.
	Block func(name string) bool
	// Module is true for module manifests: the theme must be empty.
	Module bool
	// SkipElements skips the existence check of elements (dev, where a dev server supplies them).
	SkipElements bool
}

// Check validates the manifest and joins every problem it finds.
func (m *Manifest) Check(o CheckOptions) error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(o.Where+"."+format, args...)) }
	file := func(key, name string) {
		if name == "" {
			bad("%s: empty file name", key)
		} else if o.File != nil {
			if err := o.File(name); err != nil {
				bad("%s: %v", key, err)
			}
		}
	}
	tag := func(key, v string) {
		if !tagName.MatchString(v) {
			bad("%s %q: want a custom element name (lower-case, with a dash)", key, v)
		}
	}
	policy := func(key, v string) {
		if v != "" && o.Policy != nil && !o.Policy(v) {
			bad("%s %q: unknown policy (declare it with Registry.Policy)", key, v)
		}
	}
	types := func(key string, ts []string) {
		for _, t := range ts {
			if o.Type != nil && !o.Type(t) {
				bad("%s: unknown content type %q", key, t)
			}
		}
	}
	title := func(key, v string) {
		if strings.TrimSpace(v) == "" {
			bad("%s: title is required", key)
		}
	}
	for i, e := range m.Elements {
		if !o.SkipElements {
			file("elements["+strconv.Itoa(i)+"]", e)
		}
	}
	if m.Strings != "" {
		file("strings", m.Strings)
	}
	t := m.Theme
	if o.Module && t != (Theme{}) {
		bad("theme: only the project sets the theme")
	}
	for k, v := range map[string]string{"name": t.Name, "logo": t.Logo, "accent": t.Accent, "css": t.CSS} {
		if strings.Contains(v, "url(") || strings.ContainsAny(v, ";}") {
			bad("theme.%s: must not contain url(, ; or }", k)
		}
	}
	if t.Logo != "" {
		file("theme.logo", t.Logo)
		if !slices.Contains(logoExt, strings.ToLower(path.Ext(t.Logo))) {
			bad("theme.logo: want one of %s", strings.Join(logoExt, " "))
		}
	}
	if t.CSS != "" {
		file("theme.css", t.CSS)
	}
	if a := t.Accent; a != "" && !hexColour.MatchString(a) && !fnColour.MatchString(a) && !named.MatchString(a) {
		bad("theme.accent %q: want a colour literal (#0b6, rgb(…), hsl(…) or a name)", a)
	}
	for _, kv := range []struct {
		key string
		m   map[string]string
	}{{"widgets", m.Widgets}, {"cells", m.Cells}} {
		for _, name := range sortedKeys(kv.m) {
			if !identifier.MatchString(name) {
				bad("%s: name %q: use letters, digits, _ and -, starting with a letter", kv.key, name)
			}
			tag(kv.key+"."+name, kv.m[name])
		}
	}
	for _, name := range sortedKeys(m.Previews) {
		k := "previews." + name
		if !identifier.MatchString(name) {
			bad("previews: block type %q: use letters, digits, _ and -, starting with a letter", name)
		} else if o.Block != nil && !o.Block(name) {
			bad("%s: unknown block type %q", k, name)
		}
		tag(k, m.Previews[name])
	}
	for _, g := range []struct {
		key   string
		items []Panel
		typed bool
	}{{"panels", m.Panels, true}, {"actions", m.Actions, true}, {"list_actions", m.ListActions, true},
		{"dashboard", m.Dashboard, false}} {
		for i, p := range g.items {
			k := g.key + "[" + strconv.Itoa(i) + "]"
			tag(k+".tag", p.Tag)
			title(k, p.Title)
			policy(k+".policy", p.Policy)
			if !g.typed && len(p.Types) > 0 {
				bad("%s.types: not supported here", k)
			}
			types(k+".types", p.Types)
		}
	}
	for i, v := range m.Views {
		k := "views[" + strconv.Itoa(i) + "]"
		switch {
		case !viewPath.MatchString(v.Path):
			bad("%s.path %q: must start with / and use segments of letters, digits and . _ ~ -, :name parameters, and at most one final *", k, v.Path)
		case builtinRoute(v.Path):
			bad("%s.path %q: collides with a built-in admin route", k, v.Path)
		}
		tag(k+".tag", v.Tag)
		title(k, v.Title)
		policy(k+".policy", v.Policy)
	}
	for i, s := range m.Slots {
		k := "slots[" + strconv.Itoa(i) + "]"
		tag(k+".tag", s.Tag)
		if !slices.Contains(SlotPositions, s.At) {
			bad("%s.at %q: want one of %s", k, s.At, strings.Join(SlotPositions, ", "))
		}
		policy(k+".policy", s.Policy)
	}
	return errors.Join(errs...)
}

func builtinRoute(p string) bool {
	for _, b := range BuiltinRoutes {
		if p == b || b != "/" && strings.HasPrefix(p, b+"/") {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Source is one origin of a manifest. FS nil means the project: file names are paths on disk and are read at
// request time. Otherwise Module names the module and files are names inside FS, read once.
type Source struct {
	Module   string
	FS       fs.FS
	Manifest Manifest
}

// Extensions is the "extensions" object of config.json, composed from every source (project first, then
// modules in start-up order; except elements and strings: modules first, the project last so its strings win), plus the files it serves under <prefix>/x/. All lists are present, possibly empty.
type Extensions struct {
	Elements    []string          `json:"elements"`
	Theme       Theme             `json:"theme"`
	Strings     []string          `json:"strings"`
	Widgets     map[string]string `json:"widgets"`
	Cells       map[string]string `json:"cells"`
	Panels      []Panel           `json:"panels"`
	Actions     []Panel           `json:"actions"`
	Dashboard   []Panel           `json:"dashboard"`
	Views       []View            `json:"views"`
	Slots       []Slot            `json:"slots"`
	Previews    map[string]string `json:"previews"`
	ListActions []Panel           `json:"listActions"`

	files map[string]*xfile
}

// Compose merges sources into what config.json lists and the handler serves. A non-empty devEntry lists
// <prefix>/x/dev<devEntry> in place of the project's element files.
func Compose(prefix string, srcs []Source, devEntry string) (*Extensions, error) {
	x := &Extensions{Elements: []string{}, Strings: []string{}, Widgets: map[string]string{}, Cells: map[string]string{},
		Panels: []Panel{}, Actions: []Panel{}, Dashboard: []Panel{}, Views: []View{}, Slots: []Slot{},
		Previews: map[string]string{}, ListActions: []Panel{}, files: map[string]*xfile{}}
	base := prefix + "/x/"
	add := func(rel string, f *xfile) string {
		x.files[rel] = f
		return base + rel
	}
	paths := map[string]string{}
	var projEls, projStrs []string
	for _, s := range srcs {
		els, strs := &x.Elements, &x.Strings
		if s.FS == nil {
			els, strs = &projEls, &projStrs
		}
		m := s.Manifest
		open := func(name string) (*xfile, error) { return openFile(s.FS, name) }
		serve := func(rel, name string) (string, error) {
			f, err := open(name)
			if err != nil {
				return "", err
			}
			return add(rel, f), nil
		}
		modRel := func(name string) string { return "m/" + s.Module + "/" + path.Clean(name) }
		if devEntry != "" && s.FS == nil {
			*els = append(*els, base+"dev"+devEntry)
		} else {
			for i, e := range m.Elements {
				rel := strconv.Itoa(i) + ".js"
				if s.FS != nil {
					rel = modRel(e)
				}
				u, err := serve(rel, e)
				if err != nil {
					return nil, err
				}
				*els = append(*els, u)
			}
		}
		if m.Strings != "" {
			rel := "strings.json"
			if s.FS != nil {
				rel = modRel(m.Strings)
			}
			u, err := serve(rel, m.Strings)
			if err != nil {
				return nil, err
			}
			*strs = append(*strs, u)
		}
		if s.FS == nil {
			x.Theme = Theme{Name: m.Theme.Name, Accent: m.Theme.Accent}
			if m.Theme.Logo != "" {
				u, err := serve("logo"+strings.ToLower(path.Ext(m.Theme.Logo)), m.Theme.Logo)
				if err != nil {
					return nil, err
				}
				x.Theme.Logo = u
			}
			if m.Theme.CSS != "" {
				u, err := serve("theme.css", m.Theme.CSS)
				if err != nil {
					return nil, err
				}
				x.Theme.CSS = u
			}
		}
		who := "project"
		if s.FS != nil {
			who = "module " + s.Module
		}
		for _, kv := range []struct {
			key string
			dst map[string]string
			src map[string]string
		}{{"widgets", x.Widgets, m.Widgets}, {"cells", x.Cells, m.Cells}, {"previews", x.Previews, m.Previews}} {
			for _, name := range sortedKeys(kv.src) {
				if _, dup := kv.dst[name]; dup {
					return nil, fmt.Errorf("%s: admin %s name %q is already taken", who, kv.key, name)
				}
				kv.dst[name] = kv.src[name]
			}
		}
		for _, v := range m.Views {
			if prev, dup := paths[v.Path]; dup {
				return nil, fmt.Errorf("%s: admin view path %q is already used by %s", who, v.Path, prev)
			}
			paths[v.Path] = who
		}
		x.Panels = append(x.Panels, m.Panels...)
		x.Actions = append(x.Actions, m.Actions...)
		x.Dashboard = append(x.Dashboard, m.Dashboard...)
		x.Views = append(x.Views, m.Views...)
		x.Slots = append(x.Slots, m.Slots...)
		x.ListActions = append(x.ListActions, m.ListActions...)
	}
	x.Elements = append(x.Elements, projEls...)
	x.Strings = append(x.Strings, projStrs...)
	return x, nil
}

// xfile is one served file: read from disk at request time (project) or held in memory (modules).
type xfile struct {
	disk string
	data []byte
	etag string
	typ  string
}

func openFile(fsys fs.FS, name string) (*xfile, error) {
	f := &xfile{typ: contentType(name)}
	if fsys == nil {
		f.disk = name
		_, _, err := f.read()
		return f, err
	}
	if !fs.ValidPath(name) {
		return nil, fmt.Errorf("%q is not a valid file name", name)
	}
	st, err := fs.Stat(fsys, name)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%q is not a regular file", name)
	}
	if f.data, err = fs.ReadFile(fsys, name); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(f.data)
	f.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
	return f, nil
}

// read returns the content and its ETag.
func (f *xfile) read() ([]byte, string, error) {
	if f.disk == "" {
		return f.data, f.etag, nil
	}
	st, err := os.Stat(f.disk)
	if err != nil {
		return nil, "", err
	}
	if !st.Mode().IsRegular() {
		return nil, "", fmt.Errorf("%q is not a regular file", f.disk)
	}
	b, err := os.ReadFile(f.disk)
	if err != nil {
		return nil, "", err
	}
	return b, `"` + strconv.FormatInt(st.Size(), 36) + "-" + strconv.FormatInt(st.ModTime().UnixNano(), 36) + `"`, nil
}

// CheckFile reports why name can't be served from fsys (nil: the project's disk).
func CheckFile(fsys fs.FS, name string) error {
	_, err := openFile(fsys, name)
	return err
}

var contentTypes = map[string]string{
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".css": "text/css; charset=utf-8",
	".json": "application/json", ".map": "application/json", ".svg": "image/svg+xml", ".png": "image/png",
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif", ".avif": "image/avif",
	".ico": "image/x-icon", ".woff2": "font/woff2", ".txt": "text/plain; charset=utf-8",
}

func contentType(name string) string {
	if t, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}
