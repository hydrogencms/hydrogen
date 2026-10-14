// Package hooks registers code that runs at points in an item's life (draft writes, publish, delete).
// Typed wrappers generated per content type sit on top (content.Handle).
package hooks

import (
	"context"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

type Event string

const (
	BeforeValidate Event = "beforeValidate" // normalise input before validation (draft writes)
	Validate       Event = "validate"       // add field errors (draft writes and publish)
	BeforeSave     Event = "beforeSave"     // compute fields after validation (draft writes)
	BeforePublish  Event = "beforePublish"  // last check before a version is written; may block
	AfterPublish   Event = "afterPublish"   // after the version is written (in the tx, or async after commit)
	BeforeDelete   Event = "beforeDelete"   // may block
	AfterDelete    Event = "afterDelete"    // in the tx, or async after commit
)

func (e Event) asyncAllowed() bool { return e == AfterPublish || e == AfterDelete }

// Item identifies the item a hook runs for.
type Item struct {
	ID         ids.UUID
	Type       string
	Key        string
	Version    int // published version after the operation (0 if none)
	MainLocale string
}

type Context struct {
	context.Context
	Event  Event
	Tx     store.Tx // nil in async hooks
	Item   Item
	Locale string
	// Doc is the merged document for Locale on write events; hooks may change it. Nil on delete events.
	Doc map[string]any
	// Errors collects field errors (Validate, BeforePublish).
	Errors *schema.Report
}

type Func func(*Context) error

type Hook struct {
	Type   string // content type; "" = every type
	Event  Event
	Async  bool   // AfterPublish/AfterDelete only: run after commit from the outbox
	Name   string // required for async hooks: stable consumer name
	Fn     Func
	Module string // set by the kernel: who registered it
}

// Registry holds hooks in registration order. It is filled during setup and read-only afterwards.
type Registry struct {
	hooks []Hook
}

func (r *Registry) Add(h Hook) error {
	switch {
	case h.Fn == nil:
		return fmt.Errorf("hooks: %s hook without a function", h.Event)
	case !slices.Contains([]Event{BeforeValidate, Validate, BeforeSave, BeforePublish, AfterPublish, BeforeDelete, AfterDelete}, h.Event):
		return fmt.Errorf("hooks: unknown event %q", h.Event)
	case h.Async && !h.Event.asyncAllowed():
		return fmt.Errorf("hooks: %s hooks cannot be async", h.Event)
	case h.Async && h.Name == "":
		return fmt.Errorf("hooks: async %s hook needs a name (it becomes an outbox consumer)", h.Event)
	}
	r.hooks = append(r.hooks, h)
	return nil
}

// Sync returns the synchronous hooks for a type and event, in registration order.
func (r *Registry) Sync(typ string, e Event) []Hook {
	var out []Hook
	for _, h := range r.hooks {
		if !h.Async && h.Event == e && (h.Type == "" || h.Type == typ) {
			out = append(out, h)
		}
	}
	return out
}

// Async returns every async hook.
func (r *Registry) Async() []Hook {
	var out []Hook
	for _, h := range r.hooks {
		if h.Async {
			out = append(out, h)
		}
	}
	return out
}

// Run calls the synchronous hooks for c.Event in order and stops at the first error.
func (r *Registry) Run(c *Context, typ string) error {
	for _, h := range r.Sync(typ, c.Event) {
		if err := h.Fn(c); err != nil {
			if h.Module != "" {
				return fmt.Errorf("%s hook (%s): %w", c.Event, h.Module, err)
			}
			return fmt.Errorf("%s hook: %w", c.Event, err)
		}
	}
	return nil
}
