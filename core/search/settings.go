package search

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// Kinds of indexed fields.
const (
	KindText = "text"
	KindNum  = "num"
	KindTime = "time"
	KindBool = "bool"
	KindRef  = "ref"
)

// DocRevision changes whenever BuildDocs would produce different documents for the same content, so engines
// rebuild their index after an upgrade.
const DocRevision = 2

// FieldSetting describes one indexed field of the searchable types. A field name shared by several types
// takes the kind of the first type (by name) that has it; keep the kinds of same-named fields equal.
type FieldSetting struct {
	Name     string
	Kind     string
	List     bool
	Sortable bool // scalar fields
}

// Settings are what an external engine configures its index with.
type Settings struct {
	Fields []FieldSetting // indexed fields of searchable types, sorted by name
	// Types are the searchable types, sorted.
	Types []string
	// Fingerprint is the hex SHA-256 of the canonical settings and DocRevision: stored documents must be
	// rebuilt when it changes.
	Fingerprint string
}

// KindOf returns the search kind of a field spec's shape.
func KindOf(sh schema.Shape) (string, bool) {
	switch sh.Base {
	case schema.BaseFile:
		return KindText, true
	case schema.BaseString:
		if sh.Format == "date" || sh.Format == "datetime" {
			return KindTime, true
		}
		return KindText, true
	case schema.BaseNumber:
		return KindNum, true
	case schema.BaseBoolean:
		return KindBool, true
	case schema.BaseRef:
		return KindRef, true
	}
	return "", false
}

// indexedField returns the setting of an indexed top-level field.
func indexedField(f *schema.Field) (FieldSetting, bool) {
	if !f.Index {
		return FieldSetting{}, false
	}
	elem := f
	list := f.Shape.Base == schema.BaseList
	if list {
		if f.Of == nil {
			return FieldSetting{}, false
		}
		elem = f.Of
	}
	kind, ok := KindOf(elem.Shape)
	if !ok {
		return FieldSetting{}, false
	}
	return FieldSetting{Name: f.Name, Kind: kind, List: list, Sortable: !list}, true
}

// NewSettings derives the settings from the searchable types of the schema.
func NewSettings(sc *schema.Schema) Settings {
	var s Settings
	byName := map[string]int{}
	for _, t := range sc.Types() { // sorted by name
		if !t.Searchable() {
			continue
		}
		s.Types = append(s.Types, t.Name)
		for _, f := range t.Fields {
			fs, ok := indexedField(f)
			if !ok {
				continue
			}
			if i, dup := byName[fs.Name]; dup {
				s.Fields[i].List = s.Fields[i].List || fs.List
				s.Fields[i].Sortable = s.Fields[i].Sortable && fs.Sortable
				continue
			}
			byName[fs.Name] = len(s.Fields)
			s.Fields = append(s.Fields, fs)
		}
	}
	slices.SortFunc(s.Fields, func(a, b FieldSetting) int { return strings.Compare(a.Name, b.Name) })
	s.Fingerprint = fingerprint(s, sc)
	return s
}

func fingerprint(s Settings, sc *schema.Schema) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "revision %d\ntypes %s\n", DocRevision, strings.Join(s.Types, ","))
	for _, name := range s.Types {
		t, _ := sc.Type(name)
		fmt.Fprintf(&sb, "type %s name=%q text=%s\n", name, t.NamePattern, strings.Join(textFields(t), ","))
	}
	for _, f := range s.Fields {
		fmt.Fprintf(&sb, "field %s %s list=%t sortable=%t\n", f.Name, f.Kind, f.List, f.Sortable)
	}
	sum := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(sum[:])
}

// textFields lists the top-level fields whose values feed a document's text.
func textFields(t *schema.Type) []string {
	var out []string
	for _, f := range t.Fields {
		if feedsText(f) {
			out = append(out, f.Name)
		}
	}
	return out
}

func feedsText(f *schema.Field) bool {
	if f.AI.Exclude {
		return false
	}
	switch f.Shape.Base {
	case schema.BaseString:
		return f.Shape.Format == "" || f.Shape.Format == "text" || f.Shape.Format == "slug"
	case schema.BaseRichText, schema.BaseBlocks:
		return true
	case schema.BaseList:
		return f.Of != nil && feedsText(f.Of)
	case schema.BaseGroup:
		return slices.ContainsFunc(f.Fields, feedsText)
	}
	return false
}
