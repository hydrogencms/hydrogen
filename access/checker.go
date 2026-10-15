package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

// maxCached bounds the per-principal grant cache; a full cache is cleared.
const maxCached = 4096

type CheckerOptions struct {
	DB      store.DB
	Catalog *Catalog
	Schema  *schema.Schema
	Log     *slog.Logger
}

// Checker answers authorization questions for the principal in ctx, caching grants per principal until an auth
// change is notified.
type Checker struct {
	o     CheckerOptions
	mu    sync.RWMutex
	cache map[cacheKey][]Grant
	gen   uint64
}

type cacheKey struct {
	kind Kind
	id   ids.UUID
}

func NewChecker(o CheckerOptions) *Checker {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Catalog == nil {
		o.Catalog = NewCatalog()
	}
	return &Checker{o: o, cache: map[cacheKey][]Grant{}}
}

// Run drops cached grants on NotifyChannel notifications until ctx ends.
func (c *Checker) Run(ctx context.Context) error {
	ch, err := c.o.DB.Notifier().Listen(ctx, NotifyChannel)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case _, ok := <-ch:
			if !ok {
				return nil
			}
			c.Invalidate()
		}
	}
}

// Invalidate drops every cached grant.
func (c *Checker) Invalidate() {
	c.mu.Lock()
	c.cache = map[cacheKey][]Grant{}
	c.gen++
	c.mu.Unlock()
}

func exempt(p *Principal) bool { return p == nil || p.Super || p.Kind == KindSystem }

// Grants lists the policies p holds through its role assignments (nil for the system and super principals).
func (c *Checker) Grants(ctx context.Context, p *Principal) ([]Grant, error) {
	if exempt(p) {
		return nil, nil
	}
	key := cacheKey{p.Kind, p.ID}
	c.mu.RLock()
	g, ok := c.cache[key]
	gen := c.gen
	c.mu.RUnlock()
	if ok {
		return g, nil
	}
	g, err := c.load(ctx, p)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.gen == gen {
		if len(c.cache) >= maxCached {
			c.cache = map[cacheKey][]Grant{}
		}
		c.cache[key] = g
	}
	c.mu.Unlock()
	return g, nil
}

func (c *Checker) load(ctx context.Context, p *Principal) ([]Grant, error) {
	var refs []store.PrincipalRef
	if p.Kind == KindUser {
		refs = make([]store.PrincipalRef, 0, 1+len(p.Groups))
		refs = append(refs, store.PrincipalRef{Kind: store.PrincipalUser, ID: p.ID})
		for _, g := range p.Groups {
			refs = append(refs, store.PrincipalRef{Kind: store.PrincipalGroup, ID: g})
		}
	} else {
		refs = []store.PrincipalRef{{Kind: store.PrincipalToken, ID: p.ID}}
	}
	var rgs []store.RoleGrant
	load := func(tx store.Tx) error {
		a := tx.Auth()
		if a == nil {
			return errors.New("access: the database adapter has no auth store")
		}
		var err error
		rgs, err = a.AssignmentsFor(ctx, refs)
		return err
	}
	// A caller inside a transaction has put it on the context: query through it, since a second connection
	// taken while the first is held can wait forever on a small pool.
	var err error
	if tx, ok := store.TxFrom(ctx); ok {
		err = load(tx)
	} else {
		err = c.o.DB.View(ctx, load)
	}
	if err != nil {
		return nil, fmt.Errorf("access: load grants: %w", err)
	}
	out := []Grant{}
	for _, rg := range rgs {
		out = c.appendGrants(out, rg)
	}
	return out, nil
}

func (c *Checker) appendGrants(out []Grant, rg store.RoleGrant) []Grant {
	var rps []RolePolicy
	if err := json.Unmarshal(rg.Role.Policies, &rps); err != nil {
		c.o.Log.Warn("access: skipping role with unreadable policies", "role", rg.Role.Name, "err", err)
		return out
	}
	var assigned map[string][]string
	if len(rg.Assignment.Limitations) > 0 {
		if err := json.Unmarshal(rg.Assignment.Limitations, &assigned); err != nil {
			c.o.Log.Warn("access: skipping assignment with unreadable limitations", "role", rg.Role.Name, "err", err)
			return out
		}
	}
	assignedNames := slices.Sorted(maps.Keys(assigned))
	for _, rp := range rps {
		var applies []string
		if rp.Policy != PolicyAll {
			pol, ok := c.o.Catalog.Policy(rp.Policy)
			if !ok {
				c.o.Log.Warn("access: role names an unknown policy", "role", rg.Role.Name, "policy", rp.Policy)
				continue
			}
			applies = pol.Limitations
		}
		g := Grant{Policy: rp.Policy, Role: rg.Role.Name}
		for _, name := range slices.Sorted(maps.Keys(rp.Limitations)) {
			g.Limits = append(g.Limits, Limit{Name: name, Values: rp.Limitations[name]})
		}
		for _, name := range assignedNames {
			if rp.Policy == PolicyAll || slices.Contains(applies, name) {
				g.Limits = append(g.Limits, Limit{Name: name, Values: assigned[name]})
			}
		}
		out = append(out, g)
	}
	return out
}

