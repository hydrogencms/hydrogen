// Command content is the H1 example project: every field type, a module schema transform (seo), the pages module, a typed
// project hook and a content migration, served through the management API.
package main

import (
	"strings"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"

	gen "github.com/hydrogencms/hydrogen/examples/content/gen/content"
	"github.com/hydrogencms/hydrogen/examples/content/seo"
)

func main() { hydrogen.Main(configure) }

func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), postgres.Module(), pages.Module(), seo.Module())
	gen.ArticleType.BeforeSave(h, readingTime)
	h.Migration(splitName)
}

// readingTime estimates reading minutes from the summary at 200 words a minute.
func readingTime(_ *hooks.Context, a *gen.Article) error {
	words := 0
	if a.Summary != nil {
		words = len(strings.Fields(*a.Summary))
	}
	minutes := int64(max(1, (words+199)/200))
	a.ReadingMinutes = &minutes
	return nil
}

// splitName moves person.fullName (schema_v1) into firstName and lastName.
var splitName = content.Migration{
	Name:    "0001_person_split_name",
	Type:    "person",
	Changes: []string{"person.fullName"},
	Up: func(d *content.MigrationDoc) error {
		full, _ := d.Fields["fullName"].(string)
		delete(d.Fields, "fullName")
		first, last, _ := strings.Cut(strings.TrimSpace(full), " ")
		d.Fields["firstName"] = first
		if last != "" {
			d.Fields["lastName"] = last
		}
		return nil
	},
}
