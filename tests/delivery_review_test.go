package tests

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
)

func TestDeliveryRepeatedParametersAreRejected(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "", "en", `{"title":"Hi"}`, false)
		n.update(art, "de", `{"title":"Hallo"}`)
		n.publish(art)
		p := "/items/" + art.String() + "?channel=web"
		// Both orders are errors: neither can be cached, so neither poisons the other.
		n.wantStatus(p+"&locale=de&locale=en", 400)
		n.wantStatus(p+"&locale=en&locale=de", 400)
		for _, q := range []string{"include=1&include=2", "channel=web&channel=web", "locale=en&%6Cocale=de"} {
			n.wantStatus(p+"&"+q, 400)
		}
		for _, q := range []string{"limit=1&limit=2", "offset=0&offset=1", "q=a&q=b", "sort=rank&sort=-rank", "total=true&total=true"} {
			n.wantStatus("/collections/article/items?channel=web&"+q, 400)
		}
		if e, _ := n.app.Services().Delivery.CacheStats(); e != 0 {
			t.Errorf("%d cache entries after rejected requests", e)
		}
		deq(t, "locale de", field(n.wantStatus(p+"&locale=de", 200).json(), "item", "locale"), "de")
		deq(t, "locale en", field(n.wantStatus(p+"&locale=en", 200).json(), "item", "locale"), "en")
	})
}

func TestDeliveryTagsItemsThatWereNotAvailable(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		person := n.create("person", "", "en", `{"fullName":"Pat"}`, false) // not published yet
		art := n.create("article", "", "en", fmt.Sprintf(`{"title":"A","author":%q}`, person.String()), true)
		path := "/items/" + art.String() + "?channel=web&include=1"
		r := n.wantStatus(path, 200)
		if !strings.Contains(r.Header().Get("Surrogate-Key"), "i:"+person.String()) {
			t.Errorf("tags = %q", r.Header().Get("Surrogate-Key"))
		}
		if r.json()["included"] != nil {
			t.Error("an unpublished item was included")
		}
		n.publish(person)
		n.drain()
		eventually(t, "the purge", func() bool {
			return field(n.get(path).json(), "included", person.String(), "fields", "fullName") == "Pat"
		})
	})
}

func TestDeliveryItemTagsAreCapped(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		for i := range delivery.MaxItemTags + 6 {
			n.create("article", "", "en", fmt.Sprintf(`{"title":"T%d","rank":%d}`, i, i), true)
		}
		r := n.wantStatus("/collections/article/items?channel=web&limit=100", 200)
		tags := strings.Fields(r.Header().Get("Surrogate-Key"))
		if len(tags) > delivery.MaxItemTags || !strings.Contains(" "+strings.Join(tags, " ")+" ", " c:article ") {
			t.Errorf("%d tags: %v", len(tags), tags)
		}
		for _, tg := range tags {
			if strings.HasPrefix(tg, "i:") {
				t.Errorf("item tag %s beyond the cap", tg)
			}
		}
		deq(t, "cache-tag", len(strings.Split(r.Header().Get("Cache-Tag"), ",")), len(tags))
		// Publishing a new article still purges the collapsed response.
		n.create("article", "", "en", `{"title":"Late"}`, true)
		n.drain()
		eventually(t, "the purge", func() bool {
			return len(n.get("/collections/article/items?channel=web&limit=100").json()["items"].([]any)) == delivery.MaxItemTags+7
		})
	})
}

func TestDeliveryListingPagesAgreeWithTotal(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		for i := range 7 {
			loc := "en"
			if i%2 == 1 {
				loc = "fr"
			}
			n.create("article", "", loc, fmt.Sprintf(`{"title":"T%d","rank":%d}`, i, i), true)
		}
		// fr_only shows 3 of the 7; the pages must hold exactly those.
		total := n.wantStatus("/collections/article/items?channel=fr_only&total=true", 200).json()["total"]
		deq(t, "total", total, float64(3))
		seen := 0
		for off := 0; ; off += 2 {
			r := n.wantStatus(fmt.Sprintf("/collections/article/items?channel=fr_only&sort=rank&limit=2&offset=%d", off), 200).json()
			items := r["items"].([]any)
			seen += len(items)
			if r["hasMore"] != true {
				break
			}
			if len(items) != 2 {
				t.Fatalf("short page %d before the end", len(items))
			}
		}
		deq(t, "items over all pages", seen, 3)
	})
}

