package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/outbox"
)

func TestValidate(t *testing.T) {
	ok := Config{URL: "https://x.test/hook", Events: []string{"item.*", "page.changed", "*"}, Secret: "s"}
	if err := ok.Validate("revalidate", false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mut  func(c *Config)
		dev  bool
		want string
	}{
		{"revalidate", func(c *Config) { c.URL = "http://x.test" }, false, "https is required"},
		{"revalidate", func(c *Config) { c.URL = "http://x.test" }, true, ""},
		{"revalidate", func(c *Config) { c.URL = "" }, false, "url is required"},
		{"revalidate", func(c *Config) { c.URL = "ftp://x" }, false, "not an http(s) URL"},
		{"revalidate", func(c *Config) { c.Events = nil }, false, "at least one topic"},
		{"revalidate", func(c *Config) { c.Events = []string{"Item Published"} }, false, "not a topic"},
		{"revalidate", func(c *Config) { c.Events = []string{"item.*.x"} }, false, "not a topic"},
		{"revalidate", func(c *Config) { c.Secret = "" }, false, "secret is required"},
		{"revalidate", func(c *Config) { c.Timeout = time.Hour }, false, "timeout"},
		{"Bad Name", func(c *Config) {}, false, "name must be"},
	} {
		c := ok
		tc.mut(&c)
		err := c.Validate(tc.name, tc.dev)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s %+v: %v, want %q", tc.name, c, err, tc.want)
		}
	}
}

func TestSignVerify(t *testing.T) {
	now := time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC)
	body := []byte(`{"id":"1"}`)
	h := Sign("secret", now, body)
	if err := Verify("secret", h, body, now.Add(time.Minute), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	ts, sig, _ := strings.Cut(h, ",")
	for _, tc := range []struct {
		name   string
		secret string
		header string
		body   []byte
		now    time.Time
		want   error
	}{
		{"wrong secret", "other", h, body, now, ErrSignature},
		{"tampered", "secret", h, []byte(`{"id":"2"}`), now, ErrSignature},
		{"old", "secret", h, body, now.Add(time.Hour), ErrExpired},
		{"future", "secret", h, body, now.Add(-time.Hour), ErrExpired},
		{"malformed", "secret", "garbage", body, now, ErrSignature},
		{"no v1", "secret", "t=1", body, now, ErrSignature},
		{"bad t", "secret", "t=x,v1=00", body, now, ErrSignature},
		{"rotation", "secret", ts + ",v1=00," + sig, body, now, nil},
	} {
		if err := Verify(tc.secret, tc.header, tc.body, tc.now, 5*time.Minute); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
		}
	}
	// A zero tolerance means the default, not "unchecked".
	if err := Verify("secret", h, body, now.Add(100*time.Hour), 0); !errors.Is(err, ErrExpired) {
		t.Errorf("zero tolerance, old signature: %v", err)
	}
	if err := Verify("secret", h, body, now.Add(DefaultTolerance-time.Second), -1); err != nil {
		t.Errorf("zero tolerance, recent signature: %v", err)
	}
}

func TestDelivery(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	status := http.StatusInternalServerError
	var gotBody []byte
	var gotHdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHdr = r.Header.Clone()
		w.WriteHeader(status)
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	cfg := Config{URL: srv.URL, Events: []string{"item.*"}, Secret: "s3cret"}
	c := Consumer("revalidate", cfg, Options{Clock: clk, Tags: func(string, json.RawMessage) []string {
		return []string{"c:article", "i:1"}
	}})
	if c.Name != "webhook:revalidate" || len(c.Topics) != 1 {
		t.Fatalf("consumer = %+v", c)
	}
	ev := outbox.Event{ID: 42, Topic: "item.published", Payload: json.RawMessage(`{"id":"1","type":"article"}`),
		CreatedAt: clk.Now().Add(-time.Second), Attempt: 1}

	if err := c.Handle(context.Background(), ev); err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("500: %v", err)
	}
	status = http.StatusNoContent
	ev.Attempt = 2
	if err := c.Handle(context.Background(), ev); err != nil {
		t.Fatalf("204: %v", err)
	}
	if gotHdr.Get("Hydrogen-Delivery") != "42" || gotHdr.Get("Hydrogen-Event") != "item.published" ||
		gotHdr.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", gotHdr)
	}
	if err := Verify("s3cret", gotHdr.Get("Hydrogen-Signature"), gotBody, clk.Now(), time.Minute); err != nil {
		t.Errorf("signature: %v", err)
	}
	var b struct {
		ID        string         `json:"id"`
		Topic     string         `json:"topic"`
		Data      map[string]any `json:"data"`
		Tags      []string       `json:"tags"`
		CreatedAt time.Time      `json:"createdAt"`
	}
	if err := json.Unmarshal(gotBody, &b); err != nil {
		t.Fatal(err)
	}
	if b.ID != "42" || b.Topic != "item.published" || b.Data["type"] != "article" ||
		strings.Join(b.Tags, ",") != "c:article,i:1" || !b.CreatedAt.Equal(ev.CreatedAt) {
		t.Errorf("body = %s", gotBody)
	}
}

func TestNoRedirectsAndTimeout(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect followed")
	}))
	defer target.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redir.Close()
	c := Consumer("r", Config{URL: redir.URL, Events: []string{"*"}, Secret: "s"}, Options{})
	if err := c.Handle(context.Background(), outbox.Event{ID: 1, Topic: "x.y"}); err == nil || !strings.Contains(err.Error(), "status 302") {
		t.Errorf("redirect: %v", err)
	}

	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer slow.Close()
	defer close(release)
	c = Consumer("s", Config{URL: slow.URL, Events: []string{"*"}, Secret: "s", Timeout: 50 * time.Millisecond}, Options{})
	if err := c.Handle(context.Background(), outbox.Event{ID: 1, Topic: "x.y"}); err == nil {
		t.Error("timeout: no error")
	}
}
