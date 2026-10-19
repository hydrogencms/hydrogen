package wasmhooks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

const kernelToken = "wasm-token-0123456789"

// kernelApp boots a kernel with sqlite and the module on the modes fixture. hooksYAML is the modules.wasmhooks
// hooks list.
func kernelApp(t *testing.T, hooksYAML string, names ...string) (*kernel.App, *logs) {
	t.Helper()
	root := t.TempDir()
	write := func(name, data string) {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("schema/s.yaml", testSchema)
	write("config/app.yaml", "content: { schema_dir: "+filepath.Join(root, "schema")+" }\n"+
		"database: { url: \"${env:DBURL}\" }\n"+
		"manage: { token: \"${env:MTOKEN}\" }\n"+
		"modules:\n  wasmhooks:\n    dir: "+hookDir(t, names...)+"\n    timeout: 10s\n    hooks:\n"+hooksYAML)
	cfg, err := config.Load(config.Options{Dir: filepath.Join(root, "config"), Env: "dev", LookupEnv: dbEnv(root)})
	if err != nil {
		t.Fatal(err)
	}
	l := &logs{}
	mod := &module{cfg: defaultConfig(), cache: sharedCache}
	app, err := kernel.New(context.Background(), kernel.Options{Config: cfg, Stderr: io.Discard, PasswordIterations: 1000, Stdout: io.Discard,
		Log: newLogger(l)}, sqlite.Module(), mod)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close(); _ = mod.Stop(context.Background()) })
	if err := app.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Services().Content.EnsureSchemaRev(context.Background()); err != nil {
		t.Fatal(err)
	}
	return app, l
}

func TestKernelHooks(t *testing.T) {
	app, logs := kernelApp(t, `
      - { file: reading.wasm, event: beforeSave, types: [article] }
      - { file: noshout.wasm, event: validate }
      - { file: freeze.wasm, event: beforePublish }
      - { file: notify.wasm, event: afterPublish, async: true, name: notify }
`, "reading", "noshout", "freeze", "notify")
	ctx := context.Background()
	svc := app.Services().Content

	// beforeSave computes a field; the integer stays an integer.
	body := strings.TrimSpace(strings.Repeat("word ", 450))
	v, err := svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: decodeDoc(t, `{"title":"Fine title","body":"`+body+`"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Fields["readingMinutes"]; got != json.Number("2") {
		t.Fatalf("readingMinutes = %#v (fields %v)", got, v.Fields)
	}
	got, err := svc.Get(ctx, v.ID, content.GetOptions{Draft: true})
	if err != nil || got.Fields["readingMinutes"] != json.Number("2") {
		t.Fatalf("stored: %v %v", got, err)
	}

	// validate surfaces as a field error.
	_, err = svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: decodeDoc(t, `{"title":"LOUD TITLE"}`)})
	var ve *content.ValidationError
	if !errors.As(err, &ve) || len(ve.Errors) != 1 || ve.Errors[0].Path != "title" || ve.Errors[0].Message != "No shouting" {
		t.Fatalf("validate: %v", err)
	}

	// the async hook runs through the outbox after publishing, and not before.
	if _, err := svc.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "notified") {
		t.Errorf("async hook ran inside the transaction: %s", logs.String())
	}
	if _, err := app.Services().Outbox.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if out := logs.String(); !strings.Contains(out, "notify.wasm") || !strings.Contains(out, "notified afterPublish "+v.ID.String()) {
		t.Errorf("log = %s", out)
	}

	// beforePublish blocks; the management API answers 400 with the hook's message.
	h, err := app.Handler()
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+kernelToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	frozen, err := svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Fields: decodeDoc(t, `{"title":"FROZEN one","body":"x"}`)})
	if err != nil {
		t.Fatal(err)
	}
	rec := call("POST", "/api/manage/v1/items/"+frozen.ID.String()+"/publish", "")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Publishing is frozen") || !strings.Contains(rec.Body.String(), "freeze.wasm (beforePublish)") {
		t.Fatalf("publish frozen: %d %s", rec.Code, rec.Body)
	}
}

func TestKernelUnknownTypeFailsAtStart(t *testing.T) {
	root := t.TempDir()
	for name, data := range map[string]string{
		"schema/s.yaml":   testSchema,
		"config/app.yaml": "content: { schema_dir: " + filepath.Join(root, "schema") + " }\ndatabase: { url: \"${env:DBURL}\" }\nmodules:\n  wasmhooks:\n    dir: " + hookDir(t, "echo") + "\n    hooks:\n      - { file: echo.wasm, event: beforeSave, types: [nope] }\n",
	} {
		p := filepath.Join(root, name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := config.Load(config.Options{Dir: filepath.Join(root, "config"), Env: "dev", LookupEnv: dbEnv(root)})
	if err != nil {
		t.Fatal(err)
	}
	mod := &module{cfg: defaultConfig(), cache: sharedCache}
	_, err = kernel.New(context.Background(), kernel.Options{Config: cfg, Stderr: io.Discard}, sqlite.Module(), mod)
	if err == nil || !strings.Contains(err.Error(), `echo.wasm (beforeSave): unknown content type "nope"`) {
		t.Errorf("err = %v", err)
	}
	_ = mod.Stop(context.Background())
}

// config:check only registers modules: a broken hook file shows there.
func TestKernelCheckCompiles(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "hooks")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bad.wasm"), []byte("junk"), 0o600)
	_ = os.MkdirAll(filepath.Join(root, "schema"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "config"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "schema/s.yaml"), []byte(testSchema), 0o600)
	_ = os.WriteFile(filepath.Join(root, "config/app.yaml"), []byte("content: { schema_dir: "+filepath.Join(root, "schema")+" }\nmodules:\n  wasmhooks:\n    dir: "+dir+"\n    hooks:\n      - { file: bad.wasm, event: beforeSave }\n"), 0o600)
	cfg, err := config.Load(config.Options{Dir: filepath.Join(root, "config"), Env: "dev", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	err = kernel.Check(cfg, sqlite.Module(), &module{cfg: defaultConfig(), cache: sharedCache})
	if err == nil || !strings.Contains(err.Error(), "bad.wasm: not a valid WebAssembly module") {
		t.Errorf("err = %v", err)
	}
}

func dbEnv(root string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		if k == "MTOKEN" {
			return kernelToken, true
		}
		if k == "DBURL" {
			return "sqlite:" + filepath.Join(root, "test.db"), true
		}
		return "", false
	}
}
