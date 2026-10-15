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
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/codegen"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// Builder collects a project's modules and customisations. Generated typed handles register hooks on it
// (it implements content.HookRegistrar).
type Builder struct {
	modules    []kernel.Module
	hooks      []hooks.Hook
	schemas    []func(*schema.Builder) error
	migrations []content.Migration
	commands   []kernel.Command
	endpoints  []endpoint
	jobs       []job
	mappers    []cachetag.Mapper
	rules      []typeRules
}

type typeRules struct {
	typ   string
	rules access.Rules
}

type endpoint struct {
	method, path string
	auth         kernel.Auth
	h            http.HandlerFunc
}

type job struct {
	name, schedule string
	fn             func(ctx context.Context, s *kernel.Services) error
}

// Use adds modules to the project.
func (b *Builder) Use(mods ...kernel.Module) { b.modules = append(b.modules, mods...) }

// AddHook registers a content hook. Prefer the typed handles in the generated gen/content package.
func (b *Builder) AddHook(h hooks.Hook) error {
	var check hooks.Registry
	if err := check.Add(h); err != nil {
		return err
	}
	b.hooks = append(b.hooks, h)
	return nil
}

// Schema changes the compiled schema after modules have (add fields, types or groups in code).
func (b *Builder) Schema(fn func(*schema.Builder) error) { b.schemas = append(b.schemas, fn) }

// Migration registers a content migration, applied by content:migrate.
func (b *Builder) Migration(m content.Migration) { b.migrations = append(b.migrations, m) }

// Command adds a command to the project binary.
func (b *Builder) Command(c kernel.Command) { b.commands = append(b.commands, c) }

// Endpoint adds an HTTP endpoint under /api/x (see kernel.Registry.Endpoint). Handlers reach the services with
// kernel.ServicesFrom(r.Context()). Conflicts and invalid arguments fail at start-up and config:check.
func (b *Builder) Endpoint(method, path string, auth kernel.Auth, h http.HandlerFunc) {
	b.endpoints = append(b.endpoints, endpoint{method, path, auth, h})
}

// Job runs fn on a schedule: a 5-field cron expression (UTC) or "@every <duration>". Across servers sharing a
// database each tick runs once. "job:run <name>" runs a job now.
func (b *Builder) Job(name, schedule string, fn func(ctx context.Context, s *kernel.Services) error) {
	b.jobs = append(b.jobs, job{name, schedule, fn})
}

// AccessRules registers code access rules for a content type, ANDed with role grants and with other rules for
// the type; the system and the config token are never restricted by them. Prefer the typed handle in gen/content:
// content.Article.Access(h, rules).
func (b *Builder) AccessRules(typ string, rules access.Rules) {
	b.rules = append(b.rules, typeRules{typ, rules})
}

// TagMapper maps outbox events to cache tags, for cache purges and webhooks (see kernel.Registry.TagMapper).
func (b *Builder) TagMapper(m cachetag.Mapper) { b.mappers = append(b.mappers, m) }

// all returns the modules plus the project itself, which registers after every module.
func (b *Builder) all() []kernel.Module { return append(slices.Clone(b.modules), project{b}) }

// project is the project's own registrations as a module named "project", ordered after all others.
type project struct{ b *Builder }

func (project) Name() string { return "project" }

func (p project) Requires() []string {
	names := make([]string, len(p.b.modules))
	for i, m := range p.b.modules {
		names[i] = m.Name()
	}
	return names
}

func (p project) Register(r *kernel.Registry) error {
	for _, h := range p.b.hooks {
		if err := r.AddHook(h); err != nil {
			return err
		}
	}
	for _, m := range p.b.migrations {
		r.ContentMigration(m)
	}
	for _, c := range p.b.commands {
		if err := r.Command(c); err != nil {
			return err
		}
	}
	for _, e := range p.b.endpoints {
		if err := r.Endpoint(e.method, e.path, e.auth, e.h); err != nil {
			return err
		}
	}
	for _, j := range p.b.jobs {
		if err := r.Job(j.name, j.schedule, j.fn); err != nil {
			return err
		}
	}
	for _, m := range p.b.mappers {
		r.TagMapper(m)
	}
	for _, tr := range p.b.rules {
		r.AccessRules(tr.typ, tr.rules)
	}
	return nil
}

func (p project) Schema(sb *schema.Builder) error {
	for _, fn := range p.b.schemas {
		if err := fn(sb); err != nil {
			return err
		}
	}
	return nil
}

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
	Config *config.Options // default: config.OSOptions()
	Stdin  io.Reader       // default os.Stdin
	Stdout io.Writer       // default os.Stdout
	Stderr io.Writer       // default os.Stderr
	DB     store.DB        // inject a database (tests)
	// PasswordIterations lowers the PBKDF2 cost of password hashes so tests run fast (0 = 600 000).
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
	{"export", "write content and module data to a directory: export [-published-only] <dir>"},
	{"import", "restore an export into an empty database: import [-allow-schema-change] <dir>"},
	{"version", "print the Hydrogen version"},
}

