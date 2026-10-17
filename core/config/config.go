// Package config loads a project's YAML configuration.
//
// Files: <dir>/app.yaml, <dir>/channels.yaml (nested under the key "channels"), then <dir>/env/<env>.yaml merged
// over them. Scalars may contain ${env:NAME} or
// ${env:NAME:-default}. HYDROGEN__A__B=v overrides key a.b. Decoding is strict: unknown keys fail with
// file:line, and fields tagged `config:"secret"` must come from the environment (defaults only in dev).
// An override replaces a single scalar; list-valued keys (e.g. channels' languages) can't be set that way.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

const DevEnv = "dev"

type Options struct {
	Dir       string // config directory, default "config"
	Env       string // environment name, default "dev"
	LookupEnv func(string) (string, bool)
	Environ   []string // for HYDROGEN__ overrides
}

type Config struct {
	env      string
	root     *yaml.Node
	file     map[*yaml.Node]string
	refs     map[*yaml.Node][]envRef
	override map[*yaml.Node]bool
	used     map[*yaml.Node]bool
	raw      map[*yaml.Node]string // original text of scalars that had ${env:…}, for template re-expansion
	untyped  map[*yaml.Node]bool   // unquoted scalars whose type comes from the value after env expansion
	lookup   func(string) (string, bool)
}

type envRef struct {
	name       string
	set        bool
	hasDefault bool
}

var (
	envPattern  = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)
	envNameOK   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	errNotFound = errors.New("not found")
)

// OSOptions reads APP_ENV, CONFIG_DIR and the process environment.
func OSOptions() Options {
	return Options{
		Dir:       os.Getenv("CONFIG_DIR"),
		Env:       os.Getenv("APP_ENV"),
		LookupEnv: os.LookupEnv,
		Environ:   os.Environ(),
	}
}

func Load(o Options) (*Config, error) {
	if o.Dir == "" {
		o.Dir = "config"
	}
	if o.Env == "" {
		o.Env = DevEnv
	}
	if !envNameOK.MatchString(o.Env) {
		return nil, fmt.Errorf("config: invalid environment name %q", o.Env)
	}
	if o.LookupEnv == nil {
		o.LookupEnv = func(string) (string, bool) { return "", false }
	}
	c := &Config{
		env:      o.Env,
		root:     &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"},
		file:     map[*yaml.Node]string{},
		refs:     map[*yaml.Node][]envRef{},
		override: map[*yaml.Node]bool{},
		used:     map[*yaml.Node]bool{},
		raw:      map[*yaml.Node]string{},
		untyped:  map[*yaml.Node]bool{},
		lookup:   o.LookupEnv,
	}
	files := []struct{ name, under string }{
		{"app.yaml", ""}, {"channels.yaml", "channels"}, {filepath.Join("env", o.Env+".yaml"), ""},
	}
	for _, f := range files {
		path := filepath.Join(o.Dir, f.name)
		n, err := c.parseFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if n != nil {
			merge(c.root, c.nest(n, path, f.under))
		}
	}
	c.substitute(c.root)
	if err := c.applyOverrides(o.Environ); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) Env() string { return c.env }

func (c *Config) parseFile(path string) (*yaml.Node, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 || len(doc.Content) == 0 {
		return nil, nil
	}
	n := doc.Content[0]
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s:%d: top level must be a mapping", path, n.Line)
	}
	walk(n, func(x *yaml.Node) { c.file[x] = path })
	return n, nil
}

// nest wraps a file's top-level mapping under a key (channels.yaml → channels); "" keeps it as is.
func (c *Config) nest(n *yaml.Node, path, key string) *yaml.Node {
	if key == "" {
		return n
	}
	k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key, Line: n.Line}
	wrap := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: n.Line, Content: []*yaml.Node{k, n}}
	c.file[k], c.file[wrap] = path, path
	return wrap
}

func walk(n *yaml.Node, f func(*yaml.Node)) {
	f(n)
	for _, ch := range n.Content {
		walk(ch, f)
	}
}

// merge copies src into dst: mappings merge key by key, everything else is replaced.
func merge(dst, src *yaml.Node) {
	for i := 0; i+1 < len(src.Content); i += 2 {
		k, v := src.Content[i], src.Content[i+1]
		j := keyIndex(dst, k.Value)
		if j < 0 {
			dst.Content = append(dst.Content, k, v)
			continue
		}
		if dst.Content[j+1].Kind == yaml.MappingNode && v.Kind == yaml.MappingNode {
			merge(dst.Content[j+1], v)
			continue
		}
		dst.Content[j], dst.Content[j+1] = k, v
	}
}

