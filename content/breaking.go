package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/store"
)

// compiledSchema is the part of the schema's canonical encoding needed to compare revisions.
type compiledSchema struct {
	Types  []compiledType  `json:"types"`
	Groups []compiledGroup `json:"groups"`
}

type compiledType struct {
	Name   string          `json:"name"`
	Fields []compiledField `json:"fields"`
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
	var accounted []string
	for _, m := range s.migrations {
		accounted = append(accounted, m.Changes...)
	}
	return unaccounted(diffSchemas(old, cur), accounted), nil
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
		d.fields(t.Name, t.Fields, ct.Fields, true)
	}
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
