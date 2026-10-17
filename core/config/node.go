package config

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// Node is a position-aware view of a YAML node from a loaded Config, for configuration that modules interpret
// themselves (channels, module options). Contract: planning/design/channels-h3.md.
type Node struct {
	c    *Config
	n    *yaml.Node
	path string
}

// Node returns the node at a dotted path, or nil when absent; the path counts as used.
func (c *Config) Node(path string) (*Node, error) {
	n, err := c.find(path)
	if err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return c.node(n, path), nil
}

func (c *Config) node(n *yaml.Node, path string) *Node {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return &Node{c: c, n: n, path: path}
}

// Kind is "map", "list" or "scalar".
func (n *Node) Kind() string {
	switch n.n.Kind {
	case yaml.MappingNode:
		return "map"
	case yaml.SequenceNode:
		return "list"
	}
	return "scalar"
}

// Keys lists a map's keys in file order.
func (n *Node) Keys() []string {
	if n.n.Kind != yaml.MappingNode {
		return nil
	}
	keys := make([]string, 0, len(n.n.Content)/2)
	for i := 0; i+1 < len(n.n.Content); i += 2 {
		keys = append(keys, n.n.Content[i].Value)
	}
	return keys
}

// Get returns a map's value for key, or nil. The key counts as used.
func (n *Node) Get(key string) *Node {
	if n.n.Kind != yaml.MappingNode {
		return nil
	}
	j := keyIndex(n.n, key)
	if j < 0 {
		return nil
	}
	n.c.used[n.n.Content[j]] = true
	return n.c.node(n.n.Content[j+1], n.path+"."+key)
}

// Items returns a list's elements.
func (n *Node) Items() []*Node {
	if n.n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*Node, len(n.n.Content))
	for i, ch := range n.n.Content {
		out[i] = n.c.node(ch, fmt.Sprintf("%s[%d]", n.path, i))
	}
	return out
}

// Decode decodes the node into target with the same strict rules and secret handling as Config.Decode.
func (n *Node) Decode(target any) error {
	return n.c.decode(n.n, target, n.path)
}

// DecodeSecret is Decode for a node that holds secrets: as with a `config:"secret"` field, every scalar (every
// map value and list element) must come from ${env:NAME}.
func (n *Node) DecodeSecret(target any) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("config: DecodeSecret(%s) needs a non-nil pointer", n.path)
	}
	return n.c.value(n.n, v.Elem(), n.path, true)
}

// String returns a scalar's value (an unset ${env:NAME} without default is an error).
func (n *Node) String() (string, error) {
	if n.n.Kind != yaml.ScalarNode {
		return "", n.Errorf("%s: expected a single value", n.path)
	}
	if err := n.c.checkEnv(n.n, n.path, false); err != nil {
		return "", err
	}
	return n.n.Value, nil
}

// Any converts the node to plain Go values: map[string]any, []any, string, bool, int64, float64 or nil.
// Scalars that came from an unquoted ${env:…} or from a HYDROGEN__ override are typed like YAML scalars
// (60 is an int64, true a bool, an empty value or null is nil, anything else a string); quote the reference
// ("${env:X}") to keep a string. An override can only replace a scalar, so list-valued keys can't be set.
func (n *Node) Any() (any, error) {
	return n.c.anyValue(n.n, n.path)
}

func (c *Config) anyValue(x *yaml.Node, path string) (any, error) {
	if x.Kind == yaml.AliasNode {
		x = x.Alias
	}
	switch x.Kind {
	case yaml.MappingNode:
		m := make(map[string]any, len(x.Content)/2)
		for i := 0; i+1 < len(x.Content); i += 2 {
			k := x.Content[i]
			c.used[k] = true
			v, err := c.anyValue(x.Content[i+1], path+"."+k.Value)
			if err != nil {
				return nil, err
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		s := make([]any, len(x.Content))
		for i, ch := range x.Content {
			v, err := c.anyValue(ch, fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			s[i] = v
		}
		return s, nil
	}
	if err := c.checkEnv(x, path, false); err != nil {
		return nil, err
	}
	if c.untyped[x] || c.override[x] {
		return resolveScalar(x.Value), nil
	}
	if c.refs[x] != nil {
		return x.Value, nil
	}
	return scalarAny(x), nil
}

var (
	intRE   = regexp.MustCompile(`^[-+]?[0-9]+$`)
	floatRE = regexp.MustCompile(`^[-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?$`)
)

// resolveScalar types a plain scalar the way YAML would.
func resolveScalar(v string) any {
	switch v {
	case "", "~", "null", "Null", "NULL":
		return nil
	case "true", "True", "TRUE":
		return true
	case "false", "False", "FALSE":
		return false
	}
	if intRE.MatchString(v) {
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}
	if floatRE.MatchString(v) {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return v
}

func scalarAny(x *yaml.Node) any {
	switch x.Tag {
	case "!!null":
		return nil
	case "!!bool":
		if b, err := strconv.ParseBool(x.Value); err == nil {
			return b
		}
	case "!!int":
		var i int64
		if x.Decode(&i) == nil {
			return i
		}
	case "!!float":
		var f float64
		if x.Decode(&f) == nil {
			return f
		}
	}
	return x.Value
}

// Pos is "file:line" (or "env NAME" for overrides).
func (n *Node) Pos() string {
	file := n.c.file[n.n]
	switch {
	case file == "":
		return "config"
	case len(file) > 4 && file[:4] == "env ":
		return file
	}
	return file + ":" + strconv.Itoa(n.n.Line)
}

// Errorf returns an error prefixed with the node's position.
func (n *Node) Errorf(format string, args ...any) error {
	return fmt.Errorf("%s: %s", n.Pos(), fmt.Sprintf(format, args...))
}

// Clone deep-copies the node, passing every scalar and map key through subst; positions are kept. A scalar
// that had ${env:…} is substituted in its original text and then expanded again, so a placeholder can sit
// inside a reference name and environment values are never rewritten. Values that came from an override are
// copied as they are.
func (n *Node) Clone(subst func(string) string) *Node {
	return &Node{c: n.c, n: n.c.cloneYAML(n.n, subst), path: n.path}
}

func (c *Config) cloneYAML(x *yaml.Node, subst func(string) string) *yaml.Node {
	if x.Kind == yaml.AliasNode {
		x = x.Alias
	}
	cp := *x
	cp.Content = nil
	cp.Alias = nil
	if x.Kind == yaml.ScalarNode {
		switch raw, has := c.raw[x]; {
		case has:
			cp.Value = subst(raw)
			c.raw[&cp] = cp.Value
			c.expandEnv(&cp, cp.Value, c.untyped[x])
		case c.override[x]:
		default:
			cp.Value = subst(x.Value)
		}
	}
	for _, ch := range x.Content {
		cp.Content = append(cp.Content, c.cloneYAML(ch, subst))
	}
	c.file[&cp] = c.file[x]
	if r, ok := c.refs[x]; ok && c.refs[&cp] == nil {
		c.refs[&cp] = r
	}
	if c.override[x] {
		c.override[&cp] = true
	}
	if c.used[x] {
		c.used[&cp] = true
	}
	return &cp
}
