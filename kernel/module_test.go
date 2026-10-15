package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/schema"
)

type mod struct {
	name string
	reqs []string
}

func (m mod) Name() string             { return m.name }
func (m mod) Register(*Registry) error { return nil }
func (m mod) Requires() []string       { return m.reqs }

func names(ms []Module) string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Name())
	}
	return strings.Join(out, ",")
}

func TestOrder(t *testing.T) {
	got, err := order([]Module{mod{"seo", []string{"pages"}}, mod{"sqlite", nil}, mod{"pages", nil}, mod{"site", []string{"pages", "seo"}}})
	if err != nil {
		t.Fatal(err)
	}
	if names(got) != "pages,seo,sqlite,site" {
		t.Fatalf("order: %s", names(got))
	}
}

func TestOrderErrors(t *testing.T) {
	for _, tc := range []struct {
		mods []Module
		want string
	}{
		{[]Module{mod{"a", []string{"b"}}}, `"a" requires "b", which is not in this build`},
		{[]Module{mod{"a", []string{"b"}}, mod{"b", []string{"a"}}}, "cycle"},
		{[]Module{mod{"a", nil}, mod{"a", nil}}, "added twice"},
		{[]Module{mod{"Bad_Name", nil}}, "lowercase"},
	} {
		if _, err := order(tc.mods); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("order(%s): %v, want %q", names(tc.mods), err, tc.want)
		}
	}
}

func TestCompileSchemaTypeOptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.yaml"), []byte("types:\n  home:\n    children: [a]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check := func(*schema.TypeBuilder, any) error { return nil }
	reg := newRegistry()
	reg.current = "pages"
	reg.TypeOption("children", check)
	s, err := compileSchema(dir, nil, reg)
	if err != nil {
		t.Fatal(err)
	}
	if ty, _ := s.Type("home"); ty.Options["children"] == nil {
		t.Error("option not stored")
	}

	reg = newRegistry()
	reg.current = "pages"
	reg.TypeOption("children", check)
	reg.current = "other"
	reg.TypeOption("children", check)
	if _, err := compileSchema(dir, nil, reg); err == nil || !strings.Contains(err.Error(), "module other") {
		t.Errorf("duplicate option: %v", err)
	}

	if _, err := compileSchema(dir, nil, newRegistry()); err == nil || !strings.Contains(err.Error(), `unknown key "children"`) {
		t.Errorf("unregistered option: %v", err)
	}
}

func TestManageRouteRecordsModule(t *testing.T) {
	reg := newRegistry()
	reg.current = "pages"
	reg.ManageRoute(manage.Route{Method: "GET", Path: "/x"})
	if len(reg.manageRts) != 1 || reg.manageRts[0].Module != "pages" {
		t.Errorf("routes = %+v", reg.manageRts)
	}
}
