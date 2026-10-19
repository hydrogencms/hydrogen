package main

import (
	"os"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/internal/scaffold"
)

func TestSubcommandHelp(t *testing.T) {
	for _, cmd := range []string{"dev", "generate", "add", "remove", "build", "hooks", "modules", "new"} {
		for _, flag := range []string{"-h", "--help"} {
			out, errs, code := hy(t, cmd, flag)
			if code != 0 {
				t.Errorf("hydrogen %s %s: exit %d\n%s", cmd, flag, code, errs)
			}
			if !strings.Contains(out+errs, "hydrogen "+cmd) && !strings.Contains(out+errs, "-module") {
				t.Errorf("hydrogen %s %s printed no usage:\n%s%s", cmd, flag, out, errs)
			}
		}
	}
	out, _, _ := hy(t, "dev", "-h")
	if !strings.Contains(out, "ADMIN_DEV_URL") || strings.Contains(out, "hydrogen add") {
		t.Errorf("dev usage:\n%s", out)
	}
}

func TestVersion(t *testing.T) {
	old := buildVersion
	defer func() { buildVersion = old }()

	buildVersion = ""
	if got := releaseVersion(); got != scaffold.ReleaseVersion {
		t.Errorf("dev build requires %s, want %s", got, scaffold.ReleaseVersion)
	}
	if out, _, _ := hy(t, "version"); !strings.HasPrefix(out, "hydrogen ") || strings.Contains(out, "v0.0.0") {
		t.Errorf("version: %q", out)
	}
	buildVersion = "v0.3.1"
	if out, _, _ := hy(t, "version"); out != "hydrogen v0.3.1\n" {
		t.Errorf("version: %q", out)
	}
	if got := releaseVersion(); got != "v0.3.1" {
		t.Errorf("release build requires %s", got)
	}
	buildVersion = "v0.3.2-0.20260101000000-abcdef123456"
	if got := releaseVersion(); got != scaffold.ReleaseVersion {
		t.Errorf("pseudo-version build requires %s", got)
	}
}

func TestNewRequiresRelease(t *testing.T) {
	dir := t.TempDir() + "/app"
	mustHy(t, "new", "-no-tidy", dir)
	b, err := os.ReadFile(dir + "/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "github.com/hydrogencms/hydrogen "+scaffold.ReleaseVersion) || strings.Contains(string(b), "v0.0.0") {
		t.Errorf("go.mod:\n%s", b)
	}
}