func keyIndex(m *yaml.Node, key string) int {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func (c *Config) substitute(n *yaml.Node) {
	walk(n, func(x *yaml.Node) {
		if x.Kind != yaml.ScalarNode || !strings.Contains(x.Value, "${env:") {
			return
		}
		c.raw[x] = x.Value
		quoted := x.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0
		c.expandEnv(x, x.Value, !quoted)
	})
}

// expandEnv sets x.Value to text with every ${env:…} expanded and records the references for checkEnv.
func (c *Config) expandEnv(x *yaml.Node, text string, typed bool) {
	var refs []envRef
	x.Value = envPattern.ReplaceAllStringFunc(text, func(m string) string {
		sub := envPattern.FindStringSubmatch(m)
		v, ok := c.lookup(sub[1])
		def := strings.Contains(m, ":-")
		refs = append(refs, envRef{name: sub[1], set: ok, hasDefault: def})
		if ok {
			return v
		}
		return sub[2]
	})
	if typed {
		c.untyped[x] = true
	}
	x.Tag = "!!str"
	x.Style = 0
	c.refs[x] = refs
}

func (c *Config) applyOverrides(environ []string) error {
	const prefix = "HYDROGEN__"
	slices.Sort(environ)
	for _, kv := range environ {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || !strings.HasPrefix(name, prefix) {
			continue
		}
		parts := strings.Split(strings.ToLower(strings.TrimPrefix(name, prefix)), "__")
		if slices.Contains(parts, "") {
			return fmt.Errorf("config: malformed override %s", name)
		}
		m := c.root
		for i, p := range parts {
			j := keyIndex(m, p)
			last := i == len(parts)-1
			if last {
				v := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
				c.file[v] = "env " + name
				c.override[v] = true
				if j < 0 {
					k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p}
					c.file[k] = "env " + name
					m.Content = append(m.Content, k, v)
				} else {
					m.Content[j+1] = v
				}
				break
			}
			if j < 0 {
				k := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: p}
				child := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				c.file[k], c.file[child] = "env "+name, "env "+name
				m.Content = append(m.Content, k, child)
				m = child
				continue
			}
			if m.Content[j+1].Kind != yaml.MappingNode {
				return fmt.Errorf("config: %s overrides %s, which is not a mapping", name, strings.Join(parts[:i+1], "."))
			}
			m = m.Content[j+1]
		}
	}
	return nil
}

// Decode decodes the value at a dotted path (e.g. "http", "modules.pages") into target, which must be a
// pointer. A missing path leaves target untouched, so callers set defaults first.
func (c *Config) Decode(path string, target any) error {
	n, err := c.find(path)
	if errors.Is(err, errNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.decode(n, target, path)
}

// Keys lists the keys of the mapping at path, in file order.
func (c *Config) Keys(path string) []string {
	n, err := c.find(path)
	if err != nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var keys []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

func (c *Config) find(path string) (*yaml.Node, error) {
	n := c.root
	for _, p := range strings.Split(path, ".") {
		if n.Kind != yaml.MappingNode {
			return nil, c.errorf(n, "%s: expected a mapping", path)
		}
		j := keyIndex(n, p)
		if j < 0 {
			return nil, errNotFound
		}
		c.used[n.Content[j]] = true
		n = n.Content[j+1]
	}
	return n, nil
}

// Unused reports top-level keys and module sections that nothing decoded, e.g. a typo in a section name or
// configuration for a module the build doesn't include.
func (c *Config) Unused() error {
	var errs []error
	for i := 0; i+1 < len(c.root.Content); i += 2 {
		k, v := c.root.Content[i], c.root.Content[i+1]
		if !c.used[k] {
			errs = append(errs, c.errorf(k, "unknown key %q", k.Value))
			continue
		}
		if k.Value == "modules" && v.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(v.Content); j += 2 {
				if mk := v.Content[j]; !c.used[mk] {
					errs = append(errs, c.errorf(mk, "unknown module %q (not in this build)", mk.Value))
				}
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Config) errorf(n *yaml.Node, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	file := c.file[n]
	switch {
	case file == "":
		return fmt.Errorf("config: %s", msg)
	case strings.HasPrefix(file, "env "):
		return fmt.Errorf("%s: %s", file, msg)
	default:
		return fmt.Errorf("%s:%d: %s", file, n.Line, msg)
	}
}
