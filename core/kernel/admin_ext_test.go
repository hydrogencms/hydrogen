package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/admin"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/schema"
)

func adminFixture(t *testing.T) (dir string, reg *Registry, s *schema.Schema) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.yaml"), []byte("blocks:\n  hero:\n    props:\n      h: { type: string }\ntypes:\n  article:\n    fields:\n      title: { type: string }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"el.js", "logo.svg", "t.css", "s.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg = newRegistry()
	reg.current = "project"
	if err := reg.Policy(access.Policy{Name: "import.run"}); err != nil {
		t.Fatal(err)
	}
	var err error
	if s, err = compileSchema(dir, nil, reg); err != nil {
		t.Fatal(err)
	}
	return dir, reg, s
}

func TestAdminManifestValidation(t *testing.T) {
	dir, reg, s := adminFixture(t)
	f := func(n string) string { return filepath.Join(dir, n) }
	ok := func() AdminConfig {
		return AdminConfig{Enabled: true, Prefix: "/admin", ElementsDevEntry: "/src/elements.ts", Elements: []string{f("el.js")}}
	}
	for _, tc := range []struct {
		name string
		mod  func(c *AdminConfig)
		want string
	}{
		{"valid", func(c *AdminConfig) {
			c.Theme = admin.Theme{Name: "Acme", Logo: f("logo.svg"), Accent: "#0b6", CSS: f("t.css")}
			c.Strings = f("s.json")
			c.Widgets = map[string]string{"brand-color": "x-brand-color"}
			c.Cells = map[string]string{"swatch": "x-swatch"}
			c.Panels = []admin.Panel{{Tag: "x-seo", Title: "SEO", Types: []string{"article"}}}
			c.Actions = []admin.Panel{{Tag: "x-act", Title: "Go", Policy: "content.edit"}}
			c.Dashboard = []admin.Panel{{Tag: "x-dash", Title: "Recent"}}
			c.Views = []admin.View{{Path: "/tools/import", Tag: "x-imp", Menu: "Tools", Title: "Import", Policy: "import.run"}}
			c.Slots = []admin.Slot{{Tag: "x-banner", At: "editor.before"}, {Tag: "x-n", At: "settings.after"}, {Tag: "x-o", At: "trash.before"}}
			c.Previews = map[string]string{"hero": "x-hero-preview"}
			c.ListActions = []admin.Panel{{Tag: "x-export", Title: "Export", Types: []string{"article"}, Policy: "content.edit"}}
		}, ""},
		{"preview unknown block", func(c *AdminConfig) { c.Previews = map[string]string{"nope": "x-a"} }, `unknown block type "nope"`},
		{"preview bad tag", func(c *AdminConfig) { c.Previews = map[string]string{"hero": "hero"} }, "previews.hero"},
		{"list action tag", func(c *AdminConfig) { c.ListActions = []admin.Panel{{Tag: "bad", Title: "t"}} }, "list_actions[0].tag"},
		{"list action title", func(c *AdminConfig) { c.ListActions = []admin.Panel{{Tag: "x-a"}} }, "list_actions[0]: title is required"},
		{"list action type", func(c *AdminConfig) { c.ListActions = []admin.Panel{{Tag: "x-a", Title: "t", Types: []string{"nope"}}} }, `list_actions[0].types: unknown content type "nope"`},
		{"list action policy", func(c *AdminConfig) { c.ListActions = []admin.Panel{{Tag: "x-a", Title: "t", Policy: "nope.run"}} }, "list_actions[0].policy"},
		{"new slot position", func(c *AdminConfig) { c.Slots = []admin.Slot{{Tag: "x-a", At: "pages.sideways"}} }, "slots[0].at"},
		{"missing element", func(c *AdminConfig) { c.Elements = []string{f("nope.js")} }, "admin.elements[0]"},
		{"element is a directory", func(c *AdminConfig) { c.Elements = []string{dir} }, "not a regular file"},
		{"missing strings", func(c *AdminConfig) { c.Strings = f("nope.json") }, "admin.strings"},
		{"missing logo", func(c *AdminConfig) { c.Theme.Logo = f("nope.svg") }, "admin.theme.logo"},
		{"logo not an image", func(c *AdminConfig) { c.Theme.Logo = f("el.js") }, "theme.logo: want one of"},
		{"missing css", func(c *AdminConfig) { c.Theme.CSS = f("nope.css") }, "admin.theme.css"},
		{"bad accent", func(c *AdminConfig) { c.Theme.Accent = "red; x" }, "theme.accent"},
		{"accent url", func(c *AdminConfig) { c.Theme.Accent = "url(x)" }, "must not contain"},
		{"name brace", func(c *AdminConfig) { c.Theme.Name = "a}b" }, "theme.name"},
		{"accent rgb", func(c *AdminConfig) { c.Theme.Accent = "rgb(0 128 0 / 50%)" }, ""},
		{"accent named", func(c *AdminConfig) { c.Theme.Accent = "rebeccapurple" }, ""},
		{"bad widget name", func(c *AdminConfig) { c.Widgets = map[string]string{"1x": "x-a"} }, "widgets: name"},
		{"bad widget tag", func(c *AdminConfig) { c.Widgets = map[string]string{"a": "nodash"} }, "widgets.a"},
		{"bad cell tag", func(c *AdminConfig) { c.Cells = map[string]string{"a": "Bad-Tag"} }, "cells.a"},
		{"panel tag", func(c *AdminConfig) { c.Panels = []admin.Panel{{Tag: "seo", Title: "t"}} }, "panels[0].tag"},
		{"panel title", func(c *AdminConfig) { c.Panels = []admin.Panel{{Tag: "x-a"}} }, "panels[0]: title is required"},
		{"panel type", func(c *AdminConfig) { c.Panels = []admin.Panel{{Tag: "x-a", Title: "t", Types: []string{"nope"}}} }, `unknown content type "nope"`},
		{"action policy", func(c *AdminConfig) { c.Actions = []admin.Panel{{Tag: "x-a", Title: "t", Policy: "nope.run"}} }, `policy "nope.run"`},
		{"dashboard policy", func(c *AdminConfig) { c.Dashboard = []admin.Panel{{Tag: "x-a", Title: "t", Policy: "nope.run"}} }, "dashboard[0].policy"},
		{"dashboard types", func(c *AdminConfig) {
			c.Dashboard = []admin.Panel{{Tag: "x-a", Title: "t", Types: []string{"article"}}}
		}, "dashboard[0].types"},
		{"view path", func(c *AdminConfig) { c.Views = []admin.View{{Path: "tools", Tag: "x-a", Title: "t"}} }, "views[0].path"},
		{"view builtin", func(c *AdminConfig) { c.Views = []admin.View{{Path: "/items", Tag: "x-a", Title: "t"}} }, "built-in"},
		{"view under builtin", func(c *AdminConfig) { c.Views = []admin.View{{Path: "/settings/x", Tag: "x-a", Title: "t"}} }, "built-in"},
		{"view root", func(c *AdminConfig) { c.Views = []admin.View{{Path: "/", Tag: "x-a", Title: "t"}} }, "views[0].path"},
		{"view policy", func(c *AdminConfig) { c.Views = []admin.View{{Path: "/a", Tag: "x-a", Title: "t", Policy: "nope.run"}} }, "views[0].policy"},
		{"duplicate view", func(c *AdminConfig) {
			c.Views = []admin.View{{Path: "/a", Tag: "x-a", Title: "t"}, {Path: "/a", Tag: "x-b", Title: "t"}}
		}, "already used"},
		{"slot at", func(c *AdminConfig) { c.Slots = []admin.Slot{{Tag: "x-a", At: "nowhere"}} }, "slots[0].at"},
		{"slot policy", func(c *AdminConfig) { c.Slots = []admin.Slot{{Tag: "x-a", At: "login.after", Policy: "nope.run"}} }, "slots[0].policy"},
		{"dev entry", func(c *AdminConfig) { c.ElementsDevEntry = "src/x.ts" }, "elements_dev_entry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := ok()
			tc.mod(&c)
			err := c.check()
			if err == nil {
				_, err = composeAdmin(&c, reg, s, "prod")
			}
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAdminSchemaReferences(t *testing.T) {
	dir, reg, _ := adminFixture(t)
	yaml := "types:\n  article:\n    admin: { list: [title, \"title:swatch\"] }\n    fields:\n      title: { type: string, admin: { widget: brand } }\n"
	if err := os.WriteFile(filepath.Join(dir, "s.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := compileSchema(dir, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	c := AdminConfig{Enabled: true, Prefix: "/admin", ElementsDevEntry: "/src/elements.ts"}
	_, err = composeAdmin(&c, reg, s, "prod")
	if err == nil || !strings.Contains(err.Error(), `field "title": admin.widget "brand"`) || !strings.Contains(err.Error(), `cell "swatch"`) {
		t.Fatalf("err = %v", err)
	}
	c.Widgets = map[string]string{"brand": "x-brand"}
	reg.current = "seo"
	reg.AdminExtension(AdminExtension{Manifest: admin.Manifest{Cells: map[string]string{"swatch": "x-swatch"}}})
	if _, err := composeAdmin(&c, reg, s, "prod"); err != nil {
		t.Fatal(err)
	}
}

func TestAdminBlockPropWidgets(t *testing.T) {
	dir, reg, _ := adminFixture(t)
	yaml := "blocks:\n  hero:\n    props:\n      color: { type: string, admin: { widget: swatch } }\ntypes:\n  article:\n    fields:\n      title: { type: string }\n"
	if err := os.WriteFile(filepath.Join(dir, "s.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := compileSchema(dir, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	c := AdminConfig{Enabled: true, Prefix: "/admin", ElementsDevEntry: "/src/elements.ts"}
	_, err = composeAdmin(&c, reg, s, "prod")
	if err == nil || !strings.Contains(err.Error(), `block "hero": field "color": admin.widget "swatch"`) {
		t.Fatalf("err = %v", err)
	}
	reg.current = "seo"
	reg.AdminExtension(AdminExtension{Manifest: admin.Manifest{Widgets: map[string]string{"swatch": "x-swatch"}}})
	if _, err := composeAdmin(&c, reg, s, "prod"); err != nil {
		t.Fatal(err)
	}
}

func TestAdminModuleExtensions(t *testing.T) {
	dir, reg, s := adminFixture(t)
	c := AdminConfig{Enabled: true, Prefix: "/admin", ElementsDevEntry: "/src/elements.ts", Elements: []string{filepath.Join(dir, "el.js")},
		Widgets: map[string]string{"a": "x-a"}, ListActions: []admin.Panel{{Tag: "x-pa", Title: "P"}}, Views: []admin.View{{Path: "/p", Tag: "x-p", Title: "P"}}}
	files := fstest.MapFS{"dist/el.js": {Data: []byte("m")}, "s.json": {Data: []byte("{}")}}
	add := func(module string, m admin.Manifest) {
		reg.current = module
		reg.AdminExtension(AdminExtension{Files: files, Manifest: m})
	}
	add("seo", admin.Manifest{Elements: []string{"dist/el.js"}, Strings: "s.json", Widgets: map[string]string{"b": "x-b"},
		Slots:       []admin.Slot{{Tag: "x-s", At: "list.after"}},
		Previews:    map[string]string{"hero": "x-hero"},
		ListActions: []admin.Panel{{Tag: "x-la", Title: "L"}}})
	x, err := composeAdmin(&c, reg, s, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(x.Elements, ","); got != "/admin/x/m/seo/dist/el.js,/admin/x/0.js" {
		t.Errorf("elements: %s", got)
	}
	if got := strings.Join(x.Strings, ","); got != "/admin/x/m/seo/s.json" {
		t.Errorf("strings: %s", got)
	}
	if len(x.Widgets) != 2 || len(x.Slots) != 1 {
		t.Errorf("merge: %+v", x)
	}
	if len(x.Previews) != 1 || x.Previews["hero"] != "x-hero" || len(x.ListActions) != 2 {
		t.Errorf("previews/list actions: %+v %+v", x.Previews, x.ListActions)
	}

	for name, tc := range map[string]struct {
		m    admin.Manifest
		want string
	}{
		"theme":         {admin.Manifest{Theme: admin.Theme{Name: "x"}}, "only the project sets the theme"},
		"missing file":  {admin.Manifest{Elements: []string{"nope.js"}}, "module bad: admin extension.elements[0]"},
		"escaping":      {admin.Manifest{Elements: []string{"../el.js"}}, "not a valid file name"},
		"tag":           {admin.Manifest{Slots: []admin.Slot{{Tag: "x", At: "list.after"}}}, "slots[0].tag"},
		"policy":        {admin.Manifest{Views: []admin.View{{Path: "/q", Tag: "x-q", Title: "q", Policy: "no.pe"}}}, "unknown policy"},
		"type":          {admin.Manifest{Panels: []admin.Panel{{Tag: "x-q", Title: "q", Types: []string{"nope"}}}}, "unknown content type"},
		"widget clash":  {admin.Manifest{Widgets: map[string]string{"a": "x-z"}}, `widgets name "a" is already taken`},
		"preview clash": {admin.Manifest{Previews: map[string]string{"hero": "x-z"}}, `previews name "hero" is already taken`},
		"view clash":    {admin.Manifest{Views: []admin.View{{Path: "/p", Tag: "x-q", Title: "q"}}}, "already used"},
	} {
		reg.adminExts = reg.adminExts[:1]
		add("bad", tc.m)
		if _, err := composeAdmin(&c, reg, s, "prod"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

func TestAdminElementsDevMode(t *testing.T) {
	dir, reg, s := adminFixture(t)
	c := AdminConfig{Enabled: true, Prefix: "/admin", ElementsDevEntry: "/src/elements.ts", Elements: []string{filepath.Join(dir, "unbuilt.js")}}
	if _, err := composeAdmin(&c, reg, s, "dev"); err == nil {
		t.Fatal("unbuilt element accepted without a dev server")
	}
	t.Setenv("ADMIN_ELEMENTS_DEV_URL", "http://localhost:5174")
	x, err := composeAdmin(&c, reg, s, "dev")
	if err != nil || len(x.Elements) != 1 || x.Elements[0] != "/admin/x/dev/src/elements.ts" {
		t.Fatalf("dev: %v %+v", err, x)
	}
	if _, err := composeAdmin(&c, reg, s, "prod"); err == nil {
		t.Fatal("dev server honoured outside dev")
	}
}
