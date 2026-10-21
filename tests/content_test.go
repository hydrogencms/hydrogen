package tests

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

func (e *env) create(typ, locale, js string) *content.View {
	e.t.Helper()
	e.tick()
	v, err := e.svc.Create(ctx, content.CreateInput{Type: typ, Locale: locale, Fields: doc(e.t, js)})
	if err != nil {
		e.t.Fatalf("create %s: %v", typ, err)
	}
	return v
}

func (e *env) update(id ids.UUID, locale, js string) *content.View {
	e.t.Helper()
	e.tick()
	v, err := e.svc.Update(ctx, id, content.UpdateInput{Locale: locale, Fields: doc(e.t, js)})
	if err != nil {
		e.t.Fatalf("update: %v", err)
	}
	return v
}

func (e *env) publish(id ids.UUID, locales ...string) *content.View {
	e.t.Helper()
	e.tick()
	v, err := e.svc.Publish(ctx, id, content.PublishOptions{Locales: locales})
	if err != nil {
		e.t.Fatalf("publish: %v", err)
	}
	return v
}

func (e *env) person(name string) *content.View {
	e.t.Helper()
	v := e.create("person", "en", fmt.Sprintf(`{"fullName":%q}`, name))
	return e.publish(v.ID)
}

func (e *env) article(title string) *content.View {
	e.t.Helper()
	v := e.create("article", "en", fmt.Sprintf(`{"title":%q}`, title))
	return e.publish(v.ID)
}

func (e *env) get(id ids.UUID, locale string, draft bool) *content.View {
	e.t.Helper()
	v, err := e.svc.Get(ctx, id, content.GetOptions{Locale: locale, Draft: draft})
	if err != nil {
		e.t.Fatalf("get: %v", err)
	}
	return v
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func eqSlice(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want %v", err, target)
	}
}

func wantValidation(t *testing.T, err error) *content.ValidationError {
	t.Helper()
	var ve *content.ValidationError
	if !errors.As(err, &ve) || !errors.Is(err, content.ErrInvalid) {
		t.Fatalf("error = %v, want a ValidationError", err)
	}
	return ve
}

func codes(ve *content.ValidationError) []string {
	var out []string
	for _, fe := range ve.Errors {
		out = append(out, fe.Path+":"+fe.Code)
	}
	return out
}

func TestCreateWithDefaults(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"Hello World"}`)
		eq(t, "status", v.Status, content.StatusDraft)
		eq(t, "name", v.Name, "Hello World")
		eq(t, "kind", v.Fields["kind"], any("news"))
		eq(t, "featured", v.Fields["featured"], any(false))
		eq(t, "slug", v.Fields["slug"], any("hello-world"))
		eqSlice(t, "draft parts", v.DraftParts, []string{"shared", "en"})
		eqSlice(t, "locales", v.Locales, []string{"en"})
		if _, err := e.svc.Get(ctx, v.ID, content.GetOptions{}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("published get of draft item = %v, want ErrNotFound", err)
		}
		d := e.get(v.ID, "", true)
		eq(t, "draft title", d.Fields["title"], any("Hello World"))
		eqSlice(t, "audit", e.auditActions(v.ID), []string{"item.create"})
		evs := e.drain()
		if len(evs) != 1 || evs[0].Topic != "item.created" {
			t.Fatalf("events = %+v", evs)
		}
		eq(t, "payload", asJSON(doc(t, string(evs[0].Payload))), fmt.Sprintf(`{"draft":true,"id":%q,"locales":["en"],"state":%q,"type":"article","version":0}`, v.ID, content.StateOf(v.UpdatedAt)))
	})
}

func TestInvalidDraftWrites(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		_, err := e.svc.Create(ctx, content.CreateInput{Type: "article", Fields: doc(t, `{"rating":9,"foo":1,"tags":["ok"]}`)})
		ve := wantValidation(t, err)
		eqSlice(t, "errors", codes(ve), []string{"rating:max", "foo:unknown_field"})
		eq(t, "locale", ve.Locale, "en")
		res, err := e.svc.List(ctx, content.ListOptions{})
		if err != nil || res.Total != 0 {
			t.Fatalf("list after failed create = %+v, %v", res, err)
		}

		a := e.create("article", "en", `{"title":"A"}`)
		_, err = e.svc.Update(ctx, a.ID, content.UpdateInput{Fields: doc(t, `{"tags":["x","y"],"rating":"high"}`)})
		eqSlice(t, "update errors", codes(wantValidation(t, err)), []string{"rating:type"})
		if d := e.get(a.ID, "", true); d.Fields["tags"] != nil {
			t.Errorf("failed update changed the draft: %v", d.Fields)
		}
	})
}

func TestRequiredOnlyAtPublish(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"summary":"s"}`)
		_, err := e.svc.Publish(ctx, v.ID, content.PublishOptions{})
		ve := wantValidation(t, err)
		eqSlice(t, "errors", codes(ve), []string{"title:required"})
		eq(t, "locale", ve.Locale, "en")
		e.update(v.ID, "en", `{"title":"Now"}`)
		e.publish(v.ID)
	})
}

func TestPublishViewsAndVersions(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("First")
		eq(t, "status", a.Status, content.StatusPublished)
		eq(t, "version", a.Version, 1)
		if a.PublishedAt == nil || len(a.DraftParts) != 0 {
			t.Errorf("view = %+v", a)
		}
		g := e.get(a.ID, "", false)
		eq(t, "title", g.Fields["title"], any("First"))
		e.update(a.ID, "en", `{"title":"Second"}`)
		eq(t, "published still", e.get(a.ID, "", false).Fields["title"], any("First"))
		ch := e.get(a.ID, "", true)
		eq(t, "status", ch.Status, content.StatusChanged)
		eq(t, "draft title", ch.Fields["title"], any("Second"))
		e.publish(a.ID)
		vs, err := e.svc.Versions(ctx, a.ID)
		if err != nil || len(vs) != 2 || vs[0].Version != 2 {
			t.Fatalf("versions = %+v, %v", vs, err)
		}
		eqSlice(t, "audit", e.auditActions(a.ID), []string{"item.create", "item.publish", "item.update", "item.publish"})
		_, err = e.svc.GetByKey(ctx, "nope", content.GetOptions{})
		wantErr(t, err, content.ErrNotFound)
		_, err = e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		wantErr(t, err, content.ErrBadRequest) // nothing to publish
	})
}

