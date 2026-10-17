package tests

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/features/media"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const mediaKernelSchema = `
types:
  image:
    upload: { accept: [image/*] }
    fields:
      alt: { type: string }
  article:
    name: "{title}"
    fields:
      title: { type: string, required: true }
      hero:  { type: asset, to: [image] }
`

func mediaConfig(t *testing.T, schemaYAML, appYAML string, env map[string]string) *config.Config {
	t.Helper()
	dir := writeFiles(t, map[string]string{"schema/s.yaml": schemaYAML})
	app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n" + strings.ReplaceAll(appYAML, "MEDIADIR", filepath.Join(dir, "media"))
	if err := os.MkdirAll(filepath.Join(dir, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
		LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestMediaKernelEndToEnd(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		cfg := mediaConfig(t, mediaKernelSchema, `
manage: { token: "${env:TOKEN}" }
media:
  max_upload: 1MB
  storage: { driver: local, dir: MEDIADIR }
`, map[string]string{"TOKEN": "kernel-token-0123456789"})
		a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard, PasswordIterations: 1000}, sqlite.Module())
		if err != nil {
			t.Fatal(err)
		}
		if a.Services().Media == nil || a.Services().Media.MaxUpload() != 1<<20 {
			t.Fatalf("media service: %+v", a.Services().Media)
		}
		if err := a.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := a.Services().Content.EnsureSchemaRev(ctx); err != nil {
			t.Fatal(err)
		}
		h, err := a.Handler()
		if err != nil {
			t.Fatal(err)
		}
		call := func(method, path, ct string, body io.Reader) *httptest.ResponseRecorder {
			req := httptest.NewRequest(method, path, body)
			req.Header.Set("Authorization", "Bearer kernel-token-0123456789")
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			return rec
		}

		png := string(mtPNG(16, 9, 5))
		ct, body := multipartBody(t, [3]string{"type", "", "image"}, [3]string{"file", "hero.png", png})
		rec := call("POST", "/api/manage/v1/uploads", ct, body)
		if rec.Code != 201 {
			t.Fatalf("upload: %d %s", rec.Code, rec.Body)
		}
		var file map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &file)

		item := `{"fields":{"file":` + rec.Body.String() + `,"alt":"A hero"}}`
		rec = call("POST", "/api/manage/v1/collections/image/items", "application/json", strings.NewReader(item))
		if rec.Code != 201 {
			t.Fatalf("create image: %d %s", rec.Code, rec.Body)
		}
		var view struct{ ID string }
		_ = json.Unmarshal(rec.Body.Bytes(), &view)
		if rec := call("POST", "/api/manage/v1/items/"+view.ID+"/publish", "", nil); rec.Code != 200 {
			t.Fatalf("publish: %d %s", rec.Code, rec.Body)
		}

		url := "/assets/" + file["id"].(string) + "/hero.png"
		if rec := httptest.NewRecorder(); true {
			h.ServeHTTP(rec, httptest.NewRequest("GET", url, nil)) // public: no credentials
			if rec.Code != 200 || rec.Body.String() != png || rec.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("asset: %d %v", rec.Code, rec.Header())
			}
		}

		// The OpenAPI document lists the route.
		rec = call("GET", "/api/manage/v1/openapi.json", "", nil)
		if !strings.Contains(rec.Body.String(), `"/uploads"`) {
			t.Fatalf("openapi misses /uploads")
		}

		// The admin learns the upload limit and where files are served.
		rec = call("GET", "/admin/config.json", "", nil)
		if !strings.Contains(rec.Body.String(), `"media":{"maxUpload":1048576,"assetBase":"/assets"}`) {
			t.Fatalf("admin config.json: %s", rec.Body)
		}

		if !slices.Contains(a.JobNames(), media.JobName) {
			t.Errorf("jobs = %v", a.JobNames())
		}
		if err := a.RunJob(ctx, media.JobName); err != nil {
			t.Errorf("job:run media.gc: %v", err)
		}
	})
}

func TestMediaKernelWithoutUploadTypes(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		cfg := mediaConfig(t, standardSchema, "", nil)
		a, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Clock: clk, DB: b.open(t, clk), Stderr: io.Discard}, sqlite.Module())
		if err != nil {
			t.Fatal(err)
		}
		if a.Services().Media != nil {
			t.Fatal("media is on without an upload type")
		}
		h, _ := a.Handler()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/assets/"+shaOf("a")+"/x.png", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("/assets without media: %d", rec.Code)
		}
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/admin/config.json", nil))
		if strings.Contains(rec.Body.String(), `"media"`) {
			t.Fatalf("admin config.json announces media: %s", rec.Body)
		}
	})
}

func TestMediaConfigChecks(t *testing.T) {
	for name, c := range map[string]struct {
		app, want string
		env       map[string]string
	}{
		"unknown driver":     {"media: { storage: { driver: ftp } }", `driver "ftp"`, nil},
		"s3 needs a bucket":  {"media: { storage: { driver: s3, access_key: \"${env:K}\", secret_key: \"${env:S}\" } }", "bucket is required", map[string]string{"K": "k", "S": "s"}},
		"s3 needs keys":      {"media: { storage: { driver: s3, bucket: b } }", "access_key and secret_key", nil},
		"s3 keys from env":   {"media: { storage: { driver: s3, bucket: b, access_key: literal, secret_key: \"${env:S}\" } }", "must come from ${env:NAME}", map[string]string{"S": "s"}},
		"local with s3 keys": {"media: { storage: { driver: local, bucket: b } }", "belong to driver s3", nil},
		"bad public url":     {"media: { public_url: cdn.example.com }", "media.public_url", nil},
		"bad size":           {"media: { max_upload: lots }", "not a size", nil},
		"unknown key":        {"media: { storage: { dirr: x } }", `unknown key "dirr"`, nil},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeFiles(t, map[string]string{"schema/s.yaml": mediaKernelSchema})
			app := "content: { schema_dir: " + filepath.Join(dir, "schema") + " }\n" + c.app + "\n"
			_ = os.MkdirAll(filepath.Join(dir, "config"), 0o755)
			_ = os.WriteFile(filepath.Join(dir, "config/app.yaml"), []byte(app), 0o600)
			cfg, err := config.Load(config.Options{Dir: filepath.Join(dir, "config"), Env: "dev",
				LookupEnv: func(k string) (string, bool) { v, ok := c.env[k]; return v, ok }})
			if err == nil {
				err = kernel.Check(cfg, sqlite.Module())
			}
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v, want %q", err, c.want)
			}
		})
	}

	// A complete s3 configuration passes the check.
	cfg := mediaConfig(t, mediaKernelSchema, `
media:
  max_upload: 100MB
  public_url: https://cdn.example.com/files
  storage: { driver: s3, bucket: b, region: eu-west-1, endpoint: "http://minio:9000", path_style: true, prefix: media/, access_key: "${env:K}", secret_key: "${env:S}" }
`, map[string]string{"K": "k", "S": "s"})
	if err := kernel.Check(cfg, sqlite.Module()); err != nil {
		t.Fatal(err)
	}
	// Configured media without a file-holding type is only worth a warning.
	cfg = mediaConfig(t, standardSchema, "media: { max_upload: 5MB }\n", nil)
	ws, err := kernel.Warnings(cfg, sqlite.Module())
	if err != nil || len(ws) != 1 || !strings.Contains(ws[0], "media is configured") {
		t.Fatalf("warnings = %v, %v", ws, err)
	}
}
