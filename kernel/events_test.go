package kernel

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/config"
)

type evMod struct {
	name string
	reg  func(r *Registry) error
}

func (m evMod) Name() string               { return m.name }
func (m evMod) Register(r *Registry) error { return m.reg(r) }

func appConfig(t *testing.T, env, appYAML string, vars map[string]string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "schema/s.yaml"), []byte("types:\n  article:\n    fields:\n      title: { type: string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n" + appYAML
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: dir, Env: env, LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

var noop = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

func dbMod() Module { return chanMod{name: "db", db: true} }

func TestEndpointRegistration(t *testing.T) {
	reg := newRegistry()
	reg.current = "a"
	if err := reg.Endpoint("GET", "/ping", Public, noop); err != nil {
		t.Fatal(err)
	}
	reg.current = "b"
	for _, tc := range []struct {
		method, path string
		auth         Auth
		h            http.HandlerFunc
		want         string
	}{
		{"GET", "/ping", ManageToken, noop, "registered by both a and b"},
		{"FETCH", "/x", Public, noop, "method must be"},
		{"GET", "x", Public, noop, "path must start"},
		{"GET", "/a b", Public, noop, "path must start"},
		{"GET", "/a//b", Public, noop, "path must start"},
		{"GET", "/x", 0, noop, "unknown auth"},
		{"GET", "/x", Public, nil, "no handler"},
	} {
		if err := reg.Endpoint(tc.method, tc.path, tc.auth, tc.h); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: %v, want %q", tc.method, tc.path, err, tc.want)
		}
	}
	if err := reg.Endpoint("POST", "/ping", Public, noop); err != nil {
		t.Errorf("other method: %v", err)
	}
}

func TestJobRegistration(t *testing.T) {
	reg := newRegistry()
	fn := func(context.Context, *Services) error { return nil }
	reg.current = "a"
	if err := reg.Job("sync", "@every 5m", fn); err != nil {
		t.Fatal(err)
	}
	reg.current = "b"
	for _, tc := range []struct {
		name, sched string
		fn          func(context.Context, *Services) error
		want        string
	}{
		{"sync", "@daily", fn, "registered by both a and b"},
		{"Bad", "@daily", fn, "lowercase"},
		{"x", "61 * * * *", fn, "outside 0-59"},
		{"x", "@daily", nil, "no function"},
	} {
		if err := reg.Job(tc.name, tc.sched, tc.fn); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s %s: %v, want %q", tc.name, tc.sched, err, tc.want)
		}
	}
}

func TestCheckWebhooksAndTokens(t *testing.T) {
	hook := "webhooks:\n  revalidate:\n    url: %URL%\n    events: [item.*]\n    secret: ${env:WH_SECRET:-dev-secret}\n"
	prod := map[string]string{"MANAGE": "t", "WH_SECRET": "s", "DB": "sqlite:x.db"}
	prodHead := "database:\n  url: ${env:DB}\nmanage:\n  token: ${env:MANAGE}\n"
	for _, tc := range []struct {
		name, env, yaml string
		mods            []Module
		want            string
	}{
		{"dev http ok", "dev", strings.ReplaceAll(hook, "%URL%", "http://localhost:9/h"), nil, ""},
		{"prod https ok", "prod", prodHead + strings.ReplaceAll(hook, "%URL%", "https://x.test/h"), nil, ""},
		{"prod http", "prod", prodHead + strings.ReplaceAll(hook, "%URL%", "http://x.test/h"), nil, "https is required"},
		{"no events", "dev", "webhooks:\n  w:\n    url: https://x.test\n    secret: ${env:S:-x}\n", nil, "at least one topic"},
		{"literal secret", "dev", "webhooks:\n  w:\n    url: https://x.test\n    events: [\"*\"]\n    secret: abc\n", nil, "must come from ${env:NAME}"},
		{"unknown key", "dev", "webhooks:\n  w:\n    urls: https://x.test\n", nil, `unknown key "urls"`},
		{"manage endpoint needs token", "prod", "database:\n  url: ${env:DB}\n", []Module{evMod{"ep", func(r *Registry) error {
			return r.Endpoint("GET", "/x", ManageToken, noop)
		}}}, "manage.token is required"},
		{"public endpoint ok", "prod", "database:\n  url: ${env:DB}\n", []Module{evMod{"ep", func(r *Registry) error {
			return r.Endpoint("GET", "/x", Public, noop)
		}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := appConfig(t, tc.env, tc.yaml, prod)
			mods := tc.mods
			if tc.name == "manage endpoint needs token" || tc.name == "public endpoint ok" {
				// no content types: the token is needed only by the endpoint
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(tc.yaml+"content: { schema_dir: "+filepath.Join(dir, "none")+" }\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				var err error
				if cfg, err = config.Load(config.Options{Dir: dir, Env: tc.env, LookupEnv: func(k string) (string, bool) { v, ok := prod[k]; return v, ok }}); err != nil {
					t.Fatal(err)
				}
			}
			err := Check(cfg, append(mods, dbMod())...)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Check: %v, want %q", err, tc.want)
			}
		})
	}
}

func TestTagMapperRegistered(t *testing.T) {
	reg := newRegistry()
	reg.TagMapper(func(string, json.RawMessage) []string { return []string{cachetag.Nav("main")} })
	if len(reg.mappers) != 1 {
		t.Fatal("mapper not stored")
	}
}
