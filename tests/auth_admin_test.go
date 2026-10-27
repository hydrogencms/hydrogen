package tests

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

func userRef(u access.User) store.PrincipalRef {
	return store.PrincipalRef{Kind: store.PrincipalUser, ID: u.ID}
}

func wantForbidden(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, access.ErrForbidden) {
		t.Errorf("%s: want ErrForbidden, got %v", what, err)
	}
}

func wantInvalid(t *testing.T, what, field string, err error) {
	t.Helper()
	var ie *access.InvalidError
	if !errors.As(err, &ie) || ie.Fields[field] == "" {
		t.Errorf("%s: want InvalidError on %q, got %v", what, field, err)
	}
}

func TestAdminSeedAndEnsureAdmin(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		roles, _, _ := e.admin.ListRoles(ctx, store.Page{})
		if len(roles) != len(access.Templates()) {
			t.Fatalf("roles = %d", len(roles))
		}
		// seeded once: a deleted role is not resurrected
		ed := e.role("editor")
		if err := e.admin.DeleteRole(ctx, ed.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.admin.SeedTemplates(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.RoleByName(ctx, "editor"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("editor came back: %v", err)
		}
		u, err := e.admin.EnsureAdmin(ctx, "Root", authPassword)
		if err != nil || u.Login != "root" {
			t.Fatalf("ensure: %v %+v", err, u)
		}
		if _, _, err := e.login("root", authPassword); err != nil {
			t.Fatal(err)
		}
		// again: new password, still one assignment, no duplicates
		if _, err := e.admin.EnsureAdmin(ctx, "root", "second long password"); err != nil {
			t.Fatal(err)
		}
		as, err := e.admin.AssignmentsOf(ctx, userRef(u))
		if err != nil || len(as) != 1 || as[0].Role != "administrator" {
			t.Fatalf("assignments %v %v", as, err)
		}
		if _, _, err := e.login("root", "second long password"); err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.EnsureAdmin(ctx, "BAD LOGIN", authPassword); err == nil {
			t.Fatal("bad login accepted")
		}
		if _, err := e.admin.EnsureAdmin(ctx, "ok", "short"); err == nil {
			t.Fatal("short password accepted")
		}
	})
}

func TestAdminUserValidation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		for _, bad := range []string{"", "has space", "ünï", strings.Repeat("a", 65), "a/b", "a;b"} {
			_, err := e.admin.CreateUser(ctx, access.UserInput{Login: bad})
			wantInvalid(t, "login "+bad, "login", err)
		}
		_, err := e.admin.CreateUser(ctx, access.UserInput{Login: "ok", Password: "short"})
		wantInvalid(t, "short password", "password", err)
		_, err = e.admin.CreateUser(ctx, access.UserInput{Login: "ok", Name: strings.Repeat("n", 201)})
		wantInvalid(t, "long name", "name", err)
		_, err = e.admin.CreateUser(ctx, access.UserInput{Login: "ok", Email: "nope"})
		wantInvalid(t, "email", "email", err)
		if _, err := e.admin.CreateUser(ctx, access.UserInput{Login: "dup"}); err != nil {
			t.Fatal(err)
		}
		_, err = e.admin.CreateUser(ctx, access.UserInput{Login: "dup"})
		wantInvalid(t, "duplicate login", "login", err)
		u, err := e.admin.CreateUser(ctx, access.UserInput{Login: "x.y_z@w-1"})
		if err != nil || u.Name != "x.y_z@w-1" || u.HasPassword {
			t.Fatalf("%+v %v", u, err)
		}
		_, err = e.admin.CreateGroup(ctx, " ", "")
		wantInvalid(t, "group name", "name", err)
		_, err = e.admin.CreateRole(ctx, access.RoleInput{Name: "r", Policies: []access.RolePolicy{{Policy: "nope.nothing"}}})
		if err == nil {
			t.Error("unknown policy accepted")
		}
		_, _, err = e.admin.CreateToken(ctx, access.TokenInput{Name: "t", Kind: "wizard"})
		wantInvalid(t, "kind", "kind", err)
		past := e.clk.Now().Add(-time.Minute)
		_, _, err = e.admin.CreateToken(ctx, access.TokenInput{Name: "t", ExpiresAt: &past})
		wantInvalid(t, "expiry", "expiresAt", err)
	})
}

