# The site module: websites rendered by the binary

`modules/site` renders a website from the same routing, delivery and preview layers any external frontend uses. A
web channel opts in with `render: go` (it needs a `match` and public access). Pages, collection routes, previews
and cache purges work out of the box with the embedded **starter** theme; this page covers what you add: your own
templates, themes, custom routes and components.

```yaml
# config/app.yaml
modules:
  pages: { sites: [main] }
  site:
    theme: starter          # the embedded theme (default); see Themes
    dir: site               # the project's own files (default "site")
    admin_url: https://cms.example.com   # required outside dev
    routes:                 # collection items that are not in the page tree
      - { path: /blog, type: post, template: posts, limit: 20 }
      - { path: "/blog/{slug}", type: post }
```

`hydrogen site:check [-db]` parses every template and renders each page type, collection route, block type and
custom route against sample data (with `-db`: against the published content too). `hydrogen config:check`
validates the theme, its bases and the custom routes' reserved paths without a database.

## Layers

Files are found by name in layers; a later layer replaces an earlier one's file of the same name:

1. the theme's base chain (the base first),
2. the selected theme,
3. the project's `site/` directory.

Each layer may hold `templates/`, `assets/` and `translations/`. Templates are `layout.html`, `404.html`,
`pages/<type>.html` (else `pages/default.html`), `collections/<template>.html`, `blocks/<type>.html`,
`routes/<name>.html` (custom routes) and any partials. Translation files (`translations/en.yaml`) merge key by key
across layers. Assets are served under fingerprinted URLs (`{{asset "site.css"}}`).

Collection listing routes are paged: `/blog` is page 1 and `/blog/page/2` the next, `limit` items each. A listing
template gets `.Pager` (`.Page`, `.Prev`, `.Next`; nil on a single page); the starter theme shows it with
`partials/pager.html`.

## Template functions

Every template has these generic functions. They are pure and never mark a string as safe HTML, so
`html/template` still escapes everything by its context. The subject comes last, so they chain:
`{{.Title | truncate 40 "…"}}`.

| Function | Meaning |
| --- | --- |
| `dict "k" v …`, `list a b …` | a `map[string]any` from key/value pairs; a list |
| `add`, `sub`, `mul`, `div`, `mod` | arithmetic on ints and floats (JSON numbers are floats). The result is an int when both operands are whole numbers (`div` only when exact: `7 \| div 2` is 3.5); dividing by zero is an error, `mod` needs integers |
| `seq n`, `seq a b` | `0..n-1`; `a..b` included (descending when `a > b`); at most 10000 numbers |
| `default d v` | `v`, or `d` when `v` is empty (nil, "", 0, false, empty list or map) |
| `lower`, `upper`, `title`, `trim` | case (title: each word's first letter) and surrounding space |
| `contains needle s` | substring of a string, or element of a list |
| `hasPrefix p s`, `hasSuffix p s` | |
| `split sep s`, `join sep list`, `replace old new s` | |
| `truncate n s`, `truncate n "…" s` | at most `n` characters (runes, ellipsis included) |
| `date layout v` | formats a `time.Time` or a string (RFC 3339 or `2006-01-02`) with a Go layout; "" when `v` is not a date |
| `json v` | the JSON text, for `data-*` attributes (inside `<script>` pass the value itself: `html/template` already encodes it as JSON) |
| `first list`, `last list` | the element, or nil |

`len`, `index`, `slice`, `eq`, `printf` and the other functions of Go templates are available as usual.

Projects add their own with `site.Funcs`, available when templates are parsed and rendered, including in themes:

```go
site.Funcs(h, template.FuncMap{"initials": func(name string) string { return … }})
```

A function returns one value, or a value and an error. A name that collides with a built-in, a page helper (`blocks`,
`partial`, `items`, `item`, `query`, `richtext`, `link`, `t`, `meta`, `nav`, `breadcrumbs`, `asset`, `edit`, `setting`,
`component`), a Go template function or another registration fails start-up and `config:check`. A module that
`Require`s `site` calls `kernel.Lookup[*site.Extensions](r).AddFuncs(m)`.

## Reading content in templates

These helpers read in process, as a public client of the channel (drafts when previewing), in the page's locale.
Everything they read tags the page, so publishing a listed item purges it (and the static export re-renders it).
Identical calls within one render ask the delivery API once.

- `items "post" (dict "limit" 4 "sort" "-slug" "where[category][eq]" "news")` returns the items (`[]*Item`, each with
  `.Path` so `{{link .}}` works). Dict values are strings, numbers, booleans or lists (repeated parameters); an
  unknown field or a 5xx answer is an error.
- `item "id"` returns one item or nil.
- `query "/tree" (dict "depth" 3)` returns the decoded JSON (`map[string]any`) of a delivery path (`/tree`,
  `/search`, …) when it answers 200, else nil; a 5xx answer is an error.
- `partial "blocks/hero/wide.html" data` executes a template by a computed name (Go's `{{template}}` needs a
  constant) and returns its HTML; `data` defaults to the page's view. A missing template is an error in
  `site:check` and a logged, empty result when serving. Partials may nest up to 32 levels.

```html
{{range items "post" (dict "limit" 3 "sort" "-slug")}}<a href="{{link .}}">{{.Name}}</a>{{end}}
{{partial (printf "blocks/imageBoxes/%s.html" .Props.view) .}}
```

In `site:check` the reading helpers return empty results (no database). Go components use `v.Client()`, the same
client with `Get`, `Items` and `Item`.

## Themes

A theme is a `site.Theme{Name, FS, Base}`; `FS` holds `templates/`, `assets/` and `translations/` and `Base` names
the theme it builds on (`starter` for the embedded one). Ship one as a Go package:

```go
package acmetheme

//go:embed all:templates all:assets all:translations
var files embed.FS

func Theme() site.Theme { return site.Theme{Name: "acme", FS: files, Base: "starter"} }
```

Register it in the project and select it in config:

```go
func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), pages.Module(), site.Module())
	site.UseTheme(h, acmetheme.Theme())
}
```

```yaml
modules:
  site:
    theme: acme
```

An unknown theme, a missing base, a cycle or a theme registered twice fails `config:check` and start-up. A module
that provides themes calls `kernel.Lookup[*site.Extensions](r)` in `Register` (it must `Require` `site`) and uses
`AddTheme`.

## Custom routes

For pages that are not content (search results, forms, accounts), add a route to the builder:

```go
site.Handle(h, "GET /search", func(r *site.Request) (*site.Response, error) {
	var found struct{ Items []*site.Item `json:"items"` }
	if _, err := r.Get("/search", url.Values{"q": {r.Query.Get("q")}}, &found); err != nil {
		return nil, err
	}
	return &site.Response{Template: "routes/search.html", Title: "Search", Data: found.Items}, nil
}).Sample(func() *site.Response {
	return &site.Response{Template: "routes/search.html"}
})
```

- The pattern is `http.ServeMux` syntax relative to the channel's prefix (`"/search"`, `"POST /contact"`,
  `"GET /tags/{tag}"`). Without a method every method matches.
