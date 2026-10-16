package tests

import (
	"bufio"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/store"
)

// collabNode is a delivery test node with the collaboration notifications running and an HTTP server.
func collabNode(t *testing.T, db store.DB, clk *clock.Manual, appYAML string) (*dnode, *httptest.Server) {
	t.Helper()
	n, err := tryNodeConfig(t, db, clk, shareChannels, appYAML)
	if err != nil {
		t.Fatal(err)
	}
	lctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if c := n.app.Services().Collab; c != nil {
		if err := c.Start(lctx); err != nil {
			t.Fatal(err)
		}
	}
	srv := httptest.NewServer(n.h)
	t.Cleanup(srv.Close)
	return n, srv
}

func runCollab(t *testing.T, fn func(t *testing.T, n *dnode, srv *httptest.Server)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n, srv := collabNode(t, b.open(t, clk), clk, "")
		fn(t, n, srv)
	})
}

func (n *dnode) collab(tok, item, verb string, body any, want int) resp {
	n.t.Helper()
	r := n.as(tok, "POST", "/collab/items/"+item+"/"+verb, body)
	if r.Code != want {
		n.t.Fatalf("POST collab %s: %d, want %d: %s", verb, r.Code, want, r.Body)
	}
	return r
}

func (n *dnode) join(tok, item, locale string, edit bool, want int) map[string]any {
	n.t.Helper()
	r := n.collab(tok, item, "join", map[string]any{"locale": locale, "edit": edit}, want)
	if want != 200 {
		return nil
	}
	return r.json()
}

func mustID(t *testing.T, s string) ids.UUID {
	t.Helper()
	id, err := ids.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func strs(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}

// editors makes n token holders that read and edit.
func (n *dnode) editors(names ...string) []string {
	var out []string
	for _, name := range names {
		out = append(out, n.tokenFor(name, rolePolicy("content.read", nil), rolePolicy("content.edit", nil)))
	}
	return out
}

func openCollab(t *testing.T, base, tok, item, query string) *sse {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/api/manage/v1/collab/items/"+item+"/events?"+query, nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &sse{t: t, resp: resp, ev: make(chan [2]string, 256)}
	if resp.StatusCode != 200 {
		return s
	}
	go func() {
		defer close(s.ev)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<20)
		var name string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				s.ev <- [2]string{name, strings.TrimPrefix(line, "data: ")}
			}
		}
	}()
	return s
}

// until returns the data of the next event called name, skipping others.
func (s *sse) until(name string) map[string]any {
	s.t.Helper()
	for range 2000 {
		if ev, data := s.next(); ev == name {
			return data
		}
	}
	s.t.Fatalf("no %s event", name)
	return nil
}

// presenceWith waits for a presence event listing want clients.
func (s *sse) presenceWith(want int) []any {
	s.t.Helper()
	for range 50 {
		cl := s.until("presence")["clients"].([]any)
		if len(cl) == want {
			return cl
		}
	}
	s.t.Fatalf("no presence with %d clients", want)
	return nil
}

