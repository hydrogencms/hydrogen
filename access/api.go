// Package access authenticates principals (users, API tokens, agents) and authorizes what they do: RBAC with
// limitations, plus access rules registered in code. A context without a principal is the system, which may do
// everything (the Local API, hooks, jobs). Contract: planning/design/auth-h5a.md.
package access

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

var (
	// ErrUnauthenticated: no valid credentials.
	ErrUnauthenticated = errors.New("access: unauthenticated")
	// ErrForbidden: authenticated, but no grant allows the action. Errors from Can wrap it with the policy name.
	ErrForbidden = errors.New("access: forbidden")
)

// ThrottledError is returned by Login after too many failures.
type ThrottledError struct{ RetryAfter time.Duration }

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("access: too many failed logins; retry after %s", e.RetryAfter.Round(time.Second))
}

type Kind string

const (
	KindUser   Kind = "user"
	KindToken  Kind = "token"
	KindAgent  Kind = "agent"
	KindSystem Kind = "system"
)

// Principal is who is acting. Super principals (the system and the config token) pass every check.
type Principal struct {
	Kind   Kind
	ID     ids.UUID   // zero for the system and the config token
	Name   string     // login, token name or agent name
	Groups []ids.UUID // users only
	Super  bool
	// Detail is recorded with audit entries (agent model and task, client IP on login).
	Detail map[string]string
}

// Policy is an action that roles grant, named "module.action".
type Policy struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Limitations []string `json:"limitations"` // limitation types that apply to it
}

// Target is what a check is about.
type Target struct {
	Type    string         // content type; "" for policies not about content
	Item    *store.Item    // nil on create and for policies not about one item
	Locales []string       // locales being written (write policies)
	Doc     map[string]any // working fields of the main locale, for Where rules
	Extra   map[string]any // module data, e.g. the pages module's placement parent
}

// Choice is a value the role editor offers for a limitation.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Limitation narrows a grant. Modules register their own through the kernel.
type Limitation interface {
	Name() string
	// Validate checks values entered in the role editor.
	Validate(ctx context.Context, values []string) error
	Match(ctx context.Context, p *Principal, values []string, t Target) (bool, error)
	// Scope narrows a listing of typ to the items Match would accept: ok=false when the limitation doesn't
	// restrict listings (locale, collection). An error means the grant cannot list anything.
	Scope(ctx context.Context, p *Principal, values []string, typ string) (part store.ScopeTerm, ok bool, err error)
	// Choices lists values for the role editor; nil means free text.
	Choices(ctx context.Context) ([]Choice, error)
}

// RolePolicy is one entry of a role's policy list (stored as JSON in store.Role.Policies).
type RolePolicy struct {
	Policy      string              `json:"policy"`
	Limitations map[string][]string `json:"limitations,omitempty"`
}

// Grant is a policy a principal holds through one role assignment: the role's limitations followed by the
// assignment's. Every Limit must match; a name may repeat (role and assignment both limit it).
type Grant struct {
	Policy string  `json:"policy"`
	Role   string  `json:"role"`
	Limits []Limit `json:"limits,omitempty"`
}

type Limit struct {
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// Result is an access rule's verdict: allow, deny, or allow only items matching conditions on indexed fields.
type Result struct {
	deny  bool
	where []store.Cond
}

func Allow() Result { return Result{} }
func Deny() Result  { return Result{deny: true} }

// Where allows items whose indexed field matches (the operators of store.Op).
func Where(field string, op store.Op, value any) Result {
	return Result{where: []store.Cond{{Field: field, Op: op, Value: value}}}
}

// And combines results: any deny denies, conditions add up.
func And(rs ...Result) Result {
	var out Result
	for _, r := range rs {
		out.deny = out.deny || r.deny
		out.where = append(out.where, r.where...)
	}
	return out
}

func (r Result) Denied() bool             { return r.deny }
func (r Result) Conditions() []store.Cond { return r.where }

// Rules are code access rules for one content type, ANDed with RBAC. A nil func allows.
type Rules struct {
	Read, Create, Update, Publish, Delete func(ctx context.Context, p *Principal) Result
}

// Template is a role seeded once when the auth tables are empty of it; editable afterwards.
type Template struct {
	Name        string
	Description string
	Policies    []RolePolicy
}

// Core policy names.
const (
	PolicyAll            = "*"
	PolicyContentRead    = "content.read"
	PolicyContentCreate  = "content.create"
	PolicyContentEdit    = "content.edit"
	PolicyContentPublish = "content.publish"
	PolicyContentDelete  = "content.delete"
	PolicyContentPreview = "content.preview"
	// PolicyContentPreviewShare creates and revokes share links: preview links anyone can open.
	PolicyContentPreviewShare = "content.preview.share"
	PolicyUserManage          = "user.manage"
	PolicyRoleManage          = "role.manage"
	PolicyTokenManage         = "token.manage"
	PolicyAuditRead           = "audit.read"
)

// NotifyChannel carries auth changes; listeners drop cached grants (empty payload: all).
const NotifyChannel = "hydrogen_auth"

// InvalidError reports rejected input field by field (path → message).
type InvalidError struct{ Fields map[string]string }

func (e *InvalidError) Error() string {
	parts := make([]string, 0, len(e.Fields))
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "access: invalid input: " + strings.Join(parts, "; ")
}
