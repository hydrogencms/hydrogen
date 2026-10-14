// Package kernel assembles a Hydrogen application from modules.
//
// Lifecycle: modules are ordered by their requirements, configured from modules.<name>, then Register runs for
// each (declare extension points, touch no services), core services are built, Boot runs for each, and Run
// starts them, serves HTTP and delivers outbox events until the context ends.
package kernel

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"regexp"
	"sort"

	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

type Module interface {
	Name() string
	Register(r *Registry) error
}

// Requirer lists modules that must be present and registered first.
type Requirer interface {
	Requires() []string
}

// Configurable modules return a pointer to their config, pre-filled with defaults. It is decoded from
// modules.<name> before Register.
type Configurable interface {
	Config() any
}

type Booter interface {
	Boot(ctx context.Context, s *Services) error
}

// Starter starts background work that must stop when ctx ends. Start must not block.
type Starter interface {
	Start(ctx context.Context) error
}

type Stopper interface {
	Stop(ctx context.Context) error
}

type Command struct {
	Name    string
	Summary string
	Run     func(ctx context.Context, app *App, args []string) error
}

var moduleName = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Registry collects what modules contribute during Register.
type Registry struct {
	current    string
	openers    map[string]registered[store.Opener]
	migrations []registered[MigrationSource]
	consumers  []outbox.Consumer
	routes     []registered[route]
	commands   map[string]registered[Command]
	provided   map[reflect.Type]any
}

type registered[T any] struct {
	module string
	value  T
}

type route struct {
	pattern string
	handler http.Handler
}

// MigrationSource returns a module's migrations for a dialect ("postgres", "sqlite"), or an error if the module
// doesn't support it.
type MigrationSource struct {
	Source string
	FS     func(dialect string) (fs.FS, error)
}

func newRegistry() *Registry {
	return &Registry{
		openers:  map[string]registered[store.Opener]{},
		commands: map[string]registered[Command]{},
		provided: map[reflect.Type]any{},
	}
}

// Database registers an adapter for URLs with the given scheme ("sqlite" for sqlite:path).
func (r *Registry) Database(scheme string, open store.Opener) error {
	if prev, ok := r.openers[scheme]; ok {
		return fmt.Errorf("database scheme %q registered by both %s and %s", scheme, prev.module, r.current)
	}
	r.openers[scheme] = registered[store.Opener]{r.current, open}
	return nil
}

func (r *Registry) Migrations(m MigrationSource) {
	r.migrations = append(r.migrations, registered[MigrationSource]{r.current, m})
}

func (r *Registry) Consumer(c outbox.Consumer) {
	r.consumers = append(r.consumers, c)
}

// Handle adds an HTTP route (net/http ServeMux pattern syntax).
func (r *Registry) Handle(pattern string, h http.Handler) {
	r.routes = append(r.routes, registered[route]{r.current, route{pattern, h}})
}

func (r *Registry) Command(c Command) error {
	if prev, ok := r.commands[c.Name]; ok {
		return fmt.Errorf("command %q registered by both %s and %s", c.Name, prev.module, r.current)
	}
	r.commands[c.Name] = registered[Command]{r.current, c}
	return nil
}

// Provide makes a value available to other modules by its type.
func Provide[T any](r *Registry, v T) {
	r.provided[reflect.TypeFor[T]()] = v
}

// Lookup finds a value another module provided.
func Lookup[T any](r *Registry) (T, bool) {
	v, ok := r.provided[reflect.TypeFor[T]()]
	if !ok {
		var zero T
		return zero, false
	}
	return v.(T), true
}

// order returns modules with every module after the ones it requires. Among unconstrained modules, the given
// order is kept, so the result is deterministic.
func order(mods []Module) ([]Module, error) {
	byName := map[string]int{}
	for i, m := range mods {
		n := m.Name()
		if !moduleName.MatchString(n) {
			return nil, fmt.Errorf("module name %q: use lowercase letters, digits and dashes", n)
		}
		if _, dup := byName[n]; dup {
			return nil, fmt.Errorf("module %q added twice", n)
		}
		byName[n] = i
	}
	deps := make([][]int, len(mods))
	for i, m := range mods {
		req, ok := m.(Requirer)
		if !ok {
			continue
		}
		for _, n := range req.Requires() {
			j, ok := byName[n]
			if !ok {
				return nil, fmt.Errorf("module %q requires %q, which is not in this build", m.Name(), n)
			}
			deps[i] = append(deps[i], j)
		}
		sort.Ints(deps[i])
	}
	const (
		unseen = iota
		visiting
		done
	)
	state := make([]int, len(mods))
	var out []Module
	var visit func(i int, path []string) error
	visit = func(i int, path []string) error {
		switch state[i] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("module requirements form a cycle: %v", append(path, mods[i].Name()))
		}
		state[i] = visiting
		for _, j := range deps[i] {
			if err := visit(j, append(path, mods[i].Name())); err != nil {
				return err
			}
		}
		state[i] = done
		out = append(out, mods[i])
		return nil
	}
	for i := range mods {
		if err := visit(i, nil); err != nil {
			return nil, err
		}
	}
	return out, nil
}
