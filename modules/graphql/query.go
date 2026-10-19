package graphql

import (
	"strconv"

	gql "github.com/graphql-go/graphql"
)

// queryType builds the root Query: per collection a getter and a list, per singleton a getter, item, and the
// route and search fields when the delivery API has those routes.
func (b *builder) queryType(lists map[string]*gql.Object) (*gql.Object, error) {
	taken := map[string]bool{"item": true, "route": true, "search": true}
	pick := func(name string) string {
		if !taken[name] {
			taken[name] = true
			return name
		}
		for i := 2; ; i++ {
			if n := name + strconv.Itoa(i); !taken[n] {
				taken[n] = true
				return n
			}
		}
	}
	fields := gql.Fields{}
	getters := map[string]string{}
	for _, t := range b.types {
		getters[t.Name] = pick(t.Name)
	}
	for _, t := range b.types {
		obj := b.objects[t.Name]
		name := getters[t.Name]
		if t.Singleton {
			fields[name] = &gql.Field{Type: obj, Description: "The " + obj.Name() + " singleton.",
				Args: gql.FieldConfigArgument{"locale": localeArg()}, Resolve: b.resolveSingleton(t.Name, obj.Name())}
			continue
		}
		fields[name] = &gql.Field{Type: obj, Description: "One " + obj.Name() + " by id or key.",
			Args: gql.FieldConfigArgument{
				"id":     {Type: gql.ID, Description: "The item's id."},
				"key":    {Type: gql.String, Description: "The item's key."},
				"locale": localeArg(),
			}, Resolve: b.resolveGet(t.Name, obj.Name())}
	}
	for _, t := range b.types {
		if t.Singleton {
			continue
		}
		l := lists[t.Name]
		fields[pick(getters[t.Name]+"List")] = &gql.Field{Type: l, Description: "A page of " + b.objects[t.Name].Name() + " items.",
			Args: gql.FieldConfigArgument{
				"where":  {Type: b.json, Description: `Filters on indexed fields: {"title": {"eq": "x"}}.`},
				"sort":   {Type: gql.String, Description: "Comma-separated fields, - for descending."},
				"limit":  {Type: gql.Int},
				"offset": {Type: gql.Int},
				"locale": localeArg(),
			}, Resolve: b.resolveList(t.Name, l.Name())}
	}
	fields["item"] = &gql.Field{Type: b.item, Description: "Any item by id.",
		Args: gql.FieldConfigArgument{"id": {Type: gql.NewNonNull(gql.ID)}, "locale": localeArg()}, Resolve: b.resolveAny()}
	if b.caps.route {
		fields["route"] = b.routeField()
	}
	if b.caps.search {
		fields["search"] = b.searchField()
	}
	return gql.NewObject(gql.ObjectConfig{Name: "Query", Fields: fields}), nil
}

func localeArg() *gql.ArgumentConfig {
	return &gql.ArgumentConfig{Type: gql.String, Description: "Show the item in this locale (a language of the channel)."}
}

func (b *builder) routeField() *gql.Field {
	itemOut := b.item
	b.setMeta("Route", "item", fieldMeta{})
	r := gql.NewObject(gql.ObjectConfig{Name: "Route", Description: "Where a URL leads.", Fields: gql.Fields{
		"kind":       {Type: gql.NewNonNull(gql.String), Description: "page, item, redirect or not_found.", Resolve: routeKey("kind")},
		"item":       {Type: itemOut, Resolve: routeKey("item")},
		"status":     {Type: gql.Int, Resolve: routeKey("status")},
		"location":   {Type: gql.String, Description: "The target of a redirect.", Resolve: routeKey("location")},
		"canonical":  {Type: gql.String, Resolve: routeKey("canonical")},
		"alternates": {Type: b.json, Resolve: routeKey("alternates")},
	}})
	return &gql.Field{Type: r, Description: "Locate a URL: the page or item it shows, a redirect, or not found.",
		Args: gql.FieldConfigArgument{"url": {Type: gql.NewNonNull(gql.String)}}, Resolve: b.resolveRoute()}
}

func routeKey(k string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		r, ok := p.Source.(*routeVal)
		if !ok {
			return nil, nil
		}
		switch k {
		case "kind":
			return r.kind, nil
		case "item":
			if r.item == nil {
				return nil, nil
			}
			return r.item, nil
		case "status":
			if r.status == 0 {
				return nil, nil
			}
			return r.status, nil
		case "location":
			return nilIfEmpty(r.location), nil
		case "canonical":
			return nilIfEmpty(r.canonical), nil
		}
		return r.alternates, nil
	}
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (b *builder) searchField() *gql.Field {
	b.setMeta("SearchResult", "items", fieldMeta{})
	res := gql.NewObject(gql.ObjectConfig{Name: "SearchResult", Fields: gql.Fields{
		"items":  {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(b.item))), Resolve: searchKey("items")},
		"hits":   {Type: b.json, Description: "Per item: score and highlights, parallel to items.", Resolve: searchKey("hits")},
		"total":  {Type: gql.Int, Resolve: searchKey("total")},
		"facets": {Type: b.json, Resolve: searchKey("facets")},
	}})
	return &gql.Field{Type: res, Description: "Full-text search over the channel's searchable collections.",
		Args: gql.FieldConfigArgument{
			"q":      {Type: gql.NewNonNull(gql.String)},
			"types":  {Type: gql.NewList(gql.NewNonNull(gql.String)), Description: "Collections to search; default all searchable ones."},
			"where":  {Type: b.json},
			"facets": {Type: gql.NewList(gql.NewNonNull(gql.String)), Description: "Indexed text fields to count over all matches."},
			"sort":   {Type: gql.String},
			"limit":  {Type: gql.Int},
			"offset": {Type: gql.Int},
			"locale": localeArg(),
		}, Resolve: b.resolveSearch()}
}

func searchKey(k string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		s, ok := p.Source.(*searchVal)
		if !ok {
			return nil, nil
		}
		switch k {
		case "items":
			return s.items, nil
		case "hits":
			return s.hits, nil
		case "total":
			return s.total, nil
		}
		return s.facets, nil
	}
}
