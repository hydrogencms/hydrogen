package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/store"

	gen "github.com/hydrogencms/hydrogen/examples/content/gen/content"
)

const userPassword = "correct horse battery"

// newsOnly lets the named user work with news articles only; everyone else is unaffected by the rule.
func newsOnly(user string) access.Rules {
	rule := func(_ context.Context, p *access.Principal) access.Result {
		if p.Name == user {
			return access.Where("kind", store.OpEq, "news")
		}
		return access.Allow()
	}
	return access.Rules{Read: rule, Create: rule, Update: rule, Publish: rule, Delete: rule}
}

func configureWithRules(h *hydrogen.Builder) {
	configure(h)
	gen.ArticleType.Access(h, newsOnly("rita"))
}

type who struct {
	t      *testing.T
	base   string
	bearer string
	cookie string
	csrf   string
}

func (w *who) do(method, path string, body any, out any) int {
	w.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, w.base+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if w.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+w.bearer)
	}
	if w.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "hydrogen_session", Value: w.cookie})
		req.Header.Set("X-CSRF-Token", w.csrf)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			w.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func (w *who) ok(status int, method, path string, body any, out any) {
	w.t.Helper()
	if got := w.do(method, path, body, out); got != status {
		w.t.Fatalf("%s %s: status %d, want %d", method, path, got, status)
	}
}

func (w *who) notFound(method, path string, body any) {
	w.t.Helper()
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if got := w.do(method, path, body, &e); got != 404 || e.Error.Code != "not_found" {
		w.t.Fatalf("%s %s: %d %+v, want 404 not_found", method, path, got, e.Error)
	}
}

func (w *who) forbidden(method, path string, body any, policy string) {
	w.t.Helper()
	var e struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if got := w.do(method, path, body, &e); got != 403 || e.Error.Code != "forbidden" || e.Error.Message != policy {
		w.t.Fatalf("%s %s: %d %+v, want 403 forbidden %s", method, path, got, e.Error, policy)
	}
}

type ref struct {
	ID string `json:"id"`
}

