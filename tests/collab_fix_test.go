package tests

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/store"
)

// setPolicies replaces the policies of the role tokenFor made for name.
func (n *dnode) setPolicies(name string, ps ...access.RolePolicy) {
	n.t.Helper()
	s := n.app.Services()
	r, err := s.Admin.RoleByName(ctx, name)
	if err != nil {
		n.t.Fatal(err)
	}
	if _, err := s.Admin.UpdateRole(ctx, r.ID, access.RolePatch{Policies: &ps}); err != nil {
		n.t.Fatal(err)
	}
}

// appendRaw adds count updates of size bytes to the item's log, as another node would have.
func (n *dnode) appendRaw(art string, count, size int) {
	n.t.Helper()
	id, perr := ids.Parse(art)
	if perr != nil {
		n.t.Fatal(perr)
	}
	err := n.app.Services().DB.Tx(ctx, func(tx store.Tx) error {
		for range count {
			if _, err := tx.Collab().AppendUpdate(ctx, store.CollabUpdate{Item: id, Client: "filler-client", Data: []byte(strings.Repeat("x", size)), At: n.clk.Now()}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		n.t.Fatal(err)
	}
}

// ends reports whether the stream ends (after whatever events are still queued).
func (s *sse) ends() bool {
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-s.ev:
			if !ok {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func (n *dnode) patchAs(tok, art string, hdr map[string]string, title string) resp {
	n.t.Helper()
	h := map[string]string{"Authorization": "Bearer " + tok}
	for k, v := range hdr {
		h[k] = v
	}
	n.clk.Advance(time.Second)
	return n.do("PATCH", "/api/manage/v1/items/"+art, h, map[string]any{"locale": "en", "fields": map[string]any{"title": title}})
}

func (n *dnode) seed(tok, art string, j map[string]any) {
	n.t.Helper()
	n.collab(tok, art, "updates", map[string]any{"client": j["client"], "data": b64("seed"), "init": []string{"shared", "l:en"}}, 200)
}

// A client named by a save is honoured only when it belongs to the principal that saved.
func TestCollabSavedRequiresOwnClient(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		n.seed(u[0], art, a)
		sa := openCollab(t, srv.URL, u[0], art, "client="+a["client"].(string))
		sa.until("presence")

		// Bob saves naming Alice's client: that is not a save of Alice's session.
		if r := n.patchAs(u[1], art, map[string]string{"X-Hydrogen-Collab-Client": a["client"].(string)}, "Spoof"); r.Code != 200 {
			t.Fatalf("patch: %d %s", r.Code, r.Body)
		}
		n.drain()
		deq(t, "spoofed save", sa.until("reset")["reason"], "external")
	})
}

func TestCollabSavedCarriesEventEtagAndSeq(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		n.seed(u[0], art, a)
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string)+"&since=0")
		sb.until("presence")

		hdr := map[string]string{"X-Hydrogen-Collab-Client": a["client"].(string), "X-Hydrogen-Collab-Seq": "7"}
		r1 := n.patchAs(u[0], art, hdr, "One")
		// Another write lands before the outbox delivers the first event: the saved etag is the save's, not now's.
		n.update(mustID(t, art), "en", `{"title":"Two"}`)
		n.drain()
		d := sb.until("saved")
		deq(t, "etag", d["etag"], r1.Header().Get("ETag"))
		deq(t, "seq", d["seq"], float64(7))
		deq(t, "later write resets", sb.until("reset")["reason"], "external")

		// Without the header the seq is 0.
		j := n.join(u[0], art, "en", true, 200)
		n.seed(u[0], art, j)
		s2 := openCollab(t, srv.URL, u[1], art, "client="+n.join(u[1], art, "en", false, 200)["client"].(string))
		s2.until("presence")
		n.patchAs(u[0], art, map[string]string{"X-Hydrogen-Collab-Client": j["client"].(string)}, "Three")
		n.drain()
		deq(t, "default seq", s2.until("saved")["seq"], float64(0))
	})
}

