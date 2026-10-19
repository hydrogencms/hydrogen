// Package siteexport renders the website of the site module to static files that a CDN or static host serves.
// `hydrogen site:export` writes <out>/<host or channel>/<path>/index.html for every page, item, listing page,
// custom route with static params and 404 page of the rendered channels, the fingerprinted assets, a
// manifest.json (file, content hash, cache tags) and redirect files, then hands the tree to a publisher
// (versioned directory, S3-compatible bucket, or a command). With modules.site-export.auto the running server
// re-exports incrementally when published content changes. See docs/site-export.md.
package siteexport

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/site"
)

// ConsumerName is the outbox consumer of incremental exports.
const ConsumerName = "site.export"

// Module returns the site-export module. It needs the site and pages modules.
func Module() kernel.Module {
	return &module{cfg: Config{Out: "site-export", Redirects: []string{"html"}, Concurrency: 8}}
}

type module struct {
	cfg   Config
	ex    *site.Exporter
	pages *pages.Service
	r     *runner
}

func (*module) Name() string       { return "site-export" }
func (*module) Requires() []string { return []string{"site", "pages"} }
func (m *module) Config() any      { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	if err := m.cfg.validate(); err != nil {
		return err
	}
	var ok bool
	if m.ex, ok = kernel.Lookup[*site.Exporter](r); !ok {
		return errors.New("site-export: the site module provided no exporter")
	}
	if m.pages, ok = kernel.Lookup[*pages.Service](r); !ok {
		return errors.New("site-export: the pages module provided no service")
	}
	kernel.Provide(r, &Service{m: m})
	if m.cfg.Auto {
		r.Consumer(outbox.Consumer{Name: ConsumerName, Topics: []string{"*"}, Handle: m.handle})
	}
	return r.Command(kernel.Command{Name: "site:export", Summary: "render the website to static files and publish them: site:export [-channel name] [-out dir] [-full]",
		Run: m.runCommand})
}

// Service runs exports from code (kernel.LookupService[*siteexport.Service]), as the command and the
// incremental consumer do.
type Service struct{ m *module }

// Export runs one export (see Options). It returns the result even when it fails after rendering.
func (s *Service) Export(ctx context.Context, o Options) (*Result, error) {
	if s.m.r == nil {
		return nil, errors.New("site-export: not booted")
	}
	return s.m.r.run(ctx, o)
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	m.r = &runner{ex: m.ex, pages: m.pages, db: s.DB, clock: s.Clock, log: s.Log, cfg: &m.cfg, chans: s.Channels.Get, tags: s.Tags}
	return nil
}

// handle re-exports what an event touched.
func (m *module) handle(ctx context.Context, e outbox.Event) error {
	if m.r == nil {
		return errors.New("site-export: not booted")
	}
	tags := m.r.tags(e.Topic, e.Payload)
	if len(tags) == 0 {
		return nil
	}
	res, err := m.r.run(ctx, Options{Tags: tags})
	if res != nil {
		m.r.log.Info("site-export: incremental export", "event", e.Topic, "rendered", res.Rendered, "written", len(res.Written),
			"removed", len(res.Removed), "redirects", res.Redirects, "published", res.Published)
		for _, w := range res.Warnings {
			m.r.log.Warn("site-export: " + w)
		}
	}
	return err
}

func (m *module) runCommand(ctx context.Context, app *kernel.App, args []string) error {
	return m.command(ctx, os.Stdout, args)
}

func (m *module) command(ctx context.Context, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("site:export", flag.ContinueOnError)
	fs.SetOutput(out)
	var o Options
	fs.StringVar(&o.Channel, "channel", "", "export one channel (default: every channel with render: go)")
	fs.StringVar(&o.Out, "out", "", "the work tree (default modules.site-export.out)")
	fs.BoolVar(&o.Full, "full", false, "rewrite every file, remove files the export does not know and publish everything again")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("usage: site:export [-channel name] [-out dir] [-full]")
	}
	if m.r == nil {
		return errors.New("site-export: not booted")
	}
	res, err := m.r.run(ctx, o)
	if res != nil {
		for _, w := range slices.Sorted(slices.Values(res.Warnings)) {
			fmt.Fprintln(out, "warning:", w)
		}
		fmt.Fprintf(out, "site:export %s: %s%s\n", res.Out, res, map[bool]string{true: ", published"}[res.Published])
	}
	return err
}
