package schema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const blocksYAML = `
tokens:
  tone: [light, dark, brand]
blocks:
  hero:
    label: Hero
    icon: layout
    props:
      heading: { type: string, required: true, max: 20 }
      tone: { type: token, set: tone, default: light }
      cta: { type: ref, to: [page] }
      note: { type: richtext }
      when1: { type: string, when: { field: tone, eq: dark } }
  cards:
    props:
      title: { type: text }
    slots:
      items: { allow: [card], min: 1, max: 2 }
  card:
    props:
      title: { type: string, required: true }
      link: { type: ref, to: [page], on_delete: nullify }
    slots:
      inner: { allow: [card] }
types:
  page:
    page: true
    fields:
      title: { type: string, translatable: true }
      body: { type: blocks, allow: [hero, cards, shared], max: 3, translatable: true }
  sharedBlock:
    blockset: true
    fields:
      title: { type: string }
      content: { type: blocks, allow: [hero] }
`

const (
	b1 = "0190a000-0000-7000-8000-000000000001"
	b2 = "0190a000-0000-7000-8000-000000000002"
	b3 = "0190a000-0000-7000-8000-000000000003"
	b4 = "0190a000-0000-7000-8000-000000000004"
)

func TestBlocksCompile(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	var names []string
	for _, b := range s.Blocks() {
		names = append(names, b.Name)
	}
	if !reflect.DeepEqual(names, []string{"card", "cards", "hero", "shared"}) {
		t.Fatalf("blocks = %v", names)
	}
	hero, _ := s.Block("hero")
	if hero.Label != "Hero" || hero.Icon != "layout" || len(hero.Props) != 5 {
		t.Fatalf("hero = %+v", hero)
	}
	cards, _ := s.Block("cards")
	if sl, ok := cards.Slot("items"); !ok || sl.Min != 1 || sl.Max != 2 || sl.Label != "Items" {
		t.Fatalf("slot = %+v", sl)
	}
	if tk, ok := s.Token("tone"); !ok || !reflect.DeepEqual(tk.Values, []string{"light", "dark", "brand"}) {
		t.Fatalf("tokens = %+v", s.Tokens())
	}
	shared, _ := s.Block("shared")
	if shared.Props[0].Name != "item" || !reflect.DeepEqual(shared.Props[0].Shape.To, []string{"sharedBlock"}) {
		t.Fatalf("shared = %+v", shared.Props[0])
	}
	page, _ := s.Type("page")
	body, _ := page.Field("body")
	if body.Shape.Base != BaseBlocks || !body.Translatable || !reflect.DeepEqual(body.Shape.Allow, []string{"hero", "cards", "shared"}) {
		t.Fatalf("body = %+v", body)
	}
	if !s.MaySharedBlocks(body) {
		t.Fatal("body may hold shared blocks")
	}
	sb, _ := s.Type("sharedBlock")
	if !sb.Blockset {
		t.Fatal("blockset flag")
	}
	if f, _ := sb.BlocksField(); s.MaySharedBlocks(f) {
		t.Fatal("blockset field reaches shared")
	}
}

func TestBlocksHashAndExport(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	other := mustCompile(t, strings.Replace(blocksYAML, "[light, dark, brand]", "[light, dark]", 1))
	if s.Hash() == other.Hash() {
		t.Fatal("hash ignores tokens")
	}
	changed := mustCompile(t, strings.Replace(blocksYAML, "max: 2 }", "max: 3 }", 1))
	if s.Hash() == changed.Hash() {
		t.Fatal("hash ignores slots")
	}
	data, err := s.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Blocks []map[string]any `json:"blocks"`
		Tokens []map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Blocks) != 4 || len(out.Tokens) != 1 {
		t.Fatalf("export: %s", data)
	}
	plain := mustCompile(t, "types:\n  a:\n    fields:\n      t: { type: string }\n")
	if d, _ := plain.MarshalJSON(); strings.Contains(string(d), `"blocks"`) || strings.Contains(string(d), `"tokens"`) {
		t.Fatalf("empty block sets leak into the export: %s", d)
	}
}

