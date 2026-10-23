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

Templates are parsed file by file. Outside dev the server refuses to start while a file does not parse and names
each one. In dev a broken file fails only the pages that use it (the error is on the page and in the log); after a
reload the file's last good version is served instead, and every page shows the parse errors over it until they are
fixed.

In dev a request checks the site directory (templates, assets, translations) for changes, at most every 250 ms, and
a reload clears the page cache.

## Layers

Files are found by name in layers; a later layer replaces an earlier one's file of the same name:

1. the theme's base chain (the base first),
2. the selected theme,
3. the project's `site/` directory.

Each layer may hold `templates/`, `assets/` and `translations/`. Templates are `layout.html`, `404.html`,
`pages/<type>.html` (else `pages/default.html`), `collections/<template>.html`, `blocks/<type>.html`,
`richtext/<type>.html` (rich text overrides), `routes/<name>.html` (custom routes) and any partials. Translation files (`translations/en.yaml`) merge key by key
across layers. Assets are served under fingerprinted URLs (`{{asset "site.css"}}`).

### Assets

`{{asset "name"}}` returns `/__hydrogen/assets/<name>.<hash>.<ext>`, served with `Cache-Control: public,
max-age=31536000, immutable`. Files are listed at start but not read: a file is hashed (SHA-256, streamed) the first
time a template or a request needs its fingerprint, remembered until the next reload, and served from the file
system (or the theme's embedded files) on each request, so start-up and memory do not grow with the size of the
assets. A fingerprint that no longer matches the file answers 404.

A request for the plain name (`/__hydrogen/assets/sprite.svg`, as a script-loaded chunk or an SVG sprite is
fetched) works too. It is answered with a strong `ETag` (the content hash), `Last-Modified` (the file's modification
time, when the file system has one) and `Cache-Control: no-cache`, so a browser revalidates and gets `304 Not
Modified` while the file is unchanged.

In style sheets (`.css`) the `url(...)` and `@import` references to other assets are rewritten to their fingerprinted
URLs, whether quoted or not and for both plain and fingerprinted requests. A reference is relative to the sheet
(`../fonts/a.woff2`) or root-relative into the assets directory (`/fonts/a.woff2`, or the full
`/__hydrogen/assets/fonts/a.woff2`). `data:` and other schemes, `http(s):`, `//host/…`, `#id` alone, references to
files that do not exist and paths that leave the assets directory stay as written; a `?query` or `#fragment`
(`a.woff2?#iefix`, `sprite.svg#icon`) is kept. The sheet's own fingerprint is the hash of the rewritten text, so
changing a font changes the sheet's URL too. Sheets that reference each other in a cycle keep those references as
written. Sheets over 4 MiB are served as they are; rewritten sheets are kept in memory up to 8 MiB in total and
recomputed beyond that.

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

### Page-aware functions

`site.ViewFuncs` registers functions that know the page being rendered. The function runs on every render with the
page's `*site.View` and returns the template functions, which close over the view:

```go
site.ViewFuncs(h, func(v *site.View) template.FuncMap {
	return template.FuncMap{
		"related": func(n int) ([]*site.Item, error) {
			return v.List("post", map[string]any{"limit": n, "where[category][eq]": v.Query("category")})
		},
		"isHere": func(path string) bool { return v.Path == path },
	}
})
```

Inside, a view offers what templates have: `List` and `Get` (`items` and `item`), `Query`, `QueryValues`, `Nav`,
`Breadcrumbs`, `Link`, `Setting`, `T`, `Client()` and the fields (`Path`, `Locale`, `Item`…). Reads tag the page like
the template helpers do. Names follow the rules of `site.Funcs` and share their namespace. The function is also
called once at registration with an empty view to learn the names, so build closures there and do not call the
view's methods. Rendering cost is unchanged when none is registered; with one, the function runs each time a
template set is prepared for a render.

## The request in templates

The view has the page's channel-relative URL as `.Path` (`/en/blog/page/2`; no query), and the request's query:

- `.Query "category"` is the first value of a query parameter, or "" (`.QueryValues "tag"` all of them, in order).
  Values are plain strings that `html/template` escapes like any other. The `preview` parameter is never readable.
- Block templates get the block as their data; `.Page` is the page's view, so a block reads `.Page.Path`,
  `.Page.Query "page"` or `.Page.Locale`. Partials default to the view; pass `.` when giving them a dict.

```html
{{range items "post" (dict "where[category][eq]" (.Query "category") "limit" 10)}}…{{end}}
<a {{if eq .Path "/en/blog"}}aria-current="page"{{end}} href="?category=news">News</a>
```

Rendered pages are cached by path. A page that never reads the query keeps one cache entry whatever the URL carries,
so `?utm_source=…` does not split it. A page that reads parameters is stored per variant: the path plus the values
of the parameters its renders read (the names are learned from renders; a template that later reads another
parameter re-keys the path). Bounds keep it from being flooded: at most 32 variants per path (the oldest makes
room), at most 8 values per parameter and 128 bytes per value; a request beyond them is rendered and not stored.
Publishing purges every variant by the page's tags. Previews are never cached. `site:check` and static exports
have no query (reads are empty) unless an export target brings one in its URL.

## Reading content in templates

These helpers read in process, as a public client of the channel (drafts when previewing), in the page's locale.
Everything they read tags the page, so publishing a listed item purges it (and the static export re-renders it).
Identical calls within one render ask the delivery API once.

- `items "post" (dict "limit" 4 "sort" "-slug" "where[category][eq]" "news")` returns the items (`[]*Item`, each with
  `.Path` so `{{link .}}` works). Dict values are strings, numbers, booleans or lists (repeated parameters); an
  unknown field or a 5xx answer is an error.
  Keys are the delivery query parameters verbatim, so boolean groups work as keys too:
  `(dict "where[or][0][featured]" true "where[or][1][category]" "blog" "where[not][kind]" "press")`. Facet counts
  (`facets`) are not available through `items`; read them with `query "/collections/post/items" (dict "facets" "category" "limit" 1)`.
- `item "id"` returns one item or nil. It takes the same kind of dict for `fields` and `locale`.
- `query "/tree" (dict "depth" 3)` returns the decoded JSON (`map[string]any`) of a delivery path (`/tree`,
  `/search`, …) when it answers 200, else nil; a 5xx answer is an error.
- `fields` (on `items`, `item` and `query "/tree"`, `"/search"`) keeps only some fields of the items:
  `items "post" (dict "fields" "title,image,seo.title")`. Use it for menus, cards and teasers, which would
  otherwise load, encode and decode whole documents with their blocks; with `/tree`, each page node gets those
  fields of its item as `fields`. A dot path keeps one member of a group, and an unknown field is an error. Links
  keep working: `items` and `item` also fetch the field a collection's detail route puts in its URL (`slug` for
  `/blog/{slug}`), and an item with a page keeps its page URL.
