package kernel

import (
	"bytes"
	"context"
	"reflect"
	"runtime/debug"
	"strings"
	"testing"
)

type verMod struct {
	chanMod
	core string
}

func (m verMod) RequiresCore() string { return m.core }

type ptrMod struct{ name string }

func (m *ptrMod) Name() string             { return m.name }
func (m *ptrMod) Register(*Registry) error { return nil }
func (m *ptrMod) Requires() []string       { return []string{"base"} }

// originMod claims a package path, as the project module does.
type originMod struct {
	chanMod
	pkg string
}

func (m originMod) Origin() string { return m.pkg }

func fakeBuild() *debug.BuildInfo {
	return &debug.BuildInfo{
		Main: debug.Module{Path: "example.com/acme", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: "github.com/hydrogencms/hydrogen", Version: "v0.5.1"},
			{Path: "github.com/hydrogencms/hydrogen/modules/sqlite", Version: "v0.5.0"},
			{Path: "github.com/acme/hydrogen-stripe", Version: "v1.2.0"},
			{Path: "github.com/acme/hydrogen-local", Version: "v0.0.0",
				Replace: &debug.Module{Path: "../hydrogen-local"}},
			{Path: "github.com/acme/hydrogen-fork", Version: "v1.0.0",
				Replace: &debug.Module{Path: "github.com/me/hydrogen-fork", Version: "v1.0.1"}},
			{Path: "github.com/acme/pseudo", Version: "v0.0.0-20261018101500-abcdef123456"},
		},
	}
}

func TestLocate(t *testing.T) {
	bi := fakeBuild()
	for _, tc := range []struct{ pkg, module, version string }{
		{"example.com/acme", "example.com/acme", "devel"},
		{"example.com/acme/internal/shop", "example.com/acme", "devel"},
		{"github.com/hydrogencms/hydrogen/modules/images", "github.com/hydrogencms/hydrogen", "v0.5.1"},
		{"github.com/hydrogencms/hydrogen", "github.com/hydrogencms/hydrogen", "v0.5.1"},
		// The longest module path wins: sqlite is its own module inside the core's path.
		{"github.com/hydrogencms/hydrogen/modules/sqlite", "github.com/hydrogencms/hydrogen/modules/sqlite", "v0.5.0"},
		{"github.com/acme/hydrogen-stripe/checkout", "github.com/acme/hydrogen-stripe", "v1.2.0"},
		{"github.com/acme/hydrogen-local", "github.com/acme/hydrogen-local", "devel (../hydrogen-local)"},
		{"github.com/acme/hydrogen-fork", "github.com/acme/hydrogen-fork", "v1.0.1"},
		{"github.com/acme/pseudo", "github.com/acme/pseudo", "devel"},
		// A path that only shares a prefix with a module is not inside it.
		{"github.com/acme/hydrogen-stripeish", "", "unknown"},
		{"", "", "unknown"},
	} {
		if m, v := locate(bi, tc.pkg); m != tc.module || v != tc.version {
			t.Errorf("locate(%q) = %q, %q; want %q, %q", tc.pkg, m, v, tc.module, tc.version)
		}
	}
	if m, v := locate(nil, "example.com/acme"); m != "" || v != "unknown" {
		t.Errorf("without build info: %q, %q", m, v)
	}
}

func TestCoreVersion(t *testing.T) {
	if got := coreVersionOf(fakeBuild()); got != "v0.5.1" {
		t.Errorf("dependency: %q", got)
	}
	main := &debug.BuildInfo{Main: debug.Module{Path: CoreModule, Version: "(devel)"}}
	if got := coreVersionOf(main); got != "devel" {
		t.Errorf("main module: %q", got)
	}
	replaced := &debug.BuildInfo{Main: debug.Module{Path: "example.com/acme"},
		Deps: []*debug.Module{{Path: CoreModule, Version: "v0.0.0", Replace: &debug.Module{Path: "../hydrogen"}}}}
	if got := coreVersionOf(replaced); got != "devel (../hydrogen)" {
		t.Errorf("replaced: %q", got)
	}
	if got := coreVersionOf(&debug.BuildInfo{Main: debug.Module{Path: "example.com/acme"}}); got != "unknown" {
		t.Errorf("absent: %q", got)
	}
	if got := coreVersionOf(nil); got != "unknown" {
		t.Errorf("nil: %q", got)
	}
}

