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
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/cachetag"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/internal/commands"
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
	policies   []access.Policy
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

// Policy declares a policy roles can grant ("project.action", lower-case with a dot), for endpoints
// (kernel.Policy) and admin extension entries to require. Invalid or duplicate policies fail at start-up and
// config:check.
func (b *Builder) Policy(p access.Policy) { b.policies = append(b.policies, p) }

// TagMapper maps outbox events to cache tags, for cache purges and webhooks (see kernel.Registry.TagMapper).
func (b *Builder) TagMapper(m cachetag.Mapper) { b.mappers = append(b.mappers, m) }

// all returns the modules plus the project itself, which registers after every module.
func (b *Builder) all() []kernel.Module { return append(slices.Clone(b.modules), project{b}) }

// project is the project's own registrations as a module named "project", ordered after all others.
type project struct{ b *Builder }

func (project) Name() string { return "project" }

// Origin reports the project under its own Go module rather than the core's ("main" maps to the main module).
func (project) Origin() string { return "main" }

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
	for _, pol := range p.b.policies {
		if err := r.Policy(pol); err != nil {
			return err
		}
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
	Clock  clock.Clock     // default clock.System; names the database backups of content.files
	// PasswordIterations lowers the PBKDF2 cost of password hashes so tests run fast (0 = 600 000).
	PasswordIterations int
	Listening          func(addr string)
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
	return commands.Run(ctx, args, b.all(), commands.Options(o))
}

// Version reports the Hydrogen module version this binary was built with.
func Version() string { return commands.Version() }