func TestCollabJoinClaimsOnePerPart(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, _ *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		if a["client"] == "" || a["client"] == b["client"] || a["session"] != art {
			t.Fatalf("join answers: %v %v", a, b)
		}
		if got := strs(a["init"]); !slices.Equal(got, []string{"l:en", "shared"}) && !slices.Equal(got, []string{"shared", "l:en"}) {
			t.Errorf("first joiner seeds %v", got)
		}
		if len(strs(a["pending"])) != 0 || len(strs(b["init"])) != 0 || len(strs(b["pending"])) != 2 {
			t.Errorf("second joiner: init %v pending %v", b["init"], b["pending"])
		}
		if !strings.HasPrefix(a["etag"].(string), `"`) || len(b["presence"].([]any)) != 2 {
			t.Errorf("etag %v presence %v", a["etag"], b["presence"])
		}
		// A third editor of another locale claims only that locale's part.
		c := n.join(u[1], art, "de", true, 200)
		if got := strs(c["init"]); !slices.Equal(got, []string{"l:de"}) {
			t.Errorf("de joiner seeds %v, want [l:de]", got)
		}

		// Seeding finishes the claim; a later joiner has nothing to wait for and gets the log.
		r := n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("seed"), "init": []string{"shared", "l:en"}}, 200)
		seq := r.json()["seq"].(float64)
		d := n.join(u[0], art, "en", true, 200)
		ups := d["updates"].([]any)
		if len(strs(d["init"]))+len(strs(d["pending"])) != 0 || len(ups) != 1 || d["seq"] != seq || field(ups[0].(map[string]any), "data") != b64("seed") {
			t.Errorf("late joiner: %v", d)
		}
		// Posting for parts you do not hold is refused.
		n.collab(u[1], art, "updates", map[string]any{"client": b["client"], "data": b64("x"), "init": []string{"shared"}}, 409)
	})
}

func TestCollabClaimExpires(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, _ *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		n.clk.Advance(10 * time.Second)
		if b := n.join(u[1], art, "en", true, 200); len(strs(b["init"])) != 0 {
			t.Fatalf("claim still held: %v", b)
		}
		n.clk.Advance(6 * time.Second)
		b := n.join(u[1], art, "en", true, 200)
		if len(strs(b["init"])) != 2 {
			t.Fatalf("expired claim not taken over: %v", b)
		}
		// The slow seeder lost its claim.
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("late"), "init": []string{"shared"}}, 409)

		// The cleanup job releases expired claims too, so a waiting client can claim by re-joining.
		n.clk.Advance(20 * time.Second)
		if err := n.app.Services().Collab.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		c := n.collab(u[0], art, "join", map[string]any{"locale": "en", "edit": true, "client": a["client"]}, 200).json()
		if c["client"] != a["client"] || len(strs(c["init"])) != 2 {
			t.Errorf("re-join: %v", c)
		}
	})
}

func TestCollabRelayAndOrder(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		sa := openCollab(t, srv.URL, u[0], art, "client="+a["client"].(string)+"&since=0")
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string)+"&since="+jsonNum(b["seq"]))
		sa.presenceWith(2)
		sb.presenceWith(2)

		var want []float64
		for i := range 5 {
			r := n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64(strings.Repeat("a", i+1))}, 200)
			want = append(want, r.json()["seq"].(float64))
		}
		for i, w := range want {
			d := sb.until("update")
			if d["seq"] != w || d["client"] != a["client"] || d["data"] != b64(strings.Repeat("a", i+1)) {
				t.Fatalf("update %d = %v, want seq %v", i, d, w)
			}
		}
		// Bob's update reaches Alice and is not echoed to Bob.
		r := n.collab(u[1], art, "updates", map[string]any{"client": b["client"], "data": b64("b")}, 200)
		d := sa.until("update")
		if d["seq"] != r.json()["seq"] || d["client"] != b["client"] {
			t.Errorf("alice got %v", d)
		}

		// A reconnecting stream resumes after Last-Event-ID / since without gaps or repeats.
		sb2 := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string)+"&since="+jsonNum(want[1]))
		for _, w := range want[2:] {
			if d := sb2.until("update"); d["seq"] != w {
				t.Fatalf("replay seq %v, want %v", d["seq"], w)
			}
		}
		// A client has one stream: the new one replaced the old, which ends without a reset.
		if !sb.ends() {
			t.Error("the older stream of the client stays open")
		}
		sb = sb2

		// Presence changes reach everyone; the focus is reported.
		n.collab(u[0], art, "presence", map[string]any{"client": a["client"], "locale": "en", "focus": "title", "idle": false}, 204)
		for range 20 {
			cl := sb.until("presence")["clients"].([]any)
			if f := field(cl[0].(map[string]any), "focus"); f == "title" {
				if field(cl[0].(map[string]any), "name") == "" || field(cl[0].(map[string]any), "color") == "" {
					t.Errorf("entry = %v", cl[0])
				}
				goto left
			}
		}
		t.Fatal("focus never announced")
	left:
		n.collab(u[0], art, "leave", map[string]any{"client": a["client"]}, 204)
		n.collab(u[0], art, "leave", map[string]any{"client": a["client"]}, 204) // idempotent
		sb.presenceWith(1)
		deq(t, "left client told", sa.until("reset")["reason"], "expired")
		if !sa.closed() {
			t.Error("the left client's stream stays open")
		}
		// An unknown client is told to join again.
		n.collab(u[0], art, "presence", map[string]any{"client": a["client"], "locale": "en"}, 404)
	})
}

