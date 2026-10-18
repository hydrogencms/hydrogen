package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/internal/modcat"
	"github.com/hydrogencms/hydrogen/internal/projectfile"
)

const docsURL = "https://github.com/hydrogencms/hydrogen/blob/main/"

// project is a Hydrogen project directory with the Go tool's output going to stderr.
type project struct {
	ctx    context.Context
	dir    string
	stdout io.Writer
	stderr io.Writer
}

type modInfo struct {
	Require []struct{ Path, Version string }
	Replace []struct {
		Old, New struct{ Path, Version string }
	}
}

func (p project) goRun(args ...string) error {
	cmd := exec.CommandContext(p.ctx, "go", args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = p.dir, p.stderr, p.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (p project) modInfo() (modInfo, error) {
	var out bytes.Buffer
	cmd := exec.CommandContext(p.ctx, "go", "mod", "edit", "-json")
	cmd.Dir, cmd.Stdout, cmd.Stderr = p.dir, &out, p.stderr
	if err := cmd.Run(); err != nil {
		return modInfo{}, fmt.Errorf("go mod edit -json: %w", err)
	}
	var mi modInfo
	return mi, json.Unmarshal(out.Bytes(), &mi)
}

func (mi modInfo) required(path string) (string, bool) {
	for _, r := range mi.Require {
		if r.Path == path {
			return r.Version, true
		}
	}
	return "", false
}

// covered reports whether a required module contains the package path.
func (mi modInfo) covered(pkg string) bool {
	for _, r := range mi.Require {
		if pkg == r.Path || strings.HasPrefix(pkg, r.Path+"/") {
			return true
		}
	}
	return false
}

// localCore returns the directory of a local hydrogen checkout the project replaces the core with.
func (mi modInfo) localCore() (string, bool) {
	for _, r := range mi.Replace {
		if r.Old.Path == modcat.Core && r.New.Version == "" {
			return r.New.Path, true
		}
	}
	return "", false
}

func (mi modInfo) coreVersion() string {
	if v, ok := mi.required(modcat.Core); ok {
		return v
	}
	return "v0.0.0"
}

// snapshot records the files a module change may touch; the returned function puts them back.
func snapshot(dir string, names ...string) func() {
	type saved struct {
		path string
		data []byte
		ok   bool
	}
	var all []saved
	for _, n := range names {
		p := filepath.Join(dir, n)
		b, err := os.ReadFile(p)
		all = append(all, saved{p, b, err == nil})
	}
	return func() {
		for _, s := range all {
			if s.ok {
				_ = projectfile.WriteFileAtomic(s.path, s.data)
			} else {
				_ = os.Remove(s.path)
			}
		}
	}
}

type request struct{ name, version string }

func parseRequests(args []string) ([]request, error) {
	var out []request
	for _, a := range args {
		name, version := projectfile.SplitVersion(a)
		if name == "" {
			return nil, fmt.Errorf("invalid module %q", a)
		}
		if err := projectfile.Validate(name); err != nil {
			return nil, err
		}
		if _, ok := modcat.Lookup(name); ok && version != "" {
			return nil, fmt.Errorf("%s is a catalog module and is versioned with the core; drop @%s", name, version)
		}
		out = append(out, request{name, version})
	}
	return out, nil
}

// goModAdd makes go.mod right for the modules being added.
func (p project) goModAdd(reqs []request) error {
	mi, err := p.modInfo()
	if err != nil {
		return err
	}
	core, local := mi.localCore()
	for _, r := range reqs {
		if m, ok := modcat.Lookup(r.name); ok {
			if m.GoModule == "" {
				continue
			}
			if _, ok := mi.required(m.GoModule); ok {
				continue
			}
			if local {
				dir := path.Join(filepath.ToSlash(core), m.Subdir())
				if !filepath.IsAbs(dir) && !path.IsAbs(dir) && !strings.HasPrefix(dir, ".") {
					dir = "./" + dir
				}
				err = p.goRun("mod", "edit", "-require="+m.GoModule+"@"+mi.coreVersion(), "-replace="+m.GoModule+"="+dir)
			} else {
				err = p.goRun("get", m.GoModule+"@"+mi.coreVersion())
			}
		} else if r.version != "" {
			err = p.goRun("get", r.name+"@"+r.version)
		} else if !mi.covered(r.name) {
			err = p.goRun("get", r.name+"@latest")
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// apply writes hydrogen.yaml and modules_gen.go for the list, runs the go changes and go mod tidy, and
// restores every touched file when anything fails.
func (p project) apply(list []string, goSteps func() error) (err error) {
	restore := snapshot(p.dir, projectfile.FileName, projectfile.GenName, "go.mod", "go.sum")
	defer func() {
		if err != nil {
			restore()
		}
	}()
	gen, err := projectfile.Generate(list)
	if err != nil {
		return err
	}
	if goSteps != nil {
		if err = goSteps(); err != nil {
			return err
		}
	}
	if err = projectfile.WriteFileAtomic(filepath.Join(p.dir, projectfile.GenName), gen); err != nil {
		return err
	}
	if err = projectfile.WriteFileAtomic(filepath.Join(p.dir, projectfile.FileName), projectfile.Marshal(list)); err != nil {
		return err
	}
	return p.goRun("mod", "tidy")
}

func (p project) load() ([]string, error) {
	if !projectfile.Exists(p.dir) {
		return nil, errors.New("no hydrogen.yaml here; hydrogen modules init <names…> creates it from the modules your project uses")
	}
	return projectfile.Load(p.dir)
}

func (p project) describe(name string) {
	m, ok := modcat.Lookup(name)
	if !ok {
		return
	}
	if m.Docs != "" {
		fmt.Fprintf(p.stdout, "  docs: %s%s\n", docsURL, m.Docs)
	}
	if m.Config != "" {
		fmt.Fprintf(p.stdout, "  config: %s in config/app.yaml\n", m.Config)
	}
}

func openProject(ctx context.Context, stdout, stderr io.Writer) (project, error) {
	dir, err := projectDir()
	return project{ctx, dir, stdout, stderr}, err
}

func addModules(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("add: give module names (hydrogen modules lists the catalog) or Go package paths[@version]")
	}
	p, err := openProject(ctx, stdout, stderr)
	if err != nil {
		return err
	}
	reqs, err := parseRequests(args)
	if err != nil {
		return fmt.Errorf("add: %w", err)
	}
	list, err := p.load()
	if err != nil {
		return err
	}
	var todo []request
	for _, r := range reqs {
		switch {
		case slices.ContainsFunc(todo, func(o request) bool { return o.name == r.name }):
			return fmt.Errorf("add: %s given twice", r.name)
		case slices.Contains(list, r.name) && r.version == "":
			fmt.Fprintf(stdout, "%s is already in %s\n", r.name, projectfile.FileName)
		default:
			todo = append(todo, r)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	for _, r := range todo {
		if !slices.Contains(list, r.name) {
			list = append(list, r.name)
		}
	}
	if err := p.apply(list, func() error { return p.goModAdd(todo) }); err != nil {
		return fmt.Errorf("add: %w (nothing was changed)", err)
	}
	for _, r := range todo {
		fmt.Fprintf(stdout, "Added %s\n", r.name)
		p.describe(r.name)
	}
	return nil
}

func removeModules(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("remove: give module names or Go package paths")
	}
	p, err := openProject(ctx, stdout, stderr)
	if err != nil {
		return err
	}
	list, err := p.load()
	if err != nil {
		return err
	}
	for _, a := range args {
		if !slices.Contains(list, a) {
			return fmt.Errorf("remove: %s is not in %s", a, projectfile.FileName)
		}
		list = slices.DeleteFunc(list, func(e string) bool { return e == a })
	}
	// go mod tidy drops a requirement nothing imports any more; the local replace goes with it.
	if err := p.apply(list, nil); err != nil {
		return fmt.Errorf("remove: %w (nothing was changed)", err)
	}
	mi, err := p.modInfo()
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not check go.mod for leftover replace directives: %v\n", err)
	} else if _, local := mi.localCore(); local {
		for _, a := range args {
			m, ok := modcat.Lookup(a)
			if !ok || m.GoModule == "" {
				continue
			}
			if _, still := mi.required(m.GoModule); still {
				continue
			}
			if err := p.goRun("mod", "edit", "-dropreplace="+m.GoModule); err != nil {
				fmt.Fprintf(stderr, "warning: could not drop the replace for %s: %v\n", m.GoModule, err)
			}
		}
	}
	for _, a := range args {
		fmt.Fprintf(stdout, "Removed %s\n", a)
	}
	return nil
}

var useCall = regexp.MustCompile(`\.Use\(`)

// handWrittenUses finds h.Use calls in the project's own Go files.
func handWrittenUses(dir string) []string {
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	var out []string
	for _, f := range files {
		if filepath.Base(f) == projectfile.GenName || strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if useCall.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d: %s", filepath.Base(f), i+1, strings.TrimSpace(line)))
			}
		}
	}
	return out
}

func modulesInit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("modules init: give the modules the project uses, e.g. hydrogen modules init sqlite pages")
	}
	p, err := openProject(ctx, stdout, stderr)
	if err != nil {
		return err
	}
	if projectfile.Exists(p.dir) {
		return fmt.Errorf("modules init: %s already exists; use hydrogen add", projectfile.FileName)
	}
	reqs, err := parseRequests(args)
	if err != nil {
		return fmt.Errorf("modules init: %w", err)
	}
	var list []string
	for _, r := range reqs {
		if slices.Contains(list, r.name) {
			return fmt.Errorf("modules init: %s given twice", r.name)
		}
		list = append(list, r.name)
	}
	if err := p.apply(list, func() error { return p.goModAdd(reqs) }); err != nil {
		return fmt.Errorf("modules init: %w (nothing was changed)", err)
	}
	fmt.Fprintf(stdout, "Created %s and %s with: %s\n", projectfile.FileName, projectfile.GenName, strings.Join(list, ", "))
	fmt.Fprintln(stdout, "Wire it up in your Go code: call useModules(h) at the start of configure, and remove the hand-written")
	fmt.Fprintln(stdout, "h.Use calls for these modules (registering a module twice fails at startup):")
	uses := handWrittenUses(p.dir)
	if len(uses) == 0 {
		fmt.Fprintln(stdout, "  (none found)")
	}
	for _, u := range uses {
		fmt.Fprintln(stdout, "  "+u)
	}
	return nil
}

