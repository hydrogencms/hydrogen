// Package theme is the example's theme: it builds on the embedded starter theme and replaces only the footer
// (a search form and a colophon link) and a few translations. A theme package like this one can be shared
// between projects; the project's own site/ directory still layers on top.
package theme

import (
	"embed"

	"github.com/hydrogencms/hydrogen/modules/site"
)

//go:embed all:templates all:translations
var files embed.FS

// Theme returns the theme; register it with site.UseTheme and select it with modules.site.theme: fullstack.
func Theme() site.Theme {
	return site.Theme{Name: "fullstack", FS: files, Base: "starter"}
}