- `link "item:<id>"` (and `item:` links in rich text) works for references in any item the render read, not only
  the page's own: the links those items carry are merged in. An id nobody carries is read once (at most 64 per
  render, in the page's locale) and linked to its page or collection route; an unavailable item gives no URL.
- `partial "blocks/hero/wide.html" data` executes a template by a computed name (Go's `{{template}}` needs a
  constant) and returns its HTML; `data` defaults to the page's view. A missing template is an error in
  `site:check` and a logged, empty result when serving. Partials may nest up to 32 levels. `hasTemplate "name"`
  tells whether a template exists, for a fallback before calling `partial`.
- `blocks` also takes a blocks value read from another item (`{{blocks (index $other.Fields "body")}}`) and renders
  it read only (no editing markers in preview).

```html
{{range items "post" (dict "limit" 3 "sort" "-slug")}}<a href="{{link .}}">{{.Name}}</a>{{end}}
{{partial (printf "blocks/imageBoxes/%s.html" .Props.view) .}}
```

In `site:check` the reading helpers return empty results (no database). Go components use `v.Client()`, the same
client with `Get`, `Items` and `Item`.

## Rich text

`{{richtext .Fields.body}}` renders a rich text field (a Go component calls `v.RichText(doc)`). Text is escaped, links
are checked (http(s), mailto, relative, `#anchor`; `item:<id>#frag` resolves the item's URL on this site and appends
the fragment, and a link without a URL keeps its text), and images must be http(s) or root-relative. Alignment,
anchors (`id`), ordered list `start`, tight lists and `target: _blank` links render as stored.

