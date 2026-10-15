package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// Token is an API token as admins see it: never the secret or its hash.
type Token struct {
	ID         ids.UUID   `json:"id"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	Prefix     string     `json:"prefix"`
	CreatedBy  string     `json:"createdBy"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
}

func tokenView(t store.Token) Token {
	return Token{ID: t.ID, Name: t.Name, Kind: t.Kind, Prefix: t.Prefix, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
}

// TokenInput creates a token. Kind is "token" (default) or "agent"; Roles are role ids assigned without
// assignment limitations.
type TokenInput struct {
	Name      string
	Kind      string
	Roles     []ids.UUID
	ExpiresAt *time.Time
}

// CreateToken returns the new token and its secret, which is never retrievable again. Creating a token with
// roles needs the same rights as assigning them.
func (a *Admin) CreateToken(ctx context.Context, in TokenInput) (Token, string, error) {
	if err := a.can(ctx, PolicyTokenManage); err != nil {
		return Token{}, "", err
	}
	name := strings.TrimSpace(in.Name)
	if err := checkName("name", name); err != nil {
		return Token{}, "", err
	}
	kind := in.Kind
	if kind == "" {
		kind = "token"
	}
	if kind != "token" && kind != "agent" {
		return Token{}, "", invalid("kind", "must be token or agent")
	}
	now := a.clock.Now()
	if in.ExpiresAt != nil && !in.ExpiresAt.After(now) {
		return Token{}, "", invalid("expiresAt", "must be in the future")
	}
	var roles []store.Role
	if len(in.Roles) > 0 {
		if err := a.can(ctx, PolicyRoleManage); err != nil {
			return Token{}, "", err
		}
		var need []string
		err := a.view(ctx, func(s store.AuthStore) error {
			for _, id := range in.Roles {
				r, err := s.GetRole(ctx, id)
				if errors.Is(err, store.ErrNotFound) {
					return invalid("roles", "role "+id.String()+" not found")
				}
				if err != nil {
					return err
				}
				names, err := rolePolicyNames(r.Policies)
				if err != nil {
					return err
				}
				need = append(need, names...)
				roles = append(roles, r)
			}
			return nil
		})
		if err != nil {
			return Token{}, "", err
		}
		if err := a.requireHeld(ctx, need); err != nil {
			return Token{}, "", err
		}
	}
	secret, hash, prefix, err := NewSecret(a.rand)
	if err != nil {
		return Token{}, "", err
	}
	t := store.Token{ID: a.ids.New(), Name: name, Kind: kind, SecretHash: hash, Prefix: prefix,
		CreatedBy: actorOf(ctx).String(), CreatedAt: now, ExpiresAt: in.ExpiresAt}
	err = a.mutate(ctx, "token.create", t.ID.String(), func(tx store.Tx) (any, error) {
		if err := tx.Auth().CreateToken(ctx, t); err != nil {
			return nil, err
		}
		var names []string
		for _, r := range roles {
			if err := roleUnchanged(ctx, tx, r); err != nil {
				return nil, err
			}
			as := store.Assignment{ID: a.ids.New(), RoleID: r.ID, Principal: store.PrincipalRef{Kind: store.PrincipalToken, ID: t.ID},
				Limitations: []byte("{}"), CreatedAt: now}
			if err := tx.Auth().CreateAssignment(ctx, as); err != nil {
				return nil, err
			}
			names = append(names, r.Name)
		}
		return map[string]any{"name": name, "kind": kind, "prefix": prefix, "roles": names, "expiresAt": in.ExpiresAt}, nil
	})
	if err != nil {
		return Token{}, "", err
	}
	return tokenView(t), secret, nil
}

func (a *Admin) tokenAction(ctx context.Context, id ids.UUID, action string, fn func(store.Tx, store.Token) error) error {
	if err := a.can(ctx, PolicyTokenManage); err != nil {
		return err
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, action, id.String(), func(tx store.Tx) (any, error) {
		t, err := tx.Auth().GetToken(ctx, id)
		if err != nil {
			return nil, err
		}
		pols, err := policiesIn(ctx, tx.Auth(), store.PrincipalRef{Kind: store.PrincipalToken, ID: id})
		if err != nil {
			return nil, err
		}
		if err := h.require(pols); err != nil {
			return nil, err
		}
		return map[string]any{"name": t.Name, "prefix": t.Prefix}, fn(tx, t)
	})
}

// RevokeToken stops a token from authenticating, keeping its record.
func (a *Admin) RevokeToken(ctx context.Context, id ids.UUID) error {
	return a.tokenAction(ctx, id, "token.revoke", func(tx store.Tx, _ store.Token) error {
		return tx.Auth().RevokeToken(ctx, id, a.clock.Now())
	})
}

func (a *Admin) DeleteToken(ctx context.Context, id ids.UUID) error {
	return a.tokenAction(ctx, id, "token.delete", func(tx store.Tx, _ store.Token) error {
		return tx.Auth().DeleteToken(ctx, id)
	})
}

func (a *Admin) GetToken(ctx context.Context, id ids.UUID) (Token, error) {
	if err := a.can(ctx, PolicyTokenManage); err != nil {
		return Token{}, err
	}
	var t store.Token
	err := a.view(ctx, func(s store.AuthStore) (err error) { t, err = s.GetToken(ctx, id); return })
	return tokenView(t), err
}

func (a *Admin) ListTokens(ctx context.Context, p store.Page) ([]Token, int, error) {
	if err := a.can(ctx, PolicyTokenManage); err != nil {
		return nil, 0, err
	}
	var ts []store.Token
	var total int
	err := a.view(ctx, func(s store.AuthStore) (err error) { ts, total, err = s.ListTokens(ctx, page(p)); return })
	out := make([]Token, len(ts))
	for i, t := range ts {
		out[i] = tokenView(t)
	}
	return out, total, err
}

const metaTemplatesSeeded = "templates_seeded"

// SeedTemplates creates the role templates once (tracked in auth meta). Existing roles of the same name are left
// alone and later edits or deletions of seeded roles are never undone. It is a system operation without
// authorization checks, meant for startup.
func (a *Admin) SeedTemplates(ctx context.Context) error {
	seeded := func() (bool, error) {
		var v string
		err := a.view(ctx, func(s store.AuthStore) (err error) { v, err = s.Meta(ctx, metaTemplatesSeeded); return })
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		return v != "", err
	}
	if ok, err := seeded(); ok || err != nil {
		return err
	}
	err := a.mutateAs(ctx, audit.System, "role.seed", "", func(tx store.Tx) (any, error) {
		if v, err := tx.Auth().Meta(ctx, metaTemplatesSeeded); err == nil && v != "" {
			return nil, errSeeded
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		now := a.clock.Now()
		var names []string
		for _, tpl := range Templates() {
			if _, err := tx.Auth().RoleByName(ctx, tpl.Name); err == nil {
				continue
			} else if !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			r := store.Role{ID: a.ids.New(), Name: tpl.Name, Description: tpl.Description, Policies: encodePolicies(tpl.Policies),
				Template: tpl.Name, CreatedAt: now, UpdatedAt: now}
			if err := tx.Auth().CreateRole(ctx, r); err != nil {
				return nil, err
			}
			names = append(names, tpl.Name)
		}
		return map[string]any{"roles": names}, tx.Auth().SetMeta(ctx, metaTemplatesSeeded, now.Format(time.RFC3339))
	})
	if errors.Is(err, errSeeded) {
		return nil
	}
	if err != nil {
		// a concurrent node may have won the race
		if ok, _ := seeded(); ok {
			return nil
		}
	}
	return err
}

var errSeeded = errors.New("access: templates already seeded")

// EnsureAdmin creates or updates the user login with the password and gives it the administrator role (the
// users:create-admin command). It is a system operation: the caller is the operator with database access. The
// user is enabled, and an existing user's sessions end.
func (a *Admin) EnsureAdmin(ctx context.Context, login, password string) (User, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if err := checkLogin(login); err != nil {
		return User{}, err
	}
	if err := checkPassword("password", password); err != nil {
		return User{}, err
	}
	if err := a.SeedTemplates(ctx); err != nil {
		return User{}, err
	}
	hash, err := HashPasswordWith(a.rand, password, a.iter)
	if err != nil {
		return User{}, err
	}
	action := "user.create"
	if err := a.view(ctx, func(s store.AuthStore) error { _, err := s.UserByLogin(ctx, login); return err }); err == nil {
		action = "user.update"
	}
	var out store.User
	err = a.mutateAs(ctx, audit.System, action, login, func(tx store.Tx) (any, error) {
		role, err := tx.Auth().RoleByName(ctx, "administrator")
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("access: the administrator role does not exist; create it, then run this again")
		}
		if err != nil {
			return nil, err
		}
		now := a.clock.Now()
		created := false
		u, err := tx.Auth().UserByLogin(ctx, login)
		switch {
		case errors.Is(err, store.ErrNotFound):
			created = true
			u = store.User{ID: a.ids.New(), Login: login, Name: login, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
			err = tx.Auth().CreateUser(ctx, u)
		case err == nil:
			u.PasswordHash, u.Disabled, u.UpdatedAt = hash, false, now
			if err = tx.Auth().UpdateUser(ctx, u); err == nil {
				err = tx.Auth().DeleteUserSessions(ctx, u.ID)
			}
		}
		if err != nil {
			return nil, err
		}
		out = u
		ref := store.PrincipalRef{Kind: store.PrincipalUser, ID: u.ID}
		gs, err := tx.Auth().AssignmentsFor(ctx, []store.PrincipalRef{ref})
		if err != nil {
			return nil, err
		}
		assigned := false
		for _, g := range gs {
			assigned = assigned || (g.Role.ID == role.ID && string(g.Assignment.Limitations) == "{}")
		}
		if !assigned {
			as := store.Assignment{ID: a.ids.New(), RoleID: role.ID, Principal: ref, Limitations: []byte("{}"), CreatedAt: now}
			if err := tx.Auth().CreateAssignment(ctx, as); err != nil {
				return nil, err
			}
		}
		return map[string]any{"login": login, "created": created}, nil
	})
	if err != nil {
		return User{}, loginTaken(err)
	}
	return userView(out), nil
}
