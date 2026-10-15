package access

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/store"
)

// SessionCookie is the session cookie's name.
const SessionCookie = "hydrogen_session"

const (
	touchInterval   = time.Minute
	agentDetailMax  = 128
	maxLoginLen     = 64
	maxLoginInput   = 256
	maxUserAgentLen = 200
)

// ErrCSRF: a session-authenticated unsafe request lacks the right X-CSRF-Token.
var ErrCSRF = errors.New("access: csrf token missing or wrong")

type AuthOptions struct {
	DB    store.DB
	Clock clock.Clock // default clock.System
	Rand  io.Reader   // default crypto/rand
	Log   *slog.Logger
	// ConfigToken is the super principal's bearer token ("" = none).
	ConfigToken string
	// DevToken marks ConfigToken as the development default: it then only authenticates requests whose direct
	// peer is a loopback address (never judged by forwarded headers), so a deployment that forgot to set
	// manage.token is not open to the network.
	DevToken    bool
	SessionIdle time.Duration // default 12h, sliding
	SessionMax  time.Duration // default 7 days, absolute
	// InsecureCookies drops the Secure cookie flag (dev only; the caller enforces that).
	InsecureCookies bool
	// TrustedProxies are the peers whose X-Forwarded-For is believed.
	TrustedProxies []netip.Prefix
	// MaxVerify caps concurrent password verifications (default GOMAXPROCS); beyond it logins get ErrBusy.
	MaxVerify int
	// PasswordIterations is the PBKDF2 cost of new hashes (0 = 600 000); tests lower it.
	PasswordIterations int
}

// Authn is an authenticated request. Session is nil for bearer tokens.
type Authn struct {
	Principal *Principal
	Session   *store.Session
}

// LoginResult is what a successful login tells the client (the session id travels only in the cookie).
type LoginResult struct {
	User      store.User
	CSRF      string
	ExpiresAt time.Time
}

// Authenticator turns requests into principals and runs login and logout.
type Authenticator struct {
	db      store.DB
	clock   clock.Clock
	rand    io.Reader
	log     *slog.Logger
	cfgHash [sha256.Size]byte
	hasCfg  bool
	devCfg  bool
	idle    time.Duration
	max     time.Duration
	secure  bool
	proxies []netip.Prefix
	guard   *loginGuard
	verify  verifyGate
	iter    int

	dummyOnce sync.Once
	dummy     string
}

// NewAuthenticator needs a DB for users, tokens and sessions; without one only the config token authenticates
// (tests and tools without a database).
func NewAuthenticator(o AuthOptions) (*Authenticator, error) {
	a := &Authenticator{db: o.DB, clock: o.Clock, rand: o.Rand, log: o.Log, idle: o.SessionIdle, max: o.SessionMax,
		secure: !o.InsecureCookies, proxies: o.TrustedProxies, guard: newLoginGuard(throttleKeys), verify: newVerifyGate(o.MaxVerify), iter: iterationsOr(o.PasswordIterations)}
	if a.clock == nil {
		a.clock = clock.System{}
	}
	if a.rand == nil {
		a.rand = rand.Reader
	}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	if a.idle <= 0 {
		a.idle = 12 * time.Hour
	}
	if a.max <= 0 {
		a.max = 7 * 24 * time.Hour
	}
	a.idle = min(a.idle, a.max)
	if o.ConfigToken != "" {
		a.cfgHash, a.hasCfg, a.devCfg = sha256.Sum256([]byte(o.ConfigToken)), true, o.DevToken
	}
	return a, nil
}

// Authenticate returns the principal of a request: a bearer credential if an Authorization header is present
// (the cookie is then ignored), else the session cookie. ErrUnauthenticated for anything unknown, expired,
// revoked or disabled; other errors are storage failures.
func (a *Authenticator) Authenticate(r *http.Request) (*Authn, error) {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, tok, ok := strings.Cut(h, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") {
			return nil, ErrUnauthenticated
		}
		return a.bearer(r, strings.TrimSpace(tok))
	}
	c, err := r.Cookie(SessionCookie)
	if a.db == nil || err != nil || !validCookieValue(c.Value) {
		return nil, ErrUnauthenticated
	}
	return a.session(r.Context(), c.Value)
}

