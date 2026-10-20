package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/store"
)

// compiledSchema is the part of the schema's canonical encoding needed to compare revisions.
type compiledSchema struct {
	Types  []compiledType  `json:"types"`
	Groups []compiledGroup `json:"groups"`
	Blocks []compiledBlock `json:"blocks"`
	Tokens []compiledToken `json:"tokens"`
}

type compiledType struct {
	Name     string          `json:"name"`
	Blockset bool            `json:"blockset"`
	Fields   []compiledField `json:"fields"`
}

type compiledBlock struct {
	Name  string          `json:"name"`
	Props []compiledField `json:"props"`
	Slots []compiledSlot  `json:"slots"`
}

type compiledSlot struct {
	Name  string   `json:"name"`
	Allow []string `json:"allow"`
	Min   int      `json:"min"`
	Max   int      `json:"max"`
}

type compiledToken struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

type compiledGroup struct {
	Name   string          `json:"name"`
	Fields []compiledField `json:"fields"`
}

type compiledField struct {
	Name         string                `json:"name"`
	Type         string                `json:"type"`
	Translatable bool                  `json:"translatable"`
	Shape        struct{ Base string } `json:"shape"`
	Of           *compiledField        `json:"of"`
	Fields       []compiledField       `json:"fields"`
	Group        string                `json:"group"`
	Options      map[string]any        `json:"options"`
}

func parseCompiled(data []byte) (*compiledSchema, error) {
	var cs compiledSchema
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&cs); err != nil {
		return nil, fmt.Errorf("content: decode compiled schema: %w", err)
	}
	return &cs, nil
}

// BreakingChanges compares the latest recorded schema revision with the current schema and returns breaking
// changes no registered migration accounts for (e.g. "article.summary: removed"). Without a recorded
// revision there are none.
func (s *Service) BreakingChanges(ctx context.Context) ([]string, error) {
	var (
		hash     string
		compiled []byte
		rev      int
	)
	err := s.o.DB.View(ctx, func(tx store.Tx) (err error) {
		rev, hash, compiled, err = tx.Content().LatestSchemaRev(ctx)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("content: latest schema revision: %w", err)
	}
	if rev == 0 || hash == s.o.Schema.Hash() {
		return nil, nil
	}
	old, err := parseCompiled(compiled)
	if err != nil {
		return nil, err
	}
	cur, err := parseCompiled(s.compiled)
	if err != nil {
		return nil, err
	}
	return unaccountedBy(diffSchemas(old, cur), old, cur, s.migrations), nil
}

// unaccountedBy drops changes the migrations account for. A change under a type is accounted by any migration
// listing it. A block or token change applies to every type whose stored documents can hold it, so each of
// those types needs a migration of its own listing it; the types still missing are named in the message. The
// old schema decides which types can hold it (their documents were written under it), restricted to types
// that still exist: narrowing a type's allowed blocks is reported on that type's own field.
func unaccountedBy(changes []string, old, cur *compiledSchema, migrations []Migration) []string {
	var all []string
	byType := make(map[string][]string)
	for _, m := range migrations {
		all = append(all, m.Changes...)
		byType[m.Type] = append(byType[m.Type], m.Changes...)
	}
	holders := holdersOf(old, cur)
	var out []string
	for _, ch := range changes {
		path, what, _ := strings.Cut(ch, ": ")
		key, ok := holderKey(path)
		if !ok || len(holders[key]) == 0 {
			out = append(out, unaccounted([]string{ch}, all)...)
			continue
		}
		var missing []string
		for _, typ := range holders[key] {
			if len(unaccounted([]string{ch}, byType[typ])) > 0 {
				missing = append(missing, typ)
			}
		}
		if len(missing) == 0 {
			continue
		}
		noun := "type "
		if len(missing) > 1 {
			noun = "types "
		}
		out = append(out, path+": "+what+" (needs a migration for "+noun+strings.Join(missing, ", ")+")")
	}
	return out
}

// holderKey names what a change path belongs to: "block:hero" or "token:tone".
func holderKey(path string) (string, bool) {
	switch {
	case strings.HasPrefix(path, "block:"):
		name, _, _ := strings.Cut(path, ".")
		return name, true
	case strings.HasPrefix(path, "token:"):
		return path, true
	}
	return "", false
}

// holdersOf maps "block:<name>" and "token:<set>" to the sorted types (present in cur) whose documents can
// hold them: through their blocks fields, transitively through block slots and block props.
func holdersOf(old, cur *compiledSchema) map[string][]string {
	groups := groupMap(old.Groups)
	blocks := make(map[string]compiledBlock, len(old.Blocks))
	for _, b := range old.Blocks {
		blocks[b.Name] = b
	}
	exists := make(map[string]bool, len(cur.Types))
	for _, t := range cur.Types {
		exists[t.Name] = true
	}
	sets := make(map[string]map[string]bool)
	for _, t := range old.Types {
		if !exists[t.Name] {
			continue
		}
		var queue []string
		keys := make(map[string]bool)
		var visit func(f *compiledField)
		visit = func(f *compiledField) {
			switch f.Type {
			case "blocks":
				queue = append(queue, optStrings(f.Options["allow"])...)
			case "token":
				if set, ok := f.Options["set"].(string); ok {
					keys["token:"+set] = true
				}
			}
			if f.Of != nil {
				visit(f.Of)
			}
			for i, c := 0, children(f, groups); i < len(c); i++ {
				visit(&c[i])
			}
		}
		for i := range t.Fields {
			visit(&t.Fields[i])
		}
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if keys["block:"+name] {
				continue
			}
			keys["block:"+name] = true
			b := blocks[name]
			for i := range b.Props {
				visit(&b.Props[i])
			}
			for _, sl := range b.Slots {
				queue = append(queue, sl.Allow...)
			}
		}
		for k := range keys {
			if sets[k] == nil {
				sets[k] = make(map[string]bool)
			}
			sets[k][t.Name] = true
		}
	}
	out := make(map[string][]string, len(sets))
	for k, v := range sets {
		for typ := range v {
			out[k] = append(out[k], typ)
		}
		slices.Sort(out[k])
	}
	return out
}

