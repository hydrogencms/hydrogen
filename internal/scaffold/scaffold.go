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

	"github.com/hydrogencms/hydrogen/internal/projectfile"
)

//go:embed all:template
var files embed.FS

// staticFiles are what `hydrogen new -static` adds to the template, or replaces in it: a site whose content lives in
// git and that Astro builds for GitHub Pages.
//
//go:embed all:static
var staticFiles embed.FS

// staticOmits reports the template's paths a static site does not have.
func staticOmits(rel string) bool {
	return rel == "schema/article.yaml" || strings.HasPrefix(rel, "admin/")
}

// ReleaseVersion is the latest released version of the hydrogen modules: what a new project requires when the
// CLI itself is not a release build.
const ReleaseVersion = "v0.1.0"

// npmRange is the version range of the @hydrogencms npm packages a new project depends on (the release's).
const npmRange = "^0.1.0"

type Options struct {
	Dir     string // target directory; must not exist or be empty
	Module  string // Go module path; default: the directory name
	Core    string // local checkout of hydrogen to use via replace directives instead of published versions
	Version string // required version of the hydrogen modules, default ReleaseVersion
	Static  bool   // a static website with its content in git and an Astro site
}

type data struct {
	Name, Module, Core, Version string
	SDK                         string // version or file: spec of @hydrogencms/admin-sdk
	Static                      bool
	Site                        map[string]string // version or file: spec of the @hydrogencms packages the Astro site uses
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
	d := data{Name: filepath.Base(abs), Module: o.Module, Version: o.Version, SDK: npmRange, Static: o.Static, Site: map[string]string{}}
	for _, n := range []string{"astro", "bridge", "client", "router"} {
		d.Site[n] = npmRange
	}
	if d.Module == "" {
		d.Module = d.Name
	}
	if !modulePath.MatchString(d.Module) {
		return fmt.Errorf("scaffold: invalid module path %q", d.Module)
	}
	if d.Version == "" {
		d.Version = ReleaseVersion
	}
	if o.Core != "" {
		core, err := filepath.Abs(o.Core)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(core, "hydrogen.go")); err != nil {
			return fmt.Errorf("scaffold: %s is not a hydrogen checkout", o.Core)
		}
		d.Core = relTo(abs, core)
		d.SDK = "file:" + relTo(filepath.Join(abs, "admin"), filepath.Join(core, "packages", "admin-sdk"))
		for n := range d.Site {
			d.Site[n] = "file:" + relTo(filepath.Join(abs, "site"), filepath.Join(core, "packages", n))
		}
	}

	modules := defaultModules()
	if o.Static {
		modules = staticModules()
	}
	if err := writeModules(abs, modules); err != nil {
		return err
	}
	overlay := map[string]bool{}
	if o.Static {
		err := fs.WalkDir(staticFiles, "static", func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() {
				overlay[strings.TrimPrefix(p, "static/")] = true
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	skip := func(rel string) bool {
		if overlay[rel] {
			return true
		}
		return staticOmits(rel)
	}
	if err := copyTree(files, "template", abs, d, func(rel string) bool { return o.Static && skip(rel) }); err != nil {
		return err
	}
	if !o.Static {
		return nil
	}
	return copyTree(staticFiles, "static", abs, d, nil)
}

// relTo is target relative to dir when that is possible, else target.
func relTo(dir, target string) string {
	if rel, err := filepath.Rel(dir, target); err == nil {
		target = rel
	}
	return filepath.ToSlash(target)
}

// copyTree writes the files below root of fsys into dir; .tmpl files are executed with d first.
func copyTree(fsys fs.FS, root, dir string, d data, skip func(rel string) bool) error {
	return fs.WalkDir(fsys, root, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		rel := strings.TrimPrefix(p, root+"/")
		if skip != nil && skip(rel) {
			return nil
		}
		out := filepath.Join(dir, filepath.FromSlash(rel))
		b, err := fs.ReadFile(fsys, p)
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

// defaultModules are the modules a new project lists in hydrogen.yaml.
func defaultModules() []string { return []string{"sqlite", "postgres"} }

// staticModules are a static site's: content in git needs SQLite, and the page tree gives the site its URLs.
func staticModules() []string { return []string{"sqlite", "pages"} }

func writeModules(dir string, modules []string) error {
	gen, err := projectfile.Generate(modules)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, projectfile.FileName), projectfile.Marshal(modules), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, projectfile.GenName), gen, 0o644)
}
