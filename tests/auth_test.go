package tests

import (
	"context"
	"crypto/pbkdf2"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

const authPassword = "correct horse battery"

// testIterations keeps PBKDF2 cheap in tests.
const testIterations = 1000

type authEnv struct {
	t      *testing.T
	db     store.DB
	clk    *clock.Manual
	auth   *access.Authenticator
	admin  *access.Admin
	chk    *access.Checker
	ob     *outbox.Outbox
	mu     sync.Mutex
	events []outbox.Event
}

func newAuthEnv(t *testing.T, b backend, mod func(*access.AuthOptions)) *authEnv {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	db := b.open(t, clk)
	e := &authEnv{t: t, db: db, clk: clk}
	var err error
	e.ob, err = outbox.New(db, clk, nil, []outbox.Consumer{{Name: "probe", Topics: []string{"auth.*"},
		Handle: func(_ context.Context, ev outbox.Event) error {
			e.mu.Lock()
			e.events = append(e.events, ev)
			e.mu.Unlock()
			return nil
		}}}, outbox.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sc := buildSchema(t, standardSchema)
	cat := access.NewCatalog()
	for _, p := range access.CorePolicies() {
		if err := cat.AddPolicy(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range access.CoreLimitations(sc, nil) {
		if err := cat.AddLimitation(l); err != nil {
			t.Fatal(err)
		}
	}
	e.chk = access.NewChecker(access.CheckerOptions{DB: db, Catalog: cat, Schema: sc})
	e.admin, err = access.NewAdmin(access.AdminOptions{DB: db, Outbox: e.ob, Audit: audit.New(clk), Clock: clk,
		IDs: ids.NewGenerator(clk, nil), Catalog: cat, Checker: e.chk, PasswordIterations: testIterations})
	if err != nil {
		t.Fatal(err)
	}
	o := access.AuthOptions{DB: db, Clock: clk, InsecureCookies: false, ConfigToken: "config-secret", PasswordIterations: testIterations}
	if mod != nil {
		mod(&o)
	}
	if e.auth, err = access.NewAuthenticator(o); err != nil {
		t.Fatal(err)
	}
	if err := e.admin.SeedTemplates(context.Background()); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *authEnv) role(name string) access.Role {
	e.t.Helper()
	r, err := e.admin.RoleByName(context.Background(), name)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *authEnv) user(login string, roles ...string) access.User {
	e.t.Helper()
	ctx := context.Background()
	u, err := e.admin.CreateUser(ctx, access.UserInput{Login: login, Password: authPassword})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, r := range roles {
		_, err := e.admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: e.role(r).ID,
			Principal: store.PrincipalRef{Kind: store.PrincipalUser, ID: u.ID}})
		if err != nil {
			e.t.Fatal(err)
		}
	}
	return u
}

func (e *authEnv) request(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "203.0.113.5:4000"
	return r
}

func (e *authEnv) login(login, password string) (*access.LoginResult, *http.Cookie, error) {
	return e.loginFrom("203.0.113.5", login, password)
}

func (e *authEnv) loginFrom(ip, login, password string) (*access.LoginResult, *http.Cookie, error) {
	w := httptest.NewRecorder()
	r := e.request("POST", "/api/manage/v1/auth/login")
	r.RemoteAddr = ip + ":4000"
	res, err := e.auth.Login(w, r, login, password)
	for _, c := range w.Result().Cookies() {
		if c.Name == access.SessionCookie {
			return res, c, err
		}
	}
	return res, nil, err
}

// as logs in and returns a context carrying the user's principal.
func (e *authEnv) as(login string) context.Context {
	e.t.Helper()
	_, c, err := e.login(login, authPassword)
	if err != nil {
		e.t.Fatal(err)
	}
	r := e.request("GET", "/")
	r.AddCookie(c)
	an, err := e.auth.Authenticate(r)
	if err != nil {
		e.t.Fatal(err)
	}
	return access.WithPrincipal(context.Background(), an.Principal)
}

func (e *authEnv) audits(action string) []store.AuditEvent {
	e.t.Helper()
	var out []store.AuditEvent
	err := e.db.View(context.Background(), func(tx store.Tx) (err error) {
		out, err = tx.Audit().List(context.Background(), store.AuditQuery{Action: action, Limit: 500})
		return
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *authEnv) authenticate(c *http.Cookie, bearer string) (*access.Authn, error) {
	r := e.request("GET", "/")
	if c != nil {
		r.AddCookie(c)
	}
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	return e.auth.Authenticate(r)
}

func TestAuthLoginSession(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		u := e.user("alice", "viewer")
		res, c, err := e.login("Alice ", authPassword)
		if err != nil {
			t.Fatal(err)
		}
		if res.User.Login != "alice" || res.User.PasswordHash != "" || res.CSRF == "" {
			t.Fatalf("result %+v", res)
		}
		if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/api" || c.MaxAge != 7*24*3600 {
			t.Fatalf("cookie flags %+v", c)
		}
		if res.ExpiresAt != e.clk.Now().Add(12*time.Hour) {
			t.Fatalf("expires %v", res.ExpiresAt)
		}
		an, err := e.authenticate(c, "")
		if err != nil {
			t.Fatal(err)
		}
		if an.Principal.Kind != access.KindUser || an.Principal.ID != u.ID || an.Principal.Name != "alice" || an.Session.CSRF != res.CSRF {
			t.Fatalf("authn %+v", an.Principal)
		}
		// the cookie value is not stored
		var s store.Session
		_ = e.db.View(context.Background(), func(tx store.Tx) (err error) {
			s, err = tx.Auth().SessionByHash(context.Background(), access.HashSecret(c.Value))
			return
		})
		if string(s.IDHash) == c.Value || s.UserID != u.ID || s.IP != "203.0.113.5" {
			t.Fatalf("stored session %+v", s)
		}
		if got, _ := e.admin.GetUser(context.Background(), u.ID); got.LastLoginAt == nil {
			t.Fatal("last_login_at not set")
		}
		if n := len(e.audits("auth.login")); n != 1 {
			t.Fatalf("auth.login audits = %d", n)
		}
	})
}

func TestAuthNoEnumerationAndFailures(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		if _, err := e.admin.CreateUser(context.Background(), access.UserInput{Login: "nopw"}); err != nil {
			t.Fatal(err)
		}
		dis := e.user("dis")
		off := true
		if _, err := e.admin.UpdateUser(context.Background(), dis.ID, access.UserPatch{Disabled: &off}); err != nil {
			t.Fatal(err)
		}
		for _, c := range [][2]string{{"alice", "wrong password!!"}, {"ghost", authPassword}, {"nopw", authPassword}, {"dis", authPassword},
			{"alice", strings.Repeat("x", 5000)}} {
			e.clk.Advance(time.Hour) // keep the throttle out of this test
			_, ck, err := e.login(c[0], c[1])
			if !errors.Is(err, access.ErrUnauthenticated) || ck != nil {
				t.Errorf("%v: err=%v cookie=%v", c[:1], err, ck)
			}
		}
		fails := e.audits("auth.login_failed")
		if len(fails) != 5 {
			t.Fatalf("failed audits = %d", len(fails))
		}
		for _, f := range fails {
			if strings.Contains(string(f.Data), authPassword) || strings.Contains(string(f.Data), "wrong password") {
				t.Fatalf("password in audit: %s", f.Data)
			}
		}
	})
}