Options go in a dict:

```html
{{richtext .Fields.body (dict "classes" (dict "paragraph" "lead" "link" "underline") "wrap" "div" "wrapClass" "prose")}}
```

- `classes` maps a node or mark type (`paragraph`, `heading`, `bold`, `callout`…) to class tokens (`A-Za-z0-9_ -`;
  anything else is dropped) added to every element of that type, after `rt-NAME` for custom types. A code block's
  classes go on `<pre>`.
- `wrap` (`div`, `section` or `article`) and `wrapClass` put one element around the output, only when it is not empty.

In Go: `v.RichText(doc, site.RichTextOptions{Classes: …, Wrap: "div", WrapClass: "prose"})`.

Custom nodes and marks declared in the schema's `richtext:` key render as their declared tag with `class="rt-NAME"`
and one `data-ATTR` per attribute, in declaration order (defaults applied, `bool` true as an empty attribute, `url`
values checked, everything escaped): `<aside class="rt-callout" data-variant="info">…</aside>`. Style them with CSS, or
replace the element with a template `richtext/<type>.html` (theme or project, like `blocks/<type>.html`), for a built-in
or a custom type alike:

```html
{{/* richtext/callout.html */}}
<aside class="callout callout-{{.Attrs.variant}} {{.Class}}" role="note">{{.Content}}</aside>
```

It receives `.Type`, `.Attrs` (the stored attributes of a built-in; the declared ones with defaults for a custom
type), `.Content` (the children, already rendered), `.Class` (the class string the options gave), `.Node` (the raw
document node) and `.View`. The templates are found once when the set loads, so documents without overrides pay
nothing for them. `site:check` renders every override over a sample node. A tight list item's paragraphs, a link
without a usable URL and an image with an unsafe source render no element, so they call no override.

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
- A route can own the channel's root even when the tree has a root page: reserve it with `reserved: [/]` (exactly `/`;
  `/*` is refused) and register `site.Handle(h, "GET /{$}", fn)`. A bare `"/"` pattern matches every path and is refused.
  The root page stays in the tree but the root URL is the route's.
- `Request` carries the HTTP request (`R`), the wildcards (`Params`), the query, and a content client: `Get`,
  `Items`, `Item` read the published content (drafts when previewing) in process and tag the page with what they
  read. `URL(path)` and `LinkTo(item)` build site URLs.
- `Response` renders `Template` (a file under `routes/`, or a component) with the full page context: layout,
  helpers, meta, navigation, translations, settings, preview. `Data` is `.Data` in the template, the wildcards
  are `.Params`. `Redirect` (302, or 303 after a non-GET) answers a redirect instead. `Status`, `Headers`, `Tags`
  and `Cacheable` are optional; the default is `Cache-Control: no-store`, and the in-process page cache never
  stores custom routes. Return `site.ErrNotFound` for the 404 page; other errors are a logged 500.
- A response sets exactly one of `Template`, `Partial`, `Body`, `JSON`, `XML` or `Redirect` (two is a 500 naming
  the route; `site:check` reports it for a `Sample`). Besides full pages, a route can answer raw bodies:
  `Partial` renders a template or component of the set without the layout (a "load more" fragment, or an XML
  template with `ContentType`) and with the same helpers and page context as `Template`; `Body` sends bytes as
  they are and needs `ContentType`; `JSON` and `XML` encode a value (`application/json`; `application/xml;
  charset=utf-8` with the XML prolog). `ContentType` overrides the default type. Raw answers keep the tags of
  everything the route read, `Cacheable`, `Tags` and the static export; a preview answer is never cached and
  only HTML answers get the preview CSP. `Cacheable` GET answers carry a strong `ETag` and answer
  `If-None-Match` with 304; `MaxAge` replaces the channel's `delivery.cache_ttl` as the shared-cache lifetime.