func TestAdminPolicyChecks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("viewer1", "viewer")
		ctx := e.as("viewer1")
		_, err := e.admin.CreateUser(ctx, access.UserInput{Login: "z"})
		wantForbidden(t, "create user", err)
		_, _, err = e.admin.ListUsers(ctx, store.Page{})
		wantForbidden(t, "list users", err)
		_, _, err = e.admin.ListRoles(ctx, store.Page{})
		wantForbidden(t, "list roles", err)
		_, _, err = e.admin.ListTokens(ctx, store.Page{})
		wantForbidden(t, "list tokens", err)
		_, _, err = e.admin.CreateToken(ctx, access.TokenInput{Name: "t"})
		wantForbidden(t, "create token", err)
		wantForbidden(t, "delete group", e.admin.DeleteGroup(ctx, ids.UUID{1}))
		// an unauthenticated (nil-principal) system context is allowed; a bare agent token is not
		_, secret, _ := e.admin.CreateToken(context.Background(), access.TokenInput{Name: "bot", Kind: "agent", Roles: []ids.UUID{e.role("ai-contributor").ID}})
		an, err := e.authenticate(nil, secret)
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.admin.CreateUser(access.WithPrincipal(context.Background(), an.Principal), access.UserInput{Login: "z"})
		wantForbidden(t, "agent create user", err)
	})
}

func TestAdminEscalationAndSelfChanges(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		sys := context.Background()
		mgr, err := e.admin.CreateRole(sys, access.RoleInput{Name: "usermgr", Policies: []access.RolePolicy{
			{Policy: access.PolicyUserManage}, {Policy: access.PolicyRoleManage}, {Policy: access.PolicyTokenManage}}})
		if err != nil {
			t.Fatal(err)
		}
		carol := e.user("carol", "usermgr")
		boss := e.user("boss", "administrator")
		bob := e.user("bob")
		cctx := e.as("carol")

		// cannot hand out what she doesn't hold
		_, err = e.admin.CreateAssignment(cctx, access.AssignmentInput{RoleID: e.role("administrator").ID, Principal: userRef(bob)})
		wantForbidden(t, "assign administrator", err)
		_, err = e.admin.CreateAssignment(cctx, access.AssignmentInput{RoleID: e.role("viewer").ID, Principal: userRef(bob)})
		wantForbidden(t, "assign viewer (content.read not held)", err)
		// can hand out what she holds
		if _, err = e.admin.CreateAssignment(cctx, access.AssignmentInput{RoleID: mgr.ID, Principal: userRef(bob)}); err != nil {
			t.Fatalf("assign held role: %v", err)
		}
		// her own account state stays off limits (roles and groups: TestAdminSelfRolesAndGroups)
		off := true
		_, err = e.admin.UpdateUser(cctx, carol.ID, access.UserPatch{Disabled: &off})
		wantInvalid(t, "self disable", "disabled", err)
		wantInvalid(t, "self delete", "id", e.admin.DeleteUser(cctx, carol.ID))
		wantInvalid(t, "self reset", "password", e.admin.ResetPassword(cctx, carol.ID, "another long password"))

		// cannot touch a more privileged user
		wantForbidden(t, "reset admin password", e.admin.ResetPassword(cctx, boss.ID, "another long password"))
		_, err = e.admin.UpdateUser(cctx, boss.ID, access.UserPatch{Disabled: &off})
		wantForbidden(t, "disable admin", err)
		wantForbidden(t, "delete admin", e.admin.DeleteUser(cctx, boss.ID))
		// but may touch a peer or lesser one
		if err := e.admin.ResetPassword(cctx, bob.ID, "another long password"); err != nil {
			t.Fatalf("reset peer: %v", err)
		}

		// tokens: roles need the same rights
		_, _, err = e.admin.CreateToken(cctx, access.TokenInput{Name: "t", Roles: []ids.UUID{e.role("administrator").ID}})
		wantForbidden(t, "token with administrator", err)
		if _, _, err = e.admin.CreateToken(cctx, access.TokenInput{Name: "t", Roles: []ids.UUID{mgr.ID}}); err != nil {
			t.Fatalf("token with held role: %v", err)
		}
		// roles: cannot add policies she lacks, nor create a role holding them
		wide := []access.RolePolicy{{Policy: access.PolicyAll}}
		_, err = e.admin.UpdateRole(cctx, mgr.ID, access.RolePatch{Policies: &wide})
		wantForbidden(t, "widen own role", err)
		_, err = e.admin.CreateRole(cctx, access.RoleInput{Name: "evil", Policies: wide})
		wantForbidden(t, "create * role", err)
		desc := "renamed"
		if _, err = e.admin.UpdateRole(cctx, mgr.ID, access.RolePatch{Description: &desc}); err != nil {
			t.Fatalf("describe role: %v", err)
		}
		// groups: membership grants the group's roles
		g, _ := e.admin.CreateGroup(sys, "admins", "")
		if _, err := e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: e.role("administrator").ID,
			Principal: store.PrincipalRef{Kind: store.PrincipalGroup, ID: g.ID}}); err != nil {
			t.Fatal(err)
		}
		wantForbidden(t, "join admin group", e.admin.AddMember(cctx, g.ID, bob.ID))
		wantForbidden(t, "join admin group herself", e.admin.AddMember(cctx, g.ID, carol.ID))
		// a restricted grant is not enough: content.read limited to one collection cannot be re-granted
		_, _ = e.admin.CreateRole(sys, access.RoleInput{Name: "lim", Policies: []access.RolePolicy{
			{Policy: access.PolicyRoleManage}, {Policy: access.PolicyContentRead, Limitations: map[string][]string{"collection": {"article"}}}}})
		e.user("dave", "lim")
		dctx := e.as("dave")
		_, err = e.admin.CreateAssignment(dctx, access.AssignmentInput{RoleID: e.role("viewer").ID, Principal: userRef(bob)})
		wantForbidden(t, "re-grant restricted policy", err)
		// the administrator may do all of it
		actx := e.as("boss")
		if _, err := e.admin.CreateAssignment(actx, access.AssignmentInput{RoleID: e.role("editor").ID, Principal: userRef(bob)}); err != nil {
			t.Fatalf("admin assign: %v", err)
		}
		wantInvalid(t, "admin self-disable", "disabled", func() error {
			_, err := e.admin.UpdateUser(actx, boss.ID, access.UserPatch{Disabled: &off})
			return err
		}())
	})
}

