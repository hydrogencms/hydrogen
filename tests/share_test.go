package tests

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/store"
)

const shareChannels = `
languages: { en: {}, de: {} }
channels:
  web:    { languages: [en, de], frontend: "https://www.acme.com", preview: { "*": "/p/{collection}/{id}" } }
  other:  { languages: [en], frontend: "https://other.acme.com" }
  people: { languages: [en], collections: [person] }
`

// tokenFor makes an API token holding the policies and returns its secret.
func (n *dnode) tokenFor(name string, policies ...access.RolePolicy) string {
	n.t.Helper()
	s := n.app.Services()
	role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: policies})
	if err != nil {
		n.t.Fatal(err)
	}
	_, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: name, Roles: []ids.UUID{role.ID}})
	if err != nil {
		n.t.Fatal(err)
	}
	return secret
}

func (n *dnode) as(secret, method, path string, body any) resp {
	n.t.Helper()
	return n.do(method, "/api/manage/v1"+path, map[string]string{"Authorization": "Bearer " + secret}, body)
}

func (n *dnode) share(body map[string]any, want int) map[string]any {
	n.t.Helper()
	r := n.manage("POST", "/preview-shares", body)
	if r.Code != want {
		n.t.Fatalf("POST /preview-shares %v: %d, want %d: %s", body, r.Code, want, r.Body)
	}
	return r.json()
}

func (n *dnode) relay(item ids.UUID, locale, title string, want int) resp {
	n.t.Helper()
	r := n.manage("PUT", "/preview-relay/"+item.String()+"?locale="+locale, map[string]any{"fields": map[string]any{"title": title}})
	if r.Code != want {
		n.t.Fatalf("PUT relay: %d, want %d: %s", r.Code, want, r.Body)
	}
	return r
}

func shareTitle(n *dnode, item ids.UUID, tok string) any {
	return field(n.wantStatus("/items/"+item.String()+"?preview="+tok, 200).json(), "item", "fields", "title")
}

func runShare(t *testing.T, fn func(t *testing.T, n *dnode)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n, err := tryNode(t, b.open(t, clk), clk, shareChannels)
		if err != nil {
			t.Fatal(err)
		}
		fn(t, n)
	})
}

