package schema

import "fmt"

// SearchOption is the type option that switches a type in or out of search.
const SearchOption = "search"

// Searchable reports whether the type takes part in search: the type option `search`, else true for
// collections and false for singletons and upload types. Only searchable types are indexed by search engines
// and accepted by the search APIs.
func (t *Type) Searchable() bool {
	if v, ok := t.Options[SearchOption].(bool); ok {
		return v
	}
	return !t.Singleton && t.Upload == nil
}

func checkSearchOption(_ *TypeBuilder, v any) error {
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("must be true or false")
	}
	return nil
}
