package access

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// User is a user as admins see it: never the password hash.
type User struct {
	ID          ids.UUID   `json:"id"`
	Login       string     `json:"login"`
	Email       string     `json:"email"`
	Name        string     `json:"name"`
	Disabled    bool       `json:"disabled"`
	HasPassword bool       `json:"hasPassword"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

// ViewUser is the user without secrets.
func ViewUser(u store.User) User { return userView(u) }

func userView(u store.User) User {
	return User{ID: u.ID, Login: u.Login, Email: u.Email, Name: u.Name, Disabled: u.Disabled,
		HasPassword: u.PasswordHash != "", CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt, LastLoginAt: u.LastLoginAt}
}

type UserInput struct {
	Login    string
	Name     string // default: the login
	Email    string
	Password string // optional: without one the user cannot log in
}

type UserPatch struct {
	Name     *string
	Email    *string
	Disabled *bool
}

func (a *Admin) CreateUser(ctx context.Context, in UserInput) (User, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return User{}, err
	}
	login := strings.ToLower(strings.TrimSpace(in.Login))
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = login
	}
	for _, err := range []error{checkLogin(login), checkName("name", name), checkEmail(in.Email)} {
		if err != nil {
			return User{}, err
		}
	}
	hash := ""
	if in.Password != "" {
		if err := checkPassword("password", in.Password); err != nil {
			return User{}, err
		}
		var err error
		if hash, err = HashPasswordWith(a.rand, in.Password, a.iter); err != nil {
			return User{}, err
		}
	}
	now := a.clock.Now()
	u := store.User{ID: a.ids.New(), Login: login, Email: in.Email, Name: name, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	err := a.mutate(ctx, "user.create", u.ID.String(), func(tx store.Tx) (any, error) {
		return map[string]any{"login": login, "password": hash != ""}, tx.Auth().CreateUser(ctx, u)
	})
	if err != nil {
		return User{}, loginTaken(err)
	}
	return userView(u), nil
}

// loginTaken maps mutate's generic duplicate error to the login field.
func loginTaken(err error) error {
	var ie *InvalidError
	if errors.As(err, &ie) && ie.Fields["name"] == dupMessage {
		return invalid("login", "already taken")
	}
	return err
}

func (a *Admin) UpdateUser(ctx context.Context, id ids.UUID, p UserPatch) (User, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return User{}, err
	}
	self := isSelf(ctx, id)
	if p.Disabled != nil && self {
		return User{}, invalid("disabled", "you cannot change your own account state")
	}
	h, err := a.holder(ctx)
	if err != nil {
		return User{}, err
	}
	var out store.User
	err = a.mutate(ctx, "user.update", id.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		if !self {
			if err := h.requireOutranks(ctx, as, id); err != nil {
				return nil, err
			}
		}
		u, err := as.GetUser(ctx, id)
		if err != nil {
			return nil, err
		}
		var fields []string
		if p.Name != nil {
			if err := checkName("name", *p.Name); err != nil {
				return nil, err
			}
			u.Name = strings.TrimSpace(*p.Name)
			fields = append(fields, "name")
		}
		if p.Email != nil {
			if err := checkEmail(*p.Email); err != nil {
				return nil, err
			}
			u.Email = *p.Email
			fields = append(fields, "email")
		}
		drop := false
		if p.Disabled != nil && *p.Disabled != u.Disabled {
			u.Disabled = *p.Disabled
			drop = u.Disabled
			fields = append(fields, "disabled")
		}
		u.UpdatedAt = a.clock.Now()
		save := func() error {
			if err := as.UpdateUser(ctx, u); err != nil {
				return err
			}
			if drop {
				return as.DeleteUserSessions(ctx, id)
			}
			return nil
		}
		if drop {
			err = keepAdmin(ctx, as, save)
		} else {
			err = save()
		}
		if err != nil {
			return nil, err
		}
		out = u
		return map[string]any{"login": u.Login, "fields": fields}, nil
	})
	if err != nil {
		return User{}, err
	}
	return userView(out), nil
}

func (a *Admin) ResetPassword(ctx context.Context, id ids.UUID, password string) error {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return err
	}
	if isSelf(ctx, id) {
		return invalid("password", "change your own password with your current password")
	}
	if err := checkPassword("password", password); err != nil {
		return err
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	hash, err := HashPasswordWith(a.rand, password, a.iter)
	if err != nil {
		return err
	}
	return a.setPassword(ctx, id, hash, "", &h, nil)
}

func (a *Admin) setPassword(ctx context.Context, id ids.UUID, hash, old string, h *holder, keep []byte) error {
	return a.mutate(ctx, "user.update", id.String(), func(tx store.Tx) (any, error) {
		if h != nil {
			if err := h.requireOutranks(ctx, tx.Auth(), id); err != nil {
				return nil, err
			}
		}
		u, err := tx.Auth().GetUser(ctx, id)
		if err != nil {
			return nil, err
		}
		if old != "" && u.PasswordHash != old {
			return nil, ErrUnauthenticated
		}
		u.PasswordHash, u.UpdatedAt = hash, a.clock.Now()
		if err := tx.Auth().UpdateUser(ctx, u); err != nil {
			return nil, err
		}
		var err2 error
		if keep != nil {
			err2 = tx.Auth().DeleteOtherSessions(ctx, id, keep)
		} else {
			err2 = tx.Auth().DeleteUserSessions(ctx, id)
		}
		if err2 != nil {
			return nil, err2
		}
		return map[string]any{"login": u.Login, "fields": []string{"password"}}, nil
	})
}

// ChangeOwnPassword sets the caller's password and ends their sessions, except the one with hash keep (nil: all).
func (a *Admin) ChangeOwnPassword(ctx context.Context, current, next string, keep []byte) error {
	p := PrincipalFrom(ctx)
	if p == nil || p.Kind != KindUser {
		return ErrUnauthenticated
	}
	if err := checkPassword("password", next); err != nil {
		return err
	}
	key := p.ID.String()
	if d := a.pwThr.take(key, a.clock.Now()); d > 0 {
		return &ThrottledError{RetryAfter: d}
	}
	if !a.verify.tryAcquire() {
		a.pwThr.give(key)
		return ErrBusy
	}
	ok, u, err := a.verifyCurrent(ctx, p.ID, current)
	a.verify.release()
	if err != nil {
		a.pwThr.give(key)
		return err
	}
	if !ok {
		return ErrUnauthenticated
	}
	hash, err := HashPasswordWith(a.rand, next, a.iter)
	if err != nil {
		return err
	}
	if err := a.setPassword(ctx, p.ID, hash, u.PasswordHash, nil, keep); err != nil {
		return err
	}
	a.pwThr.clear(key)
	return nil
}

func (a *Admin) verifyCurrent(ctx context.Context, id ids.UUID, current string) (bool, store.User, error) {
	var u store.User
	if err := a.view(ctx, func(as store.AuthStore) (err error) { u, err = as.GetUser(ctx, id); return }); err != nil {
		return false, u, err
	}
	if u.Disabled || len(current) > MaxPasswordLen {
		return false, u, nil
	}
	ok, _ := VerifyPasswordWith(u.PasswordHash, current, a.iter)
	return ok, u, nil
}

func (a *Admin) DeleteUser(ctx context.Context, id ids.UUID) error {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return err
	}
	if isSelf(ctx, id) {
		return invalid("id", "you cannot delete yourself")
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "user.delete", id.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		if err := h.requireOutranks(ctx, as, id); err != nil {
			return nil, err
		}
		u, err := as.GetUser(ctx, id)
		if err != nil {
			return nil, err
		}
		err = keepAdmin(ctx, as, func() error { return as.DeleteUser(ctx, id) })
		return map[string]any{"login": u.Login}, err
	})
}

func (a *Admin) GetUser(ctx context.Context, id ids.UUID) (User, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return User{}, err
	}
	var u store.User
	err := a.view(ctx, func(as store.AuthStore) (err error) { u, err = as.GetUser(ctx, id); return })
	return userView(u), err
}

func (a *Admin) ListUsers(ctx context.Context, p store.Page) ([]User, int, error) {
	return a.SearchUsers(ctx, p, "")
}

// SearchUsers lists users whose login, name or email contains search (all users when empty).
func (a *Admin) SearchUsers(ctx context.Context, p store.Page, search string) ([]User, int, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return nil, 0, err
	}
	var us []store.User
	var total int
	err := a.view(ctx, func(as store.AuthStore) (err error) { us, total, err = as.ListUsers(ctx, page(p), search); return })
	out := make([]User, len(us))
	for i, u := range us {
		out[i] = userView(u)
	}
	return out, total, err
}

// Groups and memberships.

func (a *Admin) CreateGroup(ctx context.Context, name, description string) (store.Group, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return store.Group{}, err
	}
	name = strings.TrimSpace(name)
	if err := checkName("name", name); err != nil {
		return store.Group{}, err
	}
	now := a.clock.Now()
	g := store.Group{ID: a.ids.New(), Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
	err := a.mutate(ctx, "group.create", g.ID.String(), func(tx store.Tx) (any, error) {
		return map[string]any{"name": name}, tx.Auth().CreateGroup(ctx, g)
	})
	return g, err
}

func (a *Admin) UpdateGroup(ctx context.Context, id ids.UUID, name, description *string) (store.Group, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return store.Group{}, err
	}
	var out store.Group
	err := a.mutate(ctx, "group.update", id.String(), func(tx store.Tx) (any, error) {
		g, err := tx.Auth().GetGroup(ctx, id)
		if err != nil {
			return nil, err
		}
		if name != nil {
			if err := checkName("name", strings.TrimSpace(*name)); err != nil {
				return nil, err
			}
			g.Name = strings.TrimSpace(*name)
		}
		if description != nil {
			g.Description = *description
		}
		g.UpdatedAt = a.clock.Now()
		out = g
		return map[string]any{"name": g.Name}, tx.Auth().UpdateGroup(ctx, g)
	})
	return out, err
}

func (a *Admin) DeleteGroup(ctx context.Context, id ids.UUID) error {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return err
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "group.delete", id.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		g, err := as.GetGroup(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := h.requireGroup(ctx, as, id); err != nil {
			return nil, err
		}
		err = keepAdmin(ctx, as, func() error { return as.DeleteGroup(ctx, id) })
		return map[string]any{"name": g.Name}, err
	})
}

// requireGroup: the caller holds every policy the group's roles give.
func (h holder) requireGroup(ctx context.Context, as store.AuthStore, group ids.UUID) error {
	pols, err := policiesIn(ctx, as, store.PrincipalRef{Kind: store.PrincipalGroup, ID: group})
	if err != nil {
		return err
	}
	return h.require(pols)
}

func (a *Admin) GetGroup(ctx context.Context, id ids.UUID) (store.Group, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return store.Group{}, err
	}
	var g store.Group
	err := a.view(ctx, func(as store.AuthStore) (err error) { g, err = as.GetGroup(ctx, id); return })
	return g, err
}

func (a *Admin) ListGroups(ctx context.Context, p store.Page) ([]store.Group, int, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return nil, 0, err
	}
	var gs []store.Group
	var total int
	err := a.view(ctx, func(as store.AuthStore) (err error) { gs, total, err = as.ListGroups(ctx, page(p)); return })
	return gs, total, err
}

func (a *Admin) Members(ctx context.Context, group ids.UUID, p store.Page) ([]User, int, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return nil, 0, err
	}
	var us []store.User
	var total int
	err := a.view(ctx, func(as store.AuthStore) (err error) { us, total, err = as.Members(ctx, group, page(p)); return })
	out := make([]User, len(us))
	for i, u := range us {
		out[i] = userView(u)
	}
	return out, total, err
}

// GroupsOf lists the groups a user belongs to.
func (a *Admin) GroupsOf(ctx context.Context, user ids.UUID) ([]ids.UUID, error) {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return nil, err
	}
	var gs []ids.UUID
	err := a.view(ctx, func(as store.AuthStore) (err error) { gs, err = as.GroupsOf(ctx, user); return })
	return gs, err
}

func (a *Admin) AddMember(ctx context.Context, group, user ids.UUID) error {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return err
	}
	if isSelf(ctx, user) {
		return invalid("user", "you cannot change your own group membership")
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "group.member_add", group.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		if _, err := as.GetGroup(ctx, group); err != nil {
			return nil, err
		}
		if _, err := as.GetUser(ctx, user); err != nil {
			return nil, err
		}
		if err := h.requireGroup(ctx, as, group); err != nil {
			return nil, err
		}
		return map[string]any{"user": user.String()}, as.AddMember(ctx, group, user)
	})
}

func (a *Admin) RemoveMember(ctx context.Context, group, user ids.UUID) error {
	if err := a.can(ctx, PolicyUserManage); err != nil {
		return err
	}
	if isSelf(ctx, user) {
		return invalid("user", "you cannot change your own group membership")
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "group.member_remove", group.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		if err := h.requireGroup(ctx, as, group); err != nil {
			return nil, err
		}
		err := keepAdmin(ctx, as, func() error { return as.RemoveMember(ctx, group, user) })
		return map[string]any{"user": user.String()}, err
	})
}
