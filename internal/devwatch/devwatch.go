// Package devwatch is the live-reload loop behind `hydrogen dev`: build the project, run it, and on changes to
// Go code, configuration, schema or migrations rebuild and restart. A failed build keeps the old server.
package devwatch

import (
	"context"
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

// scan stamps the watched files under dir. The project's gen/ directory is skipped: generation runs as part
// of every build, so its output never needs to trigger one.
func scan(dir string) (map[string]stamp, error) {
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
	return out, err
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
		o.Interval = 400 * time.Millisecond
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
	bin := filepath.Join(o.Dir, ".work", "bin", "app")
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
	// The baseline is taken before building, so a file saved during a build still counts as a change.
	state, err := scan(o.Dir)
	if err != nil {
		return err
	}
	var proc *exec.Cmd
	var exited chan error
	start := func() {
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
	} else if err := prepareAndGenerate(ctx, o.Dir, bin, o.Stderr); err != nil {
		logf("build failed; waiting for changes")
	} else {
		start()
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
		next, err := scan(o.Dir)
		if err != nil || same(state, next) {
			continue
		}
		// Let a burst of saves settle before building.
		for {
			time.Sleep(o.Interval)
			again, err := scan(o.Dir)
			if err != nil || same(next, again) {
				break
			}
			next = again
		}
		logf("change detected; rebuilding")
		syncErr := syncModules()
		if rescanned, err := scan(o.Dir); err == nil {
			next = rescanned
		}
		state = next
		if syncErr != nil {
			logf("build failed; waiting for changes")
			continue
		}
		if err := prepareAndGenerate(ctx, o.Dir, bin, o.Stderr); err != nil {
			if proc != nil {
				logf("build failed; the previous server keeps running")
			}
			continue
		}
		stop()
		start()
	}
}

// Build compiles the project in dir to bin, replacing it only when the build succeeds.
func Build(ctx context.Context, dir, bin string, stderr io.Writer) error {
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		return err
	}
	tmp := bin + ".next"
	cmd := exec.CommandContext(ctx, "go", "build", "-o", tmp, ".")
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stderr, stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	return os.Rename(tmp, bin)
}

// IsProject reports whether dir looks like a Hydrogen project (a Go module importing hydrogen).
func IsProject(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	return err == nil && strings.Contains(string(b), "github.com/hydrogencms/hydrogen")
}
