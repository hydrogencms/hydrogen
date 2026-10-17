package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/config"
)

func checkFrontend(t *testing.T, db string, templates []string, args ...string) (string, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "templates.json")
	b, _ := json.Marshal(map[string]any{"templates": templates})
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"DATABASE_URL": db, "MANAGE_TOKEN": token}
	opts := &config.Options{Dir: "config", Env: config.DevEnv, LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	var out bytes.Buffer
	err := hydrogen.Run(context.Background(), append([]string{"check-frontend", "-templates", file}, args...), configure,
		hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: &out})
	return out.String(), err
}

func TestCheckFrontend(t *testing.T) {
	db := "sqlite:" + filepath.Join(t.TempDir(), "h.db")

	// Without -db every non-page collection the channel allows is a warning; singletons and redirects are not.
	out, err := checkFrontend(t, db, []string{"page", "landing"})
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"channel default", "ok       page type page", "warning  collection article: no template",
		"warning  collection person: no template", "warning  collection image: no template", "frontend ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "settings") || strings.Contains(out, "redirect") {
		t.Errorf("singleton or redirect reported:\n%s", out)
	}

	out, err = checkFrontend(t, db, []string{"landing", "article", "person", "image", "settings"})
	if err == nil || !strings.Contains(err.Error(), "1 page type(s) without a template") ||
		!strings.Contains(out, "MISSING  page type page: no template") {
		t.Errorf("no page template: %v\n%s", err, out)
	}
	if strings.Contains(out, "warning") {
		t.Errorf("settings is a type, so no unknown-template warning:\n%s", out)
	}
	out, _ = checkFrontend(t, db, []string{"page", "landing", "settingz"})
	if !strings.Contains(out, `warning: template "settingz" matches no content type`) {
		t.Errorf("unknown template:\n%s", out)
	}
}

func TestCheckFrontendMounts(t *testing.T) {
	db := "sqlite:" + filepath.Join(t.TempDir(), "h.db")
	s := start(t, config.DevEnv, map[string]string{"DATABASE_URL": db}, configure)
	buildSite(t, s) // mounts article under the home page

	out, err := checkFrontend(t, db, []string{"page", "landing"}, "-db")
	if err != nil || !strings.Contains(out, "warning  mounted collection article: no template") || strings.Contains(out, "person") {
		t.Errorf("mounted article lacks a template: %v\n%s", err, out)
	}
	out, err = checkFrontend(t, db, []string{"page", "landing", "article"}, "-db")
	if err != nil || strings.Contains(out, "warning") || !strings.Contains(out, "ok       mounted collection article") {
		t.Errorf("article has a template: %v\n%s", err, out)
	}
}
