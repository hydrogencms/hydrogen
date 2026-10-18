package meilisearch

import (
	"fmt"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/search"
)

// fieldPrefix names the attributes of indexed fields: a field "tags" is the attribute "f_tags".
const fieldPrefix = "f_"

// Attribute names of a document.
const (
	attrItem        = "item"
	attrType        = "type"
	attrLocale      = "locale"
	attrMain        = "is_main"
	attrAlways      = "always"
	attrLocales     = "locales"
	attrName        = "name"
	attrText        = "text"
	attrPublishedAt = "published_at"
)

// docID is the primary key of a document: the item and the locale.
func docID(item ids.UUID, locale string) string { return item.String() + "_" + locale }

// cleanText drops the characters the highlight markers are made of, so that a marker in a snippet is always
// the engine's.
func cleanText(s string) string {
	if !strings.ContainsAny(s, search.MarkOpen+search.MarkClose) {
		return s
	}
	return strings.NewReplacer(search.MarkOpen, "", search.MarkClose, "").Replace(s)
}

// document converts a search document to the shape stored in Meilisearch.
func document(d search.Doc) (map[string]any, error) {
	locales := d.Locales
	if locales == nil {
		locales = []string{}
	}
	m := map[string]any{
		"id":            docID(d.Item, d.Locale),
		attrItem:        d.Item.String(),
		attrType:        d.Type,
		attrLocale:      d.Locale,
		attrMain:        d.Locale == d.MainLocale,
		attrAlways:      d.AlwaysAvailable,
		attrLocales:     locales,
		attrName:        cleanText(d.Name),
		attrText:        cleanText(d.Text),
		attrPublishedAt: d.PublishedAt.Unix(),
	}
	for name, v := range d.Fields {
		cv, err := fieldValue(v)
		if err != nil {
			return nil, fmt.Errorf("meilisearch: field %q of %s/%s: %w", name, d.Item, d.Locale, err)
		}
		if cv != nil {
			m[fieldPrefix+name] = cv
		}
	}
	return m, nil
}

// fieldValue converts an indexed value: times become Unix seconds, ids strings. Empty values are dropped.
func fieldValue(v any) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		return cleanText(x), nil
	case float64, bool, int, int64:
		return x, nil
	case float32:
		return float64(x), nil
	case int8:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case uint:
		return uint64(x), nil
	case uint8:
		return uint64(x), nil
	case uint16:
		return uint64(x), nil
	case uint32:
		return uint64(x), nil
	case uint64:
		return x, nil
	case time.Time:
		return x.Unix(), nil
	case ids.UUID:
		return x.String(), nil
	case []string:
		return list(x, func(s string) any { return cleanText(s) }), nil
	case []float64:
		return list(x, func(f float64) any { return f }), nil
	case []bool:
		return list(x, func(b bool) any { return b }), nil
	case []time.Time:
		return list(x, func(t time.Time) any { return t.Unix() }), nil
	case []ids.UUID:
		return list(x, func(u ids.UUID) any { return u.String() }), nil
	}
	return nil, fmt.Errorf("unsupported value type %T", v)
}

func list[T any](in []T, conv func(T) any) any {
	if len(in) == 0 {
		return nil
	}
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = conv(v)
	}
	return out
}
