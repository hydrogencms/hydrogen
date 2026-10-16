package tests

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
)

const blocksTestSchema = `
tokens:
  tone: [light, dark]
blocks:
  hero:
    props:
      heading: { type: string, required: true, max: 30 }
      tone: { type: token, set: tone, default: light }
      cta: { type: ref, to: [page], on_delete: nullify }
      kept: { type: ref, to: [page] }
      note: { type: richtext }
  cards:
    slots:
      items: { allow: [card], min: 1, max: 3 }
  card:
    props:
      title: { type: string, required: true }
types:
  page:
    fields:
      title: { type: string, translatable: true, required: true }
      body: { type: blocks, allow: [hero, cards, shared], translatable: true }
  sharedBlock:
    blockset: true
    fields:
      label: { type: string }
      content: { type: blocks, allow: [hero, cards] }
`

func blocksEnv(t *testing.T, fn func(t *testing.T, e *env)) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		fn(t, newEnv(t, b.open(t, clk), clk, envConfig{schema: blocksTestSchema}))
	})
}

func bodyOf(v *content.View) []any {
	b, _ := v.Fields["body"].([]any)
	return b
}

func blockID(b any) string { return b.(map[string]any)["id"].(string) }

func TestBlockIDsAreAssignedAndStable(t *testing.T) {
	blocksEnv(t, func(t *testing.T, e *env) {
		v := e.create("page", "en", `{"title":"P","body":[
			{"type":"hero","props":{"heading":"A"}},
			{"type":"cards","slots":{"items":[{"type":"card","props":{"title":"x"}},{"type":"card","props":{"title":"y"}}]}}]}`)
		body := bodyOf(v)
		first := blockID(body[0])
		if _, err := ids.Parse(first); err != nil || first[14] != '7' {
			t.Fatalf("id %q is not a UUIDv7", first)
		}
		items := body[1].(map[string]any)["slots"].(map[string]any)["items"].([]any)
		seen := map[string]bool{first: true, blockID(body[1]): true}
		for _, it := range items {
			seen[blockID(it)] = true
		}
		if len(seen) != 4 {
			t.Fatalf("ids are not distinct: %v", seen)
		}
		e.publish(v.ID)

		// The editor sends the whole tree back with its ids: they stay; a new block gets one.
		raw := asJSON(map[string]any{"body": append(body, map[string]any{"type": "hero", "props": map[string]any{"heading": "B"}})})
		u := e.update(v.ID, "en", raw)
		ub := bodyOf(u)
		if blockID(ub[0]) != first || blockID(ub[1]) != blockID(body[1]) || blockID(ub[2]) == "" {
			t.Fatalf("ids changed across saves: %v", ub)
		}
		// The first save of a translation assigns its own ids.
		de := e.update(v.ID, "de", `{"title":"P de","body":[{"type":"hero","props":{"heading":"Hallo"}}]}`)
		if id := blockID(bodyOf(de)[0]); id == first || id == "" {
			t.Fatalf("german id = %q", id)
		}
	})
}

