package access

import (
	"context"
	"encoding/json"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// selfKept are the rights people cannot take away from themselves, with the refusal for each.
var selfKept = []struct{ policy, msg string }{
	{PolicyRoleManage, "this would remove your permission to manage roles"},
	{PolicyUserManage, "this would remove your permission to manage users and groups"},
}

// heldUnrestricted reports which of selfKept's policies a user holds (or "*") with no limitation, directly or
// through groups, reading the state of the open transaction.
func heldUnrestricted(ctx context.Context, as store.AuthStore, user ids.UUID) ([]bool, error) {
	refs := []store.PrincipalRef{{Kind: store.PrincipalUser, ID: user}}
	gs, err := as.GroupsOf(ctx, user)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		refs = append(refs, store.PrincipalRef{Kind: store.PrincipalGroup, ID: g})
	}
	rgs, err := as.AssignmentsFor(ctx, refs)
	if err != nil {
		return nil, err
	}
	held := make([]bool, len(selfKept))
	for _, g := range rgs {
		var lim map[string][]string
		if len(g.Assignment.Limitations) > 0 {
			if err := json.Unmarshal(g.Assignment.Limitations, &lim); err != nil {
				return nil, err
			}
		}
		if len(lim) > 0 {
			continue
		}
		var ps []RolePolicy
		if err := json.Unmarshal(g.Role.Policies, &ps); err != nil {
			return nil, err
		}
		for _, p := range ps {
			if len(p.Limitations) > 0 {
				continue
			}
			for i, k := range selfKept {
				if p.Policy == k.policy || p.Policy == PolicyAll {
					held[i] = true
				}
			}
		}
	}
	return held, nil
}

// keepSelf runs a change and refuses it (rolling the transaction back) when the signed-in user held role.manage
// or user.manage before and no longer does after it: people may change their own roles and groups, but not cut
// off the rights that let them do so. The system, super principals and tokens are not guarded.
func keepSelf(ctx context.Context, as store.AuthStore, change func() error) error {
	p := PrincipalFrom(ctx)
	if p == nil || p.Super || p.Kind != KindUser {
		return change()
	}
	before, err := heldUnrestricted(ctx, as, p.ID)
	if err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	after, err := heldUnrestricted(ctx, as, p.ID)
	if err != nil {
		return err
	}
	for i, k := range selfKept {
		if before[i] && !after[i] {
			return invalid("administrator", k.msg)
		}
	}
	return nil
}
