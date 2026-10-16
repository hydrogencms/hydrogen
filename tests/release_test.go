package tests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/release"
	"github.com/hydrogencms/hydrogen/store"
)

// releaseProbe records the release.* outbox events.
type releaseProbe struct {
	mu     sync.Mutex
	events []outbox.Event
}

func (p *releaseProbe) Name() string { return "relprobe" }

func (p *releaseProbe) Register(r *kernel.Registry) error {
	r.Consumer(outbox.Consumer{Name: "relprobe", Topics: []string{"release.*"}, Handle: func(_ context.Context, e outbox.Event) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.events = append(p.events, e)
		return nil
	}})
	return nil
}

func (p *releaseProbe) topics() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.events {
		out = append(out, e.Topic)
	}
	return out
}

type relNode struct {
	*dnode
	probe *releaseProbe
}

func runRelease(t *testing.T, fn func(t *testing.T, n *relNode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		probe := &releaseProbe{}
		n, err := tryNode(t, b.open(t, clk), clk, deliveryChannels, probe)
		if err != nil {
			t.Fatal(err)
		}
		fn(t, &relNode{dnode: n, probe: probe})
	})
}

func (n *relNode) svc() *release.Service { return n.app.Services().Releases }

func (n *relNode) call(secret, method, path string, body any, want int) resp {
	n.t.Helper()
	var r resp
	if secret == "" {
		r = n.manage(method, path, body)
	} else {
		r = n.as(secret, method, path, body)
	}
	if r.Code != want {
		n.t.Fatalf("%s %s: %d, want %d: %s", method, path, r.Code, want, r.Body)
	}
	return r
}

func (n *relNode) newRelease(secret, name string, items ...map[string]any) string {
	n.t.Helper()
	body := map[string]any{"name": name}
	if len(items) > 0 {
		body["items"] = items
	}
	return n.call(secret, "POST", "/releases", body, 201).json()["id"].(string)
}

func ritem(id ids.UUID, action string) map[string]any {
	return map[string]any{"item": id.String(), "action": action}
}

func (n *relNode) live(id ids.UUID) bool {
	n.t.Helper()
	n.drain()
	return n.get("/items/"+id.String()+"?channel=web").Code == 200
}

func (n *relNode) status(id string) string {
	n.t.Helper()
	return n.call("", "GET", "/releases/"+id, nil, 200).json()["status"].(string)
}

func (n *relNode) auditActions(subject string) []string {
	n.t.Helper()
	var out []string
	err := n.app.Services().DB.View(ctx, func(tx store.Tx) error {
		evs, err := tx.Audit().List(ctx, store.AuditQuery{Subject: subject, Limit: 100})
		for i := len(evs) - 1; i >= 0; i-- {
			out = append(out, evs[i].Action)
		}
		return err
	})
	if err != nil {
		n.t.Fatal(err)
	}
	return out
}

func (n *relNode) auditActor(subject, action string) string {
	n.t.Helper()
	var out string
	err := n.app.Services().DB.View(ctx, func(tx store.Tx) error {
		evs, err := tx.Audit().List(ctx, store.AuditQuery{Subject: subject, Action: action, Limit: 10})
		if len(evs) > 0 {
			out = evs[0].ActorKind + ":" + evs[0].ActorID
		}
		return err
	})
	if err != nil {
		n.t.Fatal(err)
	}
	return out
}

// releaseToken makes a token with the policies; it returns the token, its role and its secret.
func (n *relNode) releaseToken(name string, policies ...access.RolePolicy) (access.Token, access.Role, string) {
	n.t.Helper()
	s := n.app.Services()
	role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: policies})
	if err != nil {
		n.t.Fatal(err)
	}
	tok, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: name, Roles: []ids.UUID{role.ID}})
	if err != nil {
		n.t.Fatal(err)
	}
	return tok, role, secret
}

func editorPolicies() []access.RolePolicy {
	var out []access.RolePolicy
	for _, p := range []string{"content.read", "content.create", "content.edit", "content.publish", "release.manage"} {
		out = append(out, rolePolicy(p, nil))
	}
	return out
}

func errorCodes(rel map[string]any) []string {
	var out []string
	for _, e := range rel["errors"].([]any) {
		m := e.(map[string]any)
		out = append(out, m["item"].(string)+":"+m["code"].(string))
	}
	slices.Sort(out)
	return out
}

