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
	// Detail is recorded under "actor" in the entry's data (an agent's model and task, a login's address).
	Detail map[string]string
}

var System = Actor{Kind: "system"}

func (a Actor) String() string {
	if a.ID == "" {
		return a.Kind
	}
	return a.Kind + ":" + a.ID
}

type actorKey struct{}

// WithActor attaches the acting user, agent or system to ctx; services record it in audit entries.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, a)
}

// ActorFrom returns the actor in ctx, or System.
func ActorFrom(ctx context.Context) Actor {
	if a, ok := ctx.Value(actorKey{}).(Actor); ok {
		return a
	}
	return System
}

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
	data, err := encodeData(e)
	if err != nil {
		return err
	}
	_, err = tx.Audit().Write(ctx, store.AuditEvent{At: l.clock.Now(), ActorKind: e.Actor.Kind, ActorID: e.Actor.ID,
		Action: e.Action, Subject: e.Subject, Data: data})
	return err
}

// encodeData encodes the entry's data as JSON. With actor detail, an object gets an "actor" key and any other
// value is wrapped as {"data": …, "actor": …}; encoding/json sorts map keys, so the bytes are deterministic.
func encodeData(e Entry) ([]byte, error) {
	var data []byte
	if e.Data != nil {
		var err error
		if data, err = json.Marshal(e.Data); err != nil {
			return nil, fmt.Errorf("audit: encode data: %w", err)
		}
	}
	if len(e.Actor.Detail) == 0 {
		return data, nil
	}
	m := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &m); err != nil || m == nil {
			m = map[string]json.RawMessage{"data": data}
		}
	}
	detail, err := json.Marshal(e.Actor.Detail)
	if err != nil {
		return nil, fmt.Errorf("audit: encode actor detail: %w", err)
	}
	m["actor"] = detail
	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("audit: encode data: %w", err)
	}
	return out, nil
}