```go
site.Handle(h, "GET /feed.xml", func(r *site.Request) (*site.Response, error) {
	posts, _, err := r.Items("post", url.Values{"limit": {"20"}, "sort": {"-publishedAt"}})
	if err != nil {
		return nil, err
	}
	return &site.Response{Partial: "routes/feed.html", Data: posts, ContentType: "application/rss+xml; charset=utf-8",
		Cacheable: true, MaxAge: 15 * time.Minute}, nil // routes/feed.html is the RSS template, without layout
}).Export()

// a sitemap or a client-side listing: encode a value
return &site.Response{XML: sitemap, Cacheable: true}, nil // a struct with encoding/xml tags
return &site.Response{JSON: map[string]any{"items": posts}}, nil
// a "load more" fragment: the same template the page uses for one card list
return &site.Response{Partial: "partials/cards.html", Data: posts}, nil
return &site.Response{Body: []byte("User-agent: *\n"), ContentType: "text/plain; charset=utf-8"}, nil
```

  Templates are `html/template`: escaping is HTML's, which suits fragments; for an XML feed write the elements
  and let the template escape the values, or build the document with `XML`. `/feed.xml`-style routes (a path
  with a file extension) are exported as that file; add `.Export()` or `.Static(fn)`.
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

### Not found

A request no page, mounted item, collection route or custom route serves is answered by `404.html` (status 404). To
answer it yourself, register one hook per project:

```go
site.NotFound(h, func(r *site.Request) (*site.Response, error) {
	if to, ok := legacy[r.Path]; ok { // r.Path is channel-relative
		return &site.Response{Redirect: r.URL(to), Status: http.StatusMovedPermanently}, nil
	}
	if strings.HasPrefix(r.Path, "/discontinued/") {
		return &site.Response{Template: "routes/gone.html", Status: http.StatusGone}, nil
	}
	return nil, site.ErrNotFound // the ordinary 404 page
})
```

It returns the same `Response` as a custom route, with two differences: `Status` defaults to 404 (302 with `Redirect`),
and `ErrNotFound` or a nil response falls back to `404.html`. The hook also sees listing pages past the end and collection
items that do not exist; it never sees reserved paths, the answers of custom routes, previews of a missing item or the
static export's 404 file. Its answers are never stored by the page cache and are `no-store` for shared caches; reads
through `r` still tag the answer. Other errors are a logged 500. Redirects that are plain data (a moved page, a retired
URL) are better kept as `redirect` items, which delivery, exports and SDK frontends all see; use the hook for
rules (patterns, lookups, a status of your own).

