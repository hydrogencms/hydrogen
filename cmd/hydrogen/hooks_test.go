package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}

type fakeTools struct {
	hookTools
	calls    []call
	missing  map[string]bool
	env      map[string]string
	failOn   string
	noOutput bool
}

func newFakeTools() *fakeTools {
	f := &fakeTools{missing: map[string]bool{}, env: map[string]string{}}
	f.lookPath = func(file string) (string, error) {
		if f.missing[file] {
			return "", errors.New("not found")
		}
		return "/bin/" + file, nil
	}
	f.getenv = func(k string) string { return f.env[k] }
	f.run = func(_ context.Context, _, name string, args []string, _, _ io.Writer) error {
		f.calls = append(f.calls, call{name, args})
		for i, a := range args {
			if a == "-o" && i+1 < len(args) && !f.noOutput {
				if err := os.WriteFile(args[i+1], []byte("wasm"), 0o644); err != nil {
					return err
				}
			}
		}
		if f.failOn != "" && (name == f.failOn || (len(args) > 1 && args[1] == f.failOn)) {
			return errors.New("exit status 1")
		}
		return nil
	}
	return f
}

func hookProject(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("export {}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func buildErr(f *fakeTools, dir string, files ...string) (string, error) {
	var out bytes.Buffer
	err := hooksBuild(context.Background(), f.hookTools, dir, files, &out, io.Discard)
	return out.String(), err
}

func TestHooksBuildDefaultsToHooksTS(t *testing.T) {
	dir := hookProject(t, "hooks/b.ts", "hooks/a.ts", "hooks/types.d.ts", "hooks/note.md")
	f := newFakeTools()
	f.env["JAVY"] = "/opt/javy"
	out, err := buildErr(f, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "built hooks/a.wasm") || !strings.Contains(out, "built hooks/b.wasm") || strings.Contains(out, "types") {
		t.Errorf("output:\n%s", out)
	}
	var javy []call
	for _, c := range f.calls {
		if c.name == "/bin//opt/javy" || strings.HasSuffix(c.name, "javy") {
			javy = append(javy, c)
		}
	}
	if len(javy) != 2 || javy[0].args[0] != "build" || !strings.HasSuffix(javy[0].args[3], "a.wasm") || !strings.Contains(strings.Join(javy[0].args, " "), "javy-stream-io") {
		t.Errorf("javy calls: %v", javy)
	}
	esb := f.calls[1]
	if esb.name != "npx" || esb.args[0] != "--no-install" || !strings.Contains(strings.Join(esb.args, " "), "--bundle --format=esm --platform=neutral") {
		t.Errorf("esbuild call: %v", esb)
	}
}

func TestHooksBuildGivenFiles(t *testing.T) {
	dir := hookProject(t, "hooks/a.ts")
	_ = os.MkdirAll(filepath.Join(dir, "src"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "src", "x.ts"), nil, 0o644)
	out, err := buildErr(newFakeTools(), dir, "src/x.ts")
	if err != nil || !strings.Contains(out, "built hooks/x.wasm") {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestHooksBuildErrors(t *testing.T) {
	dir := hookProject(t, "hooks/a.ts")
	for _, tc := range []struct {
		name  string
		setup func(*fakeTools)
		files []string
		dir   string
		want  string
	}{
		{"no node", func(f *fakeTools) { f.missing["node"] = true }, nil, dir, "node is not installed"},
		{"no npx", func(f *fakeTools) { f.missing["npx"] = true }, nil, dir, "npx is not installed"},
		{"no esbuild", func(f *fakeTools) { f.failOn = "esbuild" }, nil, dir, "npm i -D esbuild @hydrogencms/hook"},
		{"no javy", func(f *fakeTools) { f.missing["javy"] = true }, nil, dir, "javy not found"},
		{"bad JAVY", func(f *fakeTools) { f.env["JAVY"] = "/nope"; f.missing["/nope"] = true }, nil, dir, "JAVY"},
		{"javy fails", func(f *fakeTools) { f.failOn = "/bin/javy" }, nil, dir, "javy failed for"},
		{"missing file", func(*fakeTools) {}, []string{"hooks/zzz.ts"}, dir, "zzz.ts is not a file"},
		{"flag", func(*fakeTools) {}, []string{"-x"}, dir, "unknown flag"},
		{"equals sign", func(*fakeTools) {}, []string{"hooks/a=b.ts"}, dir, "name=path"},
		{"nothing", func(*fakeTools) {}, nil, t.TempDir(), "no hooks/*.ts"},
		{"same name", func(*fakeTools) {}, []string{"hooks/a.ts", "hooks/../hooks/a.ts"}, dir, "would both build"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeTools()
			tc.setup(f)
			_, err := buildErr(f, tc.dir, tc.files...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestHooksBuildLeavesNoPartialFile(t *testing.T) {
	dir := hookProject(t, "hooks/a.ts")
	f := newFakeTools()
	f.failOn = "/bin/javy"
	if _, err := buildErr(f, dir); err == nil {
		t.Fatal("want error")
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "hooks", "*wasm*")); len(m) != 0 {
		t.Errorf("leftovers: %v", m)
	}
	f = newFakeTools()
	if _, err := buildErr(f, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hooks", "a.wasm")); string(b) != "wasm" {
		t.Errorf("a.wasm = %q", b)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "hooks", ".*")); len(m) != 0 {
		t.Errorf("temp left: %v", m)
	}
}

func TestHooksSubcommand(t *testing.T) {
	failHy(t, "usage: hydrogen hooks build", "hooks")
	failHy(t, "usage: hydrogen hooks build", "hooks", "frob")
}
