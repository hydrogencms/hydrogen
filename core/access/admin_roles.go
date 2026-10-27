package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Role is a role as admins see it.
type Role struct {
	ID          ids.UUID     `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Policies    []RolePolicy `json:"policies"`
	Template    string       `json:"template,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

func roleView(r store.Role) (Role, error) {
	out := Role{ID: r.ID, Name: r.Name, Description: r.Description, Template: r.Template, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	if err := json.Unmarshal(r.Policies, &out.Policies); err != nil {
		return Role{}, err
	}
	if out.Policies == nil {
		out.Policies = []RolePolicy{}
	}
	return out, nil
}

// Assignment is a role given to a user, group or token.
type Assignment struct {
	ID          ids.UUID            `json:"id"`
	RoleID      ids.UUID            `json:"roleId"`
	Role        string              `json:"role,omitempty"`
	Kind        store.PrincipalKind `json:"principalKind"`
	PrincipalID ids.UUID            `json:"principalId"`
	Limitations map[string][]string `json:"limitations"`
	CreatedAt   time.Time           `json:"createdAt"`
}

func assignmentView(a store.Assignment, role string) (Assignment, error) {
	out := Assignment{ID: a.ID, RoleID: a.RoleID, Role: role, Kind: a.Principal.Kind, PrincipalID: a.Principal.ID,
		Limitations: map[string][]string{}, CreatedAt: a.CreatedAt}
	if len(a.Limitations) > 0 {
		if err := json.Unmarshal(a.Limitations, &out.Limitations); err != nil {
			return Assignment{}, err
		}
	}
	return out, nil
}

type RoleInput struct {
	Name        string
	Description string
	Policies    []RolePolicy
}

type RolePatch struct {
	Name        *string
	Description *string
	Policies    *[]RolePolicy
}

type AssignmentInput struct {
	RoleID      ids.UUID
	Principal   store.PrincipalRef
	Limitations map[string][]string
}

func encodePolicies(ps []RolePolicy) []byte {
	if ps == nil {
		ps = []RolePolicy{}
	}
	b, _ := json.Marshal(ps)
	return b
}

func encodeLimits(l map[string][]string) []byte {
	if len(l) == 0 {
		return []byte("{}")
	}
	b, _ := json.Marshal(l)
	return b
}

func policyNames(ps []RolePolicy) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Policy
	}
	return out
}

func (a *Admin) CreateRole(ctx context.Context, in RoleInput) (Role, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return Role{}, err
	}
	name := strings.TrimSpace(in.Name)
	if err := checkName("name", name); err != nil {
		return Role{}, err
	}
	if err := a.catalog.ValidateRole(ctx, in.Policies); err != nil {
		return Role{}, err
	}
	if err := a.requireHeld(ctx, policyNames(in.Policies)); err != nil {
		return Role{}, err
	}
	now := a.clock.Now()
	r := store.Role{ID: a.ids.New(), Name: name, Description: in.Description, Policies: encodePolicies(in.Policies), CreatedAt: now, UpdatedAt: now}
	err := a.mutate(ctx, "role.create", r.ID.String(), func(tx store.Tx) (any, error) {
		return map[string]any{"name": name, "policies": policyNames(in.Policies)}, tx.Auth().CreateRole(ctx, r)
	})
	if err != nil {
		return Role{}, err
	}
	return roleView(r)
}

