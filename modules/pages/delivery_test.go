package pages

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
)

func TestPageTags(t *testing.T) {
	got := pageTags("page.changed", json.RawMessage(`{"site":"corp","node":null}`))
	if !slices.Equal(got, []string{"r:corp", "nav:corp"}) {
		t.Errorf("tags = %v", got)
	}
	for _, c := range []struct{ topic, payload string }{
		{"item.published", `{"site":"corp"}`}, {"page.changed", `{}`}, {"page.changed", `nope`},
	} {
		if got := pageTags(c.topic, json.RawMessage(c.payload)); got != nil {
			t.Errorf("%s %s: %v", c.topic, c.payload, got)
		}
	}
}

func TestNavNodes(t *testing.T) {
	wc := newWebChannel(&channel.Channel{Name: "c", Languages: []string{"en"}, Collections: []string{"section"},
		Options: map[string]any{optMatch: matcher{Host: "a.test", Prefix: "/en"}}}, "main")
	item := ids.UUID{1}
	linked := ids.UUID{2}
	nodes := []TreeNode{
		{Node: Node{Kind: KindPage, Item: &item}, Type: "section", Name: "A", Path: "/a", Status: "published", Nodes: []TreeNode{
			{Node: Node{Kind: KindPage}, Type: "section", Name: "A1", Path: "/a/1", Status: "changed"},
			{Node: Node{Kind: KindPage, Hidden: true}, Type: "section", Name: "hidden", Path: "/a/h", Status: "published"},
		}},
		{Node: Node{Kind: KindPage}, Type: "section", Name: "draft", Status: "draft"},
		{Node: Node{Kind: KindPage}, Type: "section", Name: "no path", Status: "published"},
		{Node: Node{Kind: KindPage}, Type: "landing", Name: "type not allowed", Path: "/l", Status: "published"},
		{Node: Node{Kind: KindMount, Mount: &MountSpec{Collection: "article"}}, Name: "mount", Path: "/m"},
		{Node: Node{Kind: KindLink, Link: &LinkSpec{Item: &linked}}, Name: "item link"},
		{Node: Node{Kind: KindLink, Link: &LinkSpec{Item: &item}}, Name: "unrouted link"},
		{Node: Node{Kind: KindLink, Link: &LinkSpec{}}, Name: "broken", Broken: true},
		{Node: Node{Kind: KindLink, Link: &LinkSpec{URL: "/x"}}, Name: "path link", Target: "/x"},
		{Node: Node{Kind: KindLink, Link: &LinkSpec{URL: "https://e.com"}}, Name: "url link", Target: "https://e.com"},
	}
	got := navNodes(wc, nodes, map[ids.UUID]string{linked: "/en/l"})
	var names []string
	for _, n := range got {
		names = append(names, n.Name)
	}
	if want := []string{"A", "item link", "path link", "url link"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if got[0].URL != "https://a.test/en/a" || len(got[0].Nodes) != 1 || got[0].Nodes[0].Name != "A1" {
		t.Errorf("page = %+v", got[0])
	}
	if got[2].URL != "https://a.test/en/x" || got[3].URL != "https://e.com" {
		t.Errorf("links = %+v %+v", got[2], got[3])
	}
}

func TestMatcherOverlaps(t *testing.T) {
	h := func(v string) *headerMatch { return &headerMatch{Name: "X-A", Value: v} }
	for _, c := range []struct {
		a, b matcher
		want bool
	}{
		{matcher{Host: "a.test"}, matcher{Host: "b.test"}, false},
		{matcher{Host: "a.test"}, matcher{Prefix: "/en"}, true},
		{matcher{Prefix: "/en"}, matcher{Prefix: "/en/x"}, true},
		{matcher{Prefix: "/en"}, matcher{Prefix: "/end"}, false},
		{matcher{Host: "a.test", Prefix: "/en"}, matcher{Host: "a.test", Prefix: "/en", Header: h("1")}, true},
		{matcher{Header: h("1")}, matcher{Header: h("2")}, false},
		{matcher{Header: h("1")}, matcher{Header: &headerMatch{Name: "X-B", Value: "2"}}, true},
	} {
		if got, rev := c.a.overlaps(c.b), c.b.overlaps(c.a); got != c.want || rev != c.want {
			t.Errorf("%+v vs %+v: %v/%v, want %v", c.a, c.b, got, rev, c.want)
		}
	}
}

func TestSetFieldsSkipsUnavailable(t *testing.T) {
	a, b := ids.UUID{1}, ids.UUID{2}
	nodes := []navNode{
		{Kind: KindPage, Item: &a, Nodes: []navNode{{Kind: KindPage, Item: &b}}},
		{Kind: KindMount},
	}
	setFields(nodes, map[ids.UUID]json.RawMessage{a: json.RawMessage(`{"title":"A"}`)})
	if string(nodes[0].Fields) != `{"title":"A"}` || nodes[0].Nodes[0].Fields != nil || nodes[1].Fields != nil {
		t.Errorf("fields: %+v", nodes)
	}
}
