//go:build !windows

package siteproxy

import (
	"os"
	"os/signal"
	"syscall"
	"testing"
)

func ignoreTerm() {
	if os.Getenv("HELPER_IGNORE_TERM") != "" {
		signal.Ignore(syscall.SIGTERM)
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func waitDead(t *testing.T, pids []int) {
	t.Helper()
	waitFor(t, "processes to be gone", func() bool {
		for _, p := range pids {
			if alive(p) {
				return false
			}
		}
		return true
	})
}
