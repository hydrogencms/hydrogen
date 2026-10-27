package access

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
)

// Catalog holds the policies, limitations and code rules known to the app; filled while modules register, then
// read-only.
type Catalog struct {
	policies    map[string]Policy
	limitations map[string]Limitation
	rules       map[string][]Rules
	tplExtra    map[string][]RolePolicy
}

func NewCatalog() *Catalog {
	return &Catalog{policies: map[string]Policy{}, limitations: map[string]Limitation{}, rules: map[string][]Rules{}, tplExtra: map[string][]RolePolicy{}}
}

// validName accepts lower-case words of letters, digits and underscores joined by dots; policies need a dot
// ("module.action").
func validName(s string, dotted bool) bool {
	dots, word := 0, 0
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '.':
			if word == 0 {
				return false
			}
			dots++
			word = 0
		case ch >= 'a' && ch <= 'z':
			word++
		case ch >= '0' && ch <= '9' || ch == '_':
			if word == 0 {
				return false
			}
			word++
		default:
			return false
		}
	}
	return word > 0 && (!dotted || dots > 0)
}

func (c *Catalog) AddPolicy(p Policy) error {
	if p.Name == PolicyAll || !validName(p.Name, true) {
		return fmt.Errorf("access: invalid policy name %q (want lower-case module.action)", p.Name)
	}
	if _, dup := c.policies[p.Name]; dup {
		return fmt.Errorf("access: policy %q registered twice", p.Name)
	}
	p.Limitations = slices.Clone(p.Limitations)
	c.policies[p.Name] = p
	return nil
}

// AddTemplatePolicies adds policies (without limitations) to a role template, for modules whose policies
// the core templates can't name. The template must exist and each policy must be registered.
func (c *Catalog) AddTemplatePolicies(template string, policies ...string) error {
	if !slices.ContainsFunc(baseTemplates(), func(t Template) bool { return t.Name == template }) {
		return fmt.Errorf("access: unknown role template %q", template)
	}
	for _, name := range policies {
		if _, ok := c.policies[name]; !ok {
			return fmt.Errorf("access: unknown policy %q", name)
		}
		c.tplExtra[template] = append(c.tplExtra[template], RolePolicy{Policy: name})
	}
	return nil
}

// Templates are the core role templates plus the policies modules added to them.
func (c *Catalog) Templates() []Template {
	out := baseTemplates()
	for i := range out {
		out[i].Policies = append(out[i].Policies, c.tplExtra[out[i].Name]...)
	}
	return out
}

// AddPolicyLimitation makes a registered limitation apply to a registered policy (a module extends the core
// content policies with its own limitations).
func (c *Catalog) AddPolicyLimitation(policy, limitation string) error {
	p, ok := c.policies[policy]
	if !ok {
		return fmt.Errorf("access: unknown policy %q", policy)
	}
	if !slices.Contains(p.Limitations, limitation) {
		p.Limitations = append(slices.Clone(p.Limitations), limitation)
		c.policies[policy] = p
	}
	return nil
}

func (c *Catalog) AddLimitation(l Limitation) error {
	name := l.Name()
	if !validName(name, false) {
		return fmt.Errorf("access: invalid limitation name %q", name)
	}
	if _, dup := c.limitations[name]; dup {
		return fmt.Errorf("access: limitation %q registered twice", name)
	}
	c.limitations[name] = l
	return nil
}

// AddRules registers code rules for a content type; several registrations AND together.
func (c *Catalog) AddRules(typ string, r Rules) { c.rules[typ] = append(c.rules[typ], r) }

// RuleTypes lists the content types that have code rules, sorted.
func (c *Catalog) RuleTypes() []string {
	out := make([]string, 0, len(c.rules))
	for t := range c.rules {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}

func (c *Catalog) Policy(name string) (Policy, bool) {
	p, ok := c.policies[name]
	return p, ok
}

// Policies lists the policies by name.
func (c *Catalog) Policies() []Policy {
	out := make([]Policy, 0, len(c.policies))
	for _, p := range c.policies {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Policy) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func (c *Catalog) Limitation(name string) (Limitation, bool) {
	l, ok := c.limitations[name]
	return l, ok
}

// Limitations lists the limitations by name.
func (c *Catalog) Limitations() []Limitation {
	out := make([]Limitation, 0, len(c.limitations))
	for _, l := range c.limitations {
		out = append(out, l)
	}
	slices.SortFunc(out, func(a, b Limitation) int { return cmp.Compare(a.Name(), b.Name()) })
	return out
}

const errStarLimited = "the everything policy cannot be limited"

// ValidateRole checks a role's policies: known policies, limitations that apply to them, valid values
// (*InvalidError).
func (c *Catalog) ValidateRole(ctx context.Context, ps []RolePolicy) error {
	fields := map[string]string{}
	for i, rp := range ps {
		base := "policies[" + strconv.Itoa(i) + "]"
		var applies func(string) bool
		if rp.Policy == PolicyAll {
			if len(rp.Limitations) > 0 {
				fields[base+".limitations"] = errStarLimited
			}
			continue
		} else if p, ok := c.policies[rp.Policy]; ok {
			applies = func(name string) bool { return slices.Contains(p.Limitations, name) }
		} else {
			fields[base+".policy"] = fmt.Sprintf("unknown policy %q", rp.Policy)
			continue
		}
		c.checkLimits(ctx, fields, base+".limitations.", rp.Limitations, applies, "policy "+rp.Policy)
	}
	return invalidFields(fields)
}

// ValidateAssignment checks assignment limitations against the role's policies (*InvalidError).
func (c *Catalog) ValidateAssignment(ctx context.Context, role []RolePolicy, lim map[string][]string) error {
	fields := map[string]string{}
	if len(lim) > 0 && slices.ContainsFunc(role, func(rp RolePolicy) bool { return rp.Policy == PolicyAll }) {
		fields["limitations"] = "a role with the everything policy cannot be limited per assignment"
		return invalidFields(fields)
	}
	applies := func(name string) bool {
		for _, rp := range role {
			if p, ok := c.policies[rp.Policy]; ok && slices.Contains(p.Limitations, name) {
				return true
			}
		}
		return false
	}
	c.checkLimits(ctx, fields, "limitations.", lim, applies, "the role's policies")
	return invalidFields(fields)
}

func (c *Catalog) checkLimits(ctx context.Context, fields map[string]string, prefix string, lim map[string][]string, applies func(string) bool, what string) {
	for _, name := range slices.Sorted(maps.Keys(lim)) {
		l, ok := c.limitations[name]
		switch {
		case !ok:
			fields[prefix+name] = fmt.Sprintf("unknown limitation %q", name)
		case !applies(name):
			fields[prefix+name] = fmt.Sprintf("limitation %q does not apply to %s", name, what)
		case len(lim[name]) == 0:
			fields[prefix+name] = "needs at least one value"
		default:
			if err := l.Validate(ctx, lim[name]); err != nil {
				fields[prefix+name] = err.Error()
			}
		}
	}
}

func invalidFields(fields map[string]string) error {
	if len(fields) == 0 {
		return nil
	}
	return &InvalidError{Fields: fields}
}
