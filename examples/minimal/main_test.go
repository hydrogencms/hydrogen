package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
)

// boot runs `serve` against databaseURL, checks the health endpoints, and shuts down.
func boot(t *testing.T, databaseURL string, env string) {
	vars := map[string]string{"DATABASE_URL": databaseURL, "LISTEN_ADDR": "127.0.0.1:0", "MANAGE_TOKEN": "test-token",
		"PREVIEW_SECRET": "test-preview-secret-at-least-32-characters"}
	opts := &config.Options{Dir: "config", Env: env,
		LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if env != config.DevEnv {
		var out bytes.Buffer
		if err := hydrogen.Run(ctx, []string{"migrate"}, configure, hydrogen.RunOptions{Config: opts, Stdout: &out, Stderr: io.Discard}); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		if !strings.Contains(out.String(), "applied core/0001_core") {
			t.Fatalf("migrate output: %q", out.String())
		}
	}

	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- hydrogen.Run(ctx, []string{"serve"}, configure, hydrogen.RunOptions{Config: opts, Stderr: io.Discard, PasswordIterations: 1000,
			Listening: func(a string) { addr <- a }})
	}()
	var base string
	select {
	case a := <-addr:
		base = "http://" + a
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	}
	for _, path := range []string{"/healthz", "/readyz"} {
		resp, err := http.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestServeSQLiteDev(t *testing.T) {
	boot(t, "sqlite:"+filepath.Join(t.TempDir(), "h.db"), config.DevEnv)
}

func TestServePostgresProd(t *testing.T) {
	boot(t, pgtest.Scratch(t), "prod")
}

func TestServeRefusesPendingMigrationsOutsideDev(t *testing.T) {
	vars := map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db"), "MANAGE_TOKEN": "test-token",
		"PREVIEW_SECRET": "test-preview-secret-at-least-32-characters"}
	opts := &config.Options{Dir: "config", Env: "prod",
		LookupEnv: func(k string) (string, bool) { v, ok := vars[k]; return v, ok }}
	err := hydrogen.Run(context.Background(), []string{"serve"}, configure, hydrogen.RunOptions{Config: opts, Stderr: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "pending migrations") {
		t.Fatalf("got %v", err)
	}
}

func TestConfigCheck(t *testing.T) {
	var out bytes.Buffer
	opts := &config.Options{Dir: "config", LookupEnv: func(string) (string, bool) { return "", false }}
	if err := hydrogen.Run(context.Background(), []string{"config:check"}, configure, hydrogen.RunOptions{Config: opts, Stdout: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "config ok") {
		t.Fatalf("output %q", out.String())
	}
	// An unknown adapter scheme is a configuration error, caught without connecting.
	bad := &config.Options{Dir: "config", LookupEnv: func(k string) (string, bool) {
		if k == "DATABASE_URL" {
			return "mysql://x", true
		}
		return "", false
	}}
	err := hydrogen.Run(context.Background(), []string{"config:check"}, configure, hydrogen.RunOptions{Config: bad, Stdout: io.Discard})
	if err == nil || !strings.Contains(err.Error(), `no adapter for "mysql"`) {
		t.Fatalf("got %v", err)
	}
}
