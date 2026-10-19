package main

import (
	"regexp"
	"strings"

	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/internal/scaffold"
)

// buildVersion is set by release builds: -ldflags "-X main.buildVersion=v0.1.0".
var buildVersion string

var releaseTag = regexp.MustCompile(`^v\d+\.\d+\.\d+(-(alpha|beta|rc)[.0-9]*)?$`)

// cliVersion is what `hydrogen version` prints: the release version for tagged builds (ldflags, or the module
// version of `go install …@v0.1.0`) and "devel" otherwise.
func cliVersion() string {
	if releaseTag.MatchString(buildVersion) {
		return buildVersion
	}
	return hydrogen.Version()
}

// releaseVersion is the version `hydrogen new` requires without -core: this CLI's own version when it is a
// release, else the latest release.
func releaseVersion() string {
	if v := cliVersion(); releaseTag.MatchString(v) {
		return v
	}
	return scaffold.ReleaseVersion
}

// commandUsage returns the usage lines of one subcommand (and the dev environment for dev), or "".
func commandUsage(cmd string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(usage, "\n") {
		switch {
		case strings.HasPrefix(line, "  hydrogen "+cmd+" "):
			in = true
		case strings.HasPrefix(line, "  hydrogen "), line == "" || !strings.HasPrefix(line, " "):
			in = false
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	out := "Usage:\n" + b.String()
	if cmd == "dev" {
		_, env, _ := strings.Cut(usage, "\nEnvironment for hydrogen dev:")
		out += "\nEnvironment for hydrogen dev:" + env
	}
	return out
}