func (a *Admin) UpdateRole(ctx context.Context, id ids.UUID, p RolePatch) (Role, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return Role{}, err
	}
	if p.Policies != nil {
		if err := a.catalog.ValidateRole(ctx, *p.Policies); err != nil {
			return Role{}, err
		}
	}
	h, err := a.holder(ctx)
	if err != nil {
		return Role{}, err
	}
	if p.Policies != nil {
		if err := h.require(policyNames(*p.Policies)); err != nil {
			return Role{}, err
		}
	}
	var out store.Role
	err = a.mutate(ctx, "role.update", id.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		r, err := as.GetRole(ctx, id)
		if err != nil {
			return nil, err
		}
		var fields []string
		if p.Name != nil {
			n := strings.TrimSpace(*p.Name)
			if err := checkName("name", n); err != nil {
				return nil, err
			}
			r.Name = n
			fields = append(fields, "name")
		}
		if p.Description != nil {
			r.Description = *p.Description
			fields = append(fields, "description")
		}
		save := func() error { return as.UpdateRole(ctx, r) }
		if p.Policies != nil {
			old, err := rolePolicyNames(r.Policies)
			if err != nil {
				return nil, err
			}
			next := policyNames(*p.Policies)
			var removed []string
			for _, n := range old {
				if !slices.Contains(next, n) {
					removed = append(removed, n)
				}
			}
			if err := h.require(removed); err != nil {
				return nil, err
			}
			if slices.Contains(next, PolicyAll) && !slices.Contains(old, PolicyAll) {
				asg, err := as.RoleAssignments(ctx, id)
				if err != nil {
					return nil, err
				}
				for _, x := range asg {
					var lim map[string][]string
					if len(x.Limitations) > 0 && json.Unmarshal(x.Limitations, &lim) == nil && len(lim) > 0 {
						return nil, invalid("policies", "the everything policy cannot be added while assignments of this role carry limitations; remove them first")
					}
				}
			}
			r.Policies = encodePolicies(*p.Policies)
			fields = append(fields, "policies")
			save = func() error {
				return keepAdmin(ctx, as, func() error {
					return keepSelf(ctx, as, func() error { return as.UpdateRole(ctx, r) })
				})
			}
		}
		r.UpdatedAt = a.clock.Now()
		out = r
		return map[string]any{"name": r.Name, "fields": fields}, save()
	})
	if err != nil {
		return Role{}, err
	}
	return roleView(out)
}

func (a *Admin) DeleteRole(ctx context.Context, id ids.UUID) error {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return err
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "role.delete", id.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		r, err := as.GetRole(ctx, id)
		if err != nil {
			return nil, err
		}
		names, err := rolePolicyNames(r.Policies)
		if err != nil {
			return nil, err
		}
		if err := h.require(names); err != nil {
			return nil, err
		}
		err = keepAdmin(ctx, as, func() error {
			return keepSelf(ctx, as, func() error { return as.DeleteRole(ctx, id) })
		})
		return map[string]any{"name": r.Name}, err
	})
}

func (a *Admin) GetRole(ctx context.Context, id ids.UUID) (Role, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return Role{}, err
	}
	var r store.Role
	if err := a.view(ctx, func(as store.AuthStore) (err error) { r, err = as.GetRole(ctx, id); return }); err != nil {
		return Role{}, err
	}
	return roleView(r)
}

func (a *Admin) RoleByName(ctx context.Context, name string) (Role, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return Role{}, err
	}
	var r store.Role
	if err := a.view(ctx, func(as store.AuthStore) (err error) { r, err = as.RoleByName(ctx, name); return }); err != nil {
		return Role{}, err
	}
	return roleView(r)
}

func (a *Admin) ListRoles(ctx context.Context, p store.Page) ([]Role, int, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return nil, 0, err
	}
	var rs []store.Role
	var total int
	if err := a.view(ctx, func(as store.AuthStore) (err error) { rs, total, err = as.ListRoles(ctx, page(p)); return }); err != nil {
		return nil, 0, err
	}
	out := make([]Role, len(rs))
	for i, r := range rs {
		v, err := roleView(r)
		if err != nil {
			return nil, 0, err
		}
		out[i] = v
	}
	return out, total, nil
}