func TestRevertingToPublishedDropsDraft(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("Same")
		e.update(a.ID, "en", `{"summary":"x"}`)
		eqSlice(t, "parts", e.get(a.ID, "", true).DraftParts, []string{"en"})
		v := e.update(a.ID, "en", `{"summary":null}`)
		eq(t, "status", v.Status, content.StatusPublished)
		eq(t, "parts", len(v.DraftParts), 0)
	})
}

func TestTranslationsAreIndependent(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("Hello")
		e.update(a.ID, "de", `{"title":"Hallo"}`)
		e.update(a.ID, "fr", `{"title":"Bonjour"}`)
		eqSlice(t, "parts", e.get(a.ID, "", true).DraftParts, []string{"de", "fr"})
		v := e.publish(a.ID, "de")
		eq(t, "version", v.Version, 2)
		eq(t, "status", v.Status, content.StatusChanged)
		eqSlice(t, "draft parts", v.DraftParts, []string{"fr"})
		eqSlice(t, "locales", e.get(a.ID, "", false).Locales, []string{"de", "en"})
		eq(t, "de", e.get(a.ID, "de", false).Fields["title"], any("Hallo"))
		// A locale that is only a draft: shared fields, no name.
		fr := e.get(a.ID, "fr", false)
		eq(t, "fr name", fr.Name, "")
		eq(t, "fr title", fr.Fields["title"], any(nil))
		eq(t, "fr kind", fr.Fields["kind"], any("news"))
		eq(t, "fr draft", e.get(a.ID, "fr", true).Fields["title"], any("Bonjour"))
		e.publish(a.ID)
		eq(t, "final version", e.get(a.ID, "fr", false).Fields["title"], any("Bonjour"))
	})
}

