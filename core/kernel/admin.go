package kernel

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/hydrogencms/hydrogen/admin"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// AdminExtension is a module's contribution to the admin: custom elements and what places them. Manifest file
// names (elements, strings) are names inside Files, and are served at <admin.prefix>/x/m/<module>/<name>. Only
// the project sets the theme. Bundle each elements file into one module: nothing else under Files is served.
type AdminExtension struct {
	Files    fs.FS
	Manifest admin.Manifest
}

// AdminExtension registers admin extensions. Module entries follow the project's in every list of config.json.
func (r *Registry) AdminExtension(e AdminExtension) {
	r.adminExts = append(r.adminExts, registered[AdminExtension]{r.current, e})
}

func mergeNames(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range []map[string]string{a, b} {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// checkSchemaRefs reports field widgets and list cells the schema names but no extension declares.
func checkSchemaRefs(s *schema.Schema, widgets, cells map[string]string) error {
	var errs []error
	var walk func(typ string, f *schema.Field)
	walk = func(typ string, f *schema.Field) {
		if w := f.Admin.Widget; w != "" {
			if _, ok := widgets[w]; !ok {
				errs = append(errs, fmt.Errorf("%s: field %q: admin.widget %q is not declared in admin.widgets", typ, f.Name, w))
			}
		}
		if f.Of != nil {
			walk(typ, f.Of)
		}
		for _, m := range f.Fields {
			walk(typ, m)
		}
	}
	for _, b := range s.Blocks() {
		for _, f := range b.Props {
			walk(fmt.Sprintf("block %q", b.Name), f)
		}
	}
	for _, t := range s.Types() {
		for _, f := range t.Fields {
			walk(fmt.Sprintf("type %q", t.Name), f)
		}
		for _, l := range t.Admin.List {
			if field, cell, ok := strings.Cut(l, ":"); ok {
				if _, ok := cells[cell]; !ok {
					errs = append(errs, fmt.Errorf("type %q: admin.list %q: cell %q (field %q) is not declared in admin.cells", t.Name, l, cell, field))
				}
			}
		}
	}
	return errors.Join(errs...)
}

type noFiles struct{}

func (noFiles) Open(string) (fs.File, error) { return nil, fs.ErrNotExist }

// composeAdmin validates the project's and the modules' admin manifests and composes what the admin serves.
func composeAdmin(c *AdminConfig, reg *Registry, s *schema.Schema, env string) (*admin.Extensions, error) {
	devURL := ""
	if env == config.DevEnv {
		devURL = os.Getenv("ADMIN_ELEMENTS_DEV_URL")
	}
	cat := reg.accessCatalog()
	check := func(m *admin.Manifest, o admin.CheckOptions) error {
		o.Policy = func(n string) bool { _, ok := cat.Policy(n); return ok }
		o.Type = func(n string) bool { _, ok := s.Type(n); return ok }
		o.Block = func(n string) bool { _, ok := s.Block(n); return ok }
		return m.Check(o)
	}
	var errs []error
	pm := c.manifest()
	errs = append(errs, check(&pm, admin.CheckOptions{Where: "admin", SkipElements: devURL != "",
		File: func(n string) error { return admin.CheckFile(nil, n) }}))
	srcs := []admin.Source{{Manifest: pm}}
	for _, e := range reg.adminExts {
		files := e.value.Files
		if files == nil {
			files = noFiles{}
		}
		m := e.value.Manifest
		errs = append(errs, check(&m, admin.CheckOptions{Where: fmt.Sprintf("module %s: admin extension", e.module), Module: true,
			File: func(n string) error { return admin.CheckFile(files, n) }}))
		srcs = append(srcs, admin.Source{Module: e.module, FS: files, Manifest: m})
	}
	x := srcs[0].Manifest
	for _, e := range reg.adminExts {
		x.Widgets = mergeNames(x.Widgets, e.value.Manifest.Widgets)
		x.Cells = mergeNames(x.Cells, e.value.Manifest.Cells)
	}
	errs = append(errs, checkSchemaRefs(s, x.Widgets, x.Cells))
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	devEntry := ""
	if devURL != "" {
		devEntry = c.ElementsDevEntry
	}
	return admin.Compose(c.Prefix, srcs, devEntry)
}
