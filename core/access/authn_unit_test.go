package access

import (
	"bytes"
	"crypto/rand"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/store"
)

func TestPasswordHashVerify(t *testing.T) {
	pw := "correct horse battery"
	enc, err := HashPassword(rand.Reader, pw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enc, "pbkdf2-sha256$600000$") || strings.Count(enc, "$") != 3 {
		t.Fatalf("encoding %q", enc)
	}
	if ok, re := VerifyPassword(enc, pw); !ok || re {
		t.Fatalf("verify = %v, %v", ok, re)
	}
	if ok, _ := VerifyPassword(enc, pw+"x"); ok {
		t.Fatal("wrong password verified")
	}
	enc2, _ := HashPassword(rand.Reader, pw)
	if enc == enc2 {
		t.Fatal("salt reused")
	}
}

func TestPasswordRehashAndMalformed(t *testing.T) {
	old, err := hashPassword(rand.Reader, "correct horse battery", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if ok, re := VerifyPassword(old, "correct horse battery"); !ok || !re {
		t.Fatalf("old params = %v, %v", ok, re)
	}
	for _, bad := range []string{"", "x", "pbkdf2-sha256$1$a$b", "pbkdf2-sha256$0$AAAA$AAAA", "pbkdf2-sha256$99999999999$AAAA$AAAA",
		"md5$1000$AAAA$AAAA", "pbkdf2-sha256$1000$$AAAA", "pbkdf2-sha256$1000$AAAA$", "pbkdf2-sha256$1000$AAAA$AAAA$x"} {
		if ok, _ := VerifyPassword(bad, "correct horse battery"); ok {
			t.Errorf("%q verified", bad)
		}
	}
}

func TestPasswordLimits(t *testing.T) {
	for _, pw := range []string{"", strings.Repeat("a", MinPasswordLen-1), strings.Repeat("a", MaxPasswordLen+1)} {
		if _, err := HashPassword(rand.Reader, pw); !errors.Is(err, ErrPasswordLength) {
			t.Errorf("len %d: %v", len(pw), err)
		}
	}
	for _, pw := range []string{strings.Repeat("a", MinPasswordLen), strings.Repeat("é", MaxPasswordLen/2)} {
		if err := ValidatePassword(pw); err != nil {
			t.Errorf("len %d: %v", len(pw), err)
		}
	}
}

func TestSecrets(t *testing.T) {
	s, h, prefix, err := NewSecret(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(s, "hyt_") || len(s) != 4+43 || prefix != s[:8] || !bytes.Equal(h, HashSecret(s)) || len(h) != 32 {
		t.Fatalf("secret %q prefix %q", s, prefix)
	}
	s2, _, _, _ := NewSecret(rand.Reader)
	if s == s2 {
		t.Fatal("secrets repeat")
	}
	if _, _, _, err := NewSecret(bytes.NewReader([]byte("short"))); err == nil {
		t.Fatal("short entropy accepted")
	}
}

func TestThrottle(t *testing.T) {
	th := newThrottle(100, 5)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if d := th.take("k", now); d != 0 {
			t.Fatalf("attempt %d refused: %v", i+1, d)
		}
	}
	// the 5th attempt ran but blocked the key
	if d := th.take("k", now); d != throttleBase {
		t.Fatalf("6th: %v", d)
	}
	if d := th.take("k", now.Add(throttleBase)); d != 0 {
		t.Fatalf("after wait: %v", d)
	}
	now = now.Add(throttleBase)
	if d := th.take("k", now); d != 2*throttleBase {
		t.Fatalf("doubled block: %v", d)
	}
	for i := 0; i < 40; i++ {
		now = now.Add(throttleMax)
		th.take("k", now)
	}
	if d := th.take("k", now); d > throttleMax {
		t.Fatalf("uncapped: %v", d)
	}
	th.clear("k")
	if d := th.take("k", now); d != 0 {
		t.Fatalf("cleared: %v", d)
	}
}

func TestThrottleGiveLiftsOwnBlock(t *testing.T) {
	th := newThrottle(100, 5)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		th.take("k", now)
	}
	th.give("k") // the 5th was a success
	if d := th.take("k", now); d != 0 {
		t.Fatalf("block survived a success: %v", d)
	}
}

