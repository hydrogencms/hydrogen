package kernel

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	"github.com/hydrogencms/hydrogen/internal/semver"
)

// CoreModule is the Go module path of the Hydrogen core.
const CoreModule = "github.com/hydrogencms/hydrogen"

// SkipCompatEnv, set to 1, turns off the RequiresCore check at start-up.
const SkipCompatEnv = "HYDROGEN_SKIP_COMPAT"

// CoreRequirer modules state which core versions they work with: a range of comparisons joined by commas
// (">=0.4.0, <0.6.0"; operators = != < <= > >=, optional v prefix). The kernel checks it at start-up and refuses
// to start on a mismatch, unless the core is a local build (devel) or HYDROGEN_SKIP_COMPAT=1. An invalid range
// is always an error.
type CoreRequirer interface {
	RequiresCore() string
}

// Origin lets a module name the Go package it should be reported under when its type lives elsewhere (the
// project module is a type of the core, but belongs to the project's main module).
type Origin interface {
	Origin() string
}

// ModuleInfo describes a registered module and the Go module it was built from.
type ModuleInfo struct {
	Name     string   `json:"name"`     // Module.Name()
	Package  string   `json:"package"`  // Go package path of the module's type
	Module   string   `json:"module"`   // Go module containing the package; empty when unknown
	Version  string   `json:"version"`  // that module's version; "devel" for the main module or a local replace
	Requires []string `json:"requires"` // Requires()
}

// BuildInfo returns the build information of the running binary. Options.BuildInfo replaces it in tests.
type BuildInfo func() (*debug.BuildInfo, bool)

// Modules describes the registered modules in registration order (required modules first).
func (a *App) Modules() []ModuleInfo { return a.info }

// ModuleNames lists the names of the registered modules in registration order.
func (a *App) ModuleNames() []string {
	names := make([]string, len(a.modules))
	for i, m := range a.modules {
		names[i] = m.Name()
	}
	return names
}

// CoreVersion is the version of the Hydrogen core this binary was built with ("devel" for a local build,
// "unknown" without build information).
func (a *App) CoreVersion() string { return a.coreVersion }

func describe(mods []Module, bi *debug.BuildInfo) []ModuleInfo {
	out := make([]ModuleInfo, len(mods))
	for i, m := range mods {
		info := ModuleInfo{Name: m.Name(), Package: packageOf(m, bi), Requires: []string{}}
		if r, ok := m.(Requirer); ok {
			info.Requires = append(info.Requires, r.Requires()...)
		}
		info.Module, info.Version = locate(bi, info.Package)
		out[i] = info
	}
	return out
}

// packageOf is the module's package path. A type in package main reports the main module's path (the package's
// real import path is not in the type); "command-line-arguments" (go run of files) is unknown.
func packageOf(m Module, bi *debug.BuildInfo) string {
	var pkg string
	if o, ok := m.(Origin); ok {
		pkg = o.Origin()
	} else {
		t := reflect.TypeOf(m)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		pkg = t.PkgPath()
	}
	switch pkg {
	case "main":
		if bi != nil {
			return bi.Main.Path
		}
		return ""
	case "command-line-arguments":
		return ""
	}
	return pkg
}

// DescribeModules orders and describes mods without registering them or opening anything, for the modules
// command. A RequiresCore mismatch comes back as compat, not as err, so the list can still be shown; err is an
// ordering problem.
func DescribeModules(mods []Module, bi BuildInfo) (infos []ModuleInfo, compat error, err error) {
	if bi == nil {
		bi = debug.ReadBuildInfo
	}
	info, _ := bi()
	ordered, err := order(mods)
	if err != nil {
		return nil, nil, err
	}
	infos = describe(ordered, info)
	return infos, checkCompat(ordered, infos, coreVersionOf(info), skipCompat(&Options{})), nil
}