func TestAdminOwnPasswordChange(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		ctx := e.as("alice")
		_, c, _ := e.login("alice", authPassword)
		if err := e.admin.ChangeOwnPassword(ctx, "wrong password!!", "another long password", nil); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("wrong current: %v", err)
		}
		wantInvalid(t, "short new", "password", e.admin.ChangeOwnPassword(ctx, authPassword, "short", nil))
		if err := e.admin.ChangeOwnPassword(ctx, authPassword, "another long password", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := e.authenticate(c, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal("session survived own password change")
		}
		if _, _, err := e.login("alice", "another long password"); err != nil {
			t.Fatal(err)
		}
		// repeated wrong current passwords are throttled
		var te *access.ThrottledError
		for i := 0; i < 5; i++ {
			_ = e.admin.ChangeOwnPassword(ctx, "wrong password!!", "yet another password", nil)
		}
		if err := e.admin.ChangeOwnPassword(ctx, "another long password", "yet another password", nil); !errors.As(err, &te) {
			t.Fatalf("not throttled: %v", err)
		}
		// tokens have no password
		if err := e.admin.ChangeOwnPassword(context.Background(), "a", "b", nil); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatal(err)
		}
	})
}

func TestAdminAuditOutboxNotify(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		ch, err := e.db.Notifier().Listen(ctx, access.NotifyChannel)
		if err != nil {
			t.Fatal(err)
		}
		// drain notifications from seeding
		for len(ch) > 0 {
			<-ch
		}
		boss := e.user("boss", "administrator")
		actx := e.as("boss")
		before := len(e.audits("user.create"))
		u, err := e.admin.CreateUser(actx, access.UserInput{Login: "eve", Password: authPassword})
		if err != nil {
			t.Fatal(err)
		}
		as := e.audits("user.create")
		if len(as) != before+1 || as[0].ActorKind != "user" || as[0].ActorID != boss.ID.String() || as[0].Subject != u.ID.String() {
			t.Fatalf("audit %+v", as)
		}
		if strings.Contains(string(as[0].Data), authPassword) || strings.Contains(string(as[0].Data), "pbkdf2") {
			t.Fatalf("password material in audit: %s", as[0].Data)
		}
		select {
		case n := <-ch:
			if n.Channel != access.NotifyChannel {
				t.Fatalf("notification %+v", n)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("no auth notification")
		}
		if _, err := e.ob.Drain(ctx); err != nil {
			t.Fatal(err)
		}
		e.mu.Lock()
		n := len(e.events)
		e.mu.Unlock()
		if n == 0 {
			t.Fatal("no auth.changed outbox event")
		}
		// every mutation type audits
		g, _ := e.admin.CreateGroup(actx, "g", "")
		_ = e.admin.AddMember(actx, g.ID, u.ID)
		_ = e.admin.RemoveMember(actx, g.ID, u.ID)
		r, _ := e.admin.CreateRole(actx, access.RoleInput{Name: "r1", Policies: []access.RolePolicy{{Policy: access.PolicyContentRead}}})
		asg, err := e.admin.CreateAssignment(actx, access.AssignmentInput{RoleID: r.ID, Principal: userRef(u),
			Limitations: map[string][]string{"collection": {"article"}}})
		if err != nil {
			t.Fatal(err)
		}
		_ = e.admin.DeleteAssignment(actx, asg.ID)
		tok, _, _ := e.admin.CreateToken(actx, access.TokenInput{Name: "k"})
		_ = e.admin.RevokeToken(actx, tok.ID)
		_ = e.admin.DeleteToken(actx, tok.ID)
		_, _ = e.admin.UpdateGroup(actx, g.ID, nil, nil)
		_ = e.admin.DeleteGroup(actx, g.ID)
		_ = e.admin.DeleteRole(actx, r.ID)
		_ = e.admin.DeleteUser(actx, u.ID)
		for _, a := range []string{"group.create", "group.member_add", "group.member_remove", "group.update", "group.delete", "role.create", "role.delete",
			"assignment.create", "assignment.delete", "token.create", "token.revoke", "token.delete", "user.delete"} {
			if len(e.audits(a)) == 0 {
				t.Errorf("no %s audit", a)
			}
		}
		// failures leave nothing behind
		n0 := len(e.audits("user.create"))
		_, _ = e.admin.CreateUser(actx, access.UserInput{Login: "boss"})
		if len(e.audits("user.create")) != n0 {
			t.Fatal("failed mutation audited")
		}
	})
}