// diffSchemas lists breaking changes as "path: what", sorted.
func diffSchemas(old, cur *compiledSchema) []string {
	curTypes := make(map[string]compiledType, len(cur.Types))
	for _, t := range cur.Types {
		curTypes[t.Name] = t
	}
	d := differ{oldGroups: groupMap(old.Groups), curGroups: groupMap(cur.Groups)}
	for _, t := range old.Types {
		ct, ok := curTypes[t.Name]
		if !ok {
			d.add(t.Name, "removed")
			continue
		}
		if t.Blockset && !ct.Blockset {
			d.add(t.Name, "blockset removed")
		}
		d.fields(t.Name, t.Fields, ct.Fields, true)
	}
	d.blocks(old.Blocks, cur.Blocks)
	d.tokens(old.Tokens, cur.Tokens)
	slices.Sort(d.out)
	return slices.Compact(d.out)
}

func groupMap(gs []compiledGroup) map[string][]compiledField {
	m := make(map[string][]compiledField, len(gs))
	for _, g := range gs {
		m[g.Name] = g.Fields
	}
	return m
}

type differ struct {
	oldGroups, curGroups map[string][]compiledField
	out                  []string
}

func (d *differ) add(path, what string) { d.out = append(d.out, path+": "+what) }

func (d *differ) fields(path string, old, cur []compiledField, top bool) {
	curBy := make(map[string]*compiledField, len(cur))
	for i := range cur {
		curBy[cur[i].Name] = &cur[i]
	}
	for i := range old {
		of := &old[i]
		p := path + "." + of.Name
		nf, ok := curBy[of.Name]
		switch {
		case !ok:
			d.add(p, "removed")
		case of.Type != nf.Type || of.Shape.Base != nf.Shape.Base:
			d.add(p, "type changed")
		default:
			if top && of.Translatable != nf.Translatable {
				d.add(p, "translatable changed")
			}
			d.optionChanges(p, of, nf)
			d.fields(p, children(of, d.oldGroups), children(nf, d.curGroups), false)
		}
	}
}