func (a *Authenticator) bearer(r *http.Request, tok string) (*Authn, error) {
	if tok == "" {
		return nil, ErrUnauthenticated
	}
	sum := sha256.Sum256([]byte(tok))
	if a.hasCfg && subtle.ConstantTimeCompare(sum[:], a.cfgHash[:]) == 1 && (!a.devCfg || directLoopback(r)) {
		return &Authn{Principal: &Principal{Kind: KindToken, Name: "config", Super: true}}, nil
	}
	if a.db == nil || !strings.HasPrefix(tok, TokenPrefix) {
		return nil, ErrUnauthenticated
	}
	ctx, now := r.Context(), a.clock.Now()
	var t store.Token
	err := a.db.View(ctx, func(tx store.Tx) (err error) {
		t, err = tx.Auth().TokenByHash(ctx, sum[:])
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("access: token lookup: %w", err)
	}
	if t.RevokedAt != nil || (t.ExpiresAt != nil && !now.Before(*t.ExpiresAt)) {
		return nil, ErrUnauthenticated
	}
	if t.LastUsedAt == nil || now.Sub(*t.LastUsedAt) >= touchInterval {
		err := a.db.Tx(ctx, func(tx store.Tx) error { return tx.Auth().TouchToken(ctx, t.ID, now) })
		if err != nil {
			a.log.Warn("access: touch token", "token", t.ID.String(), "err", err)
		}
	}
	p := &Principal{Kind: KindToken, ID: t.ID, Name: t.Name}
	if t.Kind == "agent" {
		p.Kind = KindAgent
		p.Detail = map[string]string{}
		if v := cleanDetail(r.Header.Get("X-Hydrogen-Agent-Model")); v != "" {
			p.Detail["model"] = v
		}
		if v := cleanDetail(r.Header.Get("X-Hydrogen-Agent-Task")); v != "" {
			p.Detail["task"] = v
		}
	}
	return &Authn{Principal: p}, nil
}

// directLoopback: the connection itself comes from this machine and no proxy header says otherwise.
func directLoopback(r *http.Request) bool {
	for _, h := range []string{"X-Forwarded-For", "Forwarded", "X-Real-IP"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.Unmap().IsLoopback()
}

// cleanDetail drops control characters (log and audit injection) and caps the length.
func cleanDetail(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return truncateRunes(strings.TrimSpace(s), agentDetailMax)
}

func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func validCookieValue(v string) bool {
	if len(v) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil && len(b) == 32
}

func (a *Authenticator) session(ctx context.Context, cookie string) (*Authn, error) {
	hash := HashSecret(cookie)
	now := a.clock.Now()
	var su store.SessionUser
	err := a.db.View(ctx, func(tx store.Tx) (err error) {
		su, err = tx.Auth().SessionUserByHash(ctx, hash)
		return err
	})
	s, u, groups := su.Session, su.User, su.Groups
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthenticated
	}
	if err != nil {
		return nil, fmt.Errorf("access: session lookup: %w", err)
	}
	if !now.Before(s.ExpiresAt) || !now.Before(s.CreatedAt.Add(a.max)) {
		if err := a.db.Tx(ctx, func(tx store.Tx) error { return tx.Auth().DeleteSession(ctx, hash) }); err != nil {
			a.log.Warn("access: delete expired session", "err", err)
		}
		return nil, ErrUnauthenticated
	}
	if u.Disabled {
		return nil, ErrUnauthenticated
	}
	if now.Sub(s.LastSeenAt) >= touchInterval {
		exp := now.Add(a.idle)
		if abs := s.CreatedAt.Add(a.max); abs.Before(exp) {
			exp = abs
		}
		if err := a.db.Tx(ctx, func(tx store.Tx) error { return tx.Auth().TouchSession(ctx, hash, now, exp) }); err != nil {
			a.log.Warn("access: touch session", "err", err)
		} else {
			s.LastSeenAt, s.ExpiresAt = now, exp
		}
	}
	return &Authn{Principal: &Principal{Kind: KindUser, ID: u.ID, Name: u.Login, Groups: groups}, Session: &s}, nil
}

// CheckCSRF requires the session's token on unsafe methods of cookie-authenticated requests. Bearer requests
// carry no ambient credential and pass.
func (a *Authenticator) CheckCSRF(r *http.Request, an *Authn) error {
	if an == nil || an.Session == nil {
		return nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return nil
	}
	got := r.Header.Get("X-CSRF-Token")
	if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(an.Session.CSRF)) != 1 {
		return ErrCSRF
	}
	return nil
}

