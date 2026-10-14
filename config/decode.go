package config

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var (
	durationType        = reflect.TypeFor[time.Duration]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

func (c *Config) decode(n *yaml.Node, target any, path string) error {
	v := reflect.ValueOf(target)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("config: Decode(%s) needs a non-nil pointer", path)
	}
	return c.value(n, v.Elem(), path, false)
}

func (c *Config) value(n *yaml.Node, v reflect.Value, path string, secret bool) error {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	if isNull(n) && c.refs[n] == nil {
		if secret {
			return c.errorf(n, "%s is a secret and must come from ${env:NAME}", path)
		}
		v.SetZero()
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		return c.value(n, v.Elem(), path, secret)
	}
	if n.Kind == yaml.ScalarNode {
		if err := c.checkEnv(n, path, secret); err != nil {
			return err
		}
	} else if secret {
		return c.errorf(n, "%s is a secret and must be a single value from ${env:NAME}", path)
	}

	if v.CanAddr() && v.Addr().Type().Implements(textUnmarshalerType) && v.Type() != durationType {
		if n.Kind != yaml.ScalarNode {
			return c.errorf(n, "%s: expected a single value", path)
		}
		if err := v.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(n.Value)); err != nil {
			return c.errorf(n, "%s: %v", path, err)
		}
		return nil
	}

	switch v.Kind() {
	case reflect.Struct:
		return c.structValue(n, v, path)
	case reflect.Map:
		return c.mapValue(n, v, path)
	case reflect.Slice:
		if n.Kind != yaml.SequenceNode {
			return c.errorf(n, "%s: expected a list", path)
		}
		s := reflect.MakeSlice(v.Type(), len(n.Content), len(n.Content))
		for i, item := range n.Content {
			if err := c.value(item, s.Index(i), fmt.Sprintf("%s[%d]", path, i), false); err != nil {
				return err
			}
		}
		v.Set(s)
		return nil
	}

	if n.Kind != yaml.ScalarNode {
		return c.errorf(n, "%s: expected a single value", path)
	}
	return c.scalar(n, v, path)
}

func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

func (c *Config) checkEnv(n *yaml.Node, path string, secret bool) error {
	refs := c.refs[n]
	if secret && len(refs) == 0 && !c.override[n] {
		return c.errorf(n, "%s is a secret and must come from ${env:NAME}", path)
	}
	for _, r := range refs {
		switch {
		case r.set:
		case !r.hasDefault:
			return c.errorf(n, "%s: environment variable %s is not set", path, r.name)
		case secret && c.env != DevEnv:
			return c.errorf(n, "%s: environment variable %s must be set outside %s (defaults for secrets only apply in %s)",
				path, r.name, DevEnv, DevEnv)
		}
	}
	return nil
}

func (c *Config) structValue(n *yaml.Node, v reflect.Value, path string) error {
	if n.Kind != yaml.MappingNode {
		return c.errorf(n, "%s: expected a mapping", path)
	}
	type field struct {
		index  int
		secret bool
	}
	t := v.Type()
	fields := map[string]field{}
	var names []string
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		fields[name] = field{index: i, secret: f.Tag.Get("config") == "secret"}
		names = append(names, name)
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, val := n.Content[i], n.Content[i+1]
		f, ok := fields[k.Value]
		if !ok {
			return c.errorf(k, "unknown key %q in %s (known: %s)", k.Value, path, strings.Join(names, ", "))
		}
		c.used[k] = true
		if err := c.value(val, v.Field(f.index), path+"."+k.Value, f.secret); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) mapValue(n *yaml.Node, v reflect.Value, path string) error {
	if n.Kind != yaml.MappingNode {
		return c.errorf(n, "%s: expected a mapping", path)
	}
	if v.Type().Key().Kind() != reflect.String {
		return fmt.Errorf("config: %s: only string-keyed maps are supported", path)
	}
	if v.IsNil() {
		v.Set(reflect.MakeMap(v.Type()))
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, val := n.Content[i], n.Content[i+1]
		c.used[k] = true
		elem := reflect.New(v.Type().Elem()).Elem()
		if err := c.value(val, elem, path+"."+k.Value, false); err != nil {
			return err
		}
		v.SetMapIndex(reflect.ValueOf(k.Value).Convert(v.Type().Key()), elem)
	}
	return nil
}

func (c *Config) scalar(n *yaml.Node, v reflect.Value, path string) error {
	s := n.Value
	bad := func(what string) error { return c.errorf(n, "%s: %q is not %s", path, s, what) }
	switch {
	case v.Type() == durationType:
		d, err := time.ParseDuration(s)
		if err != nil {
			return bad("a duration (e.g. 30s, 5m)")
		}
		v.SetInt(int64(d))
	case v.Kind() == reflect.String:
		v.SetString(s)
	case v.Kind() == reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return bad("true or false")
		}
		v.SetBool(b)
	case v.CanInt():
		i, err := strconv.ParseInt(s, 10, v.Type().Bits())
		if err != nil {
			return bad("an integer")
		}
		v.SetInt(i)
	case v.CanUint():
		u, err := strconv.ParseUint(s, 10, v.Type().Bits())
		if err != nil {
			return bad("a non-negative integer")
		}
		v.SetUint(u)
	case v.CanFloat():
		f, err := strconv.ParseFloat(s, v.Type().Bits())
		if err != nil {
			return bad("a number")
		}
		v.SetFloat(f)
	default:
		return fmt.Errorf("config: %s: unsupported field type %s", path, v.Type())
	}
	return nil
}