func jsonNum(v any) string {
	f, _ := v.(float64)
	return strconv.FormatInt(int64(f), 10)
}

func TestCollabAccess(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		reader := n.tokenFor("reader", rolePolicy("content.read", nil))
		nobody := n.tokenFor("nobody", rolePolicy("content.preview", nil))
		editor := n.editors("editor")[0]

		n.join(nobody, art, "en", false, 403)
		n.join(reader, art, "en", true, 403)
		n.join(reader, "00000000-0000-7000-8000-000000000001", "en", false, 404)
		n.join(reader, "nope", "en", false, 404)
		n.collab(reader, art, "join", map[string]any{"locale": "EN!"}, 400)

		// A viewer sees the log and presence but cannot post.
		e := n.join(editor, art, "en", true, 200)
		v := n.join(reader, art, "en", false, 200)
		if len(strs(v["init"])) != 0 || len(strs(v["pending"])) != 2 {
			t.Errorf("viewer: %v", v)
		}
		n.collab(reader, art, "updates", map[string]any{"client": v["client"], "data": b64("x")}, 403)
		n.collab(reader, art, "compact", map[string]any{"client": v["client"], "data": b64("x"), "upto": 1}, 403)
		// Clients belong to their principal.
		n.collab(reader, art, "updates", map[string]any{"client": e["client"], "data": b64("x")}, 404)
		n.collab(reader, art, "leave", map[string]any{"client": e["client"]}, 204)
		n.collab(editor, art, "presence", map[string]any{"client": e["client"], "locale": "en"}, 204)
		if s := openCollab(t, srv.URL, reader, art, "client="+e["client"].(string)); s.resp.StatusCode != 404 {
			t.Errorf("stream of another's client: %d", s.resp.StatusCode)
		}
		if s := openCollab(t, srv.URL, reader, art, "client=short"); s.resp.StatusCode != 400 {
			t.Errorf("bad client: %d", s.resp.StatusCode)
		}
		// Limits.
		n.collab(editor, art, "updates", map[string]any{"client": e["client"], "data": b64(strings.Repeat("x", 512<<10+1))}, 413)
		n.collab(editor, art, "updates", map[string]any{"client": e["client"], "data": ""}, 400)
		n.collab(editor, art, "updates", map[string]any{"client": e["client"], "data": b64("x"), "init": []string{"bogus"}}, 400)
		// The right is checked again by the stream, and an unauthenticated call is refused.
		if r := n.do("POST", "/api/manage/v1/collab/items/"+art+"/join", nil, map[string]any{"locale": "en"}); r.Code != 401 {
			t.Errorf("anonymous join: %d", r.Code)
		}
		// A trashed item cannot be joined.
		n.manage("DELETE", "/items/"+art, nil)
		n.join(editor, art, "en", false, 404)
	})
}