// ClientIP is the request's peer address. With TrustedProxies configured and the peer one of them, it is the
// right-most X-Forwarded-For entry that is not itself a trusted proxy, so a client cannot choose its address by
// sending the header. Without them the header is ignored.
func (a *Authenticator) ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	cur := peer.Unmap().WithZone("")
	if len(a.proxies) == 0 || !a.trusted(cur) {
		return cur.String()
	}
	parts := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			break
		}
		ip = ip.Unmap().WithZone("")
		if !a.trusted(ip) {
			return ip.String()
		}
		cur = ip
	}
	return cur.String()
}

func (a *Authenticator) trusted(ip netip.Addr) bool {
	for _, p := range a.proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func (a *Authenticator) dummyHash() string {
	a.dummyOnce.Do(func() {
		a.dummy, _ = hashPassword(a.rand, "dummy password for equal timing", a.iter)
	})
	return a.dummy
}

// checkCredentials looks the login up and verifies the password once, whatever the outcome. reason is empty on
// success.
func (a *Authenticator) checkCredentials(ctx context.Context, login, password string) (u store.User, reason string, rehash bool, err error) {
	found := false
	err = a.db.View(ctx, func(tx store.Tx) error {
		var err error
		u, err = tx.Auth().UserByLogin(ctx, login)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		found = err == nil
		return err
	})
	if err != nil {
		return u, "", false, fmt.Errorf("access: login lookup: %w", err)
	}
	hash := u.PasswordHash
	if !found || hash == "" {
		hash = a.dummyHash()
	}
	pw := password
	if len(pw) > MaxPasswordLen {
		pw = ""
	}
	ok, rehash := VerifyPasswordWith(hash, pw, a.iter)
	switch {
	case !found:
		reason = "unknown_user"
	case u.PasswordHash == "":
		reason = "no_password"
	case !ok || pw != password:
		reason = "bad_password"
	case u.Disabled:
		reason = "disabled"
	}
	return u, reason, rehash, nil
}

// Login verifies the credentials and starts a session: a fresh session id every time (no fixation), set as an
// HttpOnly cookie. Unknown login, wrong password, disabled user and missing password all return
// ErrUnauthenticated after one password verification. Too many failures return *ThrottledError.
func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request, login, password string) (*LoginResult, error) {
	if a.db == nil {
		return nil, ErrUnauthenticated
	}
	ctx, now := r.Context(), a.clock.Now()
	if len(login) > maxLoginInput {
		login = login[:maxLoginInput]
	}
	login = truncateRunes(strings.ToLower(strings.TrimSpace(login)), maxLoginLen)
	ip := a.ClientIP(r)
	res, wait := a.guard.attempt(login, ip, now)
	if wait > 0 {
		return nil, &ThrottledError{RetryAfter: wait}
	}
	if !a.verify.tryAcquire() {
		a.guard.cancel(res)
		return nil, ErrBusy
	}
	u, reason, rehash, err := a.checkCredentials(ctx, login, password)
	a.verify.release()
	if err != nil {
		a.guard.cancel(res)
		return nil, err
	}
	if reason != "" {
		a.auditLoginFailed(ctx, login, ip, reason, u)
		return nil, ErrUnauthenticated
	}

	var newHash string
	if rehash {
		if newHash, err = hashPassword(a.rand, password, a.iter); err != nil {
			return nil, err
		}
	}
	sid, err := a.randomToken()
	if err != nil {
		return nil, err
	}
	csrf, err := a.randomToken()
	if err != nil {
		return nil, err
	}
	sess := store.Session{IDHash: HashSecret(sid), UserID: u.ID, CSRF: csrf, CreatedAt: now, LastSeenAt: now,
		ExpiresAt: now.Add(a.idle), IP: ip, UserAgent: truncateRunes(r.UserAgent(), maxUserAgentLen)}
	err = a.db.Tx(ctx, func(tx store.Tx) error {
		cur, err := tx.Auth().GetUser(ctx, u.ID)
		if err != nil {
			return err
		}
		if cur.Disabled || cur.PasswordHash != u.PasswordHash {
			return ErrUnauthenticated // changed since the check
		}
		cur.LastLoginAt = &now
		if newHash != "" {
			cur.PasswordHash = newHash
		}
		cur.UpdatedAt = now
		if err := tx.Auth().UpdateUser(ctx, cur); err != nil {
			return err
		}
		if err := tx.Auth().CreateSession(ctx, sess); err != nil {
			return err
		}
		return a.auditTx(ctx, tx, userActor(u), "auth.login", u.ID.String(), map[string]string{"login": login, "ip": ip})
	})
	if err != nil {
		if errors.Is(err, ErrUnauthenticated) {
			return nil, err
		}
		return nil, fmt.Errorf("access: login: %w", err)
	}
	a.guard.release(res)
	http.SetCookie(w, a.cookie(sid, int(a.max/time.Second), now.Add(a.max)))
	u.PasswordHash = ""
	u.LastLoginAt = &now
	return &LoginResult{User: u, CSRF: csrf, ExpiresAt: sess.ExpiresAt}, nil
}

