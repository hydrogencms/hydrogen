package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/store"
)

type chanMod struct {
	name   string
	opts   []channel.Option
	checks []channel.Check
	db     bool
}

func (m chanMod) Name() string { return m.name }

func (m chanMod) Register(r *Registry) error {
	for _, o := range m.opts {
		r.ChannelOption(o)
	}
	for _, c := range m.checks {
		r.ChannelCheck(c)
	}
	if m.db {
		return r.Database("sqlite", func(context.Context, string, store.OpenOptions) (store.DB, error) { return nil, errors.New("no") })
	}
	return nil
}

func project(t *testing.T, channels string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	write("schema/s.yaml", "types:\n  article:\n    fields:\n      title: { type: string }\n")
	write("app.yaml", "content: { schema_dir: "+filepath.Join(dir, "schema")+" }\n")
	if channels != "" {
		write("channels.yaml", channels)
	}
	t.Chdir(dir)
	cfg, err := config.Load(config.Options{Dir: ".", LookupEnv: func(string) (string, bool) { return "", false }})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func siteOpt() channel.Option {
	return channel.Option{Name: "site", Decode: func(n *config.Node) (any, error) { return n.String() }}
}

func TestChannelsCompiledInPrepare(t *testing.T) {
	cfg := project(t, "channels:\n  web: { languages: [en], collections: [article], site: main }\n")
	p, err := prepare(cfg, []Module{chanMod{name: "pages", opts: []channel.Option{siteOpt()}, db: true}}, true)
	if err != nil {
		t.Fatal(err)
	}
	web, ok := p.chans.Get("web")
	if !ok || web.Options["site"] != "main" || !web.AllowsCollection("article") {
		t.Fatalf("%+v", web)
	}
}

func TestChannelsImplicitDefault(t *testing.T) {
	p, err := prepare(project(t, ""), []Module{chanMod{name: "m", db: true}}, true)
	if err != nil {
		t.Fatal(err)
	}
	all := p.chans.All()
	if len(all) != 1 || all[0].Name != "default" || all[0].Languages[0] != "en" {
		t.Fatalf("%+v", all)
	}
}

func TestChannelsNotCompiledForCodegen(t *testing.T) {
	cfg := project(t, "channels:\n  web: { colour: red }\n")
	if _, err := CompileSchema(cfg); err != nil {
		t.Fatalf("code generation must not need valid channels: %v", err)
	}
}

func TestCheckReportsChannelErrors(t *testing.T) {
	cfg := project(t, "channels:\n  web:\n    languages: [en]\n    collections: [nope]\n    site: x\n")
	err := Check(cfg, chanMod{name: "pages", opts: []channel.Option{siteOpt()}, db: true})
	if err == nil || !strings.Contains(err.Error(), "channels.yaml:4: unknown collection") {
		t.Fatalf("got %v", err)
	}
}

func TestChannelCheckErrorsNameModule(t *testing.T) {
	chk := func(s *channel.Set) error {
		return errors.Join(errors.New("web and web2 both match /"), errors.New("second"))
	}
	cfg := project(t, "channels:\n  web: { languages: [en] }\n")
	err := Check(cfg, chanMod{name: "pages", checks: []channel.Check{chk}, db: true})
	if err == nil || !strings.Contains(err.Error(), "module pages: web and web2 both match /") || !strings.Contains(err.Error(), "module pages: second") {
		t.Fatalf("got %v", err)
	}
}

func TestChannelOptionDuplicateNamesModules(t *testing.T) {
	cfg := project(t, "channels:\n  web: { languages: [en] }\n")
	err := Check(cfg, chanMod{name: "pages", opts: []channel.Option{siteOpt()}, db: true}, chanMod{name: "other", opts: []channel.Option{siteOpt()}})
	if err == nil || !strings.Contains(err.Error(), `channel option "site" registered by both pages and other`) {
		t.Fatalf("got %v", err)
	}
}