func TestDeliveryInvalidKeysAndIdsNeverReachTheDatabase(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		n.wantStatus("/items/by-key/ok?channel=web", 404) // a valid key does query
		before := n.db.dbCalls()
		for _, p := range []string{"/items/by-key/UPPER", "/items/by-key/a%00b", "/items/by-key/%00", "/items/by-key/-x",
			"/items/by-key/" + strings.Repeat("a", 129), "/items/by-key/a%2Fb", "/items/%00", "/items/not-an-id", "/items/by-key/%ff"} {
			r := n.wantStatus(p+"?channel=web", 404)
			deq(t, p, field(r.json(), "error", "code"), "not_found")
		}
		deq(t, "db calls", n.db.dbCalls(), before)
		n.wantStatus("/items/by-key/"+strings.Repeat("a", 128)+"?channel=web", 404)
	})
}

func TestDeliveryPreviewByKeyOfNeverPublishedItem(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		id := n.create("article", "draft-only", "en", `{"title":"Not yet"}`, false)
		n.wantStatus("/items/by-key/draft-only?channel=web", 404)
		tok := n.previewToken(map[string]any{"channel": "web"}, 200)
		r := n.wantStatus("/items/by-key/draft-only?channel=web", 200, previewHeaderName, tok)
		deq(t, "title", field(r.json(), "item", "fields", "title"), "Not yet")
		deq(t, "id", field(r.json(), "item", "id"), id.String())
		scoped := n.previewToken(map[string]any{"channel": "web", "item": id.String()}, 200)
		n.wantStatus("/items/by-key/draft-only?channel=web", 200, previewHeaderName, scoped)
		other := n.previewToken(map[string]any{"channel": "web", "item": ids.UUID{7}.String()}, 200)
		n.wantStatus("/items/by-key/draft-only?channel=web", 404, previewHeaderName, other)
	})
}

func TestDeliveryETagOfAnUnchangedPayloadIsTheStoredOne(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWith(t, b.open(t, clk), clk, rawMod{})
		art := n.create("article", "", "en", `{"title":"Raw"}`, true)
		snaps, err := n.app.Services().DB.Reader().Snapshots(ctx, []ids.UUID{art}, []string{"en"})
		if err != nil || len(snaps) != 1 {
			t.Fatalf("snapshots %v %v", snaps, err)
		}
		r := n.wantStatus("/raw/"+art.String()+"?channel=web", 200)
		deq(t, "body is the payload", r.Body.String(), string(snaps[0].Payload))
		deq(t, "etag", r.Header().Get("Etag"), `"`+snaps[0].ETag+`"`)
		sum := sha256.Sum256(r.Body.Bytes())
		deq(t, "etag is the body's hash", snaps[0].ETag, hex.EncodeToString(sum[:]))
		n.wantStatus("/raw/"+art.String()+"?channel=web", http.StatusNotModified, "If-None-Match", r.Header().Get("Etag"))
	})
}

// rawMod serves a snapshot payload unchanged.
type rawMod struct{}

func (rawMod) Name() string { return "rawmod" }

func (rawMod) Register(r *kernel.Registry) error {
	r.DeliveryRoute(delivery.Route{Method: "GET", Path: "/raw/{id}", Summary: "Raw",
		Handler: func(w http.ResponseWriter, r *http.Request) {
			id, err := ids.Parse(r.PathValue("id"))
			if err != nil {
				delivery.Fail(w, r, content.ErrNotFound)
				return
			}
			out, err := delivery.Items(r, "", id)
			if err != nil || out[0] == nil {
				delivery.Fail(w, r, content.ErrNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = w.Write(out[0])
		}})
	return nil
}