func TestAdminCacheInvalidatedOnChange(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		u := e.user("alice")
		uctx := e.as("alice")
		if err := e.chk.Can(uctx, access.PolicyContentRead, access.Target{}); err == nil {
			t.Fatal("no role yet")
		}
		a, err := e.admin.CreateAssignment(ctx, access.AssignmentInput{RoleID: e.role("viewer").ID, Principal: userRef(u)})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.chk.Can(uctx, access.PolicyContentRead, access.Target{}); err != nil {
			t.Fatalf("grant not visible: %v", err)
		}
		if err := e.admin.DeleteAssignment(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.chk.Can(uctx, access.PolicyContentRead, access.Target{}); err == nil {
			t.Fatal("revoked grant still cached")
		}
	})
}

func TestAdminDestructiveActionsNeedTheRights(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		sys := context.Background()
		_, _ = e.admin.CreateRole(sys, access.RoleInput{Name: "usermgr", Policies: []access.RolePolicy{
			{Policy: access.PolicyUserManage}, {Policy: access.PolicyRoleManage}, {Policy: access.PolicyTokenManage}}})
		e.user("carol", "usermgr")
		boss := e.user("boss", "administrator")
		e.user("root", "administrator") // keeps the last-administrator guard out of this test
		bob := e.user("bob", "editor")
		cctx := e.as("carol")

		wantForbidden(t, "delete editor role", e.admin.DeleteRole(cctx, e.role("editor").ID))
		mix, _ := e.admin.CreateRole(sys, access.RoleInput{Name: "mix", Policies: []access.RolePolicy{
			{Policy: access.PolicyContentRead}, {Policy: access.PolicyUserManage}}})
		narrow := []access.RolePolicy{{Policy: access.PolicyUserManage}}
		_, err := e.admin.UpdateRole(cctx, mix.ID, access.RolePatch{Policies: &narrow})
		wantForbidden(t, "remove a policy she lacks", err)
		desc := "just a description"
		if _, err := e.admin.UpdateRole(cctx, mix.ID, access.RolePatch{Description: &desc}); err != nil {
			t.Errorf("describe: %v", err)
		}
		as, _ := e.admin.AssignmentsOf(sys, userRef(bob))
		wantForbidden(t, "delete editor assignment", e.admin.DeleteAssignment(cctx, as[0].ID))

		g, _ := e.admin.CreateGroup(sys, "admins", "")
		if _, err := e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: e.role("administrator").ID,
			Principal: store.PrincipalRef{Kind: store.PrincipalGroup, ID: g.ID}}); err != nil {
			t.Fatal(err)
		}
		if err := e.admin.AddMember(sys, g.ID, bob.ID); err != nil {
			t.Fatal(err)
		}
		wantForbidden(t, "remove from admin group", e.admin.RemoveMember(cctx, g.ID, bob.ID))
		wantForbidden(t, "delete admin group", e.admin.DeleteGroup(cctx, g.ID))

		tok, _, err := e.admin.CreateToken(sys, access.TokenInput{Name: "adm", Roles: []ids.UUID{e.role("administrator").ID}})
		if err != nil {
			t.Fatal(err)
		}
		wantForbidden(t, "revoke admin token", e.admin.RevokeToken(cctx, tok.ID))
		wantForbidden(t, "delete admin token", e.admin.DeleteToken(cctx, tok.ID))
		plain, _, _ := e.admin.CreateToken(sys, access.TokenInput{Name: "plain"})
		if err := e.admin.RevokeToken(cctx, plain.ID); err != nil {
			t.Errorf("revoke plain token: %v", err)
		}

		name := "renamed"
		_, err = e.admin.UpdateUser(cctx, boss.ID, access.UserPatch{Name: &name})
		wantForbidden(t, "rename admin", err)
		_, err = e.admin.UpdateUser(cctx, bob.ID, access.UserPatch{Name: &name})
		wantForbidden(t, "rename a group-admin", err)
		carol, _, _ := e.admin.ListUsers(sys, store.Page{})
		for _, u := range carol {
			if u.Login == "carol" {
				if _, err := e.admin.UpdateUser(cctx, u.ID, access.UserPatch{Name: &name}); err != nil {
					t.Errorf("edit own profile: %v", err)
				}
			}
		}
	})
}

