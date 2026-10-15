package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

const (
	token  = "test-token"
	secret = "test-preview-secret-0123456789abcdef"
)

// runCLI runs a project command against the database in vars.
func runCLI(t *testing.T, vars map[string]string, args ...string) {
	t.Helper()
	vars["MANAGE_TOKEN"], vars["PREVIEW_SECRET"] = token, secret
	opts := &config.Options{Dir: "config", Env: "prod", LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	var out strings.Builder
	if err := hydrogen.Run(context.Background(), args, configure, hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: io.Discard}); err != nil {
		t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out.String())
	}
}

type server struct {
	t    *testing.T
	base string // .../api
}

func start(t *testing.T, dbURL string) *server {
	t.Helper()
	vars := map[string]string{"DATABASE_URL": dbURL, "LISTEN_ADDR": "127.0.0.1:0", "MANAGE_TOKEN": token, "PREVIEW_SECRET": secret}
	opts := &config.Options{Dir: "config", Env: "prod", LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	ctx, cancel := context.WithCancel(context.Background())
	addr, done := make(chan string, 1), make(chan error, 1)
	go func() {
		done <- hydrogen.Run(ctx, []string{"serve"}, configure, hydrogen.RunOptions{Config: opts, Stderr: io.Discard,
			PasswordIterations: 1000, Listening: func(a string) { addr <- a }})
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	select {
	case a := <-addr:
		return &server{t: t, base: "http://" + a + "/api"}
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not start")
	}
	return nil
}

type reply struct {
	status int
	header http.Header
	body   []byte
}

func (r reply) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatalf("decode %q: %v", r.body, err)
	}
	return m
}

func (s *server) send(method, path string, body any) reply {
	s.t.Helper()
	return s.request(method, path, body, true)
}

func (s *server) request(method, path string, body any, auth bool) reply {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.base+path, rd)
	if auth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return reply{resp.StatusCode, resp.Header, b}
}

func (s *server) ok(method, path string, body any) map[string]any {
	s.t.Helper()
	r := s.send(method, path, body)
	if r.status/100 != 2 {
		s.t.Fatalf("%s %s: %d %s", method, path, r.status, r.body)
	}
	if len(r.body) == 0 {
		return nil
	}
	return r.json(s.t)
}

func (s *server) manage(method, path string, body any) map[string]any {
	return s.ok(method, "/manage/v1"+path, body)
}

func (s *server) deliver(path string) reply { return s.request("GET", "/deliver/v1"+path, nil, false) }

func at(v any, path ...any) any {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]any)
			v = m[k]
		case int:
			l, _ := v.([]any)
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func backends(t *testing.T, fn func(t *testing.T, dbURL string)) {
	t.Run("sqlite", func(t *testing.T) { fn(t, "sqlite:"+filepath.Join(t.TempDir(), "starter.db")) })
	t.Run("postgres", func(t *testing.T) { fn(t, pgtest.Scratch(t)) })
}

// imported is a server over a database that holds the fixture.
func imported(t *testing.T, dbURL string) *server {
	t.Helper()
	vars := map[string]string{"DATABASE_URL": dbURL}
	runCLI(t, vars, "migrate")
	runCLI(t, vars, "import", "fixture")
	return start(t, dbURL)
}

func routeURL(channel, path string) string {
	return "/route?channel=" + channel + "&url=" + url.QueryEscape("http://localhost:3000"+path)
}

// located returns the delivery payload of a page URL: {located, item}.
func (s *server) located(channel, path string) (map[string]any, reply) {
	s.t.Helper()
	r := s.deliver(routeURL(channel, path))
	if r.status != 200 {
		return nil, r
	}
	return r.json(s.t), r
}

func (s *server) mustLocate(channel, path string) (map[string]any, reply) {
	s.t.Helper()
	m, r := s.located(channel, path)
	if m == nil {
		s.t.Fatalf("GET %s: %d %s", path, r.status, r.body)
	}
	return m, r
}

// blockTypes lists the types of a block tree, depth first.
func blockTypes(blocks any) []string {
	var out []string
	list, _ := blocks.([]any)
	for _, b := range list {
		m, _ := b.(map[string]any)
		out = append(out, m["type"].(string))
		if slots, ok := m["slots"].(map[string]any); ok {
			for _, k := range []string{"items"} {
				out = append(out, blockTypes(slots[k])...)
			}
		}
	}
	return out
}

