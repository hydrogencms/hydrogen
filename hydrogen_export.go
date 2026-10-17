package hydrogen

import (
	"context"
	"flag"
	"fmt"

	"github.com/hydrogencms/hydrogen/core/kernel"
)

// parseDirArgs parses flags and the one directory argument, in any order.
func parseDirArgs(fs *flag.FlagSet, args []string) (string, error) {
	var dir string
	for {
		if err := fs.Parse(args); err != nil {
			return "", err
		}
		if fs.NArg() == 0 {
			break
		}
		if dir != "" {
			return "", fmt.Errorf("%s takes one directory", fs.Name())
		}
		dir, args = fs.Arg(0), fs.Args()[1:]
	}
	if dir == "" {
		return "", fmt.Errorf("usage: %s [flags] <dir>", fs.Name())
	}
	return dir, nil
}

func exportCommand(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	published := fs.Bool("published-only", false, "export only the published version of each published item (no history, no drafts)")
	dir, err := parseDirArgs(fs, args)
	if err != nil {
		return err
	}
	m, err := app.Export(ctx, dir, kernel.ExportOptions{Version: Version(), PublishedOnly: *published, Progress: o.Stdout})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(o.Stdout, "exported %d items (%d versions, %d drafts) to %s\n", m.Counts.Items, m.Counts.Versions, m.Counts.Drafts, dir)
	return err
}

func importCommand(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	allow := fs.Bool("allow-schema-change", false, "import an export made under a different schema")
	dir, err := parseDirArgs(fs, args)
	if err != nil {
		return err
	}
	pending, err := app.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d pending database migrations; run migrate first", len(pending))
	}
	_, err = app.Import(ctx, dir, kernel.ImportOptions{AllowSchemaChange: *allow, Progress: o.Stdout})
	return err
}
