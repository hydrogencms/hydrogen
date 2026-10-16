package access

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// errExcluded is what Scope returns when a grant cannot list the type at all.
var errExcluded = errors.New("access: type excluded by limitation")

// OwnerSelf is the only value of the owner limitation.
const OwnerSelf = "self"

func CorePolicies() []Policy {
	read := []string{"collection", "owner", "channel"}
	write := []string{"collection", "locale", "owner", "channel"}
	return []Policy{
		{Name: PolicyContentRead, Description: "Read items, drafts, versions and references", Limitations: read},
		{Name: PolicyContentCreate, Description: "Create items", Limitations: write},
		{Name: PolicyContentEdit, Description: "Edit and discard drafts", Limitations: write},
		{Name: PolicyContentPublish, Description: "Publish and unpublish", Limitations: write},
		{Name: PolicyContentDelete, Description: "Delete items", Limitations: read},
		{Name: PolicyContentPreview, Description: "Create preview tokens", Limitations: read},
		{Name: PolicyContentPreviewShare, Description: "Create and revoke share links to previews", Limitations: read},
		{Name: PolicyUserManage, Description: "Manage users, groups and memberships"},
		{Name: PolicyRoleManage, Description: "Manage roles and assignments"},
		{Name: PolicyTokenManage, Description: "Manage API tokens"},
		{Name: PolicyAuditRead, Description: "Read the audit log"},
	}
}

// CoreLimitations are collection, locale, owner and channel. sc and chans may be nil (nothing to validate
// against).
func CoreLimitations(sc *schema.Schema, chans *channel.Set) []Limitation {
	return []Limitation{collectionLimit{sc}, localeLimit{chans}, ownerLimit{}, channelLimit{chans, sc}}
}

func templatePolicies(names ...string) []RolePolicy {
	out := make([]RolePolicy, len(names))
	for i, n := range names {
		out[i] = RolePolicy{Policy: n}
	}
	return out
}

// Templates are the core role templates; Catalog.Templates adds what modules contributed.
func Templates() []Template { return baseTemplates() }

func baseTemplates() []Template {
	return []Template{
		{Name: "administrator", Description: "Everything", Policies: templatePolicies(PolicyAll)},
		{Name: "editor", Description: "Full control of content", Policies: templatePolicies(PolicyContentRead,
			PolicyContentCreate, PolicyContentEdit, PolicyContentPublish, PolicyContentDelete, PolicyContentPreview,
			PolicyContentPreviewShare)},
		{Name: "contributor", Description: "Writes drafts for others to publish", Policies: templatePolicies(
			PolicyContentRead, PolicyContentCreate, PolicyContentEdit, PolicyContentPreview)},
		{Name: "ai-contributor", Description: "For agent tokens: reads and writes drafts, never publishes or deletes",
			Policies: templatePolicies(PolicyContentRead, PolicyContentCreate, PolicyContentEdit)},
		{Name: "viewer", Description: "Reads content", Policies: templatePolicies(PolicyContentRead)},
	}
}

func hasType(values []string, typ string) bool { return typ != "" && slices.Contains(values, typ) }

func checkKnown(what string, values []string, ok func(string) bool) error {
	for _, v := range values {
		if !ok(v) {
			return fmt.Errorf("unknown %s %q", what, v)
		}
	}
	return nil
}

type collectionLimit struct{ sc *schema.Schema }

func (collectionLimit) Name() string { return "collection" }

func (l collectionLimit) Validate(_ context.Context, values []string) error {
	if l.sc == nil {
		return nil
	}
	return checkKnown("content type", values, func(v string) bool { _, ok := l.sc.Type(v); return ok })
}

func (collectionLimit) Match(_ context.Context, _ *Principal, values []string, t Target) (bool, error) {
	return hasType(values, t.Type), nil
}

func (collectionLimit) Scope(_ context.Context, _ *Principal, values []string, typ string) (store.ScopeTerm, bool, error) {
	if !hasType(values, typ) {
		return store.ScopeTerm{}, false, errExcluded
	}
	return store.ScopeTerm{}, false, nil
}

func (l collectionLimit) Choices(context.Context) ([]Choice, error) {
	if l.sc == nil {
		return nil, nil
	}
	out := make([]Choice, 0, len(l.sc.Types()))
	for _, t := range l.sc.Types() {
		out = append(out, Choice{Value: t.Name, Label: cmpLabel(t.Label, t.Name)})
	}
	return out, nil
}