func TestCollabSavedAndReset(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("seed"), "init": []string{"shared", "l:en"}}, 200)
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string)+"&since=0")
		sb.until("presence")
		hdr := func(tok, client string) map[string]string {
			return map[string]string{"Authorization": "Bearer " + tok, "X-Hydrogen-Collab-Client": client}
		}

		// A save by a participant: saved with the new etag, the session and its log stay.
		n.clk.Advance(time.Second)
		r := n.do("PATCH", "/api/manage/v1/items/"+art, hdr(u[0], a["client"].(string)), map[string]any{"locale": "en", "fields": map[string]any{"title": "Saved"}})
		if r.Code != 200 {
			t.Fatalf("patch: %d %s", r.Code, r.Body)
		}
		n.drain()
		d := sb.until("saved")
		deq(t, "etag", d["etag"], r.Header().Get("ETag"))
		deq(t, "client", d["client"], a["client"])
		deq(t, "name", d["name"], "alice")
		if len(n.join(u[0], art, "en", true, 200)["updates"].([]any)) != 1 {
			t.Error("a save dropped the log")
		}

		// A save naming a client that is not in the session resets it instead.
		n.clk.Advance(time.Second)
		n.do("PATCH", "/api/manage/v1/items/"+art, hdr(u[0], "unknown-client-1"), map[string]any{"locale": "en", "fields": map[string]any{"title": "Spoof"}})
		n.drain()
		deq(t, "spoofed client", sb.until("reset")["reason"], "external")
		if !sb.closed() {
			t.Error("stream open after reset")
		}
		if got := n.join(u[1], art, "en", true, 200); len(got["updates"].([]any)) != 0 || len(strs(got["init"])) != 2 {
			t.Errorf("reset kept the log: %v", got)
		}
	})
}

func TestCollabExternalWritesReset(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		u := n.editors("alice")
		for name, act := range map[string]func(art ids.UUID){
			"api write": func(art ids.UUID) { n.update(art, "en", `{"title":"API"}`) },
			"publish":   func(art ids.UUID) { n.publish(art) },
			"discard": func(art ids.UUID) {
				n.update(art, "en", `{"title":"Again"}`)
				if err := n.app.Services().Content.Discard(ctx, art, nil); err != nil {
					t.Fatal(err)
				}
			},
			"trash": func(art ids.UUID) { n.manage("DELETE", "/items/"+art.String(), nil) },
		} {
			art := n.create("article", "k-"+strings.ReplaceAll(name, " ", "-"), "en", `{"title":"T"}`, false)
			a := n.join(u[0], art.String(), "en", true, 200)
			n.collab(u[0], art.String(), "updates", map[string]any{"client": a["client"], "data": b64("s"), "init": []string{"shared", "l:en"}}, 200)
			s := openCollab(t, srv.URL, u[0], art.String(), "client="+a["client"].(string))
			s.until("presence")
			n.clk.Advance(time.Second)
			act(art)
			n.drain()
			want := "external"
			if name == "trash" {
				want = "trashed"
			}
			deq(t, name+" reason", s.until("reset")["reason"], want)
			if name != "trash" {
				v := n.join(u[0], art.String(), "en", false, 200)
				if v["seq"] != float64(0) || len(v["updates"].([]any)) != 0 {
					t.Errorf("%s kept the log: %v", name, v)
				}
			}
		}
	})
}

func TestCollabCompact(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		sa := openCollab(t, srv.URL, u[0], art, "client="+a["client"].(string))
		sa.until("presence")
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string))
		sb.until("presence")
		var last float64
		for i := range 301 {
			if i%40 == 0 {
				n.clk.Advance(3 * time.Second) // the rate limit refills
			}
			last = n.collab(u[1], art, "updates", map[string]any{"client": b["client"], "data": b64("u")}, 200).json()["seq"].(float64)
		}
		// The most recently active editor is asked, once the log passed 300 updates.
		n.collab(u[0], art, "presence", map[string]any{"client": a["client"], "locale": "en"}, 204)
		n.clk.Advance(11 * time.Second)
		n.collab(u[1], art, "updates", map[string]any{"client": b["client"], "data": b64("u")}, 200)
		d := sa.until("compact")
		upto := d["upto"].(float64)
		if upto < last {
			t.Fatalf("compact upto %v < %v", upto, last)
		}
		n.collab(u[0], art, "compact", map[string]any{"client": a["client"], "data": b64("state"), "upto": upto}, 204)
		j := n.join(u[1], art, "en", true, 200)
		ups := j["updates"].([]any)
		if len(ups) > 2 {
			t.Fatalf("log after compaction has %d updates", len(ups))
		}
		if first := ups[0].(map[string]any); first["data"] != b64("state") || first["client"] != a["client"] {
			t.Errorf("state update = %v", first)
		}
		n.collab(u[0], art, "compact", map[string]any{"client": a["client"], "data": b64("x"), "upto": 0}, 400)
	})
}

