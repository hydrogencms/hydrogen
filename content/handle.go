package content

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
)

// Handle is the typed API for one content type; codegen emits one per type (var ArticleType =
// content.Handle[Article]{Type: "article"}).
type Handle[T any] struct {
	Type string
}

// HookRegistrar receives hooks; *hydrogen.Builder and the kernel registry implement it.
type HookRegistrar interface {
	AddHook(hooks.Hook) error
}

// Item is a typed view.
type Item[T any] struct {
	Meta View // Meta.Fields is nil; use Doc
	Doc  T
}

type HookOption func(*hooks.Hook)

// Async runs an AfterPublish/AfterDelete hook after commit from the outbox under a stable name.
func Async(name string) HookOption {
	return func(h *hooks.Hook) { h.Async, h.Name = true, name }
}

// The typed hook registrations panic when the registry rejects the hook (an invalid option such as Async on
// a BeforeSave hook): that is a programming error found at start-up.

func (h Handle[T]) BeforeValidate(r HookRegistrar, fn func(c *hooks.Context, doc *T) error) {
	h.register(r, hooks.BeforeValidate, typedFunc(fn))
}

func (h Handle[T]) Validate(r HookRegistrar, fn func(c *hooks.Context, doc *T) error) {
	h.register(r, hooks.Validate, typedFunc(fn))
}

func (h Handle[T]) BeforeSave(r HookRegistrar, fn func(c *hooks.Context, doc *T) error) {
	h.register(r, hooks.BeforeSave, typedFunc(fn))
}

func (h Handle[T]) BeforePublish(r HookRegistrar, fn func(c *hooks.Context, doc *T) error) {
	h.register(r, hooks.BeforePublish, typedFunc(fn))
}

func (h Handle[T]) AfterPublish(r HookRegistrar, fn func(c *hooks.Context, doc *T) error, opts ...HookOption) {
	h.register(r, hooks.AfterPublish, typedFunc(fn), opts...)
}

func (h Handle[T]) BeforeDelete(r HookRegistrar, fn func(c *hooks.Context) error) {
	h.register(r, hooks.BeforeDelete, fn)
}

func (h Handle[T]) AfterDelete(r HookRegistrar, fn func(c *hooks.Context) error, opts ...HookOption) {
	h.register(r, hooks.AfterDelete, fn, opts...)
}

func (h Handle[T]) register(r HookRegistrar, ev hooks.Event, fn hooks.Func, opts ...HookOption) {
	hk := hooks.Hook{Type: h.Type, Event: ev, Fn: fn}
	for _, o := range opts {
		o(&hk)
	}
	if err := r.AddHook(hk); err != nil {
		panic(fmt.Errorf("content: register %s hook for %q: %w", ev, h.Type, err))
	}
}

// typedFunc adapts a typed hook: the map document is converted to *T, and the fields the hook changed are
// written back into the same map (untouched fields keep their stored form).
func typedFunc[T any](fn func(*hooks.Context, *T) error) hooks.Func {
	return func(c *hooks.Context) error {
		var doc T
		if err := fromMap(c.Doc, &doc); err != nil {
			return err
		}
		before, err := marshalFields(&doc)
		if err != nil {
			return err
		}
		if err := fn(c, &doc); err != nil {
			return err
		}
		after, err := marshalFields(&doc)
		if err != nil {
			return err
		}
		return writeBack(c.Doc, before, after)
	}
}

func marshalFields(v any) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("content: encode typed document: %w", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("content: typed document is not an object: %w", err)
	}
	return m, nil
}

func writeBack(doc map[string]any, before, after map[string]json.RawMessage) error {
	if doc == nil {
		return nil
	}
	for k := range before {
		if _, ok := after[k]; !ok {
			delete(doc, k)
		}
	}
	for k, raw := range after {
		if bytes.Equal(raw, before[k]) {
			continue
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("content: decode field %q: %w", k, err)
		}
		doc[k] = v
	}
	return nil
}

func fromMap(m map[string]any, out any) error {
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("content: encode document: %w", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("content: convert document: %w", err)
	}
	return nil
}

func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("content: encode typed document: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("content: typed document is not an object: %w", err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func (h Handle[T]) item(v *View) (*Item[T], error) {
	if v.Type != h.Type {
		return nil, badRequest("item %s is a %q, not a %q", v.ID, v.Type, h.Type)
	}
	var doc T
	if err := fromMap(v.Fields, &doc); err != nil {
		return nil, err
	}
	meta := *v
	meta.Fields = nil
	return &Item[T]{Meta: meta, Doc: doc}, nil
}

func (h Handle[T]) Get(ctx context.Context, s *Service, id ids.UUID, o GetOptions) (*Item[T], error) {
	v, err := s.Get(ctx, id, o)
	if err != nil {
		return nil, err
	}
	return h.item(v)
}

func (h Handle[T]) Create(ctx context.Context, s *Service, locale string, doc *T) (*Item[T], error) {
	fields, err := toMap(doc)
	if err != nil {
		return nil, err
	}
	v, err := s.Create(ctx, CreateInput{Type: h.Type, Locale: locale, Fields: fields})
	if err != nil {
		return nil, err
	}
	return h.item(v)
}

// Update writes every field of doc into the draft for locale (zero-valued pointer fields are removed).
func (h Handle[T]) Update(ctx context.Context, s *Service, id ids.UUID, locale string, doc *T) (*Item[T], error) {
	t, err := s.typeOf(h.Type)
	if err != nil {
		return nil, err
	}
	patch, err := toMap(doc)
	if err != nil {
		return nil, err
	}
	for _, f := range t.Fields {
		if _, ok := patch[f.Name]; !ok {
			patch[f.Name] = nil
		}
	}
	v, err := s.Update(ctx, id, UpdateInput{Locale: locale, Fields: patch})
	if err != nil {
		return nil, err
	}
	return h.item(v)
}

func (h Handle[T]) Publish(ctx context.Context, s *Service, id ids.UUID, o PublishOptions) (*Item[T], error) {
	v, err := s.Publish(ctx, id, o)
	if err != nil {
		return nil, err
	}
	return h.item(v)
}

func (h Handle[T]) List(ctx context.Context, s *Service, o ListOptions) (*ListResult, error) {
	o.Type = h.Type
	return s.List(ctx, o)
}
