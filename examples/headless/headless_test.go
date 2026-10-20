package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/api/client"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

const (
	manageToken = "test-token"
	appToken    = "app-secret-token-0001"
	frontend    = "http://localhost:3000"
)

type server struct {
	t    *testing.T
	host string
}

// start runs `serve` for the duration of the test.
func start(t *testing.T, env string, vars map[string]string) *server {
	t.Helper()
	vars["LISTEN_ADDR"] = "127.0.0.1:0"
	vars["MANAGE_TOKEN"] = manageToken
	vars["PREVIEW_SECRET"] = "test-preview-secret-0123456789abcdef"
	vars["APP_TOKEN"] = appToken
	opts := &config.Options{Dir: "config", Env: env, LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	ctx, cancel := context.WithCancel(context.Background())
	run := func(args ...string) error {
		return hydrogen.Run(ctx, args, configure, hydrogen.RunOptions{Config: opts, Stdout: io.Discard, Stderr: io.Discard})
	}
	if env != config.DevEnv {
		for _, cmd := range []string{"migrate", "content:migrate"} {
			if err := run(cmd); err != nil {
				cancel()
				t.Fatalf("%s: %v", cmd, err)
			}
		}
	}
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- hydrogen.Run(ctx, []string{"serve"}, configure, hydrogen.RunOptions{Config: opts, Stderr: io.Discard, PasswordIterations: 1000,
			Listening: func(a string) { addr <- a }})
	}()
	select {
	case a := <-addr:
		t.Cleanup(func() {
			cancel()
			if err := <-done; err != nil {
				t.Errorf("serve: %v", err)
			}
		})
		return &server{t: t, host: "http://" + a}
	case err := <-done:
		cancel()
		t.Fatalf("serve exited: %v", err)
	case <-time.After(30 * time.Second):
		cancel()
		t.Fatal("serve did not start")
	}
	return nil
}

