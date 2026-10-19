//go:build !linux && !windows

package siteproxy

import "syscall"

func deathSignal(*syscall.SysProcAttr) {}
