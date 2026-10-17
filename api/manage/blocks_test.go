package manage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/schema"
)

const blocksTestSchema = `
tokens:
  tone: [light, dark]
blocks:
  hero:
    label: Hero
    icon: layout
    props:
      heading: { type: string, required: true }
      tone: { type: token, set: tone }
  cards:
    slots:
      items: { allow: [card], min: 1, max: 4 }
  card:
    props:
      title: { type: string }
types:
  page:
    fields:
      body: { type: blocks, allow: [hero, cards, shared], max: 5 }
  shell:
    blockset: true
    fields:
      content: { type: blocks, allow: [hero] }
`

func blocksFake(t testing.TB) *fake {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(blocksTestSchema)}}, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	f.sc = s
	return f
}

func TestBlocksEndpoint(t *testing.T) {
	h := newAPI(t, blocksFake(t))
	w := do(h, "GET", "/blocks", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var m struct {
		Blocks []map[string]any `json:"blocks"`
		Tokens []map[string]any `json:"tokens"`
	}
	mustJSON(t, w, &m)
	var names []string
	for _, b := range m.Blocks {
		names = append(names, b["name"].(string))
	}
	if strings.Join(names, ",") != "card,cards,hero,shared" {
		t.Fatalf("blocks = %v", names)
	}
	hero := m.Blocks[2]
	if hero["label"] != "Hero" || hero["icon"] != "layout" {
		t.Errorf("hero = %v", hero)
	}
	props := hero["props"].([]any)
	if len(props) != 2 || props[1].(map[string]any)["shape"].(map[string]any)["set"] != "tone" {
		t.Errorf("props = %v", props)
	}
	slot := m.Blocks[1]["slots"].([]any)[0].(map[string]any)
	if slot["name"] != "items" || slot["min"] != float64(1) || slot["max"] != float64(4) || fmt.Sprint(slot["allow"]) != "[card]" {
		t.Errorf("slot = %v", slot)
	}
	if len(m.Tokens) != 1 || m.Tokens[0]["name"] != "tone" || fmt.Sprint(m.Tokens[0]["values"]) != "[light dark]" {
		t.Errorf("tokens = %v", m.Tokens)
	}

	// The type summaries carry the blocks field's allow.
	var ts []map[string]any
	mustJSON(t, do(h, "GET", "/types", ""), &ts)
	field := ts[0]["fields"].([]any)[0].(map[string]any)
	if field["shape"].(map[string]any)["base"] != "blocks" || fmt.Sprint(field["options"].(map[string]any)["allow"]) != "[hero cards shared]" {
		t.Errorf("blocks field = %v", field)
	}
}

func TestBlocksEndpointWithoutBlocks(t *testing.T) {
	w := do(newAPI(t, newFake(t)), "GET", "/blocks", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"blocks":[],"tokens":[]}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestBlocksInOpenAPI(t *testing.T) {
	doc, raw := openapiDoc(t, blocksFake(t))
	if strings.Contains(string(raw), `"#/$defs/`) || strings.Contains(string(raw), `"$defs"`) {
		t.Error("OpenAPI still refers to $defs")
	}
	schemas := resolve(doc, "#/components/schemas").(map[string]any)
	for _, name := range []string{"Block", "Block_hero", "Block_cards", "Block_card", "Block_shared", "BlockCatalog", "Fields_page"} {
		if schemas[name] == nil {
			t.Errorf("schema %s missing", name)
		}
	}
	if doc["paths"].(map[string]any)["/blocks"] == nil {
		t.Error("GET /blocks is not documented")
	}
	body, _ := json.Marshal(schemas["Fields_page"])
	if !strings.Contains(string(body), `"#/components/schemas/Block_hero"`) {
		t.Errorf("Fields_page does not refer to Block_hero: %s", body)
	}
	hero, _ := json.Marshal(schemas["Block_hero"])
	if !strings.Contains(string(hero), `"const":"hero"`) {
		t.Errorf("Block_hero lacks the type discriminator: %s", hero)
	}
	var refs int
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if r, ok := x["$ref"].(string); ok {
				refs++
				if resolve(doc, r) == nil {
					t.Errorf("unresolved %s", r)
				}
			}
			for _, c := range x {
				walk(c)
			}
		case []any:
			for _, c := range x {
				walk(c)
			}
		}
	}
	walk(doc)
	if refs == 0 {
		t.Error("no refs checked")
	}
}
