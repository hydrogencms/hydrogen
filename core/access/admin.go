package access

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/store"
)

// TopicAuthChanged is the outbox topic of every admin mutation.
const TopicAuthChanged = "auth.changed"

const dupMessage = "already exists"

const (
	maxNameLen  = 200
	maxLoginCap = 64
	maxPageSize = 500
)

// RoleValidator is the part of Catalog the admin service needs.
type RoleValidator interface {
	ValidateRole(ctx context.Context, ps []RolePolicy) error
	ValidateAssignment(ctx context.Context, role []RolePolicy, lim map[string][]string) error
}

// Authorizer is the part of Checker the admin service needs.
type Authorizer interface {
	Can(ctx context.Context, policy string, t Target) error
	Grants(ctx context.Context, p *Principal) ([]Grant, error)
	Invalidate()
}

type AdminOptions struct {
	DB      store.DB
	Outbox  *outbox.Outbox
	Audit   *audit.Log
	Clock   clock.Clock // default clock.System
	IDs     *ids.Generator
	Rand    io.Reader // default crypto/rand
	Catalog RoleValidator
	Checker Authorizer
	Log     *slog.Logger
	// PasswordIterations is the PBKDF2 cost of new hashes (0 = 600 000); tests lower it.
	PasswordIterations int
	// SetupSeed, when set, derives a stable setup token from it instead of a random one per process: dev passes
	// the dev token, so the setup link survives the restarts of live reload.
	SetupSeed string
}

// Admin manages users, groups, roles, assignments and API tokens. Every method checks the caller's policy in
// ctx (no principal = system) and every mutation is one transaction with an audit entry, an auth.changed outbox
// event and a NotifyChannel notification.
type Admin struct {
	db      store.DB
	outbox  *outbox.Outbox
	audit   *audit.Log
	clock   clock.Clock
	ids     *ids.Generator
	rand    io.Reader
	catalog RoleValidator
	checker Authorizer
	log     *slog.Logger
	pwThr   *throttle
	verify  verifyGate
	iter    int
	setup   setupState
	seed    string
}

func NewAdmin(o AdminOptions) (*Admin, error) {
	switch {
	case o.DB == nil, o.Outbox == nil, o.Audit == nil, o.IDs == nil, o.Catalog == nil, o.Checker == nil:
		return nil, errors.New("access: admin needs DB, Outbox, Audit, IDs, Catalog and Checker")
	}
	a := &Admin{db: o.DB, outbox: o.Outbox, audit: o.Audit, clock: o.Clock, ids: o.IDs, rand: o.Rand,
		catalog: o.Catalog, checker: o.Checker, log: o.Log, pwThr: newThrottle(throttleKeys, limitPair), verify: newVerifyGate(0), iter: iterationsOr(o.PasswordIterations), seed: o.SetupSeed}
	if a.clock == nil {
		a.clock = clock.System{}
	}
	if a.rand == nil {
		a.rand = rand.Reader
	}
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	return a, nil
}

