package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

const userPassword = "correct horse battery"

// who is a client of the management API with its own credentials: a bearer token, or a session cookie and CSRF
// token from login. The cookie is sent by hand because it is Secure and the test server speaks plain HTTP.
type who struct {
	t      *testing.T
	base   string
	bearer string
	cookie string
	csrf   string
	// sendCSRF controls whether unsafe requests carry the CSRF token.
	sendCSRF bool
	headers  map[string]string
}

func (w *who) request(method, path string, body any) (*http.Response, []byte) {
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
	}
	if w.csrf != "" && w.sendCSRF {
		req.Header.Set("X-CSRF-Token", w.csrf)
	}
	for k, v := range w.headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		w.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

// do returns the status and decodes the JSON response into out when given.
func (w *who) do(method, path string, body any, out any) int {
	w.t.Helper()
	resp, raw := w.request(method, path, body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			w.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// refuse checks a refusal: the status and error code, and for 403 forbidden the policy named.
func (w *who) refuse(method, path string, body any, status int, code, message string) {
	w.t.Helper()
	var e apiError
	if got := w.do(method, path, body, &e); got != status || e.Error.Code != code || (message != "" && e.Error.Message != message) {
		w.t.Fatalf("%s %s: %d %+v, want %d %s %q", method, path, got, e.Error, status, code, message)
	}
}

func (w *who) ok(status int, method, path string, body any, out any) {
	w.t.Helper()
	if got := w.do(method, path, body, out); got != status {
		w.t.Fatalf("%s %s: status %d, want %d", method, path, got, status)
	}
}

func (s *server) as() *who {
	return &who{t: s.t, base: s.base, bearer: token, sendCSRF: true}
}

// login signs in and returns the session client; it fails the test when the login is refused.
func (s *server) login(login, password string) *who {
	s.t.Helper()
	w := &who{t: s.t, base: s.base, sendCSRF: true}
	resp, raw := w.request("POST", "/auth/login", map[string]string{"login": login, "password": password})
	if resp.StatusCode != 200 {
		s.t.Fatalf("login %s: %d %s", login, resp.StatusCode, raw)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "hydrogen_session" {
			w.cookie = c.Value
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/api" {
				s.t.Errorf("session cookie flags: %+v", c)
			}
		}
	}
	var out struct {
		User struct {
			Login string `json:"login"`
		} `json:"user"`
		CSRF string `json:"csrf"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.CSRF == "" || out.User.Login != login || w.cookie == "" {
		s.t.Fatalf("login response %s (cookie %q): %v", raw, w.cookie, err)
	}
	if strings.Contains(string(raw), "passwordHash") || strings.Contains(string(raw), "pbkdf2") {
		s.t.Fatalf("login leaks the hash: %s", raw)
	}
	w.csrf = out.CSRF
	return w
}

type idOnly struct {
	ID string `json:"id"`
}

func (s *server) newUser(login string, roles map[string]map[string][]string) idOnly {
	s.t.Helper()
	root := s.as()
	var u idOnly
	root.ok(201, "POST", "/users", map[string]any{"login": login, "password": userPassword}, &u)
	for role, limits := range roles {
		var r idOnly
		root.ok(200, "GET", "/roles/"+s.roleID(role), nil, &r)
		root.ok(201, "POST", "/assignments", map[string]any{"roleId": r.ID, "principalKind": "user", "principalId": u.ID,
			"limitations": limits}, nil)
	}
	return u
}

func (s *server) roleID(name string) string {
	s.t.Helper()
	var out struct {
		Items []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}
	s.as().ok(200, "GET", "/roles?limit=500", nil, &out)
	for _, r := range out.Items {
		if r.Name == name {
			return r.ID
		}
	}
	s.t.Fatalf("role %q not seeded", name)
	return ""
}

type listed struct {
	Items []struct {
		ID string `json:"id"`
	} `json:"items"`
	Total int `json:"total"`
}

type itemWithActions struct {
	ID      string `json:"id"`
	Actions *struct {
		Edit, Publish, Delete bool
	} `json:"actions"`
}

func accessScenario(t *testing.T, s *server) {
	f := buildSites(s)
	root := s.as()

	t.Run("seeded roles and policies", func(t *testing.T) {
		var pol struct {
			Policies []struct {
				Name string `json:"name"`
			} `json:"policies"`
			Limitations []struct {
				Name    string                   `json:"name"`
				Choices []struct{ Value string } `json:"choices"`
			} `json:"limitations"`
		}
		root.ok(200, "GET", "/policies", nil, &pol)
		var chans []string
		for _, l := range pol.Limitations {
			if l.Name == "channel" {
				for _, c := range l.Choices {
					chans = append(chans, c.Value)
				}
			}
		}
		if !contains(chans, "careers_en") || !contains(chans, "corporate_de") || len(pol.Policies) < 10 {
			t.Errorf("catalog: channels %v, %d policies", chans, len(pol.Policies))
		}
		for _, r := range []string{"administrator", "editor", "contributor", "ai-contributor", "viewer"} {
			s.roleID(r)
		}
	})

	t.Run("the config token and unauthenticated callers", func(t *testing.T) {
		anon := &who{t: t, base: s.base}
		anon.refuse("GET", "/types", nil, 401, "unauthorized", "")
		anon.refuse("GET", "/openapi.json", nil, 401, "unauthorized", "")
		bad := &who{t: t, base: s.base, bearer: "hyt_notatoken"}
		bad.refuse("GET", "/types", nil, 401, "unauthorized", "")
		root.ok(200, "GET", "/items/"+f.article, nil, nil)
	})

	s.newUser("ann", map[string]map[string][]string{"editor": {"channel": {"careers_en"}}})

	t.Run("editor scoped to the careers channel", func(t *testing.T) {
		ann := s.login("ann", userPassword)
		var sess struct {
			Principal struct{ Kind, Name string } `json:"principal"`
			Grants    []struct{ Policy string }   `json:"grants"`
			CSRF      string                      `json:"csrf"`
		}
		ann.ok(200, "GET", "/auth/session", nil, &sess)
		if sess.Principal.Kind != "user" || sess.Principal.Name != "ann" || sess.CSRF != ann.csrf || len(sess.Grants) == 0 {
			t.Errorf("session %+v", sess)
		}

		// Writes with the cookie need the CSRF token.
		ann.sendCSRF = false
		ann.refuse("POST", "/collections/landing/items", map[string]any{"locale": "en", "fields": title("Careers home")}, 403, "csrf", "")
		ann.csrf = "wrong"
		ann.sendCSRF = true
		ann.refuse("POST", "/collections/landing/items", map[string]any{"locale": "en", "fields": title("Careers home")}, 403, "csrf", "")
		ann.csrf = s.loginCSRF(ann)

		var landing itemWithActions
		ann.ok(201, "POST", "/collections/landing/items", map[string]any{"locale": "en", "fields": title("Careers home")}, &landing)
		if landing.Actions == nil || !landing.Actions.Edit || !landing.Actions.Publish || !landing.Actions.Delete {
			t.Errorf("actions %+v", landing.Actions)
		}
		ann.ok(200, "PATCH", "/items/"+landing.ID, map[string]any{"locale": "en", "fields": title("Careers home, edited")}, nil)
		ann.ok(200, "POST", "/items/"+landing.ID+"/publish", map[string]any{}, nil)
		var page itemWithActions
		ann.ok(201, "POST", "/collections/page/items", map[string]any{"locale": "en", "fields": title("Benefits")}, &page)
		ann.ok(200, "POST", "/items/"+page.ID+"/publish", map[string]any{}, nil)

		// Locales outside the channel are refused for creating and translating.
		ann.refuse("POST", "/collections/landing/items", map[string]any{"locale": "de", "fields": title("Karriere")}, 403, "forbidden", "content.create")
		ann.refuse("PATCH", "/items/"+landing.ID, map[string]any{"locale": "de", "fields": title("Karriere")}, 403, "forbidden", "content.edit")

		// Corporate collections are refused in every way: creating is forbidden, and what ann cannot read is not found.
		ann.refuse("POST", "/collections/article/items", map[string]any{"locale": "en", "fields": title("Nope")}, 403, "forbidden", "content.create")
		ann.refuse("GET", "/items/"+f.article, nil, 404, "not_found", "")
		ann.refuse("GET", "/items/"+f.productEN+"/versions", nil, 404, "not_found", "")
		ann.refuse("GET", "/items/"+f.productEN+"/references", nil, 404, "not_found", "")
		ann.refuse("PATCH", "/items/"+f.article, map[string]any{"fields": title("Hacked")}, 404, "not_found", "")
		ann.refuse("POST", "/items/"+f.article+"/publish", map[string]any{}, 404, "not_found", "")
		ann.refuse("DELETE", "/items/"+f.article+"/draft", nil, 404, "not_found", "")
		ann.refuse("DELETE", "/items/"+f.article, nil, 404, "not_found", "")
		ann.refuse("POST", "/preview-tokens", map[string]any{"channel": "corporate_en", "item": f.article}, 404, "not_found", "")

		// A channel-wide preview token shows every draft of the channel, so it needs the whole channel.
		ann.ok(200, "POST", "/preview-tokens", map[string]any{"channel": "careers_en"}, nil)
		ann.ok(200, "POST", "/preview-tokens", map[string]any{"channel": "careers_en", "item": landing.ID}, nil)
		ann.refuse("POST", "/preview-tokens", map[string]any{"channel": "corporate_en"}, 403, "forbidden", "content.preview")

		// The editor template carries pages.read and pages.edit, so the careers site and its tree are reachable.
		var sites struct {
			Items []string `json:"items"`
		}
		ann.ok(200, "GET", "/pages/sites", nil, &sites)
		if !contains(sites.Items, "careers") {
			t.Errorf("editor sees sites %v, want careers", sites.Items)
		}
		ann.ok(200, "GET", "/pages/sites/careers/tree", nil, nil)

		// Listings show only the collections of the channel.
		var l listed
		ann.ok(200, "GET", "/collections/article/items", nil, &l)
		if l.Total != 0 || len(l.Items) != 0 {
			t.Errorf("corporate article listing leaks: %+v", l)
		}
		ann.ok(200, "GET", "/collections/product/items?locale=en", nil, &l)
		if l.Total != 0 {
			t.Errorf("product listing leaks: %+v", l)
		}
		ann.ok(200, "GET", "/collections/landing/items", nil, &l)
		if l.Total != 1 || l.Items[0].ID != landing.ID {
			t.Errorf("landing listing: %+v", l)
		}
		root.ok(200, "GET", "/collections/article/items", nil, &l)
		if l.Total == 0 {
			t.Error("the config token must see the corporate articles")
		}

		// Other administration is refused.
		ann.refuse("GET", "/users", nil, 403, "forbidden", "user.manage")
		ann.refuse("POST", "/roles", map[string]any{"name": "x", "policies": []any{}}, 403, "forbidden", "role.manage")
		ann.refuse("GET", "/tokens", nil, 403, "forbidden", "token.manage")
		ann.refuse("GET", "/audit", nil, 403, "forbidden", "audit.read")

		// A header beats the cookie: a bad token does not fall back to the session.
		mixed := *ann
		mixed.bearer = "hyt_wrong"
		mixed.refuse("GET", "/types", nil, 401, "unauthorized", "")

		// Logout needs CSRF, ends the session, and the cookie is dead afterwards.
		ann.sendCSRF = false
		ann.refuse("POST", "/auth/logout", nil, 403, "csrf", "")
		ann.sendCSRF = true
		ann.ok(204, "POST", "/auth/logout", nil, nil)
		ann.refuse("GET", "/types", nil, 401, "unauthorized", "")
		ann.refuse("POST", "/collections/landing/items", map[string]any{"locale": "en", "fields": title("Late")}, 401, "unauthorized", "")

		// Audit records the editor as a user.
		var a struct {
			Items []struct {
				ActorKind string `json:"actorKind"`
				ActorID   string `json:"actorId"`
				Action    string `json:"action"`
			} `json:"items"`
		}
		root.ok(200, "GET", "/audit?action=item.publish&actor=user", nil, &a)
		if len(a.Items) == 0 || a.Items[0].ActorKind != "user" {
			t.Errorf("audit %+v", a)
		}
	})

	t.Run("ai contributor agent token", func(t *testing.T) {
		var created struct {
			Token  idOnly `json:"token"`
			Secret string `json:"secret"`
		}
		root.ok(201, "POST", "/tokens", map[string]any{"name": "newsbot", "kind": "agent", "roles": []string{"ai-contributor"}}, &created)
		if !strings.HasPrefix(created.Secret, "hyt_") {
			t.Fatalf("secret %q", created.Secret)
		}
		var listedTokens string
		_, raw := root.request("GET", "/tokens", nil)
		listedTokens = string(raw)
		if strings.Contains(listedTokens, created.Secret) || strings.Contains(listedTokens, "secretHash") {
			t.Fatalf("tokens list leaks the secret: %s", listedTokens)
		}
		bot := &who{t: t, base: s.base, bearer: created.Secret, headers: map[string]string{
			"X-Hydrogen-Agent-Model": "claude-test-1", "X-Hydrogen-Agent-Task": "draft the news"}}
		var draft idOnly
		bot.ok(201, "POST", "/collections/article/items", map[string]any{"locale": "en", "fields": title("Machine news")}, &draft)
		bot.ok(200, "PATCH", "/items/"+draft.ID, map[string]any{"fields": title("Machine news, edited")}, nil)
		bot.ok(200, "GET", "/items/"+draft.ID+"?draft=true", nil, nil)
		bot.refuse("POST", "/items/"+draft.ID+"/publish", map[string]any{}, 403, "forbidden", "content.publish")
		bot.refuse("DELETE", "/items/"+draft.ID, nil, 403, "forbidden", "content.delete")
		bot.refuse("DELETE", "/items/"+f.article, nil, 403, "forbidden", "content.delete")
		bot.refuse("GET", "/tokens", nil, 403, "forbidden", "token.manage")
		bot.refuse("POST", "/tokens", map[string]any{"name": "mine", "roles": []string{"administrator"}}, 403, "forbidden", "token.manage")

		var a struct {
			Items []struct {
				ActorKind string          `json:"actorKind"`
				ActorID   string          `json:"actorId"`
				Action    string          `json:"action"`
				Subject   string          `json:"subject"`
				Data      json.RawMessage `json:"data"`
			} `json:"items"`
		}
		root.ok(200, "GET", "/audit?actor=agent:"+created.Token.ID+"&subject="+draft.ID, nil, &a)
		if len(a.Items) != 2 {
			t.Fatalf("agent audit entries: %+v", a)
		}
		for _, e := range a.Items {
			var d struct {
				Actor map[string]string `json:"actor"`
			}
			if e.ActorKind != "agent" || json.Unmarshal(e.Data, &d) != nil || d.Actor["model"] != "claude-test-1" || d.Actor["task"] != "draft the news" {
				t.Errorf("entry %+v", e)
			}
		}
		root.refuse("GET", "/audit?since=yesterday", nil, 400, "bad_request", "")

		// Revoked tokens fail at once.
		root.ok(204, "DELETE", "/tokens/"+created.Token.ID, nil, nil)
		bot.refuse("GET", "/types", nil, 401, "unauthorized", "")
		root.ok(200, "GET", "/items/"+draft.ID+"?draft=true", nil, nil)
	})

	t.Run("expired and disabled", func(t *testing.T) {
		var created struct {
			Token  idOnly `json:"token"`
			Secret string `json:"secret"`
		}
		exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
		root.ok(201, "POST", "/tokens", map[string]any{"name": "short", "roles": []string{"viewer"}, "expiresAt": exp}, &created)
		(&who{t: t, base: s.base, bearer: created.Secret}).ok(200, "GET", "/items/"+f.article, nil, nil)
		root.refuse("POST", "/tokens", map[string]any{"name": "past", "expiresAt": "2020-01-01T00:00:00Z"}, 400, "bad_request", "")

		bob := s.newUser("bob", map[string]map[string][]string{"viewer": nil})
		session := s.login("bob", userPassword)
		session.ok(200, "GET", "/items/"+f.article, nil, nil)
		session.refuse("PATCH", "/items/"+f.article, map[string]any{"fields": title("x")}, 403, "forbidden", "content.edit")
		root.ok(200, "PATCH", "/users/"+bob.ID, map[string]any{"disabled": true}, nil)
		session.refuse("GET", "/items/"+f.article, nil, 401, "unauthorized", "")
		var e apiError
		anon := &who{t: t, base: s.base}
		if got := anon.do("POST", "/auth/login", map[string]string{"login": "bob", "password": userPassword}, &e); got != 401 {
			t.Errorf("disabled user logged in: %d", got)
		}
	})

	t.Run("roles through groups", func(t *testing.T) {
		var group idOnly
		root.ok(201, "POST", "/groups", map[string]any{"name": "careers-team", "description": "Hiring"}, &group)
		root.ok(201, "POST", "/assignments", map[string]any{"roleId": s.roleID("editor"), "principalKind": "group",
			"principalId": group.ID, "limitations": map[string][]string{"channel": {"careers_en"}}}, nil)
		gina := s.newUser("gina", nil)
		session := s.login("gina", userPassword)
		fields := map[string]any{"locale": "en", "fields": title("Team page")}
		session.refuse("POST", "/collections/landing/items", fields, 403, "forbidden", "content.create")

		root.ok(204, "PUT", "/groups/"+group.ID+"/members/"+gina.ID, nil, nil)
		session.ok(201, "POST", "/collections/landing/items", fields, nil)
		session.refuse("POST", "/collections/article/items", fields, 403, "forbidden", "content.create")
		var members struct {
			Items []struct{ Login string } `json:"items"`
			Total int                      `json:"total"`
		}
		root.ok(200, "GET", "/groups/"+group.ID+"/members", nil, &members)
		if members.Total != 1 || members.Items[0].Login != "gina" {
			t.Errorf("members %+v", members)
		}

		root.ok(204, "DELETE", "/groups/"+group.ID+"/members/"+gina.ID, nil, nil)
		session.refuse("POST", "/collections/landing/items", fields, 403, "forbidden", "content.create")
		root.ok(204, "PUT", "/groups/"+group.ID+"/members/"+gina.ID, nil, nil)
		session.ok(201, "POST", "/collections/landing/items", fields, nil)
		// Deleting the group takes its roles away at once.
		root.ok(204, "DELETE", "/groups/"+group.ID, nil, nil)
		session.refuse("POST", "/collections/landing/items", fields, 403, "forbidden", "content.create")
		root.refuse("GET", "/groups/"+group.ID, nil, 404, "not_found", "")
	})

	t.Run("no privilege escalation", func(t *testing.T) {
		// lead may manage roles and users but does not hold publish rights, so cannot hand them out.
		var role idOnly
		root.ok(201, "POST", "/roles", map[string]any{"name": "lead", "policies": []map[string]any{
			{"policy": "role.manage"}, {"policy": "user.manage"}, {"policy": "content.read"}}}, &role)
		lead := s.newUser("lead", map[string]map[string][]string{"lead": nil})
		session := s.login("lead", userPassword)
		other := s.newUser("carl", nil)
		session.refuse("POST", "/assignments", map[string]any{"roleId": s.roleID("editor"), "principalKind": "user", "principalId": other.ID},
			403, "forbidden", "")
		session.refuse("POST", "/assignments", map[string]any{"roleId": s.roleID("administrator"), "principalKind": "user", "principalId": other.ID},
			403, "forbidden", "")
		session.refuse("POST", "/roles", map[string]any{"name": "sneaky", "policies": []map[string]any{{"policy": "content.publish"}}},
			403, "forbidden", "")
		session.refuse("PATCH", "/users/"+lead.ID, map[string]any{"disabled": true}, 400, "bad_request", "")
		session.ok(201, "POST", "/assignments", map[string]any{"roleId": role.ID, "principalKind": "user", "principalId": other.ID}, nil)
		session.refuse("GET", "/audit", nil, 403, "forbidden", "audit.read")
	})

	t.Run("login is throttled", func(t *testing.T) {
		anon := &who{t: t, base: s.base}
		// The limit is five failures per login and client; the sixth attempt is refused.
		throttled := false
		for i := 1; i <= 6 && !throttled; i++ {
			resp, raw := anon.request("POST", "/auth/login", map[string]string{"login": "ghost", "password": "wrong password here"})
			switch resp.StatusCode {
			case 401:
			case 429:
				throttled = true
				if i != 6 || resp.Header.Get("Retry-After") == "" || !strings.Contains(string(raw), "throttled") {
					t.Errorf("attempt %d: %d %v %s", i, resp.StatusCode, resp.Header, raw)
				}
			default:
				t.Fatalf("attempt %d: %d %s", i, resp.StatusCode, raw)
			}
		}
		if !throttled {
			t.Error("six failures were not throttled")
		}
		// Another login from the same client is not locked by that pair's limit.
		anon.ok(200, "POST", "/auth/login", map[string]string{"login": "ann", "password": userPassword}, nil)
	})
}

// loginCSRF reads the session's CSRF token again, as a client that reloaded would.
func (s *server) loginCSRF(w *who) string {
	s.t.Helper()
	var sess struct {
		CSRF string `json:"csrf"`
	}
	w.ok(200, "GET", "/auth/session", nil, &sess)
	return sess.CSRF
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestAccessSQLite(t *testing.T) {
	accessScenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configureWithProbe))
}

func TestAccessPostgres(t *testing.T) {
	accessScenario(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configureWithProbe))
}

// Two servers on one database: a role change on one is refused by the other within moments.
func TestAccessGrantInvalidationAcrossNodes(t *testing.T) {
	url := pgtest.Scratch(t)
	a := start(t, "prod", map[string]string{"DATABASE_URL": url}, configure)
	b := start(t, "prod", map[string]string{"DATABASE_URL": url}, configure)
	root := a.as()
	a.newUser("ann", map[string]map[string][]string{"editor": {"channel": {"careers_en"}}})
	var ass struct {
		Items []idOnly `json:"items"`
	}
	var u idOnly
	var users struct {
		Items []struct{ ID, Login string } `json:"items"`
	}
	root.ok(200, "GET", "/users", nil, &users)
	for _, x := range users.Items {
		if x.Login == "ann" {
			u.ID = x.ID
		}
	}
	root.ok(200, "GET", "/assignments?principalKind=user&principalId="+u.ID, nil, &ass)
	if len(ass.Items) != 1 {
		t.Fatalf("assignments %+v", ass)
	}

	ann := a.login("ann", userPassword)
	onB := &who{t: t, base: b.base, cookie: ann.cookie, csrf: ann.csrf, sendCSRF: true}
	var landing idOnly
	onB.ok(201, "POST", "/collections/landing/items", map[string]any{"locale": "en", "fields": title("One")}, &landing)

	root.ok(204, "DELETE", "/assignments/"+ass.Items[0].ID, nil, nil)
	deadline := time.Now().Add(5 * time.Second)
	for {
		var e apiError
		status := onB.do("POST", "/collections/landing/items", map[string]any{"locale": "en", "fields": title("Two")}, &e)
		if status == 403 && e.Error.Code == "forbidden" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("node b still allows the removed grant (last status %d)", status)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Disabling the user ends the session on the other node too.
	root.ok(200, "PATCH", "/users/"+u.ID, map[string]any{"disabled": true}, nil)
	onB.refuse("GET", "/types", nil, 401, "unauthorized", "")
}

// The bootstrap commands: the password and the token secret travel by environment, stdin and stdout only.
func TestBootstrapCommands(t *testing.T) {
	dbURL := "sqlite:" + filepath.Join(t.TempDir(), "h.db")
	vars := map[string]string{"DATABASE_URL": dbURL}
	opts := &config.Options{Dir: "config", Env: config.DevEnv, LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	run := func(stdin string, args ...string) (string, string, error) {
		var out, errOut bytes.Buffer
		err := hydrogen.Run(context.Background(), args, configure, hydrogen.RunOptions{Config: opts,
			Stdin: strings.NewReader(stdin), Stdout: &out, Stderr: &errOut, PasswordIterations: 1000})
		return out.String(), errOut.String(), err
	}
	if _, _, err := run("", "users:create-admin", "root"); err == nil || !strings.Contains(err.Error(), "migrate") {
		t.Fatalf("before migrating: %v", err)
	}
	if _, _, err := run("", "migrate"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HYDROGEN_ADMIN_PASSWORD", "")
	if _, _, err := run("short\n", "users:create-admin", "root"); err == nil || !strings.Contains(err.Error(), "password") {
		t.Errorf("a short password was accepted: %v", err)
	}
	out, errOut, err := run(userPassword+"\n", "users:create-admin", "Root")
	if err != nil || !strings.Contains(out, "root") || strings.Contains(out+errOut, userPassword) {
		t.Fatalf("create-admin: %q %q %v", out, errOut, err)
	}
	if _, _, err := run("", "users:create-admin", "root"); err == nil {
		t.Error("no password at all was accepted")
	}
	t.Setenv("HYDROGEN_ADMIN_PASSWORD", "another long password")
	if _, _, err := run("", "users:create-admin", "root"); err != nil {
		t.Fatalf("reset through the environment: %v", err)
	}

	for _, args := range [][]string{{"tokens:create"}, {"tokens:create", "-name", "x", "-role", "nope"}, {"tokens:create", "-name", "x", "-expires", "-1h"}} {
		if _, _, err := run("", args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
	secret, info, err := run("", "tokens:create", "-name", "ci", "-role", "viewer", "-agent", "-expires", "1h")
	secret = strings.TrimSpace(secret)
	if err != nil || !strings.HasPrefix(secret, "hyt_") || strings.Contains(secret, "\n") || strings.Contains(info, secret) {
		t.Fatalf("tokens:create: %q %q %v", secret, info, err)
	}

	s := start(t, config.DevEnv, vars, configure)
	admin := s.login("root", "another long password")
	admin.ok(200, "GET", "/users", nil, nil)
	old := &who{t: t, base: s.base}
	if got := old.do("POST", "/auth/login", map[string]string{"login": "root", "password": userPassword}, nil); got != 401 {
		t.Errorf("the old password still works: %d", got)
	}
	ci := &who{t: t, base: s.base, bearer: secret}
	ci.ok(200, "GET", "/types", nil, nil)
	ci.refuse("POST", "/collections/article/items", map[string]any{"locale": "en", "fields": title("x")}, 403, "forbidden", "content.create")
	var tokens struct {
		Items []struct{ Name, Kind, Prefix string } `json:"items"`
	}
	admin.ok(200, "GET", "/tokens", nil, &tokens)
	if len(tokens.Items) != 1 || tokens.Items[0].Kind != "agent" || !strings.HasPrefix(secret, tokens.Items[0].Prefix) {
		t.Errorf("tokens %+v", tokens)
	}
}
