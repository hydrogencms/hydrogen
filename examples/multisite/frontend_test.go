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

// checkFrontend runs `check-frontend` against a database with the given templates and returns its output.
func checkFrontend(t *testing.T, db string, templates []string, args ...string) (string, error) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "templates.json")
	b, _ := json.Marshal(map[string]any{"templates": templates})
	if err := os.WriteFile(file, b, 0o600); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{"DATABASE_URL": db, "MANAGE_TOKEN": token, "IOS_TOKEN": iosToken}
	opts := &config.Options{Dir: "config", Env: config.DevEnv, LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	var out bytes.Buffer
	err := hydrogen.Run(context.Background(), append([]string{"check-frontend", "-templates", file}, args...), configureWithProbe,
		hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: &out})
	return out.String(), err
}

func TestCheckFrontend(t *testing.T) {
	db := "sqlite:" + filepath.Join(t.TempDir(), "h.db")
	all := []string{"page", "landing", "article", "product"}

	t.Run("complete", func(t *testing.T) {
		out, err := checkFrontend(t, db, all)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		for _, want := range []string{"channel corporate_en", "channel careers_en", "channel ios_app", "channel corporate_sg_en",
			"ok       page type landing", "ok       collection article", "ok       collection product", "frontend ok"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in\n%s", want, out)
			}
		}
	})

	t.Run("missing page type fails", func(t *testing.T) {
		out, err := checkFrontend(t, db, []string{"page", "article", "ghost"})
		if err == nil || !strings.Contains(err.Error(), "page type(s) without a template") {
			t.Fatalf("err %v\n%s", err, out)
		}
		for _, want := range []string{"MISSING  page type landing: no template", `warning: template "ghost" matches no content type`,
			"warning  collection product: no template", "ok       page type page"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in\n%s", want, out)
			}
		}
		if strings.Contains(out, "frontend ok") {
			t.Error("claims ok")
		}
	})

	t.Run("one channel", func(t *testing.T) {
		out, err := checkFrontend(t, db, []string{"product"}, "-channel", "ios_app")
		if err != nil || strings.Contains(out, "corporate") || !strings.Contains(out, "ok       collection product") {
			t.Errorf("ios_app: %v\n%s", err, out)
		}
		out, err = checkFrontend(t, db, nil, "-channel", "careers_en")
		if err == nil || !strings.Contains(out, "MISSING  page type page") || !strings.Contains(out, "MISSING  page type landing") ||
			strings.Contains(out, "article") {
			t.Errorf("careers_en: %v\n%s", err, out)
		}
		if _, err := checkFrontend(t, db, all, "-channel", "nope"); err == nil || !strings.Contains(err.Error(), "unknown channel") {
			t.Errorf("unknown channel: %v", err)
		}
	})

	t.Run("bad input", func(t *testing.T) {
		if _, err := checkFrontend(t, db, all, "extra"); err == nil {
			t.Error("stray argument accepted")
		}
		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte(`{"template": ["page"]}`), 0o600)
		opts := &config.Options{Dir: "config", Env: config.DevEnv, LookupEnv: func(k string) (string, bool) { return map[string]string{"DATABASE_URL": db}[k], k == "DATABASE_URL" }}
		var out bytes.Buffer
		err := hydrogen.Run(context.Background(), []string{"check-frontend", "-templates", bad}, configureWithProbe,
			hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: &out})
		if err == nil || !strings.Contains(err.Error(), `want {"templates"`) {
			t.Errorf("unknown key: %v", err)
		}
		err = hydrogen.Run(context.Background(), []string{"check-frontend"}, configureWithProbe, hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: &out})
		if err == nil || !strings.Contains(err.Error(), "usage") {
			t.Errorf("no flags: %v", err)
		}
	})
}

func TestCheckFrontendMounts(t *testing.T) {
	db := "sqlite:" + filepath.Join(t.TempDir(), "h.db")
	s := start(t, config.DevEnv, map[string]string{"DATABASE_URL": db}, configureWithProbe)
	buildSites(s) // mounts article under the corporate site's news

	// product is allowed on corporate channels but mounted nowhere: no warning with -db.
	out, err := checkFrontend(t, db, []string{"page", "landing"}, "-db")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if n := strings.Count(out, "warning  mounted collection article: no template"); n != 4 {
		t.Errorf("article warned %d times, want 4 corporate channels:\n%s", n, out)
	}
	if strings.Contains(out, "product") {
		t.Errorf("product is not mounted:\n%s", out)
	}
	careers := out[strings.Index(out, "channel careers_en"):]
	if end := strings.Index(careers[1:], "channel "); end > 0 {
		careers = careers[:end+1]
	}
	if strings.Contains(careers, "article") {
		t.Errorf("careers has no mounts:\n%s", careers)
	}

	out, err = checkFrontend(t, db, []string{"page", "landing", "article"}, "-db")
	if err != nil || strings.Contains(out, "warning") || !strings.Contains(out, "ok       mounted collection article") {
		t.Errorf("with the template: %v\n%s", err, out)
	}
}
