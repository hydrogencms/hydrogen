package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func keyOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func readAll(t *testing.T, rc io.ReadCloser) string {
	t.Helper()
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// testStorage is the contract every driver passes.
func testStorage(t *testing.T, st Storage) {
	ctx := context.Background()
	body := strings.Repeat("0123456789", 100)
	key := keyOf(body)

	if _, err := st.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat of a missing object: %v", err)
	}
	if _, _, err := st.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get of a missing object: %v", err)
	}
	if err := st.Delete(ctx, key); err != nil {
		t.Fatalf("delete of a missing object: %v", err)
	}
	if err := st.Put(ctx, "../etc/passwd", strings.NewReader("x"), 1, ""); err == nil {
		t.Fatal("a path as key was accepted")
	}

	if err := st.Put(ctx, key, strings.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	info, err := st.Stat(ctx, key)
	if err != nil || info.Size != int64(len(body)) {
		t.Fatalf("stat: %+v %v", info, err)
	}
	if time.Since(info.ModTime) > time.Hour || info.ModTime.Location() != time.UTC {
		t.Fatalf("mod time = %v", info.ModTime)
	}
	rc, info, err := st.Get(ctx, key)
	if err != nil || info.Size != int64(len(body)) || readAll(t, rc) != body {
		t.Fatalf("get: %+v %v", info, err)
	}
	for _, c := range []struct{ off, n int64 }{{0, 10}, {995, 5}, {500, 1}, {990, 100}, {0, 1000}} {
		rc, info, err := st.GetRange(ctx, key, c.off, c.n)
		if err != nil || info.Size != 1000 {
			t.Fatalf("range %v: %+v %v", c, info, err)
		}
		if got, want := readAll(t, rc), body[c.off:min(c.off+c.n, 1000)]; got != want {
			t.Fatalf("range %v = %q, want %q", c, got, want)
		}
	}
	rc, _, err = st.GetRange(ctx, key, 1000, 10)
	if err != nil || readAll(t, rc) != "" {
		t.Fatalf("range at the end: %v", err)
	}

	// Replacing an object, and a body of the wrong length.
	if err := st.Put(ctx, key, strings.NewReader("short"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	if rc, _, err = st.Get(ctx, key); err != nil || readAll(t, rc) != "short" {
		t.Fatalf("replaced object: %v", err)
	}
	if err := st.Put(ctx, keyOf("x"), strings.NewReader("abc"), 10, ""); err == nil {
		t.Fatal("a short body was accepted")
	}
	if _, err := st.Stat(ctx, keyOf("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a failed put left an object: %v", err)
	}

	// An empty object.
	empty := keyOf("")
	if err := st.Put(ctx, empty, bytes.NewReader(nil), 0, ""); err != nil {
		t.Fatal(err)
	}
	if info, err := st.Stat(ctx, empty); err != nil || info.Size != 0 {
		t.Fatalf("empty object: %+v %v", info, err)
	}

	// Listing: key order, prefix, after, limit.
	var keys []string
	for i := range 7 {
		k := keyOf(fmt.Sprint("list", i))
		keys = append(keys, k)
		if err := st.Put(ctx, k, strings.NewReader("v"), 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	all := append(slices.Clone(keys), key, empty)
	slices.Sort(all)
	got, err := st.List(ctx, "", "", 100)
	if err != nil || !slices.Equal(entryKeys(got), all) {
		t.Fatalf("list = %v, %v; want %v", entryKeys(got), err, all)
	}
	if got, _ := st.List(ctx, "", "", 3); !slices.Equal(entryKeys(got), all[:3]) {
		t.Fatalf("list limit: %v", entryKeys(got))
	}
	if got, _ := st.List(ctx, "", all[2], 3); !slices.Equal(entryKeys(got), all[3:6]) {
		t.Fatalf("list after: %v", entryKeys(got))
	}
	prefix := all[4][:3]
	var want []string
	for _, k := range all {
		if strings.HasPrefix(k, prefix) {
			want = append(want, k)
		}
	}
	if got, _ := st.List(ctx, prefix, "", 100); !slices.Equal(entryKeys(got), want) {
		t.Fatalf("list prefix %s: %v, want %v", prefix, entryKeys(got), want)
	}
	// Paging through everything in pages of 4 returns each object once.
	var paged []string
	for after := ""; ; {
		page, err := st.List(ctx, "", after, 4)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		paged = append(paged, entryKeys(page)...)
		after = page[len(page)-1].Key
	}
	if !slices.Equal(paged, all) {
		t.Fatalf("paged list = %v", paged)
	}
	if got, _ := st.List(ctx, "", "", 100); got[0].Size == 0 && got[0].Key != empty {
		t.Fatalf("entry size: %+v", got[0])
	}

	if err := st.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func entryKeys(es []Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Key
	}
	return out
}

func TestLocalStorage(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocal(filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	testStorage(t, l)
}

func TestLocalLayoutAndAtomicity(t *testing.T) {
	dir := t.TempDir()
	l, err := NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := keyOf("a")
	if err := l.Put(context.Background(), key, strings.NewReader("hello"), 5, ""); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, key[:2], key[2:4], key)); err != nil || string(b) != "hello" {
		t.Fatalf("sharded path: %q %v", b, err)
	}
	// A failed write leaves neither the object nor a temporary file.
	if err := l.Put(context.Background(), keyOf("b"), io.MultiReader(strings.NewReader("ab"), errReader{}), 10, ""); err == nil {
		t.Fatal("a failing reader was accepted")
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, ".tmp")); len(entries) != 0 {
		t.Fatalf("temporary files left: %v", entries)
	}
	if _, err := l.Stat(context.Background(), keyOf("b")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial object visible: %v", err)
	}
	// Stale temporary files are removed when the driver starts.
	old := filepath.Join(dir, ".tmp", "put-old")
	if err := os.WriteFile(old, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(old, past, past)
	if _, err := NewLocal(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale temporary file kept: %v", err)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestLocalListSkipsAhead(t *testing.T) {
	l, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var all []string
	for i := range 40 {
		k := keyOf(fmt.Sprint(i))
		all = append(all, k)
		if err := l.Put(context.Background(), k, strings.NewReader("v"), 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(all)
	for i := range all {
		got, err := l.List(context.Background(), "", all[i], 1)
		if i == len(all)-1 {
			if len(got) != 0 {
				t.Fatalf("after the last: %v", got)
			}
			continue
		}
		if err != nil || len(got) != 1 || got[0].Key != all[i+1] {
			t.Fatalf("after %s: %v %v", all[i], got, err)
		}
	}
}
