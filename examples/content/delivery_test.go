package main

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

func (s *server) deliver(path string) (int, http.Header, map[string]any) {
	s.t.Helper()
	resp, err := http.Get(strings.TrimSuffix(s.base, "/manage/v1") + "/deliver/v1" + path)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		s.t.Fatalf("GET %s: decode %q: %v", path, b, err)
	}
	return resp.StatusCode, resp.Header, m
}

func dget(m map[string]any, path ...string) any {
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

// deliveryFlow serves the pages fixture through the delivery API: a project with no channels has one default
// channel with every language and collection and no web matcher.
func deliveryFlow(t *testing.T, s *server) {
	f := buildSite(t, s)

	// The navigation: pages by position (newest first), the mount under its label, links resolved.
	status, h, tree := s.deliver("/tree?depth=3")
	if status != 200 || tree["site"] != "main" {
		t.Fatalf("tree: %d %v", status, tree)
	}
	var names []string
	var about map[string]any
	for _, n := range tree["nodes"].([]any) {
		m := n.(map[string]any)
		names = append(names, m["name"].(string))
		if m["item"] == f.about.Item {
			about = m
		}
	}
	if strings.Join(names, ",") != "News,About,About us" {
		t.Errorf("nodes = %v", names)
	}
	if about == nil || about["path"] != "/about" {
		t.Fatalf("about: %v", tree)
	}
	if kids, _ := about["nodes"].([]any); len(kids) != 1 || kids[0].(map[string]any)["name"] != "Team" {
		t.Errorf("depth 3 lists the team: %v", about["nodes"])
	}
	if sk := h.Get("Surrogate-Key"); !strings.Contains(sk, "nav:main") || !strings.Contains(sk, "r:main") {
		t.Errorf("tags %q", sk)
	}
	if st, _, _ := s.deliver("/tree?site=other"); st != 404 {
		t.Errorf("unknown site: %d", st)
	}
	if st, _, _ := s.deliver("/tree?depth=99"); st != 400 {
		t.Errorf("depth: %d", st)
	}

	// The channel has no matcher, so there is no URL to locate; items get path-only urls.
	if st, _, _ := s.deliver("/route?url=/about"); st != 404 {
		t.Errorf("route without a web channel: %d", st)
	}
	status, _, item := s.deliver("/items/" + f.about.Item)
	if status != 200 || dget(item, "item", "fields", "title") != "About" {
		t.Fatalf("about item: %d %v", status, item)
	}
	if dget(item, "item", "url") != "/about" {
		t.Errorf("a channel without a matcher gets path-only urls: %v", item)
	}

	// A singleton that links to pages: the channel has no matcher, so there is no web channel to link into and no links.
	var settings view
	s.must(s.do("PATCH", "/singletons/settings", map[string]any{"locale": "en",
		"fields": map[string]any{"siteName": "Hydrogen", "footerLinks": []string{f.about.Item}}}, &settings), 200, "settings")
	s.must(s.do("POST", "/singletons/settings/publish", map[string]any{}, nil), 200, "publish settings")
	status, _, got := s.deliver("/singletons/settings?include=1")
	if status != 200 || dget(got, "item", "fields", "siteName") != "Hydrogen" || dget(got, "included", f.about.Item) == nil {
		t.Fatalf("settings: %d %v", status, got)
	}
	if dget(got, "item", "links") != nil || dget(got, "included", f.about.Item, "url") != "/about" {
		t.Errorf("links: %v", got)
	}
	status, _, info := s.deliver("/openapi.json")
	if status != 200 || dget(info, "paths", "/route", "get") == nil || dget(info, "paths", "/tree", "get") == nil {
		t.Errorf("openapi: %d", status)
	}
}

func TestDeliverySQLite(t *testing.T) {
	deliveryFlow(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configure))
}

func TestDeliveryPostgres(t *testing.T) {
	deliveryFlow(t, start(t, "prod", map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configure))
}
