// Package devwatch is the live-reload loop behind `hydrogen dev`: build the project, run it, and on changes to
// Go code, configuration, schema or migrations rebuild and restart. A failed build keeps the old server. Packages of
// dependencies replaced by a local directory (a hydrogen checkout with `hydrogen new -core`) or belonging to another
// module of the go.work workspace (the examples in this repository) count as project code.
package devwatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/internal/projectfile"
)

type Options struct {
	Dir      string   // project directory
	Args     []string // command for the project binary, default ["serve"]
	Interval time.Duration
	Stdout   io.Writer
	Stderr   io.Writer
}

var watched = map[string]bool{".go": true, ".mod": true, ".sum": true, ".yaml": true, ".yml": true, ".sql": true}

var skipped = map[string]bool{".work": true, ".git": true, "node_modules": true}

type stamp struct {
	mod  time.Time
	size int64
}

// local is what the project builds from directories outside it: the package directories (their non-test Go files)
// and the embedded files and go.mod files of dependencies that a replace directive points at a local directory or
// that are other modules of the workspace.
type local struct {
	dirs, files []string
}

// localDeps lists the project's local dependencies with go list. On failure it returns prev, so a broken go.mod
// keeps watching what it watched before.
func localDeps(ctx context.Context, dir string, prev local) local {
	cmd := exec.CommandContext(ctx, "go", "list", "-e", "-deps", "-json=Dir,Module,EmbedFiles", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return prev
	}
	var l local
	mods := map[string]bool{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p struct {
			Dir        string
			EmbedFiles []string
			Module     *struct {
				Main    bool
				GoMod   string
				Replace *struct{ Version, GoMod string }
			}
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return l
		} else if err != nil {
			return prev
		}
		if p.Module == nil || p.Dir == "" || inside(dir, p.Dir) {
			continue
		}
		gomod := p.Module.GoMod
		if r := p.Module.Replace; r != nil && r.Version == "" {
			gomod = r.GoMod
		} else if !p.Module.Main {
			continue
		}
		l.dirs = append(l.dirs, p.Dir)
		for _, f := range p.EmbedFiles {
			l.files = append(l.files, filepath.Join(p.Dir, f))
		}
		if gomod != "" && !mods[gomod] {
			mods[gomod] = true
			l.files = append(l.files, gomod)
		}
	}
}

// inside reports whether path is dir or below it.
func inside(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// scan stamps the watched files under dir and the local dependencies' files. The project's gen/ directory is
// skipped: generation runs as part of every build, so its output never needs to trigger one.
func scan(dir string, deps local) (map[string]stamp, error) {
	out := map[string]stamp{}
	gen := filepath.Join(dir, "gen")
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != dir && (skipped[d.Name()] || p == gen) {
				return filepath.SkipDir
			}
			return nil
		}
		if !watched[filepath.Ext(p)] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // removed while walking
		}
		out[p] = stamp{info.ModTime(), info.Size()}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, d := range deps.dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue // removed: its importer no longer builds, or go list drops it next time
		}
		for _, e := range entries {
			if n := e.Name(); e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			if info, err := e.Info(); err == nil {
				out[filepath.Join(d, e.Name())] = stamp{info.ModTime(), info.Size()}
			}
		}
	}
	for _, f := range deps.files {
		if info, err := os.Stat(f); err == nil {
			out[f] = stamp{info.ModTime(), info.Size()}
		}
	}
	return out, nil
}

