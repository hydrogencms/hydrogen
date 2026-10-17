package access

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/store"
)

// AuditFilter narrows AuditLog. Actor is a kind ("agent") or "kind:id" ("user:0197…").
type AuditFilter struct {
	Actor   string
	Action  string
	Subject string
	Since   time.Time
	Before  int64 // only events with a smaller id
	Limit   int
	Offset  int
}

// AuditEntry is an audit event as the API shows it.
type AuditEntry struct {
	ID        int64           `json:"id"`
	At        time.Time       `json:"at"`
	ActorKind string          `json:"actorKind"`
	ActorID   string          `json:"actorId,omitempty"`
	Action    string          `json:"action"`
	Subject   string          `json:"subject,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// AuditLog lists audit events newest first; hasMore says a further page exists. It needs audit.read.
func (a *Admin) AuditLog(ctx context.Context, f AuditFilter) (entries []AuditEntry, hasMore bool, err error) {
	if err := a.can(ctx, PolicyAuditRead); err != nil {
		return nil, false, err
	}
	p := page(store.Page{Limit: f.Limit, Offset: f.Offset})
	q := store.AuditQuery{Action: f.Action, Subject: f.Subject, Since: f.Since, Before: f.Before, Limit: p.Limit + 1, Offset: p.Offset}
	q.ActorKind, q.ActorID, _ = strings.Cut(f.Actor, ":")
	var evs []store.AuditEvent
	err = a.db.View(ctx, func(tx store.Tx) (err error) { evs, err = tx.Audit().List(ctx, q); return })
	if err != nil {
		return nil, false, err
	}
	if hasMore = len(evs) > p.Limit; hasMore {
		evs = evs[:p.Limit]
	}
	entries = make([]AuditEntry, len(evs))
	for i, e := range evs {
		entries[i] = AuditEntry{ID: e.ID, At: e.At, ActorKind: e.ActorKind, ActorID: e.ActorID, Action: e.Action,
			Subject: e.Subject, Data: e.Data}
	}
	return entries, hasMore, nil
}
