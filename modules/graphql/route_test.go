package graphql

import (
	"context"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/pages"
)

const routeSchema = `
types:
  home:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
`

const routeChannels = `
languages: { en: {} }
channels:
  web: { languages: [en], match: { host: a.test } }
`

func TestRoute(t *testing.T) {
	e := newEnv(t, envOpts{schema: routeSchema, channels: routeChannels, app: "modules:\n  pages: { sites: [main] }\n", mods: []kernel.Module{pages.Module()}})
	home := e.create("home", "", "en", `{"title":"Home"}`, true)
	svc, ok := kernel.LookupService[*pages.Service](e.app.Services())
	if !ok {
		t.Fatal("no pages service")
	}
	if _, err := svc.Place(context.Background(), "main", pages.Place{Item: home}); err != nil {
		t.Fatal(err)
	}
	r := e.post("web", `{ route(url: "https://a.test/") { kind status location canonical item { id type ... on Home { title } } } }`, nil)
	r.noErrors(t)
	eq(t, "kind", path(r.data(), "route", "kind"), "page")
	eq(t, "status", path(r.data(), "route", "status"), 200)
	eq(t, "item", path(r.data(), "route", "item", "title"), "Home")
	eq(t, "item id", path(r.data(), "route", "item", "id"), home.String())

	r = e.post("web", `{ route(url: "https://a.test/nothing") { kind status item { id } } }`, nil)
	r.noErrors(t)
	eq(t, "not found", path(r.data(), "route", "kind"), "not_found")
	eq(t, "404", path(r.data(), "route", "status"), 404)
	if path(r.data(), "route", "item") != nil {
		t.Error("item on not found")
	}
	if !strings.Contains(e.mod.sdlOf(t, "web"), "route(url: String!): Route") {
		t.Error("route field missing")
	}
}