func same(a, b map[string]stamp) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func Run(ctx context.Context, o Options) error {
	if o.Interval <= 0 {
		o.Interval = 100 * time.Millisecond
	}
	if len(o.Args) == 0 {
		o.Args = []string{"serve"}
	}
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	// Builds alternate between two binaries and overwrite the one not running in place: macOS checks every new
	// executable file on its first start (about half a second), but not a rewritten one.
	bins := [2]string{filepath.Join(o.Dir, ".work", "bin", "dev-a"), filepath.Join(o.Dir, ".work", "bin", "dev-b")}
	slot := 0
	logf := func(format string, args ...any) { fmt.Fprintf(o.Stderr, "hydrogen dev: "+format+"\n", args...) }

	// Keep modules_gen.go in step with hydrogen.yaml first, so the baseline already contains it and an edit
	// restarts once. A bad hydrogen.yaml is reported here, once per edit.
	syncModules := func() error {
		if _, err := projectfile.Sync(o.Dir); err != nil {
			fmt.Fprintln(o.Stderr, err)
			return err
		}
		return nil
	}
	syncErr := syncModules()
	deps := localDeps(ctx, o.Dir, local{})
	if len(deps.dirs) > 0 {
		logf("watching %d packages of local dependencies too", len(deps.dirs))
	}
	// The baseline is taken before building, so a file saved during a build still counts as a change.
	state, err := scan(o.Dir, deps)
	if err != nil {
		return err
	}
	var proc *exec.Cmd
	var exited chan error
	start := func(bin string) {
		proc = exec.Command(bin, o.Args...)
		proc.Dir, proc.Stdout, proc.Stderr = o.Dir, o.Stdout, o.Stderr
		if err := proc.Start(); err != nil {
			logf("start failed: %v", err)
			proc = nil
			return
		}
		ch := make(chan error, 1)
		exited = ch
		go func(c *exec.Cmd) { ch <- c.Wait() }(proc)
	}
	stop := func() {
		if proc == nil {
			return
		}
		_ = proc.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = proc.Process.Kill()
			<-exited
		}
		proc = nil
	}
	defer stop()

	if syncErr != nil {
		logf("build failed; waiting for changes")
	} else if err := prepareAndGenerate(ctx, o.Dir, bins[slot], o.Stderr); err != nil {
		logf("build failed; waiting for changes")
	} else {
		start(bins[slot])
		slot = 1 - slot
	}
	tick := time.NewTicker(o.Interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-exited:
			exited = nil
			proc = nil
			logf("server exited (%v); waiting for changes", err)
			continue
		case <-tick.C:
		}
		next, err := scan(o.Dir, deps)
		if err != nil || same(state, next) {
			continue
		}
		// Let a burst of saves settle before building.
		for {
			time.Sleep(o.Interval)
			again, err := scan(o.Dir, deps)
			if err != nil || same(next, again) {
				break
			}
			next = again
		}
		logf("change detected; rebuilding")
		syncErr := syncModules()
		// Imports may have changed: list the local dependencies again before taking the new baseline.
		deps = localDeps(ctx, o.Dir, deps)
		if rescanned, err := scan(o.Dir, deps); err == nil {
			next = rescanned
		}
		state = next
		if syncErr != nil {
			logf("build failed; waiting for changes")
			continue
		}
		if err := prepareAndGenerate(ctx, o.Dir, bins[slot], o.Stderr); err != nil {
			if proc != nil {
				logf("build failed; the previous server keeps running")
			}
			continue
		}
		stop()
		start(bins[slot])
		slot = 1 - slot
	}
}

// Build compiles the project in dir to bin, replacing it only when the build succeeds.
func Build(ctx context.Context, dir, bin string, stderr io.Writer) error {
	return build(ctx, dir, bin, false, stderr)
}

// build is Build. inPlace (the dev loop) rewrites an existing bin instead of renaming over it, which a running bin
// can't survive, and links without DWARF (a third less link time) unless GOFLAGS sets -ldflags.
func build(ctx context.Context, dir, bin string, inPlace bool, stderr io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	tmp := bin + ".next"
	args := []string{"build", "-o", tmp}
	if inPlace && !strings.Contains(os.Getenv("GOFLAGS"), "-ldflags") {
		args = append(args, "-ldflags=-w")
	}
	cmd := exec.CommandContext(ctx, "go", append(args, ".")...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stderr, stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if inPlace && overwrite(tmp, bin) == nil {
		return os.Remove(tmp)
	}
	return os.Rename(tmp, bin)
}

// overwrite copies src over the existing file dst, keeping dst's inode.
func overwrite(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// IsProject reports whether dir looks like a Hydrogen project (a Go module importing hydrogen).
func IsProject(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && strings.Contains(string(b), "github.com/hydrogencms/hydrogen")
}
