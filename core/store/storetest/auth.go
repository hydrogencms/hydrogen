package storetest

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

var authTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"AuthUsers", testAuthUsers},
	{"AuthGroups", testAuthGroups},
	{"AuthRoles", testAuthRoles},
	{"AuthAssignments", testAuthAssignments},
	{"AuthTokens", testAuthTokens},
	{"AuthSessions", testAuthSessions},
	{"AuthMeta", testAuthMeta},
	{"AuthSessionUser", testAuthSessionUser},
	{"AuthPaging", testAuthPaging},
	{"AuthCascades", testAuthCascades},
}

type authFixture struct {
	t  *testing.T
	db store.DB
	g  *ids.Generator
}

func newAuthFixture(t *testing.T, db store.DB) *authFixture {
	return &authFixture{t, db, ids.NewGenerator(clock.NewManual(t0), nil)}
}

func (f *authFixture) do(fn func(store.AuthStore) error) {
	f.t.Helper()
	if err := f.db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Auth()) }); err != nil {
		f.t.Fatal(err)
	}
}

// fails runs fn in its own transaction and checks that it returns want.
func (f *authFixture) fails(want error, fn func(store.AuthStore) error) {
	f.t.Helper()
	err := f.db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Auth()) })
	if !errors.Is(err, want) {
		f.t.Fatalf("got %v, want %v", err, want)
	}
}

func (f *authFixture) view(fn func(store.AuthStore) error) {
	f.t.Helper()
	if err := f.db.View(context.Background(), func(tx store.Tx) error { return fn(tx.Auth()) }); err != nil {
		f.t.Fatal(err)
	}
}

func (f *authFixture) user(login string) store.User {
	u := store.User{ID: f.g.New(), Login: login, Email: login + "@example.com", Name: login, PasswordHash: "hash",
		CreatedAt: t0, UpdatedAt: t0.Add(time.Second)}
	f.do(func(a store.AuthStore) error { return a.CreateUser(context.Background(), u) })
	return u
}

func (f *authFixture) group(name string) store.Group {
	g := store.Group{ID: f.g.New(), Name: name, Description: name + " group", CreatedAt: t0, UpdatedAt: t0}
	f.do(func(a store.AuthStore) error { return a.CreateGroup(context.Background(), g) })
	return g
}

func (f *authFixture) role(name string) store.Role {
	r := store.Role{ID: f.g.New(), Name: name, Policies: []byte(`[{"policy":"content.read","limitations":{}}]`),
		CreatedAt: t0, UpdatedAt: t0}
	f.do(func(a store.AuthStore) error { return a.CreateRole(context.Background(), r) })
	return r
}

func (f *authFixture) token(name string, secret byte) store.Token {
	tk := store.Token{ID: f.g.New(), Name: name, Kind: "token", SecretHash: bytes.Repeat([]byte{secret}, 32),
		Prefix: "hyt_" + name, CreatedBy: "user:x", CreatedAt: t0.Add(time.Duration(secret) * time.Second)}
	f.do(func(a store.AuthStore) error { return a.CreateToken(context.Background(), tk) })
	return tk
}

func (f *authFixture) assign(role ids.UUID, kind store.PrincipalKind, principal ids.UUID, limitations string) store.Assignment {
	as := store.Assignment{ID: f.g.New(), RoleID: role, Principal: store.PrincipalRef{Kind: kind, ID: principal},
		Limitations: []byte(limitations), CreatedAt: t0}
	f.do(func(a store.AuthStore) error { return a.CreateAssignment(context.Background(), as) })
	return as
}

