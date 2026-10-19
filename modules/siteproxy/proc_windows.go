//go:build windows

package siteproxy

import "os/exec"

func setGroup(*exec.Cmd) {}

// signalGroup kills the process: Windows has no SIGTERM, and children are not grouped.
func signalGroup(c *exec.Cmd, _ bool) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
