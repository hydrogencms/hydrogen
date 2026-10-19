//go:build windows

package siteproxy

import "testing"

func ignoreTerm() {}

func waitDead(*testing.T, []int) {}