func TestReleaseCRUD(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		a := n.create("article", "a", "en", `{"title":"Alpha"}`, false)
		b := n.create("article", "b", "en", `{"title":"Beta"}`, true)

		id := n.newRelease("", "  Autumn launch  ", ritem(a, "publish"))
		rel := n.call("", "GET", "/releases/"+id, nil, 200).json()
		deq(t, "name", rel["name"], "Autumn launch")
		deq(t, "status", rel["status"], "open")
		deq(t, "kind", rel["kind"], "release")
		deq(t, "itemCount", rel["itemCount"], float64(1))
		it := rel["items"].([]any)[0].(map[string]any)
		deq(t, "item name", it["name"], "Alpha")
		deq(t, "item type", it["type"], "article")
		deq(t, "item state", it["state"], "changes")
		deq(t, "createdByName", rel["createdByName"], "")

		// A put replaces the action; an unpublished item cannot be put twice with different results.
		n.call("", "PUT", "/releases/"+id+"/items/"+b.String(), map[string]any{"action": "unpublish"}, 200)
		n.call("", "PUT", "/releases/"+id+"/items/"+a.String(), map[string]any{"locales": []string{"en"}}, 200)
		rel = n.call("", "GET", "/releases/"+id, nil, 200).json()
		deq(t, "itemCount", rel["itemCount"], float64(2))
		states := map[string]string{}
		for _, x := range rel["items"].([]any) {
			m := x.(map[string]any)
			states[m["name"].(string)] = m["action"].(string) + "/" + m["state"].(string)
		}
		deq(t, "states", asJSON(states), `{"Alpha":"publish/changes","Beta":"unpublish/changes"}`)

		// Lists: by status, kind and item; the item's own list shows its action.
		l := n.call("", "GET", "/releases?status=open,scheduled&kind=release", nil, 200).json()
		deq(t, "list total", l["total"], float64(1))
		l = n.call("", "GET", "/releases?status=published", nil, 200).json()
		deq(t, "published list", len(l["items"].([]any)), 0)
		of := n.call("", "GET", "/items/"+b.String()+"/releases", nil, 200).json()["items"].([]any)
		if len(of) != 1 || of[0].(map[string]any)["action"] != "unpublish" || of[0].(map[string]any)["id"] != id {
			t.Fatalf("item releases = %v", of)
		}

		// Remove, rename, validation.
		n.call("", "DELETE", "/releases/"+id+"/items/"+b.String(), nil, 204)
		n.call("", "DELETE", "/releases/"+id+"/items/"+b.String(), nil, 204)
		deq(t, "after remove", len(n.call("", "GET", "/items/"+b.String()+"/releases", nil, 200).json()["items"].([]any)), 0)
		r := n.call("", "PATCH", "/releases/"+id, map[string]any{"name": "Winter", "description": "d"}, 200).json()
		deq(t, "renamed", r["name"], "Winter")
		n.call("", "PATCH", "/releases/"+id, map[string]any{"name": " "}, 400)
		n.call("", "POST", "/releases", map[string]any{"name": ""}, 400)
		n.call("", "POST", "/releases", map[string]any{"name": "x", "kind": "weird"}, 400)
		n.call("", "PUT", "/releases/"+id+"/items/"+a.String(), map[string]any{"action": "burn"}, 400)
		n.call("", "PUT", "/releases/"+id+"/items/"+a.String(), map[string]any{"action": "unpublish", "locales": []string{"en"}}, 400)
		n.call("", "PUT", "/releases/"+id+"/items/"+ids.NewGenerator(n.clk, nil).New().String(), map[string]any{}, 404)
		n.call("", "GET", "/releases/"+ids.NewGenerator(n.clk, nil).New().String(), nil, 404)
		n.call("", "GET", "/releases/nope", nil, 404)
		n.call("", "POST", "/releases", map[string]any{"name": "x", "items": []any{map[string]any{"item": "nope"}}}, 400)

		// Singletons cannot be unpublished.
		set := n.create("settings", "", "en", `{"siteName":"Acme"}`, true)
		n.call("", "PUT", "/releases/"+id+"/items/"+set.String(), map[string]any{"action": "unpublish"}, 400)

		// Trashed items cannot be added.
		gone := n.create("article", "gone", "en", `{"title":"Gone"}`, false)
		if err := n.app.Services().Content.Trash(ctx, gone); err != nil {
			t.Fatal(err)
		}
		deq(t, "trashed code", field(n.call("", "PUT", "/releases/"+id+"/items/"+gone.String(), map[string]any{}, 409).json(), "error", "code"), "trashed")

		n.call("", "DELETE", "/releases/"+id, nil, 204)
		n.call("", "GET", "/releases/"+id, nil, 404)
		deq(t, "audit", strings.Join(n.auditActions(id), ","), "release.create,release.update,release.update,release.update,release.update,release.update,release.delete")

		// The OpenAPI document lists the routes.
		spec := n.call("", "GET", "/openapi.json", nil, 200).json()
		for _, p := range []string{"/releases", "/releases/{id}", "/releases/{id}/items/{item}", "/releases/{id}/schedule",
			"/releases/{id}/unschedule", "/releases/{id}/cancel", "/releases/{id}/publish", "/items/{id}/releases"} {
			if field(spec, "paths", p) == nil {
				t.Errorf("openapi lacks %s", p)
			}
		}
	})
}