func TestBlockValidationThroughTheService(t *testing.T) {
	blocksEnv(t, func(t *testing.T, e *env) {
		hero := func(id string) string { return `{"id":"` + id + `","type":"hero","props":{"heading":"A"}}` }
		const id1, id2 = "0190a000-0000-7000-8000-000000000001", "0190a000-0000-7000-8000-000000000002"
		tests := []struct {
			name, body string
			want       []string
		}{
			{"duplicate ids", `[` + hero(id1) + `,` + hero(id1) + `]`, []string{"body[1].id:duplicate_id"}},
			{"malformed id", `[{"id":"nope","type":"hero"}]`, []string{"body[0].id:format"}},
			{"unknown type", `[{"type":"ghost"}]`, []string{"body[0].type:block_type"}},
			{"type not allowed", `[{"type":"card","props":{"title":"x"}}]`, []string{"body[0].type:block_type"}},
			{"prop error", `[` + hero(id1) + `,{"id":"` + id2 + `","type":"hero","props":{"heading":7}}]`, []string{"body[1].props.heading:type"}},
			{"unknown prop", `[{"type":"hero","props":{"x":1}}]`, []string{"body[0].props.x:unknown_field"}},
			{"nested error", `[{"type":"cards","slots":{"items":[{"type":"card","props":{"title":3}}]}}]`, []string{"body[0].slots.items[0].props.title:type"}},
			{"slot max", `[{"type":"cards","slots":{"items":[{"type":"card"},{"type":"card"},{"type":"card"},{"type":"card"}]}}]`, []string{"body[0].slots.items:max_items"}},
			{"bad token", `[{"type":"hero","props":{"tone":"pink"}}]`, []string{"body[0].props.tone:enum"}},
		}
		for _, tc := range tests {
			_, err := e.svc.Create(ctx, content.CreateInput{Type: "page", Locale: "en", Fields: doc(t, `{"title":"P","body":`+tc.body+`}`)})
			eqSlice(t, tc.name, codes(wantValidation(t, err)), tc.want)
		}

		// Required props and slot minimums are checked at publish, not on drafts.
		v := e.create("page", "en", `{"title":"P","body":[{"type":"hero","props":{}},{"type":"cards"}]}`)
		_, err := e.svc.Publish(ctx, v.ID, content.PublishOptions{})
		eqSlice(t, "publish", codes(wantValidation(t, err)), []string{"body[0].props.heading:required", "body[1].slots.items:min_items"})
	})
}

func TestBlockDefaultsApplyOnCreate(t *testing.T) {
	blocksEnv(t, func(t *testing.T, e *env) {
		v := e.create("page", "en", `{"title":"P","body":[{"type":"hero","props":{"heading":"A"}}]}`)
		props := bodyOf(v)[0].(map[string]any)["props"].(map[string]any)
		eq(t, "tone default", props["tone"], any("light"))
	})
}

func TestBlockProjections(t *testing.T) {
	blocksEnv(t, func(t *testing.T, e *env) {
		target := e.create("page", "en", `{"title":"Target"}`)
		e.publish(target.ID)
		kept := e.create("page", "en", `{"title":"Kept"}`)
		e.publish(kept.ID)
		linked := e.create("page", "en", `{"title":"Linked"}`)
		e.publish(linked.ID)

		body := fmt.Sprintf(`[
			{"type":"hero","props":{"heading":"Welcome zeppelin","cta":%q,"kept":%q,"note":{"type":"doc","content":[{"type":"paragraph","content":[
				{"type":"text","text":"read","marks":[{"type":"link","attrs":{"href":"item:%s"}}]}]}]}}},
			{"type":"cards","slots":{"items":[{"type":"card","props":{"title":"Marmalade card"}}]}}]`, target.ID, kept.ID, linked.ID)
		p := e.create("page", "en", `{"title":"Home","body":`+body+`}`)
		e.publish(p.ID)

		// Search covers block text; block ids and props that are not text are not indexed.
		for _, q := range []string{"zeppelin", "marmalade"} {
			res, err := e.svc.List(ctx, content.ListOptions{Type: "page", Search: q})
			if err != nil || len(res.Items) != 1 || res.Items[0].ID != p.ID {
				t.Fatalf("search %q = %+v, %v", q, res, err)
			}
		}

		// References keep their block paths; delete policies apply to them.
		refs, err := e.svc.References(ctx, target.ID)
		if err != nil || len(refs) != 1 {
			t.Fatalf("references = %+v, %v", refs, err)
		}
		eq(t, "path", refs[0].FieldPath, "body[0].props.cta")
		eq(t, "onDelete", refs[0].OnDelete, "nullify")
		link, err := e.svc.References(ctx, linked.ID)
		if err != nil || len(link) != 1 || link[0].Kind != "link" || link[0].FieldPath != "body[0].props.note" {
			t.Fatalf("link references = %+v, %v", link, err)
		}
		wantErr(t, e.svc.Delete(ctx, kept.ID), content.ErrReferenced)

		hardDelete(t, e.svc, target.ID)
		pub := e.get(p.ID, "", false)
		props := bodyOf(pub)[0].(map[string]any)["props"].(map[string]any)
		if _, ok := props["cta"]; ok {
			t.Errorf("nullified block ref kept: %v", props)
		}
		eq(t, "version bumped", pub.Version, 2)

		// A ref to the wrong type fails at publish with the block path.
		person := e.create("sharedBlock", "en", `{"label":"S"}`)
		bad := e.create("page", "en", fmt.Sprintf(`{"title":"Bad","body":[{"type":"hero","props":{"heading":"x","kept":%q}}]}`, person.ID))
		_, err = e.svc.Publish(ctx, bad.ID, content.PublishOptions{})
		eqSlice(t, "ref target", codes(wantValidation(t, err)), []string{"body[0].props.kept:ref_target"})
	})
}