func testAuthUsers(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	u := f.user("ada")

	f.view(func(a store.AuthStore) error {
		got, err := a.GetUser(ctx, u.ID)
		if err != nil || !reflect.DeepEqual(got, u) {
			t.Errorf("GetUser %+v %v, want %+v", got, err, u)
		}
		if got, err = a.UserByLogin(ctx, "ada"); err != nil || got.ID != u.ID {
			t.Errorf("UserByLogin %+v %v", got, err)
		}
		if _, err = a.UserByLogin(ctx, "nobody"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("UserByLogin missing: %v", err)
		}
		if _, err = a.GetUser(ctx, f.g.New()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetUser missing: %v", err)
		}
		return nil
	})

	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error {
		return a.CreateUser(ctx, store.User{ID: f.g.New(), Login: "ada", CreatedAt: t0, UpdatedAt: t0})
	})
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error { return a.CreateUser(ctx, u) })

	login := t0.Add(time.Hour + 123*time.Millisecond)
	u.Name, u.Email, u.Disabled, u.PasswordHash = "Ada L.", "ada@lovelace.test", true, ""
	u.UpdatedAt, u.LastLoginAt = t0.Add(2*time.Hour), &login
	f.do(func(a store.AuthStore) error { return a.UpdateUser(ctx, u) })
	f.view(func(a store.AuthStore) error {
		got, err := a.GetUser(ctx, u.ID)
		if err != nil || !reflect.DeepEqual(got, u) {
			t.Errorf("after update %+v %v, want %+v", got, err, u)
		}
		return nil
	})

	u.LastLoginAt = nil
	f.do(func(a store.AuthStore) error { return a.UpdateUser(ctx, u) })
	f.view(func(a store.AuthStore) error {
		if got, _ := a.GetUser(ctx, u.ID); got.LastLoginAt != nil {
			t.Errorf("LastLoginAt %v, want nil", got.LastLoginAt)
		}
		return nil
	})

	other := f.user("bob")
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error { other.Login = "ada"; return a.UpdateUser(ctx, other) })
	f.do(func(a store.AuthStore) error { return a.UpdateUser(ctx, u) }) // same login, own row
	f.fails(store.ErrNotFound, func(a store.AuthStore) error {
		return a.UpdateUser(ctx, store.User{ID: f.g.New(), Login: "ghost", CreatedAt: t0, UpdatedAt: t0})
	})

	f.do(func(a store.AuthStore) error { return a.DeleteUser(ctx, u.ID) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.DeleteUser(ctx, u.ID) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { _, err := a.GetUser(ctx, u.ID); return err })
}