- The route's whole pattern must be reserved: every path it can match has to fall under a `reserved` entry of
  each channel it applies to. `/account/{id}/edit` needs `/account/*`; `/{a}/edit` is refused because no
  reserved entry covers it. The path must be one the channel **reserves** (`reserved: [/search]` in `channels.yaml`; `/account/*` for a
  subtree), so the page tree never routes it. `config:check` fails otherwise.
- `Request` carries the HTTP request (`R`), the wildcards (`Params`), the query, and a content client: `Get`,
  `Items`, `Item` read the published content (drafts when previewing) in process and tag the page with what they
  read. `URL(path)` and `LinkTo(item)` build site URLs.
- `Response` renders `Template` (a file under `routes/`, or a component) with the full page context: layout,
  helpers, meta, navigation, translations, settings, preview. `Data` is `.Data` in the template, the wildcards
  are `.Params`. `Redirect` (302, or 303 after a non-GET) answers a redirect instead. `Status`, `Headers`, `Tags`
  and `Cacheable` are optional; the default is `Cache-Control: no-store`, and the in-process page cache never
  stores custom routes. Return `site.ErrNotFound` for the 404 page; other errors are a logged 500.
- **Safety.** A non-GET/HEAD request is refused with 403 when the browser marks it cross-site
  (`Sec-Fetch-Site` other than `same-origin`/`none`, or an `Origin` that is neither the request host nor the
  channel's `frontend`); there are no CSRF tokens, so a route that must accept cross-origin calls opts out with
  `.AllowCrossSite()` and then owns its own authentication. The request body is capped at 1 MiB (multipart
  included; `.MaxBody(n)` changes it) and reading past it fails with `*http.MaxBytesError`, which answers 413.
  `Response.Redirect` is sent as given: never put unvalidated user input in it (open redirect). Output of a
  `site.Renderer` is not escaped by the site: templ escapes for you, but a component that writes strings or a
  `template.HTML` value directly must escape them itself.
- `.Sample(fn)` gives `site:check` data to render the route with (the function itself is not called).
- `.Static(fn)` (or `.Export()` for a route without wildcards) lists the parameter sets that become files in a
  static export; `site.ClientFrom(ctx)` reads content inside it. `.Channels(names...)` restricts the route.

## Components and templ

A template name can be backed by a Go component instead of a file. `site.Renderer` has exactly the method set of
`templ.Component`, so [templ](https://templ.guide) components plug in without hydrogen depending on templ:

```go
site.Component(h, "routes/colophon.html", func(v *site.View) site.Renderer { return colophon(v) })
```

A component registered under a page-like name (`pages/…`, `collections/…`, `routes/…`, `404.html`) renders the
whole document and wins over a file of that name; under `blocks/<type>.html` it renders that block (`v.Block`).
Templates can render any component with `{{component "name"}}`. `View` has the helpers templates have as methods:
`T`, `Meta`, `Nav`, `Breadcrumbs`, `Asset`, `Link`, `Setting`, `Edit`, `Blocks`, `RichText`.

`examples/fullstack` has a theme package (`theme/`), a template-based route (`/search`) and a templ page
(`/colophon`; run `go run github.com/a-h/templ/cmd/templ@v0.3.1070 generate` after editing the `.templ` file and
commit the generated file).

## Rendering without HTTP (for modules)

`*site.Exporter` (from `kernel.Lookup[*site.Exporter](r)` in `Register`, usable from `Boot`) renders what a
visitor would see, for modules that write the site out:

```go
Channels() []ExportChannel                                   // rendered channels: Name, Host, Prefix, Origin, Languages
Targets(ctx, channel string) ([]Target, error)               // every renderable URL, sorted
Render(ctx, channel, uri string) (*Rendered, error)          // Status, Header, Body, Tags
Purge(tags ...string)                                        // drop cached delivery answers before re-rendering
Assets() ([]AssetFile, error)                                // fingerprinted assets
```

`Targets` lists pages and mounted items, collection routes and every listing page, custom routes with static
params and the 404 page (kind `not_found`, rendered with status 404). `Render` takes a `Target.URL` (path and
query with the channel's prefix, no host), bypasses the in-process page cache, never previews, and returns the
cache tags the answer touched. Redirects and misses are results; a 5xx is an error.