func sharedBlocksOf(t *testing.T, blocks any) []any {
	t.Helper()
	for _, b := range blocks.([]any) {
		if m := b.(map[string]any); m["type"] == "shared" {
			inner, _ := m["blocks"].([]any)
			return inner
		}
	}
	t.Fatal("no shared block")
	return nil
}

func TestFixtureImportsAndServes(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := imported(t, dbURL)

		cat := s.manage("GET", "/blocks", nil)
		var names []string
		for _, b := range cat["blocks"].([]any) {
			names = append(names, b.(map[string]any)["name"].(string))
		}
		if strings.Join(names, ",") != "callout,card,cards,hero,image,richText,shared" {
			t.Errorf("blocks = %v", names)
		}
		if at(cat, "tokens", 0, "name") != "space" || at(cat, "tokens", 1, "name") != "tone" {
			t.Errorf("tokens = %v", cat["tokens"])
		}

		oa := s.deliver("/openapi.json")
		if oa.status != 200 || strings.Contains(string(oa.body), "$defs") || !strings.Contains(string(oa.body), `"#/components/schemas/Block_hero"`) {
			t.Errorf("delivery OpenAPI: %d, block definitions missing or not hoisted", oa.status)
		}

		home, _ := s.mustLocate("en", "/en")
		if at(home, "item", "type") != "landing" || at(home, "item", "fields", "title") != "Home" {
			t.Fatalf("home = %v", home["item"])
		}
		got := blockTypes(at(home, "item", "fields", "body"))
		if strings.Join(got, ",") != "hero,cards,card,card,card,richText,image,callout,shared" {
			t.Errorf("home blocks = %v", got)
		}
		inner := sharedBlocksOf(t, at(home, "item", "fields", "body"))
		if strings.Join(blockTypes(inner), ",") != "callout,richText" {
			t.Errorf("shared blocks = %v", inner)
		}
		// Links inside shared content resolve through the page's links.
		if links, _ := at(home, "item", "links").(map[string]any); links["01a0f6b4-d000-70f5-824c-eb2a0a977bd1"] == nil {
			t.Errorf("links lack the contact page: %v", at(home, "item", "links"))
		}

		de, _ := s.mustLocate("de", "/de")
		if at(de, "item", "fields", "title") != "Startseite" || at(de, "item", "locale") != "de" {
			t.Errorf("de home = %v", de["item"])
		}
		germanInner := sharedBlocksOf(t, at(de, "item", "fields", "body"))
		if at(germanInner, 0, "props", "text") != "Fragen? Wir helfen gern." {
			t.Errorf("shared blocks are not German: %v", germanInner)
		}

		about, _ := s.mustLocate("en", "/en/about")
		if sharedBlocksOf(t, at(about, "item", "fields", "body")) == nil {
			t.Error("about lacks the shared blocks")
		}
		if _, r := s.located("en", "/en/about/team"); r.status != 200 {
			t.Errorf("team: %d", r.status)
		}
		if soon, _ := s.located("en", "/en/coming-soon"); at(soon, "located", "resolution", "status") != float64(404) {
			t.Errorf("the never-published page resolves: %v", at(soon, "located", "resolution"))
		}

		red, _ := s.mustLocate("en", "/en/company")
		if at(red, "located", "resolution", "status") != float64(301) || !strings.HasSuffix(at(red, "located", "resolution", "location").(string), "/en/about") {
			t.Errorf("redirect = %v", at(red, "located", "resolution"))
		}

		list := s.deliver("/collections/post/items?channel=en")
		if list.status != 200 || len(list.json(t)["items"].([]any)) != 3 {
			t.Errorf("posts: %d %s", list.status, list.body)
		}
		bySlug := s.deliver("/collections/post/items?channel=en&where[slug][eq]=release-notes")
		if items, _ := bySlug.json(t)["items"].([]any); bySlug.status != 200 || len(items) != 1 || at(items, 0, "fields", "title") != "Release notes" {
			t.Errorf("post by slug: %d %s", bySlug.status, bySlug.body)
		}
		pageBySlug := s.deliver("/collections/page/items?channel=en&where[slug][eq]=about")
		if items, _ := pageBySlug.json(t)["items"].([]any); pageBySlug.status != 200 || len(items) != 1 {
			t.Errorf("page by slug: %d %s", pageBySlug.status, pageBySlug.body)
		}
		set := s.deliver("/singletons/siteSettings?channel=de")
		if set.status != 200 || at(set.json(t), "item", "fields", "footerText") == nil {
			t.Errorf("settings: %d %s", set.status, set.body)
		}
	})
}

