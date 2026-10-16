package tests

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

func TestTrashAndRestore(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		keep := e.create("article", "en", `{"title":"Keep"}`)
		a, err := e.svc.Create(ctx, content.CreateInput{Type: "article", Key: "home-story", Fields: doc(t, `{"title":"Story","summary":"s"}`)})
		if err != nil {
			t.Fatal(err)
		}
		e.publish(a.ID)
		e.update(a.ID, "en", `{"summary":"edited"}`)
		e.update(a.ID, "de", `{"title":"Geschichte"}`)
		e.drain()

		e.tick()
		if err := e.svc.Trash(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		// Hidden everywhere.
		for what, call := range map[string]func() error{
			"get":       func() error { _, err := e.svc.Get(ctx, a.ID, content.GetOptions{}); return err },
			"get draft": func() error { _, err := e.svc.Get(ctx, a.ID, content.GetOptions{Draft: true}); return err },
			"by key":    func() error { _, err := e.svc.GetByKey(ctx, "home-story", content.GetOptions{Draft: true}); return err },
			"update": func() error {
				_, err := e.svc.Update(ctx, a.ID, content.UpdateInput{Fields: doc(t, `{"title":"x"}`)})
				return err
			},
			"publish":    func() error { _, err := e.svc.Publish(ctx, a.ID, content.PublishOptions{}); return err },
			"unpublish":  func() error { _, err := e.svc.Unpublish(ctx, a.ID); return err },
			"discard":    func() error { return e.svc.Discard(ctx, a.ID, nil) },
			"trash":      func() error { return e.svc.Trash(ctx, a.ID) },
			"versions":   func() error { _, err := e.svc.Versions(ctx, a.ID); return err },
			"references": func() error { _, err := e.svc.References(ctx, a.ID); return err },
		} {
			if err := call(); !errors.Is(err, content.ErrNotFound) {
				t.Errorf("%s of a trashed item: %v, want not found", what, err)
			}
		}
		eqSlice(t, "list", e.names(content.ListOptions{}), []string{"Keep"})
		eqSlice(t, "search", e.names(content.ListOptions{Search: "Story"}), nil)
		eqSlice(t, "published", e.delivered(content.ListOptions{Status: store.StatusPublished}), nil)
		eq(t, "snapshots", len(e.snapshotsOf(a.ID)), 0)
		if list, err := e.svc.List(ctx, content.ListOptions{Type: "article", Where: []content.Filter{{Field: "slug", Op: store.OpEq, Value: "story"}}}); err != nil || len(list.Items) != 0 {
			t.Errorf("filtered list = %+v %v", list, err)
		}

		// The trash lists it; reading it needs the option.
		tr, err := e.svc.ListTrash(ctx, content.TrashOptions{})
		if err != nil || tr.Total != 1 || len(tr.Items) != 1 {
			t.Fatalf("trash = %+v %v", tr, err)
		}
		it := tr.Items[0]
		eq(t, "id", it.ID, a.ID)
		eq(t, "name", it.Name, "Story")
		eq(t, "deleted by", it.DeletedBy, "system")
		if !it.DeletedAt.Equal(e.clk.Now()) {
			t.Errorf("deletedAt = %s, want %s", it.DeletedAt, e.clk.Now())
		}
		if got, err := e.svc.ListTrash(ctx, content.TrashOptions{Type: "person"}); err != nil || got.Total != 0 || len(got.Items) != 0 {
			t.Errorf("trash of another type = %+v %v", got, err)
		}
		v, err := e.svc.Get(ctx, a.ID, content.GetOptions{Trashed: true})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "trashed status", v.Status, content.StatusUnpublished)
		eq(t, "trashed summary", v.Fields["summary"], any("edited"))
		if v.DeletedAt == nil || v.DeletedBy != "system" {
			t.Errorf("view deleted = %v %q", v.DeletedAt, v.DeletedBy)
		}
		if live, err := e.svc.Get(ctx, keep.ID, content.GetOptions{Trashed: true, Draft: true}); err != nil || live.DeletedAt != nil {
			t.Errorf("trashed option on a live item: %+v %v", live, err)
		}

		// The key is free while it is in the trash.
		other, err := e.svc.Create(ctx, content.CreateInput{Type: "article", Key: "home-story", Fields: doc(t, `{"title":"Other"}`)})
		if err != nil {
			t.Fatalf("key reuse: %v", err)
		}
		e.tick()
		_, err = e.svc.Restore(ctx, a.ID)
		wantErr(t, err, content.ErrDuplicateKey)
		if err := e.svc.Trash(ctx, other.ID); err != nil {
			t.Fatal(err)
		}

		e.tick()
		r, err := e.svc.Restore(ctx, a.ID)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "restored status", r.Status, content.StatusUnpublished)
		eq(t, "restored key", r.Key, "home-story")
		eq(t, "restored summary", r.Fields["summary"], any("edited"))
		if r.DeletedAt != nil {
			t.Errorf("restored view still deleted: %v", r.DeletedAt)
		}
		_, err = e.svc.Get(ctx, a.ID, content.GetOptions{})
		wantErr(t, err, content.ErrNotFound) // not published
		eqSlice(t, "list after restore", e.names(content.ListOptions{}), []string{"Keep", "Story"})
		wantErr(t, restoreErr(e, a.ID), content.ErrConflict)

		e.publish(a.ID)
		eq(t, "republished", e.get(a.ID, "", false).Version, 2)
		eq(t, "de draft kept", e.get(a.ID, "de", true).Name, "Geschichte")

		var topics []string
		for _, ev := range e.drain() {
			topics = append(topics, ev.Topic)
		}
		for _, want := range []string{"item.trashed", "item.restored"} {
			if !slices.Contains(topics, want) {
				t.Errorf("no %s event in %v", want, topics)
			}
		}
		eqSlice(t, "audit", e.auditActions(a.ID), []string{"item.create", "item.publish", "item.update", "item.update", "item.trash", "item.restore", "item.publish"})
	})
}

