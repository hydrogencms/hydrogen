package commands

import (
	"context"
	"fmt"
	"io"
	"runtime/debug"
	"strings"
	"text/tabwriter"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Options are hydrogen.RunOptions (converted field for field) with every default filled.
type Options struct {
	Config             *config.Options
	Stdin              io.Reader
	Stdout             io.Writer
	Stderr             io.Writer
	DB                 store.DB
	Clock              clock.Clock
	PasswordIterations int
	Listening          func(addr string)
}

var core = []struct{ name, summary string }{
	{"serve", "run the server (applies migrations automatically in dev)"},
	{"migrate", "apply pending database migrations"},
	{"config:check", "validate configuration, schema and modules (-db: also check the database)"},
	{"content:migrate", "apply pending content migrations"},
	{"content:reindex", "rebuild every item's index, search, reference, snapshot and working projections"},
	{"generate", "write typed Go (gen/content) and TypeScript (gen/types.ts) from the schema"},
	{"job:run", "run a scheduled job now: job:run <name>"},
	{"check-frontend", "check that a frontend has a template for every page type and mount: check-frontend -templates <file> [-channel name] [-db]"},
	{"users:create-admin", "create or reset an administrator: users:create-admin <login> (password from HYDROGEN_ADMIN_PASSWORD or one line of stdin)"},
	{"tokens:create", "create an API token and print its secret once: tokens:create -name <n> -role <role> [-agent] [-expires 720h]"},
	{"export", "write content and module data to a directory: export [-published-only] [-layout ndjson|items] <dir>"},
	{"import", "restore an export into an empty database: import [-allow-schema-change] <dir>"},
	{"content:save", "write the database's content to content.files (content in git)"},
	{"content:load", "replace the database with the content of content.files: content:load [-force]"},
	{"version", "print the Hydrogen version"},
}

// Run executes one command of a project built from mods. The options are complete: the caller fills the defaults.
func Run(ctx context.Context, args []string, mods []kernel.Module, o Options) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return usage(ctx, mods, o)
	}
	name, rest := args[0], args[1:]

	switch name {
	case "version":
		_, err := fmt.Fprintln(o.Stdout, "hydrogen", Version())
		return err
	case "modules":
		return modulesList(mods, o, rest)
	case "config:check":
		return configCheck(ctx, mods, o, rest)
	case "generate":
		return generate(mods, o, rest)
	case "check-frontend":
		return checkFrontend(ctx, mods, o, rest)
	}

	app, err := newApp(ctx, mods, o)
	if err != nil {
		return err
	}
	defer func() {
		if app != nil {
			_ = app.Close()
		}
	}()
	switch name {
	case "serve":
		if len(rest) > 0 {
			return fmt.Errorf("serve takes no arguments")
		}
		var rebuilt bool
		if app, rebuilt, err = reconcileContentFiles(ctx, mods, o, app); err != nil {
			return err
		}
		return serve(ctx, app, o, rebuilt)
	case "content:save":
		return contentSave(ctx, app, o, rest)
	case "content:load":
		app, err = contentLoad(ctx, mods, o, app, rest)
		return err
	case "migrate":
		if len(rest) > 0 {
			return fmt.Errorf("migrate takes no arguments")
		}
		return migrate(ctx, app, o)
	case "content:migrate":
		return contentMigrate(ctx, app, o)
	case "content:reindex":
		return contentReindex(ctx, app, o, rest)
	case "users:create-admin":
		return createAdmin(ctx, app, o, rest)
	case "tokens:create":
		return createToken(ctx, app, o, rest)
	case "job:run":
		return jobRun(ctx, app, o, rest)
	case "export":
		return exportCommand(ctx, app, o, rest)
	case "import":
		return importCommand(ctx, app, o, rest)
	}
	if c, ok := app.Command(name); ok {
		return c.Run(ctx, app, rest)
	}
	return fmt.Errorf("unknown command %q (run with no arguments for a list)", name)
}

func newApp(ctx context.Context, mods []kernel.Module, o Options) (*kernel.App, error) {
	cfg, err := config.Load(*o.Config)
	if err != nil {
		return nil, err
	}
	return kernel.New(ctx, kernel.Options{Config: cfg, DB: o.DB, Clock: o.Clock, Stderr: o.Stderr, Stdout: o.Stdout, PasswordIterations: o.PasswordIterations}, mods...)
}

func usage(ctx context.Context, mods []kernel.Module, o Options) error {
	w := tabwriter.NewWriter(o.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Commands:")
	for _, c := range core {
		fmt.Fprintf(w, "  %s\t%s\n", c.name, c.summary)
	}
	cfg, err := config.Load(*o.Config)
	if err == nil {
		err = kernel.Check(cfg, mods...)
	}
	if err == nil {
		if app, err2 := newApp(ctx, mods, o); err2 == nil {
			for _, c := range app.Commands() {
				fmt.Fprintf(w, "  %s\t%s\n", c.Name, c.Summary)
			}
			_ = app.Close()
		} else {
			err = err2
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err != nil {
		fmt.Fprintf(o.Stderr, "\nModule commands unavailable: %v\n", err)
	}
	return nil
}

// modulesList prints the modules before anything is opened, so it works when the app can't start; a core
// compatibility mismatch is a warning here.
func modulesList(mods []kernel.Module, o Options, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("modules takes no arguments")
	}
	infos, compat, err := kernel.DescribeModules(mods, nil)
	if err != nil {
		return err
	}
	if err := kernel.WriteModules(o.Stdout, infos); err != nil {
		return err
	}
	if compat != nil {
		fmt.Fprintf(o.Stderr, "warning: %v\n", compat)
	}
	return nil
}

// Version reports the Hydrogen module version this binary was built with.
func Version() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if bi.Main.Path == "github.com/hydrogencms/hydrogen" {
		return versionOrDevel(bi.Main.Version)
	}
	for _, d := range bi.Deps {
		if d.Path == "github.com/hydrogencms/hydrogen" {
			if d.Replace != nil {
				return "devel (" + d.Replace.Path + ")"
			}
			return versionOrDevel(d.Version)
		}
	}
	return "unknown"
}

func versionOrDevel(v string) string {
	if v == "" || v == "(devel)" || strings.HasPrefix(v, "v0.0.0-") {
		return "devel"
	}
	return v
}