// A session seeded from an older draft is dropped by the next join; a late delivery of an older write does not
// kill a session made after it.
func TestCollabStaleSessionAndLateReset(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		n.seed(u[0], art, a)
		sa := openCollab(t, srv.URL, u[0], art, "client="+a["client"].(string))
		sa.until("presence")
		oldEtag := a["etag"].(string)

		// The draft changes and the event is not delivered yet: the next join finds the stale session and resets it.
		n.update(mustID(t, art), "en", `{"title":"API"}`)
		b := n.join(u[1], art, "en", true, 200)
		deq(t, "stale session reset", sa.until("reset")["reason"], "external")
		if len(b["updates"].([]any)) != 0 || len(strs(b["init"])) != 2 || b["etag"] == oldEtag {
			t.Fatalf("join after the draft changed: %v", b)
		}
		n.seed(u[1], art, b)
		// Delivering the outbox now (the write's own reset event, state <= the session's) leaves the new session alone.
		n.drain()
		if got := n.join(u[1], art, "en", false, 200); len(got["updates"].([]any)) != 1 {
			t.Fatalf("a late reset killed the new session: %v", got)
		}
		// So does a replay of an older write.
		old := fmt.Sprintf(`{"id":%q,"state":%q}`, art, strings.Trim(oldEtag, `"`))
		if err := n.app.Services().Collab.Consumer().Handle(ctx, outbox.Event{Topic: "item.updated", Payload: json.RawMessage(old)}); err != nil {
			t.Fatal(err)
		}
		if got := n.join(u[1], art, "en", false, 200); len(got["updates"].([]any)) != 1 {
			t.Fatalf("a replayed old event killed the new session: %v", got)
		}
		// A write later than the session still resets it.
		n.update(mustID(t, art), "en", `{"title":"Newer"}`)
		n.drain()
		if got := n.join(u[1], art, "en", false, 200); len(got["updates"].([]any)) != 0 {
			t.Fatalf("a later write left the log: %v", got)
		}
	})
}

func TestCollabUnpublishResets(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, true)
		u := n.editors("alice")
		a := n.join(u[0], art.String(), "en", true, 200)
		n.seed(u[0], art.String(), a)
		s := openCollab(t, srv.URL, u[0], art.String(), "client="+a["client"].(string))
		s.until("presence")
		n.clk.Advance(time.Second)
		if _, err := n.app.Services().Content.Unpublish(ctx, art); err != nil {
			t.Fatal(err)
		}
		n.drain()
		deq(t, "unpublish", s.until("reset")["reason"], "external")
	})
}

func TestCollabRightsRecheck(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		n.seed(u[0], art, a)
		sa := openCollab(t, srv.URL, u[0], art, "client="+a["client"].(string))
		sa.presenceWith(2)

		// Bob keeps reading but loses editing: his next check downgrades him.
		n.setPolicies("bob", rolePolicy("content.read", nil))
		n.clk.Advance(11 * time.Second)
		n.collab(u[1], art, "updates", map[string]any{"client": b["client"], "data": b64("x")}, 403)
		n.collab(u[1], art, "presence", map[string]any{"client": b["client"], "locale": "en"}, 204)
		for range 20 {
			cl := sa.until("presence")["clients"].([]any)
			var bob map[string]any
			for _, c := range cl {
				if c.(map[string]any)["client"] == b["client"] {
					bob = c.(map[string]any)
				}
			}
			if bob != nil && bob["edit"] == false {
				goto downgraded
			}
		}
		t.Fatal("bob stays an editor in presence")
	downgraded:
		// A viewer cannot reset either.
		n.collab(u[1], art, "reset", map[string]any{"client": b["client"]}, 403)

		// Alice loses the item (not the API): her heartbeat drops her and her stream is told.
		n.setPolicies("alice", rolePolicy("content.read", map[string][]string{"collection": {"person"}}), rolePolicy("content.edit", nil))
		n.clk.Advance(11 * time.Second)
		n.collab(u[0], art, "presence", map[string]any{"client": a["client"], "locale": "en"}, 404)
		deq(t, "dropped", sa.until("reset")["reason"], "expired")
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("x")}, 404)
	})
}

// The check also happens on updates when the last one is old enough.
func TestCollabRightsRecheckOnUpdate(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice")
		a := n.join(u[0], art, "en", true, 200)
		n.seed(u[0], art, a)
		n.setPolicies("alice", rolePolicy("content.read", map[string][]string{"collection": {"person"}}), rolePolicy("content.edit", nil))
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("x")}, 200) // checked < 10 s ago
		n.clk.Advance(11 * time.Second)
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("x")}, 404)
	})
}

