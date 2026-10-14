package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

const token = "test-token"

// server runs `serve` for the duration of the test and returns an API client.
type server struct {
	t    *testing.T
	base string
}

func start(t *testing.T, env string, vars map[string]string, configure func(*hydrogen.Builder)) *server {
	t.Helper()
	vars["LISTEN_ADDR"] = "127.0.0.1:0"
	vars["MANAGE_TOKEN"] = token
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
		done <- hydrogen.Run(ctx, []string{"serve"}, configure, hydrogen.RunOptions{Config: opts, Stderr: io.Discard,
			Listening: func(a string) { addr <- a }})
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	select {
	case a := <-addr:
		return &server{t: t, base: "http://" + a + "/api/manage/v1"}
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("serve did not start")
	}
	return nil
}

// do sends a request and decodes the JSON response into out (if non-nil); it returns the status.
func (s *server) do(method, path string, body any, out any) int {
	s.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, s.base+path, r)
	req.Header.Set("Authorization", "Bearer "+token)
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
		reflect.ValueOf(out).Elem().SetZero() // Unmarshal would merge into a reused map
		if err := json.Unmarshal(raw, out); err != nil {
			s.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

type view struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"`
	Version    int            `json:"version"`
	Name       string         `json:"name"`
	Fields     map[string]any `json:"fields"`
	DraftParts []string       `json:"draftParts"`
}

type apiError struct {
	Error struct {
		Code   string `json:"code"`
		Fields []struct {
			Path, Code string
		} `json:"fields"`
	} `json:"error"`
}

func (s *server) must(status, want int, what string) {
	s.t.Helper()
	if status != want {
		s.t.Fatalf("%s: status %d, want %d", what, status, want)
	}
}

func lifecycle(t *testing.T, s *server) {
	var person view
	s.must(s.do("POST", "/collections/person/items", map[string]any{"locale": "en",
		"fields": map[string]any{"firstName": "Ada", "lastName": "Lovelace"}}, &person), 201, "create person")
	s.must(s.do("POST", "/items/"+person.ID+"/publish", map[string]any{}, &person), 200, "publish person")

	// Invalid draft write: 422 with field paths.
	var bad apiError
	s.must(s.do("POST", "/collections/article/items", map[string]any{"locale": "en", "fields": map[string]any{
		"title": strings.Repeat("x", 121), "rating": 9, "tags": []any{"ok", 3}}}, &bad), 422, "invalid article")
	paths := map[string]string{}
	for _, f := range bad.Error.Fields {
		paths[f.Path] = f.Code
	}
	if paths["title"] == "" || paths["rating"] == "" || paths["tags[1]"] == "" {
		t.Fatalf("field errors: %+v", bad.Error.Fields)
	}

	// Draft without required title is fine; publishing it is not.
	var art view
	s.must(s.do("POST", "/collections/article/items", map[string]any{"locale": "en", "fields": map[string]any{
		"summary": "One two three.", "author": person.ID, "tags": []any{"go", "cms"}, "rating": 4,
		"publishDate": "2026-10-14T10:00:00+02:00"}}, &art), 201, "create article")
	if art.Fields["kind"] != "news" || art.Fields["featured"] != false {
		t.Fatalf("defaults: %v", art.Fields)
	}
	if art.Fields["readingMinutes"] != float64(1) {
		t.Fatalf("project hook did not run: %v", art.Fields["readingMinutes"])
	}
	if art.Fields["publishDate"] != "2026-10-14T08:00:00.000Z" {
		t.Fatalf("datetime not normalised: %v", art.Fields["publishDate"])
	}
	s.must(s.do("POST", "/items/"+art.ID+"/publish", map[string]any{}, &bad), 422, "publish without title")

	s.must(s.do("PATCH", "/items/"+art.ID, map[string]any{"locale": "en", "fields": map[string]any{
		"title": "Hello Hydrogen", "rating": nil}}, &art), 200, "patch article")
	if art.Fields["slug"] != "hello-hydrogen" || art.Fields["rating"] != nil {
		t.Fatalf("slug from title / null removal: %v", art.Fields)
	}
	s.must(s.do("POST", "/items/"+art.ID+"/publish", map[string]any{}, &art), 200, "publish article")
	if art.Status != "published" || art.Version != 1 || art.Name != "Hello Hydrogen" {
		t.Fatalf("published view: %+v", art)
	}

	// A German translation drafted and published on its own.
	s.must(s.do("PATCH", "/items/"+art.ID, map[string]any{"locale": "de", "fields": map[string]any{"title": "Hallo Hydrogen"}}, &art), 200, "de draft")
	s.must(s.do("POST", "/items/"+art.ID+"/publish", map[string]any{"locales": []string{"de"}}, &art), 200, "publish de")

	// Listing: filters, search, sort.
	var list struct {
		Items []view `json:"items"`
		Total int    `json:"total"`
	}
	s.must(s.do("GET", "/collections/article/items?locale=en&where[tags][contains]=go&where[kind]=news&q=hello&sort=-publishDate", nil, &list), 200, "list")
	if list.Total != 1 || list.Items[0].ID != art.ID {
		t.Fatalf("list: %+v", list)
	}

	// Delete policy: the article blocks deleting its author.
	s.must(s.do("DELETE", "/items/"+person.ID, nil, &bad), 409, "delete referenced person")
	if bad.Error.Code != "referenced" {
		t.Fatalf("error code %q", bad.Error.Code)
	}

	// Singleton with a nullify reference to a page; deleting the page clears it.
	var page, settings view
	s.must(s.do("POST", "/collections/page/items", map[string]any{"locale": "en", "fields": map[string]any{
		"title": "About", "seo": map[string]any{"metaTitle": "About us"}}}, &page), 201, "create page (seo from module)")
	s.must(s.do("POST", "/items/"+page.ID+"/publish", map[string]any{}, &page), 200, "publish page")
	s.must(s.do("PATCH", "/singletons/settings", map[string]any{"locale": "en", "fields": map[string]any{
		"siteName": "Acme", "footerLinks": []any{page.ID}}}, &settings), 200, "settings")
	s.must(s.do("POST", "/singletons/settings/publish", map[string]any{}, &settings), 200, "publish settings")
	s.must(s.do("DELETE", "/items/"+page.ID, nil, nil), 204, "delete page")
	s.must(s.do("GET", "/singletons/settings?locale=en", nil, &settings), 200, "settings after delete")
	if links, _ := settings.Fields["footerLinks"].([]any); len(links) != 0 {
		t.Fatalf("nullify: %v", settings.Fields["footerLinks"])
	}

	// OpenAPI includes module-added fields.
	var doc map[string]any
	s.must(s.do("GET", "/openapi.json", nil, &doc), 200, "openapi")
	if b, _ := json.Marshal(doc); !strings.Contains(string(b), "metaTitle") {
		t.Fatal("openapi lacks the seo group added by the module")
	}
}

