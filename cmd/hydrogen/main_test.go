package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fixtureModule = "example.com/hydrogen-fixture"

// Absolute while the working directory is still the package directory; tests change directory.
var (
	root       = mustAbs(filepath.Join("..", ".."))
	fixtureSrc = mustAbs(filepath.Join("testdata", "fixture"))
)

func mustAbs(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		panic(err)
	}
	return abs
}

func hy(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return out.String(), errb.String(), code
}

func mustHy(t *testing.T, args ...string) string {
	t.Helper()
	out, errs, code := hy(t, args...)
	if code != 0 {
		t.Fatalf("hydrogen %s: exit %d\n%s%s", strings.Join(args, " "), code, out, errs)
	}
	return out
}

func failHy(t *testing.T, want string, args ...string) {
	t.Helper()
	out, errs, code := hy(t, args...)
	if code == 0 || !strings.Contains(errs, want) {
		t.Fatalf("hydrogen %s: exit %d, want failure containing %q\n%s%s", strings.Join(args, " "), code, want, out, errs)
	}
}

// scratchProject scaffolds a project against this checkout and makes it the working directory.
func scratchProject(t *testing.T) string {
	t.Helper()
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	dir := filepath.Join(t.TempDir(), "acme")
	mustHy(t, "new", "-module", "example.com/acme", "-core", root, dir)
	t.Chdir(dir)
	return dir
}

func read(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func has(t *testing.T, name, want string) {
	t.Helper()
	if got := read(t, name); !strings.Contains(got, want) {
		t.Errorf("%s lacks %q:\n%s", name, want, got)
	}
}

func lacks(t *testing.T, name, unwanted string) {
	t.Helper()
	if got := read(t, name); strings.Contains(got, unwanted) {
		t.Errorf("%s has %q:\n%s", name, unwanted, got)
	}
}

func TestNewHasModuleWiring(t *testing.T) {
	scratchProject(t)
	has(t, "hydrogen.yaml", "  - sqlite\n  - postgres\n")
	has(t, "modules_gen.go", "func useModules(h *hydrogen.Builder)")
	has(t, "hydrogen.go", "useModules(h)")
	mustHy(t, "build")
	out := mustHy(t, "modules")
	if !strings.Contains(out, "* sqlite") || !strings.Contains(out, "  images") {
		t.Errorf("modules:\n%s", out)
	}
}

func TestAddRemoveCatalog(t *testing.T) {
	scratchProject(t)
	out := mustHy(t, "add", "images")
	if !strings.Contains(out, "docs/images.md") || !strings.Contains(out, "modules.images") {
		t.Errorf("add output:\n%s", out)
	}
	has(t, "hydrogen.yaml", "  - images\n")
	has(t, "modules_gen.go", "images.Module()")
	lacks(t, "go.mod", "modules/images")

	if out := mustHy(t, "add", "images"); !strings.Contains(out, "already") {
		t.Errorf("second add:\n%s", out)
	}

	mustHy(t, "remove", "postgres")
	lacks(t, "hydrogen.yaml", "postgres")
	lacks(t, "modules_gen.go", "postgres")
	lacks(t, "go.mod", "modules/postgres")
	mustHy(t, "build", "-o", "out/app")
	if _, err := os.Stat("out/app"); err != nil {
		t.Fatal(err)
	}

	mustHy(t, "add", "postgres")
	has(t, "go.mod", "github.com/hydrogencms/hydrogen/modules/postgres v0.0.0")
	has(t, "go.mod", "github.com/hydrogencms/hydrogen/modules/postgres => ")
	has(t, "hydrogen.yaml", "  - sqlite\n  - images\n  - postgres\n")

	mustHy(t, "remove", "images")
	lacks(t, "modules_gen.go", "images")
	out = mustHy(t, "build")
	if !strings.Contains(out, "Built bin/acme") || !strings.Contains(out, "Modules: sqlite, postgres") {
		t.Errorf("build output:\n%s", out)
	}
	if _, err := os.Stat("bin/acme"); err != nil {
		t.Fatal(err)
	}
	if got, err := exec.Command("bin/acme", "version").Output(); err != nil || !strings.Contains(string(got), "hydrogen") {
		t.Fatalf("version: %q %v", got, err)
	}
	mustHy(t, "add", "images")
	mustHy(t, "build")
	got, err := exec.Command("bin/acme", "modules").CombinedOutput()
	if err != nil || !strings.Contains(string(got), "images") || strings.Contains(string(got), "meilisearch") {
		t.Fatalf("modules: %q %v", got, err)
	}
}

func TestThirdPartyModule(t *testing.T) {
	scratchProject(t)
	fixture := filepath.Join(t.TempDir(), "fixture")
	if err := os.CopyFS(fixture, os.DirFS(fixtureSrc)); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"mod", "edit", "-require=" + fixtureModule + "@v0.0.0"},
		{"mod", "edit", "-replace=" + fixtureModule + "=" + fixture},
	} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	mustHy(t, "add", fixtureModule)
	has(t, "hydrogen.yaml", "  - "+fixtureModule+"\n")
	has(t, "modules_gen.go", `fixture "`+fixtureModule+`"`)
	has(t, "modules_gen.go", "fixture.Module()")
	mustHy(t, "build")
	if out := mustHy(t, "modules"); !strings.Contains(out, "* "+fixtureModule) {
		t.Errorf("modules:\n%s", out)
	}
	mustHy(t, "remove", fixtureModule)
	lacks(t, "go.mod", "hydrogen-fixture v0.0.0\n)")
	lacks(t, "modules_gen.go", "fixture")
	mustHy(t, "build")
}