// Prepare loads the grants of the principal in ctx into the cache, so later checks inside a transaction find
// them without a query of their own (a pooled connection waiting on another while one is held can deadlock).
func (c *Checker) Prepare(ctx context.Context) error {
	p := PrincipalFrom(ctx)
	if exempt(p) {
		return nil
	}
	_, err := c.Grants(ctx, p)
	return err
}

// HasRules reports whether code access rules exist for the content type, so callers know whether checks need
// the item's fields (Target.Doc).
func (c *Checker) HasRules(typ string) bool { return len(c.o.Catalog.rules[typ]) > 0 }

// Holds reports whether any grant gives the principal in ctx the policy, whatever its limitations: a coarse
// gate for routes, never a substitute for Can on the thing being acted on.
func (c *Checker) Holds(ctx context.Context, policy string) error {
	p := PrincipalFrom(ctx)
	if exempt(p) {
		return nil
	}
	grants, err := c.Grants(ctx, p)
	if err != nil {
		return err
	}
	for i := range grants {
		if grants[i].Policy == policy || grants[i].Policy == PolicyAll {
			return nil
		}
	}
	return forbidden(policy)
}

func forbidden(policy string) error { return fmt.Errorf("%w: %s", ErrForbidden, policy) }

// Can returns nil when the principal in ctx may perform policy on t, else an error wrapping ErrForbidden. Content
// policies also apply the type's code rules (Where conditions match t.Doc).
func (c *Checker) Can(ctx context.Context, policy string, t Target) error {
	return c.can(ctx, policy, t, true)
}

// Screen is Can without matching Where rules against the (not yet known) document: grants, limitations and
// Deny verdicts apply. A caller uses it to refuse early and must still call Can once it has the document.
func (c *Checker) Screen(ctx context.Context, policy string, t Target) error {
	return c.can(ctx, policy, t, false)
}

func (c *Checker) can(ctx context.Context, policy string, t Target, matchDoc bool) error {
	p := PrincipalFrom(ctx)
	if exempt(p) {
		return nil
	}
	grants, err := c.Grants(ctx, p)
	if err != nil {
		return err
	}
	granted := false
	for i := range grants {
		g := &grants[i]
		if (g.Policy == policy || g.Policy == PolicyAll) && c.limitsMatch(ctx, p, g, t) {
			granted = true
			break
		}
	}
	if !granted {
		return forbidden(policy)
	}
	if t.Type == "" || len(c.o.Catalog.rules[t.Type]) == 0 {
		return nil
	}
	conds, denied, err := c.ruleConds(ctx, p, policy, t.Type)
	if err != nil {
		return err
	}
	if denied {
		return forbidden(policy)
	}
	if len(conds) > 0 && matchDoc {
		typ, _ := c.o.Schema.Type(t.Type)
		if !storeutil.MatchWhere(conds, c.o.Schema.IndexValues(typ, t.Doc)) {
			return forbidden(policy)
		}
	}
	return nil
}

func (c *Checker) limitsMatch(ctx context.Context, p *Principal, g *Grant, t Target) bool {
	for _, l := range g.Limits {
		lim, ok := c.o.Catalog.limitations[l.Name]
		if !ok {
			return false
		}
		match, err := lim.Match(ctx, p, l.Values, t)
		if err != nil {
			c.o.Log.Warn("access: limitation failed", "limitation", l.Name, "role", g.Role, "err", err)
			return false
		}
		if !match {
			return false
		}
	}
	return true
}

func ruleFor(r Rules, policy string) func(context.Context, *Principal) Result {
	switch policy {
	case PolicyContentRead:
		return r.Read
	case PolicyContentCreate:
		return r.Create
	case PolicyContentEdit:
		return r.Update
	case PolicyContentPublish:
		return r.Publish
	case PolicyContentDelete:
		return r.Delete
	}
	return nil
}