func TestFirstPublishNeedsMainLocale(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"A"}`)
		e.update(v.ID, "fr", `{"title":"B"}`)
		_, err := e.svc.Publish(ctx, v.ID, content.PublishOptions{Locales: []string{"fr"}})
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.Publish(ctx, v.ID, content.PublishOptions{Locales: []string{"de"}})
		wantErr(t, err, content.ErrBadRequest)
		e.publish(v.ID, "en")
	})
}

// publishBehind simulates another publisher: a new version in which locale's document differs.
func (e *env) publishBehind(id ids.UUID, locale, title string) {
	e.t.Helper()
	err := e.db.Tx(ctx, func(tx store.Tx) error {
		c := tx.Content()
		it, err := c.GetItem(ctx, id, true)
		if err != nil {
			return err
		}
		v, err := c.GetVersion(ctx, id, it.PublishedVersion)
		if err != nil {
			return err
		}
		v.Version++
		v.CreatedAt = e.clk.Now()
		for i := range v.Locales {
			if v.Locales[i].Locale == locale {
				v.Locales[i].Data = []byte(fmt.Sprintf(`{"title":%q}`, title))
			}
		}
		if err := c.InsertVersion(ctx, v); err != nil {
			return err
		}
		it.PublishedVersion = v.Version
		return c.UpdateItem(ctx, it)
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func TestConflictAndForce(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("Base")
		e.update(a.ID, "en", `{"title":"Mine"}`)
		e.publishBehind(a.ID, "en", "Theirs")
		_, err := e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		wantErr(t, err, content.ErrConflict)
		var ce *content.ConflictError
		if !errors.As(err, &ce) || !slices.Equal(ce.Parts, []string{"en"}) {
			t.Fatalf("conflict = %v", err)
		}
		v, err := e.svc.Publish(ctx, a.ID, content.PublishOptions{Force: true})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "version", v.Version, 3)
		eq(t, "title", v.Fields["title"], any("Mine"))
	})
}

func TestNoConflictWhenPartUnchanged(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("Base")
		e.update(a.ID, "de", `{"title":"Hallo"}`) // base version 1
		e.publishBehind(a.ID, "en", "Other")      // en changed, de did not exist
		e.publish(a.ID, "de")
	})
}

func TestMergePatch(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.create("article", "en", `{"title":"T","summary":"s","address":{"city":"X"}}`)
		v := e.update(a.ID, "en", `{"summary":null,"address":{"street":"S"}}`)
		if _, ok := v.Fields["summary"]; ok {
			t.Errorf("summary kept: %v", v.Fields)
		}
		eq(t, "address", asJSON(v.Fields["address"]), `{"city":"X","street":"S"}`)
		v = e.update(a.ID, "en", `{"address":{"city":null}}`)
		eq(t, "address", asJSON(v.Fields["address"]), `{"street":"S"}`)
		e.update(a.ID, "en", `{"tags":["a"]}`)
		v = e.update(a.ID, "en", `{"tags":["b","c"]}`)
		eq(t, "tags replaced", asJSON(v.Fields["tags"]), `["b","c"]`)
	})
}

func TestSlugFromTitle(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.create("article", "en", `{"title":"Ünïcode & Friends!"}`)
		eq(t, "slug", a.Fields["slug"], any("unicode-friends"))
		v := e.update(a.ID, "en", `{"title":"Changed"}`)
		eq(t, "slug kept", v.Fields["slug"], any("unicode-friends"))
		v = e.update(a.ID, "en", `{"slug":"mine"}`)
		eq(t, "explicit slug", v.Fields["slug"], any("mine"))
	})
}

func TestHiddenFieldsPruned(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.create("article", "en", `{"title":"V","kind":"video","video":"https://example.com/v"}`)
		eq(t, "video", a.Fields["video"], any("https://example.com/v"))
		v := e.update(a.ID, "en", `{"kind":"news"}`)
		if _, ok := v.Fields["video"]; ok {
			t.Errorf("hidden field kept: %v", v.Fields)
		}
	})
}

func TestReferenceTargets(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		p := e.person("Ann")
		a := e.create("article", "en", `{"title":"A"}`)
		missing := ids.NewGenerator(e.clk, nil).New()
		e.update(a.ID, "en", fmt.Sprintf(`{"author":%q}`, missing))
		_, err := e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		eqSlice(t, "missing", codes(wantValidation(t, err)), []string{"author:ref_target"})

		other := e.article("Other")
		e.update(a.ID, "en", fmt.Sprintf(`{"author":%q}`, other.ID))
		_, err = e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		eqSlice(t, "wrong type", codes(wantValidation(t, err)), []string{"author:ref_target"})

		e.update(a.ID, "en", fmt.Sprintf(`{"author":%q,"related":[%q,%q]}`, p.ID, other.ID, missing))
		_, err = e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		eqSlice(t, "list element", codes(wantValidation(t, err)), []string{"related[1]:ref_target"})
		e.update(a.ID, "en", fmt.Sprintf(`{"related":[%q]}`, other.ID))
		e.publish(a.ID)

		refs, err := e.svc.References(ctx, p.ID)
		if err != nil || len(refs) != 1 {
			t.Fatalf("references = %+v, %v", refs, err)
		}
		eq(t, "referrer", refs[0].ItemID, a.ID)
		eq(t, "type", refs[0].Type, "article")
		eq(t, "name", refs[0].Name, "A")
		eq(t, "field", refs[0].FieldPath, "author")
		eq(t, "onDelete", refs[0].OnDelete, "block")
	})
}

func TestDeletePolicies(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		blocked, free := e.person("Blocked"), e.person("Free")
		a := e.create("article", "en", fmt.Sprintf(`{"title":"A","author":%q,"editor":%q,"reviewer":%q}`, blocked.ID, free.ID, free.ID))
		e.publish(a.ID)

		err := e.svc.Delete(ctx, blocked.ID)
		wantErr(t, err, content.ErrReferenced)
		var re *content.ReferencedError
		if !errors.As(err, &re) || len(re.Referrers) != 1 || re.Referrers[0].Name != "A" || re.Referrers[0].Type != "article" {
			t.Fatalf("referenced = %v", err)
		}
		if _, err := e.svc.Get(ctx, blocked.ID, content.GetOptions{}); err != nil {
			t.Fatal(err)
		}

		e.update(a.ID, "en", `{"summary":"draft edit","editor":null}`) // a draft that still has the nullified ref gone
		e.update(a.ID, "en", fmt.Sprintf(`{"editor":%q}`, free.ID))
		hardDelete(t, e.svc, free.ID)
		_, err = e.svc.Get(ctx, free.ID, content.GetOptions{})
		wantErr(t, err, content.ErrNotFound)
		pub := e.get(a.ID, "", false)
		eq(t, "version bumped", pub.Version, 2)
		if _, ok := pub.Fields["editor"]; ok {
			t.Errorf("nullified ref kept: %v", pub.Fields)
		}
		if pub.Fields["reviewer"] != free.ID.String() { // warn: the dangling id stays; only nullify fields clear
			t.Errorf("reviewer (on_delete: warn) changed: %v", pub.Fields)
		}
		dr := e.get(a.ID, "", true)
		if _, ok := dr.Fields["editor"]; ok {
			t.Errorf("draft kept nullified ref: %v", dr.Fields)
		}
		eq(t, "draft change kept", dr.Fields["summary"], any("draft edit"))
		acts := e.auditActions(a.ID)
		eq(t, "last audit", acts[len(acts)-1], "item.update")
		eqSlice(t, "deleted audit", e.auditActions(free.ID), []string{"item.create", "item.publish", "item.trash", "item.purge"})
		// warn left a dangling reviewer id: publishing must fail until it is fixed.
		_, err = e.svc.Publish(ctx, a.ID, content.PublishOptions{})
		if ve := wantValidation(t, err); !slices.Contains(codes(ve), "reviewer:ref_target") {
			t.Fatalf("dangling warn ref: codes %v", codes(ve))
		}
		e.update(a.ID, "en", `{"reviewer":null}`)
		e.publish(a.ID) // not a conflict: the draft was rebased
		if _, err := e.svc.Versions(ctx, free.ID); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("versions of deleted = %v", err)
		}
		// Deleting the referrer first unblocks.
		if err := e.svc.Delete(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Delete(ctx, blocked.ID); err != nil {
			t.Fatal(err)
		}
	})
}

func TestListProjections(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		p := e.person("Zed")
		mk := func(title, js string) *content.View {
			v := e.create("article", "en", js)
			e.update(v.ID, "en", fmt.Sprintf(`{"title":%q}`, title))
			e.clk.Advance(time.Second)
			return e.publish(v.ID)
		}
		a := mk("Alpha", `{"rating":1,"featured":true,"tags":["go","cms"],"publishDate":"2026-01-01T00:00:00Z","day":"2026-02-01","kind":"video"}`)
		b := mk("Beta", fmt.Sprintf(`{"rating":3,"tags":["go"],"publishDate":"2026-06-01T12:00:00+02:00","day":"2026-03-01","author":%q}`, p.ID))
		c := mk("Gamma", `{"rating":5}`)
		d := e.create("article", "en", `{"title":"Draft only"}`)

		list := func(o content.ListOptions) []string {
			t.Helper()
			o.Type = "article"
			res, err := e.svc.List(ctx, o)
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, it := range res.Items {
				names = append(names, it.Name)
			}
			return names
		}
		w := func(field string, op store.Op, v any) content.ListOptions {
			return content.ListOptions{Where: []content.Filter{{Field: field, Op: op, Value: v}}, Sort: []content.SortSpec{{Field: "name"}}}
		}
		eqSlice(t, "num gte", list(w("rating", store.OpGte, "3")), []string{"Beta", "Gamma"})
		eqSlice(t, "num eq typed", list(w("rating", store.OpEq, 1)), []string{"Alpha"})
		eqSlice(t, "num in", list(w("rating", store.OpIn, "1,5")), []string{"Alpha", "Gamma"})
		eqSlice(t, "bool", list(w("featured", store.OpEq, "true")), []string{"Alpha"})
		eqSlice(t, "enum", list(w("kind", store.OpEq, "video")), []string{"Alpha"})
		eqSlice(t, "text prefix", list(w("slug", store.OpPrefix, "ga")), []string{"Gamma"})
		eqSlice(t, "datetime gt", list(w("publishDate", store.OpGt, "2026-03-01T00:00:00Z")), []string{"Beta"})
		eqSlice(t, "date eq", list(w("day", store.OpEq, "2026-02-01")), []string{"Alpha"})
		eqSlice(t, "ref eq", list(w("author", store.OpEq, p.ID.String())), []string{"Beta"})
		eqSlice(t, "list contains", list(w("tags", store.OpContains, "go")), []string{"Alpha", "Beta"})
		eqSlice(t, "list contains rare", list(w("tags", store.OpContains, "cms")), []string{"Alpha"})
		eqSlice(t, "exists", list(w("author", store.OpExists, "true")), []string{"Beta"})
		eqSlice(t, "not exists", list(w("author", store.OpExists, false)), []string{"Alpha", "Draft only", "Gamma"})
		eqSlice(t, "ne", list(w("rating", store.OpNe, 1)), []string{"Beta", "Draft only", "Gamma"})
		eqSlice(t, "search", list(content.ListOptions{Search: "gam"}), []string{"Gamma"})
		eqSlice(t, "sort desc", list(content.ListOptions{Sort: []content.SortSpec{{Field: "rating", Desc: true}}, Status: store.StatusPublished}), []string{"Gamma", "Beta", "Alpha"})
		eqSlice(t, "published only", list(content.ListOptions{Status: store.StatusPublished, Sort: []content.SortSpec{{Field: "name"}}}), []string{"Alpha", "Beta", "Gamma"})
		eqSlice(t, "draft status", list(content.ListOptions{Status: store.StatusDraft}), []string{"Draft only"})

		res, err := e.svc.List(ctx, content.ListOptions{Type: "article", Sort: []content.SortSpec{{Field: "name"}}, Limit: 2, Offset: 1})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "total", res.Total, 4)
		eq(t, "page", len(res.Items), 2)
		eq(t, "page first", res.Items[0].Name, "Beta")
		eq(t, "status", res.Items[0].Status, content.StatusPublished)
		_ = []*content.View{a, b, c, d}

		for _, bad := range []content.Filter{{Field: "summary", Op: store.OpEq, Value: "x"}, {Field: "nope", Op: store.OpEq, Value: 1},
			{Field: "rating", Op: store.OpEq, Value: "abc"}, {Field: "rating", Op: store.OpContains, Value: 1}} {
			_, err := e.svc.List(ctx, content.ListOptions{Type: "article", Where: []content.Filter{bad}})
			wantErr(t, err, content.ErrBadRequest)
		}
		_, err = e.svc.List(ctx, content.ListOptions{Type: "article", Sort: []content.SortSpec{{Field: "summary"}}})
		wantErr(t, err, content.ErrBadRequest)

		// publishedAt orders by the latest publication; never published items come last either way
		e.update(a.ID, "en", `{"rating":2}`)
		e.clk.Advance(time.Minute)
		e.publish(a.ID)
		byPub := func(desc bool, st store.Status) []string {
			return list(content.ListOptions{Status: st, Sort: []content.SortSpec{{Field: "publishedAt", Desc: desc}}})
		}
		eqSlice(t, "publishedAt desc", byPub(true, store.StatusPublished), []string{"Alpha", "Gamma", "Beta"})
		eqSlice(t, "publishedAt asc", byPub(false, store.StatusPublished), []string{"Beta", "Gamma", "Alpha"})
		eqSlice(t, "publishedAt drafts last", byPub(true, store.StatusAny), []string{"Alpha", "Gamma", "Beta", "Draft only"})
		_, err = e.svc.List(ctx, content.ListOptions{Type: "ghost"})
		wantErr(t, err, content.ErrUnknownType)

		// Query limits protect the Local API as well as the REST API.
		eqs := func(n int) []content.Filter {
			return slices.Repeat([]content.Filter{{Field: "rating", Op: store.OpGte, Value: 0}}, n)
		}
		okOpts := content.ListOptions{Type: "article", Where: eqs(content.MaxConditions),
			Sort:   slices.Repeat([]content.SortSpec{{Field: "name"}}, content.MaxSortKeys),
			Search: strings.Repeat("a ", content.MaxSearchTerms)}
		if _, err := e.svc.List(ctx, okOpts); err != nil {
			t.Errorf("queries at the limits must work: %v", err)
		}
		inList := func(n int) content.ListOptions {
			return content.ListOptions{Type: "article", Where: []content.Filter{{Field: "rating", Op: store.OpIn, Value: slices.Repeat([]any{1.0}, n)}}}
		}
		if _, err := e.svc.List(ctx, inList(content.MaxInValues)); err != nil {
			t.Errorf("in list at the limit: %v", err)
		}
		for name, o := range map[string]content.ListOptions{
			"conditions": {Type: "article", Where: eqs(content.MaxConditions + 1)},
			"in values":  inList(content.MaxInValues + 1),
			"search":     {Type: "article", Search: strings.Repeat("a ", content.MaxSearchTerms+1)},
			"sort keys":  {Type: "article", Sort: slices.Repeat([]content.SortSpec{{Field: "name"}}, content.MaxSortKeys+1)},
		} {
			_, err := e.svc.List(ctx, o)
			if !errors.Is(err, content.ErrBadRequest) {
				t.Errorf("%s over the limit: %v", name, err)
			}
		}

		// Management lists the working copy; a published-only list follows the current version, and updates on republish.
		e.update(a.ID, "en", `{"rating":4}`)
		eqSlice(t, "draft visible", list(w("rating", store.OpEq, 4)), []string{"Alpha"})
		eqSlice(t, "draft not published", list(content.ListOptions{Type: "article", Status: store.StatusPublished,
			Where: []content.Filter{{Field: "rating", Op: store.OpEq, Value: 4}}}), nil)
		e.publish(a.ID)
		eqSlice(t, "after republish", list(w("rating", store.OpEq, 4)), []string{"Alpha"})
	})
}

func TestTranslatableIndexPerLocale(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("Hello")
		e.update(a.ID, "de", `{"title":"Hallo","slug":"hallo"}`)
		e.publish(a.ID)
		for locale, want := range map[string]int{"de": 1, "en": 0} {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "article", Locale: locale,
				Where: []content.Filter{{Field: "slug", Op: store.OpEq, Value: "hallo"}}})
			if err != nil {
				t.Fatal(err)
			}
			eq(t, "matches in "+locale, res.Total, want)
		}
		res, err := e.svc.List(ctx, content.ListOptions{Type: "article", Locale: "de", Search: "hallo"})
		if err != nil || res.Total != 1 || res.Items[0].Name != "Hallo" {
			t.Fatalf("search de = %+v, %v", res, err)
		}
	})
}

func TestSingletons(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		_, err := e.svc.Singleton(ctx, "settings", content.GetOptions{})
		wantErr(t, err, content.ErrNotFound)
		_, err = e.svc.Singleton(ctx, "article", content.GetOptions{})
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.UpdateSingleton(ctx, "article", content.UpdateInput{})
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.UpdateSingleton(ctx, "ghost", content.UpdateInput{})
		wantErr(t, err, content.ErrUnknownType)

		v, err := e.svc.UpdateSingleton(ctx, "settings", content.UpdateInput{Fields: doc(t, `{"siteName":"Acme"}`)})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "key", v.Key, "settings")
		v2, err := e.svc.UpdateSingleton(ctx, "settings", content.UpdateInput{Fields: doc(t, `{"maintenance":true}`)})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "same item", v2.ID, v.ID)
		if _, err := e.svc.PublishSingleton(ctx, "settings", content.PublishOptions{}); err != nil {
			t.Fatal(err)
		}
		g, err := e.svc.Singleton(ctx, "settings", content.GetOptions{})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "siteName", g.Fields["siteName"], any("Acme"))
		eq(t, "maintenance", g.Fields["maintenance"], any(true))
		_, err = e.svc.Create(ctx, content.CreateInput{Type: "settings", Key: "other"})
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.Create(ctx, content.CreateInput{Type: "settings"})
		wantErr(t, err, content.ErrDuplicateKey)
		k := "renamed"
		_, err = e.svc.Update(ctx, v.ID, content.UpdateInput{Key: &k})
		wantErr(t, err, content.ErrBadRequest)
	})
}

func TestKeysLocalesAndTypes(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		_, err := e.svc.Create(ctx, content.CreateInput{Type: "ghost"})
		wantErr(t, err, content.ErrUnknownType)
		_, err = e.svc.Create(ctx, content.CreateInput{Type: "person", Locale: "EN_us"})
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.Create(ctx, content.CreateInput{Type: "person", Key: "Bad Key"})
		wantErr(t, err, content.ErrBadRequest)
		a, err := e.svc.Create(ctx, content.CreateInput{Type: "person", Key: "ann", Fields: doc(t, `{"fullName":"Ann"}`)})
		if err != nil {
			t.Fatal(err)
		}
		_, err = e.svc.Create(ctx, content.CreateInput{Type: "person", Key: "ann"})
		wantErr(t, err, content.ErrDuplicateKey)
		e.publish(a.ID)
		g, err := e.svc.GetByKey(ctx, "ann", content.GetOptions{})
		if err != nil || g.ID != a.ID {
			t.Fatalf("by key = %+v, %v", g, err)
		}
		_, err = e.svc.Update(ctx, a.ID, content.UpdateInput{Locale: "x"})
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.Update(ctx, ids.UUID{1}, content.UpdateInput{})
		wantErr(t, err, content.ErrNotFound)
		always := true
		v, err := e.svc.Update(ctx, a.ID, content.UpdateInput{AlwaysAvailable: &always})
		if err != nil || !v.AlwaysAvailable {
			t.Fatalf("always = %+v, %v", v, err)
		}
	})
}

func TestDiscard(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("A")
		e.update(a.ID, "en", `{"summary":"x","kind":"video","video":"https://e.com"}`)
		e.update(a.ID, "de", `{"title":"D"}`)
		eqSlice(t, "parts", e.get(a.ID, "", true).DraftParts, []string{"shared", "de", "en"})
		if err := e.svc.Discard(ctx, a.ID, []string{"shared", "de"}); err != nil {
			t.Fatal(err)
		}
		v := e.get(a.ID, "", true)
		eqSlice(t, "parts", v.DraftParts, []string{"en"})
		eq(t, "kind", v.Fields["kind"], any("news"))
		if err := e.svc.Discard(ctx, a.ID, nil); err != nil {
			t.Fatal(err)
		}
		eq(t, "status", e.get(a.ID, "", true).Status, content.StatusPublished)
		acts := e.auditActions(a.ID)
		eq(t, "audit", acts[len(acts)-1], "item.discard")
		wantErr(t, e.svc.Discard(ctx, a.ID, []string{"!"}), content.ErrBadRequest)
	})
}

func TestHooks(t *testing.T) {
	var log []string
	forEachBackend(t, func(t *testing.T, b backend) {
		log = nil
		add := func(r *hooks.Registry, ev hooks.Event, fn hooks.Func) {
			if err := r.Add(hooks.Hook{Type: "article", Event: ev, Fn: fn}); err != nil {
				t.Fatal(err)
			}
		}
		e := newEnvFor(t, b, envConfig{setup: func(r *hooks.Registry) {
			add(r, hooks.BeforeValidate, func(c *hooks.Context) error {
				log = append(log, "beforeValidate")
				if s, ok := c.Doc["summary"].(string); ok {
					c.Doc["summary"] = s + "!"
				}
				return nil
			})
			add(r, hooks.Validate, func(c *hooks.Context) error {
				log = append(log, "validate")
				if s, _ := c.Doc["summary"].(string); s == "bad!" {
					c.Errors.Add("summary", "custom", "no bad summaries")
				}
				return nil
			})
			add(r, hooks.BeforeSave, func(c *hooks.Context) error {
				log = append(log, "beforeSave")
				if c.Tx == nil {
					t.Error("sync hook without a transaction")
				}
				c.Doc["rating"] = 2
				return nil
			})
			add(r, hooks.BeforePublish, func(c *hooks.Context) error {
				log = append(log, "beforePublish")
				if t, _ := c.Doc["title"].(string); t == "Blocked" {
					c.Errors.Add("title", "blocked", "title is blocked")
				}
				if t, _ := c.Doc["title"].(string); t == "Explode" {
					return errors.New("boom")
				}
				return nil
			})
			add(r, hooks.AfterPublish, func(c *hooks.Context) error {
				log = append(log, "afterPublish:"+c.Locale)
				if t, _ := c.Doc["title"].(string); t == "Rollback" {
					return errors.New("after failed")
				}
				return nil
			})
		}})
		_, err := e.svc.Create(ctx, content.CreateInput{Type: "article", Fields: doc(t, `{"title":"T","summary":"bad"}`)})
		eqSlice(t, "validate error", codes(wantValidation(t, err)), []string{"summary:custom"})
		eqSlice(t, "order on failure", log, []string{"beforeValidate", "validate"})

		log = nil
		v := e.create("article", "en", `{"title":"T","summary":"ok"}`)
		eqSlice(t, "write order", log, []string{"beforeValidate", "validate", "beforeSave"})
		eq(t, "mutated summary", v.Fields["summary"], any("ok!"))
		eq(t, "saved computed", asJSON(v.Fields["rating"]), "2")

		log = nil
		e.publish(v.ID)
		eqSlice(t, "publish order", log, []string{"validate", "beforePublish", "afterPublish:en"})

		blocked := e.create("article", "en", `{"title":"Blocked"}`)
		_, err = e.svc.Publish(ctx, blocked.ID, content.PublishOptions{})
		eqSlice(t, "blocked", codes(wantValidation(t, err)), []string{"title:blocked"})
		explode := e.create("article", "en", `{"title":"Explode"}`)
		if _, err = e.svc.Publish(ctx, explode.ID, content.PublishOptions{}); err == nil || !strings.Contains(err.Error(), "boom") {
			t.Fatalf("hook error not returned: %v", err)
		}
		eq(t, "still unpublished", e.get(explode.ID, "", true).Version, 0)
	})
}

func newEnvFor(t *testing.T, b backend, cfg envConfig) *env {
	t.Helper()
	clk := newClock()
	return newEnv(t, b.open(t, clk), clk, cfg)
}

func TestHookFailureRollsBackAuditAndOutbox(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := newEnvFor(t, b, envConfig{setup: func(r *hooks.Registry) {
			_ = r.Add(hooks.Hook{Event: hooks.AfterPublish, Fn: func(c *hooks.Context) error {
				if t, _ := c.Doc["title"].(string); t == "Rollback" {
					return errors.New("after failed")
				}
				return nil
			}})
		}})
		v := e.create("article", "en", `{"title":"Rollback"}`)
		e.drain()
		if _, err := e.svc.Publish(ctx, v.ID, content.PublishOptions{}); err == nil {
			t.Fatal("publish should fail")
		}
		eqSlice(t, "audit", e.auditActions(v.ID), []string{"item.create"})
		if evs := e.drain(); len(evs) != 0 {
			t.Errorf("events after failed tx: %v", evs)
		}
		d := e.get(v.ID, "", true)
		eq(t, "version", d.Version, 0)
		eqSlice(t, "drafts kept", d.DraftParts, []string{"shared", "en"})
		if vs, _ := e.svc.Versions(ctx, v.ID); len(vs) != 0 {
			t.Errorf("versions = %v", vs)
		}
	})
}

func TestAsyncHooks(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		type call struct {
			event  hooks.Event
			locale string
			title  any
			tx     bool
			typ    string
		}
		var calls []call
		rec := func(c *hooks.Context) error {
			calls = append(calls, call{c.Event, c.Locale, c.Doc["title"], c.Tx != nil, c.Item.Type})
			return nil
		}
		e := newEnvFor(t, b, envConfig{setup: func(r *hooks.Registry) {
			_ = r.Add(hooks.Hook{Type: "article", Event: hooks.AfterPublish, Async: true, Name: "notify", Fn: rec})
			_ = r.Add(hooks.Hook{Type: "person", Event: hooks.AfterPublish, Async: true, Name: "people", Fn: func(*hooks.Context) error {
				t.Error("person hook ran for an article")
				return nil
			}})
			_ = r.Add(hooks.Hook{Event: hooks.AfterDelete, Async: true, Name: "gone", Fn: rec})
		}})
		a := e.article("Async")
		if len(calls) != 0 {
			t.Fatal("async hook ran inside the transaction")
		}
		e.drain()
		if len(calls) != 1 || calls[0] != (call{hooks.AfterPublish, "en", "Async", false, "article"}) {
			t.Fatalf("calls = %+v", calls)
		}
		e.drain()
		eq(t, "idempotent drain", len(calls), 1)
		hardDelete(t, e.svc, a.ID)
		e.drain()
		if len(calls) != 2 || calls[1].event != hooks.AfterDelete || calls[1].tx {
			t.Fatalf("calls = %+v", calls)
		}
	})
}

func TestContentMigrationEndToEnd(t *testing.T) {
	const v1 = `
