package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/config"
)

const (
	token    = "test-token"
	iosToken = "ios-secret-token-0001"
)

// server runs `serve` for the duration of the test and returns an API client.
type server struct {
	t    *testing.T
	base string
}

func start(t *testing.T, env string, vars map[string]string, configure func(*hydrogen.Builder)) *server {
	t.Helper()
	vars["LISTEN_ADDR"] = "127.0.0.1:0"
	vars["MANAGE_TOKEN"] = token
	vars["PREVIEW_SECRET"] = "test-preview-secret-0123456789abcdef"
	vars["IOS_TOKEN"] = iosToken
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
	exited := false
	t.Cleanup(func() {
		cancel()
		if exited {
			return
		}
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	select {
	case a := <-addr:
		return &server{t: t, base: "http://" + a + "/api/manage/v1"}
	case err := <-done:
		exited = true
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