func TestCollabPresenceLocaleRights(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		en := n.tokenFor("en-editor", rolePolicy("content.read", nil), rolePolicy("content.edit", map[string][]string{"locale": {"en"}}))
		a := n.join(en, art, "en", true, 200)
		n.join(en, art, "de", true, 403)
		n.collab(en, art, "presence", map[string]any{"client": a["client"], "locale": "de"}, 403)
		n.collab(en, art, "presence", map[string]any{"client": a["client"], "locale": "en", "focus": "title"}, 204)
		v := n.join(en, art, "de", false, 200)
		n.collab(en, art, "presence", map[string]any{"client": v["client"], "locale": "fr"}, 204) // read-only: reading is enough
	})
}

func TestCollabRateLimit(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		post := func(tok string, j map[string]any, want int) resp {
			return n.collab(tok, art, "updates", map[string]any{"client": j["client"], "data": b64("u")}, want)
		}
		for range 50 {
			post(u[0], a, 200)
		}
		r := post(u[0], a, 429)
		if code := r.json()["error"].(map[string]any)["code"]; code != "rate_limited" || r.Header().Get("Retry-After") == "" {
			t.Errorf("429 = %s retry-after %q", r.Body, r.Header().Get("Retry-After"))
		}
		post(u[1], b, 200) // another client has its own budget
		n.clk.Advance(500 * time.Millisecond)
		for range 10 {
			post(u[0], a, 200)
		}
		post(u[0], a, 429)
		n.clk.Advance(time.Second)
		post(u[0], a, 200)
	})
}

func TestCollabHardCapAndCompactionRequests(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string))
		sb.until("presence")
		n.clk.Advance(time.Second)
		n.collab(u[1], art, "presence", map[string]any{"client": b["client"], "locale": "en"}, 204) // bob is the most recent

		// Nobody asked for a compaction yet.
		n.collab(u[1], art, "compact", map[string]any{"client": b["client"], "data": b64("s"), "upto": 1}, 409)

		n.appendRaw(art, 2000, 1)
		r := n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("u")}, 409)
		if code := r.json()["error"].(map[string]any)["code"]; code != "compact_required" {
			t.Fatalf("hard cap: %s", r.Body)
		}
		d := sb.until("compact")
		upto := d["upto"].(float64)
		if upto < 2000 {
			t.Fatalf("compact upto %v", upto)
		}
		// Only the client asked may compact, and not beyond what was asked.
		n.collab(u[0], art, "compact", map[string]any{"client": a["client"], "data": b64("s"), "upto": upto}, 409)
		n.collab(u[1], art, "compact", map[string]any{"client": b["client"], "data": b64("s"), "upto": upto + 1}, 409)
		n.collab(u[1], art, "compact", map[string]any{"client": b["client"], "data": b64("s"), "upto": upto}, 204)
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("u")}, 200)
		// The request is spent.
		n.collab(u[1], art, "compact", map[string]any{"client": b["client"], "data": b64("s"), "upto": upto}, 409)
	})
}

func TestCollabJoinPages(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		n.appendRaw(art, 450, 1)
		a := n.join(u[0], art, "en", false, 200)
		ups := a["updates"].([]any)
		if len(ups) != 200 || a["more"] != true || a["seq"] != ups[199].(map[string]any)["seq"] {
			t.Fatalf("first page: %d updates, more %v, seq %v", len(ups), a["more"], a["seq"])
		}
		total, seq := len(ups), a["seq"].(float64)
		for more := true; more; {
			r := n.as(u[0], "GET", fmt.Sprintf("/collab/items/%s/updates?client=%s&since=%.0f", art, a["client"], seq), nil)
			if r.Code != 200 {
				t.Fatalf("page: %d %s", r.Code, r.Body)
			}
			p := r.json()
			got := p["updates"].([]any)
			if len(got) > 0 && got[0].(map[string]any)["seq"].(float64) <= seq {
				t.Fatalf("page repeats: %v after %v", got[0], seq)
			}
			total += len(got)
			seq, more = p["seq"].(float64), p["more"].(bool)
		}
		if total != 450 {
			t.Errorf("pages hold %d updates, want 450", total)
		}
		// Another client's id, or none, is refused.
		n.as(u[1], "GET", fmt.Sprintf("/collab/items/%s/updates?client=%s&since=0", art, a["client"]), nil)
		if r := n.as(u[1], "GET", fmt.Sprintf("/collab/items/%s/updates?client=%s&since=0", art, a["client"]), nil); r.Code != 404 {
			t.Errorf("page of another's client: %d", r.Code)
		}
		if r := n.as(u[0], "GET", "/collab/items/"+art+"/updates?client="+a["client"].(string), nil); r.Code != 400 {
			t.Errorf("page without since: %d", r.Code)
		}
	})
}