func TestAdminKeepsAnAdministrator(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		sys := context.Background()
		wantLast := func(what string, err error) {
			t.Helper()
			wantInvalid(t, what, "administrator", err)
		}
		adminRole := e.role("administrator")
		boss := e.user("boss", "administrator")
		// a token with the administrator role is not a user
		if _, _, err := e.admin.CreateToken(sys, access.TokenInput{Name: "adm", Roles: []ids.UUID{adminRole.ID}}); err != nil {
			t.Fatal(err)
		}
		off := true
		_, err := e.admin.UpdateUser(sys, boss.ID, access.UserPatch{Disabled: &off})
		wantLast("disable the last admin", err)
		wantLast("delete the last admin", e.admin.DeleteUser(sys, boss.ID))
		wantLast("delete the admin role", e.admin.DeleteRole(sys, adminRole.ID))
		as, _ := e.admin.AssignmentsOf(sys, userRef(boss))
		wantLast("delete the admin assignment", e.admin.DeleteAssignment(sys, as[0].ID))
		weak := []access.RolePolicy{{Policy: access.PolicyContentRead}}
		_, err = e.admin.UpdateRole(sys, adminRole.ID, access.RolePatch{Policies: &weak})
		wantLast("take * out of the admin role", err)
		// nothing changed
		if u, _ := e.admin.GetUser(sys, boss.ID); u.Disabled {
			t.Fatal("boss was disabled")
		}
		if _, err := e.admin.RoleByName(sys, "administrator"); err != nil {
			t.Fatal(err)
		}

		// administrators through a group
		g, _ := e.admin.CreateGroup(sys, "admins", "")
		ga, err := e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: adminRole.ID,
			Principal: store.PrincipalRef{Kind: store.PrincipalGroup, ID: g.ID}})
		if err != nil {
			t.Fatal(err)
		}
		dana := e.user("dana")
		if err := e.admin.AddMember(sys, g.ID, dana.ID); err != nil {
			t.Fatal(err)
		}
		// boss may go now (dana remains)... but then dana is the last one
		if err := e.admin.DeleteUser(sys, boss.ID); err != nil {
			t.Fatalf("second admin exists: %v", err)
		}
		wantLast("remove the last group member", e.admin.RemoveMember(sys, g.ID, dana.ID))
		wantLast("delete the admin group", e.admin.DeleteGroup(sys, g.ID))
		wantLast("delete the group's admin assignment", e.admin.DeleteAssignment(sys, ga.ID))
		_, err = e.admin.UpdateUser(sys, dana.ID, access.UserPatch{Disabled: &off})
		wantLast("disable the last group admin", err)
		// new limited assignments of * are rejected
		wantInvalid(t, "limited * assignment", "limitations", assignErr(e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: adminRole.ID, Principal: userRef(dana),
			Limitations: map[string][]string{"collection": {"article"}}})))
		// a legacy restricted assignment of * (stored before the rule) does not count
		err = e.db.Tx(sys, func(tx store.Tx) error {
			return tx.Auth().CreateAssignment(sys, store.Assignment{ID: ids.UUID{15: 9}, RoleID: adminRole.ID, Principal: userRef(dana),
				Limitations: []byte(`{"collection":["article"]}`), CreatedAt: e.clk.Now()})
		})
		if err != nil {
			t.Fatal(err)
		}
		wantLast("restricted * is not an administrator", e.admin.DeleteGroup(sys, g.ID))
	})
}

