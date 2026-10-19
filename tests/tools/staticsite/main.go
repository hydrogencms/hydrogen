// Command staticsite rebuilds internal/scaffold/static/content, the starter content of `hydrogen new -static`: a
// home page, an about page and the site settings, published. It writes the scaffold's schema and configuration to a
// scratch project, creates the content through the Local API on a scratch SQLite database with a fixed clock and a
// seeded random source, and exports it, so the output only changes when this program or the schema does. Run it
// from the tests directory with `make static-content`; TestStaticContentIsCurrent fails when the committed
// directory is stale.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/internal/scaffold"
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func main() {
	out := flag.String("out", "../internal/scaffold/static/content", "directory to write the export to (replaced)")
	flag.Parse()
	if err := run(context.Background(), *out); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run exports into out. The schema directory is relative to the working directory, so it works inside the scratch
// project and restores the directory afterwards.
func run(ctx context.Context, out string) error {
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	prev, err := os.Getwd()
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "static-content-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	project := filepath.Join(tmp, "project")
	if err := scaffold.Write(scaffold.Options{Dir: project, Static: true}); err != nil {
		return err
	}
	if err := os.Chdir(project); err != nil {
		return err
	}
	defer func() { _ = os.Chdir(prev) }()
	env := map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(tmp, "content.db")}
	cfg, err := config.Load(config.Options{Dir: "config", LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	if err != nil {
		return err
	}
	clk := clock.NewManual(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	seed := [32]byte{'s', 't', 'a', 't', 'i', 'c'}
	app, err := kernel.New(ctx, kernel.Options{Config: cfg, Clock: clk, Rand: rand.NewChaCha8(seed), Stderr: io.Discard},
		sqlite.Module(), pages.Module())
	if err != nil {
		return err
	}
	defer func() { _ = app.Close() }()
	if err := app.Migrate(ctx); err != nil {
		return err
	}
	s := app.Services()
	if _, err := s.Content.EnsureSchemaRev(ctx); err != nil {
		return err
	}
	pg, ok := kernel.LookupService[*pages.Service](s)
	if !ok {
		return fmt.Errorf("the pages module is not loaded")
	}
	b := &builder{ctx: ctx, svc: s.Content, pages: pg, clk: clk}
	b.build()
	if b.err != nil {
		return b.err
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	m, err := app.Export(ctx, out, kernel.ExportOptions{Version: "static"})
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d items, %d versions\n", out, m.Counts.Items, m.Counts.Versions)
	return nil
}

// builder creates the content. After the first error the remaining calls are skipped.
type builder struct {
	ctx   context.Context
	svc   *content.Service
	pages *pages.Service
	clk   *clock.Manual
	err   error
}

func (b *builder) tick(err error) {
	if b.err == nil {
		b.err = err
	}
	b.clk.Advance(time.Minute)
}

func (b *builder) create(title string) ids.UUID {
	if b.err != nil {
		return ids.UUID{}
	}
	v, err := b.svc.Create(b.ctx, content.CreateInput{Type: "page", Locale: "en", Fields: map[string]any{"title": title}})
	b.tick(err)
	return v.ID
}

func (b *builder) fill(id ids.UUID, fields map[string]any) {
	if b.err != nil {
		return
	}
	_, err := b.svc.Update(b.ctx, id, content.UpdateInput{Locale: "en", Fields: fields})
	b.tick(err)
	if b.err == nil {
		_, err = b.svc.Publish(b.ctx, id, content.PublishOptions{})
		b.tick(err)
	}
}

func (b *builder) place(id ids.UUID, parent *pages.Node) *pages.Node {
	if b.err != nil {
		return nil
	}
	in := pages.Place{Item: id}
	if parent != nil {
		in.Parent = &parent.ID
	}
	n, err := b.pages.Place(b.ctx, "main", in)
	b.tick(err)
	return n
}

func (b *builder) build() {
	home, about := b.create("Home"), b.create("About")
	b.fill(home, homeFields(about.String()))
	b.fill(about, aboutFields(home.String()))
	root := b.place(home, nil)
	b.place(about, root)
	if b.err != nil {
		return
	}
	_, err := b.svc.UpdateSingleton(b.ctx, "siteSettings", content.UpdateInput{Locale: "en", Fields: map[string]any{
		"siteName": "My site", "footerText": "Built with Hydrogen. The content of this site lives in git.",
	}})
	b.tick(err)
	if b.err == nil {
		_, err = b.svc.PublishSingleton(b.ctx, "siteSettings", content.PublishOptions{})
		b.tick(err)
	}
}