// ruleConds runs the type's code rules for policy and returns their conditions, with columns and values
// resolved against the schema.
func (c *Checker) ruleConds(ctx context.Context, p *Principal, policy, typ string) (conds []store.Cond, denied bool, err error) {
	var raw []store.Cond
	for _, r := range c.o.Catalog.rules[typ] {
		fn := ruleFor(r, policy)
		if fn == nil {
			continue
		}
		res := fn(ctx, p)
		if res.deny {
			return nil, true, nil
		}
		raw = append(raw, res.where...)
	}
	if len(raw) == 0 {
		return nil, false, nil
	}
	var st *schema.Type
	if c.o.Schema != nil {
		st, _ = c.o.Schema.Type(typ)
	}
	if st == nil {
		return nil, false, fmt.Errorf("access: rules for unknown type %q", typ)
	}
	conds = make([]store.Cond, len(raw))
	for i, cd := range raw {
		f, ok := st.Field(cd.Field)
		if !ok || !f.Index {
			return nil, false, fmt.Errorf("access: rule on %s.%s: not an indexed field", typ, cd.Field)
		}
		if f.Translatable {
			// A listing compiles the rule against the rows of the requested locale while a single item is
			// matched on its main locale, so the two would disagree.
			return nil, false, fmt.Errorf("access: rule on %s.%s: a rule can only use shared fields, and this one is translatable", typ, cd.Field)
		}
		elem := f
		if f.Shape.Base == schema.BaseList && f.Of != nil {
			elem = f.Of
		}
		col, ok := columnOf(elem.Shape)
		if !ok {
			return nil, false, fmt.Errorf("access: rule on %s.%s: field cannot be filtered", typ, cd.Field)
		}
		cd.Column = col
		switch cd.Op {
		case store.OpExists:
		case store.OpIn:
			if cd.Value, err = storeutil.List(col, cd.Value); err != nil {
				return nil, false, fmt.Errorf("access: rule on %s.%s: %w", typ, cd.Field, err)
			}
		default:
			if cd.Value, err = storeutil.Scalar(col, cd.Value); err != nil {
				return nil, false, fmt.Errorf("access: rule on %s.%s: %w", typ, cd.Field, err)
			}
		}
		conds[i] = cd
	}
	return conds, false, nil
}

func columnOf(sh schema.Shape) (store.Column, bool) {
	switch sh.Base {
	case schema.BaseString:
		if sh.Format == "date" || sh.Format == "datetime" {
			return store.ColTime, true
		}
		return store.ColText, true
	case schema.BaseNumber:
		return store.ColNum, true
	case schema.BaseBoolean:
		return store.ColBool, true
	case schema.BaseRef:
		return store.ColRef, true
	}
	return "", false
}

// ListScope returns the access restriction for listing typ under policy, for store.Query's Scoped/Scope.
func (c *Checker) ListScope(ctx context.Context, policy, typ string) (scoped bool, terms []store.ScopeTerm, err error) {
	p := PrincipalFrom(ctx)
	if exempt(p) {
		return false, nil, nil
	}
	if typ == "" {
		// Code rules belong to a type; a listing across types would skip them.
		return false, nil, errors.New("access: a listing needs a content type")
	}
	grants, err := c.Grants(ctx, p)
	if err != nil {
		return false, nil, err
	}
	unrestricted := false
	for i := range grants {
		g := &grants[i]
		if g.Policy != policy && g.Policy != PolicyAll {
			continue
		}
		term, ok := c.grantTerm(ctx, p, g, typ)
		if !ok {
			continue
		}
		if term.CreatedBy == nil && len(term.Where) == 0 {
			unrestricted = true
			break
		}
		terms = append(terms, term)
	}
	if !unrestricted && len(terms) == 0 {
		return true, nil, nil
	}
	if len(c.o.Catalog.rules[typ]) == 0 {
		return !unrestricted, terms, nil
	}
	conds, denied, err := c.ruleConds(ctx, p, policy, typ)
	if err != nil {
		return false, nil, err
	}
	if denied {
		return true, nil, nil
	}
	if len(conds) == 0 {
		return !unrestricted, terms, nil
	}
	if unrestricted {
		return true, []store.ScopeTerm{{Where: conds}}, nil
	}
	for i := range terms {
		terms[i].Where = append(slices.Clip(terms[i].Where), conds...)
	}
	return true, terms, nil
}

// grantTerm ANDs the scope parts of a grant's limitations; ok=false when the grant cannot list typ.
func (c *Checker) grantTerm(ctx context.Context, p *Principal, g *Grant, typ string) (term store.ScopeTerm, ok bool) {
	for _, l := range g.Limits {
		lim, known := c.o.Catalog.limitations[l.Name]
		if !known {
			return term, false
		}
		part, restricts, err := lim.Scope(ctx, p, l.Values, typ)
		if err != nil {
			if !errors.Is(err, errExcluded) {
				c.o.Log.Warn("access: limitation cannot scope a listing", "limitation", l.Name, "role", g.Role, "err", err)
			}
			return term, false
		}
		if !restricts {
			continue
		}
		if part.CreatedBy != nil {
			if term.CreatedBy == nil {
				term.CreatedBy = part.CreatedBy
			} else {
				term.CreatedBy = slices.DeleteFunc(slices.Clone(term.CreatedBy), func(a string) bool {
					return !slices.Contains(part.CreatedBy, a)
				})
				if len(term.CreatedBy) == 0 {
					return term, false
				}
			}
		}
		term.Where = append(term.Where, part.Where...)
	}
	return term, true
}

// Allowed reports, per policy, whether Can would allow it on t (for the admin's action buttons).
func (c *Checker) Allowed(ctx context.Context, t Target, policies ...string) (map[string]bool, error) {
	out := make(map[string]bool, len(policies))
	for _, pol := range policies {
		err := c.Can(ctx, pol, t)
		switch {
		case err == nil:
			out[pol] = true
		case errors.Is(err, ErrForbidden):
			out[pol] = false
		default:
			return nil, err
		}
	}
	return out, nil
}
