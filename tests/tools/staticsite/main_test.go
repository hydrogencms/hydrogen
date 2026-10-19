package main

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestStaticContentIsCurrent regenerates the starter content and compares it with the committed directory.
func TestStaticContentIsCurrent(t *testing.T) {
	got := filepath.Join(t.TempDir(), "content")
	if err := run(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs("../../../internal/scaffold/static/content")
	if err != nil {
		t.Fatal(err)
	}
	read := func(dir string) map[string][]byte {
		files := map[string][]byte{}
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			rel, _ := filepath.Rel(dir, p)
			files[rel] = b
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return files
	}
	g, w := read(got), read(want)
	for name, b := range g {
		if !bytes.Equal(b, w[name]) {
			t.Errorf("internal/scaffold/static/content/%s is stale: run make static-content", name)
		}
	}
	for name := range w {
		if _, ok := g[name]; !ok {
			t.Errorf("internal/scaffold/static/content/%s is not generated: run make static-content", name)
		}
	}
}
