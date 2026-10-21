package hydrogen

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/kernel"
)

// adminPasswordEnv carries the password of users:create-admin; without it one line is read from stdin.
const adminPasswordEnv = "HYDROGEN_ADMIN_PASSWORD"

func requireMigrated(ctx context.Context, app *kernel.App) error {
	pending, err := app.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("%d pending database migrations; run migrate first", len(pending))
	}
	return nil
}

// createAdmin makes (or resets) a user with the administrator role. The password never appears in arguments or
// output.
func createAdmin(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return errors.New("usage: users:create-admin <login> (password from " + adminPasswordEnv + " or one line of stdin)")
	}
	if err := requireMigrated(ctx, app); err != nil {
		return err
	}
	pw := os.Getenv(adminPasswordEnv)
	if pw == "" {
		fmt.Print("Enter password: ")
		line, err := bufio.NewReader(io.LimitReader(o.Stdin, 4096)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read password: %w", err)
		}
		pw = strings.TrimRight(line, "\r\n")
	}
	if pw == "" {
		return errors.New("no password: set " + adminPasswordEnv + " or pipe one line to stdin")
	}
	u, err := app.Services().Admin.EnsureAdmin(ctx, args[0], pw)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(o.Stdout, "administrator %s ready (id %s)\n", u.Login, u.ID)
	return err
}

type listFlag []string

func (l *listFlag) String() string { return strings.Join(*l, ",") }

func (l *listFlag) Set(v string) error {
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			*l = append(*l, p)
		}
	}
	return nil
}

// createToken makes an API token with the named roles and prints its secret, once, as the only line on
// stdout; the token's id goes to stderr.
func createToken(ctx context.Context, app *kernel.App, o RunOptions, args []string) error {
	fs := flag.NewFlagSet("tokens:create", flag.ContinueOnError)
	fs.SetOutput(o.Stderr)
	name := fs.String("name", "", "token name (required)")
	agent := fs.Bool("agent", false, "an agent token: audit entries record actor kind agent")
	expires := fs.Duration("expires", 0, "valid for this long (default: never expires)")
	var roles listFlag
	fs.Var(&roles, "role", "role name to assign (repeat or comma-separate)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 || strings.TrimSpace(*name) == "" {
		return errors.New("usage: tokens:create -name <name> -role <role> [-agent] [-expires 720h]")
	}
	if *expires < 0 {
		return errors.New("-expires must not be negative")
	}
	if err := requireMigrated(ctx, app); err != nil {
		return err
	}
	s := app.Services()
	if err := s.Admin.SeedTemplates(ctx); err != nil {
		return err
	}
	in := access.TokenInput{Name: *name, Kind: "token"}
	if *agent {
		in.Kind = "agent"
	}
	if *expires > 0 {
		t := s.Clock.Now().Add(*expires)
		in.ExpiresAt = &t
	}
	for _, r := range roles {
		role, err := s.Admin.RoleByName(ctx, r)
		if err != nil {
			return fmt.Errorf("role %q: %w", r, err)
		}
		in.Roles = append(in.Roles, role.ID)
	}
	t, secret, err := s.Admin.CreateToken(ctx, in)
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Stderr, "created %s token %s (id %s)\n", t.Kind, t.Name, t.ID)
	_, err = fmt.Fprintln(o.Stdout, secret)
	return err
}
