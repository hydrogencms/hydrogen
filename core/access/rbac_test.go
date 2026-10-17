package access_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const rbacSchema = `
types:
  article:
    fields:
      title: { type: string, translatable: true }
      label: { type: string, translatable: true, index: true }
      kind: { type: string, index: true }
      rating: { type: number, index: true }
      tags: { type: list, of: { type: string }, index: true }
  job:
    fields:
      title: { type: string }
  settings:
    singleton: true
    fields:
      site: { type: string }
`

const rbacChannels = `
languages:
  en: {}
  de: {}
  fr: {}
channels:
  careers_en: { languages: [en], collections: [job] }
  corp_de:    { languages: [de, en], collections: [article] }
`

func rbacSchemaOf(tb testing.TB) *schema.Schema {
	tb.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": &fstest.MapFile{Data: []byte(rbacSchema)}}, "schema"); err != nil {
		tb.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		tb.Fatal(err)
	}
	return s
}

func rbacChannelsOf(tb testing.TB, sc *schema.Schema) *channel.Set {
	tb.Helper()
	dir := tb.TempDir()
	tb.Chdir(dir)
	if err := os.WriteFile(filepath.Join(dir, "channels.yaml"), []byte(rbacChannels), 0o644); err != nil {
		tb.Fatal(err)
	}
	c, err := config.Load(config.Options{Dir: "."})
	if err != nil {
		tb.Fatal(err)
	}
	n, err := c.Node("channels")
	if err != nil {
		tb.Fatal(err)
	}
	set, err := channel.Compile(n, channel.CompileOptions{Schema: sc, DefaultLocale: "en"})
	if err != nil {
		tb.Fatal(err)
	}
	return set
}

// rbacDB is an in-memory store.DB that serves assignments only (T1's adapters may not have landed).
type rbacDB struct {
	store.DB
	mu     sync.Mutex
	rows   []store.RoleGrant
	views  int
	notify chan store.Notification
}

type rbacTx struct {
	store.Tx
	db *rbacDB
}

type rbacAuth struct {
	store.AuthStore
	db *rbacDB
}

type rbacNotifier struct{ db *rbacDB }

func (d *rbacDB) View(_ context.Context, fn func(store.Tx) error) error {
	d.mu.Lock()
	d.views++
	d.mu.Unlock()
	return fn(rbacTx{db: d})
}

func (d *rbacDB) Notifier() store.Notifier { return rbacNotifier{d} }

func (n rbacNotifier) Listen(context.Context, string) (<-chan store.Notification, error) {
	return n.db.notify, nil
}

func (t rbacTx) Auth() store.AuthStore { return rbacAuth{db: t.db} }