func TestSharedBlockRefMustBeABlockset(t *testing.T) {
	blocksEnv(t, func(t *testing.T, e *env) {
		other := e.create("page", "en", `{"title":"Not a blockset"}`)
		e.publish(other.ID)
		p := e.create("page", "en", fmt.Sprintf(`{"title":"P","body":[{"type":"shared","props":{"item":%q}}]}`, other.ID))
		_, err := e.svc.Publish(ctx, p.ID, content.PublishOptions{})
		eqSlice(t, "wrong type", codes(wantValidation(t, err)), []string{"body[0].props.item:ref_target"})

		set := e.create("sharedBlock", "en", `{"label":"S","content":[{"type":"hero","props":{"heading":"H"}}]}`)
		e.publish(set.ID)
		ok := e.create("page", "en", fmt.Sprintf(`{"title":"P","body":[{"type":"shared","props":{"item":%q}}]}`, set.ID))
		e.publish(ok.ID)
		// Deleting a shared item that pages use is refused.
		wantErr(t, e.svc.Delete(ctx, set.ID), content.ErrReferenced)
	})
}

func TestSchemaRevisionIncludesBlocks(t *testing.T) {
	changed := strings.Replace(blocksTestSchema, "[light, dark]", "[light, dark, brand]", 1)
	if buildSchema(t, blocksTestSchema).Hash() == buildSchema(t, changed).Hash() {
		t.Fatal("tokens are not part of the schema hash")
	}
	s := buildSchema(t, blocksTestSchema)
	if _, ok := s.Block("shared"); !ok {
		t.Fatal("no shared block")
	}
}

func TestBreakingBlockChangesNeedAMigration(t *testing.T) {
	narrowed := strings.Replace(strings.Replace(blocksTestSchema, "[hero, cards, shared]", "[hero, shared]", 1),
		"[light, dark]", "[light]", 1)
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		newEnv(t, db, clk, envConfig{schema: blocksTestSchema})
		e := newEnv(t, db, clk, envConfig{schema: narrowed, skipRev: true})
		got, err := e.svc.BreakingChanges(ctx)
		if err != nil {
			t.Fatal(err)
		}
		eqSlice(t, "breaking", got, []string{"page.body: allow narrowed (removed cards)", "token:tone: values removed (dark)"})

		m := content.Migration{Name: "0001_narrow", Type: "page", Changes: []string{"page.body", "token:tone"},
			Up: func(*content.MigrationDoc) error { return nil }}
		e = newEnv(t, db, clk, envConfig{schema: narrowed, migrations: []content.Migration{m}, skipRev: true})
		if got, err = e.svc.BreakingChanges(ctx); err != nil || len(got) != 0 {
			t.Fatalf("breaking with migration = %v, %v", got, err)
		}
	})
}