func TestShareLinkLifecycle(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"Draft only"}`, false)
		other := n.create("article", "o", "en", `{"title":"Other"}`, false)

		sh := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)
		tok := sh["token"].(string)
		if !strings.HasPrefix(tok, "hps_") || sh["channel"] != "web" || sh["locale"] != "en" || sh["id"] == "" {
			t.Fatalf("share = %v", sh)
		}
		wantExp := n.clk.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		deq(t, "default expiry", sh["expiresAt"], wantExp)
		if u, _ := sh["url"].(string); !strings.HasPrefix(u, "https://www.acme.com/p/article/"+art.String()+"?preview="+tok) {
			t.Errorf("url = %v", sh["url"])
		}

		// The token previews its item (and nothing else), uncached and not indexable.
		r := n.wantStatus("/items/"+art.String()+"?preview="+tok, 200)
		deq(t, "title", field(r.json(), "item", "fields", "title"), "Draft only")
		deq(t, "robots", r.Header().Get("X-Robots-Tag"), "noindex")
		deq(t, "cache", r.Header().Get("Cache-Control"), "no-store")
		n.wantStatus("/items/"+other.String()+"?preview="+tok, 404)
		n.wantStatus("/items/"+art.String()+"?preview="+tok+"&channel=other", 401)
		n.wantStatus("/items/"+art.String()+"?channel=web", 401, previewHeaderName, tok+"x")

		// The list names the link but never its token.
		l := n.manage("GET", "/preview-shares?item="+art.String(), nil)
		if l.Code != 200 || strings.Contains(l.Body.String(), tok) {
			t.Fatalf("list: %d %s", l.Code, l.Body)
		}
		list := l.json()["shares"].([]any)
		if len(list) != 1 || field(list[0].(map[string]any), "id") != sh["id"] || field(list[0].(map[string]any), "revokedAt") != nil {
			t.Fatalf("list = %v", list)
		}

		// Revoking refuses the token at once on this server, and audits.
		if r := n.manage("DELETE", "/preview-shares/"+sh["id"].(string), nil); r.Code != 204 {
			t.Fatalf("revoke: %d %s", r.Code, r.Body)
		}
		n.wantStatus("/items/"+art.String()+"?preview="+tok, 401)
		if r := n.manage("DELETE", "/preview-shares/"+sh["id"].(string), nil); r.Code != 204 {
			t.Errorf("revoke twice: %d", r.Code)
		}
		if r := n.manage("DELETE", "/preview-shares/"+ids.NewGenerator(n.clk, nil).New().String(), nil); r.Code != 404 {
			t.Errorf("revoke unknown: %d", r.Code)
		}
		list = n.manage("GET", "/preview-shares?item="+art.String(), nil).json()["shares"].([]any)
		if field(list[0].(map[string]any), "revokedAt") == nil {
			t.Errorf("revokedAt missing: %v", list)
		}
		var actions []string
		err := n.db.View(ctx, func(tx store.Tx) error {
			evs, err := tx.Audit().List(ctx, store.AuditQuery{Subject: art.String(), Limit: 100})
			for _, e := range evs {
				actions = append(actions, e.Action)
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(actions, " ")
		if !strings.Contains(joined, "preview.share.create") || !strings.Contains(joined, "preview.share.revoke") {
			t.Errorf("audit = %v", actions)
		}
	})
}

func TestShareLinkExpiryTTLAndCache(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false)
		n.share(map[string]any{"item": art.String(), "channel": "web", "ttl": "200h"}, 400)
		n.share(map[string]any{"item": art.String(), "channel": "web", "ttl": "0s"}, 400)
		n.share(map[string]any{"item": art.String(), "channel": "nope"}, 400)
		n.share(map[string]any{"item": art.String(), "channel": "web", "locale": "fr"}, 400)
		n.share(map[string]any{"item": "x", "channel": "web"}, 400)
		n.share(map[string]any{"item": ids.NewGenerator(n.clk, nil).New().String(), "channel": "web"}, 404)
		n.share(map[string]any{"item": art.String(), "channel": "people"}, 404) // the channel does not serve articles

		sh := n.share(map[string]any{"item": art.String(), "channel": "web", "ttl": 3600, "locale": "de"}, 201)
		deq(t, "locale", sh["locale"], "de")
		tok := sh["token"].(string)
		deq(t, "title", shareTitle(n, art, tok), "T")

		// Another server revokes: this one keeps accepting the token only until its lookup cache runs out.
		err := n.db.Tx(ctx, func(tx store.Tx) error {
			id, _ := ids.Parse(sh["id"].(string))
			return tx.Preview().RevokeShare(ctx, id, n.clk.Now())
		})
		if err != nil {
			t.Fatal(err)
		}
		n.wantStatus("/items/"+art.String()+"?preview="+tok, 200)
		n.clk.Advance(6 * time.Second)
		n.wantStatus("/items/"+art.String()+"?preview="+tok, 401)

		// Expiry follows the clock.
		sh = n.share(map[string]any{"item": art.String(), "channel": "web", "ttl": "1m"}, 201)
		tok = sh["token"].(string)
		deq(t, "title", shareTitle(n, art, tok), "T")
		n.clk.Advance(61 * time.Second)
		n.wantStatus("/items/"+art.String()+"?preview="+tok, 401)

		// PreviewTokenInfo accepts both kinds.
		sh = n.share(map[string]any{"item": art.String(), "channel": "web", "ttl": "1h"}, 201)
		d := n.app.Services().Delivery
		chn, item, exp, err := d.PreviewTokenInfo(sh["token"].(string))
		if err != nil || chn != "web" || item != art.String() || exp.Format(time.RFC3339) != sh["expiresAt"] {
			t.Errorf("share info = %q %q %v %v", chn, item, exp, err)
		}
		hmac := n.previewToken(map[string]any{"channel": "other", "item": art.String()}, 200)
		if chn, item, _, err := d.PreviewTokenInfo(hmac); err != nil || chn != "other" || item != art.String() {
			t.Errorf("signed info = %q %q %v", chn, item, err)
		}
		if _, _, _, err := d.PreviewTokenInfo("hps_unknown"); err == nil {
			t.Error("unknown share token accepted")
		}
	})
}

func TestShareLinksTurnedOff(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n, err := tryNodeConfig(t, b.open(t, clk), clk, shareChannels, "delivery: { share_links: false }\n")
		if err != nil {
			t.Fatal(err)
		}
		art := n.create("article", "k", "en", `{"title":"T"}`, false)
		r := n.share(map[string]any{"item": art.String(), "channel": "web"}, 403)
		if !strings.Contains(field(r, "error", "message").(string), "share_links") {
			t.Errorf("message = %v", r)
		}
		if r := n.manage("GET", "/preview-shares?item="+art.String(), nil); r.Code != 200 {
			t.Errorf("list: %d", r.Code)
		}
	})
}

func TestShareConfigValidation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		for _, yaml := range []string{"delivery: { share_max_ttl: 800h }\n", "delivery: { share_default_ttl: 200h }\n",
			"delivery: { share_default_ttl: 0s }\n"} {
			clk := newClock()
			if _, err := tryNodeConfig(t, b.open(t, clk), clk, shareChannels, yaml); err == nil || !strings.Contains(err.Error(), "share_") {
				t.Errorf("%q: %v", yaml, err)
			}
		}
		clk := newClock()
		if _, err := tryNodeConfig(t, b.open(t, clk), clk, shareChannels, "delivery: { share_default_ttl: 1h, share_max_ttl: 720h }\n"); err != nil {
			t.Error(err)
		}
	})
}

func TestSharePolicies(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false)
		per := n.create("person", "p", "en", `{"fullName":"P"}`, false)
		read, prev, share := rolePolicy("content.read", nil), rolePolicy("content.preview", nil), rolePolicy("content.preview.share", nil)
		previewer := n.tokenFor("previewer", read, prev)
		sharer := n.tokenFor("sharer", read, prev, share)
		onlyPeople := n.tokenFor("people", rolePolicy("content.read", nil), prev,
			rolePolicy("content.preview.share", map[string][]string{"collection": {"person"}}))
		body := map[string]any{"item": art.String(), "channel": "web"}

		if r := n.as(previewer, "POST", "/preview-shares", body); r.Code != 403 {
			t.Errorf("share without the policy: %d %s", r.Code, r.Body)
		}
		if r := n.as(onlyPeople, "POST", "/preview-shares", body); r.Code != 403 && r.Code != 404 {
			t.Errorf("share outside the limitation: %d %s", r.Code, r.Body)
		}
		if r := n.as(onlyPeople, "POST", "/preview-shares", map[string]any{"item": per.String(), "channel": "people"}); r.Code != 201 {
			t.Errorf("share inside the limitation: %d %s", r.Code, r.Body)
		}
		r := n.as(sharer, "POST", "/preview-shares", body)
		if r.Code != 201 {
			t.Fatalf("share: %d %s", r.Code, r.Body)
		}
		id := r.json()["id"].(string)

		// Listing needs content.preview only; revoking needs the share policy.
		if r := n.as(previewer, "GET", "/preview-shares?item="+art.String(), nil); r.Code != 200 {
			t.Errorf("list: %d %s", r.Code, r.Body)
		}
		if r := n.as(n.tokenFor("reader", read), "GET", "/preview-shares?item="+art.String(), nil); r.Code != 403 {
			t.Errorf("list without preview: %d", r.Code)
		}
		if r := n.as(previewer, "DELETE", "/preview-shares/"+id, nil); r.Code != 403 {
			t.Errorf("revoke without the policy: %d %s", r.Code, r.Body)
		}
		if r := n.as(sharer, "DELETE", "/preview-shares/"+id, nil); r.Code != 204 {
			t.Errorf("revoke: %d %s", r.Code, r.Body)
		}
		// The built-in editor role has the policy; the contributor does not.
		for _, tpl := range access.Templates() {
			has := false
			for _, p := range tpl.Policies {
				has = has || p.Policy == access.PolicyContentPreviewShare
			}
			if want := tpl.Name == "editor"; has != want {
				t.Errorf("template %s grants content.preview.share = %v", tpl.Name, has)
			}
		}
	})
}

func TestRelayOverlay(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"Live"}`, true)
		n.update(art, "en", `{"title":"Saved draft"}`)
		hm := n.previewToken(map[string]any{"channel": "web", "item": art.String()}, 200)
		sh := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["token"].(string)

		n.clk.Advance(time.Second)
		deq(t, "no relay", shareTitle(n, art, hm), "Saved draft")
		rev := n.relay(art, "en", "Typing", 200).json()["rev"]
		deq(t, "rev", rev, float64(1))
		deq(t, "signed token", shareTitle(n, art, hm), "Typing")
		deq(t, "share token", shareTitle(n, art, sh), "Typing")
		deq(t, "published unaffected", field(n.wantStatus("/items/"+art.String()+"?channel=web", 200).json(), "item", "fields", "title"), "Live")
		deq(t, "name follows", field(n.wantStatus("/items/"+art.String()+"?preview="+hm, 200).json(), "item", "name"), "Typing")
		deq(t, "rev 2", n.relay(art, "en", "Typing more", 200).json()["rev"], float64(2))
		deq(t, "latest wins", shareTitle(n, art, hm), "Typing more")

		// A listing in a channel-wide preview shows it too.
		wide := n.previewToken(map[string]any{"channel": "web"}, 200)
		art2 := n.create("article", "k2", "en", `{"title":"Second saved"}`, false)
		n.clk.Advance(time.Second)
		n.relay(art2, "en", "Second typing", 200)
		titles := map[any]bool{}
		items := n.wantStatus("/collections/article/items?preview="+wide, 200).json()["items"].([]any)
		for _, it := range items {
			titles[field(it.(map[string]any), "fields", "title")] = true
		}
		if len(items) != 2 || !titles["Typing more"] || !titles["Second typing"] {
			t.Errorf("listing = %v, want both relays applied", titles)
		}
		n.manage("DELETE", "/preview-relay/"+art2.String()+"?locale=en", nil)

		// Dropping it falls back to the saved draft.
		if r := n.manage("DELETE", "/preview-relay/"+art.String()+"?locale=en", nil); r.Code != 204 {
			t.Fatalf("delete: %d %s", r.Code, r.Body)
		}
		deq(t, "dropped", shareTitle(n, art, hm), "Saved draft")

		// Saving a newer draft supersedes a relay; so does age.
		n.relay(art, "en", "Old relay", 200)
		n.update(art, "en", `{"title":"Newer save"}`)
		deq(t, "superseded by a save", shareTitle(n, art, hm), "Newer save")
		n.clk.Advance(time.Second)
		n.relay(art, "en", "Fresh relay", 200)
		deq(t, "fresh", shareTitle(n, art, hm), "Fresh relay")
		n.clk.Advance(59 * time.Minute)
		sh2 := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["token"].(string)
		deq(t, "just under an hour", shareTitle(n, art, sh2), "Fresh relay")
		n.clk.Advance(2 * time.Minute)
		sh3 := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["token"].(string)
		deq(t, "stale", shareTitle(n, art, sh3), "Newer save")

		// The cleanup removes stale rows.
		if err := n.app.Services().Delivery.CleanupPreview(ctx); err != nil {
			t.Fatal(err)
		}
		err := n.db.View(ctx, func(tx store.Tx) error {
			_, err := tx.Preview().GetRelay(ctx, art, "en")
			if err == nil {
				t.Error("stale relay kept")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestRelayValidationAndAccess(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false)
		path := "/preview-relay/" + art.String() + "?locale=en"
		put := func(secret string, body any) resp { return n.as(secret, "PUT", path, body) }

		// Types and shapes are checked; a missing required field is fine in a draft.
		r := n.manage("PUT", path, map[string]any{"fields": map[string]any{"rank": "high"}})
		if r.Code != 422 || field(r.json(), "error", "code") != "invalid" {
			t.Errorf("bad type: %d %s", r.Code, r.Body)
		}
		if r := n.manage("PUT", path, map[string]any{"fields": map[string]any{}}); r.Code != 200 {
			t.Errorf("empty fields: %d %s", r.Code, r.Body)
		}
		if r := n.manage("PUT", path, map[string]any{"fields": []any{1}}); r.Code != 400 {
			t.Errorf("array fields: %d", r.Code)
		}
		if r := n.manage("PUT", path, map[string]any{"other": 1}); r.Code != 400 {
			t.Errorf("unknown key: %d", r.Code)
		}
		if r := n.manage("PUT", "/preview-relay/"+ids.NewGenerator(n.clk, nil).New().String(), map[string]any{"fields": map[string]any{}}); r.Code != 404 {
			t.Errorf("unknown item: %d", r.Code)
		}
		if r := n.manage("PUT", "/preview-relay/"+art.String()+"?locale=bad_locale!", map[string]any{"fields": map[string]any{}}); r.Code != 400 {
			t.Errorf("bad locale: %d", r.Code)
		}

		fields := map[string]any{"fields": map[string]any{"title": "x"}}
		reader := n.tokenFor("reader", rolePolicy("content.read", nil), rolePolicy("content.preview", nil))
		if r := put(reader, fields); r.Code != 403 {
			t.Errorf("relay without content.edit: %d %s", r.Code, r.Body)
		}
		if r := n.as(reader, "DELETE", path, nil); r.Code != 403 {
			t.Errorf("drop without content.edit: %d", r.Code)
		}
		editor := n.tokenFor("editor", rolePolicy("content.read", nil), rolePolicy("content.edit", nil))
		if r := put(editor, fields); r.Code != 200 {
			t.Errorf("relay: %d %s", r.Code, r.Body)
		}
		deLimited := n.tokenFor("de", rolePolicy("content.read", nil), rolePolicy("content.edit", map[string][]string{"locale": {"de"}}))
		if r := put(deLimited, fields); r.Code != 403 {
			t.Errorf("relay in a locale the editor may not write: %d", r.Code)
		}
		if r := n.do("PUT", "/api/manage/v1"+path, nil, fields); r.Code != 401 {
			t.Errorf("anonymous: %d", r.Code)
		}
	})
}