func modulesCmd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && args[0] == "init" {
		return modulesInit(ctx, args[1:], stdout, stderr)
	}
	if len(args) > 0 && (args[0] == "-versions" || args[0] == "--versions") {
		code, err := passthrough(ctx, []string{"modules"}, stdout, stderr)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("modules exited with status %d", code)
		}
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("modules: unknown argument %q (see hydrogen help)", args[0])
	}
	var used []string
	inProject, hasFile := false, false
	if dir, err := projectDir(); err == nil {
		inProject = true
		if hasFile = projectfile.Exists(dir); hasFile {
			if used, err = projectfile.Load(dir); err != nil {
				return err
			}
		}
	}
	for _, m := range modcat.All() {
		mark := " "
		if slices.Contains(used, m.Name) {
			mark = "*"
		}
		fmt.Fprintf(stdout, "%s %-12s %s\n", mark, m.Name, m.Summary)
	}
	for _, u := range used {
		if _, ok := modcat.Lookup(u); !ok {
			fmt.Fprintf(stdout, "* %s\n", u)
		}
	}
	switch {
	case !inProject:
	case !hasFile:
		fmt.Fprintf(stdout, "\nThis project has no %s; hydrogen modules init <names…> creates one.\n", projectfile.FileName)
	default:
		fmt.Fprintln(stdout, "\n* used by this project (hydrogen add / remove change the list)")
	}
	return nil
}

