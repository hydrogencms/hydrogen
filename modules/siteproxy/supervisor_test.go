//go:build !windows

package siteproxy

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type logBuf struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *logBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}
func (b *logBuf) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.sb.String() }

func TestSupervisorHealthGating(t *testing.T) {
	var logs logBuf
	s := newSupervisor(helperConfig(map[string]string{"HELPER_DELAY": "700ms"}), slog.New(slog.NewTextHandler(&logs, nil)))
	s.Start(context.Background())
	defer func() { _ = s.Stop(context.Background()) }()
	time.Sleep(300 * time.Millisecond)
	if s.Target() != nil {
		t.Fatal("target published before the process was healthy")
	}
	waitFor(t, "healthy target", func() bool { return s.Target() != nil })
	resp, err := http.Get(s.Target().String() + "/anything")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "frontend:/anything" {
		t.Fatalf("body %q", body)
	}
	if out := logs.String(); !strings.Contains(out, "frontend: helper booting") || !strings.Contains(out, "stream=stderr") {
		t.Errorf("child output not logged:\n%s", out)
	}
}

func TestSupervisorRestartsACrashedProcess(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "crashed")
	s := newSupervisor(helperConfig(map[string]string{"HELPER_CRASH_ONCE": marker}), slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Start(context.Background())
	defer func() { _ = s.Stop(context.Background()) }()
	waitFor(t, "first healthy target", func() bool { return s.Target() != nil })
	first := s.Target().Host
	waitFor(t, "target to drop after the crash", func() bool { return s.Target() == nil })
	waitFor(t, "restarted target", func() bool { return s.Target() != nil })
	if s.Target().Host == first {
		t.Log("same port reused (allowed)")
	}
	time.Sleep(700 * time.Millisecond) // the second run does not crash again
	if s.Target() == nil {
		t.Fatal("the restarted process went away")
	}
}

func TestSupervisorStopKillsTheProcessGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	cfg := helperConfig(map[string]string{"HELPER_PIDFILE": pidfile, "HELPER_GRANDCHILD": "1"})
	s := newSupervisor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Start(context.Background())
	waitFor(t, "healthy target", func() bool { return s.Target() != nil })
	pids := readPids(t, pidfile)
	if len(pids) != 2 || !alive(pids[0]) || !alive(pids[1]) {
		t.Fatalf("pids %v", pids)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Target() != nil {
		t.Error("target kept after stop")
	}
	waitDead(t, pids)
}

func TestSupervisorKillsAProcessThatIgnoresSIGTERM(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	cfg := helperConfig(map[string]string{"HELPER_PIDFILE": pidfile, "HELPER_IGNORE_TERM": "1"})
	cfg.StopTimeout = 300 * time.Millisecond
	s := newSupervisor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Start(context.Background())
	waitFor(t, "healthy target", func() bool { return s.Target() != nil })
	pids := readPids(t, pidfile)
	began := time.Now()
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(began); d < 250*time.Millisecond {
		t.Errorf("stopped in %s: SIGTERM was not given its grace period", d)
	}
	waitDead(t, pids)
}

func TestSupervisorStartTimeout(t *testing.T) {
	cfg := helperConfig(map[string]string{"HELPER_DELAY": "5s"})
	cfg.StartTimeout = 300 * time.Millisecond
	cfg.StopTimeout = 200 * time.Millisecond
	var logs logBuf
	s := newSupervisor(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	s.Start(context.Background())
	waitFor(t, "a restart after the timeout", func() bool { return strings.Count(logs.String(), "frontend process started") >= 2 })
	if s.Target() != nil {
		t.Error("an unhealthy process was routed to")
	}
	_ = s.Stop(context.Background())
}

func TestSupervisorStartFailureRetries(t *testing.T) {
	cfg := helperConfig(nil)
	cfg.Command = []string{"/nonexistent/program"}
	var logs logBuf
	s := newSupervisor(cfg, slog.New(slog.NewTextHandler(&logs, nil)))
	s.Start(context.Background())
	waitFor(t, "two failed attempts", func() bool { return strings.Count(logs.String(), "restarting") >= 2 })
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorLocalhostHost(t *testing.T) {
	cfg := helperConfig(nil)
	cfg.Host = "localhost"
	s := newSupervisor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.Start(context.Background())
	defer func() { _ = s.Stop(context.Background()) }()
	waitFor(t, "healthy target", func() bool { return s.Target() != nil })
	if !strings.HasPrefix(s.Target().Host, "localhost:") {
		t.Errorf("target %s", s.Target())
	}
}

func TestSupervisorEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://secret")
	t.Setenv("NODE_OPTIONS_TEST", "x")
	for _, inherit := range []bool{false, true} {
		file := filepath.Join(t.TempDir(), "env")
		cfg := helperConfig(map[string]string{"HELPER_ENVFILE": file, "FROM_CONFIG": "yes"})
		cfg.InheritEnv = inherit
		s := newSupervisor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		s.Start(context.Background())
		waitFor(t, "healthy target", func() bool { return s.Target() != nil })
		b, _ := os.ReadFile(file)
		_ = s.Stop(context.Background())
		env := string(b)
		if !strings.Contains(env, "FROM_CONFIG=yes") || !strings.Contains(env, "PATH=") {
			t.Errorf("inherit=%v: configured or PATH missing", inherit)
		}
		if got := strings.Contains(env, "DATABASE_URL="); got != inherit {
			t.Errorf("inherit=%v: DATABASE_URL present = %v", inherit, got)
		}
		if !strings.Contains(env, "NODE_OPTIONS_TEST=x") {
			t.Errorf("inherit=%v: NODE_* not passed", inherit)
		}
	}
}
