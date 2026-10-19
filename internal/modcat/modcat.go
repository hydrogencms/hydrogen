// Package modcat is the catalog of first-party modules the hydrogen CLI can add to a project.
package modcat

import "strings"

const (
	Core      = "github.com/hydrogencms/hydrogen"
	modPrefix = Core + "/modules/"
)

type Module struct {
	Name     string // catalog name, as written in hydrogen.yaml
	Package  string // Go package with func Module() kernel.Module
	GoModule string // Go module containing it when it is its own module (needs go get); empty = in the core module
	Summary  string
	Docs     string // docs page relative to the repository root, empty when there is none
	Config   string // config key in config/app.yaml, empty when the module has no settings
}

// Subdir is where a separate module lives inside a hydrogen checkout.
func (m Module) Subdir() string { return strings.TrimPrefix(m.GoModule, Core+"/") }

var catalog = []Module{
	{Name: "sqlite", Package: modPrefix + "sqlite", GoModule: modPrefix + "sqlite", Summary: "SQLite database adapter (pure Go, no cgo)"},
	{Name: "postgres", Package: modPrefix + "postgres", GoModule: modPrefix + "postgres", Summary: "PostgreSQL database adapter"},
	{Name: "pages", Package: modPrefix + "pages", Summary: "page tree per site, routes with history, redirects", Config: "modules.pages"},
	{Name: "site", Package: modPrefix + "site", Summary: "server-rendered website for web channels with render: go", Config: "modules.site"},
	{Name: "site-export", Package: modPrefix + "siteexport", Summary: "static export of the site module: files, manifest, redirects, incremental export, publishers", Docs: "docs/site-export.md", Config: "modules.site-export"},
	{Name: "site-proxy", Package: modPrefix + "siteproxy", Summary: "serves a JS frontend (static export or supervised SSR process) behind the binary", Docs: "docs/site-proxy.md", Config: "modules.site-proxy"},
	{Name: "overlays", Package: modPrefix + "overlays", Summary: "locale overlays: a language inherits fields from another", Docs: "docs/locales.md"},
	{Name: "images", Package: modPrefix + "images", Summary: "image presets, focal points and variants", Docs: "docs/images.md", Config: "modules.images"},
	{Name: "wasmhooks", Package: modPrefix + "wasmhooks", GoModule: modPrefix + "wasmhooks", Summary: "content hooks as sandboxed WebAssembly programs (Go, Rust, TypeScript, …)", Docs: "docs/wasm-hooks.md", Config: "modules.wasmhooks"},
	{Name: "graphql", Package: modPrefix + "graphql", GoModule: modPrefix + "graphql", Summary: "read-only GraphQL delivery API generated from the schema", Docs: "docs/graphql.md", Config: "modules.graphql"},
	{Name: "meilisearch", Package: modPrefix + "meilisearch", Summary: "Meilisearch as the search engine", Docs: "docs/meilisearch.md", Config: "modules.meilisearch"},
}

// All returns the catalog in display order.
func All() []Module { return append([]Module(nil), catalog...) }

func Lookup(name string) (Module, bool) {
	for _, m := range catalog {
		if m.Name == name {
			return m, true
		}
	}
	return Module{}, false
}

// ByPackage finds the catalog module whose package is path.
func ByPackage(path string) (Module, bool) {
	for _, m := range catalog {
		if m.Package == path {
			return m, true
		}
	}
	return Module{}, false
}