func TestIncludeResolvesRefsInBlocks(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := imported(t, dbURL)
		home, _ := s.mustLocate("en", "/en")
		id := at(home, "item", "id").(string)
		r := s.deliver("/items/" + id + "?channel=en&include=1")
		if r.status != 200 {
			t.Fatalf("%d %s", r.status, r.body)
		}
		inc, _ := r.json(t)["included"].(map[string]any)
		titles := map[string]bool{}
		for _, v := range inc {
			titles[at(v, "fields", "title").(string)] = true
		}
		// The hero's call to action and the cards link to pages; the shared block's item is included too.
		for _, want := range []string{"About", "Services", "Team", "Careers", "Get in touch"} {
			if !titles[want] {
				t.Errorf("included lacks %q: %v", want, titles)
			}
		}
	})
}

// TestPublishingSharedBlockPurgesPages edits the shared block and expects the cached pages that use it to
// refresh, with the shared item's tag on their responses.
func TestPublishingSharedBlockPurgesPages(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := imported(t, dbURL)
		list := s.manage("GET", "/collections/sharedBlock/items", nil)
		shared := at(list, "items", 0, "id").(string)

		path := routeURL("en", "/en/about")
		first := s.deliver(path)
		if first.status != 200 {
			t.Fatalf("%d %s", first.status, first.body)
		}
		if tags := first.header.Get("Surrogate-Key"); !strings.Contains(tags, "i:"+shared) {
			t.Fatalf("the page response is not tagged with the shared item: %q", tags)
		}
		if again := s.deliver(path); again.header.Get("Etag") != first.header.Get("Etag") {
			t.Fatal("second response differs")
		}

		blocks := []any{map[string]any{"type": "callout", "props": map[string]any{"text": "Changed text", "tone": "dark"}}}
		s.manage("PATCH", "/items/"+shared, map[string]any{"locale": "en", "fields": map[string]any{"content": blocks}})
		s.manage("POST", "/items/"+shared+"/publish", map[string]any{})

		deadline := time.Now().Add(10 * time.Second)
		for {
			m, _ := s.mustLocate("en", "/en/about")
			inner := sharedBlocksOf(t, at(m, "item", "fields", "body"))
			if at(inner, 0, "props", "text") == "Changed text" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the page still shows the old shared blocks: %v", inner)
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

// TestPreviewInlinesSharedDraft shows a draft page with the draft of its shared block, uncached.
func TestPreviewInlinesSharedDraft(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := imported(t, dbURL)
		shared := at(s.manage("GET", "/collections/sharedBlock/items", nil), "items", 0, "id").(string)
		home, _ := s.mustLocate("en", "/en")
		page := at(home, "item", "id").(string)

		blocks := []any{map[string]any{"type": "callout", "props": map[string]any{"text": "Draft only", "tone": "dark"}}}
		s.manage("PATCH", "/items/"+shared, map[string]any{"locale": "en", "fields": map[string]any{"content": blocks}})
		s.manage("PATCH", "/items/"+page, map[string]any{"locale": "en", "fields": map[string]any{"title": "Home (draft)"}})

		tok := s.manage("POST", "/preview-tokens", map[string]any{"channel": "en", "item": page, "locale": "en"})
		token, _ := tok["token"].(string)
		if token == "" {
			t.Fatalf("token = %v", tok)
		}
		r := s.deliver("/items/" + page + "?channel=en&preview=" + url.QueryEscape(token))
		if r.status != 200 {
			t.Fatalf("%d %s", r.status, r.body)
		}
		item := r.json(t)["item"]
		if at(item, "fields", "title") != "Home (draft)" {
			t.Errorf("title = %v", at(item, "fields", "title"))
		}
		inner := sharedBlocksOf(t, at(item, "fields", "body"))
		if at(inner, 0, "props", "text") != "Draft only" {
			t.Errorf("preview shows %v", inner)
		}
		// Published readers still see the published shared blocks.
		pub := s.deliver("/items/" + page + "?channel=en")
		if !strings.Contains(string(pub.body), "Questions? We are happy to help.") || strings.Contains(string(pub.body), "Draft only") {
			t.Errorf("published response leaks the draft: %s", pub.body)
		}
	})
}