// sse reads the events of a stream.
type sse struct {
	t    *testing.T
	resp *http.Response
	ev   chan [2]string
}

func openStream(t *testing.T, base, proof, origin string) *sse {
	t.Helper()
	req, _ := http.NewRequest("GET", base+"/api/deliver/v1/preview/events?proof="+proof, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &sse{t: t, resp: resp, ev: make(chan [2]string, 64)}
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

func (s *sse) next() (string, map[string]any) {
	s.t.Helper()
	select {
	case e, ok := <-s.ev:
		if !ok {
			s.t.Fatal("stream closed")
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(e[1]), &m); err != nil {
			s.t.Fatalf("event data %q: %v", e[1], err)
		}
		return e[0], m
	case <-time.After(5 * time.Second):
		s.t.Fatal("no event within 5s")
	}
	return "", nil
}

func (s *sse) closed() bool {
	select {
	case _, ok := <-s.ev:
		return !ok
	case <-time.After(5 * time.Second):
		return false
	}
}

func proofOf(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

func TestShareEvents(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		srv := httptest.NewServer(n.h)
		t.Cleanup(srv.Close)
		art := n.create("article", "k", "en", `{"title":"Saved"}`, false)
		sh := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)
		tok, id := sh["token"].(string), sh["id"].(string)

		for _, bad := range []string{"", "zz", strings.Repeat("0", 64), proofOf("hps_nope")} {
			if s := openStream(t, srv.URL, bad, ""); s.resp.StatusCode != 401 {
				t.Errorf("proof %q: %d", bad, s.resp.StatusCode)
			}
		}
		// The token itself is not the proof.
		if s := openStream(t, srv.URL, tok, ""); s.resp.StatusCode != 401 {
			t.Errorf("token as proof: %d", s.resp.StatusCode)
		}

		s := openStream(t, srv.URL, proofOf(tok), "https://www.acme.com")
		h := s.resp.Header
		if s.resp.StatusCode != 200 || !strings.HasPrefix(h.Get("Content-Type"), "text/event-stream") || h.Get("Cache-Control") != "no-store" ||
			h.Get("X-Accel-Buffering") != "no" || h.Get("Access-Control-Allow-Origin") != "https://www.acme.com" {
			t.Fatalf("stream: %d %v", s.resp.StatusCode, h)
		}
		ev, data := s.next()
		deq(t, "first event", ev, "draft")
		deq(t, "first title", field(data, "item", "fields", "title"), "Saved")
		deq(t, "first rev", data["rev"], float64(0))
		v0, _ := data["v"].(string)
		if len(v0) != 16 {
			t.Fatalf("v = %q, want 16 hex characters", data["v"])
		}
		eventually(t, "the viewer to be counted", func() bool {
			l := n.manage("GET", "/preview-shares?item="+art.String(), nil).json()["shares"].([]any)
			return l[0].(map[string]any)["viewers"] == float64(1)
		})

		n.clk.Advance(time.Second)
		n.relay(art, "en", "Typing", 200)
		ev, data = s.next()
		deq(t, "update event", ev, "draft")
		deq(t, "update title", field(data, "item", "fields", "title"), "Typing")
		deq(t, "update rev", data["rev"], float64(1))
		if v1, _ := data["v"].(string); v1 == "" || v1 == v0 {
			t.Errorf("v did not change with the item: %q then %q", v0, v1)
		}
		// A save ends the relay: the next event has rev 0 and a new v even though no relay applies.
		n.update(art, "en", `{"title":"Saved twice"}`)
		if r := n.manage("DELETE", "/preview-relay/"+art.String()+"?locale=en", nil); r.Code != 204 {
			t.Fatal(r.Code)
		}
		_, data = s.next()
		deq(t, "back to saved", field(data, "item", "fields", "title"), "Saved twice")
		deq(t, "rev without a relay", data["rev"], float64(0))
		if v2, _ := data["v"].(string); v2 == "" || v2 == v0 {
			t.Errorf("v after a save = %q (first %q)", v2, v0)
		}

		// Bursts coalesce: the last state arrives, in far fewer events than writes.
		for i := range 20 {
			n.clk.Advance(time.Millisecond)
			n.relay(art, "en", "Burst "+string(rune('a'+i)), 200)
		}
		last := ""
		deadline := time.After(5 * time.Second)
		count := 0
	burst:
		for last != "Burst t" {
			select {
			case e := <-s.ev:
				var m map[string]any
				_ = json.Unmarshal([]byte(e[1]), &m)
				last, _ = field(m, "item", "fields", "title").(string)
				count++
			case <-deadline:
				break burst
			}
		}
		if last != "Burst t" || count > 12 {
			t.Errorf("burst: last %q after %d events", last, count)
		}

		if r := n.manage("DELETE", "/preview-shares/"+id, nil); r.Code != 204 {
			t.Fatal(r.Code)
		}
		eventually(t, "the end event", func() bool {
			select {
			case e, ok := <-s.ev:
				return ok && e[0] == "end" && strings.Contains(e[1], `"revoked"`)
			default:
				return false
			}
		})
		if !s.closed() {
			t.Error("stream open after end")
		}
		if s := openStream(t, srv.URL, proofOf(tok), ""); s.resp.StatusCode != 401 {
			t.Errorf("revoked link opens a stream: %d", s.resp.StatusCode)
		}
	})
}