func TestCollabCleanup(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, srv *httptest.Server) {
		if !slices.Contains(n.app.JobNames(), "collab.cleanup") {
			t.Fatalf("jobs = %v", n.app.JobNames())
		}
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		u := n.editors("alice", "bob")
		a := n.join(u[0], art, "en", true, 200)
		b := n.join(u[1], art, "en", true, 200)
		n.collab(u[0], art, "updates", map[string]any{"client": a["client"], "data": b64("s"), "init": []string{"shared", "l:en"}}, 200)
		sa := openCollab(t, srv.URL, u[0], art, "client="+a["client"].(string))
		sb := openCollab(t, srv.URL, u[1], art, "client="+b["client"].(string))
		sa.presenceWith(2)
		sb.presenceWith(2)

		// Bob keeps beating; Alice goes silent for 45 s and is dropped, her stream told.
		n.clk.Advance(30 * time.Second)
		n.collab(u[1], art, "presence", map[string]any{"client": b["client"], "locale": "en"}, 204)
		n.clk.Advance(20 * time.Second)
		n.app.TickJobs(ctx)
		deq(t, "expired", sa.until("reset")["reason"], "expired")
		sb.presenceWith(1)
		n.collab(u[0], art, "presence", map[string]any{"client": a["client"], "locale": "en"}, 404)

		// The last one out ends the session with its log: saving is manual, so unsaved changes leave with them.
		n.collab(u[1], art, "leave", map[string]any{"client": b["client"]}, 204)
		got := n.join(u[1], art, "en", true, 200)
		if len(got["updates"].([]any)) != 0 || len(strs(got["init"])) != 2 {
			t.Fatalf("session kept after the last leave: %v", got)
		}
		// A client that vanishes without leaving (a crashed tab): dropped as stale, then the empty session goes.
		c := got["client"].(string)
		n.collab(u[1], art, "updates", map[string]any{"client": c, "data": b64("s2"), "init": []string{"shared", "l:en"}}, 200)
		n.clk.Advance(50 * time.Second)
		n.app.TickJobs(ctx)
		n.clk.Advance(3 * time.Minute)
		if err := n.app.Services().Collab.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
		if got := n.join(u[1], art, "en", true, 200); len(got["updates"].([]any)) != 0 || len(strs(got["init"])) != 2 {
			t.Errorf("idle session kept: %v", got)
		}
	})
}

func TestCollabStreamCapsAndDisabled(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n, srv := collabNode(t, b.open(t, clk), clk, "")
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		eds := n.editors("alice", "bob", "carol", "dave")
		ed := eds[0]
		var streams []*sse
		for i := range 20 {
			ed := eds[i%len(eds)]
			j := n.join(ed, art, "en", false, 200)
			s := openCollab(t, srv.URL, ed, art, "client="+j["client"].(string))
			if s.resp.StatusCode != 200 {
				t.Fatalf("stream %d: %d", i, s.resp.StatusCode)
			}
			streams = append(streams, s)
		}
		// The 21st client does not fit in the session.
		n.join(ed, art, "en", false, 429)
		_ = streams

		off, _ := collabNode(t, b.open(t, newClock()), newClock(), "collab: { enabled: false }\n")
		if off.app.Services().Collab != nil {
			t.Fatal("collab service built while disabled")
		}
		art2 := off.create("article", "k", "en", `{"title":"T"}`, false).String()
		off.collab(off.tokenFor("e", rolePolicy("content.read", nil)), art2, "join", map[string]any{"locale": "en"}, 404)
	})
}