// locate finds the Go module that holds a package: the longest module path that prefixes it.
func locate(bi *debug.BuildInfo, pkg string) (module, version string) {
	if bi == nil || pkg == "" {
		return "", "unknown"
	}
	best := -1
	mods := append([]*debug.Module{&bi.Main}, bi.Deps...)
	for i, d := range mods {
		if d.Path == "" || (pkg != d.Path && !strings.HasPrefix(pkg, d.Path+"/")) {
			continue
		}
		if best < 0 || len(d.Path) > len(mods[best].Path) {
			best = i
		}
	}
	if best < 0 {
		return "", "unknown"
	}
	return mods[best].Path, versionOf(mods[best])
}

func versionOf(d *debug.Module) string {
	if d.Replace != nil {
		if d.Replace.Version != "" && d.Replace.Version != "(devel)" {
			return d.Replace.Version
		}
		return "devel (" + d.Replace.Path + ")"
	}
	return develOr(d.Version)
}

func develOr(v string) string {
	if v == "" || v == "(devel)" || strings.HasPrefix(v, "v0.0.0-") {
		return "devel"
	}
	return v
}

func coreVersionOf(bi *debug.BuildInfo) string {
	if bi == nil {
		return "unknown"
	}
	if bi.Main.Path == CoreModule {
		return versionOf(&bi.Main)
	}
	for _, d := range bi.Deps {
		if d.Path == CoreModule {
			return versionOf(d)
		}
	}
	return "unknown"
}

// checkCompat enforces CoreRequirer ranges against the core version.
func checkCompat(mods []Module, infos []ModuleInfo, core string, skip bool) error {
	local := skip || core == "unknown" || strings.HasPrefix(core, "devel")
	var cv semver.Version
	if !local {
		v, err := semver.Parse(core)
		if err != nil {
			local = true // not a release version (a fork or a pseudo-version we can't order): nothing to compare
		}
		cv = v
	}
	for i, m := range mods {
		cr, ok := m.(CoreRequirer)
		if !ok {
			continue
		}
		text := cr.RequiresCore()
		r, err := semver.ParseRange(text)
		if err != nil {
			return fmt.Errorf("module %s: RequiresCore: %w", m.Name(), err)
		}
		if local || r.Contains(cv) {
			continue
		}
		from := infos[i].Module
		if from != "" {
			from = fmt.Sprintf(" (%s %s)", from, infos[i].Version)
		}
		return fmt.Errorf("module %s%s requires hydrogen core %s, but this build has %s; use a version of the module "+
			"that matches this core, or update the core (%s=1 skips this check)", m.Name(), from, r, core, SkipCompatEnv)
	}
	return nil
}

func skipCompat(o *Options) bool {
	return o.SkipCompat || os.Getenv(SkipCompatEnv) == "1"
}

// WriteModules prints modules as an aligned table: name, version, Go module path.
func WriteModules(w io.Writer, infos []ModuleInfo) error {
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVERSION\tMODULE")
	for _, m := range infos {
		mod := m.Module
		if mod == "" {
			mod = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", m.Name, m.Version, mod)
	}
	return tw.Flush()
}

func modulesCommand() Command {
	return Command{
		Name:    "modules",
		Summary: "list the modules in this build with their versions",
		Run: func(_ context.Context, app *App, args []string) error {
			if len(args) > 0 {
				return fmt.Errorf("modules takes no arguments")
			}
			return WriteModules(app.stdout, app.info)
		},
	}
}

// SystemInfo is the system report served by GET /api/manage/v1/system.
type SystemInfo struct {
	Core     CoreInfo     `json:"core"`
	Modules  []ModuleInfo `json:"modules"`
	Database string       `json:"database"` // the adapter's dialect: sqlite or postgres
	Search   string       `json:"search"`   // the search engine's name
	Media    bool         `json:"media"`    // whether uploads are enabled
}

// CoreInfo is the core's version and the Go toolchain it was built with.
type CoreInfo struct {
	Version string `json:"version"`
	Go      string `json:"go"`
}

// System reports the versions and what the build uses.
func (a *App) System() SystemInfo {
	s := a.services
	return SystemInfo{
		Core:     CoreInfo{Version: a.coreVersion, Go: runtime.Version()},
		Modules:  a.info,
		Database: s.DB.Dialect(),
		Search:   s.Search.Name(),
		Media:    s.Media != nil,
	}
}
