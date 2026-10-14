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
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/outbox"
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

type CoreConfig struct {
	HTTP     HTTPConfig
	Database DatabaseConfig
	Log      LogConfig
	Outbox   OutboxConfig
}

// Services are the core services modules receive in Boot.
type Services struct {
	Env    string
	Config *config.Config
	Core   CoreConfig
	Log    *slog.Logger
	Clock  clock.Clock
	IDs    *ids.Generator
	DB     store.DB
	Outbox *outbox.Outbox
	Audit  *audit.Log

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
	p, err := prepare(o.Config, mods)
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
	ob, err := outbox.New(db, o.Clock, log, reg.consumers, outbox.Options{Poll: core.Outbox.Poll, Lease: core.Outbox.Lease})
	if err != nil {
		a.closeDB(db)
		return nil, err
	}
	a.services = &Services{
		Env: cfg.Env(), Config: cfg, Core: core, Log: log, Clock: o.Clock,
		IDs: ids.NewGenerator(o.Clock, o.Rand), DB: db, Outbox: ob, Audit: audit.New(o.Clock), reg: reg,
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
}

// prepare orders modules, decodes core and module configuration, and runs Register. It opens nothing.
func prepare(cfg *config.Config, mods []Module) (*plan, error) {
	ordered, err := order(mods)
	if err != nil {
		return nil, err
	}
	core := CoreConfig{
		HTTP: HTTPConfig{Addr: ":8080", ReadHeaderTimeout: 10 * time.Second, ShutdownTimeout: 15 * time.Second},
		Log:  LogConfig{Level: "info"},
	}
	for _, d := range []struct {
		path   string
		target any
	}{{"http", &core.HTTP}, {"database", &core.Database}, {"log", &core.Log}, {"outbox", &core.Outbox}} {
		if err := cfg.Decode(d.path, d.target); err != nil {
			return nil, err
		}
	}
	if core.Database.URL == "" {
		if cfg.Env() != config.DevEnv {
			return nil, fmt.Errorf("database.url is required outside %s", config.DevEnv)
		}
		core.Database.URL = DefaultDevDatabase
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
	if err := cfg.Unused(); err != nil {
		return nil, err
	}
	if _, err := opener(reg, core.Database.URL); err != nil {
		return nil, err
	}
	if _, err := newLogger(core.Log, cfg.Env(), io.Discard); err != nil {
		return nil, err
	}
	return &plan{ordered: ordered, core: core, reg: reg}, nil
}

// Check validates configuration and module wiring without opening the database (config:check).
func Check(cfg *config.Config, mods ...Module) error {
	_, err := prepare(cfg, mods)
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