func TestAuthThrottle(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		for i := 0; i < 5; i++ {
			if _, _, err := e.login("alice", "wrong password!!"); !errors.Is(err, access.ErrUnauthenticated) {
				t.Fatal(err)
			}
		}
		var te *access.ThrottledError
		if _, _, err := e.login("alice", authPassword); !errors.As(err, &te) || te.RetryAfter <= 0 || te.RetryAfter > 15*time.Minute {
			t.Fatalf("expected throttle, got %v", err)
		}
		// the same login from another client is not locked by the pair limit
		if _, _, err := e.loginFrom("198.51.100.9", "alice", authPassword); err != nil {
			t.Fatalf("other client: %v", err)
		}
		// refused attempts are not audited
		if n := len(e.audits("auth.login_failed")); n != 5 {
			t.Fatalf("failed-login audits = %d, want 5", n)
		}
		e.clk.Advance(te.RetryAfter + time.Second)
		if _, _, err := e.login("alice", authPassword); err != nil {
			t.Fatalf("after wait: %v", err)
		}
	})
}

func TestAuthThrottleClientAcrossLogins(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		for i := 0; i < 30; i++ {
			if _, _, err := e.loginFrom("203.0.113.66", fmt.Sprintf("guess%d", i), "wrong password!!"); !errors.Is(err, access.ErrUnauthenticated) {
				t.Fatalf("attempt %d: %v", i, err)
			}
		}
		var te *access.ThrottledError
		if _, _, err := e.loginFrom("203.0.113.66", "alice", authPassword); !errors.As(err, &te) {
			t.Fatalf("spraying client not blocked: %v", err)
		}
		if _, _, err := e.loginFrom("203.0.113.67", "alice", authPassword); err != nil {
			t.Fatalf("other client: %v", err)
		}
		// unknown names are not stored in the audit trail
		for _, a := range e.audits("auth.login_failed") {
			if strings.Contains(string(a.Data), "guess") {
				t.Fatalf("unknown login stored: %s", a.Data)
			}
		}
	})
}

