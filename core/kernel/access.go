package kernel

import "github.com/hydrogencms/hydrogen/core/access"

// Contract: planning/design/auth-h5a.md. Wiring (Services.Catalog/Access/Auth/Admin) is H5a T4.

func (r *Registry) accessCatalog() *access.Catalog {
	if r.catalog == nil {
		r.catalog = access.NewCatalog()
		for _, p := range access.CorePolicies() {
			if err := r.catalog.AddPolicy(p); err != nil {
				panic(err) // core policies are valid by construction
			}
		}
	}
	return r.catalog
}

// Policy declares a policy roles can grant ("module.action") and the limitation types that apply to it.
func (r *Registry) Policy(p access.Policy) error { return r.accessCatalog().AddPolicy(p) }

// Limitation adds a limitation type to the role editor and to every policy that lists it.
func (r *Registry) Limitation(l access.Limitation) error { return r.accessCatalog().AddLimitation(l) }

// PolicyLimitation makes a limitation apply to an existing policy (e.g. a module's limitation on content.edit).
func (r *Registry) PolicyLimitation(policy, limitation string) error {
	return r.accessCatalog().AddPolicyLimitation(policy, limitation)
}

// TemplatePolicies adds registered policies to a seeded role template ("editor", "viewer", …), for a module
// whose policies the core templates can't name. Roles already seeded keep their policies.
func (r *Registry) TemplatePolicies(template string, policies ...string) error {
	return r.accessCatalog().AddTemplatePolicies(template, policies...)
}

// AccessRules adds code access rules for a content type, ANDed with RBAC and with other rules for the type.
func (r *Registry) AccessRules(typ string, rules access.Rules) {
	r.accessCatalog().AddRules(typ, rules)
}
