package tests

import (
	"fmt"
	"strings"
	"testing"
)

func TestDeliveryFieldProjection(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		ann := n.create("person", "ann", "en", `{"fullName":"Ann"}`, true)
		bob := n.create("person", "bob", "en", `{"fullName":"Bob"}`, true)
		art := n.create("article", "hello", "en", fmt.Sprintf(`{"title":"Hello","rank":3,"author":%q,"related":[%q]}`, ann, bob), true)
		n.create("settings", "settings", "en", `{"siteName":"Acme"}`, true)

		// The envelope stays, fields holds the selection.
		r := n.wantStatus("/items/"+art.String()+"?channel=web&fields=title", 200)
		item := asMap(r.json()["item"])
		for _, k := range []string{"id", "type", "key", "locale", "version", "publishedAt", "name"} {
			if item[k] == nil {
				t.Errorf("envelope key %s missing: %v", k, item)
			}
		}
		if fields := asMap(item["fields"]); len(fields) != 1 || fields["title"] != "Hello" {
			t.Fatalf("fields = %v", fields)
		}
		whole := n.wantStatus("/items/"+art.String()+"?channel=web", 200)
		if r.Header().Get("Etag") == whole.Header().Get("Etag") {
			t.Error("a projection has the ETag of the whole item")
		}
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "i:"+art.String()) {
			t.Errorf("tags = %q", r.Header().Get("Surrogate-Key"))
		}

		// include follows the references of the selected fields only.
		r = n.wantStatus("/items/"+art.String()+"?channel=web&fields=title&include=1", 200)
		if r.json()["included"] != nil {
			t.Errorf("included without a selected reference: %v", r.json()["included"])
		}
		r = n.wantStatus("/items/"+art.String()+"?channel=web&fields=author&include=1", 200)
		inc := asMap(r.json()["included"])
		if len(inc) != 1 || inc[ann.String()] == nil {
			t.Fatalf("included = %v, want only the author", inc)
		}
		// Included items are whole.
		deq(t, "included name", field(asMap(inc[ann.String()]), "fields", "fullName"), "Ann")
		if tags := r.Header().Get("Surrogate-Key"); strings.Contains(tags, "i:"+bob.String()) {
			t.Errorf("an unselected reference was looked up: %q", tags)
		}

		// Listings, by key and singletons.
		l := n.wantStatus("/collections/article/items?channel=web&fields=rank,title&sort=rank", 200)
		items, _ := l.json()["items"].([]any)
		if len(items) != 1 || len(asMap(field(asMap(items[0]), "fields"))) != 2 {
			t.Fatalf("listed = %v", items)
		}
		if f := asMap(field(n.wantStatus("/items/by-key/hello?channel=web&fields=rank", 200).json(), "item", "fields")); len(f) != 1 {
			t.Errorf("by key = %v", f)
		}
		if f := asMap(field(n.wantStatus("/singletons/settings?channel=web&fields=siteName", 200).json(), "item", "fields")); f["siteName"] != "Acme" {
			t.Errorf("singleton = %v", f)
		}
		// By id, any type of the channel may have the field; an item whose type lacks it shows no fields.
		if f := asMap(field(n.wantStatus("/items/"+ann.String()+"?channel=web&fields=title", 200).json(), "item", "fields")); len(f) != 0 {
			t.Errorf("person with fields=title = %v", f)
		}
		// Empty is no projection.
		if f := asMap(field(n.wantStatus("/items/"+art.String()+"?channel=web&fields=", 200).json(), "item", "fields")); len(f) != 4 {
			t.Errorf("fields= = %v", f)
		}

		for _, p := range []string{
			"/collections/article/items?channel=web&fields=fullName", // another type's field
			"/collections/article/items?channel=web&fields=title.x",  // not a group
			"/items/" + art.String() + "?channel=web&fields=nope",
			"/items/" + art.String() + "?channel=web&fields=title&fields=rank",
			"/singletons/settings?channel=web&fields=title",
			"/items/" + art.String() + "?channel=people&fields=title", // the channel shows no article
		} {
			deq(t, p, field(n.wantStatus(p, 400).json(), "error", "code"), "bad_request")
		}
	})
}

func TestDeliveryFieldProjectionSkipsUnselectedAssets(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		img, _ := n.image("cat.png", 3, `{"alt":"A cat"}`, true)
		art := n.create("article", "a", "en", fmt.Sprintf(`{"title":"A","hero":%q}`, img), true)

		r := n.wantStatus("/collections/article/items?channel=web&fields=title", 200)
		if tags := r.Header().Get("Surrogate-Key"); strings.Contains(tags, "i:"+img.String()) {
			t.Errorf("an unselected asset was looked up: %q", tags)
		}
		r = n.wantStatus("/items/"+art.String()+"?channel=web&fields=hero", 200)
		deq(t, "expanded", field(r.json(), "item", "fields", "hero", "fields", "alt"), "A cat")
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "i:"+img.String()) {
			t.Errorf("tags = %q", r.Header().Get("Surrogate-Key"))
		}
	})
}
