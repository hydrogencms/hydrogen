package site

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// FormsConfig is modules.site.forms: the spam checks of Request.Submit and Request.CheckForm.
type FormsConfig struct {
	// Honeypot names the hidden input {{formGuard}} renders; a request that fills it is dropped silently.
	Honeypot string `yaml:"honeypot"`
	// MinFill is how long after the form was rendered a submission is accepted (0 turns the check off).
	MinFill time.Duration `yaml:"min_fill"`
	// MaxAge is how long a rendered form stays valid.
	MaxAge time.Duration `yaml:"max_age"`
}

var (
	// ErrSpam is returned by CheckForm for a request that filled the honeypot. Answer as if it succeeded.
	ErrSpam = errors.New("site: form honeypot filled")
	// ErrTooFast: the form was submitted sooner than modules.site.forms.min_fill after it was rendered.
	ErrTooFast = errors.New("site: form submitted too fast")
	// ErrFormInvalid: the form has no valid {{formGuard}} token, or it is older than modules.site.forms.max_age.
	// Render the form again.
	ErrFormInvalid = errors.New("site: form token missing, invalid or expired")
	// ErrNotAllowed: the type is not listed in modules.site.submissions.
	ErrNotAllowed = errors.New("site: type is not open for submissions")
)

const guardField = "_hg_form"

var honeypotRE = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)

func (c *FormsConfig) validate() error {
	switch {
	case !honeypotRE.MatchString(c.Honeypot) || c.Honeypot == guardField:
		return fmt.Errorf("site: modules.site.forms.honeypot %q must be a lower-case field name", c.Honeypot)
	case c.MinFill < 0 || c.MaxAge <= c.MinFill:
		return errors.New("site: modules.site.forms: min_fill must not be negative and max_age must exceed it")
	}
	return nil
}

// formGuard signs the time a form was rendered, so a submission can be checked for being neither instant nor
// stale without server state.
type formGuard struct {
	key   []byte
	cfg   FormsConfig
	clock clock.Clock
}

func newFormGuard(secret string, cfg FormsConfig, c clock.Clock) *formGuard {
	sum := sha256.Sum256([]byte("hydrogen site form\x00" + secret))
	return &formGuard{key: sum[:], cfg: cfg, clock: c}
}

func (g *formGuard) sign(channel, ts string) string {
	m := hmac.New(sha256.New, g.key)
	m.Write([]byte(channel + "\x00" + ts))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

func (g *formGuard) token(channel string) string {
	ts := strconv.FormatInt(g.clock.Now().Unix(), 10)
	return ts + "." + g.sign(channel, ts)
}

func (g *formGuard) verify(channel, tok string) error {
	ts, sig, ok := strings.Cut(tok, ".")
	if !ok || subtle.ConstantTimeCompare([]byte(sig), []byte(g.sign(channel, ts))) != 1 {
		return ErrFormInvalid
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrFormInvalid
	}
	age := g.clock.Now().Sub(time.Unix(sec, 0))
	switch {
	case age > g.cfg.MaxAge:
		return ErrFormInvalid
	case age < g.cfg.MinFill:
		return ErrTooFast
	}
	return nil
}

// html is the inputs {{formGuard}} emits: the signed token and the honeypot, hidden from people.
func (g *formGuard) html(channel string) template.HTML {
	var b bytes.Buffer
	fmt.Fprintf(&b, `<input type="hidden" name="%s" value="%s">`, guardField, template.HTMLEscapeString(g.token(channel)))
	fmt.Fprintf(&b, `<div hidden aria-hidden="true"><label>Leave this empty <input type="text" name="%s" tabindex="-1" autocomplete="off" value=""></label></div>`,
		template.HTMLEscapeString(g.cfg.Honeypot))
	return template.HTML(b.String())
}

// formGuardFn renders the form protection fields; place it inside every <form> that posts to a route using
// CheckForm or Submit. A page that contains it must not be cached by shared caches.
func (p *page) formGuardFn() template.HTML {
	if p.guard == nil {
		return `<input type="hidden" name="` + guardField + `" value="check">`
	}
	return p.guard.html(p.ch.Name)
}

// CheckForm reads the form (R.ParseForm) and applies the spam checks: ErrSpam when the honeypot is filled,
// ErrFormInvalid when the {{formGuard}} token is missing, forged or older than modules.site.forms.max_age and
// ErrTooFast when it is younger than min_fill. Submit calls it; call it from routes that handle a form
// otherwise (sending mail, say). Cross-site posts are refused before the route runs (see AllowCrossSite).
func (r *Request) CheckForm() error {
	if err := r.R.ParseForm(); err != nil {
		return err
	}
	g := r.srv.guard
	if r.R.PostForm.Get(g.cfg.Honeypot) != "" {
		return ErrSpam
	}
	return g.verify(r.Channel, r.R.PostForm.Get(guardField))
}

// Submit stores a form as a new draft item of a type listed in modules.site.submissions and returns its id. The
// item is never published; editors review it in the admin, and the audit log and created_by name the actor
// "system:site:<route path>" with the client address. fields are the item's fields, chosen by the route (not
// the raw form), and are validated like any write, with required fields enforced. A validation problem is a
// *content.ValidationError (FieldErrors reads it). A request that filled the honeypot is dropped: the id is ""
// and the error nil, so the route can answer as for a success. Only non-GET routes may submit.
func (r *Request) Submit(typ string, fields map[string]any) (string, error) {
	if r.R.Method == http.MethodGet || r.R.Method == http.MethodHead {
		return "", errors.New("site: Submit needs a route that handles POST")
	}
	s := r.srv
	if !slices.Contains(s.cfg.Submissions, typ) {
		return "", fmt.Errorf("%w: %s", ErrNotAllowed, typ)
	}
	switch err := r.CheckForm(); {
	case errors.Is(err, ErrSpam):
		return "", nil
	case err != nil:
		return "", err
	}
	t, ok := s.svc.Schema.Type(typ)
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrNotAllowed, typ)
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("site: submission fields: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("site: submission fields: %w", err)
	}
	var missing []schema.FieldError
	for _, fe := range s.svc.Schema.Validate(t, doc, schema.ModePublish) {
		if fe.Code == "required" {
			missing = append(missing, fe)
		}
	}
	if len(missing) > 0 {
		return "", &content.ValidationError{Locale: r.Locale, Errors: missing}
	}
	name := r.route.path
	if name == "" {
		name = r.route.pattern
	}
	actor := audit.Actor{Kind: "system", ID: "site:" + name,
		Detail: map[string]string{"channel": r.Channel, "ip": s.svc.Auth.ClientIP(r.R)}}
	v, err := s.svc.Content.Create(audit.WithActor(r.R.Context(), actor), content.CreateInput{Type: typ, Locale: r.Locale, Fields: doc})
	if err != nil {
		return "", err
	}
	return v.ID.String(), nil
}

// FieldErrors maps the fields of a validation problem returned by Submit to their messages (the first per
// field), for re-rendering the form; nil when err is not one.
func FieldErrors(err error) map[string]string {
	var ve *content.ValidationError
	if !errors.As(err, &ve) {
		return nil
	}
	m := map[string]string{}
	for _, fe := range ve.Errors {
		if _, ok := m[fe.Path]; !ok {
			m[fe.Path] = fe.Message
		}
	}
	return m
}
