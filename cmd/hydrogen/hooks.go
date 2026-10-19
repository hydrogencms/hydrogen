package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// javyFlags turn on the stream I/O and TextEncoder/TextDecoder that @hydrogencms/hook uses (Javy 9).
var javyFlags = []string{"-J", "javy-stream-io", "-J", "text-encoding"}

// hookTools is what `hooks build` needs from the machine; tests replace it.
type hookTools struct {
	lookPath func(file string) (string, error)
	getenv   func(key string) string
	// run executes a program in dir with its output on stderr.
	run func(ctx context.Context, dir, name string, args []string, stdout, stderr io.Writer) error
}

func systemHookTools() hookTools {
	return hookTools{lookPath: exec.LookPath, getenv: os.Getenv, run: func(ctx context.Context, dir, name string, args []string, stdout, stderr io.Writer) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stdout, stderr
		return cmd.Run()
	}}
}

func hooksCmd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "build" {
		return errors.New("hooks: usage: hydrogen hooks build [files…]")
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	return hooksBuild(ctx, systemHookTools(), dir, args[1:], stdout, stderr)
}

// hooksBuild compiles TypeScript (or JavaScript) hooks to WebAssembly: esbuild bundles each file into one ES
// module, Javy turns that into hooks/<name>.wasm. Without files it builds hooks/*.ts.
func hooksBuild(ctx context.Context, t hookTools, dir string, files []string, stdout, stderr io.Writer) error {
	for _, f := range files {
		if strings.HasPrefix(f, "-") {
			return fmt.Errorf("hooks build: unknown flag %s (usage: hydrogen hooks build [files…])", f)
		}
	}
	for _, f := range files {
		if strings.Contains(f, "=") {
			return fmt.Errorf("hooks build: %s contains \"=\", which esbuild would read as name=path; rename the file", f)
		}
	}
	if len(files) == 0 {
		matches, err := filepath.Glob(filepath.Join(dir, "hooks", "*.ts"))
		if err != nil {
			return err
		}
		for _, m := range matches {
			if !strings.HasSuffix(m, ".d.ts") {
				files = append(files, m)
			}
		}
		if len(files) == 0 {
			return errors.New("hooks build: no hooks/*.ts files here; give the files to build")
		}
	}
	for _, f := range files {
		p := f
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return fmt.Errorf("hooks build: %s is not a file", f)
		}
	}
	names := map[string]string{}
	for _, f := range files {
		base := filepath.Base(f)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if prev, dup := names[name]; dup {
			return fmt.Errorf("hooks build: %s and %s would both build hooks/%s.wasm", prev, f, name)
		}
		names[name] = f
	}

	for _, tool := range []string{"node", "npx"} {
		if _, err := t.lookPath(tool); err != nil {
			return fmt.Errorf("hooks build: %s is not installed or not on PATH (Node 20+ is needed to bundle TypeScript)", tool)
		}
	}
	if err := t.run(ctx, dir, "npx", []string{"--no-install", "esbuild", "--version"}, io.Discard, io.Discard); err != nil {
		return errors.New("hooks build: esbuild is not installed in this project; run: npm i -D esbuild @hydrogencms/hook")
	}
	javy := t.getenv("JAVY")
	if javy == "" {
		javy = "javy"
	}
	javyPath, err := t.lookPath(javy)
	if err != nil {
		return errors.New("hooks build: javy not found: install a Javy release (github.com/bytecodealliance/javy; scripts/javy.sh in the Hydrogen repository downloads one), put it on PATH or set JAVY=/path/to/javy")
	}

	tmp, err := os.MkdirTemp("", "hydrogen-hooks-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	outDir := filepath.Join(dir, "hooks")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(names))
	for n := range names {
		keys = append(keys, n)
	}
	slices.Sort(keys)
	for _, name := range keys {
		src := names[name]
		js := filepath.Join(tmp, name+".js")
		err := t.run(ctx, dir, "npx", []string{"--no-install", "esbuild", src, "--bundle", "--format=esm", "--platform=neutral", "--log-level=warning", "--outfile=" + js}, stdout, stderr)
		if err != nil {
			return fmt.Errorf("hooks build: esbuild failed for %s: %w", src, err)
		}
		wasm := filepath.Join(outDir, name+".wasm")
		part := filepath.Join(tmp, name+".wasm")
		if err := t.run(ctx, dir, javyPath, append([]string{"build", js, "-o", part}, javyFlags...), stdout, stderr); err != nil {
			return fmt.Errorf("hooks build: javy failed for %s: %w", src, err)
		}
		if err := moveFile(part, wasm); err != nil {
			return fmt.Errorf("hooks build: %w", err)
		}
		rel, rerr := filepath.Rel(dir, wasm)
		if rerr != nil {
			rel = wasm
		}
		fmt.Fprintf(stdout, "built %s\n", rel)
	}
	return nil
}

// moveFile puts src at dst atomically: it is copied next to dst first, so a reader never sees a partial file.
func moveFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("javy produced no output for %s: %w", dst, err)
	}
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