// do calls the management API and decodes the answer into out; it returns the status.
func (s *server) do(method, path string, body, out any) int {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.host+"/api/manage/v1"+path, r)
	req.Header.Set("Authorization", "Bearer "+manageToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && len(raw) > 0 {
		reflect.ValueOf(out).Elem().SetZero()
		if err := json.Unmarshal(raw, out); err != nil {
			s.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func (s *server) must(status, want int, what string) {
	s.t.Helper()
	if status != want {
		s.t.Fatalf("%s: status %d, want %d", what, status, want)
	}
}

// deliver calls the delivery API; headers are name, value pairs.
func (s *server) deliver(path string, headers ...string) (int, map[string]any) {
	s.t.Helper()
	req, _ := http.NewRequest("GET", s.host+"/api/deliver/v1"+path, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

type minted struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

// mint asks for a preview token of an item; it returns the status and the answer.
func (s *server) mint(body map[string]any) (int, minted) {
	s.t.Helper()
	var m minted
	return s.do("POST", "/preview-tokens", body, &m), m
}

func (s *server) mintOK(ch, item string) minted {
	s.t.Helper()
	status, m := s.mint(map[string]any{"channel": ch, "item": item})
	s.must(status, 200, "mint "+ch)
	return m
}

// create makes an unpublished item and returns its id.
func (s *server) create(typ, locale string, fields map[string]any) string {
	s.t.Helper()
	var v struct {
		ID string `json:"id"`
	}
	s.must(s.do("POST", "/collections/"+typ+"/items", map[string]any{"locale": locale, "fields": fields}, &v), 201, "create "+typ)
	return v.ID
}

func (s *server) publish(id string) {
	s.t.Helper()
	s.must(s.do("POST", "/items/"+id+"/publish", map[string]any{}, nil), 200, "publish")
}

func listed(body map[string]any) []string {
	var titles []string
	items, _ := body["items"].([]any)
	for _, it := range items {
		f, _ := it.(map[string]any)["fields"].(map[string]any)
		titles = append(titles, f["title"].(string))
	}
	return titles
}

func scenario(t *testing.T, s *server) {
	ctx := context.Background()
	hello := s.create("post", "en", map[string]any{"title": "Hello", "slug": "hello-world", "body": "Draft body"})
	other := s.create("post", "en", map[string]any{"title": "Secret", "slug": "secret"})

	t.Run("a draft previews at its frontend URL", func(t *testing.T) {
		m := s.mintOK("web", hello)
		if want := frontend + "/blog/hello-world?preview=" + url.QueryEscape(m.Token); m.URL != want {
			t.Fatalf("url %q, want %q", m.URL, want)
		}
		// The frontend forwards the token as ?preview= or as the header.
		for name, get := range map[string]func(path string) (int, map[string]any){
			"query":  func(p string) (int, map[string]any) { return s.deliver(p + "preview=" + url.QueryEscape(m.Token)) },
			"header": func(p string) (int, map[string]any) { return s.deliver(p+"channel=web", "X-Hydrogen-Preview", m.Token) },
		} {
			st, got := get("/items/" + hello + "?")
			item, _ := got["item"].(map[string]any)
			if f, _ := item["fields"].(map[string]any); st != 200 || f["title"] != "Hello" || f["slug"] != "hello-world" {
				t.Errorf("%s: GET item %d %v", name, st, item)
			}
			st, list := get("/collections/post/items?where[slug][eq]=hello-world&")
			if titles := listed(list); st != 200 || strings.Join(titles, ",") != "Hello" {
				t.Errorf("%s: slug lookup %d %v", name, st, list)
			}
		}
		if st, _ := s.deliver("/items/" + hello + "?channel=web"); st != 404 {
			t.Errorf("draft without a token: %d", st)
		}
		st, list := s.deliver("/collections/post/items?channel=web&where[slug][eq]=hello-world")
		if st != 200 || len(listed(list)) != 0 {
			t.Errorf("listing without a token: %d %v", st, list)
		}
	})

	t.Run("an item token does not reveal other drafts", func(t *testing.T) {
		m := s.mintOK("web", hello)
		q := "preview=" + url.QueryEscape(m.Token)
		if st, _ := s.deliver("/items/" + other + "?" + q); st != 404 {
			t.Errorf("other draft by id: %d", st)
		}
		st, list := s.deliver("/collections/post/items?where[slug][eq]=secret&" + q)
		if st != 200 || len(listed(list)) != 0 {
			t.Errorf("other draft by slug: %d %v", st, list)
		}
		st, list = s.deliver("/collections/post/items?" + q)
		if titles := listed(list); st != 200 || strings.Join(titles, ",") != "Hello" {
			t.Errorf("listing under an item token: %d %v", st, list)
		}
	})

	t.Run("a published item is listed once, as its draft", func(t *testing.T) {
		pub := s.create("post", "en", map[string]any{"title": "Live", "slug": "live"})
		s.publish(pub)
		s.must(s.do("PATCH", "/items/"+pub, map[string]any{"locale": "en", "fields": map[string]any{"title": "Live v2"}}, nil), 200, "edit")
		m := s.mintOK("web", pub)
		_, list := s.deliver("/collections/post/items?where[slug][eq]=live&preview=" + url.QueryEscape(m.Token))
		if titles := listed(list); strings.Join(titles, ",") != "Live v2" {
			t.Errorf("listing: %v", list)
		}
		_, list = s.deliver("/collections/post/items?channel=web&where[slug][eq]=live")
		if titles := listed(list); strings.Join(titles, ",") != "Live" {
			t.Errorf("public listing: %v", list)
		}
	})

	t.Run("singleton and fallback patterns", func(t *testing.T) {
		var settings struct {
			ID string `json:"id"`
		}
		s.must(s.do("PATCH", "/singletons/settings", map[string]any{"locale": "en", "fields": map[string]any{"siteTitle": "Acme"}}, &settings), 200, "settings")
		m := s.mintOK("web", settings.ID)
		if want := frontend + "/?preview=" + url.QueryEscape(m.Token); m.URL != want {
			t.Errorf("settings url %q, want %q", m.URL, want)
		}
		author := s.create("author", "en", map[string]any{"displayName": "Ada"})
		m = s.mintOK("web", author)
		if want := frontend + "/preview/author/" + author + "?preview=" + url.QueryEscape(m.Token); m.URL != want {
			t.Errorf("author url %q, want %q", m.URL, want)
		}
	})

	t.Run("no URL without a slug, an item or a channel that shows it", func(t *testing.T) {
		bare := s.create("post", "en", map[string]any{"title": "No slug"})
		if m := s.mintOK("web", bare); m.Token == "" || m.URL != "" {
			t.Errorf("empty slug: %+v", m)
		}
		status, m := s.mint(map[string]any{"channel": "web"})
		if s.must(status, 200, "mint without item"); m.Token == "" || m.URL != "" {
			t.Errorf("no item: %+v", m)
		}
		if m := s.mintOK("app", s.create("post", "en", map[string]any{"title": "App only", "slug": "app-only"})); m.URL != "" {
			t.Errorf("channel without a pattern: %+v", m)
		}
	})

	t.Run("channel is optional when one channel serves the item", func(t *testing.T) {
		var settings struct {
			ID string `json:"id"`
		}
		s.must(s.do("PATCH", "/singletons/settings", map[string]any{"locale": "en", "fields": map[string]any{"siteTitle": "Acme"}}, &settings), 200, "settings")
		status, m := s.mint(map[string]any{"item": settings.ID})
		if s.must(status, 200, "mint settings without a channel"); m.Token == "" || !strings.HasPrefix(m.URL, frontend+"/?") {
			t.Errorf("settings: %+v", m)
		}
		post := s.create("post", "en", map[string]any{"title": "Both", "slug": "both"})
		for _, body := range []map[string]any{{"item": post}, {}} {
			var e struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if status := s.do("POST", "/preview-tokens", body, &e); status != 400 || !strings.Contains(e.Error.Message, "channel is required (one of: app, web)") {
				t.Errorf("%v: status %d, %+v", body, status, e)
			}
		}
	})

	t.Run("locale", func(t *testing.T) {
		s.must(s.do("PATCH", "/items/"+hello, map[string]any{"locale": "de", "fields": map[string]any{"title": "Hallo", "slug": "hallo-welt"}}, nil), 200, "translate")
		status, m := s.mint(map[string]any{"channel": "web", "item": hello, "locale": "de"})
		s.must(status, 200, "mint de")
		if want := frontend + "/blog/hallo-welt?preview=" + url.QueryEscape(m.Token); m.URL != want {
			t.Errorf("de url %q, want %q", m.URL, want)
		}
		if m := s.mintOK("web", hello); !strings.HasPrefix(m.URL, frontend+"/blog/hello-world?") {
			t.Errorf("main locale: %q", m.URL)
		}
		for _, c := range []map[string]any{
			{"channel": "web", "item": hello, "locale": "fr"},
			{"channel": "app", "item": hello, "locale": "de"},
		} {
			if status, _ := s.mint(c); status != 400 {
				t.Errorf("%v: status %d, want 400", c, status)
			}
		}
		onlyDE := s.create("post", "de", map[string]any{"title": "Nur deutsch", "slug": "nur-deutsch"})
		if m := s.mintOK("web", onlyDE); !strings.HasPrefix(m.URL, frontend+"/blog/nur-deutsch?") {
			t.Errorf("item whose main locale is de: %q", m.URL)
		}
	})

	t.Run("app channel", func(t *testing.T) {
		s.publish(hello)
		app := client.NewDelivery(s.host, client.WithToken(appToken))
		res, err := app.List(ctx, "post", &client.ListOptions{Sort: []client.Sort{client.Asc("name")}})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, raw := range res.Items {
			var it struct {
				Fields struct{ Title string }
			}
			if err := json.Unmarshal(raw, &it); err != nil {
				t.Fatal(err)
			}
			got = append(got, it.Fields.Title)
		}
		if strings.Join(got, ",") != "Hello,Live" {
			t.Errorf("app channel lists %v", got)
		}
		if _, err := app.Item(ctx, other, nil); !client.IsNotFound(err) {
			t.Errorf("unpublished post on the app channel: %v", err)
		}

		m := client.NewManage(s.host, manageToken)
		tok, err := m.PreviewToken(ctx, client.PreviewTokenInput{Channel: "web", Item: other, Locale: "en"})
		if err != nil || tok.URL != frontend+"/blog/secret?preview="+url.QueryEscape(tok.Token) {
			t.Errorf("client preview token %+v %v", tok, err)
		}
		if _, err := app.With(client.WithPreview(tok.Token)).Item(ctx, other, nil); err == nil {
			t.Error("a web preview token worked on the app channel")
		}
	})
}

func TestHeadlessSQLite(t *testing.T) {
	scenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}))
}

func TestHeadlessPostgres(t *testing.T) {
	scenario(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}))
}
