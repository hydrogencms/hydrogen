// Command headless is a project without the pages module: posts, authors and a settings singleton for a
// frontend that owns its URLs. The web channel turns an item into a preview link from its preview patterns, the
// app channel is a token-protected consumer; tested on SQLite and Postgres.
package main

import (
	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func main() { hydrogen.Main(configure) }

func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), postgres.Module())
}
