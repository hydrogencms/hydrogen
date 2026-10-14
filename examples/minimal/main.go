// Command minimal is the smallest Hydrogen project: core plus the two database adapters.
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
