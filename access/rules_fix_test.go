package access_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/store"
)

// A rule may only use shared fields: a listing would compile it for the requested locale while an item is
// matched on its main locale, and the two could disagree.
func TestRuleOnTranslatableFieldIsRejected(t *testing.T) {
	e := newEnv(t)
	e.grant("a", []access.RolePolicy{pol("*")}, nil)
	e.cat.AddRules("article", access.Rules{Read: func(context.Context, *access.Principal) access.Result {
		return access.Where("label", store.OpEq, "x")
	}})
	err := e.chk.Can(e.ctx(), "content.read", access.Target{Type: "article", Doc: map[string]any{"label": "x"}})
	if err == nil || forbiddenErr(err) || !strings.Contains(err.Error(), "translatable") {
		t.Errorf("Can: %v", err)
	}
	if _, _, err := e.chk.ListScope(e.ctx(), "content.read", "article"); err == nil || !strings.Contains(err.Error(), "translatable") {
		t.Errorf("ListScope: %v", err)
	}
}

// A listing needs a type: code rules belong to one.
func TestListScopeNeedsAType(t *testing.T) {
	e := newEnv(t)
	e.grant("a", []access.RolePolicy{pol("*")}, nil)
	if _, _, err := e.chk.ListScope(e.ctx(), "content.read", ""); err == nil {
		t.Error("an untyped listing was scoped")
	}
	if scoped, _, err := e.chk.ListScope(context.Background(), "content.read", ""); err != nil || scoped {
		t.Errorf("the system lists everything: %v %v", scoped, err)
	}
}