// Logout deletes the request's session and expires the cookie. Callers check CSRF first.
func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, a.cookie("", -1, time.Unix(0, 0)))
	c, err := r.Cookie(SessionCookie)
	if a.db == nil || err != nil || !validCookieValue(c.Value) {
		return nil
	}
	ctx, hash := r.Context(), HashSecret(c.Value)
	err = a.db.Tx(ctx, func(tx store.Tx) error {
		s, err := tx.Auth().SessionByHash(ctx, hash)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		u, err := tx.Auth().GetUser(ctx, s.UserID)
		if err != nil {
			return err
		}
		if err := tx.Auth().DeleteSession(ctx, hash); err != nil {
			return err
		}
		return a.auditTx(ctx, tx, userActor(u), "auth.logout", u.ID.String(), map[string]string{"login": u.Login, "ip": a.ClientIP(r)})
	})
	if err != nil {
		return fmt.Errorf("access: logout: %w", err)
	}
	return nil
}

func (a *Authenticator) cookie(value string, maxAge int, expires time.Time) *http.Cookie {
	return &http.Cookie{Name: SessionCookie, Value: value, Path: "/api", HttpOnly: true, Secure: a.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge, Expires: expires}
}

func (a *Authenticator) randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(a.rand, b); err != nil {
		return "", fmt.Errorf("access: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func userActor(u store.User) audit.Actor {
	return audit.Actor{Kind: string(KindUser), ID: u.ID.String()}
}

func (a *Authenticator) auditTx(ctx context.Context, tx store.Tx, actor audit.Actor, action, subject string, data any) error {
	_, err := tx.Audit().Write(ctx, store.AuditEvent{At: a.clock.Now(), ActorKind: actor.Kind, ActorID: actor.ID,
		Action: action, Subject: subject, Data: mustJSON(data)})
	return err
}

// auditLoginFailed records a failed login. An unknown login name is not stored (anyone can send anything);
// only attempts on real users carry the name.
func (a *Authenticator) auditLoginFailed(ctx context.Context, login, ip, reason string, u store.User) {
	subject := ""
	data := map[string]string{"ip": ip, "reason": reason}
	if !u.ID.IsZero() {
		subject = u.ID.String()
		data["login"] = login
	}
	err := a.db.Tx(ctx, func(tx store.Tx) error {
		return a.auditTx(ctx, tx, audit.System, "auth.login_failed", subject, data)
	})
	if err != nil {
		a.log.Warn("access: audit failed login", "err", err)
	}
}
