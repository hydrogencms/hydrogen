package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/modules/site"
)

func formsTheme() site.Theme {
	return site.Theme{Name: "forms", Base: "starter", FS: fstest.MapFS{
		"templates/routes/form.html": {Data: []byte(`{{define "content"}}<form method="post">{{formGuard}}<input name="name"></form>{{end}}`)},
	}}
}

func jsonOut(status int, v any) *site.Response { return &site.Response{Status: status, JSON: v} }

func formRoutes(r *siteRegistrar) {
	site.UseTheme(r, formsTheme())
	site.Handle(r, "GET /forms/contact", func(q *site.Request) (*site.Response, error) {
		return &site.Response{Template: "routes/form.html", Title: "Contact"}, nil
	}).Sample(func() *site.Response { return &site.Response{Template: "routes/form.html"} })
	site.Handle(r, "POST /forms/contact", func(q *site.Request) (*site.Response, error) {
		id, err := q.Submit("submission", map[string]any{"sender": q.R.PostFormValue("name"), "email": q.R.PostFormValue("email"), "message": q.R.PostFormValue("message")})
		switch {
		case err == nil:
			return &site.Response{Redirect: q.URL("/forms/thanks?id=" + id)}, nil
		case site.FieldErrors(err) != nil:
			return jsonOut(422, site.FieldErrors(err)), nil
		case err == site.ErrTooFast:
			return jsonOut(400, "fast"), nil
		case err == site.ErrFormInvalid:
			return jsonOut(400, "invalid"), nil
		}
		return nil, err
	})
	site.Handle(r, "POST /forms/other", func(q *site.Request) (*site.Response, error) {
		if _, err := q.Submit("article", map[string]any{"title": "x"}); err != nil {
			return jsonOut(403, err.Error()), nil
		}
		return jsonOut(200, "ok"), nil
	})
	site.Handle(r, "POST /forms/client", func(q *site.Request) (*site.Response, error) { return jsonOut(200, "ok"), nil }).
		Limit(site.Limit{PerClient: 2, Window: time.Minute})
	site.Handle(r, "POST /forms/total", func(q *site.Request) (*site.Response, error) { return jsonOut(200, "ok"), nil }).
		Limit(site.Limit{Total: 2, Window: time.Minute})
}

var guardRE = regexp.MustCompile(`name="_hg_form" value="([^"]+)"`)

func (e *siteEnv) post(path string, form url.Values, hdr map[string]string, remote string) siteReply {
	e.t.Helper()
	req := httptest.NewRequest("POST", "http://site.test"+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return siteReply{rec.Code, rec.Header(), rec.Body.String()}
}

// token renders the form and waits past the minimum fill time.
func (e *siteEnv) token(wait time.Duration) string {
	e.t.Helper()
	r := e.get("/en/forms/contact")
	m := guardRE.FindStringSubmatch(r.body)
	if r.status != 200 || m == nil || !strings.Contains(r.body, `name="website_url"`) {
		e.t.Fatalf("form: %d %s", r.status, r.body)
	}
	e.clk.Advance(wait)
	return m[1]
}

func (e *siteEnv) submissions() []content.Summary {
	e.t.Helper()
	l, err := e.c.List(context.Background(), content.ListOptions{Type: "submission", Limit: 50})
	if err != nil {
		e.t.Fatal(err)
	}
	return l.Items
}

func runForms(t *testing.T, fn func(t *testing.T, e *siteEnv)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e, err := newSiteEnv(t, b.open(t, clk), clk, "forms", formRoutes)
		if err != nil {
			t.Fatal(err)
		}
		fn(t, e)
	})
}