func (a *Admin) RoleAssignments(ctx context.Context, role ids.UUID) ([]Assignment, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return nil, err
	}
	var as []store.Assignment
	var r store.Role
	err := a.view(ctx, func(s store.AuthStore) (err error) {
		if r, err = s.GetRole(ctx, role); err != nil {
			return err
		}
		as, err = s.RoleAssignments(ctx, role)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Assignment, len(as))
	for i, x := range as {
		if out[i], err = assignmentView(x, r.Name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// AssignmentsOf lists the roles given to one principal.
func (a *Admin) AssignmentsOf(ctx context.Context, ref store.PrincipalRef) ([]Assignment, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return nil, err
	}
	var gs []store.RoleGrant
	if err := a.view(ctx, func(s store.AuthStore) (err error) {
		gs, err = s.AssignmentsFor(ctx, []store.PrincipalRef{ref})
		return
	}); err != nil {
		return nil, err
	}
	out := make([]Assignment, len(gs))
	for i, g := range gs {
		v, err := assignmentView(g.Assignment, g.Role.Name)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// principalExists loads the principal of an assignment inside tx.
func principalExists(ctx context.Context, tx store.Tx, ref store.PrincipalRef) error {
	var err error
	switch ref.Kind {
	case store.PrincipalUser:
		_, err = tx.Auth().GetUser(ctx, ref.ID)
	case store.PrincipalGroup:
		_, err = tx.Auth().GetGroup(ctx, ref.ID)
	case store.PrincipalToken:
		_, err = tx.Auth().GetToken(ctx, ref.ID)
	default:
		return invalid("principalKind", "must be user, group or token")
	}
	if errors.Is(err, store.ErrNotFound) {
		return invalid("principalId", "not found")
	}
	return err
}

// CreateAssignment gives a role to a principal. The caller needs role.manage, must hold every policy of the
// role unrestricted.
func (a *Admin) CreateAssignment(ctx context.Context, in AssignmentInput) (Assignment, error) {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return Assignment{}, err
	}
	var role store.Role
	if err := a.view(ctx, func(s store.AuthStore) (err error) { role, err = s.GetRole(ctx, in.RoleID); return }); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return Assignment{}, invalid("roleId", "not found")
		}
		return Assignment{}, err
	}
	var policies []RolePolicy
	if err := json.Unmarshal(role.Policies, &policies); err != nil {
		return Assignment{}, err
	}
	if err := a.catalog.ValidateAssignment(ctx, policies, in.Limitations); err != nil {
		return Assignment{}, err
	}
	if err := a.requireHeld(ctx, policyNames(policies)); err != nil {
		return Assignment{}, err
	}
	as := store.Assignment{ID: a.ids.New(), RoleID: role.ID, Principal: in.Principal, Limitations: encodeLimits(in.Limitations), CreatedAt: a.clock.Now()}
	err := a.mutate(ctx, "assignment.create", as.ID.String(), func(tx store.Tx) (any, error) {
		if err := principalExists(ctx, tx, in.Principal); err != nil {
			return nil, err
		}
		if err := roleUnchanged(ctx, tx, role); err != nil {
			return nil, err
		}
		data := map[string]any{"role": role.Name, "principalKind": in.Principal.Kind, "principalId": in.Principal.ID.String(), "limitations": json.RawMessage(as.Limitations)}
		return data, tx.Auth().CreateAssignment(ctx, as)
	})
	if err != nil {
		var ie *InvalidError
		if errors.As(err, &ie) && ie.Fields["name"] == dupMessage {
			return Assignment{}, invalid("roleId", "already assigned")
		}
		return Assignment{}, err
	}
	return assignmentView(as, role.Name)
}

func (a *Admin) DeleteAssignment(ctx context.Context, id ids.UUID) error {
	if err := a.can(ctx, PolicyRoleManage); err != nil {
		return err
	}
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return a.mutate(ctx, "assignment.delete", id.String(), func(tx store.Tx) (any, error) {
		as := tx.Auth()
		cur, err := as.GetAssignment(ctx, id)
		if err != nil {
			return nil, err
		}
		role, err := as.GetRole(ctx, cur.RoleID)
		if err != nil {
			return nil, err
		}
		names, err := rolePolicyNames(role.Policies)
		if err != nil {
			return nil, err
		}
		if err := h.require(names); err != nil {
			return nil, err
		}
		data := map[string]any{"role": role.Name, "roleId": cur.RoleID.String(), "principalKind": cur.Principal.Kind, "principalId": cur.Principal.ID.String()}
		return data, keepAdmin(ctx, as, func() error {
			return keepSelf(ctx, as, func() error { return as.DeleteAssignment(ctx, id) })
		})
	})
}

// roleUnchanged fails when the role's policies changed since the caller's rights were checked against them,
// so a concurrent role edit cannot widen what an assignment hands out.
func roleUnchanged(ctx context.Context, tx store.Tx, seen store.Role) error {
	cur, err := tx.Auth().GetRole(ctx, seen.ID)
	if errors.Is(err, store.ErrNotFound) {
		return invalid("roleId", "not found")
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(cur.Policies, seen.Policies) {
		return invalid("roleId", "the role changed meanwhile; try again")
	}
	return nil
}
