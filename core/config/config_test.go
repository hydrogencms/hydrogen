package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type httpConf struct {
	Addr    string        `yaml:"addr"`
	Timeout time.Duration `yaml:"timeout"`
}

type dbConf struct {
	URL string `yaml:"url" config:"secret"`
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
}

func TestLoadMergeSubstituteOverride(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.yaml", `
http:
  addr: ":8080"
  timeout: 5s
database:
  url: ${env:DATABASE_URL:-sqlite:dev.db}
`)
	write(t, dir, "env/prod.yaml", "http:\n  timeout: 30s\n")

	c, err := Load(Options{Dir: dir, Env: "prod",
		LookupEnv: env(map[string]string{"DATABASE_URL": "postgres://x"}),
		Environ:   []string{"HYDROGEN__HTTP__ADDR=:9000"}})
	if err != nil {
		t.Fatal(err)
	}
	var h httpConf
	if err := c.Decode("http", &h); err != nil {
		t.Fatal(err)
	}
	if h.Addr != ":9000" || h.Timeout != 30*time.Second {
		t.Fatalf("got %+v", h)
	}
	var d dbConf
	if err := c.Decode("database", &d); err != nil {
		t.Fatal(err)
	}
	if d.URL != "postgres://x" {
		t.Fatalf("url %q", d.URL)
	}
	if err := c.Unused(); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name, body, env, path string
		target                any
		vars                  map[string]string
		want                  string
	}{
		{name: "unknown key", body: "http:\n  adr: x\n", path: "http", target: &httpConf{},
			want: `app.yaml:2: unknown key "adr" in http (known: addr, timeout)`},
		{name: "bad duration", body: "http:\n  timeout: soon\n", path: "http", target: &httpConf{},
			want: `app.yaml:2: http.timeout: "soon" is not a duration`},
		{name: "secret literal", body: "database:\n  url: postgres://x\n", path: "database", target: &dbConf{},
			want: "database.url is a secret and must come from ${env:NAME}"},
		{name: "secret default outside dev", body: "database:\n  url: ${env:DB:-sqlite:x}\n", env: "prod",
			path: "database", target: &dbConf{}, want: "environment variable DB must be set outside dev"},
		{name: "unset without default", body: "http:\n  addr: ${env:ADDR}\n", path: "http", target: &httpConf{},
			want: "environment variable ADDR is not set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "app.yaml", tc.body)
			c, err := Load(Options{Dir: dir, Env: tc.env, LookupEnv: env(tc.vars)})
			if err != nil {
				t.Fatal(err)
			}
			err = c.Decode(tc.path, tc.target)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestSecretDefaultInDev(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.yaml", "database:\n  url: ${env:DB:-sqlite:x.db}\n")
	c, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var d dbConf
	if err := c.Decode("database", &d); err != nil || d.URL != "sqlite:x.db" {
		t.Fatalf("%v %q", err, d.URL)
	}
}

func TestUnused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.yaml", "htp:\n  addr: x\nmodules:\n  pages: {}\n  seo: {}\n")
	c, err := Load(Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var pages struct{}
	if err := c.Decode("modules.pages", &pages); err != nil {
		t.Fatal(err)
	}
	err = c.Unused()
	if err == nil || !strings.Contains(err.Error(), `unknown key "htp"`) || !strings.Contains(err.Error(), `unknown module "seo"`) {
		t.Fatalf("Unused() = %v", err)
	}
}

func TestMissingFilesAreFine(t *testing.T) {
	c, err := Load(Options{Dir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	h := httpConf{Addr: ":8080"}
	if err := c.Decode("http", &h); err != nil || h.Addr != ":8080" {
		t.Fatalf("%v %+v", err, h)
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "512": 512, "64KB": 64 << 10, "50MB": 50 << 20, "50 mb": 50 << 20, "2GiB": 2 << 30, "1B": 1} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MB", "-1", "1.5MB", "5TB", "99999999999GB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) accepted", in)
		}
	}
}

func TestFlowMappingSubstitutionHint(t *testing.T) {
	for name, c := range map[string]struct {
		yaml string
		hint bool
	}{
		"unquoted in braces":   {"web:\n  match: { host: ${env:HOST:-localhost} }\n", true},
		"default with a colon": {"web:\n  site: main\n  frontend: { url: ${env:SITE_URL:-http://localhost:4321} }\n", true},
		"quoted":               {"web:\n  match: { host: \"${env:HOST:-localhost}\" }\n", false},
		"block style":          {"web:\n  match:\n    host: ${env:HOST:-localhost}\n", false},
		"other error":          {"web:\n  a: [1, 2\n", false},
	} {
		dir := t.TempDir()
		write(t, dir, "app.yaml", c.yaml)
		_, err := Load(Options{Dir: dir})
		got := err != nil && strings.Contains(err.Error(), "quote ${...} values inside { } or [ ]")
		if got != c.hint {
			t.Errorf("%s: %v", name, err)
		}
	}
}