func TestShareEventsExpiryAndCORS(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		srv := httptest.NewServer(n.h)
		t.Cleanup(srv.Close)
		art := n.create("article", "k", "en", `{"title":"Saved"}`, false)
		tok := n.share(map[string]any{"item": art.String(), "channel": "web", "ttl": "1m"}, 201)["token"].(string)

		// Only the channels' frontends (and the server's own origin) may read the stream across origins.
		cases := map[string]string{"https://www.acme.com": "https://www.acme.com", "https://other.acme.com": "https://other.acme.com",
			srv.URL: srv.URL, "https://evil.example": "", "null": "",
			"http://www.acme.com":                         "", // the scheme must match too
			"https://WWW.ACME.COM":                        "https://WWW.ACME.COM",
			"https://www.acme.com:443":                    "https://www.acme.com:443",
			"https://www.acme.com:8443":                   "",
			"https://www.acme.com.evil.io":                "",
			"https" + strings.TrimPrefix(srv.URL, "http"): "", // own host, wrong scheme
		}
		for origin, want := range cases {
			s := openStream(t, srv.URL, proofOf(tok), origin)
			if got := s.resp.Header.Get("Access-Control-Allow-Origin"); got != want {
				t.Errorf("origin %s: ACAO %q, want %q", origin, got, want)
			}
		}
		bad := openStream(t, srv.URL, proofOf("hps_nope"), "https://www.acme.com")
		if bad.resp.StatusCode != 401 || bad.resp.Header.Get("Access-Control-Allow-Origin") != "https://www.acme.com" {
			t.Errorf("401 without CORS headers: %v", bad.resp.Header)
		}

		s := openStream(t, srv.URL, proofOf(tok), "")
		s.next()
		n.clk.Advance(2 * time.Minute)
		n.relay(art, "en", "Late", 200) // wakes the stream, which finds the link expired
		ev, data := s.next()
		deq(t, "end", ev, "end")
		deq(t, "reason", data["reason"], "expired")
		if !s.closed() {
			t.Error("stream open after end")
		}
	})
}

