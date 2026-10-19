package siteproxy

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The test binary doubles as the supervised child: TestMain runs helperMain when HELPER_MODE is set.
//
// HELPER_PORT        port to listen on
// HELPER_DELAY       wait this long (a duration) before listening
// HELPER_CRASH_ONCE  a marker path: the first run creates it and exits 3 shortly after it is healthy
// HELPER_PIDFILE     where the pid (and the grandchild's) are written
// HELPER_GRANDCHILD  spawn a copy of this process that only sleeps
// HELPER_IGNORE_TERM ignore SIGTERM (checked in helper_unix_test.go)
func TestMain(m *testing.M) {
	if mode := os.Getenv("HELPER_MODE"); mode != "" {
		helperMain(mode)
		return
	}
	os.Exit(m.Run())
}

func helperMain(mode string) {
	if mode == "sleep" {
		time.Sleep(time.Hour)
		return
	}
	ignoreTerm()
	if f := os.Getenv("HELPER_PIDFILE"); f != "" {
		pids := strconv.Itoa(os.Getpid())
		if os.Getenv("HELPER_GRANDCHILD") != "" {
			c := exec.Command(os.Args[0])
			c.Env = append(os.Environ(), "HELPER_MODE=sleep")
			if err := c.Start(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(2)
			}
			pids += "\n" + strconv.Itoa(c.Process.Pid)
		}
		_ = os.WriteFile(f, []byte(pids), 0o600)
	}
	if f := os.Getenv("HELPER_ENVFILE"); f != "" {
		_ = os.WriteFile(f, []byte(strings.Join(os.Environ(), "\n")), 0o600)
	}
	if d, err := time.ParseDuration(os.Getenv("HELPER_DELAY")); err == nil {
		time.Sleep(d)
	}
	fmt.Println("helper booting on", os.Getenv("HELPER_PORT"))
	fmt.Fprintln(os.Stderr, "helper stderr line")
	l, err := net.Listen("tcp", os.Getenv("HELPER_HOST")+":"+os.Getenv("HELPER_PORT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		for _, h := range []string{"X-Forwarded-For", "X-Forwarded-Proto", "X-Forwarded-Host"} {
			fmt.Fprintf(w, "%s=%s\n", h, r.Header.Get(h))
		}
		fmt.Fprintf(w, "Host=%s\nPath=%s\nMethod=%s\n", r.Host, r.URL.RequestURI(), r.Method)
	})
	mux.HandleFunc("/stream", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "first\n")
		w.(http.Flusher).Flush()
		time.Sleep(600 * time.Millisecond)
		fmt.Fprint(w, "second\n")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "frontend:%s", r.URL.Path) })
	if marker := os.Getenv("HELPER_CRASH_ONCE"); marker != "" {
		if _, err := os.Stat(marker); err != nil {
			_ = os.WriteFile(marker, nil, 0o600)
			go func() { time.Sleep(400 * time.Millisecond); os.Exit(3) }()
		}
	}
	_ = http.Serve(l, mux)
}

// helperConfig is an SSRConfig that runs this test binary as the child.
func helperConfig(env map[string]string) SSRConfig {
	e := map[string]string{"HELPER_MODE": "serve", "HELPER_PORT": "{port}", "HELPER_HOST": "{host}"}
	for k, v := range env {
		e[k] = v
	}
	return SSRConfig{Host: "127.0.0.1", Command: []string{os.Args[0], "-test.run=NONE"}, Env: e, Health: "/healthz",
		StartTimeout: 10 * time.Second, RestartBackoff: 50 * time.Millisecond, RestartBackoffMax: 200 * time.Millisecond,
		StopTimeout: 3 * time.Second}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func readPids(t *testing.T, file string) []int {
	t.Helper()
	var out []int
	waitFor(t, "pid file", func() bool {
		b, err := os.ReadFile(file)
		if err != nil || len(b) == 0 {
			return false
		}
		out = nil
		for f := range strings.FieldsSeq(string(b)) {
			n, _ := strconv.Atoi(f)
			out = append(out, n)
		}
		return true
	})
	return out
}
