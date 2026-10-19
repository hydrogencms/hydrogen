package siteproxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testProxy(t *testing.T, backend http.Handler, trusted ...string) *httptest.Server {
	t.Helper()
	be := httptest.NewServer(backend)
	t.Cleanup(be.Close)
	u, _ := url.Parse(be.URL)
	p := newProxy(func() *url.URL { return u }, 2*time.Second, parsePrefixes(trusted), slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	return srv
}

func echo(w http.ResponseWriter, r *http.Request) {
	for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host"} {
		_, _ = io.WriteString(w, h+"="+r.Header.Get(h)+"\n")
	}
	_, _ = io.WriteString(w, "Host="+r.Host+"\nURI="+r.URL.RequestURI()+"\n")
}

func fetch(t *testing.T, srv *httptest.Server, path string, hdr map[string]string) string {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.Host = "www.example.com"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func TestProxyForwardsHostAndHeaders(t *testing.T) {
	srv := testProxy(t, http.HandlerFunc(echo))
	got := fetch(t, srv, "/a/b?x=1&y=%2F", map[string]string{"X-Forwarded-For": "6.6.6.6", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.test"})
	for _, want := range []string{"X-Forwarded-For=127.0.0.1\n", "X-Forwarded-Proto=http\n", "X-Forwarded-Host=www.example.com\n", "Host=www.example.com\n", "URI=/a/b?x=1&y=%2F\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestProxyTrustsForwardedHeadersFromTrustedPeers(t *testing.T) {
	srv := testProxy(t, http.HandlerFunc(echo), "127.0.0.0/8")
	got := fetch(t, srv, "/", map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "cdn.example.com"})
	for _, want := range []string{"X-Forwarded-For=203.0.113.9, 127.0.0.1\n", "X-Forwarded-Proto=https\n", "X-Forwarded-Host=cdn.example.com\n", "Host=www.example.com\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

func TestProxyStreams(t *testing.T) {
	srv := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "first\n")
		w.(http.Flusher).Flush()
		time.Sleep(500 * time.Millisecond)
		_, _ = io.WriteString(w, "second\n")
	}))
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	began := time.Now()
	buf := make([]byte, 6)
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "first\n" {
		t.Fatalf("first chunk: %q %v", buf, err)
	}
	if d := time.Since(began); d > 300*time.Millisecond {
		t.Errorf("first chunk arrived after %s: the response is buffered", d)
	}
	rest, _ := io.ReadAll(resp.Body)
	if string(rest) != "second\n" {
		t.Errorf("rest %q", rest)
	}
}

func TestProxyStatusesAndErrors(t *testing.T) {
	srv := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
		case "/slow":
			time.Sleep(3 * time.Second)
		default:
			http.Error(w, "teapot", http.StatusTeapot)
		}
	}))
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if resp, err := client.Get(srv.URL + "/redirect"); err != nil || resp.StatusCode != 302 || resp.Header.Get("Location") != "/elsewhere" {
		t.Errorf("redirect passthrough: %v %v", resp, err)
	}
	if resp, _ := client.Get(srv.URL + "/x"); resp.StatusCode != http.StatusTeapot {
		t.Errorf("status passthrough: %d", resp.StatusCode)
	}
	if resp, _ := client.Get(srv.URL + "/slow"); resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("header timeout: %d, want 504", resp.StatusCode)
	}
}

func TestProxyDownIs503AndDeadBackendIs502(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	down := httptest.NewServer(newProxy(func() *url.URL { return nil }, time.Second, nil, log))
	defer down.Close()
	resp, _ := http.Get(down.URL)
	if resp.StatusCode != 503 || resp.Header.Get("Retry-After") == "" {
		t.Errorf("down: %d %v", resp.StatusCode, resp.Header)
	}
	dead := httptest.NewServer(newProxy(func() *url.URL { u, _ := url.Parse("http://127.0.0.1:1"); return u }, time.Second, nil, log))
	defer dead.Close()
	if resp, _ := http.Get(dead.URL); resp.StatusCode != http.StatusBadGateway {
		t.Errorf("refused: %d", resp.StatusCode)
	}
}

func TestProxyStripsTheSessionCookie(t *testing.T) {
	srv := testProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, r.Header.Get("Cookie")) }))
	got := fetch(t, srv, "/", map[string]string{"Cookie": "a=1; hydrogen_session=SECRET; hydrogen-preview=tok"})
	if got != "a=1; hydrogen-preview=tok" {
		t.Errorf("cookie = %q", got)
	}
	if got := fetch(t, srv, "/", map[string]string{"Cookie": "hydrogen_session=SECRET"}); got != "" {
		t.Errorf("only the session: %q", got)
	}
}

func TestProxyDrainsInFlightRequests(t *testing.T) {
	be := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		_, _ = io.WriteString(w, "done")
	}))
	defer be.Close()
	u, _ := url.Parse(be.URL)
	p := newProxy(func() *url.URL { return u }, 2*time.Second, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	front := httptest.NewServer(p)
	defer front.Close()
	res := make(chan string, 1)
	go func() {
		resp, err := http.Get(front.URL)
		if err != nil {
			res <- err.Error()
			return
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		res <- string(b)
	}()
	time.Sleep(100 * time.Millisecond)
	drained := make(chan struct{})
	go func() { p.drain(context.Background()); close(drained) }()
	time.Sleep(50 * time.Millisecond)
	if resp, _ := http.Get(front.URL); resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("new request while draining: %d", resp.StatusCode)
	}
	select {
	case <-drained:
		t.Fatal("drain returned before the in-flight request finished")
	default:
	}
	if got := <-res; got != "done" {
		t.Errorf("in-flight request: %q", got)
	}
	select {
	case <-drained:
	case <-time.After(2 * time.Second):
		t.Fatal("drain did not return")
	}
}