Endpoints under `/api/x` (see [hooks](hooks.md#endpoints)) that want CDN caching can set the same headers with
`cachetag.SetHeaders(w.Header(), ttl, tags)` (`core/cachetag`): `Cache-Control` with `s-maxage`, plus `Surrogate-Key`
and `Cache-Tag` from tags such as `cachetag.Item(id)` or `cachetag.Collection("post")`, so purges reach them
(`cachetag.NoStore` as the ttl sends `no-store`).

## Forms

A custom route can store a form submission as content (access control, audit, export and review come with it)
and protect itself from abuse, without a module that captures the content service.

```yaml
# config/app.yaml
modules:
  site:
    submissions: [contactMessage]   # the types Request.Submit may create
    forms:                          # the spam checks (these are the defaults)
      honeypot: website_url
      min_fill: 3s                  # a form submitted sooner after it was rendered is refused; 0 turns it off
      max_age: 4h                   # a rendered form stays valid this long
```

```yaml
# schema: submissions are ordinary items; required fields are enforced
types:
  contactMessage:
    name: "{sender}"
    fields:
      sender:  { type: string, required: true }
      email:   { type: string, required: true }
      message: { type: text, required: true }
```

```go
site.Handle(h, "GET /contact", func(q *site.Request) (*site.Response, error) {
	return &site.Response{Template: "routes/contact.html", Title: "Contact"}, nil
})
site.Handle(h, "POST /contact", func(q *site.Request) (*site.Response, error) {
	_, err := q.Submit("contactMessage", map[string]any{
		"sender": q.R.PostFormValue("sender"), "email": q.R.PostFormValue("email"), "message": q.R.PostFormValue("message"),
	})
	switch {
	case err == nil: // also when the honeypot was filled: the bot is told it worked
		return &site.Response{Redirect: q.URL("/contact/thanks")}, nil // post/redirect/get: a reload does not resubmit
	case site.FieldErrors(err) != nil:
		return &site.Response{Template: "routes/contact.html", Status: 422, Data: site.FieldErrors(err)}, nil
	case errors.Is(err, site.ErrTooFast), errors.Is(err, site.ErrFormInvalid):
		return &site.Response{Template: "routes/contact.html", Status: 400, Data: map[string]string{"form": "Please try again."}}, nil
	}
	return nil, err
}).Limit(site.Limit{PerClient: 5, Total: 200, Window: time.Hour})
```

```html
<!-- site/templates/routes/contact.html -->
{{define "content"}}
<form method="post" action="{{.Path}}">
  {{formGuard}}
  <label>Name <input name="sender" required></label>
  <label>Email <input name="email" type="email" required></label>
  <label>Message <textarea name="message" required></textarea></label>
  {{with .Data}}<p class="error">{{index . "sender"}} {{index . "email"}} {{index . "message"}} {{index . "form"}}</p>{{end}}
  <button>Send</button>
</form>
{{end}}
```

- **Submit.** `q.Submit(type, fields)` creates a draft of a type listed in `modules.site.submissions` and returns its
  id; it never publishes. The route chooses `fields` (never pass the raw form). Required fields are enforced, and a
  problem is a `*content.ValidationError`; `site.FieldErrors(err)` turns it into a path to message map for
  re-rendering the form. A type that is not listed fails with `site.ErrNotAllowed`. Only routes that handle POST,
  PUT, PATCH or DELETE may submit. The item is created with the actor `system:site:<route path>` (`created_by`),
  and its audit entry records the channel and the client address (`http.trusted_proxies` decides which address that
  is), so editors can tell submissions from edits. Give editors a role with `content.read` on the type.
- **Cross-site posts** are refused by the custom-route rules above (`Origin` and `Sec-Fetch-Site`); do not use
  `.AllowCrossSite()` on a form route.
- **Spam.** `{{formGuard}}` renders a hidden input with a signed timestamp (an HMAC under a key derived from
  `delivery.preview_secret`, or `manage.token` in dev, so every node agrees) and a honeypot field hidden with the
  `hidden` attribute. `Submit` (or `q.CheckForm()` for routes that do something else with the form, such as sending
  mail) answers `ErrFormInvalid` for a missing, forged or expired token, `ErrTooFast` below `min_fill`, and drops a
  submission that filled the honeypot: `Submit` returns an empty id and no error, so answer as for a success. A
  token proves only that a form was rendered a while ago, not who got it; a bot that loads the form first passes
  it. Put the rate limit below, and a CAPTCHA at your edge if you need more. Render forms in custom routes, or
  keep their pages out of shared caches: a cached token goes stale after `max_age` and is shared by every visitor.
- **Rate limit.** `.Limit(site.Limit{PerClient, Total, Window})` counts the route's non-GET requests per client
  address (IPv6 by /64) and for all clients, in one fixed window kept in the `site_limit` table, so servers that
  share the database share the count. A request over a limit is answered 429 with `Retry-After`. Run
  `hydrogen migrate` after updating (the module has its own migration); the `site.limits` job removes ended
  windows every 10 minutes. Windows are fixed: up to twice the limit can pass around a boundary. Every counted
  request is one small write, so a limit costs a transaction per request on that route; routes without a limit
  cost nothing.

## Components and templ

A template name can be backed by a Go component instead of a file. `site.Renderer` has exactly the method set of
`templ.Component`, so [templ](https://templ.guide) components plug in without hydrogen depending on templ:

```go
site.Component(h, "routes/colophon.html", func(v *site.View) site.Renderer { return colophon(v) })
```

A component registered under a page-like name (`pages/…`, `collections/…`, `routes/…`, `404.html`) renders the
whole document and wins over a file of that name; under `blocks/<type>.html` it renders that block (`v.Block`).
Templates can render any component with `{{component "name"}}`. `View` has the helpers templates have as methods:
`T`, `Meta`, `Nav`, `Breadcrumbs`, `Asset`, `Link`, `Setting`, `Edit`, `Blocks`, `RichText`, and
`Partial(name, data...)`, which executes a template of the set like `{{partial}}`.

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
