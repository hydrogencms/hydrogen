package projectfile

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

func TestParse(t *testing.T) {
	in := "# comment\nmodules:\n  - sqlite\n  - images\n  - github.com/acme/hydrogen-stripe\n"
	got, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"sqlite", "images", "github.com/acme/hydrogen-stripe"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got, err := Parse(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty file: %v %v", got, err)
	}
	if got, err := Parse(Marshal(nil)); err != nil || len(got) != 0 {
		t.Fatalf("empty list: %v %v", got, err)
	}
}

func TestParseErrors(t *testing.T) {
	for in, want := range map[string]string{
		"modules: [sqlite, sqlite]": "listed twice",
		"modules: [nope]":           "unknown module",
		"modules: [sqlite@v1]":      "versions belong in go.mod",
		"modules: [github.com/hydrogencms/hydrogen/modules/images]": "catalog module",
		"modules: [acme/x]":           "unknown module",
		"modules: [example.com/../x]": "invalid module path",
		"modules: [sqlite]\nextra: 1": "field extra not found",
		"modulez: [sqlite]":           "field modulez not found",
		"modules: [\"\"]":             "unknown module",
		"modules: {a: b}":             "hydrogen.yaml",
	} {
		_, err := Parse([]byte(in))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", in, err, want)
		}
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	mods := []string{"postgres", "sqlite", "example.com/x/y"}
	got, err := Parse(Marshal(mods))
	if err != nil || !reflect.DeepEqual(got, mods) {
		t.Fatalf("%v %v", got, err)
	}
}

func TestGenerateGolden(t *testing.T) {
	cases := map[string][]string{
		"empty": nil,
		"first": {"sqlite", "postgres", "images"},
		"third": {"images", "github.com/acme/hydrogen-stripe", "example.com/other/images", "gitlab.com/x/stripe/v2", "example.com/go-sqlite", "example.com/type"},
	}
	for name, mods := range cases {
		got, err := Generate(mods)
		if err != nil {
			t.Fatal(name, err)
		}
		path := filepath.Join("testdata", name+".golden")
		if *update {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}
	}
}

func TestGenerateDeterministicAndUnique(t *testing.T) {
	mods := []string{"example.com/a/images", "images", "example.com/b/images", "example.com/c/type"}
	a, err := Generate(mods)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Generate(mods)
	if string(a) != string(b) {
		t.Fatal("not deterministic")
	}
	// The catalog module keeps its plain name even when a third-party module comes first.
	if !strings.Contains(string(a), "\t\"github.com/hydrogencms/hydrogen/modules/images\"\n") {
		t.Errorf("catalog import was renamed:\n%s", a)
	}
	if !strings.Contains(string(a), "images2 \"example.com/a/images\"") || !strings.Contains(string(a), "images3 \"example.com/b/images\"") || !strings.Contains(string(a), "typemod \"example.com/c/type\"") {
		t.Errorf("aliases:\n%s", a)
	}
}

func TestSync(t *testing.T) {
	dir := t.TempDir()
	if changed, err := Sync(dir); err != nil || changed {
		t.Fatalf("no hydrogen.yaml: %v %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, GenName)); err == nil {
		t.Fatal("generated without hydrogen.yaml")
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), Marshal([]string{"sqlite"}), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := Sync(dir); err != nil || !changed {
		t.Fatalf("first sync: %v %v", changed, err)
	}
	if changed, err := Sync(dir); err != nil || changed {
		t.Fatalf("second sync: %v %v", changed, err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("modules: [bad]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(dir); err == nil {
		t.Fatal("bad hydrogen.yaml accepted")
	}
}

func TestReservedAliases(t *testing.T) {
	mods := []string{"example.com/a/main", "example.com/a/init", "example.com/a/configure", "example.com/a/string", "example.com/a/func", "example.com/b/string", "example.com/a/h", "example.com/a/hydrogen"}
	got, err := Generate(mods)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`mainmod "example.com/a/main"`, `initmod "example.com/a/init"`, `configuremod "example.com/a/configure"`,
		`stringmod "example.com/a/string"`, `funcmod "example.com/a/func"`, `stringmod2 "example.com/b/string"`, `hmod "example.com/a/h"`, `hydrogenmod "example.com/a/hydrogen"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("missing %s in:\n%s", want, got)
		}
	}
}
