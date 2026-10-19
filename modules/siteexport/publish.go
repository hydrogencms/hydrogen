package siteexport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
)

// publishInput is what a publisher delivers: the tree, its manifest, and what changed since the last run.
type publishInput struct {
	Tree     string
	Manifest *Manifest
	Changed  []string
	Removed  []string
	Full     bool
}

type publisher interface {
	Publish(ctx context.Context, in publishInput) error
}

func (r *runner) publisher() publisher {
	p := r.cfg.Publisher
	switch {
	case p.Dir.Path != "":
		return &dirPublisher{cfg: p.Dir, clock: r.clock}
	case p.S3.Bucket != "":
		return &s3Publisher{cfg: p.S3, clock: r.clock, conc: r.cfg.Concurrency, log: r.log}
	case len(p.Command.Argv) > 0:
		return &commandPublisher{cfg: p.Command, log: r.log}
	}
	return nil
}

// publisherID names the configured publisher and its target, so changing it publishes again.
func (r *runner) publisherID() string {
	p := r.cfg.Publisher
	switch {
	case p.Dir.Path != "":
		abs, _ := filepath.Abs(p.Dir.Path)
		return "dir:" + abs
	case p.S3.Bucket != "":
		return "s3:" + p.S3.Endpoint + "/" + p.S3.Bucket + "/" + p.S3.Prefix
	case len(p.Command.Argv) > 0:
		return "command:" + strings.Join(p.Command.Argv, "\x00")
	}
	return ""
}

// published lists the manifest files a publisher delivers: everything but the manifest itself.
func published(m *Manifest) []string {
	var out []string
	for f := range m.Files {
		if f != ManifestName {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// ---- dir

const (
	releasesDir = "releases"
	currentLink = "current"
)

type dirPublisher struct {
	cfg   DirConfig
	clock clock.Clock
}

func (p *dirPublisher) Publish(_ context.Context, in publishInput) error {
	base, err := filepath.Abs(p.cfg.Path)
	if err != nil {
		return err
	}
	rel := filepath.Join(base, releasesDir)
	if err := os.MkdirAll(rel, 0o755); err != nil {
		return err
	}
	cur := filepath.Join(base, currentLink)
	if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s exists and is not a symlink: remove it so the publisher can manage it", cur)
	}
	var prevDir string
	if t, err := os.Readlink(cur); err == nil {
		prevDir = filepath.Join(base, t)
	}
	var prev *Manifest
	if prevDir != "" {
		prev, _ = loadManifestFile(prevDir + ".json") // only hashes matter
	}
	name := p.clock.Now().UTC().Format("20060102T150405.000Z")
	dst := filepath.Join(rel, name)
	for n := 1; ; n++ {
		if err := os.Mkdir(dst, 0o755); err == nil {
			break
		} else if !errors.Is(err, fs.ErrExist) || n > 100 {
			return err
		}
		dst = filepath.Join(rel, fmt.Sprintf("%s-%d", name, n))
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dst)
			_ = os.Remove(dst + ".json")
		}
	}()
	for _, f := range published(in.Manifest) {
		to := filepath.Join(dst, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return err
		}
		// An unchanged file is a hard link to the previous release: a release costs only what changed.
		if prev != nil && prevDir != "" && prev.Files[f].Hash == in.Manifest.Files[f].Hash {
			if os.Link(filepath.Join(prevDir, filepath.FromSlash(f)), to) == nil {
				continue
			}
		}
		if err := copyFile(filepath.Join(in.Tree, filepath.FromSlash(f)), to); err != nil {
			return err
		}
	}
	if err := in.Manifest.saveFile(dst + ".json"); err != nil { // beside the release: the site itself stays clean
		return err
	}
	tmp := filepath.Join(base, fmt.Sprintf(".%s.%d", currentLink, os.Getpid()))
	_ = os.Remove(tmp)
	if err := os.Symlink(filepath.Join(releasesDir, filepath.Base(dst)), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, cur); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	ok = true
	return p.prune(rel, filepath.Base(dst))
}

// prune keeps the newest releases (names sort by time) and the one current points to.
func (p *dirPublisher) prune(rel, current string) error {
	ents, err := os.ReadDir(rel)
	if err != nil {
		return err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)
	for len(names) > p.cfg.Keep {
		if names[0] != current {
			if err := os.RemoveAll(filepath.Join(rel, names[0])); err != nil {
				return err
			}
			_ = os.Remove(filepath.Join(rel, names[0]+".json"))
		}
		names = names[1:]
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// ---- command

type commandPublisher struct {
	cfg CommandConfig
	log *slog.Logger
}

func (p *commandPublisher) Publish(ctx context.Context, in publishInput) error {
	argv := make([]string, len(p.cfg.Argv))
	subst := false
	for i, a := range p.cfg.Argv {
		argv[i] = strings.ReplaceAll(a, "{dir}", in.Tree)
		subst = subst || argv[i] != a
	}
	if !subst {
		argv = append(argv, in.Tree)
	}
	return runArgv(ctx, p.log, argv, p.cfg.Timeout, "HYDROGEN_EXPORT_DIR="+in.Tree)
}

// runArgv runs a program without a shell and fails with the tail of its output.
func runArgv(ctx context.Context, log *slog.Logger, argv []string, timeout time.Duration, env ...string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if len(out) > 2000 {
		out = "…" + out[len(out)-2000:]
	}
	if err != nil {
		return fmt.Errorf("%s: %w: %s", argv[0], err, out)
	}
	if out != "" && log != nil {
		log.Info("site-export: "+argv[0], "output", out)
	}
	return nil
}
