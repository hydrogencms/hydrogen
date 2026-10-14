// Package hydrogen is the entry point of a Hydrogen project:
//
//	func main() { hydrogen.Main(configure) }
//
//	func configure(h *hydrogen.Builder) {
//		h.Use(sqlite.Module(), postgres.Module())
//	}
//
// The resulting binary runs the project's commands: serve, migrate, config:check, and any module commands.
package hydrogen

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/store"
)

// Builder collects a project's modules and customisations.
type Builder struct {
	modules []kernel.Module
}

// Use adds modules to the project.
func (b *Builder) Use(mods ...kernel.Module) { b.modules = append(b.modules, mods...) }

// Main runs the project binary's command line and exits.
func Main(configure func(*Builder)) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := Run(ctx, os.Args[1:], configure, RunOptions{})
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

type RunOptions struct {
	Config    *config.Options // default: config.OSOptions()
	Stdout    io.Writer       // default os.Stdout
	Stderr    io.Writer       // default os.Stderr
	DB        store.DB        // inject a database (tests)
	Listening func(addr string)
}

var core = []struct{ name, summary string }{
	{"serve", "run the server (applies migrations automatically in dev)"},
	{"migrate", "apply pending database migrations"},
	{"config:check", "validate configuration and modules (-db: also check the database)"},
	{"version", "print the Hydrogen version"},
}

// Run executes one command. It is Main without the process handling, for tests and embedding.
func Run(ctx context.Context, args []string, configure func(*Builder), o RunOptions) error {
	if o.Stdout == nil {
		o.Stdout = os.Stdout
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	if o.Config == nil {
		c := config.OSOptions()
		o.Config = &c
	}
	b := &Builder{}
	if configure != nil {
		configure(b)
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		return usage(ctx, b, o)
	}
	name, rest := args[0], args[1:]

	switch name {
	case "version":
		_, err := fmt.Fprintln(o.Stdout, "hydrogen", Version())
		return err
	case "config:check":
		return configCheck(ctx, b, o, rest)
	}

	app, err := newApp(ctx, b, o)
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()
	switch name {
	case "serve":
		if len(rest) > 0 {
			return fmt.Errorf("serve takes no arguments")
		}
		return serve(ctx, app, o)
	case "migrate":
		if len(rest) > 0 {
			return fmt.Errorf("migrate takes no arguments")
		}
		return migrate(ctx, app, o)
	}
	if c, ok := app.Command(name); ok {
		return c.Run(ctx, app, rest)
	}
	return fmt.Errorf("unknown command %q (run with no arguments for a list)", name)
}

func newApp(ctx context.Context, b *Builder, o RunOptions) (*kernel.App, error) {
	cfg, err := config.Load(*o.Config)
	if err != nil {
		return nil, err
	}
	return kernel.New(ctx, kernel.Options{Config: cfg, DB: o.DB, Stderr: o.Stderr}, b.modules...)
}

func serve(ctx context.Context, app *kernel.App, o RunOptions) error {
	s := app.Services()
	pending, err := app.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		if s.Env != config.DevEnv {
			return fmt.Errorf("%d pending migrations; run migrate first", len(pending))
		}
		s.Log.Info("applying migrations", "count", len(pending))
		if err := app.Migrate(ctx); err != nil {
			return err
		}
	}
	return app.Run(ctx, o.Listening)
}

func migrate(ctx context.Context, app *kernel.App, o RunOptions) error {
	pending, err := app.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		_, err := fmt.Fprintln(o.Stdout, "database is up to date")
		return err
	}
	if err := app.Migrate(ctx); err != nil {
		return err
	}
	for _, m := range pending {
		fmt.Fprintf(o.Stdout, "applied %s/%04d_%s\n", m.Source, m.Version, m.Name)
	}
	return nil
}

func configCheck(ctx context.Context, b *Builder, o RunOptions, args []string) error {
	fs := flag.NewFlagSet("config:check", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	withDB := fs.Bool("db", false, "also connect to the database and report pending migrations")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*o.Config)
	if err != nil {
		return err
	}
	if err := kernel.Check(cfg, b.modules...); err != nil {
		return err
	}
	if *withDB {
		app, err := newApp(ctx, b, o)
		if err != nil {
			return err
		}
		defer func() { _ = app.Close() }()
		if err := app.Services().DB.Ping(ctx); err != nil {
			return err
		}
		pending, err := app.Pending(ctx)
		if err != nil {
			return err
		}
		if len(pending) > 0 {
			fmt.Fprintf(o.Stdout, "database: %d pending migrations\n", len(pending))
		} else {
			fmt.Fprintln(o.Stdout, "database: up to date")
		}
	}
	_, err = fmt.Fprintf(o.Stdout, "config ok (env %s)\n", cfg.Env())
	return err
}

func usage(ctx context.Context, b *Builder, o RunOptions) error {
	w := tabwriter.NewWriter(o.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Commands:")
	for _, c := range core {
		fmt.Fprintf(w, "  %s\t%s\n", c.name, c.summary)
	}
	cfg, err := config.Load(*o.Config)
	if err == nil {
		err = kernel.Check(cfg, b.modules...)
	}
	if err == nil {
		if app, err2 := newApp(ctx, b, o); err2 == nil {
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
