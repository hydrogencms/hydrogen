// Command fullstack is the starter project served by one binary: the admin (/admin), the APIs (/api) and the
// website, rendered by the site module's Go templates. It uses examples/starter's schema and fixture
// (SCHEMA_DIR defaults to ../starter/schema), so `hydrogen import ../starter/fixture` fills it.
package main

import (
	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/site"
	"github.com/hydrogencms/hydrogen/modules/siteexport"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func main() { hydrogen.Main(configure) }

func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), postgres.Module(), pages.Module(), site.Module(), siteexport.Module())
	register(h)
}
