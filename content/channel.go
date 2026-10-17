package content

import (
	"context"
	"fmt"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// ChannelGetOptions configures GetForChannel.
type ChannelGetOptions struct {
	// Draft overlays draft parts; draft-only locales then count as available too.
	Draft bool
}

// GetForChannel returns the view of an item as a channel shows it: the locale is the first of the channel's
// languages the item has (else its main locale when it is always available), and View.Locale holds it, so a
// caller sees a fallback by comparing it with the channel's first language. ErrNotFound when the channel doesn't
// allow the item's collection, the item isn't published (without Draft), or no locale is available. All three
// give the same error text, so a caller cannot tell from it which items exist.
func (s *Service) GetForChannel(ctx context.Context, id ids.UUID, ch *channel.Channel, o ChannelGetOptions) (*View, error) {
	if ch == nil {
		return nil, badRequest("a channel is required")
	}
	var v *View
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		it, err := getItem(ctx, tx, id, false)
		if err != nil {
			return err
		}
		unavailable := fmt.Errorf("%w: %s is not available on channel %s", ErrNotFound, id, ch.Name)
		if !ch.AllowsCollection(it.Type) || it.PublishedVersion == 0 && !o.Draft {
			return unavailable
		}
		st, err := s.newState(it)
		if err != nil {
			return err
		}
		if err := st.load(ctx, tx.Content()); err != nil {
			return err
		}
		locale, _, ok := channel.PickLocale(ch, st.availableLocales(o.Draft), it.MainLocale, it.AlwaysAvailable)
		if !ok {
			return unavailable
		}
		v, err = st.view(locale, o.Draft)
		return err
	})
	return v, err
}
