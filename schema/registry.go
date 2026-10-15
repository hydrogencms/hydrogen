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
	opts  map[string]func(t *TypeBuilder, v any) error
}

// coreTypeKeys are the keys a type spec has on its own; type options may not reuse them.
func coreTypeKeys() []string {
	return []string{"admin", "ai", "blockset", "description", "fields", "label", "name", "page", "singleton"}
}

// NewRegistry returns a registry with all core field types.
func NewRegistry() *Registry {
	r := &Registry{types: map[string]FieldType{}, opts: map[string]func(*TypeBuilder, any) error{}}
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

// TypeOption registers a type-level schema key for a module. Type specs (YAML or Spec) may then carry the key;
// its value is any JSON-compatible value (numbers are json.Number), stored in Type.Options and part of the
// canonical JSON and hash. check runs at Compile, after all transforms, for every type that sets the option;
// it may inspect other types through t. Its error is reported at the option's position. The name must be a
// valid lower camel-case identifier that is neither a core type key nor an already registered option.
func (r *Registry) TypeOption(name string, check func(t *TypeBuilder, v any) error) error {
	switch {
	case !nameRE.MatchString(name) || len(name) > maxName:
		return fmt.Errorf("schema: type option name %q must match %s and be at most %d characters", name, nameRE, maxName)
	case check == nil:
		return fmt.Errorf("schema: type option %q needs a check function", name)
	case slices.Contains(coreTypeKeys(), name):
		return fmt.Errorf("schema: type option %q clashes with a core type key", name)
	}
	if _, dup := r.opts[name]; dup {
		return fmt.Errorf("schema: type option %q is already registered", name)
	}
	r.opts[name] = check
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
