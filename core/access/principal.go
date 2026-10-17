package access

import (
	"context"

	"github.com/hydrogencms/hydrogen/core/audit"
)

type principalKey struct{}

// WithPrincipal makes p the acting principal of ctx and its Actor the audit actor, so the two cannot differ.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return audit.WithActor(context.WithValue(ctx, principalKey{}, p), p.Actor())
}

// PrincipalFrom returns the acting principal, or nil for the system.
func PrincipalFrom(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}

// Actor is how the principal appears in audit entries and created_by columns.
func (p *Principal) Actor() audit.Actor {
	switch {
	case p == nil || p.Kind == KindSystem:
		return audit.System
	case p.Super && p.ID.IsZero():
		return audit.Actor{Kind: string(KindToken), ID: "config", Detail: p.Detail}
	}
	return audit.Actor{Kind: string(p.Kind), ID: p.ID.String(), Detail: p.Detail}
}
