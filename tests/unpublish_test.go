package tests

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

func TestUnpublish(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		person := e.person("Pat")
		a := e.create("article", "en", fmt.Sprintf(`{"title":"First","author":%q,"rating":4}`, person.ID))
		e.publish(a.ID)
		e.update(a.ID, "de", `{"title":"Erste"}`)
		e.publish(a.ID)
		e.drain()
		eq(t, "published", e.get(a.ID, "", false).Version, 2)
		if refs, err := e.svc.References(ctx, person.ID); err != nil || len(refs) != 1 {
			t.Fatalf("references before = %v %v", refs, err)
		}

		e.tick()
		v, err := e.svc.Unpublish(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "status", v.Status, content.StatusUnpublished)
		eq(t, "version", v.Version, 0)
		eqSlice(t, "locales", v.Locales, []string{"de", "en"})
		eqSlice(t, "draft parts", v.DraftParts, []string{"shared", "de", "en"})
		eq(t, "name", v.Name, "First")

		// Gone from delivery's rows, published reads, references and the snapshots; present in the working view.
		_, err = e.svc.Get(ctx, a.ID, content.GetOptions{})
		wantErr(t, err, content.ErrNotFound)
		eq(t, "snapshots", len(e.snapshotsOf(a.ID)), 0)
		eqSlice(t, "delivered", e.delivered(content.ListOptions{Status: store.StatusPublished}), nil)
		eqSlice(t, "working list", e.names(content.ListOptions{}), []string{"First"})
		if refs, err := e.svc.References(ctx, person.ID); err != nil || len(refs) != 0 {
			t.Errorf("references after = %v %v", refs, err)
		}
		if res, err := e.svc.List(ctx, content.ListOptions{Type: "article", Status: store.StatusPublished}); err != nil || len(res.Items) != 0 {
			t.Errorf("published list = %+v %v", res, err)
		}
		dr := e.get(a.ID, "", true)
		eq(t, "draft status", dr.Status, content.StatusUnpublished)
		eq(t, "draft title", dr.Fields["title"], any("First"))
		if got := versionNumbers(t, e, a.ID); !slices.Equal(got, []int{2, 1}) {
			t.Errorf("history = %v", got)
		}

		wantErr(t, e.unpublishErr(a.ID), content.ErrBadRequest) // already offline

		// Events and audit.
		evs := e.drain()
		if len(evs) != 1 || evs[0].Topic != "item.unpublished" {
			t.Fatalf("events = %+v", evs)
		}
		var p struct {
			ID      ids.UUID `json:"id"`
			Type    string   `json:"type"`
			Version int      `json:"version"`
			Locales []string `json:"locales"`
		}
		if err := json.Unmarshal(evs[0].Payload, &p); err != nil {
			t.Fatal(err)
		}
		eq(t, "payload id", p.ID, a.ID)
		eq(t, "payload version", p.Version, 2)
		eqSlice(t, "payload locales", p.Locales, []string{"de", "en"})
		tags := cachetag.Tags([]cachetag.Mapper{cachetag.Items(func(string) bool { return false })}, evs[0].Topic, evs[0].Payload)
		if !slices.Contains(tags, cachetag.Item(a.ID.String())) || !slices.Contains(tags, cachetag.Collection("article")) {
			t.Errorf("tags = %v", tags)
		}
		acts := e.auditActions(a.ID)
		eq(t, "audit", acts[len(acts)-1], "item.unpublish")

		// The draft is editable; publishing again builds the next version from the kept content.
		e.update(a.ID, "en", `{"title":"First again"}`)
		e.publish(a.ID)
		again := e.get(a.ID, "", false)
		eq(t, "version after republish", again.Version, 3)
		eq(t, "title", again.Fields["title"], any("First again"))
		eq(t, "author kept", again.Fields["author"], any(person.ID.String()))
		eq(t, "rating kept", fmt.Sprint(again.Fields["rating"]), "4")
		de := e.get(a.ID, "de", false)
		eq(t, "de kept", de.Fields["title"], any("Erste"))
		eq(t, "status", again.Status, content.StatusPublished)
		if refs, err := e.svc.References(ctx, person.ID); err != nil || len(refs) != 1 {
			t.Errorf("references after republish = %v %v", refs, err)
		}
		eq(t, "snapshots back", len(e.snapshotsOf(a.ID)), 2)
		e.checkSnapshotsComplete()

		// Without any edit, an unpublished item publishes again as it was.
		e.tick()
		if _, err := e.svc.Unpublish(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		e.publish(a.ID)
		eq(t, "version", e.get(a.ID, "", false).Version, 4)
	})
}

