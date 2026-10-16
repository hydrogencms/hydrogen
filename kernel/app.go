package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/admin"
	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/delivery"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/jobs"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/release"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/webhook"
)

// DefaultDevDatabase is used in the dev environment when no database is configured.
const DefaultDevDatabase = "sqlite:.work/hydrogen.db"

type HTTPConfig struct {
	Addr              string        `yaml:"addr"`
	ReadHeaderTimeout time.Duration `yaml:"read_header_timeout"`
	ShutdownTimeout   time.Duration `yaml:"shutdown_timeout"`
	// TrustedProxies are the CIDRs (or addresses) of reverse proxies whose X-Forwarded-For is believed when
	// the server needs a client address (login throttling, session records).
	TrustedProxies []string `yaml:"trusted_proxies"`
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

// JobsConfig controls the scheduled jobs runner.
type JobsConfig struct {
	// Enabled starts the runner in serve (default true). A node that sets it to false never runs scheduled jobs;
	// job:run still works.
	Enabled bool `yaml:"enabled"`
}

type ContentConfig struct {
	SchemaDir     string `yaml:"schema_dir"`     // default "schema"
	DefaultLocale string `yaml:"default_locale"` // default "en"
	// TrashRetention is how long an item stays in the trash before the content.trash job purges it. Default
	// 720h (30 days); 0 keeps trashed items until someone purges them.
	TrashRetention time.Duration `yaml:"trash_retention"`
}

type ManageConfig struct {
	// Token is a bearer token with every right, for automation and bootstrapping (audited as token:config).
	// Optional: when unset only users and API tokens authenticate. In dev it defaults to "dev-token".
	Token string `yaml:"token" config:"secret"`
}

// DevManageToken is the management API token in dev when manage.token is unset.
const DevManageToken = "dev-token"

type DeliveryConfig struct {
	// CacheBytes bounds the in-process response cache of the delivery API; 0 turns it off. Default 32 MiB.
	CacheBytes int64 `yaml:"cache_bytes"`
	// PreviewSecret signs preview tokens: at least 32 characters, and required outside dev. In dev, when unset,
	// it derives from manage.token, so every node sharing that token accepts the same previews.
	PreviewSecret string `yaml:"preview_secret" config:"secret"`
	// ShareLinks lets editors create share links to previews (default true); off, creating one is refused and
	// existing ones stop working.
	ShareLinks bool `yaml:"share_links"`
	// ShareDefaultTTL and ShareMaxTTL are the lifetime of a new share link and the longest one allowed
	// (default 24h and 168h; at most 720h).
	ShareDefaultTTL time.Duration `yaml:"share_default_ttl"`
	ShareMaxTTL     time.Duration `yaml:"share_max_ttl"`
}

type AdminConfig struct {
	// Enabled serves the admin app (default true).
	Enabled bool `yaml:"enabled"`
	// Prefix is where the admin app is mounted: a leading slash, no trailing one. Default "/admin".
	Prefix string `yaml:"prefix"`
	// Extensions: custom elements the admin loads into its slots. File paths are relative to the working
	// directory (the project). Contract: admin.Manifest.
	Elements []string          `yaml:"elements"`
	Theme    admin.Theme       `yaml:"theme"`
	Strings  string            `yaml:"strings"`
	Widgets  map[string]string `yaml:"widgets"`
	Cells    map[string]string `yaml:"cells"`
	Panels   []admin.Panel     `yaml:"panels"`
	Actions  []admin.Panel     `yaml:"actions"`
	// Dashboard lists dashboard widgets (tag, title, optional policy).
	Dashboard []admin.Panel `yaml:"dashboard"`
	Views     []admin.View  `yaml:"views"`
	Slots     []admin.Slot  `yaml:"slots"`
	// ElementsDevEntry is the Vite entry listed instead of the built files when ADMIN_ELEMENTS_DEV_URL is set
	// in dev. Default "/src/elements.ts".
	ElementsDevEntry string `yaml:"elements_dev_entry"`
}

func (c *AdminConfig) manifest() admin.Manifest {
	return admin.Manifest{Elements: c.Elements, Theme: c.Theme, Strings: c.Strings, Widgets: c.Widgets, Cells: c.Cells,
		Panels: c.Panels, Actions: c.Actions, Dashboard: c.Dashboard, Views: c.Views, Slots: c.Slots}
}

func (c *AdminConfig) check() error {
	if !adminPrefix.MatchString(c.Prefix) {
		return fmt.Errorf("admin.prefix %q must start with / and not end with one, and use only letters, digits and . _ ~ - in its segments (default /admin)", c.Prefix)
	}
	if c.Prefix == "/api" || strings.HasPrefix(c.Prefix, "/api/") || c.Prefix == "/healthz" || c.Prefix == "/readyz" {
		return fmt.Errorf("admin.prefix %q collides with a core route", c.Prefix)
	}
	if !devEntry.MatchString(c.ElementsDevEntry) {
		return fmt.Errorf("admin.elements_dev_entry %q must start with / and use only letters, digits and . _ ~ @ - / (default /src/elements.ts)", c.ElementsDevEntry)
	}
	return nil
}

var devEntry = regexp.MustCompile(`^/[A-Za-z0-9._~@/-]+$`)

var adminPrefix = regexp.MustCompile(`^/[A-Za-z0-9._~-]+(/[A-Za-z0-9._~-]+)*$`)

type CoreConfig struct {
	HTTP     HTTPConfig
	Database DatabaseConfig
	Log      LogConfig
	Outbox   OutboxConfig
	Jobs     JobsConfig
	Content  ContentConfig
	Manage   ManageConfig
	Auth     AuthConfig
	Delivery DeliveryConfig
	Admin    AdminConfig
	// Webhooks deliver outbox events to HTTP endpoints, by name.
	Webhooks map[string]webhook.Config
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
	// Releases publish groups of items together, now or scheduled.
	Releases *release.Service
	// Channels are the compiled delivery channels (config/channels.yaml); one implicit "default" channel when
	// none are configured.
	Channels *channel.Set
	// Delivery is the delivery API (/api/deliver/v1): response cache, purge listener and preview tokens. It is
	// built after every module booted, so it is nil in Boot.
	Delivery *delivery.API
	// Catalog lists policies, limitations and code access rules; Access authorizes the principal in a context
	// (no principal: the system). Auth authenticates requests; Admin manages users, groups, roles and tokens.
	Catalog *access.Catalog
	Access  *access.Checker
	Auth    *access.Authenticator
	Admin   *access.Admin

	reg     *Registry
	mappers []cachetag.Mapper
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
	// JobHold is how long a job keeps its lock after a run (default 2s); tests with a manual clock shorten it.
	JobHold time.Duration
	// PasswordIterations lowers the PBKDF2 cost of password hashes so tests run fast (0 = 600 000). Never read
	// from configuration.
	PasswordIterations int
}

type App struct {
	services *Services
	modules  []Module
	reg      *Registry
	ownDB    bool
	runner   *jobs.Runner
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
	mappers := tagMappers(p.schema, reg.mappers)
	consumers = append(consumers, purgeConsumer(db, mappers))
	consumers = append(consumers, webhookConsumers(core.Webhooks, o.Clock,
		func(topic string, payload json.RawMessage) []string { return cachetag.Tags(mappers, topic, payload) })...)
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
	catalog := reg.accessCatalog()
	checker := access.NewChecker(access.CheckerOptions{DB: db, Catalog: catalog, Schema: p.schema, Log: log})
	proxies, _ := trustedProxies(core.HTTP.TrustedProxies) // validated by prepare
	authn, err := access.NewAuthenticator(access.AuthOptions{DB: db, Clock: o.Clock, Rand: o.Rand, Log: log,
		ConfigToken: core.Manage.Token, DevToken: core.Manage.Token == DevManageToken, SessionIdle: core.Auth.SessionIdle, SessionMax: core.Auth.SessionMax,
		InsecureCookies: core.Auth.InsecureCookies, TrustedProxies: proxies, PasswordIterations: o.PasswordIterations})
	if err == nil {
		var admin *access.Admin
		admin, err = access.NewAdmin(access.AdminOptions{DB: db, Outbox: ob, Audit: al, Clock: o.Clock, IDs: gen,
			Rand: o.Rand, Catalog: catalog, Checker: checker, Log: log, PasswordIterations: o.PasswordIterations})
		if err == nil {
			svc, err = content.New(content.Options{DB: db, Schema: p.schema, Hooks: hookReg, Outbox: ob, Audit: al,
				Clock: o.Clock, IDs: gen, DefaultLocale: core.Content.DefaultLocale, Migrations: migrations, Access: checker})
			a.services = &Services{
				Env: cfg.Env(), Config: cfg, Core: core, Log: log, Clock: o.Clock, IDs: gen, DB: db, Outbox: ob,
				Audit: al, Schema: p.schema, Hooks: hookReg, Content: svc, Channels: p.chans, Catalog: catalog,
				Access: checker, Auth: authn, Admin: admin, reg: reg, mappers: mappers,
			}
		}
	}
	if err != nil {
		a.closeDB(db)
		return nil, err
	}
	if a.services.Releases, err = newReleases(a.services); err != nil {
		a.closeDB(db)
		return nil, err
	}
	if a.runner, err = newRunner(a.services, reg, o.JobHold); err != nil {
		a.closeDB(db)
		return nil, err
	}
	for _, m := range ordered {
		if b, ok := m.(Booter); ok {
			if err := b.Boot(ctx, a.services); err != nil {
				a.closeDB(db)
				return nil, fmt.Errorf("module %s: boot: %w", m.Name(), err)
			}
		}
	}
	if a.services.Delivery, err = newDelivery(a.services, reg); err != nil {
		a.closeDB(db)
		return nil, err
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
		Content: ContentConfig{SchemaDir: "schema", DefaultLocale: "en", TrashRetention: DefaultTrashRetention},
		Delivery: DeliveryConfig{CacheBytes: delivery.DefaultCacheBytes, ShareLinks: true,
			ShareDefaultTTL: delivery.DefaultShareTTL, ShareMaxTTL: delivery.MaxShareTTL},
		Jobs:  JobsConfig{Enabled: true},
		Admin: AdminConfig{Enabled: true, Prefix: "/admin", ElementsDevEntry: "/src/elements.ts"},
	}
	sections := []struct {
		path   string
		target any
	}{{"content", &core.Content}}
	if runtime {
		sections = append(sections, []struct {
			path   string
			target any
		}{{"http", &core.HTTP}, {"database", &core.Database}, {"log", &core.Log}, {"outbox", &core.Outbox}, {"jobs", &core.Jobs},
			{"manage", &core.Manage}, {"auth", &core.Auth}, {"delivery", &core.Delivery}, {"admin", &core.Admin}, {"webhooks", &core.Webhooks}}...)
	}
	for _, d := range sections {
		if err := cfg.Decode(d.path, d.target); err != nil {
			return nil, err
		}
	}
	reg := newRegistry()
	reg.env = cfg.Env()
	if runtime {
		reg.current = "core"
		if core.Content.TrashRetention < 0 {
			return nil, fmt.Errorf("content.trash_retention must not be negative (0 keeps trashed items forever)")
		}
		if err := reg.Job("auth.cleanup", "0 * * * *", cleanupSessions); err != nil {
			return nil, err
		}
		if err := reg.Job("content.trash", "@hourly", purgeTrash(core.Content.TrashRetention)); err != nil {
			return nil, err
		}
	}
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
		if err := addCoreLimitations(reg, s, p.chans); err != nil {
			return nil, err
		}
		for _, typ := range reg.accessCatalog().RuleTypes() {
			if _, ok := s.Type(typ); !ok {
				return nil, fmt.Errorf("access rules for unknown content type %q", typ)
			}
		}
		for _, e := range reg.endpoints {
			if name, ok := e.value.auth.policy(); ok {
				if _, known := reg.accessCatalog().Policy(name); !known {
					return nil, fmt.Errorf("module %s: endpoint %s %s: unknown policy %q (declare it with Registry.Policy)",
						e.module, e.value.method, e.value.path, name)
				}
			}
		}
		if err := p.checkRuntime(cfg); err != nil {
			return nil, err
		}
		if p.core.Admin.Enabled {
			if _, err := composeAdmin(&p.core.Admin, reg, s, cfg.Env()); err != nil {
				return nil, err
			}
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
	if err := validateWebhooks(p.core.Webhooks, dev); err != nil {
		return err
	}
	if err := p.core.Auth.check(dev); err != nil {
		return err
	}
	if err := p.core.Admin.check(); err != nil {
		return err
	}
	if _, err := trustedProxies(p.core.HTTP.TrustedProxies); err != nil {
		return err
	}
	// The dev token stands in for the config token while developing, wherever something needs credentials.
	needsToken := len(p.schema.Types()) > 0
	for _, e := range p.reg.endpoints {
		needsToken = needsToken || e.value.auth != Public
	}
	if p.core.Manage.Token == "" && needsToken && dev {
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

// Inspect returns the compiled schema and channels without opening the database, for tools that only need the
// project's shape (check-frontend). Runtime configuration must be valid, as for Check.
func Inspect(cfg *config.Config, mods ...Module) (*schema.Schema, *channel.Set, []string, error) {
	p, err := prepare(cfg, mods, true)
	if err != nil {
		return nil, nil, nil, err
	}
	names := make([]string, len(p.ordered))
	for i, m := range p.ordered {
		names[i] = m.Name()
	}
	return p.schema, p.chans, names, nil
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
	const prefix = "/api/manage/v1"
	{
		api, err := manage.New(manage.Options{Content: a.services.Content, Auth: a.services.Auth, Gate: a.services.Access,
			Admin: a.services.Admin, Catalog: a.services.Catalog,
			Channels: a.services.Channels, Log: a.services.Log, Prefix: prefix,
			Routes: slices.Concat(a.reg.manageRts, a.services.Delivery.ManageRoutes(), a.services.Releases.ManageRoutes())})
		if err != nil {
			return nil, err
		}
		mux.Handle(prefix+"/", http.StripPrefix(prefix, api))
	}
	if len(a.services.Schema.Types()) > 0 {
		mux.Handle(delivery.Prefix+"/", http.StripPrefix(delivery.Prefix, a.services.Delivery.Handler()))
	}
	if err := a.mountAdmin(mux); err != nil {
		return nil, err
	}
	if err := a.mountEndpoints(mux); err != nil {
		return nil, err
	}
	for _, rt := range a.reg.routes {
		if err := handle(mux, rt.value.pattern, rt.value.handler); err != nil {
			return nil, fmt.Errorf("module %s: route %q: %w", rt.module, rt.value.pattern, err)
		}
	}
	return recoverer(a.services.Log, mux), nil
}

// mountAdmin serves the admin app. In dev, ADMIN_DEV_URL (a Vite dev server) replaces the embedded build.
func (a *App) mountAdmin(mux *http.ServeMux) error {
	s := a.services
	if !s.Core.Admin.Enabled {
		return nil
	}
	o := admin.Options{APIPrefix: "/api/manage/v1", DeliveryPrefix: delivery.Prefix, Pages: slices.Contains(a.Modules(), "pages")}
	o.FrameOrigins = frameOrigins(s, a.reg)
	if s.Env == config.DevEnv {
		o.DevURL = os.Getenv("ADMIN_DEV_URL")
		o.ElementsDevURL = os.Getenv("ADMIN_ELEMENTS_DEV_URL")
	}
	ext, err := composeAdmin(&s.Core.Admin, a.reg, s.Schema, s.Env)
	if err != nil {
		return err
	}
	o.Extensions = ext
	if err := handle(mux, s.Core.Admin.Prefix+"/", admin.Handler(s.Core.Admin.Prefix, admin.Dist(), o)); err != nil {
		return fmt.Errorf("admin.prefix %q: %w", s.Core.Admin.Prefix, err)
	}
	return nil
}

// frameOrigins are the origins of the channels' frontends: what the admin may frame and what may read the
// preview event stream.
func frameOrigins(s *Services, reg *Registry) []string {
	var out []string
	for _, ch := range s.Channels.All() {
		origin := ch.Frontend
		for _, f := range reg.frameOrigins {
			if x := f(ch); x != "" {
				origin = x
				break
			}
		}
		if origin != "" {
			out = append(out, origin)
		}
	}
	return out
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
	wg.Go(func() {
		if err := s.Access.Run(ctx); err != nil {
			s.Log.Error("access grant invalidation stopped", "err", err)
		}
	})
	if err := s.Delivery.Start(ctx); err != nil {
		cancel()
		return fmt.Errorf("delivery: %w", err)
	}
	if s.Core.Jobs.Enabled {
		wg.Go(func() {
			if err := a.runner.Run(ctx); err != nil {
				s.Log.Error("job runner stopped", "err", err)
			}
		})
	} else {
		s.Log.Info("scheduled jobs are disabled on this node (jobs.enabled: false)")
	}

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
		s.Log.Warn("the config token is the dev token, which has every right; set manage.token before exposing this server")
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

func newRunner(s *Services, reg *Registry, hold time.Duration) (*jobs.Runner, error) {
	list := make([]jobs.Job, len(reg.jobs))
	for i, j := range reg.jobs {
		list[i] = jobs.Job{Name: j.value.name, Schedule: j.value.schedule,
			Run: func(ctx context.Context) error { return j.value.fn(ctx, s) }}
	}
	list = append(list, releaseJob(s))
	list = append(list, jobs.Job{Name: "preview.cleanup", Schedule: "@every 10m",
		Run: func(ctx context.Context) error { return s.Delivery.CleanupPreview(ctx) }})
	return jobs.NewRunner(jobs.Options{Clock: s.Clock, Locker: s.DB.Locker(), Log: s.Log, Hold: hold}, list)
}