func TestReleasePublishIsAtomic(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		good := n.create("article", "good", "en", `{"title":"Good"}`, false)
		bad := n.create("person", "bad", "en", `{}`, false) // a required field is missing
		current := n.create("article", "current", "en", `{"title":"Current"}`, true)
		trashed := n.create("article", "trashed", "en", `{"title":"Trashed"}`, false)

		id := n.newRelease("", "Mixed", ritem(good, "publish"), ritem(bad, "publish"), ritem(current, "publish"), ritem(trashed, "publish"))
		if err := n.app.Services().Content.Trash(ctx, trashed); err != nil {
			t.Fatal(err)
		}
		n.drain()
		before := n.probe.topics()

		r := n.call("", "POST", "/releases/"+id+"/publish", nil, 422).json()
		deq(t, "code", field(r, "error", "code"), "release_failed")
		rel := r["release"].(map[string]any)
		deq(t, "status", rel["status"], "failed")
		deq(t, "errors", strings.Join(errorCodes(rel), " "), strings.Join(sorted(
			bad.String()+":invalid", current.String()+":nothing_to_publish", trashed.String()+":trashed"), " "))
		for _, e := range rel["errors"].([]any) {
			m := e.(map[string]any)
			if m["item"] == bad.String() && m["path"] != "fullName" {
				t.Errorf("path = %v", m["path"])
			}
		}

		// Nothing was published, and nothing was announced.
		if n.live(good) {
			t.Fatal("good item went live although the release failed")
		}
		n.drain()
		deq(t, "no events", len(n.probe.topics()), len(before))
		deq(t, "state kept", n.status(id), "failed")
		if got := n.auditActions(good.String()); slices.Contains(got, "item.publish") {
			t.Errorf("audit of good = %v", got)
		}
		deq(t, "audit", strings.Join(n.auditActions(id), ","), "release.create,release.fail")

		// Fix what is wrong: drop the broken items and publish again (failed releases publish directly).
		n.call("", "DELETE", "/releases/"+id+"/items/"+bad.String(), nil, 204)
		deq(t, "reopened", n.status(id), "open")
		n.call("", "DELETE", "/releases/"+id+"/items/"+current.String(), nil, 204)
		n.call("", "DELETE", "/releases/"+id+"/items/"+trashed.String(), nil, 204)
		n.update(bad, "en", `{"fullName":"Pat"}`)
		n.call("", "PUT", "/releases/"+id+"/items/"+bad.String(), map[string]any{}, 200)
		pub := n.call("", "POST", "/releases/"+id+"/publish", nil, 200).json()
		deq(t, "published", pub["status"], "published")
		if pub["publishedAt"] == nil || len(pub["errors"].([]any)) != 0 {
			t.Errorf("published release = %v", pub)
		}
		if !n.live(good) {
			t.Error("good item is not live")
		}
		if n.get("/items/"+bad.String()+"?channel=people").Code != 200 {
			t.Error("fixed person is not live")
		}
		n.drain()
		if !slices.Contains(n.probe.topics(), "release.published") {
			t.Errorf("topics = %v", n.probe.topics())
		}
		deq(t, "item audit", strings.Join(n.auditActions(good.String()), ","), "item.create,item.publish")
		acts := n.auditActions(id)
		if acts[len(acts)-1] != "release.publish" || !slices.Contains(acts, "release.fail") {
			t.Errorf("audit = %v", acts)
		}

		// A published release is final.
		n.call("", "POST", "/releases/"+id+"/publish", nil, 409)
		n.call("", "POST", "/releases/"+id+"/cancel", nil, 409)
		n.call("", "POST", "/releases/"+id+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 409)
		n.call("", "PATCH", "/releases/"+id, map[string]any{"name": "x"}, 409)
		n.call("", "PUT", "/releases/"+id+"/items/"+current.String(), map[string]any{}, 409)
		n.call("", "DELETE", "/releases/"+id, nil, 409)
	})
}

