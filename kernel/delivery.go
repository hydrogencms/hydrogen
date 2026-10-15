package kernel

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/delivery"
)

// DeliveryRoute adds a read endpoint to the delivery API under /api/deliver/v1 (GET only). The handler runs
// after channel selection and authentication and may use the delivery helpers (Channel, Locale, Tag, Items,
// Fail, WriteJSON). Successful responses are cached and get ETag and cache headers.
func (r *Registry) DeliveryRoute(rt delivery.Route) {
	rt.Module = r.current
	r.deliveryRts = append(r.deliveryRts, registered[delivery.Route]{r.current, rt})
}

// Decorator adds keys to the items of delivery responses (one batched call per response).
func (r *Registry) Decorator(d delivery.Decorator) {
	r.decorators = append(r.decorators, registered[delivery.Decorator]{r.current, d})
}

// PublicChannelOption shows a channel option (registered with ChannelOption) in GET /channels/{name} of the
// delivery API. Its decoded value must be JSON-encodable and safe to publish.
func (r *Registry) PublicChannelOption(name string) {
	r.publicOpts = append(r.publicOpts, registered[string]{r.current, name})
}

// DeliveryVary lists extra request header names (beyond Authorization) a channel's delivery responses depend
// on; they are sent as Vary and are part of the in-process cache key.
func (r *Registry) DeliveryVary(v delivery.Varier) {
	r.varies = append(r.varies, v)
}

// checkPublicOptions rejects public channel options no module registered.
func (r *Registry) checkPublicOptions() error {
	known := map[string]bool{}
	for _, op := range r.chanOpts {
		known[op.value.Name] = true
	}
	var errs []error
	for _, p := range r.publicOpts {
		if !known[p.value] {
			errs = append(errs, fmt.Errorf("module %s: public channel option %q is not a registered channel option", p.module, p.value))
		}
	}
	return errors.Join(errs...)
}

// previewSecret returns the key preview tokens are signed with. delivery.preview_secret must be at least 32
// characters when set, and is required outside dev; in dev an unset secret derives from manage.token (which is
// "dev-token" when unset), so a dev cluster sharing a token agrees on previews without more configuration.
func previewSecret(s *Services) ([]byte, error) {
	secret := s.Core.Delivery.PreviewSecret
	switch {
	case secret == "" && s.Config.Env() == config.DevEnv:
		sum := sha256.Sum256([]byte("hydrogen preview\x00" + s.Core.Manage.Token))
		return sum[:], nil
	case secret == "":
		return nil, fmt.Errorf("delivery.preview_secret is required outside the %s environment (at least %d characters, e.g. ${env:PREVIEW_SECRET})",
			config.DevEnv, delivery.MinPreviewSecret)
	case len(secret) < delivery.MinPreviewSecret:
		return nil, fmt.Errorf("delivery.preview_secret must be at least %d characters (got %d)", delivery.MinPreviewSecret, len(secret))
	}
	return []byte(secret), nil
}

func newDelivery(s *Services, reg *Registry) (*delivery.API, error) {
	secret, err := previewSecret(s)
	if err != nil {
		return nil, err
	}
	o := delivery.Options{Content: s.Content, Schema: s.Schema, Channels: s.Channels, Reader: s.DB.Reader(),
		Notifier: s.DB.Notifier(), Clock: s.Clock, Log: s.Log, CacheBytes: s.Core.Delivery.CacheBytes,
		PreviewSecret: secret, Varies: reg.varies}
	for _, rt := range reg.deliveryRts {
		o.Routes = append(o.Routes, rt.value)
	}
	for _, d := range reg.decorators {
		o.Decorators = append(o.Decorators, d.value)
	}
	for _, p := range reg.publicOpts {
		o.PublicOptions = append(o.PublicOptions, p.value)
	}
	api, err := delivery.New(o)
	if err != nil {
		return nil, fmt.Errorf("delivery: %w", err)
	}
	return api, nil
}