func TestAdminOwnPasswordChangeKeepsCurrentSession(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		_, here, _ := e.login("alice", authPassword)
		_, other, _ := e.login("alice", authPassword)
		an, err := e.authenticate(here, "")
		if err != nil {
			t.Fatal(err)
		}
		ctx := access.WithPrincipal(context.Background(), an.Principal)
		if err := e.admin.ChangeOwnPassword(ctx, authPassword, "another long password", an.Session.IDHash); err != nil {
			t.Fatal(err)
		}
		if _, err := e.authenticate(here, ""); err != nil {
			t.Fatalf("current session ended: %v", err)
		}
		if _, err := e.authenticate(other, ""); !errors.Is(err, access.ErrUnauthenticated) {
			t.Fatalf("other session survived: %v", err)
		}
	})
}

func TestAdminSearchUsersAndAuditPrefix(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		e.user("alice")
		e.user("bob")
		sys := context.Background()
		us, total, err := e.admin.SearchUsers(sys, store.Page{}, "ALI")
		if err != nil || total != 1 || len(us) != 1 || us[0].Login != "alice" {
			t.Fatalf("search: %+v %d %v", us, total, err)
		}
		if _, total, _ = e.admin.SearchUsers(sys, store.Page{}, ""); total != 2 {
			t.Fatalf("all users: %d", total)
		}
		if got := e.audits("user.*"); len(got) < 2 {
			t.Fatalf("user.* = %d events", len(got))
		}
		if got := e.audits("nothing.*"); len(got) != 0 {
			t.Fatalf("nothing.* = %d events", len(got))
		}
	})
}