func sorted(s ...string) []string { slices.Sort(s); return s }

func TestReleaseMixedActions(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "c", "en", `{"title":"v1"}`, true)
		n.update(art, "en", `{"title":"v2"}`)
		other := n.create("article", "c2", "en", `{"title":"unrelated"}`, true)
		id := n.newRelease("", "Mixed", ritem(art, "publish"), ritem(other, "unpublish"))
		r := n.call("", "POST", "/releases/"+id+"/publish", map[string]any{"force": true}, 200).json()
		deq(t, "status", r["status"], "published")
		if n.get("/items/"+other.String()+"?channel=web").Code != 404 {
			t.Error("unpublish action did not take the item offline")
		}
		v, err := n.app.Services().Content.Get(ctx, art, content.GetOptions{})
		if err != nil || v.Name != "v2" || v.Version != 2 {
			t.Fatalf("published = %+v, %v", v, err)
		}
	})
}

func TestReleaseUnpublishAction(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		live := n.create("article", "live", "en", `{"title":"Live"}`, true)
		never := n.create("article", "never", "en", `{"title":"Never"}`, false)
		if !n.live(live) {
			t.Fatal("not live")
		}
		id := n.newRelease("", "Take down", ritem(live, "unpublish"), ritem(never, "unpublish"))
		n.call("", "POST", "/releases/"+id+"/publish", nil, 200)
		if n.live(live) {
			t.Fatal("still live after the unpublish release")
		}
		v, err := n.app.Services().Content.Get(ctx, live, content.GetOptions{Draft: true})
		vs, verr := n.app.Services().Content.Versions(ctx, live)
		if err != nil || verr != nil || v.Status != "unpublished" || len(vs) != 1 {
			t.Fatalf("history lost: %+v %v %v %v", v, err, vs, verr)
		}
		// The never-published item is a no-op, not an error.
		deq(t, "status", n.status(id), "published")
	})
}

