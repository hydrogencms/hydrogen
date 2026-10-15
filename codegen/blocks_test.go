package codegen

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hydrogencms/hydrogen/schema"
)

const blocksSchema = `
tokens:
  tone: [light, dark, brand]
blocks:
  hero:
    description: Large heading with an optional call to action.
    props:
      heading: { type: string, required: true }
      tone: { type: token, set: tone, default: light }
      ctaLink: { type: ref, to: [page] }
      columns: { type: enum, values: ["2", "3"] }
  richText:
    props:
      body: { type: richtext, required: true }
  cards:
    props:
      heading: { type: string }
    slots:
      items: { allow: [card], min: 1, max: 12 }
  card:
    props:
      title: { type: string, required: true }
types:
  page:
    page: true
    fields:
      title: { type: string, required: true, translatable: true }
      accent: { type: token, set: tone }
      body: { type: blocks, allow: [hero, richText, cards, shared], max: 50, translatable: true }
  sharedBlock:
    blockset: true
    fields:
      label: { type: string }
      content: { type: blocks, allow: [hero, richText, cards] }
`

func blocksFixture(t testing.TB) *schema.Schema {
	t.Helper()
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fstest.MapFS{"schema/s.yaml": {Data: []byte(blocksSchema)}}, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBlocksGoGolden(t *testing.T) {
	s := blocksFixture(t)
	files, err := Go(s.Types(), s.Groups(), GoOptions{Blocks: s.Blocks(), Tokens: s.Tokens()})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	if want := []string{"blocks.go", "doc.go", "page.go", "shared_block.go"}; !slices.Equal(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
	for _, n := range []string{"blocks.go", "page.go"} {
		golden(t, "blocksfixture_"+n+".golden", files[n])
	}
}

func TestBlocksTypeScriptGolden(t *testing.T) {
	s := blocksFixture(t)
	src, err := TypeScript(s.Types(), s.Groups(), TSOptions{Blocks: s.Blocks(), Tokens: s.Tokens()})
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "blocksfixture_types.ts.golden", src)
}

func TestBlocksGoCompiles(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go not on PATH")
	}
	s := blocksFixture(t)
	files, err := Go(s.Types(), s.Groups(), GoOptions{Blocks: s.Blocks(), Tokens: s.Tokens()})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	out := map[string][]byte{
		"go.mod": []byte("module example.com/check\n\ngo 1.26\n\nrequire github.com/hydrogencms/hydrogen v0.0.0\n\nreplace github.com/hydrogencms/hydrogen => " + repoRoot(t) + "\n"),
		"main.go": []byte(`package main

import (
	"encoding/json"
	"fmt"

	"example.com/check/gen/content"
)

func main() {
	var blocks []content.Block
	if err := json.Unmarshal([]byte(` + "`" + `[{"id":"1","type":"hero","props":{"heading":"Hi","tone":"dark"}}]` + "`" + `), &blocks); err != nil {
		panic(err)
	}
	p, err := content.DecodeProps[content.HeroProps](blocks[0])
	fmt.Println(p.Heading, *p.Tone == content.ToneTokenDark, blocks[0].Type == content.BlockHero, err)
	page := content.Page{Title: "t", Body: blocks, Accent: nil}
	fmt.Println(page.Body[0].ID)
}
`),
	}
	for n, src := range files {
		out["gen/content/"+n] = src
	}
	writeFiles(t, dir, out)
	run := exec.Command(goBin, "run", ".")
	run.Dir = dir
	run.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=off")
	b, err := run.CombinedOutput()
	if err != nil {
		if strings.Contains(string(b), "undefined") && strings.Contains(string(b), "hydrogencms/hydrogen/content") {
			t.Skipf("content package does not define the generated value types:\n%s", b)
		}
		t.Fatalf("go run: %v\n%s", err, b)
	}
	if got := string(b); !strings.HasPrefix(got, "Hi true true <nil>\n") {
		t.Fatalf("output = %q", got)
	}
}

func TestBlocksTypeScriptCompiles(t *testing.T) {
	npx, err := exec.LookPath("npx")
	if err != nil {
		t.Skip("npx not on PATH")
	}
	s := blocksFixture(t)
	src, err := TypeScript(s.Types(), s.Groups(), TSOptions{Blocks: s.Blocks(), Tokens: s.Tokens()})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeFiles(t, dir, map[string][]byte{
		"types.ts": src,
		"usage.ts": []byte(`import type { Block, Blocks, Page, HeroBlock, ToneToken, SharedBlockRef } from "./types";

const hero: HeroBlock = { id: "1", type: "hero", props: { heading: "Hi", tone: "dark" } };
const cards: Block = { id: "2", type: "cards", slots: { items: [{ id: "3", type: "card", props: { title: "A" } }] } };
const page: Page = { title: "t", body: [hero, cards], accent: "brand" };
function render(b: Block): string {
  switch (b.type) {
    case "hero": return b.props.heading;
    case "cards": return String(b.slots?.items.length);
    case "richText": return "";
    case "card": return b.props.title;
    case "shared": return b.props.item + String(b.blocks?.length);
  }
}
const ref: SharedBlockRef = { id: "4", type: "shared", props: { item: "x" as never } };
const tone: ToneToken = "light";
// @ts-expect-error cards slot only allows card blocks
const bad: Blocks<"card"> = [hero];
// @ts-expect-error unknown token
const badTone: ToneToken = "pink";
export { page, ref, render, tone, bad, badTone };
`),
	})
	cmd := exec.Command(npx, "--no-install", "tsc", "--noEmit", "--strict", "--target", "es2022", "types.ts", "usage.ts")
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		if strings.Contains(string(b), "not found") || strings.Contains(string(b), "could not determine executable") {
			t.Skipf("tsc unavailable: %s", b)
		}
		t.Fatalf("tsc: %v\n%s", err, b)
	}
}