func linksOf(m map[string]any) map[string]any {
	l, _ := at(m, "item", "links").(map[string]any)
	return l
}

// TestPostLinksComeFromTheChannelPattern checks the link pattern for items the pages module does not route: a
// post linked from rich text, a ref prop and a card gets /en/blog/<slug> (the channel prefix added), a routed
// page keeps its route, and a changed slug refreshes the pages that link to the post.
func TestPostLinksComeFromTheChannelPattern(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := imported(t, dbURL)
		home, _ := s.mustLocate("en", "/en")
		homeID := at(home, "item", "id").(string)
		posts := s.deliver("/collections/post/items?channel=en&where[slug][eq]=building-with-blocks").json(t)
		post := at(posts, "items", 0, "id").(string)

		// The rich text of the home page links to the post; the page's own routed links are untouched.
		links := linksOf(home)
		if links[post] != "/en/blog/building-with-blocks" {
			t.Fatalf("post link = %v (links %v)", links[post], links)
		}
		if links["01a0f6b2-fb40-754f-9120-582379229c76"] != "/en/about" {
			t.Errorf("a routed page lost its route: %v", links)
		}
		de, _ := s.mustLocate("de", "/de")
		if linksOf(de)[post] != "/de/blog/mit-bausteinen-bauen" {
			t.Errorf("german post link = %v", linksOf(de)[post])
		}

		// A post's own url, from the delivery API.
		r := s.deliver("/items/" + post + "?channel=en")
		if r.status != 200 || at(r.json(t), "item", "url") != "http://localhost:3000/en/blog/building-with-blocks" {
			t.Errorf("post url: %d %s", r.status, r.body)
		}

		// A ref prop and a card link to a post as well.
		body := at(s.manage("GET", "/items/"+homeID+"?locale=en&draft=true", nil), "fields", "body").([]any)
		body[0].(map[string]any)["props"].(map[string]any)["ctaLink"] = post
		cardsProps := body[1].(map[string]any)["slots"].(map[string]any)["items"].([]any)[0].(map[string]any)["props"].(map[string]any)
		cardsProps["link"] = post
		s.manage("PATCH", "/items/"+homeID, map[string]any{"locale": "en", "fields": map[string]any{"body": body}})
		s.manage("POST", "/items/"+homeID+"/publish", map[string]any{})
		home, _ = s.mustLocate("en", "/en")
		if linksOf(home)[post] != "/en/blog/building-with-blocks" {
			t.Errorf("links after the ref prop change: %v", linksOf(home))
		}

		// A new slug refreshes the cached page.
		s.manage("PATCH", "/items/"+post, map[string]any{"locale": "en", "fields": map[string]any{"slug": "blocks-guide"}})
		s.manage("POST", "/items/"+post+"/publish", map[string]any{})
		deadline := time.Now().Add(10 * time.Second)
		for {
			home, _ = s.mustLocate("en", "/en")
			if linksOf(home)[post] == "/en/blog/blocks-guide" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("the page still links the old slug: %v", linksOf(home))
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
}

func TestPreviewURLsUseThePatternsWithThePrefix(t *testing.T) {
	backends(t, func(t *testing.T, dbURL string) {
		s := imported(t, dbURL)
		post := at(s.deliver("/collections/post/items?channel=en&where[slug][eq]=release-notes").json(t), "items", 0, "id").(string)
		tok := s.manage("POST", "/preview-tokens", map[string]any{"channel": "en", "item": post, "locale": "en"})
		u, _ := tok["url"].(string)
		if !strings.HasPrefix(u, "http://localhost:3000/en/blog/release-notes?preview=") {
			t.Errorf("post preview url = %q", u)
		}
		tok = s.manage("POST", "/preview-tokens", map[string]any{"channel": "de", "item": post, "locale": "de"})
		if u, _ := tok["url"].(string); !strings.HasPrefix(u, "http://localhost:3000/de/blog/neuerungen?preview=") {
			t.Errorf("german post preview url = %q", u)
		}
		// Pages keep their routes; settings fall back to the "*" preview pattern, prefixed like the rest.
		soon := s.manage("GET", "/collections/landing/items?status=draft", nil)
		page := at(soon, "items", 0, "id").(string)
		tok = s.manage("POST", "/preview-tokens", map[string]any{"channel": "en", "item": page})
		if u, _ := tok["url"].(string); !strings.Contains(u, "/en/") {
			t.Errorf("page preview url = %q", u)
		}
	})
}
