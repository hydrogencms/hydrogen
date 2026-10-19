package siteproxy

import (
	"context"
	"errors"
	"github.com/hydrogencms/hydrogen/core/access"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

type targetKey struct{}

// proxy forwards requests to the supervised frontend.
type proxy struct {
	target    func() *url.URL
	trusted   []netip.Prefix
	rp        *httputil.ReverseProxy
	transport *http.Transport
	log       *slog.Logger

	mu       sync.Mutex
	draining bool
	inflight int
	idle     chan struct{} // closed when draining and inflight is 0
}

func newProxy(target func() *url.URL, timeout time.Duration, trusted []netip.Prefix, log *slog.Logger) *proxy {
	p := &proxy{target: target, trusted: trusted, log: log}
	p.transport = &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ResponseHeaderTimeout: timeout,
		MaxIdleConnsPerHost:   64,
		IdleConnTimeout:       60 * time.Second,
		ForceAttemptHTTP2:     false,
	}
	p.rp = &httputil.ReverseProxy{
		Rewrite:       p.rewrite,
		Transport:     p.transport,
		FlushInterval: -1, // stream responses (RSC, SSE) as they arrive
		ErrorHandler:  p.fail,
	}
	return p
}

// drain refuses new requests and waits for the running ones, until ctx ends.
func (p *proxy) drain(ctx context.Context) {
	p.mu.Lock()
	p.draining = true
	p.idle = make(chan struct{})
	if p.inflight == 0 {
		close(p.idle)
	}
	idle := p.idle
	p.mu.Unlock()
	select {
	case <-idle:
	case <-ctx.Done():
	}
}

func (p *proxy) enter() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.draining {
		return false
	}
	p.inflight++
	return true
}

func (p *proxy) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inflight--
	if p.draining && p.inflight == 0 {
		close(p.idle)
	}
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.enter() {
		w.Header().Set("Connection", "close")
		http.Error(w, "the website is shutting down", http.StatusServiceUnavailable)
		return
	}
	defer p.leave()
	t := p.target()
	if t == nil {
		w.Header().Set("Retry-After", "2")
		w.Header().Set("Cache-Control", "no-store")
		http.Error(w, "the website is starting", http.StatusServiceUnavailable)
		return
	}
	p.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), targetKey{}, t)))
}

// rewrite points the request at the frontend, keeps the client's Host and sets the X-Forwarded headers. The
// reverse proxy removes the inbound ones first; they are taken over only from a trusted proxy
// (http.trusted_proxies), as the sign-in client address is.
func (p *proxy) rewrite(pr *httputil.ProxyRequest) {
	in, out := pr.In, pr.Out
	pr.SetURL(in.Context().Value(targetKey{}).(*url.URL))
	out.Host = in.Host
	stripCookie(out.Header, access.SessionCookie)

	peer := ""
	if h, _, err := net.SplitHostPort(in.RemoteAddr); err == nil {
		peer = h
	}
	trusted := false
	if a, err := netip.ParseAddr(peer); err == nil {
		for _, pre := range p.trusted {
			if pre.Contains(a.Unmap()) {
				trusted = true
				break
			}
		}
	}
	forwarded := peer
	proto, host := "http", in.Host
	if in.TLS != nil {
		proto = "https"
	}
	if trusted {
		if v := strings.Join(in.Header.Values("X-Forwarded-For"), ", "); v != "" {
			forwarded = v + ", " + peer
		}
		if v := in.Header.Get("X-Forwarded-Proto"); v == "http" || v == "https" {
			proto = v
		}
		if v := in.Header.Get("X-Forwarded-Host"); v != "" {
			host = v
		}
	}
	if forwarded != "" {
		out.Header.Set("X-Forwarded-For", forwarded)
	}
	out.Header.Set("X-Forwarded-Proto", proto)
	out.Header.Set("X-Forwarded-Host", host)
}

func (p *proxy) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return // the client went away
	}
	p.log.Warn("site-proxy: the frontend did not answer", "method", r.Method, "path", r.URL.Path, "err", err)
	status := http.StatusBadGateway
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		status = http.StatusGatewayTimeout
	}
	http.Error(w, http.StatusText(status), status)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func parsePrefixes(list []string) []netip.Prefix {
	var out []netip.Prefix
	for _, s := range list {
		if pre, err := netip.ParsePrefix(s); err == nil {
			out = append(out, pre)
		} else if a, err := netip.ParseAddr(s); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

// stripCookie removes one cookie from the Cookie header: the admin session must not reach frontend code, which
// could replay it against the management API. Preview cookies and the frontend's own pass through.
func stripCookie(h http.Header, name string) {
	vals := h.Values("Cookie")
	if len(vals) == 0 {
		return
	}
	var keep []string
	for _, line := range vals {
		for part := range strings.SplitSeq(line, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if k, _, _ := strings.Cut(part, "="); k == name {
				continue
			}
			keep = append(keep, part)
		}
	}
	if len(keep) == 0 {
		h.Del("Cookie")
		return
	}
	h.Set("Cookie", strings.Join(keep, "; "))
}