func TestAuthThrottleLoginAcrossClientsSparesKnownClients(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		if _, _, err := e.loginFrom("192.0.2.50", "alice", authPassword); err != nil { // her usual place
			t.Fatal(err)
		}
		// a botnet: many clients, a few guesses each, against her login
		for i := 0; i < 100; i++ {
			ip := fmt.Sprintf("10.%d.%d.1", i/200, i%200)
			if _, _, err := e.loginFrom(ip, "alice", "wrong password!!"); !errors.Is(err, access.ErrUnauthenticated) {
				t.Fatalf("attempt %d: %v", i, err)
			}
		}
		var te *access.ThrottledError
		if _, _, err := e.loginFrom("10.200.0.1", "alice", authPassword); !errors.As(err, &te) {
			t.Fatalf("unknown client not blocked during the attack: %v", err)
		}
		if _, _, err := e.loginFrom("192.0.2.50", "alice", authPassword); err != nil {
			t.Fatalf("the user at a previously successful client is locked out: %v", err)
		}
	})
}

func TestAuthThrottleHoldsUnderConcurrency(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		const n = 40
		var wg sync.WaitGroup
		errs := make([]error, n)
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, errs[i] = e.login("alice", "wrong password!!")
			}()
		}
		wg.Wait()
		var throttled, busy, failed int
		for _, err := range errs {
			var te *access.ThrottledError
			switch {
			case errors.As(err, &te):
				throttled++
			case errors.Is(err, access.ErrBusy):
				busy++
			case errors.Is(err, access.ErrUnauthenticated):
				failed++
			default:
				t.Fatalf("unexpected: %v", err)
			}
		}
		if failed > 5 {
			t.Fatalf("%d passwords were checked in parallel (throttled %d, busy %d), want at most 5", failed, throttled, busy)
		}
	})
}

// slowRand blocks the first read so a password check stays in flight.
type slowRand struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (s *slowRand) Read(p []byte) (int, error) {
	s.once.Do(func() {
		close(s.entered)
		<-s.release
	})
	return crand.Read(p)
}

func TestAuthBusyWhenVerificationsAreSaturated(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		sr := &slowRand{entered: make(chan struct{}), release: make(chan struct{})}
		e := newAuthEnv(t, b, func(o *access.AuthOptions) { o.MaxVerify = 1; o.Rand = sr })
		first := make(chan error, 1)
		go func() {
			_, _, err := e.loginFrom("203.0.113.1", "ghost", "wrong password!!") // unknown login: builds the dummy hash
			first <- err
		}()
		<-sr.entered
		_, _, err := e.loginFrom("203.0.113.2", "ghost2", "wrong password!!")
		if !errors.Is(err, access.ErrBusy) {
			t.Fatalf("want ErrBusy, got %v", err)
		}
		close(sr.release)
		if err := <-first; !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal(err)
		}
		// the refused attempt was not counted against anyone
		for i := 0; i < 4; i++ {
			_, _, _ = e.loginFrom("203.0.113.2", "ghost2", "wrong password!!")
		}
		var te *access.ThrottledError
		if _, _, err := e.loginFrom("203.0.113.2", "ghost2", "wrong password!!"); errors.As(err, &te) {
			t.Fatal("busy refusal was counted")
		}
	})
}

func TestAuthLoginInputCap(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		long := strings.Repeat("A", 1<<20)
		if _, _, err := e.login(long, authPassword); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal(err)
		}
	})
}

