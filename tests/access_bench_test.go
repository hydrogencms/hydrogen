package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// authNode is a delivery-test node with the role templates seeded, a signed-in editor and an editor token.
type authNode struct {
	*dnode
	item    ids.UUID
	session *http.Cookie
	csrf    string
	secret  string
}

func newAuthNode(tb testing.TB, be backend) *authNode {
	tb.Helper()
	clk := newClock()
	n := &authNode{dnode: newDeliveryNode(tb, be.open(tb, clk), clk)}
	s := n.app.Services()
	if err := s.Admin.SeedTemplates(ctx); err != nil {
		tb.Fatal(err)
	}
	role, err := s.Admin.RoleByName(ctx, "editor")
	if err != nil {
		tb.Fatal(err)
	}
	u, err := s.Admin.CreateUser(ctx, access.UserInput{Login: "ann", Password: authPassword})
	if err != nil {
		tb.Fatal(err)
	}
	if _, err := s.Admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: role.ID,
		Principal: store.PrincipalRef{Kind: store.PrincipalUser, ID: u.ID}}); err != nil {
		tb.Fatal(err)
	}
	tok, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: "ci", Roles: []ids.UUID{role.ID}})
	if err != nil {
		tb.Fatal(err)
	}
	_ = tok
	n.secret = secret
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/manage/v1/auth/login", nil)
	res, err := s.Auth.Login(w, r, "ann", authPassword)
	if err != nil {
		tb.Fatal(err)
	}
	n.csrf = res.CSRF
	for _, c := range w.Result().Cookies() {
		n.session = c
	}
	ann := n.create("person", "", "en", `{"fullName":"Ann"}`, true)
	n.item = n.create("article", "", "en", fmt.Sprintf(`{"title":"Article with a title of some length","rank":3,"author":"%s"}`, ann), true)
	return n
}

func (n *authNode) request(mod func(*http.Request)) *http.Request {
	r := httptest.NewRequest("GET", "/api/manage/v1/items/"+n.item.String(), nil)
	r.RemoteAddr = "127.0.0.1:50000" // the dev token only works from loopback
	mod(r)
	return r
}

// BenchmarkManageGetItem measures one management GET of an item per credential. The difference to the config
// token (no lookup, no checks) is the lookup of the session or token plus authorization; BenchmarkAuthenticate
// is the lookup alone. Each case reports its median as p50-ns.
func BenchmarkManageGetItem(b *testing.B) {
	for _, be := range backends {
		n := newAuthNode(b, be)
		cases := []struct {
			name string
			req  *http.Request
		}{
			{"config-token", n.request(func(r *http.Request) { r.Header.Set("Authorization", "Bearer dev-token") })},
			{"api-token", n.request(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+n.secret) })},
			{"session", n.request(func(r *http.Request) { r.AddCookie(n.session) })},
		}
		for _, c := range cases {
			b.Run(be.name+"/"+c.name, func(b *testing.B) {
				w := &nopWriter{h: http.Header{}}
				n.h.ServeHTTP(w, c.req) // warm the grant cache and the statements
				d := make([]time.Duration, 0, b.N)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					clear(w.h)
					t0 := time.Now()
					n.h.ServeHTTP(w, c.req)
					d = append(d, time.Since(t0))
				}
				b.StopTimer()
				slices.Sort(d)
				b.ReportMetric(float64(d[len(d)/2].Nanoseconds()), "p50-ns")
			})
		}
	}
}

// BenchmarkAuthenticate is the credential lookup alone: one indexed query (session and user, or token).
func BenchmarkAuthenticate(b *testing.B) {
	for _, be := range backends {
		n := newAuthNode(b, be)
		auth := n.app.Services().Auth
		for name, req := range map[string]*http.Request{
			"api-token": n.request(func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+n.secret) }),
			"session":   n.request(func(r *http.Request) { r.AddCookie(n.session) }),
		} {
			b.Run(be.name+"/"+name, func(b *testing.B) {
				d := make([]time.Duration, 0, b.N)
				for b.Loop() {
					t0 := time.Now()
					if _, err := auth.Authenticate(req); err != nil {
						b.Fatal(err)
					}
					d = append(d, time.Since(t0))
				}
				b.StopTimer()
				slices.Sort(d)
				b.ReportMetric(float64(d[len(d)/2].Nanoseconds()), "p50-ns")
			})
		}
	}
}