func mustJSON(v any) []byte {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

func actorOf(ctx context.Context) audit.Actor {
	if p := PrincipalFrom(ctx); p != nil {
		return p.Actor()
	}
	return audit.System
}

func isSelf(ctx context.Context, user ids.UUID) bool {
	p := PrincipalFrom(ctx)
	return p != nil && p.Kind == KindUser && p.ID == user
}

func (a *Admin) can(ctx context.Context, policy string) error {
	return a.checker.Can(ctx, policy, Target{})
}

func invalid(field, msg string) error {
	return &InvalidError{Fields: map[string]string{field: msg}}
}

func checkName(field, s string) error {
	if strings.TrimSpace(s) == "" {
		return invalid(field, "required")
	}
	if utf8.RuneCountInString(s) > maxNameLen {
		return invalid(field, fmt.Sprintf("at most %d characters", maxNameLen))
	}
	return nil
}

func checkLogin(s string) error {
	if s == "" || len(s) > maxLoginCap {
		return invalid("login", fmt.Sprintf("1 to %d characters", maxLoginCap))
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && !strings.ContainsRune("._@-", c) {
			return invalid("login", "lower-case letters, digits and . _ @ - only")
		}
	}
	return nil
}

func checkEmail(s string) error {
	if s == "" {
		return nil
	}
	if len(s) > 254 || !strings.Contains(s, "@") || strings.ContainsAny(s, " \t\r\n") {
		return invalid("email", "not an email address")
	}
	return nil
}

func checkPassword(field, pw string) error {
	if err := ValidatePassword(pw); err != nil {
		return invalid(field, err.Error())
	}
	return nil
}

func page(p store.Page) store.Page {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	p.Limit = min(p.Limit, maxPageSize)
	p.Offset = max(p.Offset, 0)
	return p
}

// mutate runs fn (which returns the audit data), the audit entry and the outbox event in one transaction, and notifies other nodes. A
// duplicate key becomes a field error.
func (a *Admin) mutate(ctx context.Context, action, subject string, fn func(store.Tx) (any, error)) error {
	return a.mutateAs(ctx, actorOf(ctx), action, subject, fn)
}

func (a *Admin) mutateAs(ctx context.Context, actor audit.Actor, action, subject string, fn func(store.Tx) (any, error)) error {
	err := a.db.Tx(ctx, func(tx store.Tx) error {
		data, err := fn(tx)
		if err != nil {
			return err
		}
		if err := a.audit.Write(ctx, tx, audit.Entry{Actor: actor, Action: action, Subject: subject, Data: data}); err != nil {
			return err
		}
		if _, err := a.outbox.Publish(ctx, tx, TopicAuthChanged, map[string]string{"action": action, "subject": subject}); err != nil {
			return err
		}
		return tx.Notify(NotifyChannel, "")
	})
	if err != nil {
		return duplicateAsInvalid(err)
	}
	a.checker.Invalidate()
	return nil
}

func duplicateAsInvalid(err error) error {
	if errors.Is(err, store.ErrDuplicateKey) {
		return invalid("name", dupMessage)
	}
	return err
}

func (a *Admin) view(ctx context.Context, fn func(store.AuthStore) error) error {
	return a.db.View(ctx, func(tx store.Tx) error { return fn(tx.Auth()) })
}

// holder is what the caller holds unrestricted, loaded once before a transaction starts (the checker reads
// through its own connection, which must not happen inside one).
type holder struct {
	exempt bool
	all    bool
	set    map[string]bool
}

func (a *Admin) holder(ctx context.Context) (holder, error) {
	p := PrincipalFrom(ctx)
	if p == nil || p.Super {
		return holder{exempt: true}, nil
	}
	grants, err := a.checker.Grants(ctx, p)
	if err != nil {
		return holder{}, err
	}
	h := holder{set: map[string]bool{}}
	for _, g := range grants {
		if len(g.Limits) == 0 {
			h.set[g.Policy] = true
		}
	}
	h.all = h.set[PolicyAll]
	return h, nil
}

// require refuses unless every policy is held unrestricted (or "*" is); the system and super pass.
func (h holder) require(policies []string) error {
	if h.exempt || h.all {
		return nil
	}
	for _, n := range policies {
		if !h.set[n] {
			return fmt.Errorf("%w: cannot grant %s", ErrForbidden, n)
		}
	}
	return nil
}

func (a *Admin) requireHeld(ctx context.Context, policies []string) error {
	h, err := a.holder(ctx)
	if err != nil {
		return err
	}
	return h.require(policies)
}

func rolePolicyNames(raw []byte) ([]string, error) {
	var ps []RolePolicy
	if err := json.Unmarshal(raw, &ps); err != nil {
		return nil, fmt.Errorf("access: role policies: %w", err)
	}
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Policy
	}
	return names, nil
}

