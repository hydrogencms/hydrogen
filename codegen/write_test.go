package codegen

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/schema"
)

func TestWriteGo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "gen", "content")
	types, groups := fixture()
	n, err := WriteGo(dir, types, groups, GoOptions{})
	if err != nil || n != 6 {
		t.Fatalf("first write = %d, %v; want 6", n, err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	for _, f := range []string{"article.go", "doc.go"} {
		if err := os.Chtimes(filepath.Join(dir, f), past, past); err != nil {
			t.Fatal(err)
		}
	}
	if n, err = WriteGo(dir, types, groups, GoOptions{}); err != nil || n != 0 {
		t.Fatalf("second write = %d, %v; want 0", n, err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "article.go")); !fi.ModTime().Equal(past) {
		t.Error("unchanged file was rewritten")
	}

	hand := filepath.Join(dir, "hand.go")
	if err := os.WriteFile(hand, []byte("package content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Drop the person type: person.go is stale, hand.go stays, article.go is unchanged.
	if n, err = WriteGo(dir, types[1:], groups, GoOptions{}); err != nil || n != 1 {
		t.Fatalf("third write = %d, %v; want 1 (doc.go)", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "person.go")); !os.IsNotExist(err) {
		t.Errorf("stale person.go not removed: %v", err)
	}
	if _, err := os.Stat(hand); err != nil {
		t.Errorf("hand-written file removed: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

func TestWriteGoErrorWritesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "x")
	_, err := WriteGo(dir, []*schema.Type{{Name: "a b"}}, nil, GoOptions{})
	if err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("dir created on error: %v", err)
	}
}

func TestWriteTypeScript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gen", "types.ts")
	types, groups := fixture()
	if err := WriteTypeScript(path, types, groups, TSOptions{}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
	if err := WriteTypeScript(path, types, groups, TSOptions{}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); !fi.ModTime().Equal(past) {
		t.Error("unchanged file was rewritten")
	}
}
