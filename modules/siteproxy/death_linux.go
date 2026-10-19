//go:build linux

package siteproxy

import "syscall"

// deathSignal makes the kernel kill the child when Hydrogen dies, even by SIGKILL.
func deathSignal(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