// A relay written on one server reaches a viewer connected to another.
func TestShareEventsAcrossNodes(t *testing.T) {
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
	a, err := tryNode(t, open(), clk, shareChannels)
	if err != nil {
		t.Fatal(err)
	}
	b, err := tryNode(t, open(), clk, shareChannels)
	if err != nil {
		t.Fatal(err)
	}
	srvB := httptest.NewServer(b.h)
	t.Cleanup(srvB.Close)
	art := a.create("article", "k", "en", `{"title":"Saved"}`, false)
	tok := a.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["token"].(string)
	s := openStream(t, srvB.URL, proofOf(tok), "")
	s.next()
	a.clk.Advance(time.Second)
	a.relay(art, "en", "From A", 200)
	_, data := s.next()
	deq(t, "title", field(data, "item", "fields", "title"), "From A")

	sh := a.manage("GET", "/preview-shares?item="+art.String(), nil).json()["shares"].([]any)[0].(map[string]any)
	if r := a.manage("DELETE", "/preview-shares/"+sh["id"].(string), nil); r.Code != 204 {
		t.Fatal(r.Code)
	}
	ev, _ := s.next()
	deq(t, "end on the other node", ev, "end")
	// Node B drops its lookup cache on the notification, so the token is refused there at once.
	b.wantStatus("/items/"+art.String()+"?preview="+tok, 401)
}

