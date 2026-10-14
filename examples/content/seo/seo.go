// Package seo is an example module: it adds an "seo" group to every page type through a schema transform,
// without the project's schema files mentioning it.
package seo

import (
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/schema"
)

func Module() kernel.Module { return module{} }

type module struct{}

func (module) Name() string { return "seo" }

func (module) Register(*kernel.Registry) error { return nil }

func (module) Schema(b *schema.Builder) error {
	if err := b.AddGroup("seo", schema.Spec{"fields": schema.Spec{
		"metaTitle":       schema.Spec{"type": "string", "max": 60},
		"metaDescription": schema.Spec{"type": "text", "max": 160},
	}}); err != nil {
		return err
	}
	for _, t := range b.Types() {
		if t.Page() && !t.HasField("seo") {
			if err := t.AddField("seo", schema.Spec{"type": "group", "use": "seo"}); err != nil {
				return err
			}
		}
	}
	return nil
}
