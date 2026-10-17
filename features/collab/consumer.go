package collab

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/store"
)

// ConsumerName is the outbox consumer that turns content events into collaboration notifications.
const ConsumerName = "collab"

// Topics are the content events the consumer handles. item.restored is not among them: trashing dropped the
// session and a trashed item cannot be joined, so no session exists when it is restored (a later join compares
// the draft state it finds with the session's, as always).
var Topics = []string{"item.updated", "item.published", "item.unpublished", "item.trashed", "item.deleted"}

type contentEvent struct {
	ID           ids.UUID `json:"id"`
	CollabClient string   `json:"collabClient"`
	CollabActor  string   `json:"collabActor"`
	CollabSeq    int64    `json:"collabSeq"`
	Discard      bool     `json:"discard"`
	State        string   `json:"state"`
}

// Consumer turns the content events of items into notifications for their sessions: a participant's draft save
// is "saved" (when the client named by the write belongs to the writing principal), any other change of the
// draft (a publish, unpublish, discard, API write) is "reset external" and drops the log, and a trash or purge
// is "reset trashed". Events carry the working state the write produced, so one that the session already
// includes (a late or repeated delivery) does nothing. It is idempotent.
func (s *Service) Consumer() outbox.Consumer {
	return outbox.Consumer{
		Name:   ConsumerName,
		Topics: Topics,
		Handle: func(ctx context.Context, e outbox.Event) error {
			var p contentEvent
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return fmt.Errorf("collab: bad %s payload: %w", e.Topic, err)
			}
			switch e.Topic {
			case "item.trashed", "item.deleted":
				return s.resetSession(ctx, p.ID, "trashed", true, "")
			case "item.updated":
				if p.CollabClient != "" && !p.Discard {
					return s.saved(ctx, p)
				}
			}
			return s.resetSession(ctx, p.ID, "external", false, p.State)
		},
	}
}

// resetSession drops the item's log and tells its streams. Unless always, nothing happens for an item without
// a session, nor when state (the working state of the write behind the reset, if known) is not later than the
// state the session was seeded from or last saved.
func (s *Service) resetSession(ctx context.Context, item ids.UUID, reason string, always bool, state string) error {
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		if !always {
			sess, err := c.GetSession(ctx, item)
			has := err == nil
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if has && state != "" && !newer(state, sess.Etag) {
				return nil
			}
			clients, err := c.Clients(ctx, item)
			if err != nil {
				return err
			}
			n, _, err := c.LogSize(ctx, item)
			if err != nil {
				return err
			}
			parts, err := c.Parts(ctx, item)
			if err != nil {
				return err
			}
			if !has && len(clients) == 0 && n == 0 && len(parts) == 0 {
				return nil
			}
		}
		if err := c.DeleteSession(ctx, item); err != nil {
			return err
		}
		return notify(tx, item, "reset", reason)
	})
}

// saved announces a participant's save, or resets the session when the client is not a member of it or does not
// belong to the principal that wrote.
func (s *Service) saved(ctx context.Context, p contentEvent) error {
	member := true
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if err := c.LockSession(ctx, p.ID); err != nil {
			return err
		}
		state := p.State
		if state == "" {
			it, err := tx.Content().GetItem(ctx, p.ID, false)
			if errors.Is(err, store.ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			state = content.StateOf(it.UpdatedAt)
		}
		cl, err := c.GetClient(ctx, p.CollabClient)
		if errors.Is(err, store.ErrNotFound) || err == nil && (cl.Item != p.ID || cl.Principal != p.CollabActor) {
			member = false
			return nil
		}
		if err != nil {
			return err
		}
		sess, err := c.GetSession(ctx, p.ID)
		if err == nil && newer(state, sess.Etag) {
			sess.Etag = state
			err = c.UpdateSession(ctx, sess)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		return notify(tx, p.ID, "saved", p.CollabClient, `"`+state+`"`, fmt.Sprint(max(p.CollabSeq, 0)))
	})
	if err == nil && !member {
		return s.resetSession(ctx, p.ID, "external", false, p.State)
	}
	return err
}