func TestRelayStoresValidatedDocument(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false)
		path := "/preview-relay/" + art.String() + "?locale=en"
		n.clk.Advance(time.Second)
		r := n.manage("PUT", path, map[string]any{"fields": map[string]any{"title": "Kept", "related": []any{strings.ToUpper(art.String())}}})
		if r.Code != 200 {
			t.Fatalf("relay: %d %s", r.Code, r.Body)
		}
		var raw json.RawMessage
		err := n.db.View(ctx, func(tx store.Tx) error {
			d, err := tx.Preview().GetRelay(ctx, art, "en")
			raw = d.Fields
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		var stored map[string]any
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatal(err)
		}
		rel, _ := stored["related"].([]any)
		if stored["title"] != "Kept" || len(rel) != 1 || rel[0] != art.String() {
			t.Errorf("stored relay = %s, want the normalized document (ids in lower case)", raw)
		}

		// A NUL cannot be stored in Postgres jsonb: a 422, never a 500.
		for _, title := range []string{"a\u0000b"} {
			r := n.manage("PUT", path, map[string]any{"fields": map[string]any{"title": title}})
			if r.Code != 422 || field(r.json(), "error", "code") != "invalid" {
				t.Errorf("NUL in a string: %d %s", r.Code, r.Body)
			}
		}
	})
}

