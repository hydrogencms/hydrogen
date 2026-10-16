package review

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

const maxApprovers = 500

// Approver is a user or group that can approve an item.
type Approver struct {
	Ref  string `json:"ref"` // user:<id> or group:<id>
	Name string `json:"name"`
	Kind string `json:"kind"` // user or group
}

// Approvers lists the users and groups that can approve the item: they hold review.approve (and may read it)
// through their roles, with the item's type, collection and rule limitations applied, sorted by name. The caller
// is left out. Needs review.submit on the item.
func (s *Service) Approvers(ctx context.Context, id ids.UUID) ([]Approver, error) {
	if err := s.o.Content.Prepare(ctx); err != nil {
		return nil, err
	}
	out := []Approver{}
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := s.o.Content.LoadItemTx(ctx, tx, id, access.PolicyReviewSubmit, false)
		if err != nil {
			return err
		}
		if err := s.required(it); err != nil {
			return err
		}
		users, groups, err := s.approverCandidates(ctx, tx)
		if err != nil {
			return err
		}
		self := ""
		if p := access.PrincipalFrom(ctx); p != nil && p.Kind == access.KindUser && !p.ID.IsZero() {
			self = p.ID.String()
		}
		can := func(p *access.Principal) (bool, error) {
			got, err := s.o.Content.AllowedTx(access.WithPrincipal(ctx, p), tx, it, access.PolicyContentRead, access.PolicyReviewApprove)
			return err == nil && got[access.PolicyContentRead] && got[access.PolicyReviewApprove], err
		}
		for _, uid := range users {
			if uid.String() == self {
				continue
			}
			u, err := tx.Auth().GetUser(ctx, uid)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				return err
			}
			if u.Disabled {
				continue
			}
			gs, err := tx.Auth().GroupsOf(ctx, uid)
			if err != nil {
				return err
			}
			ok, err := can(&access.Principal{Kind: access.KindUser, ID: uid, Name: u.Login, Groups: gs})
			if err != nil {
				return err
			}
			if ok {
				out = append(out, Approver{Ref: "user:" + uid.String(), Name: cmp.Or(u.Name, u.Login), Kind: "user"})
			}
		}
		for _, gid := range groups {
			g, err := tx.Auth().GetGroup(ctx, gid)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					continue
				}
				return err
			}
			// A stand-in that holds what the group's roles grant (its id doubles as the principal id).
			ok, err := can(&access.Principal{Kind: access.KindUser, ID: gid, Name: g.Name, Groups: []ids.UUID{gid}})
			if err != nil {
				return err
			}
			if ok {
				out = append(out, Approver{Ref: "group:" + gid.String(), Name: g.Name, Kind: "group"})
			}
		}
		return nil
	})
	slices.SortFunc(out, func(a, b Approver) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.Ref, b.Ref))
	})
	return out, err
}

// approverCandidates are the users and groups assigned a role that has review.approve (or everything), and the
// members of those groups, each once.
func (s *Service) approverCandidates(ctx context.Context, tx store.Tx) (users, groups []ids.UUID, err error) {
	a := tx.Auth()
	var roles []store.Role
	for off := 0; ; off += 200 {
		page, _, err := a.ListRoles(ctx, store.Page{Limit: 200, Offset: off})
		if err != nil {
			return nil, nil, err
		}
		roles = append(roles, page...)
		if len(page) < 200 {
			break
		}
	}
	seenU, seenG := map[ids.UUID]bool{}, map[ids.UUID]bool{}
	var groupList []ids.UUID
	for _, r := range roles {
		var ps []access.RolePolicy
		if json.Unmarshal(r.Policies, &ps) != nil || !slices.ContainsFunc(ps, func(p access.RolePolicy) bool {
			return p.Policy == access.PolicyReviewApprove || p.Policy == access.PolicyAll
		}) {
			continue
		}
		as, err := a.RoleAssignments(ctx, r.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, as := range as {
			switch as.Principal.Kind {
			case store.PrincipalUser:
				if !seenU[as.Principal.ID] {
					seenU[as.Principal.ID] = true
					users = append(users, as.Principal.ID)
				}
			case store.PrincipalGroup:
				if !seenG[as.Principal.ID] {
					seenG[as.Principal.ID] = true
					groupList = append(groupList, as.Principal.ID)
				}
			}
		}
	}
	for _, gid := range groupList {
		for off := 0; len(users) < maxApprovers; off += 200 {
			page, _, err := a.Members(ctx, gid, store.Page{Limit: 200, Offset: off})
			if err != nil {
				return nil, nil, err
			}
			for _, u := range page {
				if !seenU[u.ID] {
					seenU[u.ID] = true
					users = append(users, u.ID)
				}
			}
			if len(page) < 200 {
				break
			}
		}
	}
	if len(users) > maxApprovers {
		users = users[:maxApprovers]
	}
	return users, groupList, nil
}