func TestDescribe(t *testing.T) {
	bi := fakeBuild()
	mods := []Module{
		&ptrMod{"stripe"},
		originMod{chanMod{name: "project"}, "example.com/acme"},
		verMod{chanMod: chanMod{name: "core-mod"}, core: ">=0.5.0"},
	}
	got := describe(mods, bi)
	// The test types live in the core module (this package), which the fake build lists as a dependency.
	pkg := reflect.TypeOf(chanMod{}).PkgPath()
	want := []ModuleInfo{
		{Name: "stripe", Package: pkg, Module: CoreModule, Version: "v0.5.1", Requires: []string{"base"}},
		{Name: "project", Package: "example.com/acme", Module: "example.com/acme", Version: "devel", Requires: []string{}},
		{Name: "core-mod", Package: pkg, Module: CoreModule, Version: "v0.5.1", Requires: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("describe:\n got %+v\nwant %+v", got, want)
	}
	if got := describe(mods[:1], nil)[0]; got.Module != "" || got.Version != "unknown" {
		t.Errorf("without build info: %+v", got)
	}
}

func TestCheckCompat(t *testing.T) {
	infos := func(ms []Module) []ModuleInfo { return describe(ms, fakeBuild()) }
	ok := verMod{chanMod{name: "ok"}, ">=0.4.0, <0.6.0"}
	bad := verMod{chanMod{name: "stripe"}, ">=0.6.0"}
	plain := chanMod{name: "plain"}
	for _, tc := range []struct {
		name    string
		mods    []Module
		core    string
		skip    bool
		wantErr string
	}{
		{"pass", []Module{ok, plain}, "v0.5.1", false, ""},
		{"no requirement", []Module{plain}, "v0.1.0", false, ""},
		{"fail", []Module{ok, bad}, "v0.5.1", false, `module stripe (` + CoreModule + ` v0.5.1) requires hydrogen core >=0.6.0, but this build has v0.5.1`},
		{"fail upper bound", []Module{ok}, "v0.6.0", false, "requires hydrogen core >=0.4.0, <0.6.0, but this build has v0.6.0"},
		{"skipped by env", []Module{bad}, "v0.5.1", true, ""},
		{"devel core", []Module{bad}, "devel", false, ""},
		{"replaced core", []Module{bad}, "devel (../hydrogen)", false, ""},
		{"unknown core", []Module{bad}, "unknown", false, ""},
		{"invalid range, devel core", []Module{verMod{chanMod{name: "oops"}, "~0.5"}}, "devel", false, `module oops: RequiresCore: invalid version range "~0.5"`},
		{"invalid range, skipped", []Module{verMod{chanMod{name: "oops"}, ""}}, "v0.5.1", true, "module oops: RequiresCore: empty version range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkCompat(tc.mods, infos(tc.mods), tc.core, tc.skip)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestCompatChecksAtPrepare(t *testing.T) {
	cfg := project(t, "")
	bad := verMod{chanMod{name: "stripe", db: true}, ">=0.6.0"}
	bi := fakeBuild()
	_, err := prepareWith(cfg, []Module{bad}, true, buildEnv{bi: bi})
	if err == nil || !strings.Contains(err.Error(), "module stripe") || !strings.Contains(err.Error(), SkipCompatEnv) {
		t.Fatalf("incompatible module started: %v", err)
	}
	if _, err := prepareWith(cfg, []Module{bad}, true, buildEnv{bi: bi, skipCompat: true}); err != nil {
		t.Fatalf("skip: %v", err)
	}
	if _, err := prepareWith(cfg, []Module{bad}, true, buildEnv{bi: nil}); err != nil {
		t.Fatalf("no build info: %v", err)
	}
	t.Setenv(SkipCompatEnv, "1")
	if !skipCompat(&Options{}) {
		t.Error("HYDROGEN_SKIP_COMPAT=1 not honoured")
	}
	t.Setenv(SkipCompatEnv, "true")
	if skipCompat(&Options{}) {
		t.Error("only 1 skips")
	}
}

func TestWriteModules(t *testing.T) {
	var b bytes.Buffer
	err := WriteModules(&b, []ModuleInfo{
		{Name: "sqlite", Version: "v0.5.0", Module: "github.com/hydrogencms/hydrogen/modules/sqlite"},
		{Name: "project", Version: "devel", Module: "example.com/acme"},
		{Name: "x", Version: "unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "NAME      VERSION   MODULE\n" +
		"sqlite    v0.5.0    github.com/hydrogencms/hydrogen/modules/sqlite\n" +
		"project   devel     example.com/acme\n" +
		"x         unknown   -\n"
	if b.String() != want {
		t.Errorf("table:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestModulesCommandRegistered(t *testing.T) {
	p, err := prepare(project(t, ""), []Module{chanMod{name: "m", db: true}}, true)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := p.reg.commands["modules"]
	if !ok {
		t.Fatal("no modules command")
	}
	var out bytes.Buffer
	app := &App{stdout: &out, info: []ModuleInfo{{Name: "m", Version: "devel", Module: "example.com/acme"}}}
	if err := c.value.Run(context.Background(), app, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "m      devel     example.com/acme") {
		t.Errorf("output: %q", out.String())
	}
	if err := c.value.Run(context.Background(), app, []string{"x"}); err == nil {
		t.Error("arguments accepted")
	}
}

func TestDescribeMainPackage(t *testing.T) {
	bi := fakeBuild()
	got := describe([]Module{
		originMod{chanMod{name: "a"}, "main"},
		originMod{chanMod{name: "b"}, "command-line-arguments"},
	}, bi)
	if got[0].Package != "example.com/acme" || got[0].Module != "example.com/acme" || got[0].Version != "devel" {
		t.Errorf("main package: %+v", got[0])
	}
	if got[1].Package != "" || got[1].Module != "" || got[1].Version != "unknown" {
		t.Errorf("command-line-arguments: %+v", got[1])
	}
	if got := describe([]Module{originMod{chanMod{name: "a"}, "main"}}, nil)[0]; got.Version != "unknown" {
		t.Errorf("main without build info: %+v", got)
	}
}

func TestDescribeModulesReportsCompatAsWarning(t *testing.T) {
	bi := func() (*debug.BuildInfo, bool) { return fakeBuild(), true }
	mods := []Module{verMod{chanMod{name: "stripe"}, ">=0.6.0"}, chanMod{name: "plain"}}
	infos, compat, err := DescribeModules(mods, bi)
	if err != nil || len(infos) != 2 || compat == nil || !strings.Contains(compat.Error(), "module stripe") {
		t.Fatalf("infos=%v compat=%v err=%v", infos, compat, err)
	}
	t.Setenv(SkipCompatEnv, "1")
	if _, compat, _ := DescribeModules(mods, bi); compat != nil {
		t.Errorf("skip: %v", compat)
	}
	if _, _, err := DescribeModules([]Module{chanMod{name: "a"}, chanMod{name: "a"}}, bi); err == nil {
		t.Error("ordering error swallowed")
	}
	// Every other path still refuses.
	t.Setenv(SkipCompatEnv, "")
	if _, err := prepareWith(project(t, ""), mods, true, buildEnv{bi: fakeBuild()}); err == nil {
		t.Error("prepare accepted the mismatch")
	}
}
