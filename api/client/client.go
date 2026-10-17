// Package client is the Go client for Hydrogen's HTTP APIs: [Delivery] reads published content (and drafts with
// a preview token) from the delivery API, [Manage] writes through the management API. It uses only the standard
// library and keeps no global state; every call takes a context.
//
// Delivery responses return the items as raw JSON next to the response metadata (cache tags, ETag, status), so
// a caller decodes into whatever it likes. [DecodeItem] decodes into the structs `hydrogen generate` writes to
// gen/content:
//
//	d := client.NewDelivery("https://cms.acme.com", client.WithChannel("web"))
//	res, err := d.ItemByKey(ctx, "about", nil)
//	item, err := client.DecodeItem[content.Page](res.Item)
//
// Failures come back as *[Error], decoded from the API's error envelope.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	deliverPrefix = "/api/deliver/v1"
	managePrefix  = "/api/manage/v1"
	previewHeader = "X-Hydrogen-Preview"
	maxBody       = 64 << 20
)

// Option configures a client.
type Option func(*settings)

type settings struct {
	hc      *http.Client
	channel string
	token   string
	preview string
	header  http.Header
}

// WithChannel selects the channel by name (?channel=). Token channels are selected by their token instead.
func WithChannel(name string) Option { return func(s *settings) { s.channel = name } }

// WithToken sends a bearer token: a channel token for the delivery API (it selects the channel), the management
// token for [NewManage] (which takes it as an argument already).
func WithToken(token string) Option { return func(s *settings) { s.token = token } }

// WithPreview sends a preview token (X-Hydrogen-Preview, minted by [Manage.PreviewToken]): the delivery API then
// answers with draft content, uncached.
func WithPreview(token string) Option { return func(s *settings) { s.preview = token } }

// WithHTTPClient sets the http.Client; the default is a client with a 30 s timeout.
func WithHTTPClient(hc *http.Client) Option { return func(s *settings) { s.hc = hc } }

// WithHeader adds a header to every request. It may be given repeatedly; values accumulate.
func WithHeader(name, value string) Option {
	return func(s *settings) {
		if s.header == nil {
			s.header = http.Header{}
		}
		s.header.Add(name, value)
	}
}

// Error is a non-2xx answer in the APIs' error envelope.
type Error struct {
	Status    int // HTTP status
	Code      string
	Message   string
	Fields    []FieldError // validation problems (status 422, code "invalid")
	Locale    string       // the locale a validation error belongs to
	Parts     []string     // conflicting draft parts (code "conflict")
	Referrers []Referrer   // items that block a delete (code "referenced")
}

// FieldError is one validation problem.
type FieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Referrer is an item whose fields point at another one.
type Referrer struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	Field    string `json:"field"`
	Kind     string `json:"kind"`
	OnDelete string `json:"onDelete"`
}

func (e *Error) Error() string {
	msg := fmt.Sprintf("hydrogen: %d %s: %s", e.Status, e.Code, e.Message)
	for _, f := range e.Fields {
		msg += "; " + f.Path + ": " + f.Message
	}
	return msg
}

// IsNotFound reports whether err is an *Error with status 404.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// Meta is what a delivery response says about itself.
type Meta struct {
	Status       int      // HTTP status: 200, or 304 when If-None-Match matched
	ETag         string   // strong ETag; empty on previews
	Tags         []string // cache tags (Surrogate-Key); empty on previews
	CacheControl string
	Header       http.Header
}

// NotModified reports a 304 answer to an If-None-Match request; the result has no content then.
func (m Meta) NotModified() bool { return m.Status == http.StatusNotModified }

func metaOf(resp *http.Response) Meta {
	m := Meta{Status: resp.StatusCode, ETag: resp.Header.Get("ETag"), CacheControl: resp.Header.Get("Cache-Control"), Header: resp.Header}
	if v := resp.Header.Get("Surrogate-Key"); v != "" {
		m.Tags = strings.Fields(v)
	} else if v := resp.Header.Get("Cache-Tag"); v != "" {
		for t := range strings.SplitSeq(v, ",") {
			if t = strings.TrimSpace(t); t != "" {
				m.Tags = append(m.Tags, t)
			}
		}
	}
	return m
}

type core struct {
	base   string
	prefix string
	s      settings
}

func newCore(baseURL, prefix string, opts []Option) core {
	c := core{base: strings.TrimRight(baseURL, "/"), prefix: prefix}
	for _, o := range opts {
		o(&c.s)
	}
	if c.s.hc == nil {
		c.s.hc = &http.Client{Timeout: 30 * time.Second}
	}
	return c
}

// call sends a request; query is the encoded query string without "?". It returns the response and its body
// for 2xx (and 304 when allow304); everything else is an *Error.
func (c *core) call(ctx context.Context, method, path, query string, body any, hdr http.Header, allow304 bool) (*http.Response, []byte, error) {
	u := c.base + c.prefix + path
	if query != "" {
		u += "?" + query
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("hydrogen: encode request: %w", err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, nil, fmt.Errorf("hydrogen: %w", err)
	}
	for k, vs := range c.s.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.s.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.s.token)
	}
	if c.s.preview != "" && c.prefix == deliverPrefix {
		req.Header.Set(previewHeader, c.s.preview)
	}
	resp, err := c.s.hc.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("hydrogen: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, nil, fmt.Errorf("hydrogen: %s %s: read response: %w", method, path, err)
	}
	switch {
	case resp.StatusCode/100 == 2, allow304 && resp.StatusCode == http.StatusNotModified:
		return resp, raw, nil
	}
	return nil, nil, decodeError(resp.StatusCode, raw)
}

func decodeError(status int, raw []byte) *Error {
	var env struct {
		Error struct {
			Code      string       `json:"code"`
			Message   string       `json:"message"`
			Fields    []FieldError `json:"fields"`
			Locale    string       `json:"locale"`
			Parts     []string     `json:"parts"`
			Referrers []Referrer   `json:"referrers"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Error.Code != "" {
		e := env.Error
		return &Error{Status: status, Code: e.Code, Message: e.Message, Fields: e.Fields, Locale: e.Locale, Parts: e.Parts, Referrers: e.Referrers}
	}
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 200 {
		msg = msg[:200]
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &Error{Status: status, Code: "http_error", Message: msg}
}

func decode(raw []byte, into any) error {
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("hydrogen: decode response: %w", err)
	}
	return nil
}