func TestReleaseSchedule(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		tok, _, secret := n.releaseToken("scheduler", editorPolicies()...)
		art := n.create("article", "s", "en", `{"title":"Scheduled"}`, false)
		id := n.newRelease(secret, "Later", ritem(art, "publish"))
		at := n.clk.Now().Add(2 * time.Hour)

		// Validation: the past, too far ahead, no items, wrong state.
		n.call(secret, "POST", "/releases/"+id+"/schedule", map[string]any{"at": n.clk.Now().Add(-time.Minute)}, 400)
		n.call(secret, "POST", "/releases/"+id+"/schedule", map[string]any{"at": n.clk.Now().Add(400 * 24 * time.Hour)}, 400)
		n.call(secret, "POST", "/releases/"+id+"/schedule", map[string]any{}, 400)
		empty := n.newRelease(secret, "Empty")
		n.call(secret, "POST", "/releases/"+empty+"/schedule", map[string]any{"at": at}, 400)
		n.call(secret, "POST", "/releases/"+empty+"/publish", nil, 400)
		n.call(secret, "POST", "/releases/"+id+"/unschedule", nil, 409)

		s := n.call(secret, "POST", "/releases/"+id+"/schedule", map[string]any{"at": at}, 200).json()
		deq(t, "status", s["status"], "scheduled")
		deq(t, "scheduledBy", s["scheduledBy"], "token:"+tok.ID.String())
		deq(t, "scheduledAt", s["scheduledAt"], at.UTC().Format("2006-01-02T15:04:05Z"))
		deq(t, "createdByName", s["createdByName"], "scheduler")

		// A scheduled release does not take item edits; unschedule reopens it; the item list shows it.
		n.call(secret, "PUT", "/releases/"+id+"/items/"+art.String(), map[string]any{}, 409)
		n.call(secret, "DELETE", "/releases/"+id, nil, 409)
		deq(t, "of item", len(n.call(secret, "GET", "/items/"+art.String()+"/releases", nil, 200).json()["items"].([]any)), 1)
		deq(t, "unscheduled", n.call(secret, "POST", "/releases/"+id+"/unschedule", nil, 200).json()["status"], "open")
		n.call(secret, "POST", "/releases/"+id+"/schedule", map[string]any{"at": at}, 200)

		// Not due yet: the job does nothing.
		n.clk.Advance(time.Hour)
		ran, err := n.svc().RunDue(ctx)
		if err != nil || ran != 0 {
			t.Fatalf("RunDue early = %d, %v", ran, err)
		}
		if n.live(art) {
			t.Fatal("published early")
		}

		// Due: the job (through the runner) publishes it as the token that scheduled it.
		n.clk.Advance(time.Hour + time.Second)
		n.app.TickJobs(ctx)
		deq(t, "status", n.status(id), "published")
		if !n.live(art) {
			t.Fatal("not live after the due time")
		}
		deq(t, "release actor", n.auditActor(id, "release.publish"), "token:"+tok.ID.String())
		deq(t, "item actor", n.auditActor(art.String(), "item.publish"), "token:"+tok.ID.String())
		if !slices.Contains(n.app.JobNames(), kernel.ReleasesJob) {
			t.Errorf("jobs = %v", n.app.JobNames())
		}
		// Running again publishes nothing more.
		ran, err = n.svc().RunDue(ctx)
		deq(t, "second run", ran, 0)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestReleaseSchedulerLosesRights(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		s := n.app.Services()
		art := n.create("article", "r", "en", `{"title":"Rights"}`, false)

		// Case 1: the token is revoked after scheduling.
		tok1, _, sec1 := n.releaseToken("gone", editorPolicies()...)
		r1 := n.newRelease(sec1, "Gone", ritem(art, "publish"))
		n.call(sec1, "POST", "/releases/"+r1+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		if err := s.Admin.RevokeToken(ctx, tok1.ID); err != nil {
			t.Fatal(err)
		}

		// Case 2: the token keeps release.manage but loses content.publish.
		_, role2, sec2 := n.releaseToken("demoted", editorPolicies()...)
		r2 := n.newRelease(sec2, "Demoted", ritem(art, "publish"))
		n.call(sec2, "POST", "/releases/"+r2+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		keep := []access.RolePolicy{rolePolicy("content.read", nil), rolePolicy("release.manage", nil)}
		if _, err := s.Admin.UpdateRole(ctx, role2.ID, access.RolePatch{Policies: &keep}); err != nil {
			t.Fatal(err)
		}

		// Case 3: the token loses release.manage.
		_, role3, sec3 := n.releaseToken("unmanaged", editorPolicies()...)
		r3 := n.newRelease(sec3, "Unmanaged", ritem(art, "publish"))
		n.call(sec3, "POST", "/releases/"+r3+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		keep = []access.RolePolicy{rolePolicy("content.read", nil), rolePolicy("content.publish", nil)}
		if _, err := s.Admin.UpdateRole(ctx, role3.ID, access.RolePatch{Policies: &keep}); err != nil {
			t.Fatal(err)
		}

		n.clk.Advance(2 * time.Hour)
		ran, err := n.svc().RunDue(ctx)
		if err != nil || ran != 3 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		if n.live(art) {
			t.Fatal("published although every scheduler lost rights")
		}
		for _, id := range []string{r1, r2, r3} {
			rel := n.call("", "GET", "/releases/"+id, nil, 200).json()
			deq(t, "status "+id, rel["status"], "failed")
			errs := rel["errors"].([]any)
			if len(errs) != 1 || errs[0].(map[string]any)["code"] != "forbidden" {
				t.Errorf("errors of %s = %v", id, errs)
			}
		}
		// The demoted scheduler's error names the item.
		rel := n.call("", "GET", "/releases/"+r2, nil, 200).json()
		deq(t, "item error", rel["errors"].([]any)[0].(map[string]any)["item"], art.String())
		deq(t, "audit", n.auditActions(r2)[len(n.auditActions(r2))-1], "release.fail")

		// A failed scheduled release can be fixed by an administrator and published directly.
		n.call("", "POST", "/releases/"+r2+"/publish", nil, 200)
		if !n.live(art) {
			t.Fatal("not live after the manual publish")
		}
	})
}

func TestReleaseCancelAndStates(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "x", "en", `{"title":"X"}`, false)
		id := n.newRelease("", "Cancelled", ritem(art, "publish"))
		n.call("", "POST", "/releases/"+id+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		deq(t, "cancel", n.call("", "POST", "/releases/"+id+"/cancel", nil, 200).json()["status"], "cancelled")
		for _, c := range [][3]any{{"POST", "/publish", nil}, {"POST", "/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}},
			{"POST", "/unschedule", nil}, {"POST", "/cancel", nil}, {"PATCH", "", map[string]any{"name": "n"}}} {
			n.call("", c[0].(string), "/releases/"+id+c[1].(string), c[2], 409)
		}
		n.call("", "PUT", "/releases/"+id+"/items/"+art.String(), map[string]any{}, 409)
		// The job leaves it alone.
		n.clk.Advance(2 * time.Hour)
		if ran, err := n.svc().RunDue(ctx); err != nil || ran != 0 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		if n.live(art) {
			t.Fatal("cancelled release published")
		}
		deq(t, "error code", field(n.call("", "POST", "/releases/"+id+"/publish", nil, 409).json(), "error", "code"), "release_state")
		n.call("", "DELETE", "/releases/"+id, nil, 204)
		deq(t, "audit", strings.Join(n.auditActions(id), ","), "release.create,release.schedule,release.cancel,release.delete")
	})
}

func TestReleaseStuckPublishingRunsAgain(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "st", "en", `{"title":"Stuck"}`, false)
		_, _, secret := n.releaseToken("crashy", editorPolicies()...)
		id := n.newRelease(secret, "Stuck", ritem(art, "publish"))
		rid, _ := ids.Parse(id)
		tokID := n.auditActor(id, "release.create")

		// A crashed run leaves the release in publishing, last touched by its principal.
		err := n.app.Services().DB.Tx(ctx, func(tx store.Tx) error {
			r, err := tx.Releases().GetRelease(ctx, rid)
			if err != nil {
				return err
			}
			r.Status, r.UpdatedBy, r.UpdatedAt = "publishing", tokID, n.clk.Now()
			return tx.Releases().UpdateRelease(ctx, r)
		})
		if err != nil {
			t.Fatal(err)
		}
		n.call("", "POST", "/releases/"+id+"/publish", nil, 409) // being published
		n.call("", "PATCH", "/releases/"+id, map[string]any{"name": "x"}, 409)

		n.clk.Advance(5 * time.Minute)
		if ran, _ := n.svc().RunDue(ctx); ran != 0 {
			t.Fatalf("ran a release that is not stuck yet: %d", ran)
		}
		n.clk.Advance(6 * time.Minute)
		if ran, err := n.svc().RunDue(ctx); err != nil || ran != 1 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		deq(t, "status", n.status(id), "published")
		if !n.live(art) {
			t.Fatal("not live")
		}
	})
}

