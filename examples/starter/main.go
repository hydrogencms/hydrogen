// Command starter is the page-builder starter project: a block-based schema (hero, rich text, cards, callout,
// image and shared blocks), English and German web channels, and a committed fixture of pages and posts
// (fixture/) that the frontend starters and the adapter conformance suite run against. Import it with
// `hydrogen import fixture`.
package main

import (
	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func main() { hydrogen.Main(configure) }

func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), postgres.Module(), pages.Module())
}
