// Package scaffold writes a new Hydrogen project (hydrogen new).
package scaffold

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

//go:embed all:template
var files embed.FS

type Options struct {
	Dir     string // target directory; must not exist or be empty
	Module  string // Go module path; default: the directory name
	Core    string // local checkout of hydrogen to use via replace directives (until releases exist)
	Version string // required version of the hydrogen modules, default v0.0.0
}

type data struct {
	Name, Module, Core, Version string
}

var modulePath = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~/-]*$`)

func Write(o Options) error {
	if o.Dir == "" {
		return errors.New("scaffold: target directory required")
	}
	abs, err := filepath.Abs(o.Dir)
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return fmt.Errorf("scaffold: %s is not empty", o.Dir)
	}
	d := data{Name: filepath.Base(abs), Module: o.Module, Version: o.Version}
	if d.Module == "" {
		d.Module = d.Name
	}
	if !modulePath.MatchString(d.Module) {
		return fmt.Errorf("scaffold: invalid module path %q", d.Module)
	}
	if d.Version == "" {
		d.Version = "v0.0.0"
	}
	if o.Core != "" {
		core, err := filepath.Abs(o.Core)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(core, "hydrogen.go")); err != nil {
			return fmt.Errorf("scaffold: %s is not a hydrogen checkout", o.Core)
		}
		if rel, err := filepath.Rel(abs, core); err == nil {
			core = rel
		}
		d.Core = filepath.ToSlash(core)
	}

	return fs.WalkDir(files, "template", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, "template/")
		out := filepath.Join(abs, filepath.FromSlash(rel))
		b, err := files.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".tmpl") {
			out = strings.TrimSuffix(out, ".tmpl")
			if path.Base(rel) == "gitignore.tmpl" {
				out = filepath.Join(filepath.Dir(out), ".gitignore")
			}
			t, err := template.New(rel).Option("missingkey=error").Parse(string(b))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := t.Execute(&buf, d); err != nil {
				return err
			}
			b = buf.Bytes()
		}
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}
