package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// AsyncHookConsumers returns one outbox consumer per async hook, named "hook:"+Name. AfterPublish hooks
// listen to item.published and run once per published locale with the published fields as Doc;
// AfterDelete hooks listen to item.deleted. Tx is nil, events of other types are skipped, and an item that
// no longer exists is skipped too, so handlers are idempotent. svc is resolved on delivery because the
// service is built after the consumers are registered.
func AsyncHookConsumers(hs []hooks.Hook, svc func() *Service) []outbox.Consumer {
	var out []outbox.Consumer
	for _, h := range hs {
		if !h.Async {
			continue
		}
		topic := "item.published"
		if h.Event == hooks.AfterDelete {
			topic = "item.deleted"
		}
		out = append(out, outbox.Consumer{
			Name:   "hook:" + h.Name,
			Topics: []string{topic},
			Handle: func(ctx context.Context, e outbox.Event) error { return deliver(ctx, h, svc(), e) },
		})
	}
	return out
}

func deliver(ctx context.Context, h hooks.Hook, s *Service, e outbox.Event) error {
	var p eventPayload
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("content: hook %s: bad %s payload: %w", h.Name, e.Topic, err)
	}
	if h.Type != "" && h.Type != p.Type {
		return nil
	}
	item := hooks.Item{ID: p.ID, Type: p.Type, Version: p.Version}
	if h.Event == hooks.AfterDelete {
		return h.Fn(&hooks.Context{Context: ctx, Event: h.Event, Item: item, Errors: &schema.Report{}})
	}
	for _, locale := range p.Locales {
		v, err := s.Get(ctx, p.ID, GetOptions{Locale: locale})
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		item.Key, item.Version, item.MainLocale = v.Key, v.Version, v.MainLocale
		hc := &hooks.Context{Context: ctx, Event: h.Event, Item: item, Locale: locale, Doc: v.Fields, Errors: &schema.Report{}}
		if err := h.Fn(hc); err != nil {
			return fmt.Errorf("content: hook %s (%s): %w", h.Name, locale, err)
		}
	}
	return nil
}