func testAuthGroups(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	g := f.group("editors")
	f.view(func(a store.AuthStore) error {
		got, err := a.GetGroup(ctx, g.ID)
		if err != nil || !reflect.DeepEqual(got, g) {
			t.Errorf("GetGroup %+v %v, want %+v", got, err, g)
		}
		if _, err = a.GetGroup(ctx, f.g.New()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetGroup missing: %v", err)
		}
		return nil
	})
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error {
		return a.CreateGroup(ctx, store.Group{ID: f.g.New(), Name: "editors", CreatedAt: t0, UpdatedAt: t0})
	})
	g.Name, g.Description, g.UpdatedAt = "writers", "", t0.Add(time.Minute)
	f.do(func(a store.AuthStore) error { return a.UpdateGroup(ctx, g) })
	f.view(func(a store.AuthStore) error {
		if got, _ := a.GetGroup(ctx, g.ID); !reflect.DeepEqual(got, g) {
			t.Errorf("after update %+v, want %+v", got, g)
		}
		return nil
	})
	o := f.group("admins")
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error { o.Name = "writers"; return a.UpdateGroup(ctx, o) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error {
		return a.UpdateGroup(ctx, store.Group{ID: f.g.New(), Name: "x", CreatedAt: t0, UpdatedAt: t0})
	})

	u1, u2, u3 := f.user("zed"), f.user("amy"), f.user("kim")
	f.do(func(a store.AuthStore) error {
		for _, u := range []store.User{u1, u2, u3} {
			if err := a.AddMember(ctx, g.ID, u.ID); err != nil {
				return err
			}
		}
		if err := a.AddMember(ctx, g.ID, u1.ID); err != nil { // idempotent
			return err
		}
		return a.AddMember(ctx, o.ID, u1.ID)
	})
	f.fails(store.ErrConstraint, func(a store.AuthStore) error { return a.AddMember(ctx, g.ID, f.g.New()) })
	f.fails(store.ErrConstraint, func(a store.AuthStore) error { return a.AddMember(ctx, f.g.New(), u1.ID) })

	f.view(func(a store.AuthStore) error {
		members, total, err := a.Members(ctx, g.ID, store.Page{})
		if err != nil || total != 3 || len(members) != 3 || members[0].Login != "amy" || members[1].Login != "kim" || members[2].Login != "zed" {
			t.Errorf("Members %+v %d %v", members, total, err)
		}
		if members, total, _ = a.Members(ctx, g.ID, store.Page{Limit: 1, Offset: 1}); total != 3 || len(members) != 1 || members[0].Login != "kim" {
			t.Errorf("Members page %+v %d", members, total)
		}
		of, err := a.GroupsOf(ctx, u1.ID)
		want := []ids.UUID{g.ID, o.ID}
		if want[0].String() > want[1].String() {
			want[0], want[1] = want[1], want[0]
		}
		if err != nil || !reflect.DeepEqual(of, want) {
			t.Errorf("GroupsOf %v %v, want %v", of, err, want)
		}
		if of, _ = a.GroupsOf(ctx, f.g.New()); len(of) != 0 {
			t.Errorf("GroupsOf unknown user: %v", of)
		}
		return nil
	})

	f.do(func(a store.AuthStore) error {
		if err := a.RemoveMember(ctx, g.ID, u1.ID); err != nil {
			return err
		}
		return a.RemoveMember(ctx, g.ID, u1.ID) // idempotent
	})
	f.view(func(a store.AuthStore) error {
		if _, total, _ := a.Members(ctx, g.ID, store.Page{}); total != 2 {
			t.Errorf("members after remove: %d", total)
		}
		return nil
	})
	f.do(func(a store.AuthStore) error { return a.DeleteGroup(ctx, g.ID) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.DeleteGroup(ctx, g.ID) })
	f.view(func(a store.AuthStore) error {
		if of, _ := a.GroupsOf(ctx, u2.ID); len(of) != 0 {
			t.Errorf("memberships survive group delete: %v", of)
		}
		return nil
	})
}

func testAuthRoles(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	r := f.role("editor")
	f.view(func(a store.AuthStore) error {
		got, err := a.GetRole(ctx, r.ID)
		if err != nil || !reflect.DeepEqual(got, r) {
			t.Errorf("GetRole %+v %v, want %+v", got, err, r)
		}
		if got, err = a.RoleByName(ctx, "editor"); err != nil || got.ID != r.ID {
			t.Errorf("RoleByName %+v %v", got, err)
		}
		if _, err = a.RoleByName(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("RoleByName missing: %v", err)
		}
		return nil
	})
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error {
		return a.CreateRole(ctx, store.Role{ID: f.g.New(), Name: "editor", CreatedAt: t0, UpdatedAt: t0})
	})

	r.Name, r.Description, r.Template, r.UpdatedAt = "publisher", "d", "publisher", t0.Add(time.Minute)
	r.Policies = []byte(`[{"policy":"content.publish","limitations":{"type":["page"]}}]`)
	f.do(func(a store.AuthStore) error { return a.UpdateRole(ctx, r) })
	f.view(func(a store.AuthStore) error {
		if got, _ := a.GetRole(ctx, r.ID); !reflect.DeepEqual(got, r) {
			t.Errorf("after update %+v, want %+v", got, r)
		}
		return nil
	})
	o := f.role("viewer")
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error { o.Name = "publisher"; return a.UpdateRole(ctx, o) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error {
		return a.UpdateRole(ctx, store.Role{ID: f.g.New(), Name: "x", CreatedAt: t0, UpdatedAt: t0})
	})

	f.do(func(a store.AuthStore) error {
		return a.CreateRole(ctx, store.Role{ID: f.g.New(), Name: "empty", CreatedAt: t0, UpdatedAt: t0})
	})
	f.view(func(a store.AuthStore) error {
		roles, total, err := a.ListRoles(ctx, store.Page{})
		if err != nil || total != 3 || len(roles) != 3 || roles[0].Name != "empty" || roles[1].Name != "publisher" || roles[2].Name != "viewer" {
			t.Errorf("ListRoles %+v %d %v", roles, total, err)
		}
		if string(roles[0].Policies) != "[]" {
			t.Errorf("default policies %q", roles[0].Policies)
		}
		return nil
	})
	f.do(func(a store.AuthStore) error { return a.DeleteRole(ctx, r.ID) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.DeleteRole(ctx, r.ID) })
}

