package tests

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
)

func ranks(t *testing.T, r map[string]any) []float64 {
	t.Helper()
	var out []float64
	for _, it := range r["items"].([]any) {
		out = append(out, field(it.(map[string]any), "fields", "rank").(float64))
	}
	return out
}

func facetsOf(t *testing.T, r map[string]any, name string) string {
	t.Helper()
	fs, _ := r["facets"].(map[string]any)
	var out []string
	vals, _ := fs[name].([]any)
	for _, v := range vals {
		m := v.(map[string]any)
		out = append(out, fmt.Sprintf("%v=%v", m["value"], m["count"]))
	}
	return strings.Join(out, " ")
}

func TestDeliveryGroupsAndFacets(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		n.create("article", "", "en", `{"title":"A1","rank":1,"category":"news","featured":true}`, true)
		a2 := n.create("article", "", "en", `{"title":"A2","rank":2,"category":"news"}`, true)
		n.create("article", "", "en", `{"title":"A3","rank":3,"category":"blog","featured":true}`, true)
		n.create("article", "", "en", `{"title":"A4","rank":4}`, true)
		n.create("article", "", "fr", `{"title":"Seulement fr","rank":5,"category":"news"}`, true)
		n.create("article", "", "en", `{"title":"Unpublished","rank":6,"category":"news"}`, false)
		// A draft change does not reach delivery until it is published.
		n.update(a2, "en", `{"category":"blog"}`)

		list := func(query string) map[string]any {
			t.Helper()
			return n.wantStatus("/collections/article/items?channel=web&sort=rank&"+query, 200).json()
		}
		eqRanks := func(what, query string, want ...float64) {
			t.Helper()
			if got := ranks(t, list(query)); !slices.Equal(got, want) {
				t.Errorf("%s: %v, want %v", what, got, want)
			}
		}

		eqRanks("or across fields", "where[or][0][category]=blog&where[or][1][featured]=true", 1, 3)
		eqRanks("or of and groups", "where[or][0][category]=news&where[or][0][featured]=true&where[or][1][rank]=4", 1, 4)
		eqRanks("or and plain", "where[or][0][category]=blog&where[or][1][featured]=true&where[rank][gt]=1", 3)
		eqRanks("not keeps items without the field", "where[not][category]=news", 3, 4)
		eqRanks("not featured", "where[not][featured]=true&where[rank][gte]=2", 2, 4)
		eqRanks("not of an and group", "where[not][category]=news&where[not][featured]=true", 2, 3, 4)
		eqRanks("not of several groups", "where[not][0][category]=news&where[not][1][featured]=true", 4)
		eqRanks("not in", "where[not][rank][in]=1,2", 3, 4)

		r := n.wantStatus("/collections/article/items?channel=web&sort=rank&total=true&where[not][category]=blog", 200).json()
		deq(t, "total with not", r["total"], float64(3))

		r = list("facets=category")
		deq(t, "facets", facetsOf(t, r, "category"), "news=2 blog=1")
		if len(r["items"].([]any)) != 4 {
			t.Errorf("facets must not change the items: %v", ranks(t, r))
		}
		r = list("facets=category&limit=1&where[or][0][featured]=true&where[or][1][rank]=4")
		deq(t, "facets ignore paging and follow filters", facetsOf(t, r, "category"), "blog=1 news=1")
		deq(t, "paging kept", fmt.Sprint(ranks(t, r)), "[1]")
		r = list("facets=category&where[not][category]=news")
		deq(t, "facets of not", facetsOf(t, r, "category"), "blog=1")
		r = n.wantStatus("/collections/article/items?channel=web_de&facets=category", 200).json()
		deq(t, "facets in another channel", facetsOf(t, r, "category"), "news=2 blog=1")
		r = n.wantStatus("/collections/article/items?channel=fr_only&facets=category", 200).json()
		deq(t, "facets only count the channel's items", facetsOf(t, r, "category"), "news=1")

		n.publish(a2)
		n.drain()
		r = list("facets=category")
		deq(t, "published change", facetsOf(t, r, "category"), "blog=2 news=1")
		if _, ok := list("")["facets"]; ok {
			t.Error("facets are only present when asked for")
		}

		for _, q := range []string{
			"facets=rank", "facets=nope", "facets=category,", "facets=a,b,c,d,e,f",
			"where[or][category]=news", "where[or][0][nope]=x", "where[not][rank]=x", "where[or][0][category][like]=x",
			"where[or][33][category]=x", "where[or][0]=x", "where[or][0][rank][in]=" + strings.Repeat("1,", 1001) + "1",
		} {
			if rr := n.get("/collections/article/items?channel=web&" + q); rr.Code != 400 {
				t.Errorf("%s: %d %s", url.QueryEscape(q), rr.Code, rr.Body)
			}
		}

		// Groups are not available in search.
		if rr := n.get("/search?channel=web&type=article&q=a&where[or][0][rank]=1"); rr.Code != 400 {
			t.Errorf("search with a group: %d %s", rr.Code, rr.Body)
		}
	})
}