// BenchmarkAuthorize is the authorization of a warm principal: the checks of one Get through the service.
func BenchmarkAuthorize(b *testing.B) {
	n := newAuthNode(b, backends[0])
	s := n.app.Services()
	ann, err := s.Admin.GetUser(ctx, mustUser(b, n))
	if err != nil {
		b.Fatal(err)
	}
	p := &access.Principal{Kind: access.KindUser, ID: ann.ID, Name: "ann"}
	pctx := access.WithPrincipal(ctx, p)
	t := access.Target{Type: "article", Locales: []string{"en"}}
	if err := s.Access.Can(pctx, access.PolicyContentEdit, t); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := s.Access.Prepare(pctx); err != nil {
			b.Fatal(err)
		}
		if err := s.Access.Can(pctx, access.PolicyContentEdit, t); err != nil {
			b.Fatal(err)
		}
	}
}

func mustUser(tb testing.TB, n *authNode) ids.UUID {
	tb.Helper()
	us, _, err := n.app.Services().Admin.ListUsers(ctx, store.Page{})
	if err != nil || len(us) == 0 {
		tb.Fatal(err)
	}
	return us[0].ID
}

// The Local API checks the principal in the context and leaves the system alone.
func TestLocalAPIPrincipal(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		bot, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: "bot", Kind: "agent", Roles: []ids.UUID{roleID(t, n, "ai-contributor")}})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		r.Header.Set("X-Hydrogen-Agent-Model", "m")
		an, err := s.Auth.Authenticate(r)
		if err != nil || an.Principal.Kind != access.KindAgent || an.Principal.ID != bot.ID {
			t.Fatalf("authenticate: %+v %v", an, err)
		}
		pctx := access.WithPrincipal(context.Background(), an.Principal)
		v, err := s.Content.Create(pctx, content.CreateInput{Type: "article", Fields: doc(t, `{"title":"Drafted"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if v.Actions == nil || !v.Actions.Edit || v.Actions.Publish || v.Actions.Delete {
			t.Errorf("actions %+v", v.Actions)
		}
		if _, err := s.Content.Publish(pctx, v.ID, content.PublishOptions{}); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("publish: %v", err)
		}
		if err := s.Content.Delete(pctx, v.ID); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("delete: %v", err)
		}
		if _, err := s.Content.Publish(context.Background(), v.ID, content.PublishOptions{}); err != nil {
			t.Errorf("the system must publish: %v", err)
		}
		if got, err := s.Content.Get(context.Background(), v.ID, content.GetOptions{}); err != nil || got.Actions != nil {
			t.Errorf("system view: %+v %v", got, err)
		}
		var evs []store.AuditEvent
		if err := s.DB.View(ctx, func(tx store.Tx) (err error) {
			evs, err = tx.Audit().List(ctx, store.AuditQuery{Subject: v.ID.String()})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range evs {
			var d struct {
				Actor map[string]string `json:"actor"`
			}
			found = found || (e.Action == "item.create" && e.ActorKind == "agent" && json.Unmarshal(e.Data, &d) == nil && d.Actor["model"] == "m")
		}
		if !found {
			t.Errorf("audit lacks the agent and its model: %+v", evs)
		}
	})
}

func roleID(tb testing.TB, n *authNode, name string) ids.UUID {
	tb.Helper()
	r, err := n.app.Services().Admin.RoleByName(ctx, name)
	if err != nil {
		tb.Fatal(err)
	}
	return r.ID
}

// The auth.cleanup job removes expired sessions.
func TestAuthCleanupJob(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		hash := access.HashSecret(n.session.Value)
		session := func() error {
			return s.DB.View(ctx, func(tx store.Tx) error { _, err := tx.Auth().SessionByHash(ctx, hash); return err })
		}
		if err := n.app.RunJob(ctx, "auth.cleanup"); err != nil || session() != nil {
			t.Fatalf("a live session must stay: %v", session())
		}
		n.clk.Advance(30 * 24 * time.Hour)
		if err := n.app.RunJob(ctx, "auth.cleanup"); err != nil {
			t.Fatal(err)
		}
		if err := session(); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("expired session still stored: %v", err)
		}
	})
}