func TestAdminFirstRunSetup(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		if need, err := e.admin.NeedsSetup(ctx); err != nil || !need {
			t.Fatalf("fresh database needs setup: %v %v", need, err)
		}
		if _, err := e.admin.CompleteSetup(ctx, "", "root", authPassword); !errors.Is(err, access.ErrSetupToken) {
			t.Fatalf("no token yet: %v", err)
		}
		tok, err := e.admin.SetupToken(ctx)
		if err != nil || len(tok) < 32 {
			t.Fatalf("token %q %v", tok, err)
		}
		if again, _ := e.admin.SetupToken(ctx); again != tok {
			t.Fatal("token changed within the process")
		}
		if _, err := e.admin.CompleteSetup(ctx, tok+"x", "root", authPassword); !errors.Is(err, access.ErrSetupToken) {
			t.Fatalf("wrong token: %v", err)
		}
		// an invalid password keeps the token usable
		if _, err := e.admin.CompleteSetup(ctx, tok, "root", "short"); err == nil {
			t.Fatal("short password accepted")
		}
		u, err := e.admin.CompleteSetup(ctx, tok, "Root", authPassword)
		if err != nil || u.Login != "root" {
			t.Fatalf("setup: %v %+v", err, u)
		}
		if _, _, err := e.login("root", authPassword); err != nil {
			t.Fatal(err)
		}
		as, err := e.admin.AssignmentsOf(ctx, userRef(u))
		if err != nil || len(as) != 1 || as[0].Role != "administrator" {
			t.Fatalf("assignments %v %v", as, err)
		}
		if _, err := e.admin.CompleteSetup(ctx, tok, "second", authPassword); !errors.Is(err, access.ErrSetupToken) {
			t.Fatalf("token reused: %v", err)
		}
		if tok, err := e.admin.SetupToken(ctx); tok != "" || err != nil {
			t.Fatalf("token after setup %q %v", tok, err)
		}
		if need, _ := e.admin.NeedsSetup(ctx); need {
			t.Fatal("still needs setup")
		}
	})
}

func TestAdminSetupRefusedOnceAUserExists(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		ctx := context.Background()
		tok, err := e.admin.SetupToken(ctx)
		if err != nil || tok == "" {
			t.Fatal(tok, err)
		}
		// someone ran users:create-admin meanwhile
		if _, err := e.admin.EnsureAdmin(ctx, "ops", authPassword); err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.CompleteSetup(ctx, tok, "intruder", authPassword); !errors.Is(err, access.ErrSetupDone) {
			t.Fatalf("setup after a user exists: %v", err)
		}
		if _, err := e.admin.CompleteSetup(ctx, tok, "intruder", authPassword); !errors.Is(err, access.ErrSetupToken) {
			t.Fatalf("token kept after refusal: %v", err)
		}
	})
}

func assignErr(_ access.Assignment, err error) error { return err }

func TestAdminStarCannotBeLimited(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		sys := context.Background()
		_, err := e.admin.CreateRole(sys, access.RoleInput{Name: "bad", Policies: []access.RolePolicy{
			{Policy: access.PolicyAll, Limitations: map[string][]string{"collection": {"article"}}}}})
		wantInvalid(t, "create limited *", "policies[0].limitations", err)
		r, err := e.admin.CreateRole(sys, access.RoleInput{Name: "ed", Policies: []access.RolePolicy{{Policy: access.PolicyContentRead}}})
		if err != nil {
			t.Fatal(err)
		}
		u := e.user("erin")
		if _, err := e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: r.ID, Principal: userRef(u),
			Limitations: map[string][]string{"collection": {"article"}}}); err != nil {
			t.Fatal(err)
		}
		star := []access.RolePolicy{{Policy: access.PolicyAll}}
		_, err = e.admin.UpdateRole(sys, r.ID, access.RolePatch{Policies: &star})
		wantInvalid(t, "add * while assignments are limited", "policies", err)
		as, _ := e.admin.AssignmentsOf(sys, userRef(u))
		if err := e.admin.DeleteAssignment(sys, as[0].ID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.admin.UpdateRole(sys, r.ID, access.RolePatch{Policies: &star}); err != nil {
			t.Fatal(err)
		}
	})
}
