package schema

import (
	"fmt"
	"slices"
)

// FieldType is a pluggable field type. Modules register more with Registry.Register.
type FieldType interface {
	Name() string
	// Compile validates type-specific options (every key of the field spec except the keys all
	// field types share: type, label, description, required, translatable, default, index, when, ai, admin) and returns the
	// value shape and compiled behaviour. Report option problems with Options.Errorf; unknown keys are
	// rejected by the framework.
	Compile(o *Options) (Shape, Config, error)
}

// Registry holds the known field types.
type Registry struct {
	types map[string]FieldType
}

// NewRegistry returns a registry with all core field types.
func NewRegistry() *Registry {
	r := &Registry{types: map[string]FieldType{}}
	for _, ft := range coreTypes() {
		if err := r.Register(ft); err != nil {
			panic(err) // core names are distinct
		}
	}
	return r
}

func (r *Registry) Register(ft FieldType) error {
	name := ft.Name()
	if name == "" {
		return fmt.Errorf("schema: field type needs a name")
	}
	if _, dup := r.types[name]; dup {
		return fmt.Errorf("schema: field type %q is already registered", name)
	}
	r.types[name] = ft
	return nil
}

func (r *Registry) names() []string { return sortedKeys(r.types) }

// isCommon reports whether key is a field-spec key every field type shares; FieldType implementations
// receive the rest.
func isCommon(key string) bool {
	switch key {
	case "type", "label", "description", "required", "translatable", "default", "index", "when", "ai", "admin":
		return true
	}
	return false
}

func uniqueSorted(in []string) []string {
	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}
