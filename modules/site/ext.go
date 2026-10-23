package site

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/kernel"
)

// Theme is a package of templates, assets and translations. FS holds templates/, assets/ and translations/
// (any may be missing). A theme with a Base inherits the base's files, and its own files replace them by
// name; the project's site/ directory sits on top of the chain. A Go package ships a theme as
//
//	//go:embed all:templates all:assets all:translations
//	var files embed.FS
//	func Theme() site.Theme { return site.Theme{Name: "acme", FS: files, Base: "starter"} }
//
// and the project registers it with site.UseTheme and selects it with modules.site.theme.
type Theme struct {
	Name string
	FS   fs.FS
	// Base names the theme this one builds on (default: none; "starter" is the embedded theme).
	Base string
}

var themeNameRE = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func (t Theme) validate() error {
	switch {
	case !themeNameRE.MatchString(t.Name):
		return fmt.Errorf("site: theme name %q must be lower-case letters, digits and dashes", t.Name)
	case t.FS == nil:
		return fmt.Errorf("site: theme %s has no FS", t.Name)
	case t.Base == t.Name:
		return fmt.Errorf("site: theme %s is its own base", t.Name)
	case t.Base != "" && !themeNameRE.MatchString(t.Base):
		return fmt.Errorf("site: theme %s: base %q is not a theme name", t.Name, t.Base)
	}
	return nil
}

// Renderer is what a component renders with: exactly the method set of templ.Component, so templ components
// plug in without this module importing templ.
type Renderer interface {
	Render(ctx context.Context, w io.Writer) error
}

// ComponentFunc builds the renderer of a component for the page being rendered.
type ComponentFunc func(*View) Renderer

// Registrar is what the project's builder offers: *hydrogen.Builder implements it.
type Registrar interface {
	Use(mods ...kernel.Module)
}

// Extensions is what projects and other modules add to the site module: themes, custom routes and components.
// The site module provides it with kernel.Provide, so a module that Requires "site" can reach it from Register
// with kernel.Lookup[*site.Extensions]; projects use UseTheme, Handle and Component instead.
type Extensions struct {
	themes map[string]Theme
	routes []*CustomRoute
	// miss answers a page-tree miss (see NotFound).
	miss  func(*Request) (*Response, error)
	comps map[string]ComponentFunc
	funcs template.FuncMap
}

func newExtensions() *Extensions {
	x := &Extensions{themes: map[string]Theme{}, comps: map[string]ComponentFunc{}, funcs: template.FuncMap{}}
	x.themes[starterName] = Starter()
	return x
}

const starterName = "starter"

// Starter is the embedded theme every site starts from.
func Starter() Theme {
	sub, err := fs.Sub(themeFS, "themes/starter")
	if err != nil {
		panic(err) // the directory is embedded
	}
	return Theme{Name: starterName, FS: sub}
}

// AddTheme registers a theme; a name registers once.
func (x *Extensions) AddTheme(t Theme) error {
	if err := t.validate(); err != nil {
		return err
	}
	if _, dup := x.themes[t.Name]; dup {
		return fmt.Errorf("site: theme %s is registered twice", t.Name)
	}
	x.themes[t.Name] = t
	return nil
}

// AddComponent registers a component under a template name; it is used instead of a template file of that
// name (pages/…, collections/…, routes/…, blocks/…, 404.html) and is callable from templates as
// {{component "name"}}.
func (x *Extensions) AddComponent(name string, fn ComponentFunc) error {
	switch {
	case name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, ".."):
		return fmt.Errorf("site: component name %q must be a relative template name such as routes/search.html", name)
	case fn == nil:
		return fmt.Errorf("site: component %s has no function", name)
	}
	if _, dup := x.comps[name]; dup {
		return fmt.Errorf("site: component %s is registered twice", name)
	}
	x.comps[name] = fn
	return nil
}

// AddRoute registers a custom route built with Handle.
func (x *Extensions) AddRoute(r *CustomRoute) error {
	if r.err != nil {
		return r.err
	}
	for _, o := range x.routes {
		if o.method == r.method && o.path == r.path && channelsOverlap(o.channels, r.channels) {
			return fmt.Errorf("site: custom route %s is registered twice", r.pattern)
		}
	}
	x.routes = append(x.routes, r)
	return nil
}

// AddNotFound registers the function that answers requests no page, mounted item or route serves; one per project.
func (x *Extensions) AddNotFound(fn func(*Request) (*Response, error)) error {
	switch {
	case fn == nil:
		return errors.New("site: the not-found hook has no function")
	case x.miss != nil:
		return errors.New("site: the not-found hook is registered twice")
	}
	x.miss = fn
	return nil
}