types:
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string }
      address:  { type: group, fields: { street: { type: string }, zip: { type: string } } }
`
	const v2 = `
types:
  person:
    name: "{firstName} {lastName}"
    fields:
      firstName: { type: string }
      lastName:  { type: string }
      address:   { type: group, fields: { street: { type: string } } }
`
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		old := newEnv(t, db, clk, envConfig{schema: v1})
		p := old.create("person", "en", `{"fullName":"Ada Lovelace","address":{"street":"S","zip":"1"}}`)
		old.publish(p.ID)
		old.update(p.ID, "en", `{"fullName":"Ada King"}`)
		oldRev := mustVersions(t, old, p.ID)[0].SchemaRev

		up := func(d *content.MigrationDoc) error {
			full, _ := d.Fields["fullName"].(string)
			if full == "" {
				return nil
			}
			first, last, _ := cutSpace(full)
			d.Fields["firstName"], d.Fields["lastName"] = first, last
			delete(d.Fields, "fullName")
			if addr, ok := d.Fields["address"].(map[string]any); ok {
				delete(addr, "zip")
			}
			return nil
		}
		unregistered := newEnv(t, db, clk, envConfig{schema: v2, skipRev: true})
		got, err := unregistered.svc.BreakingChanges(ctx)
		if err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "breaking", got, []string{"person.address.zip: removed", "person.fullName: removed"})

		m := content.Migration{Name: "0001_split_name", Type: "person", Changes: []string{"person.fullName", "person.address.zip"}, Up: up}
		e := newEnv(t, db, clk, envConfig{schema: v2, migrations: []content.Migration{m}, skipRev: true})
		got, err = e.svc.BreakingChanges(ctx)
		if err != nil || len(got) != 0 {
			t.Fatalf("breaking with migration = %v, %v", got, err)
		}
		pending, err := e.svc.PendingMigrations(ctx)
		if err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "pending", pending, []string{"0001_split_name"})
		done, err := e.svc.Migrate(ctx)
		if err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "done", done, []string{"0001_split_name"})
		pending, _ = e.svc.PendingMigrations(ctx)
		eq(t, "pending after", len(pending), 0)
		done, err = e.svc.Migrate(ctx)
		if err != nil || len(done) != 0 {
			t.Fatalf("second migrate = %v, %v", done, err)
		}

		pub := e.get(p.ID, "", false)
		eq(t, "published first", pub.Fields["firstName"], any("Ada"))
		eq(t, "published last", pub.Fields["lastName"], any("Lovelace"))
		eq(t, "name", pub.Name, "Ada Lovelace")
		eq(t, "address", asJSON(pub.Fields["address"]), `{"street":"S"}`)
		eq(t, "version", pub.Version, 2)
		dr := e.get(p.ID, "", true)
		eq(t, "draft last", dr.Fields["lastName"], any("King"))
		vs := mustVersions(t, e, p.ID)
		if vs[0].SchemaRev <= oldRev || vs[1].SchemaRev != oldRev {
			t.Errorf("schema revs = %+v (old %d)", vs, oldRev)
		}
		for _, tc := range []struct {
			search string
			status store.Status
			want   int
		}{{"King", store.StatusAny, 1}, {"Lovelace", store.StatusAny, 0}, {"Lovelace", store.StatusPublished, 1}, {"King", store.StatusPublished, 0}} {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "person", Search: tc.search, Status: tc.status})
			if err != nil || res.Total != tc.want {
				t.Errorf("migrated working rows: search %q status %v = %+v, %v; want %d", tc.search, tc.status, res, err, tc.want)
			}
		}
		e.publish(p.ID) // draft was rebased: no conflict
		if rev, err := e.svc.BreakingChanges(ctx); err != nil || len(rev) != 0 {
			t.Errorf("breaking after = %v, %v", rev, err)
		}
		res, err := e.svc.List(ctx, content.ListOptions{Type: "person", Search: "King"})
		if err != nil || res.Total != 1 {
			t.Errorf("search after migration = %+v, %v", res, err)
		}
	})
}

func mustVersions(t *testing.T, e *env, id ids.UUID) []store.VersionMeta {
	t.Helper()
	vs, err := e.svc.Versions(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return vs
}

func cutSpace(s string) (string, string, bool) {
	for i := range len(s) {
		if s[i] == ' ' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}

func TestBreakingChangeKinds(t *testing.T) {
	const base = `