func TestErrors(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"add", "images"}, {"remove", "images"}, {"build"}, {"modules", "init", "sqlite"}} {
		failHy(t, "not in a Hydrogen project", args...)
	}
	if out := mustHy(t, "modules"); !strings.Contains(out, "images") {
		t.Errorf("catalog outside a project:\n%s", out)
	}

	scratchProject(t)
	failHy(t, "give module names", "add")
	failHy(t, "unknown module", "add", "nope")
	failHy(t, "unknown module", "add", "images", "acme/x")
	failHy(t, "versioned with the core", "add", "images@v1.0.0")
	failHy(t, "given twice", "add", "images", "images")
	failHy(t, "is not in hydrogen.yaml", "remove", "images")
	failHy(t, "already exists", "modules", "init", "sqlite")
	failHy(t, "unknown argument", "modules", "bogus")
	if err := os.WriteFile("hydrogen.yaml", []byte("modules: [sqlite]\nextra: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	failHy(t, "field extra not found", "add", "images")
	failHy(t, "field extra not found", "build")

	if err := os.Remove("hydrogen.yaml"); err != nil {
		t.Fatal(err)
	}
	failHy(t, "hydrogen modules init", "add", "images")
}

func TestFailedAddChangesNothing(t *testing.T) {
	scratchProject(t)
	fixture := filepath.Join(t.TempDir(), "fixture")
	if err := os.CopyFS(fixture, os.DirFS(fixtureSrc)); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"mod", "edit", "-require=" + fixtureModule + "@v0.0.0"},
		{"mod", "edit", "-replace=" + fixtureModule + "=" + fixture},
	} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, out)
		}
	}
	files := []string{"hydrogen.yaml", "modules_gen.go", "go.mod", "go.sum"}
	before := map[string]string{}
	for _, f := range files {
		before[f] = read(t, f)
	}
	// The module is required, so no go get runs; the package is missing, so go mod tidy fails after the
	// files were written.
	failHy(t, "nothing was changed", "add", fixtureModule+"/missing")
	// Not required and not resolvable: go get fails first.
	t.Setenv("GOPROXY", "off")
	failHy(t, "nothing was changed", "add", "example.invalid/nope")
	for _, f := range files {
		if got := read(t, f); got != before[f] {
			t.Errorf("%s changed:\n%s", f, got)
		}
	}
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hydrogen.yaml.") || strings.HasPrefix(e.Name(), ".modules_gen.go.") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestModulesInit(t *testing.T) {
	scratchProject(t)
	if err := os.Remove("hydrogen.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("modules_gen.go"); err != nil {
		t.Fatal(err)
	}
	by := `package main

import (
	"github.com/hydrogencms/hydrogen"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func configure(h *hydrogen.Builder) {
	h.Use(sqlite.Module(), postgres.Module())
}
`
	if err := os.WriteFile("hydrogen.go", []byte(by), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := mustHy(t, "modules"); !strings.Contains(out, "no hydrogen.yaml") {
		t.Errorf("modules:\n%s", out)
	}
	out := mustHy(t, "modules", "init", "sqlite", "postgres")
	if !strings.Contains(out, "hydrogen.go:10:") || !strings.Contains(out, "h.Use(sqlite.Module()") {
		t.Errorf("init output:\n%s", out)
	}
	has(t, "hydrogen.yaml", "  - sqlite\n  - postgres\n")
	has(t, "modules_gen.go", "postgres.Module()")
	// The user's code is not rewritten.
	if got := read(t, "hydrogen.go"); got != by {
		t.Errorf("hydrogen.go rewritten:\n%s", got)
	}
}

func TestGenerateRefreshesModules(t *testing.T) {
	scratchProject(t)
	if err := os.WriteFile("hydrogen.yaml", []byte("modules:\n  - sqlite\n  - pages\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("modules_gen.go"); err != nil {
		t.Fatal(err)
	}
	mustHy(t, "generate")
	has(t, "modules_gen.go", "pages.Module()")
	lacks(t, "modules_gen.go", "postgres")
}

func TestUsage(t *testing.T) {
	out := mustHy(t, "help")
	for _, want := range []string{"hydrogen add", "hydrogen remove", "hydrogen build", "hydrogen modules init"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage lacks %q", want)
		}
	}
}
