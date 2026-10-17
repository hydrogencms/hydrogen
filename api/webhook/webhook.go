// Package webhook delivers outbox events to HTTP endpoints, signed so receivers can verify them.
//
// Each webhook is one outbox consumer (webhook:<name>), so deliveries retry independently with the outbox's
// backoff. A delivery is a POST of
//
//	{"id":"<event id>","topic":"item.published","data":{…},"tags":["i:…","c:…"],"createdAt":"<RFC 3339>"}
//
// with the headers Hydrogen-Event (topic), Hydrogen-Delivery (event id: the same on every retry, use it to
// deduplicate) and Hydrogen-Signature (t=<unix>,v1=<hex HMAC-SHA256(secret, t + "." + body)>).
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/outbox"
)

const (
	// DefaultTimeout bounds one delivery attempt.
	DefaultTimeout = 10 * time.Second
	maxTimeout     = 2 * time.Minute
	maxDrain       = 64 << 10
	// DefaultTolerance is the timestamp tolerance Verify uses when given none.
	DefaultTolerance = 5 * time.Minute

	EventHeader     = "Hydrogen-Event"
	DeliveryHeader  = "Hydrogen-Delivery"
	SignatureHeader = "Hydrogen-Signature"
)

// Config is one entry of the app.yaml "webhooks" section.
type Config struct {
	URL     string        `yaml:"url"`
	Events  []string      `yaml:"events"`
	Secret  string        `yaml:"secret" config:"secret"`
	Timeout time.Duration `yaml:"timeout"`
}

var (
	nameRe  = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	topicRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z0-9_-]+)*(\.\*)?$`)
)

// Validate checks one webhook. dev allows plain http URLs.
func (c Config) Validate(name string, dev bool) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("webhooks.%s: name must be lowercase letters, digits, dashes and underscores", name)
	}
	u, err := url.Parse(c.URL)
	switch {
	case c.URL == "":
		return fmt.Errorf("webhooks.%s.url is required", name)
	case err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http"):
		return fmt.Errorf("webhooks.%s.url: %q is not an http(s) URL", name, c.URL)
	case u.Scheme != "https" && !dev:
		return fmt.Errorf("webhooks.%s.url: https is required outside dev", name)
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("webhooks.%s.events must list at least one topic", name)
	}
	for _, e := range c.Events {
		if e != "*" && !topicRe.MatchString(e) {
			return fmt.Errorf("webhooks.%s.events: %q is not a topic (use item.published, item.* or *)", name, e)
		}
	}
	if c.Secret == "" {
		return fmt.Errorf("webhooks.%s.secret is required", name)
	}
	if c.Timeout < 0 || c.Timeout > maxTimeout {
		return fmt.Errorf("webhooks.%s.timeout must be between 0 and %s", name, maxTimeout)
	}
	return nil
}

// Options are what a webhook consumer needs from the application.
type Options struct {
	Clock  clock.Clock                                          // default clock.System
	Client *http.Client                                         // default: a client with the webhook's timeout; redirects are never followed
	Tags   func(topic string, payload json.RawMessage) []string // cache tags to include; optional
}

// Body is the JSON a webhook receives.
type Body struct {
	ID        string          `json:"id"`
	Topic     string          `json:"topic"`
	Data      json.RawMessage `json:"data"`
	Tags      []string        `json:"tags"`
	CreatedAt time.Time       `json:"createdAt"`
}

// Consumer returns the outbox consumer delivering to the webhook. The config must be valid.
func Consumer(name string, c Config, o Options) outbox.Consumer {
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := o.Client
	if client == nil {
		client = &http.Client{
			Timeout:       timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
	}
	return outbox.Consumer{
		Name:   "webhook:" + name,
		Topics: c.Events,
		Handle: func(ctx context.Context, e outbox.Event) error {
			return deliver(ctx, client, timeout, o, c, e)
		},
	}
}

func deliver(ctx context.Context, client *http.Client, timeout time.Duration, o Options, c Config, e outbox.Event) error {
	tags := []string{}
	if o.Tags != nil {
		if t := o.Tags(e.Topic, e.Payload); t != nil {
			tags = t
		}
	}
	data := e.Payload
	if len(data) == 0 {
		data = json.RawMessage("null")
	}
	body, err := json.Marshal(Body{ID: strconv.FormatInt(e.ID, 10), Topic: e.Topic, Data: data, Tags: tags,
		CreatedAt: e.CreatedAt.UTC()})
	if err != nil {
		return fmt.Errorf("webhook: encode event %d: %w", e.ID, err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Hydrogen-Webhook")
	req.Header.Set(EventHeader, e.Topic)
	req.Header.Set(DeliveryHeader, strconv.FormatInt(e.ID, 10))
	req.Header.Set(SignatureHeader, Sign(c.Secret, o.Clock.Now(), body))
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL may carry credentials
		}
		return fmt.Errorf("webhook: deliver event %d: %w", e.ID, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("webhook: deliver event %d: status %d", e.ID, resp.StatusCode)
	}
	return nil
}

// Sign returns the Hydrogen-Signature header value for body at time t.
func Sign(secret string, t time.Time, body []byte) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	return "t=" + ts + ",v1=" + mac(secret, ts, body)
}

func mac(secret, ts string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(ts))
	h.Write([]byte{'.'})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

var (
	// ErrSignature means the header is malformed or no signature matches.
	ErrSignature = errors.New("webhook: invalid signature")
	// ErrExpired means the signature is valid but its timestamp is outside the tolerance.
	ErrExpired = errors.New("webhook: signature timestamp outside tolerance")
)

// Verify checks a Hydrogen-Signature header against the raw request body. now is the receiver's time;
// tolerance bounds how far the signed timestamp may differ from it (in either direction), which limits
// replays; a tolerance of zero or less means DefaultTolerance, never "no check". A header may carry several v1 signatures (secret rotation); any match passes.
func Verify(secret, header string, body []byte, now time.Time, tolerance time.Duration) error {
	var ts string
	var sigs []string
	for part := range strings.SplitSeq(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return ErrSignature
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || len(sigs) == 0 {
		return ErrSignature
	}
	want := []byte(mac(secret, ts, body))
	ok := false
	for _, s := range sigs {
		if hmac.Equal([]byte(s), want) {
			ok = true
		}
	}
	if !ok {
		return ErrSignature
	}
	if tolerance <= 0 {
		tolerance = DefaultTolerance
	}
	d := now.Sub(time.Unix(unix, 0))
	if d < 0 {
		d = -d
	}
	if d > tolerance {
		return ErrExpired
	}
	return nil
}