// children returns the sub-fields of a group field, a group-typed list element, or none.
func children(f *compiledField, groups map[string][]compiledField) []compiledField {
	switch {
	case f.Group != "":
		return groups[f.Group]
	case f.Fields != nil:
		return f.Fields
	case f.Of != nil:
		return children(f.Of, groups)
	}
	return nil
}

// unaccounted drops changes covered by an accounted path ("article.summary", or "article" for everything
// below it).
func unaccounted(changes, accounted []string) []string {
	var out []string
	for _, ch := range changes {
		path, _, _ := strings.Cut(ch, ": ")
		covered := slices.ContainsFunc(accounted, func(a string) bool { return path == a || strings.HasPrefix(path, a+".") })
		if !covered {
			out = append(out, ch)
		}
	}
	return out
}

// optionChanges reports narrowed blocks fields (allow, min, max) and token fields moved to another set.
func (d *differ) optionChanges(path string, old, cur *compiledField) {
	switch old.Type {
	case "blocks":
		if gone := removed(optStrings(old.Options["allow"]), optStrings(cur.Options["allow"])); len(gone) > 0 {
			d.add(path, "allow narrowed (removed "+strings.Join(gone, ", ")+")")
		}
		d.limits(path, optInt(old.Options["min"]), optInt(old.Options["max"]), optInt(cur.Options["min"]), optInt(cur.Options["max"]))
	case "token":
		if old.Options["set"] != cur.Options["set"] {
			d.add(path, "token set changed")
		}
	}
}

// limits reports a raised minimum or a lowered (or new) maximum; a maximum of 0 means none.
func (d *differ) limits(path string, oldMin, oldMax, curMin, curMax int) {
	if curMin > oldMin {
		d.add(path, "min tightened")
	}
	if curMax > 0 && (oldMax == 0 || curMax < oldMax) {
		d.add(path, "max tightened")
	}
}

func (d *differ) blocks(old, cur []compiledBlock) {
	curBy := make(map[string]compiledBlock, len(cur))
	for _, b := range cur {
		curBy[b.Name] = b
	}
	for _, ob := range old {
		path := "block:" + ob.Name
		nb, ok := curBy[ob.Name]
		if !ok {
			d.add(path, "removed")
			continue
		}
		d.fields(path+".props", ob.Props, nb.Props, false)
		slotBy := make(map[string]compiledSlot, len(nb.Slots))
		for _, sl := range nb.Slots {
			slotBy[sl.Name] = sl
		}
		for _, os := range ob.Slots {
			sp := path + ".slots." + os.Name
			ns, ok := slotBy[os.Name]
			if !ok {
				d.add(sp, "removed")
				continue
			}
			if gone := removed(os.Allow, ns.Allow); len(gone) > 0 {
				d.add(sp, "allow narrowed (removed "+strings.Join(gone, ", ")+")")
			}
			d.limits(sp, os.Min, os.Max, ns.Min, ns.Max)
		}
	}
}

func (d *differ) tokens(old, cur []compiledToken) {
	curBy := make(map[string]compiledToken, len(cur))
	for _, t := range cur {
		curBy[t.Name] = t
	}
	for _, ot := range old {
		nt, ok := curBy[ot.Name]
		if !ok {
			d.add("token:"+ot.Name, "removed")
			continue
		}
		if gone := removed(ot.Values, nt.Values); len(gone) > 0 {
			d.add("token:"+ot.Name, "values removed ("+strings.Join(gone, ", ")+")")
		}
	}
}

// removed lists what old has and cur lacks, in old's order.
func removed(old, cur []string) []string {
	var out []string
	for _, v := range old {
		if !slices.Contains(cur, v) {
			out = append(out, v)
		}
	}
	return out
}

func optStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func optInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}
