// Command multisite is the H3 example project: two page sites, five web channels (prefixes on one host, a
// templated market on another host, a second site on its own host) and a token-protected app channel, tested
// through the management API on SQLite and Postgres.
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