func testAuthAssignments(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	u, g, tk := f.user("ada"), f.group("team"), f.token("ci", 1)
	rb, ra := f.role("b-role"), f.role("a-role")

	a1 := f.assign(rb.ID, store.PrincipalUser, u.ID, `{"type":["page"]}`)
	a2 := f.assign(ra.ID, store.PrincipalGroup, g.ID, `{}`)
	a3 := f.assign(ra.ID, store.PrincipalUser, u.ID, ``)
	a4 := f.assign(rb.ID, store.PrincipalToken, tk.ID, `{}`)
	a5 := f.assign(rb.ID, store.PrincipalGroup, g.ID, `{}`)
	a3.Limitations = []byte(`{}`)

	f.view(func(a store.AuthStore) error {
		got, err := a.GetAssignment(ctx, a1.ID)
		if err != nil || !reflect.DeepEqual(got, a1) {
			t.Errorf("GetAssignment %+v %v, want %+v", got, err, a1)
		}
		if _, err = a.GetAssignment(ctx, f.g.New()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetAssignment missing: %v", err)
		}
		list, err := a.RoleAssignments(ctx, rb.ID)
		if err != nil || len(list) != 3 || list[0].ID != a1.ID || list[1].ID != a4.ID || list[2].ID != a5.ID {
			t.Errorf("RoleAssignments %+v %v", list, err)
		}
		return nil
	})

	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error {
		dup := a1
		dup.ID = f.g.New()
		return a.CreateAssignment(ctx, dup)
	})
	f.fails(store.ErrConstraint, func(a store.AuthStore) error {
		return a.CreateAssignment(ctx, store.Assignment{ID: f.g.New(), RoleID: f.g.New(),
			Principal: store.PrincipalRef{Kind: store.PrincipalUser, ID: u.ID}, CreatedAt: t0})
	})

	f.view(func(a store.AuthStore) error {
		grants, err := a.AssignmentsFor(ctx, []store.PrincipalRef{{Kind: store.PrincipalUser, ID: u.ID}, {Kind: store.PrincipalGroup, ID: g.ID}})
		if err != nil {
			t.Fatal(err)
		}
		// a-role: a2 (group), a3 (user) by id; b-role: a1, a5 by id.
		want := []store.Assignment{a2, a3, a1, a5}
		slicesSortByRoleThenID(want, map[ids.UUID]string{ra.ID: ra.Name, rb.ID: rb.Name})
		if len(grants) != len(want) {
			t.Fatalf("AssignmentsFor: %d grants, want %d: %+v", len(grants), len(want), grants)
		}
		for i, w := range want {
			gr := grants[i]
			role := ra
			if w.RoleID == rb.ID {
				role = rb
			}
			if !reflect.DeepEqual(gr.Assignment, w) || !reflect.DeepEqual(gr.Role, role) {
				t.Errorf("grant %d: %+v, want %+v with %+v", i, gr, w, role)
			}
		}
		// A kind mismatch must not match: the token's id queried as a user.
		if grants, _ = a.AssignmentsFor(ctx, []store.PrincipalRef{{Kind: store.PrincipalUser, ID: tk.ID}}); len(grants) != 0 {
			t.Errorf("kind mismatch matched: %+v", grants)
		}
		if grants, _ = a.AssignmentsFor(ctx, nil); len(grants) != 0 {
			t.Errorf("no principals matched: %+v", grants)
		}
		if grants, _ = a.AssignmentsFor(ctx, []store.PrincipalRef{{Kind: store.PrincipalToken, ID: tk.ID}}); len(grants) != 1 || grants[0].Assignment.ID != a4.ID {
			t.Errorf("token grants: %+v", grants)
		}
		return nil
	})

	f.do(func(a store.AuthStore) error { return a.DeleteAssignment(ctx, a1.ID) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.DeleteAssignment(ctx, a1.ID) })
}

