package devwatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"

	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/internal/codegen"
	"github.com/hydrogencms/hydrogen/internal/projectfile"
)

// Prepare builds the project, after refreshing modules_gen.go from hydrogen.yaml. If the build fails, it regenerates gen/ from the schema files alone (stale
// generated code is the usual cause after a schema change) and builds again.
func Prepare(ctx context.Context, dir, bin string, stderr io.Writer) error {
	if _, err := projectfile.Sync(dir); err != nil {
		fmt.Fprintln(stderr, err)
		return err
	}
	var out bytes.Buffer
	if err := Build(ctx, dir, bin, &out); err == nil {
		return nil
	}
	if err := Bootstrap(dir); err != nil {
		_, _ = stderr.Write(out.Bytes())
		return fmt.Errorf("build failed (regenerating gen/ from the schema files also failed: %w)", err)
	}
	if err := Build(ctx, dir, bin, stderr); err != nil {
		return fmt.Errorf("build failed")
	}
	return nil
}

// Bootstrap writes gen/ from the project's schema files without module transforms. The project's own
// generate command produces the complete version once the project builds.
func Bootstrap(dir string) error {
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadDir(filepath.Join(dir, "schema")); err != nil {
		return err
	}
	s, err := b.Compile()
	if err != nil {
		return err
	}
	if _, err := codegen.WriteGo(filepath.Join(dir, "gen", "content"), s.Types(), s.Groups(), codegen.GoOptions{Blocks: s.Blocks(), Tokens: s.Tokens()}); err != nil {
		return err
	}
	if err := codegen.WriteTypeScript(filepath.Join(dir, "gen", "types.ts"), s.Types(), s.Groups(), codegen.TSOptions{Blocks: s.Blocks(), Tokens: s.Tokens()}); err != nil {
		return err
	}
	return codegen.WriteRichText(filepath.Join(dir, "gen", "richtext.ts"), s.RichText())
}

// Generate runs the project's generate command and reports whether generated files changed.
func Generate(ctx context.Context, dir, bin string, args []string, stdout, stderr io.Writer) (bool, error) {
	before, err := scan(filepath.Join(dir, "gen"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	cmd := exec.CommandContext(ctx, bin, append([]string{"generate"}, args...)...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stdout, stderr
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("generate failed: %w", err)
	}
	after, err := scan(filepath.Join(dir, "gen"))
	if err != nil {
		return false, err
	}
	return !same(before, after), nil
}

// prepareAndGenerate is the dev loop's build step: build, refresh gen/, and rebuild if it changed.
func prepareAndGenerate(ctx context.Context, dir, bin string, stderr io.Writer) error {
	if err := Prepare(ctx, dir, bin, stderr); err != nil {
		return err
	}
	changed, err := Generate(ctx, dir, bin, nil, io.Discard, stderr)
	if err != nil || !changed {
		return err
	}
	return Build(ctx, dir, bin, stderr)
}
