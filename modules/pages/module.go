package pages

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"

	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/outbox"
)

//go:embed migrations
var migrationFS embed.FS

var siteName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Module returns the pages module.
func Module() kernel.Module { return &module{cfg: Config{Sites: []string{"main"}}} }

type module struct {
	cfg Config
	svc *Service
}

func (*module) Name() string { return "pages" }

func (m *module) Config() any { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	if err := m.cfg.validate(); err != nil {
		return err
	}
	r.Migrations(kernel.MigrationSource{Source: "pages", FS: func(dialect string) (fs.FS, error) {
		if dialect != "sqlite" && dialect != "postgres" {
			return nil, fmt.Errorf("pages: no migrations for dialect %q", dialect)
		}
		return fs.Sub(migrationFS, "migrations/"+dialect)
	}})
	r.TypeOption(childrenOption, checkChildren)
	for _, o := range channelOptions(m.cfg.Sites) {
		r.ChannelOption(o)
	}
	r.ChannelCheck(checkMatchers)
	if err := r.Exporter("pages", exporter{func() (*service, error) { return m.svc.get() }}); err != nil {
		return err
	}

	m.svc = &Service{}
	if err := m.registerAccess(r); err != nil {
		return err
	}
	kernel.Provide(r, m.svc)
	registerDelivery(r, func() *Service { return m.svc })
	for _, rt := range routes(func() *Service { return m.svc }) {
		r.ManageRoute(rt)
	}

	for _, h := range []struct {
		event hooks.Event
		fn    func(*service, *hooks.Context) error
	}{
		{hooks.BeforePublish, (*service).beforePublish},
		{hooks.AfterPublish, (*service).afterPublish},
		{hooks.BeforeDelete, (*service).beforeDelete},
		{hooks.AfterDelete, (*service).afterDelete},
	} {
		err := r.AddHook(hooks.Hook{Event: h.event, Fn: func(hc *hooks.Context) error {
			impl, err := m.svc.get()
			if err != nil {
				return err
			}
			return h.fn(impl, hc)
		}})
		if err != nil {
			return err
		}
	}
	r.Consumer(outbox.Consumer{
		Name:   "pages.reconcile",
		Topics: []string{"item.published", "item.deleted"},
		Handle: func(ctx context.Context, e outbox.Event) error {
			impl, err := m.svc.get()
			if err != nil {
				return err
			}
			return impl.reconcile(ctx, e)
		},
	})
	return nil
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	m.svc.impl = newService(m.cfg.Sites, s)
	return nil
}

func (m *module) Start(ctx context.Context) error {
	if m.svc.impl == nil {
		return nil
	}
	return m.svc.impl.ensureAccessIndex(ctx)
}

func (c *Config) validate() error {
	if len(c.Sites) == 0 {
		return fmt.Errorf("pages: modules.pages.sites must name at least one site")
	}
	for i, s := range c.Sites {
		if !siteName.MatchString(s) {
			return fmt.Errorf("pages: site name %q: use lowercase letters, digits and dashes, starting with a letter", s)
		}
		if slices.Contains(c.Sites[:i], s) {
			return fmt.Errorf("pages: site %q is listed twice", s)
		}
	}
	return nil
}
