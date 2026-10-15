package channel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/config"
)

// parseTemplates expands each template into one channel per combination of its `for` values.
func (cx *compiler) parseTemplates(tn *config.Node) []namedSpec {
	if tn == nil {
		return nil
	}
	if tn.Kind() != "list" {
		cx.fail(tn, "templates must be a list")
		return nil
	}
	var out []namedSpec
	for _, t := range tn.Items() {
		out = append(out, cx.expand(t)...)
	}
	return out
}

func (cx *compiler) expand(t *config.Node) []namedSpec {
	if t.Kind() != "map" {
		cx.fail(t, "a template must be a mapping")
		return nil
	}
	combos, ok := cx.combinations(t)
	if !ok {
		return nil
	}
	if t.Get("name") == nil {
		cx.fail(t, "template needs a name, e.g. name: \"site_{market}\"")
		return nil
	}
	var out []namedSpec
	for _, combo := range combos {
		pairs := make([]string, 0, 4*len(combo))
		for _, kv := range combo {
			pairs = append(pairs, "{"+kv[0]+"}", kv[1], "{"+strings.ToUpper(kv[0])+"}", strings.ToUpper(kv[1]))
		}
		clone := t.Clone(strings.NewReplacer(pairs...).Replace)
		name, err := clone.Get("name").String()
		if err != nil {
			cx.add(err)
			continue
		}
		if sp := cx.parseSpec(clone, kindTemplate); sp != nil {
			out = append(out, namedSpec{name, sp})
		}
	}
	return out
}

// combinations is the cartesian product of the template's `for` lists: keys sorted, values in order.
func (cx *compiler) combinations(t *config.Node) ([][][2]string, bool) {
	fn := t.Get("for")
	if fn == nil || fn.Kind() != "map" || len(fn.Keys()) == 0 {
		cx.fail(t, "template needs `for`: a mapping of placeholder name to a list of values")
		return nil, false
	}
	keys := slices.Sorted(slices.Values(fn.Keys()))
	combos := [][][2]string{nil}
	for _, k := range keys {
		vn := fn.Get(k)
		if k == "id" || k == "locale" || k == "collection" {
			cx.fail(vn, "placeholder name %q is reserved (it is a preview placeholder)", k)
			return nil, false
		}
		if !nameRE.MatchString(k) {
			cx.fail(vn, "placeholder name %q: use lowercase letters, digits and underscores", k)
			return nil, false
		}
		vals, ok := cx.stringList(vn, fmt.Sprintf("for.%s", k))
		if !ok {
			return nil, false
		}
		var next [][][2]string
		for _, c := range combos {
			for _, v := range vals {
				next = append(next, append(slices.Clone(c), [2]string{k, v}))
			}
		}
		combos = next
	}
	return combos, true
}