func restoreErr(e *env, id ids.UUID) error {
	_, err := e.svc.Restore(ctx, id)
	return err
}

func TestTrashSingletonsAndBlockers(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		s, err := e.svc.UpdateSingleton(ctx, "settings", content.UpdateInput{Locale: "en", Fields: doc(t, `{"siteName":"S"}`)})
		if err != nil {
			t.Fatal(err)
		}
		wantErr(t, e.svc.Trash(ctx, s.ID), content.ErrBadRequest)

		// A published referrer with on_delete block stops the trash; trashing the referrer first unblocks it.
		blocked := e.person("Blocked")
		a := e.create("article", "en", fmt.Sprintf(`{"title":"A","author":%q}`, blocked.ID))
		e.publish(a.ID)
		err = e.svc.Trash(ctx, blocked.ID)
		wantErr(t, err, content.ErrReferenced)
		var re *content.ReferencedError
		if !errors.As(err, &re) || len(re.Referrers) != 1 || re.Referrers[0].Name != "A" {
			t.Fatalf("referenced = %v", err)
		}
		if _, err := e.svc.Get(ctx, blocked.ID, content.GetOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Trash(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Trash(ctx, blocked.ID); err != nil {
			t.Fatal(err)
		}
		// Restoring the referrer's target does not bring the referrer back live; publishing it again works.
		if _, err := e.svc.Restore(ctx, blocked.ID); err != nil {
			t.Fatal(err)
		}
		e.publish(blocked.ID)
		if _, err := e.svc.Restore(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		e.publish(a.ID)

		// A trashed item does not exist for references: publishing something that points at it fails.
		gone := e.person("Gone")
		if err := e.svc.Trash(ctx, gone.ID); err != nil {
			t.Fatal(err)
		}
		b := e.create("article", "en", fmt.Sprintf(`{"title":"B","editor":%q}`, gone.ID))
		_, err = e.svc.Publish(ctx, b.ID, content.PublishOptions{})
		if ve := wantValidation(t, err); !slices.Contains(codes(ve), "editor:ref_target") {
			t.Errorf("publish pointing at a trashed item: %v", codes(ve))
		}
	})
}

func TestPurge(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		ed := e.person("Eve")
		a := e.create("article", "en", fmt.Sprintf(`{"title":"T","editor":%q}`, ed.ID))
		e.publish(a.ID)

		wantErr(t, e.svc.Purge(ctx, ed.ID), content.ErrConflict) // not in the trash
		if err := e.svc.Trash(ctx, ed.ID); err != nil {
			t.Fatal(err)
		}
		// Trashing leaves the nullify referrer as it is; the purge clears it.
		eq(t, "referrer waits", e.get(a.ID, "", false).Version, 1)
		if err := e.svc.Purge(ctx, ed.ID); err != nil {
			t.Fatal(err)
		}
		pub := e.get(a.ID, "", false)
		eq(t, "version bumped", pub.Version, 2)
		if _, ok := pub.Fields["editor"]; ok {
			t.Errorf("nullified ref kept: %v", pub.Fields)
		}
		if _, err := e.svc.Get(ctx, ed.ID, content.GetOptions{Trashed: true}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("purged item: %v", err)
		}
		wantErr(t, e.svc.Purge(ctx, ed.ID), content.ErrNotFound)
		eq(t, "trash empty", len(trashedIDs(t, e.svc, "")), 0)

		var topics []string
		for _, ev := range e.drain() {
			topics = append(topics, ev.Topic)
		}
		for _, want := range []string{"item.trashed", "item.deleted"} {
			if !slices.Contains(topics, want) {
				t.Errorf("no %s event in %v", want, topics)
			}
		}
		eqSlice(t, "audit", e.auditActions(ed.ID), []string{"item.create", "item.publish", "item.trash", "item.purge"})
	})
}