types:
  a:
    fields:
      keep:   { type: string, translatable: true }
      gone:   { type: string }
      retype: { type: string }
      flip:   { type: string }
      grp:    { type: group, fields: { x: { type: string }, y: { type: string } } }
  b:
    fields:
      f: { type: string }
`
	const next = `
types:
  a:
    fields:
      keep:   { type: string, translatable: true }
      retype: { type: number }
      flip:   { type: string, translatable: true }
      grp:    { type: group, fields: { x: { type: number } } }
`
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		newEnv(t, db, clk, envConfig{schema: base})
		e := newEnv(t, db, clk, envConfig{schema: next, skipRev: true})
		got, err := e.svc.BreakingChanges(ctx)
		if err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "changes", got, []string{"a.flip: translatable changed", "a.gone: removed", "a.grp.x: type changed",
			"a.grp.y: removed", "a.retype: type changed", "b: removed"})
	})
}

type typedArticle struct {
	Title   string   `json:"title,omitempty"`
	Summary *string  `json:"summary,omitempty"`
	Rating  *float64 `json:"rating,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

func TestTypedHandle(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		h := content.Handle[typedArticle]{Type: "article"}
		reg := &hooks.Registry{}
		h.BeforeSave(registrar{reg}, func(c *hooks.Context, a *typedArticle) error {
			s := "set by hook"
			a.Summary = &s
			return nil
		})
		var after typedArticle
		h.AfterPublish(registrar{reg}, func(c *hooks.Context, a *typedArticle) error { after = *a; return nil })
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{setup: func(r *hooks.Registry) {
			for _, hk := range []hooks.Event{hooks.BeforeSave, hooks.AfterPublish} {
				for _, x := range reg.Sync("article", hk) {
					_ = r.Add(x)
				}
			}
		}})
		rating := 4.0
		item, err := h.Create(ctx, e.svc, "en", &typedArticle{Title: "Typed", Rating: &rating, Tags: []string{"a"}})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "title", item.Doc.Title, "Typed")
		eq(t, "hook summary", *item.Doc.Summary, "set by hook")
		if item.Meta.Fields != nil {
			t.Error("meta fields should be nil")
		}
		item, err = h.Update(ctx, e.svc, item.Meta.ID, "en", &typedArticle{Title: "Typed 2"})
		if err != nil {
			t.Fatal(err)
		}
		if item.Doc.Rating != nil || item.Doc.Tags != nil {
			t.Errorf("omitted fields kept: %+v", item.Doc)
		}
		pub, err := h.Publish(ctx, e.svc, item.Meta.ID, content.PublishOptions{})
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "version", pub.Meta.Version, 1)
		eq(t, "after hook title", after.Title, "Typed 2")
		got, err := h.Get(ctx, e.svc, item.Meta.ID, content.GetOptions{})
		if err != nil || got.Doc.Title != "Typed 2" {
			t.Fatalf("get = %+v, %v", got, err)
		}
		res, err := h.List(ctx, e.svc, content.ListOptions{})
		if err != nil || res.Total != 1 {
			t.Fatalf("list = %+v, %v", res, err)
		}
		other := e.person("P")
		_, err = h.Get(ctx, e.svc, other.ID, content.GetOptions{})
		wantErr(t, err, content.ErrBadRequest)
	})
}

