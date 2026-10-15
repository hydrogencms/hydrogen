package access

// Placeholders for workstream T3a (planning/design/auth-h5a.md); replace them with the implementation and delete
// this file.

import (
	"context"
	"log/slog"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

func WithPrincipal(ctx context.Context, p *Principal) context.Context { panic("H5a T3a") }

// PrincipalFrom returns the acting principal, or nil for the system.
func PrincipalFrom(ctx context.Context) *Principal { panic("H5a T3a") }

// Actor is how the principal appears in audit entries and created_by columns.
func (p *Principal) Actor() audit.Actor { panic("H5a T3a") }

// Catalog holds the policies, limitations and code rules known to the app; filled while modules register, then
// read-only.
type Catalog struct{}

func NewCatalog() *Catalog                                   { panic("H5a T3a") }
func (c *Catalog) AddPolicy(p Policy) error                  { panic("H5a T3a") }
func (c *Catalog) AddLimitation(l Limitation) error          { panic("H5a T3a") }
func (c *Catalog) AddRules(typ string, r Rules)              { panic("H5a T3a") }
func (c *Catalog) Policy(name string) (Policy, bool)         { panic("H5a T3a") }
func (c *Catalog) Policies() []Policy                        { panic("H5a T3a") }
func (c *Catalog) Limitation(name string) (Limitation, bool) { panic("H5a T3a") }
func (c *Catalog) Limitations() []Limitation                 { panic("H5a T3a") }

// ValidateRole checks a role's policies: known policies, limitations that apply to them, valid values
// (*InvalidError).
func (c *Catalog) ValidateRole(ctx context.Context, ps []RolePolicy) error { panic("H5a T3a") }

// ValidateAssignment checks assignment limitations against the role's policies (*InvalidError).
func (c *Catalog) ValidateAssignment(ctx context.Context, role []RolePolicy, lim map[string][]string) error {
	panic("H5a T3a")
}

func CorePolicies() []Policy { panic("H5a T3a") }

// CoreLimitations are collection, locale, owner and channel.
func CoreLimitations(sc *schema.Schema, chans *channel.Set) []Limitation { panic("H5a T3a") }

func Templates() []Template { panic("H5a T3a") }

type CheckerOptions struct {
	DB      store.DB
	Catalog *Catalog
	Schema  *schema.Schema
	Log     *slog.Logger
}

// Checker answers authorization questions for the principal in ctx, caching grants per principal until an auth
// change is notified.
type Checker struct{}

func NewChecker(o CheckerOptions) *Checker { panic("H5a T3a") }

// Run drops cached grants on NotifyChannel notifications until ctx ends.
func (c *Checker) Run(ctx context.Context) error                             { panic("H5a T3a") }
func (c *Checker) Invalidate()                                               { panic("H5a T3a") }
func (c *Checker) Grants(ctx context.Context, p *Principal) ([]Grant, error) { panic("H5a T3a") }

// Can returns nil when the principal in ctx may perform policy on t, else an error wrapping ErrForbidden. Content
// policies also apply the type's code rules (Where conditions match t.Doc).
func (c *Checker) Can(ctx context.Context, policy string, t Target) error { panic("H5a T3a") }

// ListScope returns the access restriction for listing typ under policy, for store.Query's Scoped/Scope.
func (c *Checker) ListScope(ctx context.Context, policy, typ string) (scoped bool, terms []store.ScopeTerm, err error) {
	panic("H5a T3a")
}

// Allowed reports, per policy, whether Can would allow it on t (for the admin's action buttons).
func (c *Checker) Allowed(ctx context.Context, t Target, policies ...string) (map[string]bool, error) {
	panic("H5a T3a")
}