func TestTrashRetention(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		day := 24 * time.Hour
		old := e.person("Old")
		recent := e.person("Recent")
		live := e.person("Live")
		if err := e.svc.Trash(ctx, old.ID); err != nil {
			t.Fatal(err)
		}
		e.clk.Advance(20 * day)
		if err := e.svc.Trash(ctx, recent.ID); err != nil {
			t.Fatal(err)
		}
		e.clk.Advance(11 * day) // old: 31 days, recent: 11 days
		n, err := e.svc.PurgeExpired(ctx, e.clk.Now().Add(-30*day))
		if err != nil || n != 1 {
			t.Fatalf("purged %d, %v", n, err)
		}
		eq(t, "trash", len(trashedIDs(t, e.svc, "")), 1)
		if _, err := e.svc.Get(ctx, old.ID, content.GetOptions{Trashed: true}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("old: %v", err)
		}
		if _, err := e.svc.Get(ctx, recent.ID, content.GetOptions{Trashed: true}); err != nil {
			t.Errorf("recent: %v", err)
		}
		if _, err := e.svc.Get(ctx, live.ID, content.GetOptions{}); err != nil {
			t.Errorf("live: %v", err)
		}
		n, err = e.svc.PurgeExpired(ctx, e.clk.Now().Add(-30*day))
		if err != nil || n != 0 {
			t.Errorf("second run purged %d, %v", n, err)
		}
		eqSlice(t, "audit", e.auditActions(old.ID), []string{"item.create", "item.publish", "item.trash", "item.purge"})
	})
}

func TestTrashRetentionJob(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		s := n.app.Services()
		art := n.create("article", "", "en", `{"title":"Doomed"}`, true)
		if err := s.Content.Trash(ctx, art); err != nil {
			t.Fatal(err)
		}
		n.clk.Advance(29 * 24 * time.Hour)
		if err := n.app.RunJob(ctx, "content.trash"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content.Get(ctx, art, content.GetOptions{Trashed: true}); err != nil {
			t.Fatalf("purged before the retention ended: %v", err)
		}
		n.clk.Advance(2 * 24 * time.Hour)
		if err := n.app.RunJob(ctx, "content.trash"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content.Get(ctx, art, content.GetOptions{Trashed: true}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("after the retention: %v", err)
		}
		if !slices.Contains(n.app.JobNames(), "content.trash") {
			t.Errorf("jobs = %v", n.app.JobNames())
		}
	})
}

