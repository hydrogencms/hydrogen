package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

type delivered struct {
	status int
	header http.Header
	body   []byte
}

func (d delivered) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(d.body, &m); err != nil {
		t.Fatalf("decode %q: %v", d.body, err)
	}
	return m
}

// deliver calls the delivery API; headers are name, value pairs.
func (s *server) deliver(path string, headers ...string) delivered {
	s.t.Helper()
	req, _ := http.NewRequest("GET", strings.TrimSuffix(s.base, "/manage/v1")+"/deliver/v1"+path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return delivered{resp.StatusCode, resp.Header, b}
}

func (s *server) deliverOK(path string, headers ...string) delivered {
	s.t.Helper()
	d := s.deliver(path, headers...)
	if d.status != 200 {
		s.t.Fatalf("GET %s: %d %s", path, d.status, d.body)
	}
	return d
}

func at(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

func routeOf(channel, raw string) string {
	return "/route?channel=" + channel + "&url=" + url.QueryEscape(raw)
}

func eventuallyEq(t *testing.T, what string, get func() any, want any) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var got any
	for time.Now().Before(deadline) {
		if got = get(); got == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s = %v, want %v", what, got, want)
}

func (f *site) delivery(t *testing.T) {
	s := f.s
	t.Run("route", func(t *testing.T) {
		d := s.deliverOK(routeOf("corporate_en", "https://www.acme.com/en/about"))
		j := d.json(t)
		if at(j, "located", "kind") != "page" || at(j, "item", "id") != f.about || at(j, "item", "fields", "title") != "About" {
			t.Errorf("about: %s", d.body)
		}
		if at(j, "item", "url") != "https://www.acme.com/en/about" || at(j, "located", "canonical") != "https://www.acme.com/en/about" {
			t.Errorf("urls: %s", d.body)
		}
		if tags := d.header.Get("Surrogate-Key"); !strings.Contains(tags, "r:corporate") || !strings.Contains(tags, "i:"+f.about) {
			t.Errorf("tags %q", tags)
		}
		if d.header.Get("Vary") != "Authorization" || !strings.HasPrefix(d.header.Get("Cache-Control"), "public") {
			t.Errorf("headers %v", d.header)
		}
		alts, _ := at(j, "located", "alternates").([]any)
		if len(alts) != 4 {
			t.Errorf("alternates: %v", alts)
		}

		j = s.deliverOK(routeOf("corporate_en", "https://www.acme.com/en/news/hello")).json(t)
		if at(j, "located", "kind") != "item" || at(j, "item", "id") != f.article ||
			at(j, "item", "url") != "https://www.acme.com/en/news/hello" {
			t.Errorf("mounted article: %v", j)
		}
		j = s.deliverOK(routeOf("corporate_dech", "https://www.acme.com/de-ch/about/team")).json(t)
		if at(j, "located", "locale") != "en" || at(j, "item", "id") != f.team {
			t.Errorf("fallback: %v", j)
		}
		j = s.deliverOK(routeOf("corporate_sg_en", "https://www.acme.sg/about")).json(t)
		if at(j, "located", "channel") != "corporate_sg_en" || at(j, "item", "url") != "https://www.acme.sg/about" {
			t.Errorf("market channel: %v", j)
		}
		j = s.deliverOK(routeOf("careers_en", "https://jobs.acme.com/open-roles")).json(t)
		if at(j, "item", "id") != f.roles || at(j, "item", "url") != "https://jobs.acme.com/open-roles" {
			t.Errorf("careers: %v", j)
		}

		for raw, kind := range map[string]string{"https://www.acme.com/en/nothing": "not_found", "https://www.acme.com/en/cart": "reserved"} {
			d = s.deliverOK(routeOf("corporate_en", raw))
			if at(d.json(t), "located", "kind") != kind || d.header.Get("Etag") == "" ||
				!strings.Contains(d.header.Get("Surrogate-Key"), "r:corporate") {
				t.Errorf("%s: %s %v", raw, d.body, d.header)
			}
		}

		// A frontend asks its own channel only.
		for _, c := range []struct{ ch, raw string }{
			{"corporate_en", "https://www.acme.com/de/uber-uns"},
			{"careers_en", "https://www.acme.com/en/about"},
			{"corporate_en", "https://other.test/en/about"},
		} {
			if d := s.deliver(routeOf(c.ch, c.raw)); d.status != 404 {
				t.Errorf("%s %s: %d", c.ch, c.raw, d.status)
			}
		}
		if d := s.deliver("/route?channel=corporate_en"); d.status != 400 {
			t.Errorf("no url: %d", d.status)
		}
		if d := s.deliver("/route?url=" + url.QueryEscape("https://www.acme.com/en/about")); d.status != 400 {
			t.Errorf("several channels need a name: %d", d.status)
		}
	})

	t.Run("not modified", func(t *testing.T) {
		for _, p := range []string{routeOf("corporate_en", "https://www.acme.com/en/about"), "/tree?channel=corporate_en&depth=2",
			"/items/" + f.about + "?channel=corporate_en"} {
			d := s.deliverOK(p)
			etag := d.header.Get("Etag")
			if etag == "" {
				t.Fatalf("%s has no etag", p)
			}
			if d2 := s.deliver(p, "If-None-Match", etag); d2.status != 304 || len(d2.body) != 0 {
				t.Errorf("%s: %d with %d bytes", p, d2.status, len(d2.body))
			}
			if d2 := s.deliver(p, "If-None-Match", `"other"`); d2.status != 200 || string(d2.body) != string(d.body) {
				t.Errorf("%s: stale etag %d", p, d2.status)
			}
		}
	})

	t.Run("tree", func(t *testing.T) {
		d := s.deliverOK("/tree?channel=corporate_en&depth=2")
		j := d.json(t)
		nodes, _ := j["nodes"].([]any)
		byName := map[string]map[string]any{}
		for _, n := range nodes {
			byName[n.(map[string]any)["name"].(string)] = n.(map[string]any)
		}
		about := byName["About"]
		if about == nil || about["url"] != "https://www.acme.com/en/about" || about["path"] != "/about" {
			t.Fatalf("about: %s", d.body)
		}
		if kids, _ := about["nodes"].([]any); len(kids) != 1 || kids[0].(map[string]any)["name"] != "Team" {
			t.Errorf("depth 2: %v", about["nodes"])
		}
		if byName["news"] == nil || byName["news"]["kind"] != "mount" {
			t.Errorf("mount missing: %s", d.body)
		}
		if tags := d.header.Get("Surrogate-Key"); !strings.Contains(tags, "nav:corporate") || !strings.Contains(tags, "r:corporate") {
			t.Errorf("tags %q", tags)
		}
		j = s.deliverOK("/tree?channel=corporate_de").json(t)
		var de []string
		for _, n := range j["nodes"].([]any) {
			de = append(de, n.(map[string]any)["name"].(string))
		}
		if j["locale"] != "de" || !strings.Contains(strings.Join(de, ","), "Uber uns") {
			t.Errorf("de tree: %v", j)
		}
		j = s.deliverOK("/tree?channel=careers_en").json(t)
		if j["site"] != "careers" || len(j["nodes"].([]any)) != 1 {
			t.Errorf("careers tree: %v", j)
		}
		if d := s.deliver("/tree?channel=corporate_en&site=nope"); d.status != 404 {
			t.Errorf("unknown site: %d", d.status)
		}
	})

	t.Run("app channel", func(t *testing.T) {
		auth := []string{"Authorization", "Bearer " + iosToken}
		if d := s.deliver("/items/" + f.productEN + "?channel=ios_app"); d.status != 401 {
			t.Errorf("token channel without a token: %d", d.status)
		}
		d := s.deliverOK("/items/"+f.productEN, auth...)
		if at(d.json(t), "item", "fields", "title") != "Kettle" || !strings.HasPrefix(d.header.Get("Cache-Control"), "private") {
			t.Errorf("product: %s %v", d.body, d.header)
		}
		if d := s.deliver("/items/"+f.about, auth...); d.status != 404 {
			t.Errorf("page type is not in the app's collections: %d", d.status)
		}
		// The app has no URLs to locate and no pages to list.
		if d := s.deliver(routeOf("ios_app", "https://www.acme.com/en/about"), auth...); d.status != 401 && d.status != 404 {
			t.Errorf("route: %d", d.status)
		}
		if d := s.deliver("/route?url="+url.QueryEscape("https://www.acme.com/en/about"), auth...); d.status != 404 {
			t.Errorf("route on the app channel: %d", d.status)
		}
		j := s.deliverOK("/tree", auth...).json(t)
		if n, _ := j["nodes"].([]any); len(n) != 0 {
			t.Errorf("app tree: %v", j)
		}
	})

	t.Run("channel info", func(t *testing.T) {
		j := s.deliverOK("/channels/corporate_en").json(t)
		if at(j, "options", "site") != "corporate" || at(j, "options", "match", "prefix") != "/en" ||
			at(j, "options", "match", "host") != "www.acme.com" {
			t.Errorf("options: %v", j)
		}
		if r, _ := at(j, "options", "reserved").([]any); len(r) != 2 {
			t.Errorf("reserved: %v", r)
		}
		doc := s.deliverOK("/openapi.json").json(t)
		for _, p := range []string{"/route", "/tree"} {
			if at(doc, "paths", p, "get", "responses") == nil {
				t.Errorf("openapi lacks %s", p)
			}
		}
	})

	t.Run("preview of a never-published page", func(t *testing.T) {
		var v view
		s.must(s.do("POST", "/collections/page/items", map[string]any{"locale": "en", "fields": title("Secret launch")}, &v), 201, "create")
		var n node
		aboutNode := ""
		for _, x := range s.deliverOK("/tree?channel=corporate_en").json(t)["nodes"].([]any) {
			if x.(map[string]any)["item"] == f.about {
				aboutNode = x.(map[string]any)["id"].(string)
			}
		}
		s.must(s.do("POST", "/pages/sites/corporate/nodes", map[string]any{"kind": "page", "item": v.ID, "parent": aboutNode}, &n), 201, "place")
		var tok struct{ Token string }
		s.must(s.do("POST", "/preview-tokens", map[string]any{"channel": "corporate_en", "item": v.ID}, &tok), 200, "mint")

		if d := s.deliver("/items/" + v.ID + "?channel=corporate_en"); d.status != 404 {
			t.Errorf("unpublished without a token: %d", d.status)
		}
		d := s.deliverOK("/items/"+v.ID+"?channel=corporate_en", "X-Hydrogen-Preview", tok.Token)
		if at(d.json(t), "item", "fields", "title") != "Secret launch" || d.header.Get("Cache-Control") != "no-store" || d.header.Get("Surrogate-Key") != "" {
			t.Errorf("preview: %s %v", d.body, d.header)
		}
		d = s.deliverOK("/items/" + v.ID + "?preview=" + url.QueryEscape(tok.Token))
		if at(d.json(t), "item", "fields", "title") != "Secret launch" {
			t.Errorf("preview chooses its channel: %s", d.body)
		}
		// The published navigation does not show it, with or without the token.
		for _, hdr := range [][]string{nil, {"X-Hydrogen-Preview", tok.Token}} {
			if strings.Contains(string(s.deliverOK("/tree?channel=corporate_en&depth=2", hdr...).body), "Secret launch") {
				t.Error("tree lists an unpublished page")
			}
		}
		// A token for another item or channel does not open it.
		other := struct{ Token string }{}
		s.must(s.do("POST", "/preview-tokens", map[string]any{"channel": "corporate_en", "item": f.about}, &other), 200, "mint other")
		if d := s.deliver("/items/"+v.ID+"?channel=corporate_en", "X-Hydrogen-Preview", other.Token); d.status != 404 {
			t.Errorf("scoped token: %d", d.status)
		}
		if d := s.deliver("/items/"+v.ID+"?channel=careers_en", "X-Hydrogen-Preview", tok.Token); d.status != 401 {
			t.Errorf("token of another channel: %d", d.status)
		}
		s.must(s.do("POST", "/items/"+v.ID+"/publish", map[string]any{}, nil), 200, "publish")
		eventuallyEq(t, "the page in the tree", func() any {
			return strings.Contains(string(s.deliverOK("/tree?channel=corporate_en&depth=2").body), "Secret launch")
		}, true)
		if at(s.deliverOK(routeOf("corporate_en", "https://www.acme.com/en/about/secret-launch")).json(t), "item", "id") != v.ID {
			t.Error("published page is not routed")
		}
	})

	t.Run("purge on rename", func(t *testing.T) {
		old := routeOf("corporate_en", "https://www.acme.com/en/contact")
		kind := func() any { return at(s.deliverOK(old).json(t), "located", "kind") }
		if kind() != "page" {
			t.Fatalf("contact: %v", kind())
		}
		tree := func() any {
			for _, n := range s.deliverOK("/tree?channel=corporate_en").json(t)["nodes"].([]any) {
				if n.(map[string]any)["item"] == f.contact {
					return n.(map[string]any)["path"]
				}
			}
			return nil
		}
		if tree() != "/contact" {
			t.Fatalf("tree path %v", tree())
		}
		s.must(s.do("PATCH", "/items/"+f.contact, map[string]any{"locale": "en", "fields": map[string]any{"slug": "reach-us"}}, nil), 200, "rename")
		s.must(s.do("POST", "/items/"+f.contact+"/publish", map[string]any{}, nil), 200, "publish")
		eventuallyEq(t, "old route kind", kind, "redirect")
		eventuallyEq(t, "tree path", tree, "/reach-us")
	})
}

func deliveryScenario(t *testing.T, s *server) {
	f := buildSites(s)
	f.delivery(t)
}

func TestMultisiteDeliverySQLite(t *testing.T) {
	deliveryScenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configureWithProbe))
}

func TestMultisiteDeliveryPostgres(t *testing.T) {
	deliveryScenario(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configureWithProbe))
}