func TestShareStreamCaps(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		srv := httptest.NewServer(n.h)
		t.Cleanup(srv.Close)
		art := n.create("article", "k", "en", `{"title":"Saved"}`, false)
		tok := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["token"].(string)
		other := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["token"].(string)
		var open []*sse
		for i := range 20 {
			s := openStream(t, srv.URL, proofOf(tok), "")
			if s.resp.StatusCode != 200 {
				t.Fatalf("stream %d: %d", i, s.resp.StatusCode)
			}
			open = append(open, s)
		}
		over := openStream(t, srv.URL, proofOf(tok), "")
		if over.resp.StatusCode != 429 || !strings.HasPrefix(over.resp.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("21st stream: %d %v", over.resp.StatusCode, over.resp.Header)
		}
		// Another share is not affected, and a closed stream frees its slot.
		if s := openStream(t, srv.URL, proofOf(other), ""); s.resp.StatusCode != 200 {
			t.Errorf("other share: %d", s.resp.StatusCode)
		}
		_ = open[0].resp.Body.Close()
		eventually(t, "a slot to free up", func() bool {
			s := openStream(t, srv.URL, proofOf(tok), "")
			return s.resp.StatusCode == 200
		})
	})
}

func TestShareStreamItemGone(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		srv := httptest.NewServer(n.h)
		t.Cleanup(srv.Close)
		art := n.create("article", "k", "en", `{"title":"Saved"}`, false)
		sh := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)
		s := openStream(t, srv.URL, proofOf(sh["token"].(string)), "")
		s.next()
		if err := n.app.Services().Content.Delete(ctx, art); err != nil {
			t.Fatal(err)
		}
		// The link to a trashed item stops working.
		n.wantStatus("/items/"+art.String()+"?preview="+sh["token"].(string), 404)
		n.clk.Advance(time.Second)
		// A relay notification wakes the stream; the item is gone, so it ends as unavailable (not revoked).
		if err := n.db.Tx(ctx, func(tx store.Tx) error {
			return tx.Notify(store.PreviewChannel, art.String()+"|en|1")
		}); err != nil {
			t.Fatal(err)
		}
		ev, data := s.next()
		deq(t, "event", ev, "end")
		deq(t, "reason", data["reason"], "unavailable")
		if !s.closed() {
			t.Error("stream open after end")
		}
		// The link can still be revoked by someone with the policy.
		if r := n.manage("DELETE", "/preview-shares/"+sh["id"].(string), nil); r.Code != 204 {
			t.Errorf("revoke after the item was deleted: %d %s", r.Code, r.Body)
		}
	})
}