// policiesIn lists the policy names the principals hold through their role assignments.
func policiesIn(ctx context.Context, as store.AuthStore, refs ...store.PrincipalRef) ([]string, error) {
	gs, err := as.AssignmentsFor(ctx, refs)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, g := range gs {
		names, err := rolePolicyNames(g.Role.Policies)
		if err != nil {
			return nil, err
		}
		out = append(out, names...)
	}
	return out, nil
}

// userPoliciesIn lists the policies a user holds directly and through groups.
func userPoliciesIn(ctx context.Context, as store.AuthStore, user ids.UUID) ([]string, error) {
	refs := []store.PrincipalRef{{Kind: store.PrincipalUser, ID: user}}
	gs, err := as.GroupsOf(ctx, user)
	if err != nil {
		return nil, err
	}
	for _, g := range gs {
		refs = append(refs, store.PrincipalRef{Kind: store.PrincipalGroup, ID: g})
	}
	return policiesIn(ctx, as, refs...)
}

// requireOutranks refuses actions on a user whose privileges the caller doesn't fully hold, so user.manage
// cannot be used to take over or lock out a more privileged account.
func (h holder) requireOutranks(ctx context.Context, as store.AuthStore, user ids.UUID) error {
	if h.exempt || h.all {
		return nil
	}
	pols, err := userPoliciesIn(ctx, as, user)
	if err != nil {
		return err
	}
	return h.require(pols)
}

var errLastAdmin = invalid("administrator", "this would leave no enabled administrator")

// hasAdmin reports whether an enabled user holds "*" unrestricted, directly or through a group. Tokens don't
// count.
func hasAdmin(ctx context.Context, as store.AuthStore) (bool, error) {
	for off := 0; ; off += maxPageSize {
		roles, total, err := as.ListRoles(ctx, store.Page{Limit: maxPageSize, Offset: off})
		if err != nil {
			return false, err
		}
		for _, r := range roles {
			var ps []RolePolicy
			if err := json.Unmarshal(r.Policies, &ps); err != nil {
				return false, err
			}
			if !slices.ContainsFunc(ps, func(p RolePolicy) bool { return p.Policy == PolicyAll && len(p.Limitations) == 0 }) {
				continue
			}
			if ok, err := roleHasAdmin(ctx, as, r.ID); ok || err != nil {
				return ok, err
			}
		}
		if off+maxPageSize >= total {
			return false, nil
		}
	}
}

func roleHasAdmin(ctx context.Context, as store.AuthStore, role ids.UUID) (bool, error) {
	assigns, err := as.RoleAssignments(ctx, role)
	if err != nil {
		return false, err
	}
	for _, a := range assigns {
		var lim map[string][]string
		if len(a.Limitations) > 0 {
			if err := json.Unmarshal(a.Limitations, &lim); err != nil {
				return false, err
			}
		}
		if len(lim) > 0 {
			continue
		}
		switch a.Principal.Kind {
		case store.PrincipalUser:
			u, err := as.GetUser(ctx, a.Principal.ID)
			if err == nil && !u.Disabled {
				return true, nil
			}
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return false, err
			}
		case store.PrincipalGroup:
			for off := 0; ; off += maxPageSize {
				us, total, err := as.Members(ctx, a.Principal.ID, store.Page{Limit: maxPageSize, Offset: off})
				if err != nil {
					return false, err
				}
				if slices.ContainsFunc(us, func(u store.User) bool { return !u.Disabled }) {
					return true, nil
				}
				if off+maxPageSize >= total {
					break
				}
			}
		}
	}
	return false, nil
}

// keepAdmin runs a change and refuses it (rolling the transaction back) when it removed the last enabled
// administrator. Without any administrator beforehand (bootstrapped by the config token) nothing is guarded.
func keepAdmin(ctx context.Context, as store.AuthStore, change func() error) error {
	before, err := hasAdmin(ctx, as)
	if err != nil {
		return err
	}
	if err := change(); err != nil {
		return err
	}
	if !before {
		return nil
	}
	after, err := hasAdmin(ctx, as)
	if err != nil {
		return err
	}
	if !after {
		return errLastAdmin
	}
	return nil
}
