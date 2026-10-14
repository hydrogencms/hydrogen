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
	for _, f := range []string{"go.mod", "main.go", "hydrogen.go", "config/app.yaml", "schema/.gitkeep", ".gitignore", "README.md"} {
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
