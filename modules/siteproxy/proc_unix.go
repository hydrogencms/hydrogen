//go:build !windows

package siteproxy

import (
	"os/exec"
	"syscall"
)

// setGroup puts the child in its own process group, so a signal reaches whatever it spawned too.
func setGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	deathSignal(c.SysProcAttr)
}

func signalGroup(c *exec.Cmd, kill bool) {
	if c.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if kill {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-c.Process.Pid, sig)
}