// Two servers on one Postgres database: updates, presence, saves and resets cross between them.
func TestCollabAcrossNodes(t *testing.T) {
	url := pgtest.Scratch(t)
	clk := newClock()
	open := func() store.DB {
		db, err := postgres.Open(ctx, url, store.OpenOptions{Clock: clk})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	a, srvA := collabNode(t, open(), clk, "")
	b, srvB := collabNode(t, open(), clk, "")
	art := a.create("article", "k", "en", `{"title":"T"}`, false).String()
	// Tokens are shared through the database.
	tok := a.editors("alice", "bob")
	ja := a.join(tok[0], art, "en", true, 200)
	jb := b.join(tok[1], art, "en", true, 200)
	if len(strs(ja["init"])) != 2 || len(strs(jb["init"])) != 0 {
		t.Fatalf("claims across nodes: %v %v", ja["init"], jb["init"])
	}
	sa := openCollab(t, srvA.URL, tok[0], art, "client="+ja["client"].(string))
	sb := openCollab(t, srvB.URL, tok[1], art, "client="+jb["client"].(string))
	sa.presenceWith(2)
	sb.presenceWith(2)

	r := a.collab(tok[0], art, "updates", map[string]any{"client": ja["client"], "data": b64("seed"), "init": []string{"shared", "l:en"}}, 200)
	d := sb.until("update")
	deq(t, "relayed seq", d["seq"], r.json()["seq"])
	deq(t, "relayed init", len(d["init"].([]any)), 2)
	r = b.collab(tok[1], art, "updates", map[string]any{"client": jb["client"], "data": b64("more")}, 200)
	deq(t, "relayed back", sa.until("update")["seq"], r.json()["seq"])

	a.clk.Advance(time.Second)
	res := b.do("PATCH", "/api/manage/v1/items/"+art, map[string]string{"Authorization": "Bearer " + tok[1], "X-Hydrogen-Collab-Client": jb["client"].(string)},
		map[string]any{"locale": "en", "fields": map[string]any{"title": "From B"}})
	if res.Code != 200 {
		t.Fatalf("patch: %d %s", res.Code, res.Body)
	}
	a.drain()
	b.drain()
	s := sa.until("saved")
	deq(t, "saved client", s["client"], jb["client"])
	deq(t, "saved etag", s["etag"], res.Header().Get("ETag"))

	a.update(mustID(t, art), "en", `{"title":"From A"}`)
	a.drain()
	b.drain()
	deq(t, "reset on a", sa.until("reset")["reason"], "external")
	deq(t, "reset on b", sb.until("reset")["reason"], "external")
}

// Concurrent joiners: exactly one claims each part.
func TestCollabJoinRace(t *testing.T) {
	runCollab(t, func(t *testing.T, n *dnode, _ *httptest.Server) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false).String()
		tok := n.editors("alice")[0]
		const joiners = 8
		results := make(chan []string, joiners)
		for range joiners {
			go func() {
				r := n.as(tok, "POST", "/collab/items/"+art+"/join", map[string]any{"locale": "en", "edit": true})
				if r.Code != 200 {
					results <- []string{"error " + r.Body.String()}
					return
				}
				results <- strs(r.json()["init"])
			}()
		}
		claimed := map[string]int{}
		for range joiners {
			for _, p := range <-results {
				claimed[p]++
			}
		}
		if claimed["shared"] != 1 || claimed["l:en"] != 1 || len(claimed) != 2 {
			t.Errorf("claims = %v, want one per part", claimed)
		}
	})
}