func slicesSortByRoleThenID(list []store.Assignment, names map[ids.UUID]string) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			a, b := list[j-1], list[j]
			na, nb := names[a.RoleID], names[b.RoleID]
			if na < nb || na == nb && a.ID.String() < b.ID.String() {
				break
			}
			list[j-1], list[j] = b, a
		}
	}
}

func testAuthTokens(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	tk := f.token("ci", 1)
	exp, used := t0.Add(24*time.Hour+5*time.Millisecond), t0.Add(time.Hour)
	agent := store.Token{ID: f.g.New(), Name: "bot", Kind: "agent", SecretHash: bytes.Repeat([]byte{2}, 32), Prefix: "hyt_bot",
		CreatedAt: t0.Add(2 * time.Second), ExpiresAt: &exp, LastUsedAt: &used}
	f.do(func(a store.AuthStore) error { return a.CreateToken(ctx, agent) })

	f.view(func(a store.AuthStore) error {
		got, err := a.GetToken(ctx, tk.ID)
		if err != nil || !reflect.DeepEqual(got, tk) {
			t.Errorf("GetToken %+v %v, want %+v", got, err, tk)
		}
		if got, err = a.TokenByHash(ctx, agent.SecretHash); err != nil || !reflect.DeepEqual(got, agent) {
			t.Errorf("TokenByHash %+v %v, want %+v", got, err, agent)
		}
		if _, err = a.TokenByHash(ctx, []byte("nope")); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("TokenByHash missing: %v", err)
		}
		if _, err = a.GetToken(ctx, f.g.New()); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetToken missing: %v", err)
		}
		return nil
	})
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error {
		dup := tk
		dup.ID = f.g.New()
		return a.CreateToken(ctx, dup)
	})

	at := t0.Add(3*time.Hour + 7*time.Millisecond)
	f.do(func(a store.AuthStore) error {
		if err := a.TouchToken(ctx, tk.ID, used); err != nil {
			return err
		}
		if err := a.RevokeToken(ctx, tk.ID, at); err != nil {
			return err
		}
		return a.RevokeToken(ctx, tk.ID, at.Add(time.Hour)) // keeps the first revocation
	})
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.TouchToken(ctx, f.g.New(), used) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.RevokeToken(ctx, f.g.New(), at) })
	f.view(func(a store.AuthStore) error {
		got, _ := a.GetToken(ctx, tk.ID)
		if got.LastUsedAt == nil || !got.LastUsedAt.Equal(used) || got.RevokedAt == nil || !got.RevokedAt.Equal(at) {
			t.Errorf("after touch and revoke: %+v", got)
		}
		list, total, err := a.ListTokens(ctx, store.Page{})
		if err != nil || total != 2 || len(list) != 2 || list[0].ID != agent.ID || list[1].ID != tk.ID {
			t.Errorf("ListTokens newest first: %+v %d %v", list, total, err)
		}
		return nil
	})

	f.do(func(a store.AuthStore) error { return a.DeleteToken(ctx, tk.ID) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.DeleteToken(ctx, tk.ID) })
}

