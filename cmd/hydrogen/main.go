// Command hydrogen is the developer tool: it creates projects, runs them with live reload, and runs a
// project's own commands (serve, migrate, config:check, …) by building the project in the current directory.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hydrogencms/hydrogen/internal/devwatch"
	"github.com/hydrogencms/hydrogen/internal/projectfile"
	"github.com/hydrogencms/hydrogen/internal/scaffold"
)

const usage = `Usage:
  hydrogen new [-module path] [-core dir] [-no-tidy] <dir>   create a project
  hydrogen dev [command args]                                 run with live reload (default: serve)
  hydrogen generate [-go dir] [-ts file]                      write typed Go and TypeScript from the schema
                                                              (and modules_gen.go from hydrogen.yaml)
  hydrogen add <module|path[@version]>…                       add modules to hydrogen.yaml and go.mod
  hydrogen remove <module|path>…                              remove modules
  hydrogen build [-o path] [go build flags]                   compile the project into bin/<name> (no cgo)
  hydrogen hooks build [files…]                               compile TypeScript hooks (default hooks/*.ts) to
                                                              hooks/<name>.wasm with esbuild and Javy ($JAVY or PATH)
  hydrogen modules                                            list the catalog; * marks this project's modules
  hydrogen modules -versions                                  build and run the project's modules command
  hydrogen modules init <module|path>…                        create hydrogen.yaml for a project that wires
                                                              modules by hand
  hydrogen version                                            print the version
  hydrogen <command> [args]                                   build the project here and run one of its
                                                              commands: serve, migrate, config:check, …

Environment for hydrogen dev:
  ADMIN_DEV_URL   e.g. http://localhost:5173: proxy the admin prefix to a Vite dev server (hot reload for
                  the admin app itself) instead of serving the embedded build
  ADMIN_ELEMENTS_DEV_URL
                  e.g. http://localhost:5174: proxy <admin.prefix>/x/dev/* to the Vite dev server of your admin
                  extensions (admin/) and list its entry (admin.elements_dev_entry, default /src/elements.ts)
                  in place of the built files; Vite's base must be <admin.prefix>/x/dev/ (the scaffold's is)
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	if len(args) > 1 && (args[1] == "-h" || args[1] == "--help") && args[0] != "new" {
		if text := commandUsage(args[0]); text != "" {
			fmt.Fprint(stdout, text)
			return 0
		}
	}
	var err error
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, "hydrogen", cliVersion())
		return 0
	case "new":
		err = newProject(ctx, args[1:], stdout, stderr)
	case "dev":
		err = dev(ctx, args[1:], stdout, stderr)
	case "generate":
		err = generate(ctx, args[1:], stdout, stderr)
	case "add":
		err = addModules(ctx, args[1:], stdout, stderr)
	case "remove":
		err = removeModules(ctx, args[1:], stdout, stderr)
	case "build":
		err = buildCmd(ctx, args[1:], stdout, stderr)
	case "hooks":
		err = hooksCmd(ctx, args[1:], stdout, stderr)
	case "modules":
		err = modulesCmd(ctx, args[1:], stdout, stderr)
	default:
		var code int
		code, err = passthrough(ctx, args, stdout, stderr)
		if err == nil {
			return code
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "hydrogen:", err)
		return 1
	}
	return 0
}

func newProject(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	module := fs.String("module", "", "Go module path (default: the directory name)")
	core := fs.String("core", "", "use a local hydrogen checkout (replace directives) instead of published versions")
	noTidy := fs.Bool("no-tidy", false, "skip go mod tidy")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("new: give exactly one target directory")
	}
	dir := fs.Arg(0)
	if err := scaffold.Write(scaffold.Options{Dir: dir, Module: *module, Core: *core, Version: releaseVersion()}); err != nil {
		return err
	}
	if !*noTidy {
		cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
		cmd.Dir, cmd.Stdout, cmd.Stderr = dir, stderr, stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go mod tidy failed (%v); fix and rerun it in %s", err, dir)
		}
	}
	fmt.Fprintf(stdout, "Created %s. Next:\n  cd %s\n  hydrogen dev    # then open the link it prints to create your admin account\n", dir, dir)
	return nil
}

func projectDir() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if !devwatch.IsProject(dir) {
		return "", errors.New("not in a Hydrogen project (no go.mod requiring github.com/hydrogencms/hydrogen); see hydrogen new")
	}
	return dir, nil
}

func dev(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	dir, err := projectDir()
	if err != nil {
		return err
	}
	return devwatch.Run(ctx, devwatch.Options{Dir: dir, Args: args, Stdout: stdout, Stderr: stderr})
}

// generate builds the project (bootstrapping gen/ from the schema files if stale generated code breaks the
// build) and runs its generate command, which includes module schema changes.
func generate(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	dir, err := projectDir()
	if err != nil {
		return err
	}
	bin := filepath.Join(dir, ".work", "bin", "app")
	if err := devwatch.Prepare(ctx, dir, bin, stderr); err != nil {
		return err
	}
	_, err = devwatch.Generate(ctx, dir, bin, args, stdout, stderr)
	return err
}

// passthrough builds the project and runs one of its commands, forwarding signals and the exit code.
func passthrough(ctx context.Context, args []string, stdout, stderr io.Writer) (int, error) {
	dir, err := projectDir()
	if err != nil {
		return 1, err
	}
	bin := filepath.Join(dir, ".work", "bin", "app")
	if _, err := projectfile.Sync(dir); err != nil {
		return 1, err
	}
	if err := devwatch.Build(ctx, dir, bin, stderr); err != nil {
		return 1, fmt.Errorf("build failed")
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Stdin, cmd.Stdout, cmd.Stderr = dir, os.Stdin, stdout, stderr
	if err := cmd.Start(); err != nil {
		return 1, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = cmd.Process.Signal(os.Interrupt)
		err = <-done
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}
