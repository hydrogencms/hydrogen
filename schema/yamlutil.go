package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"

	"go.yaml.in/yaml/v3"
)

type entry struct {
	key  string
	k, v *yaml.Node
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func mappingEntries(n *yaml.Node, fail func(at *yaml.Node, msg string)) ([]entry, bool) {
	n = deref(n)
	if n.Kind != yaml.MappingNode {
		fail(n, "expected a mapping")
		return nil, false
	}
	ents := make([]entry, 0, len(n.Content)/2)
	ok := true
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Kind != yaml.ScalarNode {
			fail(k, "keys must be plain names")
			ok = false
			continue
		}
		if slices.ContainsFunc(ents, func(e entry) bool { return e.key == k.Value }) {
			fail(k, fmt.Sprintf("duplicate key %q", k.Value))
			ok = false
			continue
		}
		ents = append(ents, entry{key: k.Value, k: k, v: v})
	}
	return ents, ok
}

func asString(n *yaml.Node) (string, bool) {
	n = deref(n)
	if n.Kind != yaml.ScalarNode {
		return "", false
	}
	switch n.ShortTag() {
	case "!!str", "!!timestamp":
		return n.Value, true
	}
	return "", false
}

func asBool(n *yaml.Node) (bool, bool) {
	n = deref(n)
	var b bool
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!bool" || n.Decode(&b) != nil {
		return false, false
	}
	return b, true
}

func asInt(n *yaml.Node) (int, bool) {
	n = deref(n)
	var i int
	if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!int" || n.Decode(&i) != nil {
		return 0, false
	}
	return i, true
}

func asFloat(n *yaml.Node) (float64, bool) {
	n = deref(n)
	var f float64
	if n.Kind != yaml.ScalarNode || (n.ShortTag() != "!!int" && n.ShortTag() != "!!float") || n.Decode(&f) != nil ||
		math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

func asStrings(n *yaml.Node) ([]string, bool) {
	n = deref(n)
	if n.Kind != yaml.SequenceNode {
		return nil, false
	}
	out := make([]string, len(n.Content))
	for i, c := range n.Content {
		s, ok := asString(c)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// toValue converts a YAML node to a JSON-compatible value (numbers are json.Number).
func toValue(n *yaml.Node) (any, error) {
	n = deref(n)
	switch n.Kind {
	case yaml.ScalarNode:
		return scalarValue(n)
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, c := range n.Content {
			v, err := toValue(c)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Kind != yaml.ScalarNode {
				return nil, errors.New("keys must be plain names")
			}
			v, err := toValue(n.Content[i+1])
			if err != nil {
				return nil, err
			}
			out[n.Content[i].Value] = v
		}
		return out, nil
	}
	return nil, errors.New("unsupported value")
}

func scalarValue(n *yaml.Node) (any, error) {
	switch n.ShortTag() {
	case "!!null":
		return nil, nil
	case "!!bool":
		var b bool
		err := n.Decode(&b)
		return b, err
	case "!!int":
		var i int64
		if n.Decode(&i) == nil {
			return json.Number(strconv.FormatInt(i, 10)), nil
		}
		var u uint64
		if err := n.Decode(&u); err != nil {
			return nil, fmt.Errorf("number %s is out of range", n.Value)
		}
		return json.Number(strconv.FormatUint(u, 10)), nil
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
			return nil, fmt.Errorf("%s is not a finite number", n.Value)
		}
		return json.Number(strconv.FormatFloat(f, 'g', -1, 64)), nil
	}
	return n.Value, nil
}

// toNode converts a programmatic Spec value to a YAML node without positions.
func toNode(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: x}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}, nil
	case int:
		return intNode(int64(x)), nil
	case int32:
		return intNode(int64(x)), nil
	case int64:
		return intNode(x), nil
	case float64:
		return floatNode(x)
	case json.Number:
		return numberNode(x)
	case []string:
		out := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, s := range x {
			out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s})
		}
		return out, nil
	case []any:
		return seqNode(x)
	case []Spec:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		return seqNode(items)
	case Spec:
		return mapNode(x)
	case map[string]Spec:
		m := make(map[string]any, len(x))
		for k, v := range x {
			m[k] = v
		}
		return mapNode(m)
	case map[string]any:
		return mapNode(x)
	}
	return nil, fmt.Errorf("unsupported value of type %T", v)
}

func intNode(i int64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(i, 10)}
}

func floatNode(f float64) (*yaml.Node, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, errors.New("numbers must be finite")
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: strconv.FormatFloat(f, 'g', -1, 64)}, nil
}

func numberNode(x json.Number) (*yaml.Node, error) {
	if _, err := x.Float64(); err != nil {
		return nil, fmt.Errorf("invalid number %q", string(x))
	}
	tag := "!!float"
	if isIntegerLiteral(string(x)) {
		tag = "!!int"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(x)}, nil
}

func seqNode(items []any) (*yaml.Node, error) {
	out := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, it := range items {
		c, err := toNode(it)
		if err != nil {
			return nil, err
		}
		out.Content = append(out.Content, c)
	}
	return out, nil
}

func mapNode(m map[string]any) (*yaml.Node, error) {
	out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, k := range sortedKeys(m) {
		c, err := toNode(m[k])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k, err)
		}
		out.Content = append(out.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}, c)
	}
	return out, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func isIntegerLiteral(s string) bool {
	if s != "" && s[0] == '-' {
		s = s[1:]
	}
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
