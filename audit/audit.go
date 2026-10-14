// Package audit records who changed what, in the same transaction as the change.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/store"
)

type Actor struct {
	Kind string // user, agent, system
	ID   string
}

var System = Actor{Kind: "system"}

type Entry struct {
	Actor   Actor
	Action  string // e.g. "item.publish"
	Subject string // e.g. an item id
	Data    any    // encoded as JSON
}

type Log struct {
	clock clock.Clock
}

func New(c clock.Clock) *Log { return &Log{clock: c} }

func (l *Log) Write(ctx context.Context, tx store.Tx, e Entry) error {
	if e.Actor.Kind == "" || e.Action == "" {
		return fmt.Errorf("audit: actor kind and action are required")
	}
	var data []byte
	if e.Data != nil {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return fmt.Errorf("audit: encode data: %w", err)
		}
	}
	_, err := tx.Audit().Write(ctx, store.AuditEvent{At: l.clock.Now(), ActorKind: e.Actor.Kind, ActorID: e.Actor.ID,
		Action: e.Action, Subject: e.Subject, Data: data})
	return err
}
