package graphql

import (
	"flag"
	"os"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	p := "testdata/" + name
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the schema (go test -update rewrites it):\n%s", p, got)
	}
}

func TestSchemaGolden(t *testing.T) {
	e := newEnv(t, envOpts{})
	golden(t, "schema.graphql", e.mod.sdlOf(t, "web"))
	// served as is
	r := e.do("GET", base+"/schema.graphql?channel=web", nil, "")
	if r.Code != 200 || r.Body.String() != e.mod.sdlOf(t, "web") {
		t.Fatalf("sdl endpoint: %d", r.Code)
	}
	if ct := r.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content type %q", ct)
	}
	if r.Header().Get("ETag") == "" || !strings.Contains(r.Header().Get("Cache-Control"), "s-maxage") {
		t.Errorf("sdl headers: %v", r.Header())
	}
	// the cached copy keeps its content type
	again := e.do("GET", base+"/schema.graphql?channel=web", nil, "")
	if ct := again.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") || again.Body.String() != r.Body.String() {
		t.Errorf("cached sdl: %q", ct)
	}
	// only the channel's collections
	lim := e.do("GET", base+"/schema.graphql?channel=lim", nil, "").Body.String()
	for _, want := range []string{"type Article ", "type Person ", "type Company "} {
		if !strings.Contains(lim, want) {
			t.Errorf("lim lacks %s", want)
		}
	}
	for _, not := range []string{"type Settings", "type Image ", "settings(", "imageList"} {
		if strings.Contains(lim, not) {
			t.Errorf("lim has %s", not)
		}
	}
}

func (m *module) sdlOf(t *testing.T, channel string) string {
	t.Helper()
	ch, ok := m.svc.Channels.Get(channel)
	if !ok {
		t.Fatalf("no channel %s", channel)
	}
	cs, err := m.schemaFor(ch)
	if err != nil {
		t.Fatal(err)
	}
	return cs.sdl
}