func TestSiteFormSubmit(t *testing.T) {
	runForms(t, func(t *testing.T, e *siteEnv) {
		same := map[string]string{"Origin": "http://site.test"}
		form := func(tok string, kv ...string) url.Values {
			v := url.Values{"_hg_form": {tok}}
			for i := 0; i < len(kv); i += 2 {
				v.Set(kv[i], kv[i+1])
			}
			return v
		}

		tok := e.token(5 * time.Second)
		r := e.post("/en/forms/contact", form(tok, "name", "Ada", "email", "ada@example.com", "message", "hi"), same, "")
		if r.status != 303 || !strings.HasPrefix(r.header.Get("Location"), "/en/forms/thanks?id=") {
			t.Fatalf("submit: %d %s %v", r.status, r.body, r.header)
		}
		items := e.submissions()
		if len(items) != 1 {
			t.Fatalf("submissions = %d", len(items))
		}
		v, err := e.c.Get(context.Background(), items[0].ID, content.GetOptions{Draft: true})
		if err != nil {
			t.Fatal(err)
		}
		if v.Fields["sender"] != "Ada" || v.Fields["email"] != "ada@example.com" || v.PublishedAt != nil || v.Status == "published" {
			t.Errorf("item = %+v", v)
		}
		var found bool
		err = e.app.Services().DB.View(context.Background(), func(tx store.Tx) error {
			evs, err := tx.Audit().List(context.Background(), store.AuditQuery{Subject: v.ID.String(), Limit: 10})
			for _, ev := range evs {
				if ev.Action == "item.create" && ev.ActorKind == "system" && ev.ActorID == "site:/forms/contact" && strings.Contains(string(ev.Data), "192.0.2.1") {
					found = true
				}
			}
			return err
		})
		if err != nil || !found {
			t.Errorf("audit actor not recorded: %v", err)
		}

		// required field missing: structured errors, nothing stored
		tok = e.token(5 * time.Second)
		r = e.post("/en/forms/contact", form(tok, "email", "x@example.com"), same, "")
		if r.status != 422 || !strings.Contains(r.body, `"sender"`) || len(e.submissions()) != 1 {
			t.Errorf("validation: %d %s", r.status, r.body)
		}

		// type not allowed
		if r := e.post("/en/forms/other", form(tok), same, ""); r.status != 403 || !strings.Contains(r.body, "not open for submissions") {
			t.Errorf("type: %d %s", r.status, r.body)
		}

		// cross-origin is refused before the route
		if r := e.post("/en/forms/contact", form(tok, "name", "Eve"), map[string]string{"Origin": "http://evil.test"}, ""); r.status != 403 {
			t.Errorf("cross-origin: %d", r.status)
		}

		// honeypot: looks like a success, stores nothing
		r = e.post("/en/forms/contact", form(tok, "name", "Bot", "website_url", "http://spam"), same, "")
		if r.status != 303 || len(e.submissions()) != 1 {
			t.Errorf("honeypot: %d, %d items", r.status, len(e.submissions()))
		}

		// too fast, missing and forged tokens, expired token
		fast := e.token(0)
		if r := e.post("/en/forms/contact", form(fast, "name", "Quick"), same, ""); r.status != 400 || !strings.Contains(r.body, "fast") {
			t.Errorf("too fast: %d %s", r.status, r.body)
		}
		for name, bad := range map[string]string{"missing": "", "forged": "1.deadbeef", "garbage": "x"} {
			if r := e.post("/en/forms/contact", form(bad, "name", "N"), same, ""); r.status != 400 || !strings.Contains(r.body, "invalid") {
				t.Errorf("%s token: %d %s", name, r.status, r.body)
			}
		}
		old := e.token(5 * time.Hour)
		if r := e.post("/en/forms/contact", form(old, "name", "Late"), same, ""); r.status != 400 || !strings.Contains(r.body, "invalid") {
			t.Errorf("expired: %d %s", r.status, r.body)
		}
		if n := len(e.submissions()); n != 1 {
			t.Errorf("submissions after rejections = %d", n)
		}
	})
}

func TestSiteRateLimit(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		e1, err := newSiteEnv(t, db, clk, "forms", formRoutes)
		if err != nil {
			t.Fatal(err)
		}
		// a second server on the same database shares the counters
		e2, err := newSiteEnv(t, db, clk, "forms", formRoutes)
		if err != nil {
			t.Fatal(err)
		}
		same := map[string]string{"Origin": "http://site.test"}
		a, b2 := "192.0.2.10:1000", "192.0.2.11:1000"
		status := func(e *siteEnv, path, remote string) int {
			return e.post(path, url.Values{}, same, remote).status
		}
		if s1, s2 := status(e1, "/en/forms/client", a), status(e2, "/en/forms/client", a); s1 != 200 || s2 != 200 {
			t.Fatalf("first two: %d %d", s1, s2)
		}
		r := e1.post("/en/forms/client", url.Values{}, same, a)
		if r.status != 429 || r.header.Get("Retry-After") == "" {
			t.Fatalf("third: %d %v", r.status, r.header)
		}
		if s := status(e2, "/en/forms/client", a); s != 429 {
			t.Errorf("other node: %d", s)
		}
		if s := status(e1, "/en/forms/client", b2); s != 200 {
			t.Errorf("other client: %d", s)
		}
		clk.Advance(61 * time.Second)
		if s := status(e2, "/en/forms/client", a); s != 200 {
			t.Errorf("after the window: %d", s)
		}

		// total: all clients together, across nodes
		for i, c := range []struct {
			e      *siteEnv
			remote string
			want   int
		}{{e1, a, 200}, {e2, b2, 200}, {e1, "192.0.2.12:1", 429}, {e2, "192.0.2.13:1", 429}} {
			if s := status(c.e, "/en/forms/total", c.remote); s != c.want {
				t.Errorf("total %d: %d, want %d", i, s, c.want)
			}
		}
		// a forwarded address counts only from a trusted proxy: without one the peer is the client
		spoof := map[string]string{"Origin": "http://site.test", "X-Forwarded-For": "198.51.100.77"}
		for range 2 {
			e1.post("/en/forms/client", url.Values{}, spoof, "192.0.2.20:1")
		}
		if r := e1.post("/en/forms/client", url.Values{}, spoof, "192.0.2.20:1"); r.status != http.StatusTooManyRequests {
			t.Errorf("spoofed header: %d", r.status)
		}
	})
}