func TestTrashAPI(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"One","rank":1}`, true)
		n.drain()
		id := art.String()
		item := "/items/" + id + "?channel=web"
		n.wantStatus(item, 200)

		if r := n.manage("DELETE", "/items/"+id+"?permanent=true", nil); r.Code != 409 || field(r.json(), "error", "code") != "conflict" {
			t.Errorf("permanent delete of a live item = %d %s", r.Code, r.Body)
		}
		if r := n.manage("DELETE", "/items/"+id+"?permanent=maybe", nil); r.Code != 400 {
			t.Errorf("bad flag = %d", r.Code)
		}
		if r := n.manage("DELETE", "/items/"+id, nil); r.Code != 204 {
			t.Fatalf("trash = %d %s", r.Code, r.Body)
		}
		n.drain()
		eventually(t, "delivery to drop the trashed item", func() bool { return n.get(item).Code == 404 })
		for _, p := range []string{"/items/" + id, "/items/" + id + "?draft=true"} {
			if r := n.manage("GET", p, nil); r.Code != 404 {
				t.Errorf("GET %s = %d", p, r.Code)
			}
		}
		if r := n.manage("GET", "/collections/article/items", nil); len(r.json()["items"].([]any)) != 0 {
			t.Errorf("list = %s", r.Body)
		}
		r := n.manage("GET", "/items/"+id+"?trashed=true", nil)
		if r.Code != 200 || field(r.json(), "deletedAt") == nil || field(r.json(), "status") != "unpublished" {
			t.Fatalf("trashed read = %d %s", r.Code, r.Body)
		}
		r = n.manage("GET", "/trash?type=article", nil)
		items := r.json()["items"].([]any)
		if r.Code != 200 || len(items) != 1 || field(r.json(), "total") != float64(1) ||
			field(items[0].(map[string]any), "id") != id || field(items[0].(map[string]any), "deletedAt") == nil {
			t.Fatalf("trash = %d %s", r.Code, r.Body)
		}
		if r := n.manage("GET", "/trash?nope=1", nil); r.Code != 400 {
			t.Errorf("unknown parameter = %d", r.Code)
		}
		r = n.manage("POST", "/items/"+id+"/restore", nil)
		if r.Code != 200 || field(r.json(), "status") != "unpublished" || field(r.json(), "deletedAt") != nil {
			t.Fatalf("restore = %d %s", r.Code, r.Body)
		}
		if r := n.manage("POST", "/items/"+id+"/restore", nil); r.Code != 409 {
			t.Errorf("restore of a live item = %d", r.Code)
		}
		if r := n.manage("DELETE", "/items/"+id, nil); r.Code != 204 {
			t.Fatalf("trash again = %d", r.Code)
		}
		if r := n.manage("DELETE", "/items/"+id+"?permanent=true", nil); r.Code != 204 {
			t.Fatalf("purge = %d %s", r.Code, r.Body)
		}
		if r := n.manage("GET", "/items/"+id+"?trashed=true", nil); r.Code != 404 {
			t.Errorf("purged = %d", r.Code)
		}
	})
}

func TestTrashAccess(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		reader := n.principalFor("reader", rolePolicy("content.read", nil), rolePolicy("content.publish", nil))
		deleter := n.principalFor("deleter", rolePolicy("content.read", nil), rolePolicy("content.delete", nil))
		peopleOnly := n.principalFor("people", rolePolicy("content.read", map[string][]string{"collection": {"person"}}),
			rolePolicy("content.delete", nil))
		if err := s.Content.Trash(reader, n.item); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("trash without content.delete: %v", err)
		}
		if err := s.Content.Trash(deleter, n.item); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content.Restore(reader, n.item); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("restore without content.delete: %v", err)
		}
		if err := s.Content.Purge(reader, n.item); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("purge without content.delete: %v", err)
		}
		// The trash shows what the principal may read.
		res, err := s.Content.ListTrash(deleter, content.TrashOptions{})
		if err != nil || len(res.Items) != 1 || res.Total != 1 {
			t.Fatalf("trash for the deleter = %+v %v", res, err)
		}
		res, err = s.Content.ListTrash(peopleOnly, content.TrashOptions{})
		if err != nil || len(res.Items) != 0 || res.Total != 0 {
			t.Errorf("trash for a people-only reader = %+v %v", res, err)
		}
		res, err = s.Content.ListTrash(peopleOnly, content.TrashOptions{Type: "article"})
		if err != nil || len(res.Items) != 0 {
			t.Errorf("trash of articles for a people-only reader = %+v %v", res, err)
		}
		if _, err := s.Content.Get(peopleOnly, n.item, content.GetOptions{Trashed: true}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("trashed read by a people-only reader: %v", err)
		}
		// Who trashed it shows in the listing.
		if got := res; got != nil && len(got.Items) > 0 {
			t.Errorf("unexpected rows: %+v", got.Items)
		}
		res, err = s.Content.ListTrash(deleter, content.TrashOptions{Type: "article"})
		if err != nil || len(res.Items) != 1 || res.Items[0].DeletedBy == "" || res.Items[0].DeletedByName != "deleter" {
			t.Errorf("trash row = %+v %v", res, err)
		}
		if _, err := s.Content.Restore(deleter, n.item); err != nil {
			t.Fatal(err)
		}
	})
}
