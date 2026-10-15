package kernel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// DefaultDevDatabase is used in the dev environment when no database is configured.
const DefaultDevDatabase = "sqlite:.work/hydrogen.db"

type HTTPConfig struct {
	Addr              string        `yaml:"addr"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"`
	ShutdownTimeout   time.Duration `yaml:"shutdown_timeout"`
}

type DatabaseConfig struct {
	URL string `yaml:"url" config:"secret"`
}

type LogConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // text or json; default text in dev, json elsewhere
}

type OutboxConfig struct {
	Poll  time.Duration `yaml:"poll"`
	Lease time.Duration `yaml:"lease"`
}

type ContentConfig struct {
	SchemaDir     string `yaml:"schema_dir"`     // default "schema"
	DefaultLocale string `yaml:"default_locale"` // default "en"
}

type ManageConfig struct {
	// Token authorises the management API (Authorization: Bearer). Until user accounts exist (H5), it is the
	// only credential: required outside dev, "dev-token" in dev when unset.
	Token string `yaml:"token" config:"secret"`
}

// DevManageToken is the management API token in dev when manage.token is unset.
const DevManageToken = "dev-token"

type CoreConfig struct {
	HTTP     HTTPConfig
	Database DatabaseConfig
	Log      LogConfig
	Outbox   OutboxConfig
	Content  ContentConfig
	Manage   ManageConfig
}

// Services are the core services modules receive in Boot.
type Services struct {
	Env     string
	Config  *config.Config
	Core    CoreConfig
	Log     *slog.Logger
	Clock   clock.Clock
	IDs     *ids.Generator
	DB      store.DB
	Outbox  *outbox.Outbox
	Audit   *audit.Log
	Schema  *schema.Schema
	Hooks   *hooks.Registry
	Content *content.Service
	// Channels are the compiled delivery channels (config/channels.yaml); one implicit "default" channel when
	// none are configured.
	Channels *channel.Set

	reg *Registry
}

// LookupService finds a value a module provided.
func LookupService[T any](s *Services) (T, bool) { return Lookup[T](s.reg) }

type Options struct {
	Config *config.Config // required
	Clock  clock.Clock    // default clock.System
	Rand   io.Reader      // default crypto/rand
	Log    *slog.Logger   // default from the log config, on stderr
	DB     store.DB       // inject a database instead of opening database.url (tests)
	Stderr io.Writer      // default os.Stderr
}

type App struct {
	services *Services
	modules  []Module
	reg      *Registry
	ownDB    bool
}

