package commands

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/internal/codegen"
)

func configCheck(ctx context.Context, mods []kernel.Module, o Options, args []string) error {
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
	if err := kernel.Check(cfg, mods...); err != nil {
		return err
	}
	warnings, err := kernel.Warnings(cfg, mods...)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		fmt.Fprintf(o.Stdout, "warning: %s\n", w)
	}
	if *withDB {
		app, err := newApp(ctx, mods, o)
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

func generate(mods []kernel.Module, o Options, args []string) error {
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
	s, err := kernel.CompileSchema(cfg, mods...)
	if err != nil {
		return err
	}
	n, err := codegen.WriteGo(*goDir, s.Types(), s.Groups(), codegen.GoOptions{Package: *pkg, Blocks: s.Blocks(), Tokens: s.Tokens()})
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Stdout, "wrote %d Go files to %s\n", n, *goDir)
	if *tsFile != "" {
		if err := codegen.WriteTypeScript(*tsFile, s.Types(), s.Groups(), codegen.TSOptions{Blocks: s.Blocks(), Tokens: s.Tokens()}); err != nil {
			return err
		}
		fmt.Fprintf(o.Stdout, "wrote %s\n", *tsFile)
		if err := codegen.WriteRichText(filepath.Join(filepath.Dir(*tsFile), "richtext.ts"), s.RichText()); err != nil {
			return err
		}
	}
	return nil
}
