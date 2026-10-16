package tests

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/client"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/kernel"
)

// twoVersions makes an article published twice: v1 {A, de A, rating 1, address.street s}, v2 {B, de B, rating 2,
// address.city c}, and leaves an unpublished draft change on top.
func twoVersions(e *env) (a *content.View, v1, v2 int) {
	a = e.create("article", "en", `{"title":"A","summary":"sum","rating":1,"address":{"street":"s"}}`)
	e.update(a.ID, "de", `{"title":"de A"}`)
	e.publish(a.ID)
	e.update(a.ID, "en", `{"title":"B","summary":null,"rating":2,"address":{"street":null,"city":"c"}}`)
	e.update(a.ID, "de", `{"title":"de B"}`)
	e.publish(a.ID)
	e.update(a.ID, "en", `{"title":"C draft"}`)
	return a, 1, 2
}

func TestRestoreVersion(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a, v1, _ := twoVersions(e)
		e.drain()
		vers, _ := e.svc.Versions(ctx, a.ID)

		e.tick()
		v, err := e.svc.RestoreVersion(ctx, a.ID, v1, nil)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "view title", v.Fields["title"], any("A"))
		en := e.get(a.ID, "en", true)
		eq(t, "en title", en.Fields["title"], any("A"))
		eq(t, "en summary", en.Fields["summary"], any("sum"))
		eq(t, "rating", asJSON(en.Fields["rating"]), "1")
		eq(t, "address", asJSON(en.Fields["address"]), `{"street":"s"}`)
		eq(t, "de title", e.get(a.ID, "de", true).Fields["title"], any("de A"))

		// Delivery and the version list are unchanged until publish.
		pub := e.get(a.ID, "en", false)
		eq(t, "published title", pub.Fields["title"], any("B"))
		eq(t, "published address", asJSON(pub.Fields["address"]), `{"city":"c"}`)
		after, _ := e.svc.Versions(ctx, a.ID)
		eq(t, "versions", len(after), len(vers))
		if acts := e.auditActions(a.ID); acts[len(acts)-1] != "item.restore_version" {
			t.Errorf("audit = %v", acts)
		}
		var found bool
		for _, ev := range e.drain() {
			found = found || ev.Topic == "item.updated"
		}
		if !found {
			t.Error("no item.updated event")
		}

		e.publish(a.ID)
		eq(t, "published after", e.get(a.ID, "en", false).Fields["title"], any("A"))
	})
}

func TestRestoreVersionOneLocale(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a, v1, _ := twoVersions(e)
		if _, err := e.svc.RestoreVersion(ctx, a.ID, v1, []string{"de"}); err != nil {
			t.Fatal(err)
		}
		eq(t, "de restored", e.get(a.ID, "de", true).Fields["title"], any("de A"))
		eq(t, "en kept", e.get(a.ID, "en", true).Fields["title"], any("C draft"))
		// The shared part always comes along.
		eq(t, "shared", asJSON(e.get(a.ID, "en", true).Fields["rating"]), "1")
	})
}

func TestRestoreVersionErrors(t *testing.T) {
	reject := false
	run(t, func(t *testing.T, e *env) {
		a, v1, _ := twoVersions(e)
		if err := e.hooks.Add(hooks.Hook{Type: "article", Event: hooks.Validate, Fn: func(c *hooks.Context) error {
			if reject {
				c.Errors.Add("title", "nope", "rejected")
			}
			return nil
		}}); err != nil {
			t.Fatal(err)
		}
		before := e.auditActions(a.ID)

		reject = true
		_, err := e.svc.RestoreVersion(ctx, a.ID, v1, nil)
		_ = wantValidation(t, err)
		reject = false
		eq(t, "draft untouched", e.get(a.ID, "en", true).Fields["title"], any("C draft"))
		eq(t, "no audit", len(e.auditActions(a.ID)), len(before))

		if _, err := e.svc.RestoreVersion(ctx, a.ID, 99, nil); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unknown version: %v", err)
		}
		if _, err := e.svc.RestoreVersion(ctx, a.ID, v1, []string{"fr"}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("locale not in version: %v", err)
		}
		if _, err := e.svc.RestoreVersion(ctx, a.ID, v1, []string{"x y"}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("bad locale: %v", err)
		}
		if err := e.svc.Trash(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.RestoreVersion(ctx, a.ID, v1, nil); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("trashed: %v", err)
		}
	})
}

func TestRestoreVersionAccess(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		n.update(n.item, "de", `{"title":"Titel"}`)
		n.update(n.item, "en", `{"rank":4}`)
		n.publish(n.item)
		const v = 2

		enOnly := n.principalFor("rv-en", rolePolicy("content.read", nil),
			rolePolicy("content.edit", map[string][]string{"locale": {"en"}}))
		viewer := n.principalFor("rv-viewer", rolePolicy("content.read", nil))
		nobody := n.principalFor("rv-nobody", rolePolicy("content.edit", map[string][]string{"collection": {"person"}}))
		editor := n.principalFor("rv-editor", rolePolicy("content.read", nil), rolePolicy("content.edit", nil))

		if _, err := s.Content.RestoreVersion(viewer, n.item, v, nil); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("viewer: %v, want forbidden", err)
		}
		if _, err := s.Content.RestoreVersion(enOnly, n.item, v, []string{"de"}); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("de with en-only edit: %v, want forbidden", err)
		}
		if _, err := s.Content.RestoreVersion(nobody, n.item, v, nil); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("unreadable: %v, want not found", err)
		}
		if _, err := s.Content.RestoreVersion(editor, n.item, v, []string{"en"}); err != nil {
			t.Errorf("editor: %v", err)
		}
	})
}

func TestRestoreVersionHTTPAndClient(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		clk := newClock()
		n := newDeliveryNode(t, be.open(t, clk), clk)
		id := n.create("article", "", "en", `{"title":"One","rank":1}`, true)
		n.update(id, "en", `{"title":"Two","rank":2}`)
		n.publish(id)

		path := "/items/" + id.String() + "/versions/1/restore"
		r := n.manage("POST", path, map[string]any{"locales": []string{"en"}})
		if r.Code != 200 {
			t.Fatalf("restore: %d %s", r.Code, r.Body)
		}
		eq(t, "title", asJSON(r.json()["fields"].(map[string]any)["title"]), `"One"`)
		for _, v := range []string{"9", "0"} {
			if got := n.manage("POST", "/items/"+id.String()+"/versions/"+v+"/restore", nil).Code; got != 404 {
				t.Errorf("version %s = %d, want 404", v, got)
			}
		}
		if got := n.manage("POST", path, map[string]any{"locales": []string{"fr"}}).Code; got != 400 {
			t.Errorf("locale not in version = %d", got)
		}

		srv := httptest.NewServer(n.h)
		defer srv.Close()
		m := client.NewManage(srv.URL, kernel.DevManageToken)
		n.update(id, "en", `{"title":"Three"}`)
		v, err := m.RestoreVersion(ctx, id.String(), 2, nil)
		if err != nil {
			t.Fatal(err)
		}
		if f, err := client.Fields[map[string]any](v); err != nil || f["title"] != "Two" {
			t.Errorf("client fields = %s %v", v.Fields, err)
		}
		if _, err := m.RestoreVersion(ctx, id.String(), 7, nil); err == nil {
			t.Error("unknown version restored")
		}
	})
}