func New(ctx context.Context, o Options, mods ...Module) (*App, error) {
	if o.Config == nil {
		return nil, errors.New("kernel: Options.Config is required")
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Stderr == nil {
		o.Stderr = os.Stderr
	}
	p, err := prepare(o.Config, mods, true)
	if err != nil {
		return nil, err
	}
	cfg, core, reg, ordered := o.Config, p.core, p.reg, p.ordered

	log := o.Log
	if log == nil {
		if log, err = newLogger(core.Log, cfg.Env(), o.Stderr); err != nil {
			return nil, err
		}
	}

	a := &App{modules: ordered, reg: reg}
	db := o.DB
	if db == nil {
		if db, err = a.open(ctx, core.Database.URL, store.OpenOptions{Clock: o.Clock, Log: log}); err != nil {
			return nil, err
		}
		a.ownDB = true
	}
	// Async hooks become outbox consumers; their handlers reach the content service, built just below.
	var svc *content.Service
	consumers := append(slices.Clone(reg.consumers), content.AsyncHookConsumers(reg.hooks.Async(), func() *content.Service { return svc })...)
	ob, err := outbox.New(db, o.Clock, log, consumers, outbox.Options{Poll: core.Outbox.Poll, Lease: core.Outbox.Lease})
	if err != nil {
		a.closeDB(db)
		return nil, err
	}
	hookReg := &reg.hooks
	migrations := make([]content.Migration, 0, len(reg.contentMig))
	for _, m := range reg.contentMig {
		migrations = append(migrations, m.value)
	}
	gen := ids.NewGenerator(o.Clock, o.Rand)
	al := audit.New(o.Clock)
	svc, err = content.New(content.Options{DB: db, Schema: p.schema, Hooks: hookReg, Outbox: ob, Audit: al,
		Clock: o.Clock, IDs: gen, DefaultLocale: core.Content.DefaultLocale, Migrations: migrations})
	if err != nil {
		a.closeDB(db)
		return nil, err
	}
	a.services = &Services{
		Env: cfg.Env(), Config: cfg, Core: core, Log: log, Clock: o.Clock, IDs: gen, DB: db, Outbox: ob,
		Audit: al, Schema: p.schema, Hooks: hookReg, Content: svc, Channels: p.chans, reg: reg,
	}
	for _, m := range ordered {
		if b, ok := m.(Booter); ok {
			if err := b.Boot(ctx, a.services); err != nil {
				a.closeDB(db)
				return nil, fmt.Errorf("module %s: boot: %w", m.Name(), err)
			}
		}
	}
	return a, nil
}

func (a *App) open(ctx context.Context, url string, o store.OpenOptions) (store.DB, error) {
	op, err := opener(a.reg, url)
	if err != nil {
		return nil, err
	}
	return op(ctx, url, o)
}

func opener(reg *Registry, url string) (store.Opener, error) {
	scheme, _, ok := strings.Cut(url, ":")
	if !ok {
		return nil, fmt.Errorf("database.url: expected scheme:… (e.g. sqlite:.work/hydrogen.db)")
	}
	op, ok := reg.openers[scheme]
	if !ok {
		have := slices.Sorted(maps.Keys(reg.openers))
		if len(have) == 0 {
			return nil, fmt.Errorf("database.url: no database adapter in this build; add one, e.g. sqlite.Module()")
		}
		return nil, fmt.Errorf("database.url: no adapter for %q in this build (have: %s)", scheme, strings.Join(have, ", "))
	}
	return op.value, nil
}

type plan struct {
	ordered []Module
	core    CoreConfig
	reg     *Registry
	schema  *schema.Schema
	chans   *channel.Set
}

// prepare orders modules, decodes core and module configuration, runs Register and compiles the schema. It
// opens nothing. With runtime false (code generation) it decodes only what the schema needs, so runtime
// settings and their secrets don't have to be present.
func prepare(cfg *config.Config, mods []Module, runtime bool) (*plan, error) {
	ordered, err := order(mods)
	if err != nil {
		return nil, err
	}
	core := CoreConfig{
		HTTP:    HTTPConfig{Addr: ":8080", ReadHeaderTimeout: 10 * time.Second, ShutdownTimeout: 15 * time.Second},
		Log:     LogConfig{Level: "info"},
		Content: ContentConfig{SchemaDir: "schema", DefaultLocale: "en"},
	}
	sections := []struct {
		path   string
		target any
	}{{"content", &core.Content}}
	if runtime {
		sections = append(sections, []struct {
			path   string
			target any
		}{{"http", &core.HTTP}, {"database", &core.Database}, {"log", &core.Log}, {"outbox", &core.Outbox},
			{"manage", &core.Manage}}...)
	}
	for _, d := range sections {
		if err := cfg.Decode(d.path, d.target); err != nil {
			return nil, err
		}
	}
	reg := newRegistry()
	for _, m := range ordered {
		if c, ok := m.(Configurable); ok {
			if err := cfg.Decode("modules."+m.Name(), c.Config()); err != nil {
				return nil, err
			}
		}
		reg.current = m.Name()
		if err := m.Register(reg); err != nil {
			return nil, fmt.Errorf("module %s: register: %w", m.Name(), err)
		}
	}
	reg.current = ""
	s, err := compileSchema(core.Content.SchemaDir, ordered, reg)
	if err != nil {
		return nil, err
	}
	p := &plan{ordered: ordered, core: core, reg: reg, schema: s}
	if runtime {
		if p.chans, err = compileChannels(cfg, core.Content.DefaultLocale, s, reg); err != nil {
			return nil, err
		}
		if err := p.checkRuntime(cfg); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// checkRuntime fills runtime defaults and rejects what a server can't run with.
func (p *plan) checkRuntime(cfg *config.Config) error {
	if err := cfg.Unused(); err != nil {
		return err
	}
	dev := cfg.Env() == config.DevEnv
	if p.core.Database.URL == "" {
		if !dev {
			return fmt.Errorf("database.url is required outside %s", config.DevEnv)
		}
		p.core.Database.URL = DefaultDevDatabase
	}
	// The management API exists only when there is content to manage; then it needs a token.
	if p.core.Manage.Token == "" && len(p.schema.Types()) > 0 {
		if !dev {
			return fmt.Errorf("manage.token is required outside %s (the schema defines content types)", config.DevEnv)
		}
		p.core.Manage.Token = DevManageToken
	}
	if _, err := opener(p.reg, p.core.Database.URL); err != nil {
		return err
	}
	_, err := newLogger(p.core.Log, cfg.Env(), io.Discard)
	return err
}

// compileSchema loads the project's schema files, lets Schemer modules transform them in dependency order,
// and compiles the result with the core and module field types.
func compileSchema(dir string, ordered []Module, reg *Registry) (*schema.Schema, error) {
	types := schema.NewRegistry()
	for _, ft := range reg.fieldTypes {
		if err := types.Register(ft.value); err != nil {
			return nil, fmt.Errorf("module %s: field type %s: %w", ft.module, ft.value.Name(), err)
		}
	}
	for _, o := range reg.typeOpts {
		if err := types.TypeOption(o.value.name, o.value.check); err != nil {
			return nil, fmt.Errorf("module %s: %w", o.module, err)
		}
	}
	b := schema.NewBuilder(types)
	if err := b.LoadDir(dir); err != nil {
		return nil, err
	}
	for _, m := range ordered {
		if sm, ok := m.(Schemer); ok {
			b.SetOrigin("module " + m.Name())
			if err := sm.Schema(b); err != nil {
				return nil, fmt.Errorf("module %s: schema: %w", m.Name(), err)
			}
		}
	}
	s, err := b.Compile()
	if err != nil {
		return nil, fmt.Errorf("schema:\n%w", err)
	}
	return s, nil
}

// CompileSchema returns the project's compiled schema (files plus module transforms) without opening the
// database, for code generation.
func CompileSchema(cfg *config.Config, mods ...Module) (*schema.Schema, error) {
	p, err := prepare(cfg, mods, false)
	if err != nil {
		return nil, err
	}
	return p.schema, nil
}

// Check validates configuration and module wiring without opening the database (config:check).
func Check(cfg *config.Config, mods ...Module) error {
	_, err := prepare(cfg, mods, true)
	return err
}

func (a *App) closeDB(db store.DB) {
	if a.ownDB {
		_ = db.Close()
	}
}

func (a *App) Services() *Services { return a.services }

func (a *App) Modules() []string {
	names := make([]string, len(a.modules))
	for i, m := range a.modules {
		names[i] = m.Name()
	}
	return names
}

func (a *App) Command(name string) (Command, bool) {
	c, ok := a.reg.commands[name]
	return c.value, ok
}

func (a *App) Commands() []Command {
	var out []Command
	for _, c := range a.reg.commands {
		out = append(out, c.value)
	}
	slices.SortFunc(out, func(x, y Command) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// MigrationSets returns the registered module migrations for the open database's dialect.
func (a *App) MigrationSets() ([]store.MigrationSet, error) {
	dialect := a.services.DB.Dialect()
	var sets []store.MigrationSet
	for _, m := range a.reg.migrations {
		fsys, err := m.value.FS(dialect)
		if err != nil {
			return nil, fmt.Errorf("module %s: migrations for %s: %w", m.module, dialect, err)
		}
		sets = append(sets, store.MigrationSet{Source: m.value.Source, FS: fsys})
	}
	return sets, nil
}

func (a *App) Migrate(ctx context.Context) error {
	sets, err := a.MigrationSets()
	if err != nil {
		return err
	}
	return a.services.DB.Migrate(ctx, sets)
}

func (a *App) Pending(ctx context.Context) ([]store.Migration, error) {
	sets, err := a.MigrationSets()
	if err != nil {
		return nil, err
	}
	return a.services.DB.Pending(ctx, sets)
}

// Handler serves health checks and module routes.
func (a *App) Handler() (http.Handler, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /readyz", a.ready)
	if len(a.services.Schema.Types()) > 0 {
		const prefix = "/api/manage/v1"
		api, err := manage.New(manage.Options{Content: a.services.Content, Token: a.services.Core.Manage.Token,
			Channels: a.services.Channels, Log: a.services.Log, Prefix: prefix, Routes: a.reg.manageRts})
		if err != nil {
			return nil, err
		}
		mux.Handle(prefix+"/", http.StripPrefix(prefix, api))
	}
	for _, rt := range a.reg.routes {
		if err := handle(mux, rt.value.pattern, rt.value.handler); err != nil {
			return nil, fmt.Errorf("module %s: route %q: %w", rt.module, rt.value.pattern, err)
		}
	}
	return recoverer(a.services.Log, mux), nil
}

func handle(mux *http.ServeMux, pattern string, h http.Handler) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	mux.Handle(pattern, h)
	return nil
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := a.services.DB.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprintf(w, "database: %v\n", err)
		return
	}
	pending, err := a.Pending(ctx)
	if err != nil || len(pending) > 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err != nil {
			_, _ = fmt.Fprintf(w, "migrations: %v\n", err)
		} else {
			_, _ = fmt.Fprintf(w, "migrations: %d pending\n", len(pending))
		}
		return
	}
	_, _ = io.WriteString(w, "ok\n")
}

func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("http: panic", "method", r.Method, "path", r.URL.Path, "panic", v)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Run starts modules, the outbox worker and the HTTP server, and blocks until ctx ends or the server fails.
// listening, if set, is called with the bound address once the server accepts connections.
func (a *App) Run(ctx context.Context, listening func(addr string)) error {
	s := a.services
	h, err := a.Handler()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var started []Module
	stop := func() {
		sctx, scancel := context.WithTimeout(context.Background(), s.Core.HTTP.ShutdownTimeout)
		defer scancel()
		for _, m := range slices.Backward(started) {
			if st, ok := m.(Stopper); ok {
				if err := st.Stop(sctx); err != nil {
					s.Log.Error("module stop failed", "module", m.Name(), "err", err)
				}
			}
		}
	}
	for _, m := range a.modules {
		if st, ok := m.(Starter); ok {
			if err := st.Start(ctx); err != nil {
				cancel()
				stop()
				return fmt.Errorf("module %s: start: %w", m.Name(), err)
			}
		}
		started = append(started, m)
	}
	defer stop()

	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() {
		if err := s.Outbox.Run(ctx); err != nil {
			s.Log.Error("outbox worker stopped", "err", err)
		}
	})

	ln, err := net.Listen("tcp", s.Core.HTTP.Addr)
	if err != nil {
		cancel()
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: s.Core.HTTP.ReadHeaderTimeout,
		ErrorLog: slog.NewLogLogger(s.Log.Handler(), slog.LevelWarn)}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	s.Log.Info("serving", "addr", ln.Addr().String(), "env", s.Env, "db", s.DB.Dialect(), "modules", a.Modules())
	if s.Core.Manage.Token == DevManageToken {
		s.Log.Warn("the management API accepts the dev token; set manage.token before exposing this server")
	}
	if listening != nil {
		listening(ln.Addr().String())
	}

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		cancel()
		return err
	}
	sctx, scancel := context.WithTimeout(context.Background(), s.Core.HTTP.ShutdownTimeout)
	defer scancel()
	cancel()
	return srv.Shutdown(sctx)
}

func (a *App) Close() error {
	if a.ownDB {
		return a.services.DB.Close()
	}
	return nil
}

func newLogger(c LogConfig, env string, w io.Writer) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Level)); err != nil {
		return nil, fmt.Errorf("log.level: %q is not debug, info, warn or error", c.Level)
	}
	format := c.Format
	if format == "" {
		format = "json"
		if env == config.DevEnv {
			format = "text"
		}
	}
	opts := &slog.HandlerOptions{Level: level}
	switch format {
	case "text":
		return slog.New(slog.NewTextHandler(w, opts)), nil
	case "json":
		return slog.New(slog.NewJSONHandler(w, opts)), nil
	}
	return nil, fmt.Errorf("log.format: %q is not text or json", format)
}
