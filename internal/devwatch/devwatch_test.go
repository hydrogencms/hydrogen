package devwatch

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanWatchesLocalDependencies(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Setenv("GOPROXY", "off")
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dep, proj := filepath.Join(root, "dep"), filepath.Join(root, "proj")
	write(t, filepath.Join(dep, "go.mod"), "module example.com/dep\n\ngo 1.22\n")
	write(t, filepath.Join(dep, "lib", "lib.go"), "package lib\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar Data string\n")
	write(t, filepath.Join(dep, "lib", "lib_test.go"), "package lib\n")
	write(t, filepath.Join(dep, "lib", "data.txt"), "v1")
	write(t, filepath.Join(dep, "unused", "unused.go"), "package unused\n")
	write(t, filepath.Join(proj, "go.mod"),
		"module example.com/proj\n\ngo 1.22\n\nrequire example.com/dep v0.0.0\n\nreplace example.com/dep => ../dep\n")
	write(t, filepath.Join(proj, "main.go"), "package main\n\nimport \"example.com/dep/lib\"\n\nfunc main() { println(lib.Data) }\n")

	deps := localDeps(context.Background(), proj, local{})
	if want := []string{filepath.Join(dep, "lib")}; !slices.Equal(deps.dirs, want) {
		t.Fatalf("dirs = %v, want %v", deps.dirs, want)
	}
	before, err := scan(proj, deps)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"lib/lib.go", "lib/data.txt", "go.mod"} {
		if _, ok := before[filepath.Join(dep, f)]; !ok {
			t.Errorf("scan misses dep/%s: %v", f, before)
		}
	}
	for _, f := range []string{"lib/lib_test.go", "unused/unused.go"} {
		if _, ok := before[filepath.Join(dep, f)]; ok {
			t.Errorf("scan watches dep/%s", f)
		}
	}

	future := time.Now().Add(time.Minute)
	if err := os.Chtimes(filepath.Join(dep, "lib", "lib.go"), future, future); err != nil {
		t.Fatal(err)
	}
	after, err := scan(proj, deps)
	if err != nil {
		t.Fatal(err)
	}
	if same(before, after) {
		t.Error("an edit in a local dependency went unnoticed")
	}

	write(t, filepath.Join(proj, "go.mod"), "not a go.mod")
	if got := localDeps(context.Background(), proj, deps); !slices.Equal(got.dirs, deps.dirs) {
		t.Errorf("a broken go.mod dropped the watched dependencies: %v", got.dirs)
	}
}
