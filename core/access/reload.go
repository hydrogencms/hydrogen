package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// PrincipalOf rebuilds the principal behind an actor string ("user:<id>", "token:<id>", "agent:<id>",
// "token:config" or "system") the way a request would be authenticated now: a user must still exist and be
// enabled, with their current groups; a token must exist, be unrevoked and unexpired. Anything gone is
// ErrUnauthenticated. "system" yields nil, the unchecked system. The config token yields its super principal.
// Agent model and task details are request headers and are not restored.
func (a *Authenticator) PrincipalOf(ctx context.Context, actor string) (*Principal, error) {
	kind, rest, _ := strings.Cut(actor, ":")
	if actor == "system" {
		return nil, nil
	}
	if kind == string(KindToken) && rest == "config" {
		return &Principal{Kind: KindToken, Name: "config", Super: true}, nil
	}
	if a.db == nil {
		return nil, ErrUnauthenticated
	}
	id, err := ids.Parse(rest)
	if err != nil {
		return nil, fmt.Errorf("%w: unknown actor %q", ErrUnauthenticated, actor)
	}
	var p *Principal
	err = a.db.View(ctx, func(tx store.Tx) error {
		switch Kind(kind) {
		case KindUser:
			u, err := tx.Auth().GetUser(ctx, id)
			if err != nil {
				return err
			}
			if u.Disabled {
				return ErrUnauthenticated
			}
			groups, err := tx.Auth().GroupsOf(ctx, id)
			if err != nil {
				return err
			}
			p = &Principal{Kind: KindUser, ID: u.ID, Name: u.Login, Groups: groups}
		case KindToken, KindAgent:
			t, err := tx.Auth().GetToken(ctx, id)
			if err != nil {
				return err
			}
			if t.RevokedAt != nil || (t.ExpiresAt != nil && !a.clock.Now().Before(*t.ExpiresAt)) {
				return ErrUnauthenticated
			}
			p = &Principal{Kind: KindToken, ID: t.ID, Name: t.Name}
			if t.Kind == "agent" {
				p.Kind = KindAgent
			}
		default:
			return fmt.Errorf("%w: unknown actor %q", ErrUnauthenticated, actor)
		}
		return nil
	})
	if errors.Is(err, store.ErrNotFound) {
		return nil, ErrUnauthenticated
	}
	return p, err
}