func testAuthSessions(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	u1, u2 := f.user("ada"), f.user("bob")
	mk := func(b byte, u ids.UUID, expires time.Time) store.Session {
		s := store.Session{IDHash: bytes.Repeat([]byte{b}, 32), UserID: u, CSRF: "csrf" + strconv.Itoa(int(b)),
			CreatedAt: t0, LastSeenAt: t0, ExpiresAt: expires, IP: "10.0.0." + strconv.Itoa(int(b)), UserAgent: "ua"}
		f.do(func(a store.AuthStore) error { return a.CreateSession(ctx, s) })
		return s
	}
	s1 := mk(1, u1.ID, t0.Add(time.Hour+9*time.Millisecond))
	s2 := mk(2, u1.ID, t0.Add(2*time.Hour))
	s3 := mk(3, u2.ID, t0.Add(3*time.Hour))

	f.view(func(a store.AuthStore) error {
		got, err := a.SessionByHash(ctx, s1.IDHash)
		if err != nil || !reflect.DeepEqual(got, s1) {
			t.Errorf("SessionByHash %+v %v, want %+v", got, err, s1)
		}
		if _, err = a.SessionByHash(ctx, []byte("nope")); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("SessionByHash missing: %v", err)
		}
		return nil
	})
	f.fails(store.ErrDuplicateKey, func(a store.AuthStore) error { return a.CreateSession(ctx, s1) })

	seen, expires := t0.Add(30*time.Minute), t0.Add(5*time.Hour)
	f.do(func(a store.AuthStore) error { return a.TouchSession(ctx, s1.IDHash, seen, expires) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { return a.TouchSession(ctx, []byte("nope"), seen, expires) })
	f.view(func(a store.AuthStore) error {
		got, _ := a.SessionByHash(ctx, s1.IDHash)
		if !got.LastSeenAt.Equal(seen) || !got.ExpiresAt.Equal(expires) || !got.CreatedAt.Equal(t0) {
			t.Errorf("after touch: %+v", got)
		}
		return nil
	})

	f.do(func(a store.AuthStore) error {
		n, err := a.DeleteExpiredSessions(ctx, t0.Add(2*time.Hour)) // s2 expires exactly now
		if n != 1 || err != nil {
			t.Errorf("DeleteExpiredSessions: %d %v, want 1", n, err)
		}
		if n, err = a.DeleteExpiredSessions(ctx, t0.Add(2*time.Hour)); n != 0 || err != nil {
			t.Errorf("repeat DeleteExpiredSessions: %d %v", n, err)
		}
		return nil
	})
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { _, err := a.SessionByHash(ctx, s2.IDHash); return err })

	s4 := mk(4, u1.ID, t0.Add(4*time.Hour))
	f.do(func(a store.AuthStore) error { return a.DeleteOtherSessions(ctx, u1.ID, s4.IDHash) })
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { _, err := a.SessionByHash(ctx, s1.IDHash); return err })
	f.view(func(a store.AuthStore) error {
		for _, s := range []store.Session{s4, s3} {
			if _, err := a.SessionByHash(ctx, s.IDHash); err != nil {
				t.Errorf("DeleteOtherSessions removed %x: %v", s.IDHash[:1], err)
			}
		}
		return nil
	})

	f.do(func(a store.AuthStore) error {
		if err := a.DeleteUserSessions(ctx, u1.ID); err != nil {
			return err
		}
		if err := a.DeleteUserSessions(ctx, u1.ID); err != nil { // idempotent
			return err
		}
		if err := a.DeleteSession(ctx, s3.IDHash); err != nil {
			return err
		}
		return a.DeleteSession(ctx, s3.IDHash)
	})
	for _, s := range []store.Session{s1, s4, s3} {
		f.fails(store.ErrNotFound, func(a store.AuthStore) error { _, err := a.SessionByHash(ctx, s.IDHash); return err })
	}
}

func testAuthMeta(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { _, err := a.Meta(ctx, "templates_seeded"); return err })
	f.do(func(a store.AuthStore) error { return a.SetMeta(ctx, "templates_seeded", "1") })
	f.do(func(a store.AuthStore) error { return a.SetMeta(ctx, "other", "x") })
	f.do(func(a store.AuthStore) error { return a.SetMeta(ctx, "templates_seeded", "2") })
	f.view(func(a store.AuthStore) error {
		if v, err := a.Meta(ctx, "templates_seeded"); err != nil || v != "2" {
			t.Errorf("Meta %q %v, want 2", v, err)
		}
		if v, err := a.Meta(ctx, "other"); err != nil || v != "x" {
			t.Errorf("Meta other %q %v", v, err)
		}
		return nil
	})
}

