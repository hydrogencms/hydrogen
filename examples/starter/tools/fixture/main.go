// Command fixture rebuilds the committed fixture/ export of the starter project: pages and posts in English and
// German that use every block, a shared block, a manual redirect, a never-published draft page and the site
// settings. It creates the content through the Local API on a scratch SQLite database with a fixed clock and a
// seeded random source, so the output only changes when this program or the schema does. Run it from the
// project directory with `make starter-fixture`.
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
	"github.com/hydrogencms/hydrogen/modules/pages"
	"github.com/hydrogencms/hydrogen/modules/sqlite"
)

func main() {
	out := flag.String("out", "fixture", "directory to write the export to (replaced)")
	flag.Parse()
	if err := run(context.Background(), *out); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, out string) error {
	tmp, err := os.MkdirTemp("", "starter-fixture-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	env := map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(tmp, "fixture.db")}
	cfg, err := config.Load(config.Options{Dir: "config", LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }})
	if err != nil {
		return err
	}
	clk := clock.NewManual(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC))
	seed := [32]byte{'s', 't', 'a', 'r', 't', 'e', 'r'}
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
	b := &builder{ctx: ctx, svc: s.Content, pages: pg, clk: clk, id: map[string]ids.UUID{}}
	if err := b.build(); err != nil {
		return err
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	m, err := app.Export(ctx, out, kernel.ExportOptions{Version: "fixture"})
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s: %d items, %d versions, %d drafts\n", out, m.Counts.Items, m.Counts.Versions, m.Counts.Drafts)
	return nil
}

// builder creates the content. After the first error the remaining calls are skipped.
type builder struct {
	ctx   context.Context
	svc   *content.Service
	pages *pages.Service
	clk   *clock.Manual
	id    map[string]ids.UUID // item ids by name
	node  map[string]*pages.Node
	err   error
}

func (b *builder) tick(err error) {
	if b.err == nil {
		b.err = err
	}
	b.clk.Advance(time.Minute)
}

func (b *builder) create(name, typ string, fields map[string]any) {
	if b.err != nil {
		return
	}
	v, err := b.svc.Create(b.ctx, content.CreateInput{Type: typ, Locale: "en", Fields: fields})
	if err == nil {
		b.id[name] = v.ID
	}
	b.tick(err)
}

func (b *builder) patch(name, locale string, fields map[string]any) {
	if b.err != nil {
		return
	}
	_, err := b.svc.Update(b.ctx, b.id[name], content.UpdateInput{Locale: locale, Fields: fields})
	b.tick(err)
}

func (b *builder) publish(name string) {
	if b.err != nil {
		return
	}
	_, err := b.svc.Publish(b.ctx, b.id[name], content.PublishOptions{})
	b.tick(err)
}

func (b *builder) place(name, parent, after string) {
	if b.err != nil {
		return
	}
	in := pages.Place{Item: b.id[name]}
	if parent != "" {
		in.Parent = &b.node[parent].ID
	}
	if after != "" {
		in.After = &b.node[after].ID
	}
	n, err := b.pages.Place(b.ctx, "main", in)
	if err == nil {
		b.node[name] = n
	}
	b.tick(err)
}

func (b *builder) build() error {
	b.node = map[string]*pages.Node{}
	r := refs(b.id)
	for _, c := range catalog {
		b.create(c.name, c.typ, map[string]any{"title": c.title["en"]})
	}
	if b.err != nil {
		return b.err
	}
	for _, c := range catalog {
		for _, loc := range []string{"en", "de"} {
			fields := c.fields(loc, r)
			if fields == nil {
				continue
			}
			b.patch(c.name, loc, fields)
		}
	}
	for _, c := range catalog {
		if !c.draft {
			b.publish(c.name)
		}
	}
	b.place("home", "", "")
	b.place("about", "home", "")
	b.place("services", "home", "about")
	b.place("contact", "home", "services")
	b.place("soon", "home", "contact")
	b.place("team", "about", "")
	b.place("careers", "about", "team")

	b.create("oldAbout", "redirect", map[string]any{"from": "/company", "to": "item:" + b.id["about"].String(), "statusCode": "301"})
	b.publish("oldAbout")

	for _, loc := range []string{"en", "de"} {
		if b.err == nil {
			_, err := b.svc.UpdateSingleton(b.ctx, "siteSettings", content.UpdateInput{Locale: loc, Fields: settings[loc]})
			b.tick(err)
		}
	}
	if b.err == nil {
		_, err := b.svc.PublishSingleton(b.ctx, "siteSettings", content.PublishOptions{})
		b.tick(err)
	}
	return b.err
}
