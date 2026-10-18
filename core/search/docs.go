package search

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// BuildDocs builds the documents of one item from its published snapshots (every locale), ordered by
// locale. Snapshots of several items give the documents of each in turn. Items of unknown or not-searchable
// types give none.
func BuildDocs(sc *schema.Schema, snaps []store.Snapshot) ([]Doc, error) {
	var order []ids.UUID
	byItem := map[ids.UUID][]store.Snapshot{}
	for _, sn := range snaps {
		if _, seen := byItem[sn.ItemID]; !seen {
			order = append(order, sn.ItemID)
		}
		byItem[sn.ItemID] = append(byItem[sn.ItemID], sn)
	}
	var out []Doc
	for _, id := range order {
		group := byItem[id]
		t, ok := sc.Type(group[0].Type)
		if !ok || !t.Searchable() {
			continue
		}
		slices.SortFunc(group, func(a, b store.Snapshot) int { return strings.Compare(a.Locale, b.Locale) })
		locales := make([]string, len(group))
		for i, sn := range group {
			locales[i] = sn.Locale
		}
		for _, sn := range group {
			d, err := buildDoc(sc, t, sn, locales)
			if err != nil {
				return nil, err
			}
			out = append(out, d)
		}
	}
	return out, nil
}

func buildDoc(sc *schema.Schema, t *schema.Type, sn store.Snapshot, locales []string) (Doc, error) {
	payload, err := schema.DecodeDoc(sn.Payload)
	if err != nil {
		return Doc{}, fmt.Errorf("search: snapshot of %s/%s: %w", sn.ItemID, sn.Locale, err)
	}
	name, _ := payload["name"].(string)
	fields, _ := payload["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}
	return Doc{
		Item: sn.ItemID, Type: t.Name, Locale: sn.Locale, MainLocale: sn.MainLocale, AlwaysAvailable: sn.AlwaysAvailable,
		Locales: slices.Clone(locales), Name: name, Text: storeutil.CapSearchText(sc.SearchText(t, fields)),
		Fields: indexFields(sc, t, fields), PublishedAt: sn.PublishedAt.UTC(),
	}, nil
}

// indexFields converts the document's indexed fields to the Go types Doc.Fields documents.
func indexFields(sc *schema.Schema, t *schema.Type, fields map[string]any) map[string]any {
	values := sc.IndexValues(t, fields)
	if len(values) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, iv := range values {
		f, _ := t.Field(iv.Field)
		var v any
		switch {
		case iv.Text != nil:
			v = *iv.Text
		case iv.Num != nil:
			v = *iv.Num
		case iv.Time != nil:
			v = time.UnixMilli(*iv.Time).UTC()
		case iv.Bool != nil:
			v = *iv.Bool
		case iv.Ref != "":
			if id, err := ids.Parse(iv.Ref); err == nil {
				if f.Shape.Base == schema.BaseList {
					v = id.String()
				} else {
					v = id
				}
			}
		}
		if v == nil {
			continue
		}
		if f.Shape.Base != schema.BaseList {
			out[iv.Field] = v
			continue
		}
		switch x := v.(type) {
		case string:
			out[iv.Field] = append(asSlice[string](out[iv.Field]), x)
		case float64:
			out[iv.Field] = append(asSlice[float64](out[iv.Field]), x)
		case bool:
			out[iv.Field] = append(asSlice[bool](out[iv.Field]), x)
		case time.Time:
			out[iv.Field] = append(asSlice[time.Time](out[iv.Field]), x)
		}
	}
	return out
}

func asSlice[T any](v any) []T {
	s, _ := v.([]T)
	return s
}