func TestReleaseAccess(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "acc", "en", `{"title":"Acc"}`, false)
		_, _, viewer := n.releaseToken("viewer", rolePolicy("content.read", nil))
		_, _, contributor := n.releaseToken("contributor", rolePolicy("content.read", nil), rolePolicy("content.edit", nil), rolePolicy("release.manage", nil))
		_, _, nobody := n.releaseToken("nobody", rolePolicy("content.preview", nil))

		id := n.newRelease("", "Guarded", ritem(art, "publish"))
		// Reading needs content.read, writing release.manage.
		n.call(viewer, "GET", "/releases", nil, 200)
		n.call(viewer, "GET", "/releases/"+id, nil, 200)
		n.call(nobody, "GET", "/releases", nil, 403)
		n.call(nobody, "GET", "/items/"+art.String()+"/releases", nil, 403)
		for _, c := range [][2]string{{"POST", "/releases"}, {"PATCH", "/releases/" + id}, {"DELETE", "/releases/" + id},
			{"PUT", "/releases/" + id + "/items/" + art.String()}, {"POST", "/releases/" + id + "/publish"}, {"POST", "/releases/" + id + "/cancel"}} {
			n.call(viewer, c[0], c[1], map[string]any{"name": "x"}, 403)
		}
		// release.manage does not publish: content.publish is checked per item.
		r := n.call(contributor, "POST", "/releases/"+id+"/publish", nil, 422).json()
		errs := r["release"].(map[string]any)["errors"].([]any)
		if len(errs) != 1 || errs[0].(map[string]any)["code"] != "forbidden" || errs[0].(map[string]any)["item"] != art.String() {
			t.Fatalf("errors = %v", errs)
		}
		if n.live(art) {
			t.Fatal("published without content.publish")
		}
		// Items the caller cannot read stay out of releases.
		_, _, people := n.releaseToken("people", rolePolicy("content.read", map[string][]string{"collection": {"person"}}), rolePolicy("release.manage", nil))
		n.call(people, "PUT", "/releases/"+id+"/items/"+art.String(), map[string]any{}, 404)
		hidden := n.call(people, "GET", "/releases/"+id, nil, 200).json()["items"].([]any)[0].(map[string]any)
		deq(t, "hidden state", hidden["state"], "hidden")
		deq(t, "hidden name", hidden["name"], "")
	})
}

