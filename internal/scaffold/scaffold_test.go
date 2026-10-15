package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme")
	if err := Write(Options{Dir: dir, Module: "example.com/acme", Core: "../.."}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"go.mod", "main.go", "hydrogen.go", "config/app.yaml", "schema/.gitkeep", ".gitignore", "README.md",
		"admin/package.json", "admin/svelte.config.js", "admin/vite.config.ts", "admin/tsconfig.json", "admin/src/elements.ts", "admin/src/Hello.svelte",
		"admin/logo.svg", "admin/theme.css", "admin/strings.json"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	gomod, _ := os.ReadFile(filepath.Join(dir, "go.mod"))
	if !strings.Contains(string(gomod), "module example.com/acme") || !strings.Contains(string(gomod), "github.com/hydrogencms/hydrogen => ") {
		t.Fatalf("go.mod:\n%s", gomod)
	}
	if err := Write(Options{Dir: dir}); err == nil {
		t.Fatal("writing into a non-empty directory succeeded")
	}
}

func TestWriteRejectsBadCore(t *testing.T) {
	if err := Write(Options{Dir: filepath.Join(t.TempDir(), "x"), Core: t.TempDir()}); err == nil {
		t.Fatal("accepted a core directory without hydrogen.go")
	}
}

func TestWriteAdminStarter(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme")
	if err := Write(Options{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	yaml, _ := os.ReadFile(filepath.Join(dir, "config/app.yaml"))
	for _, want := range []string{"#   elements: [./admin/dist/elements.js]", "#   widgets: { hello: x-hello }"} {
		if !strings.Contains(string(yaml), want) {
			t.Errorf("app.yaml lacks %q", want)
		}
	}
	vite, _ := os.ReadFile(filepath.Join(dir, "admin/vite.config.ts"))
	if !strings.Contains(string(vite), "ADMIN_PREFIX") || !strings.Contains(string(vite), "elements.js") {
		t.Errorf("vite.config.ts:\n%s", vite)
	}
	svelte, _ := os.ReadFile(filepath.Join(dir, "admin/svelte.config.js"))
	if !strings.Contains(string(svelte), "customElement: true") {
		t.Errorf("svelte.config.js:\n%s", svelte)
	}
	pkg, _ := os.ReadFile(filepath.Join(dir, "admin/package.json"))
	if !strings.Contains(string(pkg), `"@hydrogencms/admin-sdk": "^0.1.0"`) {
		t.Errorf("package.json:\n%s", pkg)
	}
	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if !strings.Contains(string(readme), "not published yet") || !strings.Contains(string(readme), "ADMIN_PREFIX") {
		t.Errorf("README:\n%s", readme)
	}

	local := filepath.Join(t.TempDir(), "acme")
	if err := Write(Options{Dir: local, Core: "../.."}); err != nil {
		t.Fatal(err)
	}
	pkg, _ = os.ReadFile(filepath.Join(local, "admin/package.json"))
	if !strings.Contains(string(pkg), `"@hydrogencms/admin-sdk": "file:`) || !strings.Contains(string(pkg), "packages/admin-sdk") {
		t.Errorf("package.json with -core:\n%s", pkg)
	}
	readme, _ = os.ReadFile(filepath.Join(local, "README.md"))
	if strings.Contains(string(readme), "not published yet") {
		t.Errorf("README with -core:\n%s", readme)
	}
}
