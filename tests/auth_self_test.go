package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Users may change their own roles and groups, but not remove what lets them do it.
func TestAdminSelfRolesAndGroups(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newAuthEnv(t, b, nil)
		sys := context.Background()
		role := func(name string, ps ...string) access.Role {
			var rp []access.RolePolicy
			for _, p := range ps {
				rp = append(rp, access.RolePolicy{Policy: p})
			}
			r, err := e.admin.CreateRole(sys, access.RoleInput{Name: name, Policies: rp})
			if err != nil {
				t.Fatal(err)
			}
			return r
		}
		group := func(name string, roles ...access.Role) store.Group {
			g, err := e.admin.CreateGroup(sys, name, "")
			if err != nil {
				t.Fatal(err)
			}
			for _, r := range roles {
				if _, err := e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: r.ID,
					Principal: store.PrincipalRef{Kind: store.PrincipalGroup, ID: g.ID}}); err != nil {
					t.Fatal(err)
				}
			}
			return g
		}
		assign := func(r access.Role, u access.User) {
			t.Helper()
			if _, err := e.admin.CreateAssignment(sys, access.AssignmentInput{RoleID: r.ID, Principal: userRef(u)}); err != nil {
				t.Fatal(err)
			}
		}
		assignments := func(u access.User) map[string]access.Assignment {
			as, err := e.admin.AssignmentsOf(sys, userRef(u))
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]access.Assignment{}
			for _, a := range as {
				m[a.Role] = a
			}
			return m
		}
		roleMgr := role("rolemgr", access.PolicyRoleManage)
		roleMgr2 := role("rolemgr2", access.PolicyRoleManage)
		userMgr := role("usermgr2", access.PolicyUserManage)
		tokens := role("tokens", access.PolicyTokenManage)
		wide := role("wide", access.PolicyAll)
		boss := e.user("boss", "administrator")

		t.Run("add role to self", func(t *testing.T) {
			carol := e.user("carol", "rolemgr")
			cctx := e.as("carol")
			_, err := e.admin.CreateAssignment(cctx, access.AssignmentInput{RoleID: wide.ID, Principal: userRef(carol)})
			wantForbidden(t, "self escalate", err)
			_, err = e.admin.CreateAssignment(cctx, access.AssignmentInput{RoleID: tokens.ID, Principal: userRef(carol)})
			wantForbidden(t, "self add unheld policy", err)
			if _, err := e.admin.CreateAssignment(cctx, access.AssignmentInput{RoleID: roleMgr2.ID, Principal: userRef(carol)}); err != nil {
				t.Fatalf("self add held role: %v", err)
			}
			as := assignments(carol)
			if len(as) != 2 {
				t.Fatalf("assignments %v", as)
			}
			// with two sources either may go; the last one may not
			if err := e.admin.DeleteAssignment(cctx, as["rolemgr2"].ID); err != nil {
				t.Fatalf("remove spare source: %v", err)
			}
			err = e.admin.DeleteAssignment(cctx, as["rolemgr"].ID)
			wantInvalid(t, "remove only source", "administrator", err)
			if err == nil || !strings.Contains(err.Error(), "manage roles") {
				t.Errorf("message: %v", err)
			}
			if len(assignments(carol)) != 1 {
				t.Fatal("refused removal was not rolled back")
			}
		})

		t.Run("remove unrelated role from self", func(t *testing.T) {
			dave := e.user("dave", "rolemgr")
			assign(tokens, dave)
			dctx := e.as("dave")
			if err := e.admin.DeleteAssignment(dctx, assignments(dave)["tokens"].ID); err != nil {
				t.Fatalf("remove unrelated: %v", err)
			}
		})

		t.Run("group assignment as the only source", func(t *testing.T) {
			erin := e.user("erin")
			g := group("rolegroup", roleMgr)
			if err := e.admin.AddMember(sys, g.ID, erin.ID); err != nil {
				t.Fatal(err)
			}
			assign(userMgr, erin)
			ectx := e.as("erin")
			ga, _ := e.admin.AssignmentsOf(sys, store.PrincipalRef{Kind: store.PrincipalGroup, ID: g.ID})
			wantInvalid(t, "remove group role", "administrator", e.admin.DeleteAssignment(ectx, ga[0].ID))
			wantInvalid(t, "delete the role", "administrator", e.admin.DeleteRole(ectx, roleMgr.ID))
			wantInvalid(t, "delete the group", "administrator", e.admin.DeleteGroup(ectx, g.ID))
			assign(roleMgr2, erin)
			if err := e.admin.DeleteAssignment(ectx, ga[0].ID); err != nil {
				t.Fatalf("remove with a second source: %v", err)
			}
		})

		t.Run("groups and user.manage", func(t *testing.T) {
			fred := e.user("fred")
			g := group("usergroup", userMgr)
			plain := group("plain")
			if err := e.admin.AddMember(sys, g.ID, fred.ID); err != nil {
				t.Fatal(err)
			}
			fctx := e.as("fred")
			if err := e.admin.AddMember(fctx, plain.ID, fred.ID); err != nil {
				t.Fatalf("self join: %v", err)
			}
			wantForbidden(t, "self join unheld", e.admin.AddMember(fctx, group("tokengroup", tokens).ID, fred.ID))
			if err := e.admin.RemoveMember(fctx, plain.ID, fred.ID); err != nil {
				t.Fatalf("self leave: %v", err)
			}
			err := e.admin.RemoveMember(fctx, g.ID, fred.ID)
			wantInvalid(t, "leave only source", "administrator", err)
			if err == nil || !strings.Contains(err.Error(), "manage users") {
				t.Errorf("message: %v", err)
			}
			wantInvalid(t, "delete only source group", "administrator", e.admin.DeleteGroup(fctx, g.ID))
			if gs, _ := e.admin.GroupsOf(sys, fred.ID); len(gs) != 1 {
				t.Fatalf("groups %v", gs)
			}
			assign(userMgr, fred)
			if err := e.admin.RemoveMember(fctx, g.ID, fred.ID); err != nil {
				t.Fatalf("leave with a second source: %v", err)
			}
		})

		t.Run("each path keeps both rights", func(t *testing.T) {
			gina := e.user("gina")
			g := group("rolegroup2", roleMgr)
			if err := e.admin.AddMember(sys, g.ID, gina.ID); err != nil {
				t.Fatal(err)
			}
			assign(userMgr, gina)
			gctx := e.as("gina")
			err := e.admin.RemoveMember(gctx, g.ID, gina.ID)
			wantInvalid(t, "leave the only role.manage group", "administrator", err)
			if err == nil || !strings.Contains(err.Error(), "manage roles") {
				t.Errorf("message: %v", err)
			}
			err = e.admin.DeleteAssignment(gctx, assignments(gina)["usermgr2"].ID)
			wantInvalid(t, "remove the only user.manage role", "administrator", err)
			if err == nil || !strings.Contains(err.Error(), "manage users") {
				t.Errorf("message: %v", err)
			}
		})

		t.Run("last administrator", func(t *testing.T) {
			actx := e.as("boss")
			wantInvalid(t, "remove last admin", "administrator", e.admin.DeleteAssignment(actx, assignments(boss)["administrator"].ID))
		})
	})
}
