package kernel

import (
	"strings"
	"testing"
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