func (a rbacAuth) AssignmentsFor(_ context.Context, refs []store.PrincipalRef) ([]store.RoleGrant, error) {
	a.db.mu.Lock()
	defer a.db.mu.Unlock()
	var out []store.RoleGrant
	for _, r := range a.db.rows {
		for _, ref := range refs {
			if r.Assignment.Principal == ref {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (d *rbacDB) viewCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.views
}

func (d *rbacDB) set(ref store.PrincipalRef, role string, policies []access.RolePolicy, lim map[string][]string) {
	pj, _ := json.Marshal(policies)
	if lim == nil {
		lim = map[string][]string{}
	}
	lj, _ := json.Marshal(lim)
	d.mu.Lock()
	d.rows = append(d.rows, store.RoleGrant{
		Assignment: store.Assignment{Principal: ref, Limitations: lj},
		Role:       store.Role{Name: role, Policies: pj},
	})
	d.mu.Unlock()
}

func (d *rbacDB) clear() {
	d.mu.Lock()
	d.rows = nil
	d.mu.Unlock()
}

type rbacEnv struct {
	db      *rbacDB
	cat     *access.Catalog
	chk     *access.Checker
	sc      *schema.Schema
	chans   *channel.Set
	alice   *access.Principal
	aliceID ids.UUID
}

func newEnv(tb testing.TB) *rbacEnv {
	tb.Helper()
	sc := rbacSchemaOf(tb)
	chans := rbacChannelsOf(tb, sc)
	cat := access.NewCatalog()
	for _, p := range access.CorePolicies() {
		if err := cat.AddPolicy(p); err != nil {
			tb.Fatal(err)
		}
	}
	for _, l := range access.CoreLimitations(sc, chans) {
		if err := cat.AddLimitation(l); err != nil {
			tb.Fatal(err)
		}
	}
	db := &rbacDB{notify: make(chan store.Notification, 4)}
	id := ids.UUID{1}
	return &rbacEnv{db: db, cat: cat, sc: sc, chans: chans, aliceID: id,
		chk:   access.NewChecker(access.CheckerOptions{DB: db, Catalog: cat, Schema: sc}),
		alice: &access.Principal{Kind: access.KindUser, ID: id, Name: "alice"}}
}

func (e *rbacEnv) grant(role string, policies []access.RolePolicy, lim map[string][]string) {
	e.db.set(store.PrincipalRef{Kind: store.PrincipalUser, ID: e.aliceID}, role, policies, lim)
}

func (e *rbacEnv) ctx() context.Context { return access.WithPrincipal(context.Background(), e.alice) }

func pol(name string, lim ...any) access.RolePolicy {
	rp := access.RolePolicy{Policy: name}
	if len(lim) > 0 {
		rp.Limitations = map[string][]string{}
		for i := 0; i < len(lim); i += 2 {
			rp.Limitations[lim[i].(string)] = lim[i+1].([]string)
		}
	}
	return rp
}

func vs(v ...string) []string { return v }

func forbiddenErr(err error) bool { return errors.Is(err, access.ErrForbidden) }

func TestActor(t *testing.T) {
	id := ids.UUID{9}
	cases := []struct {
		p    *access.Principal
		want string
	}{
		{nil, "system"},
		{&access.Principal{Kind: access.KindSystem, Super: true}, "system"},
		{&access.Principal{Kind: access.KindUser, ID: id}, "user:" + id.String()},
		{&access.Principal{Kind: access.KindToken, ID: id}, "token:" + id.String()},
		{&access.Principal{Kind: access.KindAgent, ID: id}, "agent:" + id.String()},
		{&access.Principal{Kind: access.KindToken, Super: true}, "token:config"},
	}
	for _, c := range cases {
		if got := c.p.Actor().String(); got != c.want {
			t.Errorf("%+v: %s, want %s", c.p, got, c.want)
		}
	}
	if access.PrincipalFrom(context.Background()) != nil {
		t.Error("principal in an empty context")
	}
	p := &access.Principal{Kind: access.KindUser}
	if access.PrincipalFrom(access.WithPrincipal(context.Background(), p)) != p {
		t.Error("principal lost")
	}
}

func TestCatalogRegistration(t *testing.T) {
	c := access.NewCatalog()
	for _, bad := range []string{"", "*", "content", "Content.edit", "content.", ".edit", "content..edit", "content.ed it", "1a.b"} {
		if err := c.AddPolicy(access.Policy{Name: bad}); err == nil {
			t.Errorf("policy %q accepted", bad)
		}
	}
	if err := c.AddPolicy(access.Policy{Name: "pages.edit", Limitations: vs("x")}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddPolicy(access.Policy{Name: "pages.edit"}); err == nil {
		t.Error("duplicate policy accepted")
	}
	if err := c.AddPolicyLimitation("pages.edit", "y"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddPolicyLimitation("pages.edit", "y"); err != nil {
		t.Fatal(err)
	}
	if err := c.AddPolicyLimitation("nope.edit", "y"); err == nil {
		t.Error("unknown policy extended")
	}
	if p, _ := c.Policy("pages.edit"); !reflect.DeepEqual(p.Limitations, vs("x", "y")) {
		t.Errorf("%v", p.Limitations)
	}
	e := newEnv(t)
	if err := e.cat.AddLimitation(access.CoreLimitations(nil, nil)[0]); err == nil {
		t.Error("duplicate limitation accepted")
	}
	var names []string
	for _, p := range e.cat.Policies() {
		names = append(names, p.Name)
	}
	if !reflect.DeepEqual(names, []string{"audit.read", "content.create", "content.delete", "content.edit", "content.preview", "content.preview.share",
		"content.publish", "content.read", "release.manage", "review.approve", "review.bypass", "review.submit", "role.manage", "token.manage", "user.manage"}) {
		t.Errorf("%v", names)
	}
	var lims []string
	for _, l := range e.cat.Limitations() {
		lims = append(lims, l.Name())
	}
	if !reflect.DeepEqual(lims, []string{"channel", "collection", "locale", "owner"}) {
		t.Errorf("%v", lims)
	}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var inv *access.InvalidError
	if !errors.As(err, &inv) {
		t.Fatalf("not an InvalidError: %v", err)
	}
	return inv.Fields
}

func TestValidateRole(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := e.cat.ValidateRole(ctx, []access.RolePolicy{pol("*"), pol("content.edit", "collection", vs("article"), "locale", vs("en", "de")),
		pol("content.read", "owner", vs("self"), "channel", vs("careers_en")), pol("audit.read")}); err != nil {
		t.Fatal(err)
	}
	err := e.cat.ValidateRole(ctx, []access.RolePolicy{
		pol("content.read"),
		pol("nope.read"),
		pol("content.read", "locale", vs("en"), "bogus", vs("x"), "collection", vs()),
		pol("content.edit", "locale", vs("xx"), "collection", vs("ghost"), "channel", vs("nowhere")),
		pol("content.delete", "locale", vs("en")),
		pol("user.manage", "collection", vs("article")),
		pol("content.read", "owner", vs("others")),
	})
	got := fieldsOf(t, err)
	for _, k := range []string{"policies[1].policy", "policies[2].limitations.locale", "policies[2].limitations.bogus",
		"policies[2].limitations.collection", "policies[3].limitations.locale", "policies[3].limitations.collection",
		"policies[3].limitations.channel", "policies[4].limitations.locale", "policies[5].limitations.collection",
		"policies[6].limitations.owner"} {
		if got[k] == "" {
			t.Errorf("missing %s in %v", k, got)
		}
	}
	if len(got) != 10 {
		t.Errorf("%v", got)
	}
	if !strings.Contains(got["policies[2].limitations.locale"], "does not apply") {
		t.Errorf("%q", got["policies[2].limitations.locale"])
	}
}

func TestValidateAssignment(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	role := []access.RolePolicy{pol("content.read"), pol("content.edit")}
	if err := e.cat.ValidateAssignment(ctx, role, map[string][]string{"channel": vs("careers_en"), "locale": vs("en")}); err != nil {
		t.Fatal(err)
	}
	if err := e.cat.ValidateAssignment(ctx, role, nil); err != nil {
		t.Fatal(err)
	}
	got := fieldsOf(t, e.cat.ValidateAssignment(ctx, []access.RolePolicy{pol("content.read"), pol("audit.read")},
		map[string][]string{"locale": vs("en"), "channel": vs("ghost"), "nope": vs("x"), "owner": vs()}))
	if len(got) != 4 || got["limitations.locale"] == "" || got["limitations.channel"] == "" ||
		got["limitations.nope"] == "" || got["limitations.owner"] == "" {
		t.Errorf("%v", got)
	}
	if err := e.cat.ValidateAssignment(ctx, []access.RolePolicy{pol("*")}, map[string][]string{"locale": vs("en")}); err != nil {
		t.Errorf("star role: %v", err)
	}
}

func TestChoices(t *testing.T) {
	e := newEnv(t)
	vals := func(name string) []string {
		l, _ := e.cat.Limitation(name)
		cs, err := l.Choices(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, c := range cs {
			out = append(out, c.Value)
		}
		return out
	}
	if got := vals("collection"); !reflect.DeepEqual(got, vs("article", "job", "settings")) {
		t.Errorf("%v", got)
	}
	if got := vals("locale"); !reflect.DeepEqual(got, vs("de", "en", "fr")) {
		t.Errorf("%v", got)
	}
	if got := vals("channel"); !reflect.DeepEqual(got, vs("careers_en", "corp_de")) {
		t.Errorf("%v", got)
	}
	if got := vals("owner"); !reflect.DeepEqual(got, vs("self")) {
		t.Errorf("%v", got)
	}
}

func TestCoreLimitationMatchAndScope(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	mine := &store.Item{CreatedBy: e.alice.Actor().String()}
	theirs := &store.Item{CreatedBy: "user:other"}
	cases := []struct {
		name   string
		values []string
		t      access.Target
		match  bool
		scope  string // type to scope; "" skips
		ok     bool   // scope restricts
		err    bool   // scope fails
	}{
		{"collection", vs("article"), access.Target{Type: "article"}, true, "article", false, false},
		{"collection", vs("article"), access.Target{Type: "job"}, false, "job", false, true},
		{"collection", vs("article"), access.Target{}, false, "", false, false},
		{"locale", vs("en", "de"), access.Target{Type: "article", Locales: vs("en", "de")}, true, "article", false, false},
		{"locale", vs("en"), access.Target{Type: "article", Locales: vs("en", "de")}, false, "article", false, false},
		{"locale", vs("en"), access.Target{Type: "article"}, true, "", false, false},
		{"owner", vs("self"), access.Target{Type: "article", Item: mine}, true, "article", true, false},
		{"owner", vs("self"), access.Target{Type: "article", Item: theirs}, false, "", false, false},
		{"owner", vs("self"), access.Target{Type: "article"}, true, "", false, false},
		{"channel", vs("careers_en"), access.Target{Type: "job", Locales: vs("en")}, true, "job", false, false},
		{"channel", vs("careers_en"), access.Target{Type: "job", Locales: vs("de")}, false, "", false, false},
		{"channel", vs("careers_en"), access.Target{Type: "article"}, false, "article", false, true},
		{"channel", vs("careers_en", "corp_de"), access.Target{Type: "article", Locales: vs("en", "de")}, true, "article", false, false},
		{"channel", vs("careers_en", "corp_de"), access.Target{Type: "job", Locales: vs("de")}, true, "", false, false},
		{"channel", vs("careers_en", "corp_de"), access.Target{Type: "settings"}, false, "settings", false, true},
		{"channel", vs("careers_en"), access.Target{}, false, "", false, false},
	}
	for i, c := range cases {
		l, _ := e.cat.Limitation(c.name)
		got, err := l.Match(ctx, e.alice, c.values, c.t)
		if err != nil || got != c.match {
			t.Errorf("%d %s match = %v, %v", i, c.name, got, err)
		}
		if c.scope == "" {
			continue
		}
		part, ok, err := l.Scope(ctx, e.alice, c.values, c.scope)
		if (err != nil) != c.err || ok != c.ok {
			t.Errorf("%d %s scope = %+v, %v, %v", i, c.name, part, ok, err)
		}
		if c.name == "owner" && !reflect.DeepEqual(part.CreatedBy, vs(e.alice.Actor().String())) {
			t.Errorf("owner part %+v", part)
		}
	}
}

func TestSystemAndSuperPass(t *testing.T) {
	e := newEnv(t)
	for _, ctx := range []context.Context{
		context.Background(),
		access.WithPrincipal(context.Background(), &access.Principal{Kind: access.KindToken, Super: true}),
		access.WithPrincipal(context.Background(), &access.Principal{Kind: access.KindSystem}),
	} {
		if err := e.chk.Can(ctx, access.PolicyRoleManage, access.Target{}); err != nil {
			t.Error(err)
		}
		if scoped, terms, err := e.chk.ListScope(ctx, access.PolicyContentRead, "article"); scoped || terms != nil || err != nil {
			t.Error(scoped, terms, err)
		}
	}
	if e.db.viewCount() != 0 {
		t.Error("super principals hit the database")
	}
}

func TestNoGrantsForbids(t *testing.T) {
	e := newEnv(t)
	err := e.chk.Can(e.ctx(), access.PolicyContentRead, access.Target{Type: "article"})
	if !forbiddenErr(err) || !strings.Contains(err.Error(), "content.read") {
		t.Fatalf("%v", err)
	}
	scoped, terms, err := e.chk.ListScope(e.ctx(), access.PolicyContentRead, "article")
	if !scoped || len(terms) != 0 || err != nil {
		t.Errorf("%v %v %v", scoped, terms, err)
	}
}

func TestStarGrant(t *testing.T) {
	e := newEnv(t)
	e.grant("administrator", []access.RolePolicy{pol("*")}, nil)
	for _, p := range []string{access.PolicyContentDelete, access.PolicyUserManage, access.PolicyAuditRead} {
		if err := e.chk.Can(e.ctx(), p, access.Target{Type: "article"}); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if scoped, _, _ := e.chk.ListScope(e.ctx(), access.PolicyContentRead, "job"); scoped {
		t.Error("star grant restricted")
	}
	// A limitation on an assignment of "*" narrows every policy: no type means no match.
	e2 := newEnv(t)
	e2.grant("administrator", []access.RolePolicy{pol("*")}, map[string][]string{"channel": vs("careers_en")})
	if err := e2.chk.Can(e2.ctx(), access.PolicyContentEdit, access.Target{Type: "job", Locales: vs("en")}); err != nil {
		t.Error(err)
	}
	if err := e2.chk.Can(e2.ctx(), access.PolicyContentEdit, access.Target{Type: "article", Locales: vs("en")}); !forbiddenErr(err) {
		t.Error(err)
	}
	if err := e2.chk.Can(e2.ctx(), access.PolicyUserManage, access.Target{}); !forbiddenErr(err) {
		t.Error(err)
	}
}

func TestGrantMerging(t *testing.T) {
	e := newEnv(t)
	e.grant("editor", []access.RolePolicy{
		pol("content.edit", "collection", vs("article", "job"), "locale", vs("en", "de")),
		pol("content.read", "collection", vs("article", "job")),
		pol("user.manage"),
	}, map[string][]string{"collection": vs("job"), "locale": vs("en"), "channel": vs("careers_en")})
	grants, err := e.chk.Grants(e.ctx(), e.alice)
	if err != nil {
		t.Fatal(err)
	}
	want := []access.Grant{
		{Policy: "content.edit", Role: "editor", Limits: []access.Limit{{"collection", vs("article", "job")}, {"locale", vs("en", "de")},
			{"channel", vs("careers_en")}, {"collection", vs("job")}, {"locale", vs("en")}}},
		{Policy: "content.read", Role: "editor", Limits: []access.Limit{{"collection", vs("article", "job")},
			{"channel", vs("careers_en")}, {"collection", vs("job")}}},
		{Policy: "user.manage", Role: "editor"},
	}
	if !reflect.DeepEqual(grants, want) {
		t.Fatalf("%+v", grants)
	}
	ctx := e.ctx()
	ok := access.Target{Type: "job", Locales: vs("en")}
	if err := e.chk.Can(ctx, "content.edit", ok); err != nil {
		t.Error(err)
	}
	for name, tg := range map[string]access.Target{
		"role collection and assignment collection both apply": {Type: "article", Locales: vs("en")},
		"assignment locale narrows the role locales":           {Type: "job", Locales: vs("en", "de")},
		"channel language": {Type: "job", Locales: vs("de")},
	} {
		if err := e.chk.Can(ctx, "content.edit", tg); !forbiddenErr(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := e.chk.Can(ctx, "content.read", access.Target{Type: "job"}); err != nil {
		t.Errorf("locale limit must not apply to read: %v", err)
	}
	if err := e.chk.Can(ctx, "user.manage", access.Target{}); err != nil {
		t.Error(err)
	}
}

func TestGroupAndTokenGrants(t *testing.T) {
	e := newEnv(t)
	g := ids.UUID{7}
	e.alice.Groups = []ids.UUID{g}
	e.db.set(store.PrincipalRef{Kind: store.PrincipalGroup, ID: g}, "viewers", []access.RolePolicy{pol("content.read")}, nil)
	if err := e.chk.Can(e.ctx(), "content.read", access.Target{Type: "job"}); err != nil {
		t.Error(err)
	}
	tok := &access.Principal{Kind: access.KindAgent, ID: ids.UUID{8}}
	e.db.set(store.PrincipalRef{Kind: store.PrincipalToken, ID: tok.ID}, "ai-contributor", []access.RolePolicy{pol("content.edit")}, nil)
	tctx := access.WithPrincipal(context.Background(), tok)
	if err := e.chk.Can(tctx, "content.edit", access.Target{Type: "job"}); err != nil {
		t.Error(err)
	}
	if err := e.chk.Can(tctx, "content.read", access.Target{Type: "job"}); !forbiddenErr(err) {
		t.Error(err)
	}
}

func TestUnknownPolicyAndLimitationFailClosed(t *testing.T) {
	e := newEnv(t)
	e.grant("old", []access.RolePolicy{pol("gone.thing"), {Policy: "content.read", Limitations: map[string][]string{"vanished": vs("x")}}}, nil)
	if err := e.chk.Can(e.ctx(), "content.read", access.Target{Type: "job"}); !forbiddenErr(err) {
		t.Error(err)
	}
	if scoped, terms, _ := e.chk.ListScope(e.ctx(), "content.read", "job"); !scoped || len(terms) != 0 {
		t.Error(scoped, terms)
	}
}

func TestOwnerAndListScope(t *testing.T) {
	e := newEnv(t)
	ctx := e.ctx()
	me := e.alice.Actor().String()
	e.grant("a", []access.RolePolicy{pol("content.read", "owner", vs("self"), "collection", vs("article"))}, nil)
	e.grant("b", []access.RolePolicy{pol("content.read", "collection", vs("job"))}, nil)

	scoped, terms, err := e.chk.ListScope(ctx, "content.read", "article")
	if err != nil || !scoped || !reflect.DeepEqual(terms, []store.ScopeTerm{{CreatedBy: vs(me)}}) {
		t.Errorf("article: %v %+v %v", scoped, terms, err)
	}
	if scoped, terms, _ = e.chk.ListScope(ctx, "content.read", "job"); scoped || terms != nil {
		t.Errorf("job should be unrestricted: %v %+v", scoped, terms)
	}
	if scoped, terms, _ = e.chk.ListScope(ctx, "content.read", "settings"); !scoped || len(terms) != 0 {
		t.Errorf("settings: %v %+v", scoped, terms)
	}
	if scoped, terms, _ = e.chk.ListScope(ctx, "content.edit", "article"); !scoped || len(terms) != 0 {
		t.Errorf("other policy: %v %+v", scoped, terms)
	}

	own := access.Target{Type: "article", Item: &store.Item{CreatedBy: me}}
	if err := e.chk.Can(ctx, "content.read", own); err != nil {
		t.Error(err)
	}
	other := access.Target{Type: "article", Item: &store.Item{CreatedBy: "user:x"}}
	if err := e.chk.Can(ctx, "content.read", other); !forbiddenErr(err) {
		t.Error(err)
	}

	// Two owner limitations intersect; the scope part is the same actor.
	e2 := newEnv(t)
	e2.grant("r", []access.RolePolicy{pol("content.read", "owner", vs("self"))}, map[string][]string{"owner": vs("self")})
	_, terms, _ = e2.chk.ListScope(e2.ctx(), "content.read", "article")
	if !reflect.DeepEqual(terms, []store.ScopeTerm{{CreatedBy: vs(e2.alice.Actor().String())}}) {
		t.Errorf("%+v", terms)
	}
}

func TestRulesAllowDeny(t *testing.T) {
	e := newEnv(t)
	e.grant("editor", []access.RolePolicy{pol("*")}, nil)
	e.cat.AddRules("article", access.Rules{
		Delete: func(context.Context, *access.Principal) access.Result { return access.Deny() },
		Read:   func(context.Context, *access.Principal) access.Result { return access.Allow() },
	})
	ctx := e.ctx()
	tg := access.Target{Type: "article"}
	if err := e.chk.Can(ctx, "content.read", tg); err != nil {
		t.Error(err)
	}
	if err := e.chk.Can(ctx, "content.delete", tg); !forbiddenErr(err) || !strings.Contains(err.Error(), "content.delete") {
		t.Error(err)
	}
	if err := e.chk.Can(ctx, "content.delete", access.Target{Type: "job"}); err != nil {
		t.Error(err)
	}
	if err := e.chk.Can(context.Background(), "content.delete", tg); err != nil {
		t.Errorf("system: %v", err)
	}
	if scoped, terms, _ := e.chk.ListScope(ctx, "content.delete", "article"); !scoped || len(terms) != 0 {
		t.Error("denied listing", scoped, terms)
	}
	if scoped, _, _ := e.chk.ListScope(ctx, "content.read", "article"); scoped {
		t.Error("allow rule restricted")
	}
	// preview has no code rule.
	if err := e.chk.Can(ctx, "content.preview", tg); err != nil {
		t.Error(err)
	}
}

func TestRulesWhere(t *testing.T) {
	e := newEnv(t)
	e.grant("editor", []access.RolePolicy{pol("content.read"), pol("content.edit", "collection", vs("article"))}, nil)
	news := func(context.Context, *access.Principal) access.Result {
		return access.Where("kind", store.OpEq, "news")
	}
	e.cat.AddRules("article", access.Rules{Read: news, Update: news})
	e.cat.AddRules("article", access.Rules{Update: func(context.Context, *access.Principal) access.Result {
		return access.And(access.Where("rating", store.OpGte, 3), access.Where("tags", store.OpContains, "x"))
	}})
	ctx := e.ctx()

	if err := e.chk.Can(ctx, "content.read", access.Target{Type: "article", Doc: map[string]any{"kind": "news"}}); err != nil {
		t.Error(err)
	}
	if err := e.chk.Can(ctx, "content.read", access.Target{Type: "article", Doc: map[string]any{"kind": "video"}}); !forbiddenErr(err) {
		t.Error(err)
	}
	if err := e.chk.Can(ctx, "content.read", access.Target{Type: "article"}); !forbiddenErr(err) {
		t.Error("missing field should not match")
	}
	if err := e.chk.Can(ctx, "content.read", access.Target{Type: "job"}); err != nil {
		t.Error(err)
	}
	// Update is checked before and after the change: both must match, both rules AND.
	before := access.Target{Type: "article", Doc: map[string]any{"kind": "news", "rating": 4.0, "tags": []any{"x", "y"}}}
	after := access.Target{Type: "article", Doc: map[string]any{"kind": "video", "rating": 4.0, "tags": []any{"x"}}}
	low := access.Target{Type: "article", Doc: map[string]any{"kind": "news", "rating": 1.0, "tags": []any{"x"}}}
	if err := e.chk.Can(ctx, "content.edit", before); err != nil {
		t.Error(err)
	}
	if err := e.chk.Can(ctx, "content.edit", after); !forbiddenErr(err) {
		t.Error("moved out of scope", err)
	}
	if err := e.chk.Can(ctx, "content.edit", low); !forbiddenErr(err) {
		t.Error("second rule ignored", err)
	}

	// Listings: conditions get resolved columns and ANDed into every term.
	scoped, terms, err := e.chk.ListScope(ctx, "content.read", "article")
	wantCond := store.Cond{Field: "kind", Column: store.ColText, Op: store.OpEq, Value: "news"}
	if err != nil || !scoped || !reflect.DeepEqual(terms, []store.ScopeTerm{{Where: []store.Cond{wantCond}}}) {
		t.Errorf("%v %+v %v", scoped, terms, err)
	}
	if scoped, terms, _ = e.chk.ListScope(ctx, "content.read", "job"); scoped || terms != nil {
		t.Errorf("job: %v %+v", scoped, terms)
	}
	_, terms, _ = e.chk.ListScope(ctx, "content.edit", "article")
	if len(terms) != 1 || len(terms[0].Where) != 3 || terms[0].Where[1].Column != store.ColNum || terms[0].Where[1].Value != 3.0 {
		t.Errorf("%+v", terms)
	}
}

func TestRulesWhereWithTerms(t *testing.T) {
	e := newEnv(t)
	e.grant("a", []access.RolePolicy{pol("content.read", "owner", vs("self"))}, nil)
	e.grant("b", []access.RolePolicy{pol("content.read", "collection", vs("article"))}, nil)
	e.cat.AddRules("article", access.Rules{Read: func(context.Context, *access.Principal) access.Result {
		return access.Where("kind", store.OpIn, []string{"news", "pr"})
	}})
	scoped, terms, err := e.chk.ListScope(e.ctx(), "content.read", "article")
	if err != nil || !scoped {
		t.Fatal(scoped, err)
	}
	if len(terms) != 1 {
		t.Fatalf("%+v", terms)
	}
	// Grant b is unrestricted, so the rule becomes the single term.
	if got := terms[0]; len(got.CreatedBy) != 0 || len(got.Where) != 1 || got.Where[0].Op != store.OpIn ||
		!reflect.DeepEqual(got.Where[0].Value, []any{"news", "pr"}) {
		t.Errorf("%+v", got)
	}

	e = newEnv(t)
	e.grant("a", []access.RolePolicy{pol("content.read", "owner", vs("self"))}, nil)
	e.cat.AddRules("article", access.Rules{Read: func(context.Context, *access.Principal) access.Result {
		return access.Where("kind", store.OpEq, "news")
	}})
	_, terms, _ = e.chk.ListScope(e.ctx(), "content.read", "article")
	if len(terms) != 1 || len(terms[0].CreatedBy) != 1 || len(terms[0].Where) != 1 {
		t.Errorf("%+v", terms)
	}
}

func TestRuleErrors(t *testing.T) {
	e := newEnv(t)
	e.grant("a", []access.RolePolicy{pol("*")}, nil)
	e.cat.AddRules("article", access.Rules{Read: func(context.Context, *access.Principal) access.Result {
		return access.Where("title", store.OpEq, "x") // not indexed
	}})
	err := e.chk.Can(e.ctx(), "content.read", access.Target{Type: "article"})
	if err == nil || forbiddenErr(err) {
		t.Errorf("%v", err)
	}
	if _, _, err := e.chk.ListScope(e.ctx(), "content.read", "article"); err == nil {
		t.Error("list scope accepted a bad rule")
	}
}

func TestAllowed(t *testing.T) {
	e := newEnv(t)
	e.grant("contrib", []access.RolePolicy{pol("content.read"), pol("content.edit")}, nil)
	got, err := e.chk.Allowed(e.ctx(), access.Target{Type: "job"}, "content.edit", "content.publish", "content.delete")
	if err != nil || !reflect.DeepEqual(got, map[string]bool{"content.edit": true, "content.publish": false, "content.delete": false}) {
		t.Errorf("%v %v", got, err)
	}
}

func TestGrantCacheInvalidation(t *testing.T) {
	e := newEnv(t)
	ctx := e.ctx()
	tg := access.Target{Type: "job"}
	e.grant("viewer", []access.RolePolicy{pol("content.read")}, nil)
	for range 3 {
		if err := e.chk.Can(ctx, "content.read", tg); err != nil {
			t.Fatal(err)
		}
	}
	if n := e.db.viewCount(); n != 1 {
		t.Fatalf("%d loads for 3 checks", n)
	}
	e.db.clear()
	if err := e.chk.Can(ctx, "content.read", tg); err != nil {
		t.Fatal("cache dropped without notice")
	}
	e.chk.Invalidate()
	if err := e.chk.Can(ctx, "content.read", tg); !forbiddenErr(err) {
		t.Fatal("grant survived Invalidate", err)
	}
	if n := e.db.viewCount(); n != 2 {
		t.Fatalf("%d loads", n)
	}
}

func TestGrantCacheNotification(t *testing.T) {
	e := newEnv(t)
	ctx := e.ctx()
	tg := access.Target{Type: "job"}
	e.grant("viewer", []access.RolePolicy{pol("content.read")}, nil)
	if err := e.chk.Can(ctx, "content.read", tg); err != nil {
		t.Fatal(err)
	}
	e.db.clear()
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.chk.Run(runCtx) }()
	e.db.notify <- store.Notification{Channel: access.NotifyChannel}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := e.chk.Can(ctx, "content.read", tg); forbiddenErr(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("notification did not drop the cache")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Error(err)
	}
}

func TestTemplates(t *testing.T) {
	e := newEnv(t)
	names := map[string][]string{}
	for _, tpl := range access.Templates() {
		if err := e.cat.ValidateRole(context.Background(), tpl.Policies); err != nil {
			t.Errorf("%s: %v", tpl.Name, err)
		}
		for _, p := range tpl.Policies {
			names[tpl.Name] = append(names[tpl.Name], p.Policy)
		}
	}
	want := map[string][]string{
		"administrator":  {"*"},
		"editor":         {"content.read", "content.create", "content.edit", "content.publish", "content.delete", "content.preview", "content.preview.share", "release.manage", "review.submit", "review.approve"},
		"contributor":    {"content.read", "content.create", "content.edit", "content.preview", "review.submit"},
		"ai-contributor": {"content.read", "content.create", "content.edit"},
		"viewer":         {"content.read"},
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("%v", names)
	}
}

func BenchmarkCan(b *testing.B) {
	e := newEnv(b)
	e.grant("editor", []access.RolePolicy{
		pol("content.read", "collection", vs("article", "job")),
		pol("content.edit", "collection", vs("article", "job"), "locale", vs("en", "de")),
		pol("content.publish", "channel", vs("careers_en")),
	}, map[string][]string{"owner": vs("self")})
	e.cat.AddRules("article", access.Rules{Update: func(context.Context, *access.Principal) access.Result {
		return access.Where("kind", store.OpEq, "news")
	}})
	ctx := e.ctx()
	tg := access.Target{Type: "article", Locales: vs("en"), Item: &store.Item{CreatedBy: e.alice.Actor().String()},
		Doc: map[string]any{"kind": "news"}}
	if err := e.chk.Can(ctx, "content.edit", tg); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := e.chk.Can(ctx, "content.edit", tg); err != nil {
			b.Fatal(err)
		}
	}
}

func TestCatalogTemplatePolicies(t *testing.T) {
	c := access.NewCatalog()
	if err := c.AddPolicy(access.Policy{Name: "x.read"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddTemplatePolicies("viewer", "x.read"); err != nil {
		t.Fatal(err)
	}
	if c.AddTemplatePolicies("nope", "x.read") == nil || c.AddTemplatePolicies("viewer", "y.read") == nil {
		t.Error("unknown template or policy accepted")
	}
	for _, tpl := range c.Templates() {
		if want := map[string]int{"viewer": 2}[tpl.Name]; want != 0 && len(tpl.Policies) != want {
			t.Errorf("%s: %d policies, want %d", tpl.Name, len(tpl.Policies), want)
		}
	}
	for _, tpl := range access.Templates() {
		if tpl.Name == "viewer" && len(tpl.Policies) != 1 {
			t.Error("package Templates() must not include module additions")
		}
	}
}
