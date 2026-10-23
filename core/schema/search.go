package schema

import (
	"fmt"
	"slices"
	"strings"
)

// SearchOption is the type option that configures a type's part in search: a boolean, or an object with
// `enabled`, `weights` and `exclude`.
const SearchOption = "search"

// SearchTier is how much a field's text counts towards relevance. Engines keep one column or weight class per
// tier, so the number of tiers is fixed.
type SearchTier int

const (
	TierNormal SearchTier = iota
	TierHigh
	TierLow
)

// SearchName is the key of SearchConfig.Weights that names the item's name.
const SearchName = "name"

// SearchConfig is the compiled `search` option of a type.
type SearchConfig struct {
	Enabled bool
	// Weights maps a top-level field name, or SearchName, to its tier; absent keys are TierNormal.
	Weights map[string]SearchTier
	// Exclude names a top-level boolean field; documents where it is true are not indexed.
	Exclude string
}

// Tier returns the tier of a top-level field (or SearchName).
func (c SearchConfig) Tier(name string) SearchTier { return c.Weights[name] }

// Searchable reports whether the type takes part in search: the type option `search`, else true for
// collections and false for singletons and upload types. Only searchable types are indexed by search engines
// and accepted by the search APIs.
func (t *Type) Searchable() bool { return t.search.Enabled }

// Search returns the type's search configuration.
func (t *Type) Search() SearchConfig { return t.search }

// SearchExcluded reports whether the type's exclusion field is true in doc.
func (t *Type) SearchExcluded(doc map[string]any) bool {
	if t.search.Exclude == "" {
		return false
	}
	b, _ := doc[t.search.Exclude].(bool)
	return b
}

var searchTiers = map[string]SearchTier{"high": TierHigh, "normal": TierNormal, "low": TierLow}

// parseSearch reads the option's value; def is the type's default when the option does not say.
func parseSearch(v any, def bool) (SearchConfig, error) {
	c := SearchConfig{Enabled: def}
	switch x := v.(type) {
	case bool:
		c.Enabled = x
	case map[string]any:
		c.Enabled = true
		for _, k := range sortedKeys(x) {
			switch k {
			case "enabled":
				b, ok := x[k].(bool)
				if !ok {
					return c, fmt.Errorf("enabled must be true or false")
				}
				c.Enabled = b
			case "exclude":
				s, ok := x[k].(string)
				if !ok || s == "" {
					return c, fmt.Errorf("exclude must name a boolean field")
				}
				c.Exclude = s
			case "weights":
				m, ok := x[k].(map[string]any)
				if !ok {
					return c, fmt.Errorf("weights must map field names to high, normal or low")
				}
				for _, name := range sortedKeys(m) {
					s, _ := m[name].(string)
					tier, ok := searchTiers[s]
					if !ok {
						return c, fmt.Errorf("weight of %q must be high, normal or low", name)
					}
					if tier != TierNormal {
						if c.Weights == nil {
							c.Weights = map[string]SearchTier{}
						}
						c.Weights[name] = tier
					}
				}
			default:
				return c, fmt.Errorf("unknown key %q (want enabled, weights, exclude)", k)
			}
		}
	default:
		return c, fmt.Errorf("must be true, false or an object with weights and exclude")
	}
	return c, nil
}

func checkSearchOption(_ *TypeBuilder, v any) error {
	_, err := parseSearch(v, true)
	return err
}

// compileSearch sets the type's search configuration and checks the fields it names.
func (c *compiler) compileSearch(def *typeDef, t *Type) {
	t.search = SearchConfig{Enabled: !t.Singleton && t.Upload == nil}
	o, ok := def.options[SearchOption]
	if !ok {
		return
	}
	cfg, err := parseSearch(o.value, t.search.Enabled)
	if err != nil {
		return // reported by checkTypeOptions
	}
	for _, name := range sortedKeys(cfg.Weights) {
		f, ok := t.Field(name)
		switch {
		case name == SearchName && !ok:
		case !ok:
			c.errs.add(o.pos, "type %q: option %q: weight of %q: not a field of the type", t.Name, SearchOption, name)
		case !f.FeedsSearchText():
			c.errs.add(o.pos, "type %q: option %q: weight of %q: the field has no search text (string, text, slug, rich text and blocks fields do)", t.Name, SearchOption, name)
		}
	}
	if cfg.Exclude != "" {
		if f, ok := t.Field(cfg.Exclude); !ok || f.Shape.Base != BaseBoolean {
			c.errs.add(o.pos, "type %q: option %q: exclude %q must be a boolean field of the type", t.Name, SearchOption, cfg.Exclude)
		}
	}
	t.search = cfg
}

// FeedsSearchText reports whether the field's values contribute to a document's search text.
func (f *Field) FeedsSearchText() bool {
	if f.AI.Exclude {
		return false
	}
	switch f.Shape.Base {
	case BaseString:
		return f.Shape.Format == "" || f.Shape.Format == "text" || f.Shape.Format == "slug"
	case BaseRichText, BaseBlocks:
		return true
	case BaseList:
		return f.Of != nil && f.Of.FeedsSearchText()
	case BaseGroup:
		return slices.ContainsFunc(f.Fields, (*Field).FeedsSearchText)
	}
	return false
}

// SearchTexts is the search text of a document split by tier.
type SearchTexts struct{ High, Normal, Low string }

// SearchTiers collects the searchable text of doc by tier: the item's name and each top-level field's text go
// to its tier, in schema order.
func (s *Schema) SearchTiers(t *Type, name string, doc map[string]any) SearchTexts {
	var sb [3]strings.Builder
	addText(&sb[t.search.Tier(SearchName)], name)
	for _, f := range t.Fields {
		if v, ok := doc[f.Name]; ok {
			collectText(f, v, &sb[t.search.Tier(f.Name)])
		}
	}
	return SearchTexts{High: sb[TierHigh].String(), Normal: sb[TierNormal].String(), Low: sb[TierLow].String()}
}