func TestAuthDevTokenOnlyFromLoopback(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, func(o *access.AuthOptions) { o.ConfigToken = "dev-token"; o.DevToken = true })
		try := func(remote string, hdr map[string]string) error {
			r := e.request("GET", "/")
			r.RemoteAddr = remote
			r.Header.Set("Authorization", "Bearer dev-token")
			for k, v := range hdr {
				r.Header.Set(k, v)
			}
			_, err := e.auth.Authenticate(r)
			return err
		}
		for _, ok := range []string{"127.0.0.1:5", "[::1]:5", "[::ffff:127.0.0.1]:5"} {
			if err := try(ok, nil); err != nil {
				t.Errorf("%s: %v", ok, err)
			}
		}
		for _, bad := range []string{"203.0.113.5:5", "10.0.0.1:5", "[2001:db8::1]:5"} {
			if err := try(bad, nil); !errors.Is(err, access.ErrUnauthenticated) {
				t.Errorf("%s: %v", bad, err)
			}
		}
		// a forwarded loopback address does not make a remote peer local, and a proxy on this machine forwarding
		// a remote client is not local either
		if err := try("203.0.113.5:5", map[string]string{"X-Forwarded-For": "127.0.0.1"}); !errors.Is(err, access.ErrUnauthenticated) {
			t.Errorf("spoofed forwarded address: %v", err)
		}
		if err := try("127.0.0.1:5", map[string]string{"X-Forwarded-For": "203.0.113.5"}); !errors.Is(err, access.ErrUnauthenticated) {
			t.Errorf("proxied request: %v", err)
		}
		// an explicitly configured token works from anywhere
		e2 := newAuthEnv(t, b, func(o *access.AuthOptions) { o.ConfigToken = "dev-token" })
		r := e2.request("GET", "/")
		r.Header.Set("Authorization", "Bearer dev-token")
		if _, err := e2.auth.Authenticate(r); err != nil {
			t.Errorf("configured token from a remote client: %v", err)
		}
	})
}

func TestAuthSessionExpiry(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		_, c, _ := e.login("alice", authPassword)
		// idle: slides while used
		for i := 0; i < 20; i++ {
			e.clk.Advance(10 * time.Hour)
			if _, err := e.authenticate(c, ""); i < 16 && err != nil {
				t.Fatalf("step %d: %v", i, err)
			} else if i >= 16 && !errors.Is(err, access.ErrUnauthenticated) {
				t.Fatalf("step %d past the absolute max: %v", i, err)
			} else if i >= 16 {
				break
			}
		}
		// the row is gone after the failed check
		_ = e.db.View(context.Background(), func(tx store.Tx) error {
			if _, err := tx.Auth().SessionByHash(context.Background(), access.HashSecret(c.Value)); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("expired session kept: %v", err)
			}
			return nil
		})
		// idle timeout without use
		_, c2, _ := e.login("alice", authPassword)
		e.clk.Advance(12*time.Hour + time.Second)
		if _, err := e.authenticate(c2, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("idle: %v", err)
		}
	})
}

func TestAuthTouchAtMostOncePerMinute(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		_, c, _ := e.login("alice", authPassword)
		seen := func() time.Time {
			var s store.Session
			_ = e.db.View(context.Background(), func(tx store.Tx) (err error) {
				s, err = tx.Auth().SessionByHash(context.Background(), access.HashSecret(c.Value))
				return
			})
			return s.LastSeenAt
		}
		start := seen()
		e.clk.Advance(30 * time.Second)
		_, _ = e.authenticate(c, "")
		if seen() != start {
			t.Fatal("touched within a minute")
		}
		e.clk.Advance(31 * time.Second)
		_, _ = e.authenticate(c, "")
		if seen() != e.clk.Now() {
			t.Fatal("not touched after a minute")
		}
	})
}