func TestReleaseTemplatesGrantManage(t *testing.T) {
	for _, tpl := range access.Templates() {
		has := false
		for _, p := range tpl.Policies {
			has = has || p.Policy == access.PolicyReleaseManage || p.Policy == access.PolicyAll
		}
		if want := tpl.Name == "editor" || tpl.Name == "administrator"; has != want {
			t.Errorf("template %s grants release.manage = %v", tpl.Name, has)
		}
	}
}

func TestReleaseCreateScheduledAndItemKind(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "one", "en", `{"title":"One"}`, false)
		at := n.clk.Now().Add(time.Hour)
		r := n.call("", "POST", "/releases", map[string]any{"name": "One — publish", "kind": "item", "scheduledAt": at,
			"items": []any{ritem(art, "publish")}}, 201).json()
		deq(t, "status", r["status"], "scheduled")
		deq(t, "kind", r["kind"], "item")
		deq(t, "list kind", n.call("", "GET", "/releases?kind=item", nil, 200).json()["total"], float64(1))
		deq(t, "list regular", n.call("", "GET", "/releases?kind=release", nil, 200).json()["total"], float64(0))
		n.call("", "POST", "/releases", map[string]any{"name": "e", "scheduledAt": at}, 400)
		n.clk.Advance(time.Hour + time.Second)
		if ran, err := n.svc().RunDue(ctx); err != nil || ran != 1 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		if !n.live(art) {
			t.Fatal("not live")
		}
	})
}

// Two nodes race to publish the same releases: exactly one wins every time, the loser gets a 409, and a release
// that was published is never turned into failed.
func TestReleaseConcurrentPublishAcrossNodes(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		clk := n.clk
		other, err := tryNode(t, n.app.Services().DB, clk, deliveryChannels)
		if err != nil {
			t.Fatal(err)
		}
		nodes := []*release.Service{n.svc(), other.app.Services().Releases}
		for round := range 6 {
			art := n.create("article", fmt.Sprintf("c%d", round), "en", `{"title":"Race"}`, false)
			id := n.newRelease("", "Race", ritem(art, "publish"))
			rid, _ := ids.Parse(id)
			errs := make([]error, len(nodes))
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i, svc := range nodes {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					_, errs[i] = svc.Publish(ctx, rid, false)
				}()
			}
			close(start)
			wg.Wait()
			wins := 0
			for _, err := range errs {
				var me *manage.Error
				switch {
				case err == nil:
					wins++
				case errors.As(err, &me) && me.Status == 409:
				default:
					t.Fatalf("round %d: unexpected error %v", round, err)
				}
			}
			if wins != 1 {
				t.Fatalf("round %d: %d publishes won: %v", round, wins, errs)
			}
			deq(t, "status", n.status(id), "published")
			if got := n.auditActions(id); slices.Contains(got, "release.fail") || strings.Count(strings.Join(got, ","), "release.publish") != 1 {
				t.Fatalf("round %d: audit = %v", round, got)
			}
			n.wantVersion(art, 1)
		}
	})
}

// A crashed run that two nodes pick up at once ends published, once.
func TestReleaseStuckRetryOnTwoNodes(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		other, err := tryNode(t, n.app.Services().DB, n.clk, deliveryChannels)
		if err != nil {
			t.Fatal(err)
		}
		nodes := []*release.Service{n.svc(), other.app.Services().Releases}
		art := n.create("article", "two", "en", `{"title":"Two"}`, false)
		id := n.newRelease("", "Stuck", ritem(art, "publish"))
		rid, _ := ids.Parse(id)
		err = n.app.Services().DB.Tx(ctx, func(tx store.Tx) error {
			r, err := tx.Releases().GetRelease(ctx, rid)
			if err != nil {
				return err
			}
			r.Status, r.ScheduledBy, r.UpdatedBy, r.UpdatedAt = "publishing", "system", "user:gone", n.clk.Now()
			return tx.Releases().UpdateRelease(ctx, r)
		})
		if err != nil {
			t.Fatal(err)
		}
		n.clk.Advance(11 * time.Minute)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, svc := range nodes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := svc.RunDue(ctx); err != nil {
					t.Error(err)
				}
			}()
		}
		close(start)
		wg.Wait()
		deq(t, "status", n.status(id), "published")
		if got := n.auditActions(id); slices.Contains(got, "release.fail") {
			t.Fatalf("audit = %v", got)
		}
		n.wantVersion(art, 1)
	})
}

