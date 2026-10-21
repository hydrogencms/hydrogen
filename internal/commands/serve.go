package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

// serve starts the server. rebuilt is set when this start (re)built the database from content.files.
func serve(ctx context.Context, app *kernel.App, o Options, rebuilt bool) error {
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
	if err := readyContent(ctx, app, rebuilt); err != nil {
		return err
	}
	if err := s.Admin.SeedTemplates(ctx); err != nil {
		return err
	}
	return app.Run(ctx, o.Listening)
}

// readyContent checks that stored content matches the schema: breaking changes need migrations, which apply
// automatically in dev, and the current schema is recorded as a revision. A database rebuilt from content.files
// is a disposable copy of files that stay as they are, so its migrations apply in any environment.
func readyContent(ctx context.Context, app *kernel.App, rebuilt bool) error {
	s := app.Services()
	breaking, err := s.Content.BreakingChanges(ctx)
	if err != nil {
		return err
	}
	if len(breaking) > 0 {
		msg := "schema changes need a content migration:\n  " + strings.Join(breaking, "\n  ")
		if s.Core.Content.Files != "" {
			msg += "\nwrite a content migration, run hydrogen dev (or content:migrate) to apply it, then content:save and commit " + s.Core.Content.Files
		}
		return errors.New(msg)
	}
	pending, err := s.Content.PendingMigrations(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		if s.Env != config.DevEnv && !rebuilt {
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

func migrate(ctx context.Context, app *kernel.App, o Options) error {
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

func contentMigrate(ctx context.Context, app *kernel.App, o Options) error {
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

func contentReindex(ctx context.Context, app *kernel.App, o Options, args []string) error {
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

func jobRun(ctx context.Context, app *kernel.App, o Options, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: job:run <name> (jobs: %s)", strings.Join(app.JobNames(), ", "))
	}
	if err := app.RunJob(ctx, args[0]); err != nil {
		return err
	}
	_, err := fmt.Fprintln(o.Stdout, "job", args[0], "done")
	return err
}