// Run executes one command. It is Main without the process handling, for tests and embedding.
func Run(ctx context.Context, args []string, configure func(*Builder), o RunOptions) error {
	if o.Stdin == nil {
		o.Stdin = os.Stdin
	}
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
	case "generate":
		return generate(b, o, rest)
	case "check-frontend":
		return checkFrontend(ctx, b, o, rest)
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

func jobRun(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: job:run <name> (jobs: %s)", strings.Join(app.JobNames(), ", "))
	}
	if err := app.RunJob(ctx, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintln(o.Stdout, "job", args[0], "done")
	return err
}

func newApp(ctx context.Context, b *Builder, o RunOptions) (*kernel.App, error) {
	cfg, err := config.Load(*o.Config)
	if err != nil {
		return nil, err
	}
	return kernel.New(ctx, kernel.Options{Config: cfg, DB: o.DB, Stderr: o.Stderr, PasswordIterations: o.PasswordIterations}, b.all()...)
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
	if err := readyContent(ctx, app); err != nil {
		return err
	}
	if err := s.Admin.SeedTemplates(ctx); err != nil {
		return err
	}
	return app.Run(ctx, o.Listening)
}

// readyContent checks that stored content matches the schema: breaking changes need migrations, which apply
// automatically in dev, and the current schema is recorded as a revision.
func readyContent(ctx context.Context, app *kernel.App) error {
	s := app.Services()
	breaking, err := s.Content.BreakingChanges(ctx)
	if err != nil {
		return err
	}
	if len(breaking) > 0 {
		return fmt.Errorf("schema changes need a content migration:\n  %s", strings.Join(breaking, "\n  "))
	}
	pending, err := s.Content.PendingMigrations(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		if s.Env != config.DevEnv {
			return fmt.Errorf("%d pending content migrations; run content:migrate first", len(pending))
		}
		s.Log.Info("applying content migrations", "names", pending)
		if _, err := s.Content.Migrate(ctx); err != nil {
			return err
		}
	}
	if _, err = s.Content.EnsureWorkingProjections(ctx); err != nil {
		return err
	}
	_, err = s.Content.EnsureSchemaRev(ctx)
	return err
}

func contentReindex(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("content:reindex takes no arguments")
	}
	pending, err := app.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d pending database migrations; run migrate first", len(pending))
	}
	n, err := app.Services().Content.Reindex(ctx, func(done int) { fmt.Fprintf(o.Stderr, "reindexed %d items\n", done) })
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(o.Stdout, "reindexed %d items\n", n)
	return err
}

func contentMigrate(ctx context.Context, app *kernel.App, o RunOptions) error {
	pending, err := app.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d pending database migrations; run migrate first", len(pending))
	}
	applied, err := app.Services().Content.Migrate(ctx)
	if err != nil {
		return err
	}
	if _, err := app.Services().Content.EnsureSchemaRev(ctx); err != nil {
		return err
	}
	if _, err := app.Services().Content.EnsureWorkingProjections(ctx); err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Fprintln(o.Stdout, "content is up to date")
	}
	for _, name := range applied {
		fmt.Fprintln(o.Stdout, "applied", name)
	}
	return nil
}

func generate(b *Builder, o RunOptions, args []string) error {
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	goDir := fs.String("go", "gen/content", "directory of the generated Go package")
	pkg := fs.String("package", "content", "name of the generated Go package")
	tsFile := fs.String("ts", "gen/types.ts", "generated TypeScript file (empty: skip)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*o.Config)
	if err != nil {
		return err
	}
	s, err := kernel.CompileSchema(cfg, b.all()...)
	if err != nil {
		return err
	}
	n, err := codegen.WriteGo(*goDir, s.Types(), s.Groups(), codegen.GoOptions{Package: *pkg})
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "wrote %d Go files to %s\n", n, *goDir)
	if *tsFile != "" {
		if err := codegen.WriteTypeScript(*tsFile, s.Types(), s.Groups(), codegen.TSOptions{}); err != nil {
			return err
		}
		fmt.Fprintf(o.Stdout, "wrote %s\n", *tsFile)
	}
	return nil
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
	if _, err = app.Services().Content.EnsureWorkingProjections(ctx); err != nil {
		return err
	}
	return app.Services().Admin.SeedTemplates(ctx)
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
	if err := kernel.Check(cfg, b.all()...); err != nil {
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
			breaking, err := app.Services().Content.BreakingChanges(ctx)
			if err != nil {
				return err
			}
			if len(breaking) > 0 {
				return fmt.Errorf("schema changes need a content migration:\n  %s", strings.Join(breaking, "\n  "))
			}
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
		err = kernel.Check(cfg, b.all()...)
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
