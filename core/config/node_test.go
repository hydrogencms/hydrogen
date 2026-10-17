package config

import (
	"reflect"
	"strings"
	"testing"
)

func loadNode(t *testing.T, channels string, vars map[string]string, envName, envYAML string) *Config {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, "channels.yaml", channels)
	if envYAML != "" {
		write(t, dir, "env/"+envName+".yaml", envYAML)
	}
	c, err := Load(Options{Dir: dir, Env: envName, LookupEnv: env(vars)})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestChannelsFileNestedAndOverlay(t *testing.T) {
	c := loadNode(t, "a:\n  b: 1\n  c: x\n", nil, "prod", "channels:\n  a:\n    c: y\n")
	n, err := c.Node("channels.a")
	if err != nil || n == nil {
		t.Fatal(n, err)
	}
	if got := n.Get("c"); got == nil {
		t.Fatal("missing c")
	} else if s, _ := got.String(); s != "y" {
		t.Fatalf("overlay lost: %q", s)
	}
	if want := []string{"b", "c"}; !reflect.DeepEqual(n.Keys(), want) {
		t.Fatalf("keys %v", n.Keys())
	}
	if p := n.Get("b").Pos(); !strings.HasSuffix(p, "channels.yaml:2") {
		t.Fatalf("pos %q", p)
	}
	if p := n.Get("c").Pos(); !strings.HasSuffix(p, "prod.yaml:4") && !strings.HasSuffix(p, "prod.yaml:3") {
		t.Fatalf("overlay pos %q", p)
	}
	if err := c.Unused(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeAbsentAndUnused(t *testing.T) {
	c := loadNode(t, "a: 1\n", nil, "dev", "")
	if n, err := c.Node("nothing.here"); n != nil || err != nil {
		t.Fatal(n, err)
	}
	if err := c.Unused(); err == nil || !strings.Contains(err.Error(), `unknown key "channels"`) {
		t.Fatalf("channels must be unknown until read: %v", err)
	}
	if _, err := c.Node("channels"); err != nil {
		t.Fatal(err)
	}
	if err := c.Unused(); err != nil {
		t.Fatal(err)
	}
}

func TestNodeDecodeStrict(t *testing.T) {
	c := loadNode(t, "a:\n  addr: \":1\"\n  oops: 1\n", nil, "dev", "")
	n, _ := c.Node("channels.a")
	var h httpConf
	err := n.Decode(&h)
	if err == nil || !strings.Contains(err.Error(), "channels.yaml:3") || !strings.Contains(err.Error(), `unknown key "oops"`) {
		t.Fatalf("got %v", err)
	}
}

func TestNodeAnyAndItems(t *testing.T) {
	c := loadNode(t, "s:\n  n: 3\n  f: 1.5\n  b: true\n  t: hi\n  e: ${env:V:-d}\n  l: [1, two]\n  z: ~\n", nil, "dev", "")
	n, _ := c.Node("channels.s")
	got, err := n.Any()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"n": int64(3), "f": 1.5, "b": true, "t": "hi", "e": "d", "l": []any{int64(1), "two"}, "z": nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if items := n.Get("l").Items(); len(items) != 2 || items[1].Kind() != "scalar" {
		t.Fatal(items)
	}
	if n.Kind() != "map" || n.Get("l").Kind() != "list" {
		t.Fatal("kinds")
	}
}

func TestNodeStringEnvMissing(t *testing.T) {
	c := loadNode(t, "a: ${env:NOPE}\n", nil, "dev", "")
	n, _ := c.Node("channels.a")
	if _, err := n.String(); err == nil || !strings.Contains(err.Error(), "NOPE") {
		t.Fatalf("got %v", err)
	}
}

func TestNodeClone(t *testing.T) {
	c := loadNode(t, "t:\n  name: \"x_{m}\"\n  \"k_{m}\": [\"{M}\", plain]\n  tok: ${env:T:-s}\n", map[string]string{"T": "v"}, "dev", "")
	n, _ := c.Node("channels.t")
	cl := n.Clone(func(s string) string { return strings.NewReplacer("{m}", "sg", "{M}", "SG").Replace(s) })
	if s, _ := cl.Get("name").String(); s != "x_sg" {
		t.Fatal(s)
	}
	if !reflect.DeepEqual(cl.Keys(), []string{"name", "k_sg", "tok"}) {
		t.Fatal(cl.Keys())
	}
	l := cl.Get("k_sg").Items()
	if s, _ := l[0].String(); s != "SG" {
		t.Fatal(s)
	}
	if !strings.HasSuffix(l[0].Pos(), "channels.yaml:3") {
		t.Fatal(l[0].Pos())
	}
	if s, _ := n.Get("name").String(); s != "x_{m}" {
		t.Fatalf("original changed: %s", s)
	}
	if err := cl.Get("tok").Decode(new(string)); err != nil {
		t.Fatal(err)
	}
}

type secretConf struct {
	Tokens map[string]string `yaml:"tokens" config:"secret"`
	List   []string          `yaml:"list" config:"secret"`
}

func TestSecretElementWise(t *testing.T) {
	c := loadNode(t, "ok:\n  tokens: { a: \"${env:A}\" }\n  list: [\"${env:A}\", \"${env:B:-d}\"]\n"+
		"plainmap:\n  tokens: { a: literal }\n"+
		"plainlist:\n  list: [\"${env:A}\", literal]\n"+
		"scalar:\n  tokens: literal\n", map[string]string{"A": "1"}, "dev", "")
	dec := func(name string) error {
		n, _ := c.Node("channels." + name)
		return n.Decode(&secretConf{})
	}
	var s secretConf
	n, _ := c.Node("channels.ok")
	if err := n.Decode(&s); err != nil || s.Tokens["a"] != "1" || s.List[1] != "d" {
		t.Fatalf("%v %+v", err, s)
	}
	for _, name := range []string{"plainmap", "plainlist", "scalar"} {
		if err := dec(name); err == nil || !strings.Contains(err.Error(), "secret") {
			t.Fatalf("%s: got %v", name, err)
		}
	}
}

func TestSecretElementDefaultOutsideDev(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "channels.yaml", "x:\n  tokens: { a: \"${env:A:-d}\" }\n")
	c, err := Load(Options{Dir: dir, Env: "prod", LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := c.Node("channels.x")
	if err := n.Decode(&secretConf{}); err == nil || !strings.Contains(err.Error(), "must be set outside dev") {
		t.Fatalf("got %v", err)
	}
}

func TestDecodeSecret(t *testing.T) {
	c := loadNode(t, "ok:\n  a: \"${env:A}\"\nbad:\n  a: literal\nlist: [\"${env:A}\"]\n", map[string]string{"A": "1"}, "dev", "")
	n, _ := c.Node("channels.ok")
	var m map[string]string
	if err := n.DecodeSecret(&m); err != nil || m["a"] != "1" {
		t.Fatal(err, m)
	}
	n, _ = c.Node("channels.bad")
	if err := n.DecodeSecret(&m); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
	n, _ = c.Node("channels.list")
	var l []string
	if err := n.DecodeSecret(&l); err != nil || l[0] != "1" {
		t.Fatal(err, l)
	}
}

func TestNodeAnyTypesEnvScalars(t *testing.T) {
	c := loadNode(t, "s:\n  i: ${env:I}\n  f: ${env:F}\n  b: ${env:B}\n  n: ${env:N:-}\n  t: ${env:T}\n  q: \"${env:I}\"\n  m: x${env:I}\n", map[string]string{"I": "60", "F": "1.5", "B": "true", "T": "text"}, "dev", "")
	n, _ := c.Node("channels.s")
	got, err := n.Any()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"i": int64(60), "f": 1.5, "b": true, "n": nil, "t": "text", "q": "60", "m": "x60"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
}

func TestNodeAnyTypesOverrides(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "channels.yaml", "s:\n  ttl: 1\n  name: a\n")
	c, err := Load(Options{Dir: dir, Environ: []string{"HYDROGEN__CHANNELS__S__TTL=60", "HYDROGEN__CHANNELS__S__NAME=true"}})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := c.Node("channels.s")
	got, err := n.Any()
	if err != nil || !reflect.DeepEqual(got, map[string]any{"ttl": int64(60), "name": true}) {
		t.Fatalf("%#v %v", got, err)
	}
}

func TestNodeCloneExpandsEnvAfterSubstitution(t *testing.T) {
	c := loadNode(t, "t:\n  tok: ${env:TOKEN_{M}}\n  v: ${env:V}\n  d: ${env:D_{M}:-dflt}\n", map[string]string{"TOKEN_SG": "sg-secret", "V": "{m}!"}, "dev", "")
	n, _ := c.Node("channels.t")
	cl := n.Clone(func(s string) string { return strings.NewReplacer("{M}", "SG", "{m}", "sg").Replace(s) })
	for k, want := range map[string]string{"tok": "sg-secret", "v": "{m}!", "d": "dflt"} {
		if s, err := cl.Get(k).String(); err != nil || s != want {
			t.Errorf("%s: %q %v", k, s, err)
		}
	}
	cl2 := n.Clone(func(s string) string { return strings.NewReplacer("{M}", "NZ").Replace(s) })
	if _, err := cl2.Get("tok").String(); err == nil || !strings.Contains(err.Error(), "TOKEN_NZ") {
		t.Fatalf("got %v", err)
	}
	var secret string
	if err := cl.Get("tok").DecodeSecret(&secret); err != nil || secret != "sg-secret" {
		t.Fatal(secret, err)
	}
}
