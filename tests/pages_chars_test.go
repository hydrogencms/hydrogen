package tests

import (
	"errors"
	"testing"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

const pagesCharsSchema = `
types:
  home:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
  landing:
    page: true
    children: []
    fields:
      title: { type: string, translatable: true, required: true }
      slug:  { type: slug, from: title, translatable: true, allow: ".,", unique: true }
  article:
    name: "{title}"
    fields:
      title: { type: string, translatable: true, required: true }
      slug:  { type: slug, from: title, translatable: true, allow: ".,", unique: true }
`

func TestPagesSlugCharacters(t *testing.T) {
	runPages(t, pagesOpts{schema: pagesCharsSchema}, func(t *testing.T, e *pagesEnv) {
		_, root := e.placed("home", "Home", nil, nil)
		acme := e.create("landing", "en", `{"title":"Acme","slug":"acme,-inc","vanityPath":"/benelux-b.v"}`)
		e.mustPublish(acme)
		n, err := e.pages.Place(ctx, "main", pages.Place{Item: acme, Parent: &root})
		if err != nil {
			t.Fatal(err)
		}
		e.wantRoutes(acme, "en current /acme,-inc", "en alias /benelux-b.v")
		e.wantPage("en", "/acme,-inc", acme)
		if r := e.wantPage("en", "/benelux-b.v", acme); r.Path != "/acme,-inc" {
			t.Errorf("alias canonical path = %s", r.Path)
		}

		// A mounted collection keeps the dotted slug of its items.
		e.mount(n.ID, pages.MountSpec{Segment: "news", Collection: "article", Route: "{slug}"})
		a := e.article("en", `{"title":"v1","slug":"release-1.2"}`)
		e.wantRoutes(a, "en current /acme,-inc/news/release-1.2")
		if r := e.resolve("en", "/acme,-inc/news/release-1.2"); r.Kind != pages.ResolvedItem || *r.Item != a {
			t.Errorf("resolve item = %+v", r)
		}
		e.wantNotFound("en", "/acme,-inc/news/release-1-2")

		// Two items cannot claim the same mounted path: the slug is unique per collection.
		_, err = e.c.Create(ctx, content.CreateInput{Type: "article", Fields: doc(t, `{"title":"Dup","slug":"release-1.2"}`)})
		if !errors.Is(err, content.ErrInvalid) {
			t.Errorf("duplicate slug: %v", err)
		}
		// Malformed ones never reach a path.
		_, err = e.c.Create(ctx, content.CreateInput{Type: "article", Fields: doc(t, `{"title":"Bad","slug":"a..b"}`)})
		if !errors.Is(err, content.ErrInvalid) {
			t.Errorf("a..b: %v", err)
		}
		e.update(acme, "en", `{"vanityPath":"/a..b"}`)
		if got := validation(t, e.publish(acme)); len(got) != 1 || got[0] != "vanityPath:invalid_path" {
			t.Errorf("vanity a..b: %v", got)
		}
	})
}