func channelsOverlap(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	return slices.ContainsFunc(a, func(n string) bool { return slices.Contains(b, n) })
}

// chain returns the theme and its bases, base first. Unknown themes, missing bases and cycles are errors.
func (x *Extensions) chain(name string) ([]Theme, error) {
	var out []Theme
	var seen []string
	for cur := name; cur != ""; {
		if slices.Contains(seen, cur) {
			return nil, fmt.Errorf("site: theme cycle: %s", strings.Join(append(seen, cur), " -> "))
		}
		t, ok := x.themes[cur]
		switch {
		case ok:
		case cur == name:
			return nil, fmt.Errorf("site: unknown theme %q (registered: %s; register one with site.UseTheme)", name, strings.Join(x.themeNames(), ", "))
		default:
			return nil, fmt.Errorf("site: theme %s: base %q is not registered", seen[len(seen)-1], cur)
		}
		seen = append(seen, cur)
		out = append(out, t)
		cur = t.Base
	}
	slices.Reverse(out)
	return out, nil
}

func (x *Extensions) themeNames() []string {
	names := make([]string, 0, len(x.themes))
	for n := range x.themes {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// checkThemes validates every registered theme's chain, then the selected theme.
func (x *Extensions) checkThemes(selected string) error {
	var errs []error
	for _, n := range x.themeNames() {
		if _, err := x.chain(n); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		if _, err := x.chain(selected); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// extModule carries one registration from a project's builder to the site module.
type extModule struct {
	name  string
	apply func(*Extensions) error
}

func (m *extModule) Name() string     { return m.name }
func (*extModule) Requires() []string { return []string{"site"} }
func (*extModule) Origin() string     { return "main" }
func (m *extModule) Register(r *kernel.Registry) error {
	x, ok := kernel.Lookup[*Extensions](r)
	if !ok {
		return errors.New("the site module did not provide its extensions")
	}
	return m.apply(x)
}

func suffix(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:4])
}

// UseTheme registers a theme; modules.site.theme selects it.
func UseTheme(h Registrar, t Theme) {
	name := "site-theme-" + suffix(t.Name)
	if themeNameRE.MatchString(t.Name) {
		name = "site-theme-" + t.Name
	}
	h.Use(&extModule{name: name, apply: func(x *Extensions) error { return x.AddTheme(t) }})
}

// Component registers a component under a template name. When a page, collection, route, block or the 404
// page is rendered, a registered component wins over the theme's and the project's file of that name. A
// component of a page-like template renders the whole document (View has the helpers: Meta, Nav, T, Asset…); a
// block component finds its block in View.Block. Templates call any component as {{component "name"}}.
func Component(h Registrar, name string, fn func(*View) Renderer) {
	h.Use(&extModule{name: "site-component-" + suffix(name), apply: func(x *Extensions) error {
		return x.AddComponent(name, fn)
	}})
}

// NotFound answers the requests that no page, mounted item, collection route or custom route serves, before the
// channel's 404 page: redirect retired URLs (Response.Redirect, 302 unless Status says otherwise), or render a
// template with a status of its own. Response.Status defaults to 404 here, and an answer with no Redirect keeps
// the 404 page's role: search engines see the status you set. Return ErrNotFound (or a nil response) for the
// ordinary 404 page. Req.Path is the channel-relative path; reserved paths and the static export's 404 file
// never reach the hook. Its answers are never stored by the site's page cache or shared caches.
func NotFound(h Registrar, fn func(*Request) (*Response, error)) {
	h.Use(&extModule{name: "site-not-found", apply: func(x *Extensions) error { return x.AddNotFound(fn) }})
}

// Handle adds a route for non-CMS pages (search results, forms, accounts). The pattern uses http.ServeMux
// syntax ("/search", "GET /tags/{tag}", "POST /contact") relative to each rendered channel's prefix, and must
// lie on a path the channel reserves (its `reserved` list): config:check verifies. fn returns what to render
// with the full page context, a redirect, or an error (500; ErrNotFound answers the 404 page).
func Handle(h Registrar, pattern string, fn func(*Request) (*Response, error)) *CustomRoute {
	r := newCustomRoute(pattern, fn)
	h.Use(&extModule{name: "site-route-" + suffix(pattern), apply: func(x *Extensions) error { return x.AddRoute(r) }})
	return r
}
