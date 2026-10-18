package modcat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range All() {
		if seen[m.Name] {
			t.Errorf("duplicate %s", m.Name)
		}
		seen[m.Name] = true
		if !strings.HasPrefix(m.Package, Core+"/modules/") || m.Summary == "" {
			t.Errorf("%s: %+v", m.Name, m)
		}
		if m.GoModule != "" && m.GoModule != m.Package {
			t.Errorf("%s: separate module must be its package", m.Name)
		}
	}
	if got, ok := Lookup("images"); !ok || got.GoModule != "" || got.Docs != "docs/images.md" {
		t.Errorf("images: %+v %v", got, ok)
	}
	if got, _ := Lookup("sqlite"); got.Subdir() != "modules/sqlite" {
		t.Errorf("subdir %q", got.Subdir())
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("found nope")
	}
	if m, ok := ByPackage(Core + "/modules/pages"); !ok || m.Name != "pages" {
		t.Errorf("ByPackage: %+v", m)
	}
}

// Every catalog entry must exist in the repository and its docs page must exist.
func TestCatalogMatchesRepository(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, m := range All() {
		dir := filepath.Join(root, strings.TrimPrefix(m.Package, Core+"/"))
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("%s: %v", m.Name, err)
		}
		if m.GoModule != "" {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
				t.Errorf("%s: %v", m.Name, err)
			}
		}
		if m.Docs != "" {
			if _, err := os.Stat(filepath.Join(root, m.Docs)); err != nil {
				t.Errorf("%s docs: %v", m.Name, err)
			}
		}
	}
}