func TestBlocksJSONSchema(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	page, _ := s.Type("page")
	js := s.JSONSchema(page)
	data, _ := json.Marshal(js)
	for _, want := range []string{`"$defs"`, `"Block_hero"`, `"Block_card"`, `"Block_shared"`, `"const":"hero"`, `"oneOf"`, `"#/$defs/Block_cards"`,
		`"enum":["light","dark","brand"]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("JSON Schema lacks %s", want)
		}
	}
}

func TestBlocksCompileErrors(t *testing.T) {
	base := func(blocks, fields string) string {
		return "tokens:\n  tone: [light, dark]\nblocks:\n" + blocks + "types:\n  page:\n    fields:\n" + fields +
			"  shell:\n    blockset: true\n    fields:\n      c: { type: blocks, allow: [hero] }\n"
	}
	hero := "  hero:\n    props:\n      h: { type: string }\n"
	body := "      body: { type: blocks, allow: [hero] }\n"
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"blocks field without allow", base(hero, "      body: { type: blocks }\n"), "allow is required"},
		{"empty allow", base(hero, "      body: { type: blocks, allow: [] }\n"), "allow must name at least one"},
		{"unknown allow", base(hero, "      body: { type: blocks, allow: [ghost] }\n"), `unknown block type "ghost"`},
		{"duplicate allow", base(hero, "      body: { type: blocks, allow: [hero, hero] }\n"), "lists \"hero\" twice"},
		{"min over max", base(hero, "      body: { type: blocks, allow: [hero], min: 3, max: 1 }\n"), "min must not exceed max"},
		{"blocks index", base(hero, "      body: { type: blocks, allow: [hero], index: true }\n"), "index is only allowed"},
		{"blocks in list", base(hero, "      l: { type: list, of: { type: blocks, allow: [hero] } }\n"), "top-level"},
		{"blocks in group", base(hero, "      g: { type: group, fields: { b: { type: blocks, allow: [hero] } } }\n"), "top-level"},
		{"blocks as prop", base("  hero:\n    props:\n      b: { type: blocks, allow: [hero] }\n", body), "cannot be block props"},
		{"translatable prop", base("  hero:\n    props:\n      h: { type: string, translatable: true }\n", body), "block props are not translatable"},
		{"index prop", base("  hero:\n    props:\n      h: { type: string, index: true }\n", body), "block props cannot be indexed"},
		{"shared reserved", base("  shared:\n    props: {}\n", body), "reserved"},
		{"unknown block key", base("  hero:\n    bogus: 1\n", body), `unknown key "bogus" in block "hero"`},
		{"bad block name", base("  Hero:\n    props: {}\n", body), `block name "Hero" must match`},
		{"slot unknown block", base("  hero:\n    slots:\n      s: { allow: [ghost] }\n", body), `slot "s" allows unknown block type "ghost"`},
		{"slot no allow", base("  hero:\n    slots:\n      s: { min: 1 }\n", body), "allow is required"},
		{"slot empty allow", base("  hero:\n    slots:\n      s: { allow: [] }\n", body), "at least one block type"},
		{"slot min over max", base("  hero:\n    slots:\n      s: { allow: [hero], min: 3, max: 1 }\n", body), "min must not exceed max"},
		{"slot max zero", base("  hero:\n    slots:\n      s: { allow: [hero], max: 0 }\n", body), "max must be at least 1"},
		{"slot bad key", base("  hero:\n    slots:\n      s: { allow: [hero], x: 1 }\n", body), `unknown key "x" in slot "s"`},
		{"slot bad name", base("  hero:\n    slots:\n      Bad: { allow: [hero] }\n", body), `slot name "Bad" must match`},
		{"token unknown set", base("  hero:\n    props:\n      t: { type: token, set: ghost }\n", body), `unknown token set "ghost"`},
		{"token no set", base("  hero:\n    props:\n      t: { type: token }\n", body), "set is required"},
		{"token bad default", base("  hero:\n    props:\n      t: { type: token, set: tone, default: pink }\n", body), "default is invalid"},
		{"shared without blockset", "blocks:\n  hero:\n    props: {}\ntypes:\n  page:\n    fields:\n      body: { type: blocks, allow: [shared] }\n", "no type has blockset: true"},
		{"blockset without blocks field", base(hero, body) + "  other:\n    blockset: true\n    fields:\n      n: { type: string }\n", "exactly one blocks field (found 0)"},
		{"blockset two blocks fields", base(hero, body) + "  other:\n    blockset: true\n    fields:\n      a: { type: blocks, allow: [hero] }\n      b: { type: blocks, allow: [hero] }\n", "exactly one blocks field (found 2)"},
		{"blockset allows shared", base(hero, body) + "  other:\n    blockset: true\n    fields:\n      a: { type: blocks, allow: [shared] }\n", "must not contain shared blocks"},
		{"blockset reaches shared by slot", base("  hero:\n    slots:\n      s: { allow: [shared] }\n", body), "must not contain shared blocks"},
		{"token empty", "tokens:\n  t: []\n", "needs at least one value"},
		{"token duplicate value", "tokens:\n  t: [a, a]\n", `value "a" is listed twice`},
		{"token bad value", "tokens:\n  t: [\"a b\"]\n", `value "a b" must match`},
		{"token not a list", "tokens:\n  t: x\n", "must be a list of names"},
		{"unknown top key", "bogus: 1\n", "known: blocks, groups, richtext, tokens, types"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compile(t, map[string]string{"schema/s.yaml": tc.yaml})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %v, want %q", err, tc.want)
			}
		})
	}
}

func TestBlocksDuplicatesAcrossFiles(t *testing.T) {
	_, err := compile(t, map[string]string{
		"schema/a.yaml": "tokens:\n  tone: [a]\nblocks:\n  hero:\n    props: {}\n",
		"schema/b.yaml": "tokens:\n  tone: [b]\nblocks:\n  hero:\n    props: {}\n",
	})
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{`schema/b.yaml:2: token set "tone" is already defined at schema/a.yaml:2`,
		`schema/b.yaml:4: block "hero" is already defined at schema/a.yaml:4`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	// Files merge: tokens in one file, blocks using them in another.
	if _, err := compile(t, map[string]string{
		"schema/a.yaml": "tokens:\n  tone: [a]\n",
		"schema/b.yaml": "blocks:\n  hero:\n    props:\n      t: { type: token, set: tone }\ntypes:\n  p:\n    fields:\n      b: { type: blocks, allow: [hero] }\n",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBlocksProgrammatic(t *testing.T) {
	b := NewBuilder(NewRegistry())
	b.SetOrigin("module x")
	if err := b.AddTokens("tone", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddTokens("tone", []string{"c"}); err == nil || !strings.Contains(err.Error(), "already defined") {
		t.Fatalf("duplicate: %v", err)
	}
	if err := b.AddBlock("hero", Spec{"props": Spec{"t": Spec{"type": "token", "set": "tone"}}}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddBlock("hero", Spec{}); err == nil {
		t.Fatal("duplicate block accepted")
	}
	if err := b.AddType("page", Spec{"fields": Spec{"b": Spec{"type": "blocks", "allow": []string{"hero"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Compile(); err != nil {
		t.Fatal(err)
	}
}

func errPaths(errs []FieldError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Path + ":" + e.Code
	}
	return out
}

func TestBlocksValidation(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	page, _ := s.Type("page")
	hero := func(id string) string { return `{"id":"` + id + `","type":"hero","props":{"heading":"Hi"}}` }
	card := func(id string) string { return `{"id":"` + id + `","type":"card","props":{"title":"A"}}` }
	cards := func(id, items string) string {
		return `{"id":"` + id + `","type":"cards","slots":{"items":[` + items + `]}}`
	}
	deep := `{"id":"0190a000-0000-7000-8000-000000000050","type":"card","props":{"title":"x"}}`
	for i := 1; i < 9; i++ {
		deep = `{"id":"0190a000-0000-7000-8000-0000000000` + pad(60 + i)[10:] + `","type":"card","props":{"title":"x"},"slots":{"inner":[` + deep + `]}}`
	}
	deepPath := "body[0].slots.items[0]" + strings.Repeat(".slots.inner[0]", 7) + ".slots.inner:max_depth"
	tests := []struct {
		name string
		body string
		mode Mode
		want []string
	}{
		{"valid", `[` + hero(b1) + `,` + cards(b2, card(b3)) + `]`, ModePublish, nil},
		{"empty", `[]`, ModePublish, nil},
		{"not a list", `{}`, ModeDraft, []string{"body:type"}},
		{"block not an object", `[1]`, ModeDraft, []string{"body[0]:type"}},
		{"max items", `[` + hero(b1) + `,` + hero(b2) + `,` + hero(b3) + `,` + hero(b4) + `]`, ModeDraft, []string{"body:max_items"}},
		{"missing id", `[{"type":"hero","props":{"heading":"x"}}]`, ModeDraft, []string{"body[0].id:required"}},
		{"bad id", `[{"id":"nope","type":"hero"}]`, ModeDraft, []string{"body[0].id:format"}},
		{"uuid v4 id", `[{"id":"0190a000-0000-4000-8000-000000000001","type":"hero"}]`, ModeDraft, []string{"body[0].id:format"}},
		{"id type", `[{"id":3,"type":"hero"}]`, ModeDraft, []string{"body[0].id:type"}},
		{"duplicate id", `[` + hero(b1) + `,` + hero(b1) + `]`, ModeDraft, []string{"body[1].id:duplicate_id"}},
		{"duplicate id in slot", `[` + hero(b1) + `,` + cards(b2, card(b1)) + `]`, ModeDraft, []string{"body[1].slots.items[0].id:duplicate_id"}},
		{"missing type", `[{"id":"` + b1 + `"}]`, ModeDraft, []string{"body[0].type:required"}},
		{"type not a string", `[{"id":"` + b1 + `","type":4}]`, ModeDraft, []string{"body[0].type:type"}},
		{"unknown type", `[{"id":"` + b1 + `","type":"ghost"}]`, ModeDraft, []string{"body[0].type:block_type"}},
		{"type not allowed", `[` + card(b1) + `]`, ModeDraft, []string{"body[0].type:block_type"}},
		{"type not allowed in slot", `[` + cards(b1, hero(b2)) + `]`, ModeDraft, []string{"body[0].slots.items[0].type:block_type"}},
		{"unknown block key", `[{"id":"` + b1 + `","type":"hero","extra":1}]`, ModeDraft, []string{"body[0].extra:unknown_field"}},
		{"inlined blocks refused", `[{"id":"` + b1 + `","type":"hero","blocks":[]}]`, ModeDraft, []string{"body[0].blocks:unknown_field"}},
		{"unknown prop", `[{"id":"` + b1 + `","type":"hero","props":{"bogus":1}}]`, ModeDraft, []string{"body[0].props.bogus:unknown_field"}},
		{"prop type", `[{"id":"` + b1 + `","type":"hero","props":{"heading":4}}]`, ModeDraft, []string{"body[0].props.heading:type"}},
		{"prop max", `[` + hero(b1) + `,{"id":"` + b2 + `","type":"hero","props":{"heading":"` + strings.Repeat("x", 21) + `"}}]`, ModeDraft, []string{"body[1].props.heading:max_length"}},
		{"required prop publish", `[{"id":"` + b1 + `","type":"hero","props":{}}]`, ModePublish, []string{"body[0].props.heading:required"}},
		{"required prop without props", `[{"id":"` + b1 + `","type":"hero"}]`, ModePublish, []string{"body[0].props.heading:required"}},
		{"required prop draft", `[{"id":"` + b1 + `","type":"hero","props":{}}]`, ModeDraft, nil},
		{"props not an object", `[{"id":"` + b1 + `","type":"hero","props":[]}]`, ModeDraft, []string{"body[0].props:type"}},
		{"token prop", `[{"id":"` + b1 + `","type":"hero","props":{"heading":"x","tone":"pink"}}]`, ModeDraft, []string{"body[0].props.tone:enum"}},
		{"ref prop", `[{"id":"` + b1 + `","type":"hero","props":{"heading":"x","cta":"nope"}}]`, ModeDraft, []string{"body[0].props.cta:format"}},
		{"hidden by when is fine", `[{"id":"` + b1 + `","type":"hero","props":{"heading":"x","when1":"y"}}]`, ModeDraft, nil},
		{"slot path", `[` + hero(b1) + `,` + cards(b2, `{"id":"`+b3+`","type":"card","props":{"title":5}}`) + `]`, ModeDraft, []string{"body[1].slots.items[0].props.title:type"}},
		{"slot required prop", `[` + hero(b1) + `,` + cards(b2, `{"id":"`+b3+`","type":"card"}`) + `]`, ModePublish, []string{"body[1].slots.items[0].props.title:required"}},
		{"unknown slot", `[{"id":"` + b1 + `","type":"hero","slots":{"x":[]}}]`, ModeDraft, []string{"body[0].slots.x:unknown_field"}},
		{"slot not a list", `[{"id":"` + b1 + `","type":"cards","slots":{"items":{}}}]`, ModeDraft, []string{"body[0].slots.items:type"}},
		{"slots not an object", `[{"id":"` + b1 + `","type":"cards","slots":[]}]`, ModeDraft, []string{"body[0].slots:type"}},
		{"slot null", `[{"id":"` + b1 + `","type":"cards","slots":{"items":null}}]`, ModeDraft, []string{"body[0].slots.items:type"}},
		{"slot max", `[` + cards(b1, card(b2)+`,`+card(b3)+`,`+card(b4)) + `]`, ModeDraft, []string{"body[0].slots.items:max_items"}},
		{"slot min publish", `[` + cards(b1, ``) + `]`, ModePublish, []string{"body[0].slots.items:min_items"}},
		{"slot min publish absent", `[{"id":"` + b1 + `","type":"cards"}]`, ModePublish, []string{"body[0].slots.items:min_items"}},
		{"slot min draft", `[` + cards(b1, ``) + `]`, ModeDraft, nil},
		{"depth", `[` + cards(b1, deep) + `]`, ModeDraft, []string{deepPath}},
		{"shared ok", `[{"id":"` + b1 + `","type":"shared","props":{"item":"` + b2 + `"}}]`, ModePublish, nil},
		{"shared needs item", `[{"id":"` + b1 + `","type":"shared"}]`, ModePublish, []string{"body[0].props.item:required"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := errPaths(s.Validate(page, doc(t, `{"title":"t","body":`+tc.body+`}`), tc.mode))
			if !reflect.DeepEqual(got, tc.want) && (len(got) > 0 || len(tc.want) > 0) {
				t.Fatalf("errors = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBlocksDocumentLimit(t *testing.T) {
	s := mustCompile(t, strings.Replace(blocksYAML, "max: 3, translatable", "translatable", 1))
	page, _ := s.Type("page")
	var sb strings.Builder
	sb.WriteString(`{"body":[`)
	id := 0
	next := func() string {
		id++
		return strings.Replace("0190a000-0000-7000-8000-000000000000", "000000000000", pad(id), 1)
	}
	for i := range maxDocBlocks + 1 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`{"id":"` + next() + `","type":"hero"}`)
	}
	sb.WriteString(`]}`)
	got := errPaths(s.Validate(page, doc(t, sb.String()), ModeDraft))
	if len(got) != 1 || !strings.HasSuffix(got[0], ":max_blocks") {
		t.Fatalf("errors = %v", got)
	}
}

func pad(n int) string {
	s := "000000000000" + strings.Repeat("", 0)
	d := []byte(s)
	for i := len(d) - 1; n > 0; i-- {
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d)
}

func TestBlocksNormalizeDefaultsPrune(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	page, _ := s.Type("page")
	upper := strings.ToUpper(b1)
	d := doc(t, `{"body":[
		{"id":"`+upper+`","type":"hero","props":{"heading":"x","cta":"`+strings.ToUpper(b2)+`","when1":"gone"}},
		{"id":"`+b3+`","type":"cards","slots":{"items":[{"id":"`+b4+`","type":"card","props":{"title":"t","link":"`+strings.ToUpper(b2)+`"}}]}}]}`)
	s.ApplyDefaults(page, d)
	s.Normalize(page, d)
	s.Prune(page, d)
	body := d["body"].([]any)
	h := body[0].(map[string]any)
	props := h["props"].(map[string]any)
	if h["id"] != b1 || props["cta"] != b2 || props["tone"] != "light" {
		t.Fatalf("hero = %v", h)
	}
	if _, ok := props["when1"]; ok {
		t.Fatalf("hidden prop kept: %v", props)
	}
	inner := body[1].(map[string]any)["slots"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if inner["props"].(map[string]any)["link"] != b2 {
		t.Fatalf("nested ref not lower-cased: %v", inner)
	}
	if errs := s.Validate(page, d, ModePublish); len(errs) > 0 {
		t.Fatal(errs)
	}
}

func TestAssignBlockIDs(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	page, _ := s.Type("page")
	d := doc(t, `{"body":[{"type":"hero"},{"id":"`+b1+`","type":"cards","slots":{"items":[{"type":"card"},{"id":"","type":"card"}]}}]}`)
	n := 0
	s.AssignBlockIDs(page, d, func() string {
		n++
		return "0190a000-0000-7000-8000-00000000010" + string(rune('0'+n))
	})
	body := d["body"].([]any)
	items := body[1].(map[string]any)["slots"].(map[string]any)["items"].([]any)
	got := []any{body[0].(map[string]any)["id"], body[1].(map[string]any)["id"], items[0].(map[string]any)["id"], items[1].(map[string]any)["id"]}
	want := []any{"0190a000-0000-7000-8000-000000000101", b1, "0190a000-0000-7000-8000-000000000102", "0190a000-0000-7000-8000-000000000103"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ids = %v", got)
	}
}

func TestBlocksProjections(t *testing.T) {
	s := mustCompile(t, blocksYAML)
	page, _ := s.Type("page")
	d := doc(t, `{"title":"T","body":[
		{"id":"`+b1+`","type":"hero","props":{"heading":"Welcome home","cta":"`+b2+`","note":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see","marks":[{"type":"link","attrs":{"href":"item:`+b3+`"}}]}]}]}}},
		{"id":"`+b4+`","type":"cards","props":{"title":"Grid"},"slots":{"items":[{"id":"0190a000-0000-7000-8000-000000000005","type":"card","props":{"title":"Card one","link":"`+b3+`"}}]}}]}`)
	refs := s.Refs(page, d)
	want := []RefValue{
		{Path: "body[0].props.cta", To: b2, Kind: "field", OnDelete: "block", Types: []string{"page"}},
		{Path: "body[0].props.note", To: b3, Kind: "link", OnDelete: "warn"},
		{Path: "body[1].slots.items[0].props.link", To: b3, Kind: "field", OnDelete: "nullify", Types: []string{"page"}},
	}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("refs = %+v", refs)
	}
	if got := s.SearchText(page, d); got != "T Welcome home see Grid Card one" {
		t.Fatalf("search text = %q", got)
	}
	if iv := s.IndexValues(page, d); len(iv) != 0 {
		t.Fatalf("blocks were indexed: %v", iv)
	}
	if !s.RemoveRef(page, d, b3) {
		t.Fatal("RemoveRef changed nothing")
	}
	card := d["body"].([]any)[1].(map[string]any)["slots"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if _, ok := card["props"].(map[string]any)["link"]; ok {
		t.Fatalf("nullify ref kept: %v", card)
	}
}