// buildCmd regenerates the module wiring and compiles the project into bin/<dir name>.
func buildCmd(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	p, err := openProject(ctx, stdout, stderr)
	if err != nil {
		return err
	}
	out := "bin" + string(filepath.Separator)
	var rest []string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-o" || a == "--o":
			if i+1 == len(args) {
				return errors.New("build: -o needs a path")
			}
			i++
			out = args[i]
		case strings.HasPrefix(a, "-o="):
			out = strings.TrimPrefix(a, "-o=")
		default:
			rest = append(rest, a)
		}
	}
	if _, err := projectfile.Sync(p.dir); err != nil {
		return err
	}
	intoDir := out != "" && (os.IsPathSeparator(out[len(out)-1]) || out[len(out)-1] == '/')
	if !filepath.IsAbs(out) {
		out = filepath.Join(p.dir, out)
	}
	if fi, err := os.Stat(out); intoDir || (err == nil && fi.IsDir()) {
		out = filepath.Join(out, filepath.Base(p.dir))
	}
	goos := os.Getenv("GOOS")
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos == "windows" && !strings.HasSuffix(out, ".exe") {
		out += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".next"
	goArgs := append([]string{"build", "-trimpath", "-o", tmp}, rest...)
	cmd := exec.CommandContext(ctx, "go", append(goArgs, ".")...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = p.dir, stderr, stderr
	cmd.Env = os.Environ()
	if os.Getenv("CGO_ENABLED") == "" {
		cmd.Env = append(cmd.Env, "CGO_ENABLED=0")
	}
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return errors.New("build failed")
	}
	if err := os.Rename(tmp, out); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	info, err := os.Stat(out)
	if err != nil {
		return err
	}
	shown := out
	if rel, err := filepath.Rel(p.dir, out); err == nil && !strings.HasPrefix(rel, "..") {
		shown = rel
	}
	fmt.Fprintf(stdout, "Built %s (%.1f MB)\n", shown, float64(info.Size())/(1<<20))
	if projectfile.Exists(p.dir) {
		list, _ := projectfile.Load(p.dir)
		fmt.Fprintf(stdout, "Modules: %s\n", strings.Join(list, ", "))
	} else {
		fmt.Fprintf(stdout, "Modules: registered in code (no %s)\n", projectfile.FileName)
	}
	return nil
}
