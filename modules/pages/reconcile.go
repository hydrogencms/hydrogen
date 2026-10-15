package pages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

// reconcile is the outbox consumer behind the hooks. Publishes that ran no hooks (content migrations,
// nullified references) and failed deliveries leave slugs, memberships and routes behind the content; this
// brings them back from the item's current published version. It is idempotent: when the hooks already did
// the work, it finds nothing to change.
func (s *service) reconcile(ctx context.Context, e outbox.Event) error {
	var p struct {
		ID   ids.UUID `json:"id"`
		Type string   `json:"type"`
	}
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		return fmt.Errorf("pages: bad %s payload: %w", e.Topic, err)
	}
	class := s.classOf(p.Type)
	if class == classCollection {
		if _, known := s.schema.Type(p.Type); !known {
			return nil
		}
		set, err := s.mountsOfRead(ctx, p.Type)
		if err != nil || len(set.rows) == 0 {
			return err
		}
	}
	return s.db.Tx(ctx, func(tx store.Tx) error {
		q := tx.SQL()
		if err := s.lockItems(ctx, tx, []ids.UUID{p.ID}); err != nil {
			return err
		}
		it, err := tx.Content().GetItem(ctx, p.ID, false)
		if e.Topic == "item.deleted" || errors.Is(err, store.ErrNotFound) {
			changes, err := s.removeItem(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			return s.emitChanges(ctx, tx, changes)
		}
		if err != nil {
			return fmt.Errorf("pages: read item %s: %w", p.ID, err)
		}
		docs, err := publishedDocs(ctx, tx.Content(), it)
		if err != nil {
			return err
		}
		var changes []change
		for _, d := range docs {
			var cs []change
			switch class {
			case classPage:
				cs, err = s.syncPage(ctx, tx, it.ID, it.MainLocale, d.Locale, s.slugRowOf(it.Type, d.Doc))
			case classRedirect:
				if d.Locale == it.MainLocale {
					cs, err = s.syncRedirect(ctx, q, it.ID, d.Doc)
				}
			default:
				cs, err = s.syncMountedItem(ctx, tx, it.ID, it.Type, d.Locale, d.Doc)
			}
			if err != nil {
				return fmt.Errorf("pages: reconcile %s (%s): %w", it.ID, d.Locale, err)
			}
			changes = append(changes, cs...)
		}
		return s.emitChanges(ctx, tx, changes)
	})
}

// mountsOfRead is mountsOf outside a transaction.
func (s *service) mountsOfRead(ctx context.Context, collection string) (*mountSet, error) {
	return s.mountsOf(ctx, s.db.Read(), collection)
}