func TestShareCreatorNames(t *testing.T) {
	runShare(t, func(t *testing.T, n *dnode) {
		art := n.create("article", "k", "en", `{"title":"T"}`, false)
		sharer := n.tokenFor("Docs bot", rolePolicy("content.read", nil), rolePolicy("content.preview", nil),
			rolePolicy("content.preview.share", nil))
		byToken := n.as(sharer, "POST", "/preview-shares", map[string]any{"item": art.String(), "channel": "web"}).json()["id"].(string)
		byConfig := n.share(map[string]any{"item": art.String(), "channel": "web"}, 201)["id"].(string)

		u, err := n.app.Services().Admin.CreateUser(ctx, access.UserInput{Login: "ada", Name: "Ada Lovelace"})
		if err != nil {
			t.Fatal(err)
		}
		gone := n.app.Services().IDs.New()
		insert := func(createdBy string) string {
			n.clk.Advance(time.Second)
			id := n.app.Services().IDs.New()
			h := sha256.Sum256(id[:])
			err := n.db.Tx(ctx, func(tx store.Tx) error {
				return tx.Preview().CreateShare(ctx, store.ShareSession{ID: id, TokenHash: h[:], Item: art, Channel: "web", Locale: "en",
					CreatedBy: createdBy, CreatedAt: n.clk.Now(), ExpiresAt: n.clk.Now().Add(time.Hour)})
			})
			if err != nil {
				t.Fatal(err)
			}
			return id.String()
		}
		byUser := insert("user:" + u.ID.String())
		byGoneUser := insert("user:" + gone.String())
		byJunk := insert("system")

		want := map[string]string{byToken: "Docs bot", byUser: "Ada Lovelace", byGoneUser: "", byJunk: "", byConfig: ""}
		for _, e := range n.manage("GET", "/preview-shares?item="+art.String(), nil).json()["shares"].([]any) {
			m := e.(map[string]any)
			name, present := m["createdByName"]
			if !present || name != want[m["id"].(string)] {
				t.Errorf("share %v: createdByName = %v (present %v), want %q", m["id"], name, present, want[m["id"].(string)])
			}
		}
	})
}