func cmpLabel(label, name string) string {
	if label != "" {
		return label
	}
	return name
}

type localeLimit struct{ chans *channel.Set }

func (localeLimit) Name() string { return "locale" }

func (l localeLimit) Validate(_ context.Context, values []string) error {
	if l.chans == nil {
		return nil
	}
	langs := l.chans.Languages()
	if len(langs) == 0 {
		return nil
	}
	return checkKnown("locale", values, func(v string) bool { _, ok := langs[v]; return ok })
}

func (localeLimit) Match(_ context.Context, _ *Principal, values []string, t Target) (bool, error) {
	for _, loc := range t.Locales {
		if !slices.Contains(values, loc) {
			return false, nil
		}
	}
	return true, nil
}

func (localeLimit) Scope(context.Context, *Principal, []string, string) (store.ScopeTerm, bool, error) {
	return store.ScopeTerm{}, false, nil
}

func (l localeLimit) Choices(context.Context) ([]Choice, error) {
	if l.chans == nil {
		return nil, nil
	}
	langs := l.chans.Languages()
	if len(langs) == 0 {
		return nil, nil
	}
	codes := slices.Sorted(maps.Keys(langs))
	out := make([]Choice, len(codes))
	for i, c := range codes {
		out[i] = Choice{Value: c, Label: c}
	}
	return out, nil
}

type ownerLimit struct{}

func (ownerLimit) Name() string { return "owner" }

func (ownerLimit) Validate(_ context.Context, values []string) error {
	return checkKnown("owner value", values, func(v string) bool { return v == OwnerSelf })
}

func (ownerLimit) Match(_ context.Context, p *Principal, _ []string, t Target) (bool, error) {
	if t.Item == nil {
		return true, nil
	}
	a := p.Actor()
	cb := t.Item.CreatedBy
	return len(cb) == len(a.Kind)+1+len(a.ID) && cb[:len(a.Kind)] == a.Kind && cb[len(a.Kind)] == ':' &&
		cb[len(a.Kind)+1:] == a.ID, nil
}

func (ownerLimit) Scope(_ context.Context, p *Principal, _ []string, _ string) (store.ScopeTerm, bool, error) {
	return store.ScopeTerm{CreatedBy: []string{p.Actor().String()}}, true, nil
}

func (ownerLimit) Choices(context.Context) ([]Choice, error) {
	return []Choice{{Value: OwnerSelf, Label: "Own items"}}, nil
}

type channelLimit struct {
	chans *channel.Set
	sc    *schema.Schema
}

func (channelLimit) Name() string { return "channel" }

func (l channelLimit) Validate(_ context.Context, values []string) error {
	if l.chans == nil {
		return nil
	}
	return checkKnown("channel", values, func(v string) bool { _, ok := l.chans.Get(v); return ok })
}

func (l channelLimit) channels(values []string) []*channel.Channel {
	out := make([]*channel.Channel, 0, len(values))
	for _, v := range values {
		if l.chans == nil {
			break
		}
		if c, ok := l.chans.Get(v); ok {
			out = append(out, c)
		}
	}
	return out
}

func allowsType(chs []*channel.Channel, typ string) bool {
	if typ == "" {
		return false
	}
	for _, c := range chs {
		if c.AllowsCollection(typ) {
			return true
		}
	}
	return false
}

func (l channelLimit) Match(_ context.Context, _ *Principal, values []string, t Target) (bool, error) {
	chs := l.channels(values)
	if !allowsType(chs, t.Type) {
		return false, nil
	}
	for _, loc := range t.Locales {
		ok := false
		for _, c := range chs {
			if len(c.Languages) == 0 || slices.Contains(c.Languages, loc) {
				ok = true
				break
			}
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func (l channelLimit) Scope(_ context.Context, _ *Principal, values []string, typ string) (store.ScopeTerm, bool, error) {
	if !allowsType(l.channels(values), typ) {
		return store.ScopeTerm{}, false, errExcluded
	}
	return store.ScopeTerm{}, false, nil
}

func (l channelLimit) Choices(context.Context) ([]Choice, error) {
	if l.chans == nil {
		return nil, nil
	}
	all := l.chans.All()
	out := make([]Choice, len(all))
	for i, c := range all {
		out[i] = Choice{Value: c.Name, Label: c.Name}
	}
	return out, nil
}