func TestThrottleWindowResetsAndLRU(t *testing.T) {
	th := newThrottle(3, 5)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		th.take("k", now)
	}
	now = now.Add(throttleWindow + time.Second)
	for i := 0; i < 4; i++ { // old attempts aged out: four more do not block
		if d := th.take("k", now); d != 0 {
			t.Fatalf("stale attempts counted: %v", d)
		}
	}
	for _, k := range []string{"a", "b", "c", "d"} {
		th.take(k, now)
	}
	if len(th.entries) != 3 || th.order.Len() != 3 {
		t.Fatalf("lru size %d/%d", len(th.entries), th.order.Len())
	}
	if _, ok := th.entries["k"]; ok {
		t.Fatal("oldest key kept")
	}
}

func TestLoginGuard(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fails := func(g *loginGuard, login, ip string, n int) {
		for i := 0; i < n; i++ {
			if _, d := g.attempt(login, ip, now); d > 0 {
				t.Fatalf("%s@%s attempt %d refused", login, ip, i+1)
			}
		}
	}
	t.Run("pair", func(t *testing.T) {
		g := newLoginGuard(100)
		fails(g, "victim", "203.0.113.1", 5)
		if _, d := g.attempt("victim", "203.0.113.1", now); d == 0 {
			t.Fatal("pair not blocked")
		}
		if _, d := g.attempt("victim", "198.51.100.1", now); d != 0 {
			t.Fatal("other client blocked by the pair limit")
		}
		if _, d := g.attempt("other", "203.0.113.1", now); d != 0 {
			t.Fatal("other login at the client blocked by the pair limit")
		}
	})
	t.Run("client across logins", func(t *testing.T) {
		g := newLoginGuard(100)
		for i := 0; i < limitIP; i++ {
			fails(g, "u"+string(rune('a'+i%26))+string(rune('a'+i/26)), "203.0.113.1", 1)
		}
		if _, d := g.attempt("fresh", "203.0.113.1", now); d == 0 {
			t.Fatal("client not blocked after spraying logins")
		}
		if _, d := g.attempt("fresh", "203.0.113.2", now); d != 0 {
			t.Fatal("another client blocked")
		}
	})
	t.Run("login across clients spares known clients", func(t *testing.T) {
		g := newLoginGuard(1000)
		res, d := g.attempt("victim", "192.0.2.50", now)
		if d != 0 {
			t.Fatal(d)
		}
		g.release(res) // the victim logged in from here before
		for i := 0; i < limitLogin; i++ {
			fails(g, "victim", "10.1."+itoa(i/200)+"."+itoa(i%200+1), 1)
		}
		if _, d := g.attempt("victim", "10.9.9.9", now); d == 0 {
			t.Fatal("unknown client not blocked once the login is under attack")
		}
		res, d = g.attempt("victim", "192.0.2.50", now)
		if d != 0 {
			t.Fatal("the victim's usual client was locked out")
		}
		g.release(res)
	})
	t.Run("junk logins do not evict clients", func(t *testing.T) {
		g := newLoginGuard(50)
		fails(g, "x", "203.0.113.1", 5)
		for i := 0; i < 500; i++ {
			g.attempt("junk"+itoa(i), "192.0.2.77", now)
		}
		if g.ip.entries["203.0.113.1"] == nil {
			t.Fatal("client entry evicted by junk logins")
		}
	})
	t.Run("ipv6 by prefix", func(t *testing.T) {
		if clientKey("2001:db8:1:2::1") != clientKey("2001:db8:1:2:ffff::9") || clientKey("2001:db8:1:2::1") == clientKey("2001:db8:1:3::1") {
			t.Fatal("prefix grouping")
		}
		if clientKey("192.0.2.1") != "192.0.2.1" {
			t.Fatal("ipv4 changed")
		}
	})
	t.Run("cancel gives attempts back", func(t *testing.T) {
		g := newLoginGuard(100)
		for i := 0; i < 20; i++ {
			res, d := g.attempt("u", "203.0.113.1", now)
			if d != 0 {
				t.Fatalf("attempt %d refused", i)
			}
			g.cancel(res)
		}
	})
}

func itoa(n int) string { return strconv.Itoa(n) }

func newTestAuthn(t *testing.T, proxies ...string) *Authenticator {
	t.Helper()
	var ps []netip.Prefix
	for _, p := range proxies {
		ps = append(ps, netip.MustParsePrefix(p))
	}
	a := &Authenticator{proxies: ps}
	return a
}