func TestCollabJoinPageBytes(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice")
		n.appendRaw(art, 3, 1<<20)
		a := n.join(u[0], art, "en", false, 200)
		if len(a["updates"].([]any)) != 2 || a["more"] != true {
			t.Fatalf("byte-limited page: %d updates, more %v", len(a["updates"].([]any)), a["more"])
		}
	})
}

func TestCollabResetRoute(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		n.seed(u[0], art, a)
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string))
		sb.until("presence")
		n.collab(u[1], art, "reset", map[string]any{"client": a["client"]}, 404) // not his client
		n.collab(u[1], art, "reset", map[string]any{"client": b["client"]}, 204)
		deq(t, "reset", sb.until("reset")["reason"], "external")
		got := n.join(u[0], art, "en", true, 200)
		if len(got["updates"].([]any)) != 0 || len(strs(got["init"])) != 2 {
			t.Errorf("session after reset: %v", got)
		}
		n.collab(u[0], art, "presence", map[string]any{"client": b["client"], "locale": "en"}, 404)
	})
}

func TestCollabStreamPerPrincipalCap(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		ed := n.editors("alice", "bob")
		var keep []*sse
		for i := range 6 {
			j := n.join(ed[0], art, "en", false, 200)
			s := openCollab(t, srv.URL, ed[0], art, "client="+j["client"].(string))
			if s.resp.StatusCode != 200 {
				t.Fatalf("stream %d: %d", i, s.resp.StatusCode)
			}
			keep = append(keep, s)
		}
		j := n.join(ed[0], art, "en", false, 200)
		if s := openCollab(t, srv.URL, ed[0], art, "client="+j["client"].(string)); s.resp.StatusCode != 429 {
			t.Errorf("7th stream of one principal: %d", s.resp.StatusCode)
		}
		// Another principal is not affected, and a client's reconnect replaces its stream instead of adding one.
		jb := n.join(ed[1], art, "en", false, 200)
		if s := openCollab(t, srv.URL, ed[1], art, "client="+jb["client"].(string)); s.resp.StatusCode != 200 {
			t.Errorf("other principal: %d", s.resp.StatusCode)
		}
		again := openCollab(t, srv.URL, ed[0], art, "client="+keep[0].resp.Request.URL.Query().Get("client"))
		if again.resp.StatusCode != 200 {
			t.Errorf("reconnect at the cap: %d", again.resp.StatusCode)
		}
		if !keep[0].ends() {
			t.Error("replaced stream stays open")
		}
	})
}

// Re-joining does not extend a claim, so a client cannot hold a part forever.
func TestCollabReclaimDoesNotExtend(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		n.clk.Advance(10 * time.Second)
		re := n.collab(u[0], art, "join", map[string]any{"locale": "en", "edit": true, "client": a["client"]}, 200).json()
		if len(strs(re["init"])) != 2 {
			t.Fatalf("re-join lost the claim: %v", re)
		}
		n.clk.Advance(6 * time.Second) // 16 s after the first claim
		if b := n.join(u[1], art, "en", true, 200); len(strs(b["init"])) != 2 {
			t.Fatalf("a re-claim extended the claim: %v", b)
		}
	})
}

// PostUpdate with init and a reset take the item's lock in the same order: they never deadlock.
func TestCollabInitPostVersusReset(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		for i := range 25 {
			n.clk.Advance(time.Second)
			a := n.join(u[0], art, "en", true, 200)
			b := n.join(u[1], art, "en", true, 200)
			var wg sync.WaitGroup
			codes := make([]int, 2)
			wg.Add(2)
			go func() {
				defer wg.Done()
				codes[0] = n.as(u[0], "POST", "/collab/items/"+art+"/updates", map[string]any{"client": a["client"], "data": b64("seed"), "init": []string{"shared", "l:en"}}).Code
			}()
			go func() {
				defer wg.Done()
				codes[1] = n.as(u[1], "POST", "/collab/items/"+art+"/reset", map[string]any{"client": b["client"]}).Code
			}()
			wg.Wait()
			if (codes[0] != 200 && codes[0] != 404 && codes[0] != 409) || (codes[1] != 204 && codes[1] != 404) {
				t.Fatalf("round %d: post %d, reset %d", i, codes[0], codes[1])
			}
		}
	})
}