// A scheduled release that can never start (its items were purged) fails once, with a reason.
func TestReleaseScheduledWithoutItemsFails(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "gone", "en", `{"title":"Gone"}`, false)
		id := n.newRelease("", "Empty soon", ritem(art, "publish"))
		n.call("", "POST", "/releases/"+id+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		s := n.app.Services()
		if err := s.Content.Trash(ctx, art); err != nil {
			t.Fatal(err)
		}
		if err := s.Content.Purge(ctx, art); err != nil {
			t.Fatal(err)
		}
		n.clk.Advance(2 * time.Hour)
		if ran, err := n.svc().RunDue(ctx); err != nil || ran != 1 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		rel := n.call("", "GET", "/releases/"+id, nil, 200).json()
		deq(t, "status", rel["status"], "failed")
		errs := rel["errors"].([]any)
		if len(errs) != 1 || errs[0].(map[string]any)["code"] != "no_items" {
			t.Fatalf("errors = %v", errs)
		}
		if ran, err := n.svc().RunDue(ctx); err != nil || ran != 0 {
			t.Fatalf("second RunDue = %d, %v", ran, err)
		}
		acts := n.auditActions(id)
		deq(t, "audit", acts[len(acts)-1], "release.fail")
	})
}

// With access control on, a release cannot run as a principal the server has no way to load.
func TestReleaseRunFailsClosedWithoutAuthenticator(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		s := n.app.Services()
		_, _, secret := n.releaseToken("sched", editorPolicies()...)
		art := n.create("article", "fc", "en", `{"title":"Closed"}`, false)
		id := n.newRelease(secret, "Closed", ritem(art, "publish"))
		n.call(secret, "POST", "/releases/"+id+"/schedule", map[string]any{"at": n.clk.Now().Add(time.Hour)}, 200)
		blind, err := release.New(release.Options{DB: s.DB, Content: s.Content, Access: s.Access, Outbox: s.Outbox,
			Audit: s.Audit, Clock: s.Clock, IDs: s.IDs})
		if err != nil {
			t.Fatal(err)
		}
		n.clk.Advance(2 * time.Hour)
		if ran, err := blind.RunDue(ctx); err != nil || ran != 1 {
			t.Fatalf("RunDue = %d, %v", ran, err)
		}
		if n.live(art) {
			t.Fatal("published as an unknown principal")
		}
		rel := n.call("", "GET", "/releases/"+id, nil, 200).json()
		deq(t, "status", rel["status"], "failed")
		deq(t, "code", rel["errors"].([]any)[0].(map[string]any)["code"], "forbidden")
	})
}

// Release views and item lookups use the full read check of the item, and the list gets its counts in bulk.
func TestReleaseItemReadChecks(t *testing.T) {
	runRelease(t, func(t *testing.T, n *relNode) {
		art := n.create("article", "rc", "en", `{"title":"Readable"}`, false)
		person := n.create("person", "rcp", "en", `{"fullName":"Pat"}`, false)
		id := n.newRelease("", "Mixed", ritem(art, "publish"), ritem(person, "publish"))
		_, _, people := n.releaseToken("people", rolePolicy("content.read", map[string][]string{"collection": {"person"}}))
		// The item lookup of an item the caller cannot read is a 404; a readable one lists the release.
		n.call(people, "GET", "/items/"+art.String()+"/releases", nil, 404)
		deq(t, "of person", len(n.call(people, "GET", "/items/"+person.String()+"/releases", nil, 200).json()["items"].([]any)), 1)
		n.call("", "GET", "/items/"+ids.UUID{1}.String()+"/releases", nil, 404)
		list := n.call(people, "GET", "/releases", nil, 200).json()["items"].([]any)
		deq(t, "count", list[0].(map[string]any)["itemCount"], float64(2))
		detail := n.call("", "GET", "/releases/"+id, nil, 200).json()["items"].([]any)
		names := []string{}
		for _, it := range detail {
			names = append(names, it.(map[string]any)["name"].(string))
		}
		slices.Sort(names)
		deq(t, "names", strings.Join(names, ","), "Pat,Readable")
	})
}

func (n *relNode) wantVersion(id ids.UUID, want int) {
	n.t.Helper()
	v, err := n.app.Services().Content.Get(ctx, id, content.GetOptions{})
	if err != nil {
		n.t.Fatal(err)
	}
	if v.Version != want {
		n.t.Fatalf("version = %d, want %d", v.Version, want)
	}
}