type registrar struct{ r *hooks.Registry }

func (r registrar) AddHook(h hooks.Hook) error { return r.r.Add(h) }

var _ = schema.ModeDraft

func TestListFieldsAndVersionContent(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"Alpha","rating":3}`)
		e.publish(v.ID)
		e.update(v.ID, "en", `{"title":"Alpha 2"}`)

		list := func(locale string, fields ...string) (content.Summary, error) {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "article", Locale: locale, Fields: fields})
			if err != nil || len(res.Items) != 1 {
				return content.Summary{}, err
			}
			return res.Items[0], nil
		}
		got, err := list("en", "title", "rating", "author")
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "title is the draft", got.Fields["title"], "Alpha 2")
		eq(t, "rating", fmt.Sprint(got.Fields["rating"]), "3")
		if _, ok := got.Fields["author"]; ok {
			t.Errorf("unset field returned: %v", got.Fields)
		}
		if got, err = list("de", "title"); err != nil || got.Fields["title"] != "Alpha 2" {
			t.Errorf("main-locale fallback: %v %v", got.Fields, err)
		}
		if got, err = list("en"); err != nil || got.Fields != nil {
			t.Errorf("no fields asked: %v %v", got.Fields, err)
		}
		_, err = list("en", "nope")
		wantErr(t, err, content.ErrBadRequest)
		_, err = list("en", "a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k")
		wantErr(t, err, content.ErrBadRequest)
		_, err = e.svc.List(ctx, content.ListOptions{Fields: []string{"title"}})
		wantErr(t, err, content.ErrBadRequest)

		vc, err := e.svc.VersionContent(ctx, v.ID, 1)
		if err != nil {
			t.Fatal(err)
		}
		doc := mergeVersionFields(vc)
		eq(t, "version", vc.Version, 1)
		eq(t, "version title", doc["title"], "Alpha")
		eq(t, "version rating", fmt.Sprint(doc["rating"]), "3")
		if _, ok := vc.Locales["en"]; !ok {
			t.Errorf("locales = %v", vc.Locales)
		}
		_, err = e.svc.VersionContent(ctx, v.ID, 9)
		wantErr(t, err, content.ErrNotFound)
	})
}

func mergeVersionFields(vc *content.VersionView) map[string]any {
	out := map[string]any{}
	for k, f := range vc.Shared.Fields {
		out[k] = f
	}
	for _, l := range vc.Locales {
		for k, f := range l.Fields {
			out[k] = f
		}
	}
	return out
}

func TestExpectedState(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		v := e.create("article", "en", `{"title":"Alpha"}`)
		state := content.StateOf(v.UpdatedAt)
		withState := func(s string) context.Context { return content.WithExpectedState(ctx, s) }

		e.tick()
		v2, err := e.svc.Update(withState(state), v.ID, content.UpdateInput{Locale: "en", Fields: doc(t, `{"title":"Beta"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if content.StateOf(v2.UpdatedAt) == state {
			t.Fatal("update kept the state")
		}
		// A second client holding the old state is refused, by every mutation, and nothing changes.
		var sm *content.StateMismatchError
		e.tick()
		_, err = e.svc.Update(withState(state), v.ID, content.UpdateInput{Locale: "en", Fields: doc(t, `{"title":"Gamma"}`)})
		if !errors.As(err, &sm) || !errors.Is(err, content.ErrPreconditionFailed) || sm.State != content.StateOf(v2.UpdatedAt) {
			t.Fatalf("stale update: %v", err)
		}
		_, err = e.svc.Publish(withState(state), v.ID, content.PublishOptions{})
		wantErr(t, err, content.ErrPreconditionFailed)
		wantErr(t, e.svc.Discard(withState(state), v.ID, nil), content.ErrPreconditionFailed)
		wantErr(t, e.svc.Delete(withState(state), v.ID), content.ErrPreconditionFailed)
		got, err := e.svc.Get(ctx, v.ID, content.GetOptions{Draft: true})
		if err != nil || got.Fields["title"] != "Beta" {
			t.Fatalf("refused writes changed the item: %v %v", got, err)
		}

		// The same client carries the new state through update, publish, discard and delete.
		e.tick()
		pub, err := e.svc.Publish(withState(content.StateOf(v2.UpdatedAt)), v.ID, content.PublishOptions{})
		if err != nil {
			t.Fatal(err)
		}
		e.tick()
		up, err := e.svc.Update(withState(content.StateOf(pub.UpdatedAt)), v.ID, content.UpdateInput{Locale: "en", Fields: doc(t, `{"title":"Delta"}`)})
		if err != nil {
			t.Fatal(err)
		}
		e.tick()
		if err := e.svc.Discard(withState(content.StateOf(up.UpdatedAt)), v.ID, nil); err != nil {
			t.Fatal(err)
		}
		// No expectation, no check.
		e.tick()
		if err := e.svc.Delete(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
	})
}