func versionNumbers(t *testing.T, e *env, id ids.UUID) []int {
	t.Helper()
	metas, err := e.svc.Versions(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, m := range metas {
		out = append(out, m.Version)
	}
	return out
}

func (e *env) unpublishErr(id ids.UUID) error {
	_, err := e.svc.Unpublish(ctx, id)
	return err
}

func TestUnpublishRefusals(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		draft := e.create("article", "en", `{"title":"Never published"}`)
		wantErr(t, e.unpublishErr(draft.ID), content.ErrBadRequest)
		s, err := e.svc.UpdateSingleton(ctx, "settings", content.UpdateInput{Locale: "en", Fields: doc(t, `{"siteName":"S"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PublishSingleton(ctx, "settings", content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		wantErr(t, e.unpublishErr(s.ID), content.ErrBadRequest)
		wantErr(t, e.unpublishErr(ids.UUID{1}), content.ErrNotFound)

		// A stale If-Match state is refused and nothing changes.
		a := e.create("article", "en", `{"title":"A"}`)
		pub := e.publish(a.ID)
		e.tick()
		e.update(a.ID, "en", `{"summary":"x"}`)
		_, err = e.svc.Unpublish(content.WithExpectedState(ctx, content.StateOf(pub.UpdatedAt)), a.ID)
		wantErr(t, err, content.ErrPreconditionFailed)
		eq(t, "still published", e.get(a.ID, "", false).Version, 1)
	})
}

// A release publishes and unpublishes many items in one transaction: the Tx variants join the caller's
// transaction, and a failure rolls back every item.
func TestPublishTxAtomic(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.create("article", "en", `{"title":"A"}`)
		b := e.create("article", "en", `{"title":"B"}`)
		live := e.create("article", "en", `{"title":"Live"}`)
		e.publish(live.ID)
		if err := e.svc.Prepare(ctx); err != nil {
			t.Fatal(err)
		}
		bad := e.create("article", "en", `{"title":"Bad","author":"`+ids.UUID{7}.String()+`"}`)

		err := e.db.Tx(ctx, func(tx store.Tx) error {
			if _, err := e.svc.PublishTx(ctx, tx, a.ID, content.PublishOptions{}); err != nil {
				return err
			}
			if err := e.svc.UnpublishTx(ctx, tx, live.ID); err != nil {
				return err
			}
			_, err := e.svc.PublishTx(ctx, tx, bad.ID, content.PublishOptions{})
			return err
		})
		wantErr(t, err, content.ErrInvalid)
		_, err = e.svc.Get(ctx, a.ID, content.GetOptions{})
		wantErr(t, err, content.ErrNotFound)
		eq(t, "live unchanged", e.get(live.ID, "", false).Version, 1)
		eq(t, "no snapshots of a", len(e.snapshotsOf(a.ID)), 0)

		err = e.db.Tx(ctx, func(tx store.Tx) error {
			if _, err := e.svc.PublishTx(ctx, tx, a.ID, content.PublishOptions{}); err != nil {
				return err
			}
			if _, err := e.svc.PublishTx(ctx, tx, b.ID, content.PublishOptions{}); err != nil {
				return err
			}
			return e.svc.UnpublishTx(ctx, tx, live.ID)
		})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "a", e.get(a.ID, "", false).Version, 1)
		eq(t, "b", e.get(b.ID, "", false).Version, 1)
		_, err = e.svc.Get(ctx, live.ID, content.GetOptions{})
		wantErr(t, err, content.ErrNotFound)
		var topics []string
		for _, ev := range e.drain() {
			topics = append(topics, ev.Topic)
		}
		if !slices.Contains(topics, "item.unpublished") || len(slices.DeleteFunc(slices.Clone(topics), func(s string) bool { return s != "item.published" })) != 3 {
			t.Errorf("events = %v", topics)
		}
	})
}

func TestUnpublishDelivery(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"One","rank":1}`, true)
		n.drain()
		item := "/items/" + art.String() + "?channel=web"
		list := "/collections/article/items?channel=web"
		n.wantStatus(item, 200)
		deq(t, "listed", len(n.wantStatus(list, 200).json()["items"].([]any)), 1)

		n.tick()
		r := n.manage("POST", "/items/"+art.String()+"/unpublish", nil)
		if r.Code != 200 || field(r.json(), "status") != "unpublished" {
			t.Fatalf("unpublish = %d %s", r.Code, r.Body)
		}
		n.drain()
		eventually(t, "delivery to purge the item", func() bool { return n.get(item).Code == 404 })
		eventually(t, "the listing to drop it", func() bool {
			return len(n.wantStatus(list, 200).json()["items"].([]any)) == 0
		})
		n.wantStatus("/items/by-key/k?channel=web", 404)
		r = n.manage("POST", "/items/"+art.String()+"/unpublish", nil)
		if r.Code != 400 {
			t.Errorf("second unpublish = %d %s", r.Code, r.Body)
		}

		// The draft is still there, and publishing brings the item back.
		r = n.manage("GET", "/items/"+art.String()+"?draft=true", nil)
		deq(t, "draft title", field(r.json(), "fields", "title"), "One")
		r = n.manage("POST", "/items/"+art.String()+"/publish", nil)
		if r.Code != 200 {
			t.Fatalf("republish = %d %s", r.Code, r.Body)
		}
		deq(t, "version", field(r.json(), "version"), float64(2))
		n.drain()
		eventually(t, "the item to come back", func() bool { return n.get(item).Code == 200 })
	})
}

func TestUnpublishAccess(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		reader := n.principalFor("reader", rolePolicy("content.read", nil), rolePolicy("content.edit", nil), rolePolicy("content.delete", nil))
		if _, err := s.Content.Unpublish(reader, n.item); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("unpublish without content.publish: %v", err)
		}
		if err := s.Content.Prepare(reader); err != nil {
			t.Fatal(err)
		}
		err := s.DB.Tx(reader, func(tx store.Tx) error { return s.Content.UnpublishTx(reader, tx, n.item) })
		if !errors.Is(err, access.ErrForbidden) {
			t.Errorf("UnpublishTx without content.publish: %v", err)
		}
		other := n.principalFor("people-only", rolePolicy("content.read", nil), rolePolicy("content.publish", map[string][]string{"collection": {"person"}}))
		if _, err := s.Content.Unpublish(other, n.item); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("unpublish in another collection: %v", err)
		}
		publisher := n.principalFor("publisher", rolePolicy("content.read", nil), rolePolicy("content.publish", nil))
		v, err := s.Content.Unpublish(publisher, n.item)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "status", v.Status, content.StatusUnpublished)
		if resp := n.manage("POST", "/items/"+n.item.String()+"/unpublish", nil); resp.Code != 400 {
			t.Errorf("api: %d %s", resp.Code, resp.Body)
		}
	})
}

// A locale-restricted publish of an unpublished item with history keeps the other locales of its newest version.
func TestPublishAfterUnpublishKeepsOtherLocales(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.create("article", "en", `{"title":"First"}`)
		e.publish(a.ID)
		e.update(a.ID, "de", `{"title":"Erste"}`)
		e.publish(a.ID)
		e.tick()
		if _, err := e.svc.Unpublish(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		e.update(a.ID, "en", `{"title":"First again"}`)
		e.update(a.ID, "de", `{"title":"Zweite"}`)
		e.tick()
		if _, err := e.svc.Publish(ctx, a.ID, content.PublishOptions{Locales: []string{"en"}}); err != nil {
			t.Fatal(err)
		}
		eq(t, "version", e.get(a.ID, "", false).Version, 3)
		eq(t, "en", e.get(a.ID, "en", false).Fields["title"], any("First again"))
		eq(t, "de kept from the last version", e.get(a.ID, "de", false).Fields["title"], any("Erste"))
		eq(t, "de draft kept", e.get(a.ID, "de", true).Fields["title"], any("Zweite"))
	})
}