func testAuthPaging(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	const n = 7
	for i := range n {
		f.user("u" + strconv.Itoa(i))
		f.group("g" + strconv.Itoa(i))
		f.token("t"+strconv.Itoa(i), byte(i+1))
	}
	f.view(func(a store.AuthStore) error {
		for _, p := range []store.Page{{}, {Limit: 3}, {Limit: 3, Offset: 6}, {Limit: 3, Offset: 9}, {Limit: 1000}, {Limit: -5, Offset: -2}} {
			wantRows := min(max(n-max(p.Offset, 0), 0), map[bool]int{true: 3, false: 50}[p.Limit == 3])
			users, total, err := a.ListUsers(ctx, p, "")
			if err != nil || total != n || len(users) != wantRows {
				t.Errorf("ListUsers %+v: %d rows, total %d, %v; want %d rows", p, len(users), total, err, wantRows)
			}
			groups, total, err := a.ListGroups(ctx, p)
			if err != nil || total != n || len(groups) != wantRows {
				t.Errorf("ListGroups %+v: %d rows, total %d, %v", p, len(groups), total, err)
			}
			tokens, total, err := a.ListTokens(ctx, p)
			if err != nil || total != n || len(tokens) != wantRows {
				t.Errorf("ListTokens %+v: %d rows, total %d, %v", p, len(tokens), total, err)
			}
		}
		users, _, _ := a.ListUsers(ctx, store.Page{Limit: 2, Offset: 2}, "")
		if len(users) != 2 || users[0].Login != "u2" || users[1].Login != "u3" {
			t.Errorf("ListUsers order: %+v", users)
		}
		for _, c := range []struct {
			q    string
			want int
		}{{"U3", 1}, {"u", n}, {"EXAMPLE.com", n}, {"_", 0}, {"%", 0}, {`\`, 0}, {"nobody", 0}} {
			found, total, err := a.ListUsers(ctx, store.Page{}, c.q)
			if err != nil || total != c.want || len(found) != c.want {
				t.Errorf("ListUsers search %q: %d rows, total %d, %v; want %d", c.q, len(found), total, err, c.want)
			}
		}
		if found, total, _ := a.ListUsers(ctx, store.Page{Limit: 2, Offset: 1}, "u"); total != n || len(found) != 2 || found[0].Login != "u1" {
			t.Errorf("ListUsers search paging: %+v %d", found, total)
		}
		groups, _, _ := a.ListGroups(ctx, store.Page{Limit: 2, Offset: 5})
		if len(groups) != 2 || groups[0].Name != "g5" || groups[1].Name != "g6" {
			t.Errorf("ListGroups order: %+v", groups)
		}
		return nil
	})
}

func testAuthCascades(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	u, other := f.user("ada"), f.user("bob")
	g, tk := f.group("team"), f.token("ci", 1)
	r := f.role("editor")
	f.do(func(a store.AuthStore) error {
		if err := a.AddMember(ctx, g.ID, u.ID); err != nil {
			return err
		}
		if err := a.AddMember(ctx, g.ID, other.ID); err != nil {
			return err
		}
		for _, b := range []byte{1, 2} {
			if err := a.CreateSession(ctx, store.Session{IDHash: []byte{b}, UserID: u.ID, CSRF: "c", CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour)}); err != nil {
				return err
			}
		}
		return a.CreateSession(ctx, store.Session{IDHash: []byte{3}, UserID: other.ID, CSRF: "c", CreatedAt: t0, LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour)})
	})
	au := f.assign(r.ID, store.PrincipalUser, u.ID, `{}`)
	ao := f.assign(r.ID, store.PrincipalUser, other.ID, `{}`)
	ag := f.assign(r.ID, store.PrincipalGroup, g.ID, `{}`)
	at := f.assign(r.ID, store.PrincipalToken, tk.ID, `{}`)

	remaining := func() map[ids.UUID]bool {
		m := map[ids.UUID]bool{}
		f.view(func(a store.AuthStore) error {
			list, err := a.RoleAssignments(ctx, r.ID)
			for _, as := range list {
				m[as.ID] = true
			}
			return err
		})
		return m
	}
	if m := remaining(); len(m) != 4 {
		t.Fatalf("setup: %v", m)
	}

	f.do(func(a store.AuthStore) error { return a.DeleteUser(ctx, u.ID) })
	if m := remaining(); m[au.ID] || !m[ao.ID] || !m[ag.ID] || !m[at.ID] {
		t.Errorf("delete user: assignments %v", m)
	}
	f.view(func(a store.AuthStore) error {
		for _, b := range []byte{1, 2} {
			if _, err := a.SessionByHash(ctx, []byte{b}); !errors.Is(err, store.ErrNotFound) {
				t.Errorf("session %d survives user delete: %v", b, err)
			}
		}
		if _, err := a.SessionByHash(ctx, []byte{3}); err != nil {
			t.Errorf("other user's session: %v", err)
		}
		if _, total, _ := a.Members(ctx, g.ID, store.Page{}); total != 1 {
			t.Errorf("members after user delete: %d", total)
		}
		return nil
	})

	f.do(func(a store.AuthStore) error { return a.DeleteGroup(ctx, g.ID) })
	if m := remaining(); m[ag.ID] || !m[ao.ID] || !m[at.ID] {
		t.Errorf("delete group: assignments %v", m)
	}
	f.view(func(a store.AuthStore) error {
		if of, _ := a.GroupsOf(ctx, other.ID); len(of) != 0 {
			t.Errorf("membership survives group delete: %v", of)
		}
		if _, err := a.GetUser(ctx, other.ID); err != nil {
			t.Errorf("group delete removed a user: %v", err)
		}
		return nil
	})

	f.do(func(a store.AuthStore) error { return a.DeleteToken(ctx, tk.ID) })
	if m := remaining(); m[at.ID] || !m[ao.ID] {
		t.Errorf("delete token: assignments %v", m)
	}

	f.do(func(a store.AuthStore) error { return a.DeleteRole(ctx, r.ID) })
	if m := remaining(); len(m) != 0 {
		t.Errorf("delete role: assignments %v", m)
	}
	f.fails(store.ErrNotFound, func(a store.AuthStore) error { _, err := a.GetAssignment(ctx, ao.ID); return err })
}

func testAuthSessionUser(t *testing.T, db store.DB) {
	ctx := context.Background()
	f := newAuthFixture(t, db)
	u1, u2 := f.user("ada"), f.user("bob")
	g1, g2 := f.group("a"), f.group("b")
	f.do(func(a store.AuthStore) error {
		for _, g := range []store.Group{g1, g2} {
			if err := a.AddMember(ctx, g.ID, u1.ID); err != nil {
				return err
			}
		}
		for _, u := range []store.User{u1, u2} {
			s := store.Session{IDHash: bytes.Repeat([]byte{byte(u.Login[0])}, 32), UserID: u.ID, CSRF: "c", CreatedAt: t0,
				LastSeenAt: t0, ExpiresAt: t0.Add(time.Hour), IP: "10.0.0.1", UserAgent: "ua"}
			if err := a.CreateSession(ctx, s); err != nil {
				return err
			}
		}
		return nil
	})
	f.view(func(a store.AuthStore) error {
		for _, u := range []store.User{u1, u2} {
			hash := bytes.Repeat([]byte{byte(u.Login[0])}, 32)
			got, err := a.SessionUserByHash(ctx, hash)
			if err != nil {
				t.Fatalf("SessionUserByHash %s: %v", u.Login, err)
			}
			want, _ := a.SessionByHash(ctx, hash)
			wantUser, _ := a.GetUser(ctx, u.ID)
			wantGroups, _ := a.GroupsOf(ctx, u.ID)
			if !reflect.DeepEqual(got.Session, want) || !reflect.DeepEqual(got.User, wantUser) ||
				!slices.Equal(got.Groups, wantGroups) {
				t.Errorf("SessionUserByHash %s = %+v, want %+v %+v %v", u.Login, got, want, wantUser, wantGroups)
			}
		}
		if _, err := a.SessionUserByHash(ctx, []byte("nope")); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("missing session: %v", err)
		}
		return nil
	})
}