func TestAuthDisabledPasswordLogoutAndFixation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		u := e.user("alice")
		_, c1, _ := e.login("alice", authPassword)
		_, c2, _ := e.login("alice", authPassword)
		if c1.Value == c2.Value {
			t.Fatal("session id reused across logins")
		}
		// a client-supplied cookie is never adopted
		w := httptest.NewRecorder()
		r := e.request("POST", "/")
		r.AddCookie(&http.Cookie{Name: access.SessionCookie, Value: strings.Repeat("A", 43)})
		res, err := e.auth.Login(w, r, "alice", authPassword)
		if err != nil || w.Result().Cookies()[0].Value == strings.Repeat("A", 43) || res == nil {
			t.Fatalf("fixation: %v", err)
		}
		// logout deletes only that session and audits
		w = httptest.NewRecorder()
		lr := e.request("POST", "/")
		lr.AddCookie(c1)
		if err := e.auth.Logout(w, lr); err != nil {
			t.Fatal(err)
		}
		if got := w.Result().Cookies(); len(got) != 1 || got[0].MaxAge >= 0 || got[0].Value != "" {
			t.Fatalf("logout cookie %+v", got)
		}
		if _, err := e.authenticate(c1, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("logged-out session: %v", err)
		}
		if _, err := e.authenticate(c2, ""); err != nil {
			t.Fatalf("other session: %v", err)
		}
		if len(e.audits("auth.logout")) != 1 {
			t.Fatal("logout not audited")
		}
		// admin password reset kills sessions
		e.user("boss", "administrator")
		actx := e.as("boss")
		if err := e.admin.ResetPassword(actx, u.ID, "another long password"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.authenticate(c2, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("session survived password reset: %v", err)
		}
		if _, _, err := e.login("alice", authPassword); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal("old password still works")
		}
		_, c3, err := e.login("alice", "another long password")
		if err != nil {
			t.Fatal(err)
		}
		// disabling kills sessions and blocks login
		off := true
		if _, err := e.admin.UpdateUser(actx, u.ID, access.UserPatch{Disabled: &off}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.authenticate(c3, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("disabled user's session: %v", err)
		}
		if _, _, err := e.login("alice", "another long password"); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal("disabled user logged in")
		}
	})
}

func TestAuthRehashOnLogin(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		u := e.user("alice")
		weak := weakHash(t, authPassword)
		err := e.db.Tx(context.Background(), func(tx store.Tx) error {
			cur, err := tx.Auth().GetUser(context.Background(), u.ID)
			if err != nil {
				return err
			}
			cur.PasswordHash = weak
			return tx.Auth().UpdateUser(context.Background(), cur)
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := e.login("alice", authPassword); err != nil {
			t.Fatal(err)
		}
		var got store.User
		_ = e.db.View(context.Background(), func(tx store.Tx) (err error) {
			got, err = tx.Auth().GetUser(context.Background(), u.ID)
			return
		})
		if got.PasswordHash == weak || !strings.HasPrefix(got.PasswordHash, "pbkdf2-sha256$1000$") {
			t.Fatalf("not rehashed: %s", got.PasswordHash)
		}
	})
}

func TestAuthCSRFAndHeaderWins(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice", "viewer")
		res, c, _ := e.login("alice", authPassword)
		an, _ := e.authenticate(c, "")
		post := e.request("POST", "/")
		if err := e.auth.CheckCSRF(post, an); !errors.Is(err, access.ErrCSRF) {
			t.Fatalf("no token: %v", err)
		}
		post.Header.Set("X-CSRF-Token", res.CSRF)
		if err := e.auth.CheckCSRF(post, an); err != nil {
			t.Fatal(err)
		}
		// a bad bearer header with a good cookie does not fall back to the cookie
		if _, err := e.authenticate(c, "nope"); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("header must win: %v", err)
		}
		r := e.request("GET", "/")
		r.AddCookie(c)
		r.Header.Set("Authorization", "Basic abc")
		if _, err := e.auth.Authenticate(r); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("non-bearer header: %v", err)
		}
		// no credentials at all
		if _, err := e.authenticate(nil, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal(err)
		}
		if _, err := e.authenticate(&http.Cookie{Name: access.SessionCookie, Value: "garbage"}, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal(err)
		}
	})
}

