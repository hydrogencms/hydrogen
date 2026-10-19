package siteproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// supervisor runs the frontend process: it picks a free loopback port, waits for the health check before it
// publishes the target, restarts a process that exits (with doubling backoff) and stops it with the group.
type supervisor struct {
	cfg SSRConfig
	log *slog.Logger
	// stableAfter is how long a process must stay healthy for the next restart to start from the base backoff.
	stableAfter time.Duration
	onChange    func()

	mu     sync.RWMutex
	target *url.URL

	cancel context.CancelFunc
	done   chan struct{}
}

func newSupervisor(cfg SSRConfig, log *slog.Logger) *supervisor {
	return &supervisor{cfg: cfg, log: log, stableAfter: 10 * time.Second}
}

// Target is the frontend's base URL while it is healthy, else nil.
func (s *supervisor) Target() *url.URL {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.target
}

func (s *supervisor) setTarget(u *url.URL) {
	s.mu.Lock()
	changed := (s.target == nil) != (u == nil) || (u != nil && s.target.Host != u.Host)
	s.target = u
	s.mu.Unlock()
	if changed && s.onChange != nil {
		s.onChange()
	}
}

// Start launches the supervising goroutine; it does not wait for the process.
func (s *supervisor) Start(ctx context.Context) {
	// The process outlives ctx (the server's shutdown signal): Stop ends it, after in-flight requests finished.
	ctx, s.cancel = context.WithCancel(context.WithoutCancel(ctx))
	s.done = make(chan struct{})
	go s.run(ctx)
}

// Stop ends the process (SIGTERM to its group, SIGKILL after stop_timeout) and waits for the supervisor to finish.
func (s *supervisor) Stop(ctx context.Context) error {
	if s.cancel == nil {
		return nil
	}
	s.cancel()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("site-proxy: the frontend did not stop in time: %w", ctx.Err())
	}
}

func (s *supervisor) run(ctx context.Context) {
	defer close(s.done)
	// Parent-death signals follow the thread that started the child: keep it for the supervisor's lifetime.
	runtime.LockOSThread()
	delay := s.cfg.RestartBackoff
	for ctx.Err() == nil {
		began := time.Now()
		healthyFor, err := s.runOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		if healthyFor >= s.stableAfter {
			delay = s.cfg.RestartBackoff
		}
		s.log.Warn("site-proxy: the frontend stopped; restarting", "err", err, "ran", time.Since(began).Round(time.Millisecond), "in", delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, s.cfg.RestartBackoffMax)
	}
}

// runOnce starts the process and returns when it is gone, with how long it was healthy.
func (s *supervisor) runOnce(ctx context.Context) (healthyFor time.Duration, err error) {
	port, err := freePort(s.cfg.Host)
	if err != nil {
		return 0, err
	}
	sub := func(v string) string {
		return strings.NewReplacer("{port}", strconv.Itoa(port), "{host}", s.cfg.Host).Replace(v)
	}
	args := make([]string, len(s.cfg.Command))
	for i, a := range s.cfg.Command {
		args[i] = sub(a)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = s.cfg.Dir
	cmd.Env = childEnv(s.cfg.InheritEnv)
	for k, v := range s.cfg.Env {
		cmd.Env = append(cmd.Env, k+"="+sub(v))
	}
	out := &lineLog{log: s.log, stream: "stdout"}
	errw := &lineLog{log: s.log, stream: "stderr"}
	cmd.Stdout, cmd.Stderr = out, errw
	cmd.WaitDelay = 2 * time.Second
	setGroup(cmd)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start %s: %w", args[0], err)
	}
	s.log.Info("site-proxy: frontend process started", "pid", cmd.Process.Pid, "port", port)
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	var exitErr error
	var gone bool
	finish := func() {
		if !gone {
			s.terminate(cmd, exited)
		}
		out.flush()
		errw.flush()
		signalGroup(cmd, true) // orphans of a child that exited
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort(s.cfg.Host, strconv.Itoa(port))}
	healthy := s.waitHealthy(ctx, target, exited, &exitErr, &gone)
	if !healthy {
		finish()
		if exitErr == nil {
			exitErr = errors.New("not healthy before start_timeout")
		}
		return 0, exitErr
	}
	// A health answer could come from another process that took the port between our check and the child's bind;
	// that child then fails to bind and exits. Publish only if it is still running a moment later.
	select {
	case exitErr = <-exited:
		gone = true
		finish()
		return 0, fmt.Errorf("exited right after the health check: %w", exitErr)
	case <-time.After(healthySettle):
	case <-ctx.Done():
		finish()
		return 0, ctx.Err()
	}
	s.log.Info("site-proxy: frontend is healthy", "url", target.String())
	since := time.Now()
	s.setTarget(target)
	select {
	case exitErr = <-exited:
		gone = true
	case <-ctx.Done():
	}
	s.setTarget(nil)
	finish()
	if exitErr == nil && !gone {
		exitErr = ctx.Err()
	}
	if exitErr == nil {
		exitErr = errors.New("exited")
	}
	return time.Since(since), exitErr
}

// waitHealthy polls the health path until the process answers, exits, ctx ends or start_timeout passes.
func (s *supervisor) waitHealthy(ctx context.Context, target *url.URL, exited <-chan error, exitErr *error, gone *bool) bool {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	deadline := time.NewTimer(s.cfg.StartTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			if err == nil {
				err = errors.New("exited before it was healthy")
			}
			*exitErr, *gone = err, true
			return false
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-tick.C:
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target.String()+s.cfg.Health, nil)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 500 {
			return true
		}
	}
}

// terminate asks the process group to stop and kills it after stop_timeout.
func (s *supervisor) terminate(cmd *exec.Cmd, exited <-chan error) {
	signalGroup(cmd, false)
	select {
	case <-exited:
	case <-time.After(s.cfg.StopTimeout):
		s.log.Warn("site-proxy: the frontend ignored SIGTERM; killing it", "timeout", s.cfg.StopTimeout)
		signalGroup(cmd, true)
		<-exited
	}
}

const healthySettle = 150 * time.Millisecond

// safeEnv names the inherited variables a frontend process may see unless inherit_env is set.
var safeEnv = []string{"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TEMP", "TMP", "TZ", "LANG", "SystemRoot", "USERPROFILE", "APPDATA", "LOCALAPPDATA"}

func childEnv(all bool) []string {
	env := os.Environ()
	if all {
		return env
	}
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if slices.Contains(safeEnv, k) || strings.HasPrefix(k, "LC_") || strings.HasPrefix(k, "NODE_") || strings.HasPrefix(k, "NPM_CONFIG_") {
			out = append(out, kv)
		}
	}
	return out
}

func freePort(host string) (int, error) {
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// lineLog writes the child's output to the logger line by line.
type lineLog struct {
	log    *slog.Logger
	stream string
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (l *lineLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf.Write(p)
	for {
		i := bytes.IndexByte(l.buf.Bytes(), '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(l.buf.Next(i+1)), "\r\n")
		l.emit(line)
	}
	if l.buf.Len() > 64<<10 { // a runaway line without a newline
		l.emit(l.buf.String())
		l.buf.Reset()
	}
	return len(p), nil
}

func (l *lineLog) flush() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buf.Len() > 0 {
		l.emit(strings.TrimRight(l.buf.String(), "\r\n"))
		l.buf.Reset()
	}
}

func (l *lineLog) emit(line string) {
	if line != "" {
		l.log.Info("frontend: "+line, "stream", l.stream)
	}
}