func TestLifecycleSQLite(t *testing.T) {
	lifecycle(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configure))
}

func TestLifecyclePostgres(t *testing.T) {
	lifecycle(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configure))
}

// configureV1 is the project before the person schema changed: no migration yet.
func configureV1(h *hydrogen.Builder) {
	h.Use(sqliteModule(), postgresModule())
}

func TestContentMigration(t *testing.T) {
	db := "sqlite:" + filepath.Join(t.TempDir(), "h.db")
	func() {
		s := start(t, config.DevEnv, map[string]string{"DATABASE_URL": db, "SCHEMA_DIR": "schema_v1"}, configureV1)
		var p view
		s.must(s.do("POST", "/collections/person/items", map[string]any{"locale": "en",
			"fields": map[string]any{"fullName": "Grace Hopper"}}, &p), 201, "v1 person")
		s.must(s.do("POST", "/items/"+p.ID+"/publish", map[string]any{}, nil), 200, "v1 publish")
	}()
	t.Run("breaking change without migration fails", func(t *testing.T) {
		vars := map[string]string{"DATABASE_URL": db, "MANAGE_TOKEN": token}
		opts := &config.Options{Dir: "config", LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
		err := hydrogen.Run(context.Background(), []string{"serve"}, configureV1, hydrogen.RunOptions{Config: opts, Stderr: io.Discard})
		if err == nil || !strings.Contains(err.Error(), "person.fullName") {
			t.Fatalf("got %v", err)
		}
	})
	s := start(t, config.DevEnv, map[string]string{"DATABASE_URL": db}, configure)
	var list struct{ Items []view }
	s.must(s.do("GET", "/collections/person/items?locale=en", nil, &list), 200, "list persons")
	if len(list.Items) != 1 || list.Items[0].Name != "Grace Hopper" {
		t.Fatalf("migrated: %+v", list.Items)
	}
	var p view
	s.must(s.do("GET", "/items/"+list.Items[0].ID+"?locale=en", nil, &p), 200, "get person")
	if p.Fields["firstName"] != "Grace" || p.Fields["lastName"] != "Hopper" || p.Fields["fullName"] != nil || p.Version != 2 {
		t.Fatalf("migrated fields: %+v", p)
	}
}

// TestGeneratedCodeIsCurrent regenerates into a temp dir and compares with gen/, then type-checks the
// TypeScript output.
func TestGeneratedCodeIsCurrent(t *testing.T) {
	dir := t.TempDir()
	opts := &config.Options{Dir: "config", LookupEnv: func(string) (string, bool) { return "", false }}
	args := []string{"generate", "-go", filepath.Join(dir, "content"), "-ts", filepath.Join(dir, "types.ts")}
	if err := hydrogen.Run(context.Background(), args, configure, hydrogen.RunOptions{Config: opts, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "content", "*.go"))
	files = append(files, filepath.Join(dir, "types.ts"))
	for _, f := range files {
		rel := filepath.Base(f)
		committed := filepath.Join("gen", "content", rel)
		if rel == "types.ts" {
			committed = filepath.Join("gen", rel)
		}
		want, _ := os.ReadFile(f)
		got, err := os.ReadFile(committed)
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is stale; run hydrogen generate", committed)
		}
	}
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx not available")
	}
	cmd := exec.Command("npx", "--no-install", "tsc", "--noEmit", "--strict", "--target", "es2022", filepath.Join("gen", "types.ts"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc: %v\n%s", err, out)
	}
}
