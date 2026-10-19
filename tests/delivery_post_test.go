package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/api/delivery"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

// TestDeliveryPostRoute checks a module route that also accepts POST: the same channel selection and token
// rules, no caching, a body limit and CORS preflight for allowed origins.
func TestDeliveryPostRoute(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		n := newDeliveryNodeWithChannels(t, b.open(t, clk), clk, deliveryChannels, fnMod{"postmod", func(r *kernel.Registry) error {
			r.DeliveryRoute(delivery.Route{Method: "GET", Path: "/q", Post: true, Handler: func(w http.ResponseWriter, r *http.Request) {
				body := ""
				if r.Method == http.MethodPost {
					raw, err := delivery.ReadBody(r)
					if err != nil {
						delivery.Fail(w, r, err)
						return
					}
					body = string(raw)
				}
				delivery.Tag(r, "x:q")
				delivery.WriteJSON(w, r, 200, map[string]any{"method": r.Method, "channel": delivery.Channel(r).Name,
					"preview": delivery.Preview(r), "body": body})
			}})
			return nil
		}})
		bearer := []string{"Authorization", "Bearer " + appToken}
		post := func(path string, body string, hdr ...string) resp {
			m := map[string]string{"Content-Type": "application/json"}
			for i := 0; i+1 < len(hdr); i += 2 {
				m[hdr[i]] = hdr[i+1]
			}
			return n.doRaw("POST", "/api/deliver/v1"+path, m, body)
		}

		g := n.wantStatus("/q?channel=web", 200)
		if g.Header().Get("ETag") == "" || !strings.Contains(g.Header().Get("Surrogate-Key"), "x:q") {
			t.Errorf("GET headers: %v", g.Header())
		}
		p := post("/q?channel=web", `{"a":1}`)
		if p.Code != 200 || !strings.Contains(p.Body.String(), `"body":"{\"a\":1}"`) || !strings.Contains(p.Body.String(), `"method":"POST"`) {
			t.Fatalf("POST: %d %s", p.Code, p.Body)
		}
		if p.Header().Get("Cache-Control") != "no-store" || p.Header().Get("ETag") != "" {
			t.Errorf("POST must not be cacheable: %v", p.Header())
		}
		// A POST is not served from, and does not fill, the GET cache.
		if again := n.wantStatus("/q?channel=web", 200); strings.Contains(again.Body.String(), `"body":"{`) {
			t.Errorf("GET served a POST body: %s", again.Body)
		}

		// Channel rules are the GET rules.
		if r := post("/q?channel=app", "{}"); r.Code != 401 {
			t.Errorf("token channel without token: %d", r.Code)
		}
		if r := post("/q", "{}", bearer...); r.Code != 200 || !strings.Contains(r.Body.String(), `"channel":"app"`) {
			t.Errorf("token channel: %d %s", r.Code, r.Body)
		}
		if r := post("/q", "{}"); r.Code != 400 {
			t.Errorf("no channel: %d", r.Code)
		}
		tok := n.previewToken(map[string]any{"channel": "web"}, 200)
		if r := post("/q", "{}", previewHeaderName, tok); r.Code != 200 || !strings.Contains(r.Body.String(), `"preview":true`) {
			t.Errorf("preview: %d %s", r.Code, r.Body)
		}

		// Body limit.
		if r := post("/q?channel=web", `"`+strings.Repeat("x", delivery.MaxBodyBytes)+`"`); r.Code != 413 {
			t.Errorf("large body: %d", r.Code)
		}

		// A route without Post stays GET only.
		if r := n.do("POST", "/api/deliver/v1/items/x?channel=web", nil, nil); r.Code == 200 {
			t.Errorf("POST to a GET route: %d", r.Code)
		}

		// CORS: the request's own origin is allowed, others are not.
		pf := func(origin string) resp {
			return n.do("OPTIONS", "/api/deliver/v1/q", map[string]string{"Origin": origin, "Access-Control-Request-Method": "POST"}, nil)
		}
		if r := pf("http://example.com"); r.Code != 204 || r.Header().Get("Access-Control-Allow-Origin") != "http://example.com" ||
			!strings.Contains(r.Header().Get("Access-Control-Allow-Methods"), "POST") {
			t.Errorf("preflight: %d %v", r.Code, r.Header())
		}
		if r := pf("http://evil.test"); r.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("preflight allowed a foreign origin: %v", r.Header())
		}
		if r := post("/q?channel=web", "{}", "Origin", "http://example.com"); r.Header().Get("Access-Control-Allow-Origin") != "http://example.com" {
			t.Errorf("POST without CORS headers: %v", r.Header())
		}
		if r := post("/q?channel=web", "{}", "Origin", "http://evil.test"); r.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("POST allowed a foreign origin: %v", r.Header())
		}
	})
}

// doRaw sends a request with a body as given.
func (n *dnode) doRaw(method, path string, hdr map[string]string, body string) resp {
	n.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:50000"
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	n.h.ServeHTTP(rec, req)
	return resp{rec}
}
