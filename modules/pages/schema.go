package pages

import (
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/schema"
)

const (
	childrenOption = "children"
	redirectType   = "redirect"
)

// checkChildren validates the `children` type option: a list of page type names, on a page type.
func checkChildren(t *schema.TypeBuilder, v any) error {
	if !t.Page() {
		return errors.New("children is only valid on page types (page: true)")
	}
	list, ok := v.([]any)
	if !ok {
		return errors.New("children must be a list of page type names")
	}
	for _, e := range list {
		name, ok := e.(string)
		if !ok {
			return errors.New("children must be a list of page type names")
		}
		child, found := t.Type(name)
		if !found {
			return fmt.Errorf("children: unknown type %q", name)
		}
		if !child.Page() {
			return fmt.Errorf("children: type %q is not a page type", name)
		}
	}
	return nil
}

// Schema adds the slug and vanityPath fields to page types and the redirect collection type.
func (m *module) Schema(b *schema.Builder) error {
	for _, t := range b.Types() {
		if !t.Page() {
			continue
		}
		if !t.HasField("slug") {
			spec := schema.Spec{"type": "slug", "translatable": true}
			if t.HasField("title") {
				spec["from"] = "title"
			}
			if err := t.AddField("slug", spec); err != nil {
				return err
			}
		}
		if !t.HasField("vanityPath") {
			err := t.AddField("vanityPath", schema.Spec{"type": "string", "translatable": true,
				"description": "Extra URL serving this page, e.g. /promo"})
			if err != nil {
				return err
			}
		}
	}
	if _, exists := b.Type(redirectType); exists {
		return fmt.Errorf("pages: the schema defines type %q, which the pages module owns", redirectType)
	}
	if err := b.AddType(redirectType, schema.Spec{"label": "Redirect", "name": "{from}",
		"description": "Sends visitors from one path to another URL."}); err != nil {
		return err
	}
	t, _ := b.Type(redirectType)
	for _, f := range []struct {
		name string
		spec schema.Spec
	}{
		{"from", schema.Spec{"type": "string", "required": true, "index": true, "max": 1024,
			"description": "Site path to redirect, e.g. /old-page"}},
		{"to", schema.Spec{"type": "string", "required": true, "max": 2048,
			"description": "Absolute URL, site path starting with /, or item:<id>"}},
		{"statusCode", schema.Spec{"type": "enum", "values": []any{"301", "302", "307", "308"}, "default": "301"}},
		{"site", schema.Spec{"type": "string", "default": m.cfg.Sites[0]}},
		{"forLocale", schema.Spec{"type": "string", "description": "Locale code; empty: every locale"}},
	} {
		if err := t.AddField(f.name, f.spec); err != nil {
			return err
		}
	}
	return nil
}