func reqFrom(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name    string
		proxies []string
		remote  string
		xff     []string
		want    string
	}{
		{"no proxies ignores header", nil, "203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"untrusted peer ignores header", []string{"10.0.0.0/8"}, "203.0.113.9:5000", []string{"1.2.3.4"}, "203.0.113.9"},
		{"trusted peer", []string{"10.0.0.0/8"}, "10.0.0.1:5000", []string{"198.51.100.7"}, "198.51.100.7"},
		{"spoofed left entry", []string{"10.0.0.0/8"}, "10.0.0.1:5000", []string{"1.2.3.4, 198.51.100.7"}, "198.51.100.7"},
		{"chain of proxies", []string{"10.0.0.0/8"}, "10.0.0.1:5000", []string{"1.2.3.4, 198.51.100.7, 10.0.0.2"}, "198.51.100.7"},
		{"two header lines", []string{"10.0.0.0/8"}, "10.0.0.1:5000", []string{"1.2.3.4", "198.51.100.7"}, "198.51.100.7"},
		{"all trusted", []string{"10.0.0.0/8"}, "10.0.0.1:5000", []string{"10.0.0.3, 10.0.0.2"}, "10.0.0.3"},
		{"garbage entry stops", []string{"10.0.0.0/8"}, "10.0.0.1:5000", []string{"1.2.3.4, nonsense"}, "10.0.0.1"},
		{"no header", []string{"10.0.0.0/8"}, "10.0.0.1:5000", nil, "10.0.0.1"},
		{"ipv4-mapped", []string{"10.0.0.0/8"}, "[::ffff:10.0.0.1]:5000", []string{"::ffff:198.51.100.7"}, "198.51.100.7"},
		{"ipv6 client", []string{"fd00::/8"}, "[fd00::1]:5000", []string{"2001:db8::5"}, "2001:db8::5"},
	}
	for _, c := range cases {
		a := newTestAuthn(t, c.proxies...)
		if got := a.ClientIP(reqFrom(c.remote, c.xff...)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCheckCSRF(t *testing.T) {
	a := newTestAuthn(t)
	sess := &Authn{Principal: &Principal{Kind: KindUser}, Session: &store.Session{CSRF: "tok"}}
	bearer := &Authn{Principal: &Principal{Kind: KindToken}}
	mk := func(method, hdr string) *http.Request {
		r := httptest.NewRequest(method, "/", nil)
		if hdr != "" {
			r.Header.Set("X-CSRF-Token", hdr)
		}
		return r
	}
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		if err := a.CheckCSRF(mk(m, ""), sess); err != nil {
			t.Errorf("%s: %v", m, err)
		}
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if err := a.CheckCSRF(mk(m, ""), sess); !errors.Is(err, ErrCSRF) {
			t.Errorf("%s without token: %v", m, err)
		}
		if err := a.CheckCSRF(mk(m, "wrong"), sess); !errors.Is(err, ErrCSRF) {
			t.Errorf("%s wrong token: %v", m, err)
		}
		if err := a.CheckCSRF(mk(m, "tok"), sess); err != nil {
			t.Errorf("%s right token: %v", m, err)
		}
		if err := a.CheckCSRF(mk(m, ""), bearer); err != nil {
			t.Errorf("%s bearer: %v", m, err)
		}
	}
}

func TestCleanDetail(t *testing.T) {
	if got := cleanDetail("gpt\r\nX: y\x00"); got != "gptX: y" {
		t.Errorf("control chars kept: %q", got)
	}
	if got := cleanDetail(strings.Repeat("é", 300)); len([]rune(got)) != agentDetailMax {
		t.Errorf("len %d", len([]rune(got)))
	}
}

func TestPasswordIterationsConfigurable(t *testing.T) {
	enc, err := HashPasswordWith(rand.Reader, "correct horse battery", 1000)
	if err != nil || !strings.HasPrefix(enc, "pbkdf2-sha256$1000$") {
		t.Fatalf("%q %v", enc, err)
	}
	if ok, re := VerifyPasswordWith(enc, "correct horse battery", 1000); !ok || re {
		t.Fatalf("at the configured count: %v %v", ok, re)
	}
	// raising the configured count makes the old hash stale; the default count too
	if ok, re := VerifyPasswordWith(enc, "correct horse battery", 2000); !ok || !re {
		t.Fatalf("after raising the count: %v %v", ok, re)
	}
	if ok, re := VerifyPassword(enc, "correct horse battery"); !ok || !re {
		t.Fatalf("against the default: %v %v", ok, re)
	}
	if d, _ := HashPasswordWith(rand.Reader, "correct horse battery", 0); !strings.HasPrefix(d, "pbkdf2-sha256$600000$") {
		t.Fatalf("zero must mean the default: %q", d)
	}
}
