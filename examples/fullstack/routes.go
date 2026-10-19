package main

import (
	"net/url"
	"strings"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/modules/site"

	"github.com/hydrogencms/hydrogen/examples/fullstack/theme"
)

// result is one search hit as routes/search.html shows it.
type result struct {
	Name    string
	URL     string
	Excerpt string
}

type searchData struct {
	Query   string
	Results []result
}

// register adds the project's theme, custom routes and component to the site module.
func register(h *hydrogen.Builder) {
	site.UseTheme(h, theme.Theme())

	// /search sits on a path the channels reserve (config/channels.yaml), so the page tree never routes it.
	site.Handle(h, "GET /search", search).Sample(func() *site.Response {
		return &site.Response{Template: "routes/search.html", Title: "Search",
			Data: searchData{Query: "sample", Results: []result{{Name: "A page", URL: "/sample", Excerpt: "An excerpt"}}}}
	})

	// /colophon is one fixed page of a static export, rendered by a templ component.
	site.Handle(h, "GET /colophon", func(*site.Request) (*site.Response, error) {
		return &site.Response{Template: "routes/colophon.html", Title: "Colophon", Cacheable: true}, nil
	}).Export().Sample(func() *site.Response {
		return &site.Response{Template: "routes/colophon.html", Title: "Colophon"}
	})
	site.Component(h, "routes/colophon.html", func(v *site.View) site.Renderer { return colophon(v) })
}

func assetURL(v *site.View, name string) string {
	u, _ := v.Asset(name)
	return u
}

func siteName(v *site.View) string {
	if s, ok := v.Setting("siteName").(string); ok && s != "" {
		return s
	}
	return v.T("home")
}

// search asks the delivery API's search first (it needs types with `search:` in the schema) and falls back to
// scanning the pages and posts, so the example works with the shared starter schema.
func search(r *site.Request) (*site.Response, error) {
	q := strings.TrimSpace(r.Query.Get("q"))
	data := searchData{Query: q}
	if q != "" {
		var found struct {
			Items []*site.Item `json:"items"`
		}
		status, err := r.Get("/search", url.Values{"q": {q}, "limit": {"20"}}, &found)
		if err != nil {
			return nil, err
		}
		items := found.Items
		if status != 200 {
			if items, err = scan(r, q); err != nil {
				return nil, err
			}
		}
		for _, it := range items {
			if u := r.LinkTo(it); u != "" {
				ex, _ := it.Fields["excerpt"].(string)
				data.Results = append(data.Results, result{Name: it.Name, URL: u, Excerpt: ex})
			}
		}
	}
	return &site.Response{Template: "routes/search.html", Title: "Search", Data: data}, nil
}

func scan(r *site.Request, q string) ([]*site.Item, error) {
	var out []*site.Item
	q = strings.ToLower(q)
	for _, typ := range []string{"page", "landing", "post"} {
		items, _, err := r.Items(typ, url.Values{"limit": {"100"}})
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			ex, _ := it.Fields["excerpt"].(string)
			if strings.Contains(strings.ToLower(it.Name+" "+ex), q) {
				out = append(out, it)
			}
		}
	}
	return out, nil
}
