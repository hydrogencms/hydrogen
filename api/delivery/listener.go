package delivery

import (
	"context"
	"strings"

	"github.com/hydrogencms/hydrogen/core/cachetag"
)

// Start subscribes to preview (relay) notifications and cache purges and applies them until ctx ends. It returns once the subscription is
// active, so a purge sent after Start returns is not missed. An empty-payload notification (sent after a
// listener reconnects, when messages may have been lost) clears the whole cache.
func (a *API) Start(ctx context.Context) error {
	if err := a.startEvents(ctx); err != nil {
		return err
	}
	if a.cache == nil || a.o.Notifier == nil {
		return nil
	}
	ch, err := a.o.Notifier.Listen(ctx, cachetag.PurgeChannel)
	if err != nil {
		return err
	}
	go func() {
		for n := range ch {
			tags := strings.Fields(n.Payload)
			if len(tags) == 0 {
				a.cache.clear()
				continue
			}
			a.cache.purge(tags...)
		}
	}()
	return nil
}