type page struct {
	Items []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"items"`
	Total int `json:"total"`
}

func (p page) has(id string) bool {
	for _, it := range p.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

func rulesScenario(t *testing.T, s *server) {
	root := &who{t: t, base: s.base, bearer: token}

	article := func(who *who, kind string, extra map[string]any) string {
		t.Helper()
		fields := map[string]any{"title": "A " + kind, "kind": kind}
		if kind == "video" {
			fields["video"] = "https://example.com/v.mp4"
		}
		for k, v := range extra {
			fields[k] = v
		}
		var r ref
		who.ok(201, "POST", "/collections/article/items", map[string]any{"locale": "en", "fields": fields}, &r)
		return r.ID
	}
	newsPublished := article(root, "news", map[string]any{"rating": 4})
	root.ok(200, "POST", "/items/"+newsPublished+"/publish", map[string]any{}, nil)
	videoPublished := article(root, "video", map[string]any{"rating": 4})
	root.ok(200, "POST", "/items/"+videoPublished+"/publish", map[string]any{}, nil)
	newsDraft := article(root, "news", map[string]any{"rating": 3})
	videoDraft := article(root, "video", map[string]any{"rating": 3})

	t.Run("listings find never-published items by an indexed field", func(t *testing.T) {
		var l page
		root.ok(200, "GET", "/collections/article/items?where[rating][gte]=3&where[kind]=news", nil, &l)
		if !l.has(newsDraft) || !l.has(newsPublished) || l.has(videoDraft) || l.Total != 2 {
			t.Errorf("draft-aware filter: %+v", l)
		}
		root.ok(200, "GET", "/collections/article/items?where[rating]=3", nil, &l)
		if l.Total != 2 || !l.has(newsDraft) {
			t.Errorf("never-published item not found by rating: %+v", l)
		}
		// Only published items when asked.
		root.ok(200, "GET", "/collections/article/items?where[rating]=3&status=published", nil, &l)
		if l.Total != 0 {
			t.Errorf("status=published must not list drafts: %+v", l)
		}
		// An edit shows at once in the working view, and the published rows keep the old value.
		root.ok(200, "PATCH", "/items/"+newsPublished, map[string]any{"fields": map[string]any{"rating": 1}}, nil)
		root.ok(200, "GET", "/collections/article/items?where[rating]=1", nil, &l)
		if !l.has(newsPublished) {
			t.Errorf("edited rating not found: %+v", l)
		}
		root.ok(200, "GET", "/collections/article/items?where[rating]=1&status=published", nil, &l)
		if l.Total != 0 {
			t.Errorf("published rows changed by a draft edit: %+v", l)
		}
		root.ok(200, "PATCH", "/items/"+newsPublished, map[string]any{"fields": map[string]any{"rating": 4}}, nil)
		root.ok(204, "DELETE", "/items/"+newsPublished+"/draft", nil, nil)
	})

	var u ref
	root.ok(201, "POST", "/users", map[string]any{"login": "rita", "password": userPassword}, &u)
	var roles struct {
		Items []struct{ ID, Name string } `json:"items"`
	}
	root.ok(200, "GET", "/roles", nil, &roles)
	for _, r := range roles.Items {
		if r.Name == "editor" {
			root.ok(201, "POST", "/assignments", map[string]any{"roleId": r.ID, "principalKind": "user", "principalId": u.ID}, nil)
		}
	}
	login := &who{t: t, base: s.base}
	var res struct {
		CSRF string `json:"csrf"`
	}
	req, _ := http.NewRequest("POST", s.base+"/auth/login", strings.NewReader(`{"login":"rita","password":"`+userPassword+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || json.Unmarshal(raw, &res) != nil {
		t.Fatalf("login: %d %s", resp.StatusCode, raw)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "hydrogen_session" {
			login.cookie = c.Value
		}
	}
	rita := login
	rita.csrf = res.CSRF

	t.Run("reads are scoped", func(t *testing.T) {
		var l page
		rita.ok(200, "GET", "/collections/article/items", nil, &l)
		if l.Total != 2 || !l.has(newsPublished) || !l.has(newsDraft) || l.has(videoPublished) || l.has(videoDraft) {
			t.Errorf("rita sees %+v", l)
		}
		rita.ok(200, "GET", "/collections/article/items?where[kind]=video", nil, &l)
		if l.Total != 0 {
			t.Errorf("filtering cannot reach out of scope: %+v", l)
		}
		rita.ok(200, "GET", "/collections/article/items?status=published", nil, &l)
		if l.Total != 1 || !l.has(newsPublished) {
			t.Errorf("published scope: %+v", l)
		}
		rita.ok(200, "GET", "/collections/article/items?q=video", nil, &l)
		if l.Total != 0 {
			t.Errorf("search reaches out of scope: %+v", l)
		}
		rita.ok(200, "GET", "/items/"+newsDraft+"?draft=true", nil, nil)
		rita.notFound("GET", "/items/"+videoPublished, nil)
		rita.notFound("GET", "/items/"+videoDraft+"?draft=true", nil)
		rita.notFound("GET", "/items/"+videoPublished+"/versions", nil)
		rita.notFound("GET", "/items/"+videoPublished+"/references", nil)
	})

	t.Run("writes are scoped, before and after", func(t *testing.T) {
		rita.notFound("PATCH", "/items/"+videoPublished, map[string]any{"fields": map[string]any{"rating": 1}})
		rita.notFound("POST", "/items/"+videoPublished+"/publish", map[string]any{})
		rita.notFound("DELETE", "/items/"+videoPublished, nil)
		rita.notFound("DELETE", "/items/"+videoDraft+"/draft", nil)
		rita.forbidden("POST", "/collections/article/items", map[string]any{"locale": "en", "fields": map[string]any{
			"title": "Sneaky", "kind": "video", "video": "https://example.com/v.mp4"}}, "content.create")

		// Moving an item out of scope is refused and changes nothing.
		rita.forbidden("PATCH", "/items/"+newsDraft, map[string]any{"fields": map[string]any{
			"kind": "video", "video": "https://example.com/v.mp4"}}, "content.edit")
		var v struct {
			Fields map[string]any `json:"fields"`
		}
		root.ok(200, "GET", "/items/"+newsDraft+"?draft=true", nil, &v)
		if v.Fields["kind"] != "news" {
			t.Errorf("the refused move changed the item: %v", v.Fields)
		}

		// Within scope everything works, including the default kind.
		var own ref
		rita.ok(201, "POST", "/collections/article/items", map[string]any{"locale": "en", "fields": map[string]any{"title": "Rita's"}}, &own)
		rita.ok(200, "PATCH", "/items/"+own.ID, map[string]any{"fields": map[string]any{"rating": 2}}, nil)
		rita.ok(200, "POST", "/items/"+own.ID+"/publish", map[string]any{}, nil)
		rita.ok(204, "DELETE", "/items/"+own.ID, nil, nil)
	})

	t.Run("accounts", func(t *testing.T) {
		var us struct {
			Items []struct{ Login string } `json:"items"`
			Total int
		}
		root.ok(200, "GET", "/users?q=RIT", nil, &us)
		if us.Total != 1 || us.Items[0].Login != "rita" {
			t.Errorf("user search: %+v", us)
		}
		root.ok(200, "GET", "/users?q=nobody", nil, &us)
		if us.Total != 0 {
			t.Errorf("user search miss: %+v", us)
		}
		var gs struct {
			Items []any `json:"items"`
			Total int
		}
		root.ok(200, "GET", "/users/"+u.ID+"/groups", nil, &gs)
		if gs.Total != 0 || gs.Items == nil {
			t.Errorf("user groups: %+v", gs)
		}
		root.notFound("GET", "/users/"+newsDraft+"/groups", nil)
		var sess struct {
			Groups []string `json:"groups"`
		}
		rita.ok(200, "GET", "/auth/session", nil, &sess)
		if sess.Groups == nil {
			t.Errorf("session has no groups: %+v", sess)
		}

		var au struct {
			Items   []struct{ ID int64 } `json:"items"`
			HasMore bool                 `json:"hasMore"`
			Next    int64                `json:"next"`
		}
		root.ok(200, "GET", "/audit?limit=1", nil, &au)
		if !au.HasMore || len(au.Items) != 1 || au.Next != au.Items[0].ID {
			t.Fatalf("audit page: %+v", au)
		}
		first := au.Next
		root.ok(200, "GET", fmt.Sprintf("/audit?limit=1&before=%d", first), nil, &au)
		if len(au.Items) != 1 || au.Items[0].ID >= first {
			t.Errorf("audit before: %+v", au)
		}

		other := signIn(t, s.base, "rita", userPassword)
		other.ok(200, "GET", "/auth/session", nil, nil)
		rita.ok(204, "POST", "/auth/password", map[string]any{"current": userPassword, "new": "another long password"}, nil)
		rita.ok(200, "GET", "/auth/session", nil, nil)
		if got := other.do("GET", "/auth/session", nil, nil); got != 401 {
			t.Errorf("other session after password change: %d", got)
		}
	})

	t.Run("if-match", func(t *testing.T) {
		call := func(method, path, etag string, body string) (*http.Response, []byte) {
			req, _ := http.NewRequest(method, s.base+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+root.bearer)
			if etag != "" {
				req.Header.Set("If-Match", etag)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return resp, raw
		}
		resp, _ := call("GET", "/items/"+newsDraft+"?draft=true", "", "")
		first := resp.Header.Get("ETag")
		if resp.StatusCode != 200 || first == "" {
			t.Fatalf("get: %d etag %q", resp.StatusCode, first)
		}
		resp, _ = call("PATCH", "/items/"+newsDraft, first, `{"fields":{"rating":2}}`)
		second := resp.Header.Get("ETag")
		if resp.StatusCode != 200 || second == "" || second == first {
			t.Fatalf("patch with current etag: %d %q -> %q", resp.StatusCode, first, second)
		}
		resp, raw := call("PATCH", "/items/"+newsDraft, first, `{"fields":{"rating":3}}`)
		if resp.StatusCode != 412 || resp.Header.Get("ETag") != second || !strings.Contains(string(raw), `"precondition_failed"`) ||
			!strings.Contains(string(raw), `"current":{"updatedAt":`) {
			t.Fatalf("stale patch: %d %q %s", resp.StatusCode, resp.Header.Get("ETag"), raw)
		}
		if resp, _ = call("DELETE", "/items/"+newsDraft+"/draft", first, ""); resp.StatusCode != 412 {
			t.Errorf("stale discard: %d", resp.StatusCode)
		}
		if resp, _ = call("PATCH", "/items/"+newsDraft, "", `{"fields":{"rating":4}}`); resp.StatusCode != 200 {
			t.Errorf("no header: %d", resp.StatusCode)
		}
	})

	t.Run("the config token is unaffected", func(t *testing.T) {
		var l page
		root.ok(200, "GET", "/collections/article/items", nil, &l)
		if l.Total < 4 || !l.has(videoPublished) {
			t.Errorf("config token lists %+v", l)
		}
		root.ok(200, "PATCH", "/items/"+videoPublished, map[string]any{"fields": map[string]any{"rating": 5}}, nil)
	})
}

func TestRulesSQLite(t *testing.T) {
	rulesScenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configureWithRules))
}

func TestRulesPostgres(t *testing.T) {
	rulesScenario(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configureWithRules))
}

func signIn(t *testing.T, base, login, password string) *who {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/auth/login", strings.NewReader(`{"login":"`+login+`","password":"`+password+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	var res struct {
		CSRF string `json:"csrf"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(raw, &res) != nil {
		t.Fatalf("login: %d %s", resp.StatusCode, raw)
	}
	w := &who{t: t, base: base, csrf: res.CSRF}
	for _, c := range resp.Cookies() {
		if c.Name == "hydrogen_session" {
			w.cookie = c.Value
		}
	}
	return w
}