func TestAuthTokens(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		// config token
		an, err := e.authenticate(nil, "config-secret")
		if err != nil || !an.Principal.Super || an.Principal.Actor().String() != "token:config" || an.Session != nil {
			t.Fatalf("config token: %v %+v", err, an)
		}
		if _, err := e.authenticate(nil, "config-secre"); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal("near-miss config token accepted")
		}
		// api token
		exp := e.clk.Now().Add(time.Hour)
		tok, secret, err := e.admin.CreateToken(ctx, access.TokenInput{Name: "ci", Roles: []ids.UUID{e.role("viewer").ID}, ExpiresAt: &exp})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(secret, "hyt_") || tok.Prefix != secret[:8] {
			t.Fatalf("secret %q prefix %q", secret, tok.Prefix)
		}
		an, err = e.authenticate(nil, secret)
		if err != nil || an.Principal.Kind != access.KindToken || an.Principal.ID != tok.ID || an.Principal.Super {
			t.Fatalf("token auth: %v", err)
		}
		if g, err := e.chk.Grants(ctx, an.Principal); err != nil || len(g) != 1 || g[0].Policy != access.PolicyContentRead {
			t.Fatalf("token grants: %v %v", g, err)
		}
		// last_used_at at most once a minute
		used := func() *time.Time { x, _ := e.admin.GetToken(ctx, tok.ID); return x.LastUsedAt }
		first := *used()
		e.clk.Advance(20 * time.Second)
		_, _ = e.authenticate(nil, secret)
		if !used().Equal(first) {
			t.Fatal("touched within a minute")
		}
		e.clk.Advance(41 * time.Second)
		_, _ = e.authenticate(nil, secret)
		if !used().After(first) {
			t.Fatal("not touched after a minute")
		}
		// expiry
		e.clk.Advance(time.Hour)
		if _, err := e.authenticate(nil, secret); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("expired token: %v", err)
		}
		// revoke
		_, secret2, _ := e.admin.CreateToken(ctx, access.TokenInput{Name: "x"})
		list, _, _ := e.admin.ListTokens(ctx, store.Page{})
		var id2 ids.UUID
		for _, x := range list {
			if x.Name == "x" {
				id2 = x.ID
			}
		}
		if _, err := e.authenticate(nil, secret2); err != nil {
			t.Fatal(err)
		}
		if err := e.admin.RevokeToken(ctx, id2); err != nil {
			t.Fatal(err)
		}
		if _, err := e.authenticate(nil, secret2); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("revoked token: %v", err)
		}
		// unknown hyt_ token
		if _, err := e.authenticate(nil, "hyt_"+strings.Repeat("A", 43)); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal("unknown token accepted")
		}
		// no secrets in listings or audit
		raw, _ := json.Marshal(list)
		if strings.Contains(string(raw), secret) || strings.Contains(strings.ToLower(string(raw)), "hash") {
			t.Fatalf("listing leaks: %s", raw)
		}
		for _, a := range e.audits("token.create") {
			if strings.Contains(string(a.Data), secret) || strings.Contains(string(a.Data), secret2) {
				t.Fatal("secret in audit")
			}
		}
	})
}

func TestAuthAgentDetail(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		_, secret, err := e.admin.CreateToken(context.Background(), access.TokenInput{Name: "bot", Kind: "agent"})
		if err != nil {
			t.Fatal(err)
		}
		r := e.request("GET", "/")
		r.Header.Set("Authorization", "bearer "+secret)
		r.Header.Set("X-Hydrogen-Agent-Model", "claude\nx")
		r.Header.Set("X-Hydrogen-Agent-Task", strings.Repeat("t", 500))
		an, err := e.auth.Authenticate(r)
		if err != nil {
			t.Fatal(err)
		}
		p := an.Principal
		if p.Kind != access.KindAgent || p.Actor().Kind != "agent" || p.Detail["model"] != "claudex" || len(p.Detail["task"]) != 128 {
			t.Fatalf("agent principal %+v", p)
		}
	})
}

func TestAuthNoConfigToken(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, func(o *access.AuthOptions) { o.ConfigToken = "" })
		if _, err := e.authenticate(nil, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal(err)
		}
		r := e.request("GET", "/")
		r.Header.Set("Authorization", "Bearer ")
		if _, err := e.auth.Authenticate(r); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal("empty bearer accepted with no config token")
		}
	})
}

func TestAuthInsecureCookiesAndProxy(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, func(o *access.AuthOptions) { o.InsecureCookies = true })
		e.user("alice")
		_, c, err := e.login("alice", authPassword)
		if err != nil || c.Secure {
			t.Fatalf("insecure cookie flag: %v %+v", err, c)
		}
	})
}

// weakHash is authPassword's hash with fewer iterations, in the documented encoding.
func weakHash(t *testing.T, pw string) string {
	t.Helper()
	salt := []byte("0123456789abcdef")
	key, err := pbkdf2.Key(sha256.New, pw, salt, 500, 32)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawStdEncoding
	return "pbkdf2-sha256$500$" + enc.EncodeToString(salt) + "$" + enc.EncodeToString(key)
}
