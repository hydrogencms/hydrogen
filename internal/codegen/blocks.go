package codegen

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/schema"
)

func sortedBlocks(blocks []*schema.BlockType) []*schema.BlockType {
	out := slices.Clone(blocks)
	slices.SortStableFunc(out, func(a, b *schema.BlockType) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func sortedTokens(tokens []*schema.TokenSet) []*schema.TokenSet {
	out := slices.Clone(tokens)
	slices.SortStableFunc(out, func(a, b *schema.TokenSet) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func checkBlocks(blocks []*schema.BlockType, tokens []*schema.TokenSet) error {
	seen := map[string]bool{}
	for _, b := range blocks {
		if err := checkName("block", b.Name); err != nil {
			return err
		}
		if seen[b.Name] {
			return fmt.Errorf("codegen: block %q is defined twice", b.Name)
		}
		seen[b.Name] = true
	}
	seen = map[string]bool{}
	for _, tk := range tokens {
		if err := checkName("token set", tk.Name); err != nil {
			return err
		}
		if seen[tk.Name] {
			return fmt.Errorf("codegen: token set %q is defined twice", tk.Name)
		}
		seen[tk.Name] = true
	}
	return nil
}

func hasRequired(fields []*schema.Field) bool {
	return slices.ContainsFunc(fields, func(f *schema.Field) bool { return f.Required && f.When == nil })
}

func slotRequired(sl schema.Slot) bool { return sl.Min > 0 }

// blockInterface names the TypeScript interface of a block type: HeroBlock. The built-in shared block is
// SharedBlockRef, because a blockset type is usually called sharedBlock.
func blockInterface(name, goCased string) string {
	if name == "shared" {
		return "SharedBlockRef"
	}
	return goCased + "Block"
}

// tokenTypeName is the generated name of a token set: "tone" becomes ToneToken.
func tokenTypeName(set string) (string, error) {
	n, err := goName(set)
	return n + "Token", err
}

// ---- TypeScript

// tsBlocks declares the token unions, the props and block interfaces and the Block/Blocks helpers.
func (g *tsGen) blocks(b *strings.Builder, blocks []*schema.BlockType, tokens []*schema.TokenSet) error {
	for _, tk := range sortedTokens(tokens) {
		name, err := tokenTypeName(tk.Name)
		if err != nil {
			return err
		}
		if err := g.names.declare(name, fmt.Sprintf("token set %q", tk.Name)); err != nil {
			return err
		}
		vals := make([]string, len(tk.Values))
		for i, v := range tk.Values {
			vals[i] = tsString(v)
		}
		if len(vals) == 0 {
			vals = []string{"never"}
		}
		b.WriteString("\n" + jsDoc("", "The \""+tk.Name+"\" design tokens.") + "export type " + name + " = " + strings.Join(vals, " | ") + ";\n")
	}
	if len(blocks) == 0 {
		return nil
	}
	sorted := sortedBlocks(blocks)
	type decl struct{ iface, block string }
	var decls []decl
	for _, bt := range sorted {
		k, err := goName(bt.Name)
		if err != nil {
			return err
		}
		what := fmt.Sprintf("block %q", bt.Name)
		if err := g.names.declare(k+"Props", "the props of "+what); err != nil {
			return err
		}
		iface := blockInterface(bt.Name, k)
		if err := g.names.declare(iface, what); err != nil {
			return err
		}
		doc := "The props of the \"" + bt.Name + "\" block."
		props, err := g.interfaceDecl(k+"Props", doc, bt.Props, what)
		if err != nil {
			return err
		}
		b.WriteString("\n" + props)

		bdoc := "The \"" + bt.Name + "\" block."
		if bt.Description != "" {
			bdoc += "\n\n" + bt.Description
		}
		b.WriteString("\n" + jsDoc("", bdoc) + "export interface " + iface + " {\n  id: string;\n  type: " + tsString(bt.Name) + ";\n")
		q := "?"
		if hasRequired(bt.Props) {
			q = ""
		}
		fmt.Fprintf(b, "  props%s: %sProps;\n", q, k)
		if len(bt.Slots) > 0 {
			sq := "?"
			if slices.ContainsFunc(bt.Slots, slotRequired) {
				sq = ""
			}
			fmt.Fprintf(b, "  slots%s: {\n", sq)
			for _, sl := range bt.Slots {
				if err := checkName("slot", sl.Name); err != nil {
					return fmt.Errorf("%w (in %s)", err, what)
				}
				q := "?"
				if slotRequired(sl) {
					q = ""
				}
				fmt.Fprintf(b, "    %s%s: %s;\n", tsKey(sl.Name), q, tsBlocksType(sl.Allow))
			}
			b.WriteString("  };\n")
		}
		if bt.Name == "shared" {
			b.WriteString("  /** The shared item's blocks, inlined by the delivery API. */\n  blocks?: Block[];\n")
		}
		b.WriteString("}\n")
		decls = append(decls, decl{iface, bt.Name})
	}
	b.WriteString("\n/** Block types by name. */\nexport interface HydrogenBlocks {\n")
	for _, d := range decls {
		fmt.Fprintf(b, "  %s: %s;\n", tsKey(d.block), d.iface)
	}
	b.WriteString("}\n\n/** The name of a block type. */\nexport type BlockName = keyof HydrogenBlocks;\n\n")
	b.WriteString("/** A block, discriminated by `type`; narrow it with a union of names. */\nexport type Block<N extends BlockName = BlockName> = HydrogenBlocks[N];\n\n")
	b.WriteString("/** The blocks of a blocks field or slot. */\nexport type Blocks<N extends BlockName = BlockName> = Block<N>[];\n")
	return nil
}

func tsBlocksType(allow []string) string {
	names := make([]string, len(allow))
	for i, a := range allow {
		names[i] = tsString(a)
	}
	return "Blocks<" + strings.Join(names, " | ") + ">"
}

// ---- Go

func (g *goGen) blocks(blocks []*schema.BlockType, tokens []*schema.TokenSet) (string, error) {
	var b strings.Builder
	if err := g.names.declare("Block", "the generated block type"); err != nil {
		return "", err
	}
	if err := g.names.declare("DecodeProps", "a generated helper"); err != nil {
		return "", err
	}
	if len(blocks) > 0 {
		b.WriteString(`// Block is one block of a blocks field. Props holds the typed props of its type: decode them with DecodeProps.
// Blocks is set by the delivery API on shared blocks, with the shared item's blocks.
type Block struct {
	ID     string             ` + "`json:\"id\"`" + `
	Type   string             ` + "`json:\"type\"`" + `
	Props  json.RawMessage    ` + "`json:\"props,omitempty\"`" + `
	Slots  map[string][]Block ` + "`json:\"slots,omitempty\"`" + `
	Blocks []Block            ` + "`json:\"blocks,omitempty\"`" + `
}

// DecodeProps decodes the props of a block into a props struct such as HeroProps.
func DecodeProps[T any](b Block) (T, error) {
	var v T
	if len(b.Props) == 0 {
		return v, nil
	}
	err := json.Unmarshal(b.Props, &v)
	return v, err
}

`)
	}
	for _, tk := range sortedTokens(tokens) {
		name, err := tokenTypeName(tk.Name)
		if err != nil {
			return "", err
		}
		what := fmt.Sprintf("token set %q", tk.Name)
		if err := g.names.declare(name, what); err != nil {
			return "", err
		}
		b.WriteString(lineComment("", name+" is a value of the \""+tk.Name+"\" design tokens."))
		b.WriteString("type " + name + " string\n\nconst (\n")
		consts, err := enumConstNames(tk.Values)
		if err != nil {
			return "", err
		}
		for i, v := range tk.Values {
			if err := g.names.declare(name+consts[i], fmt.Sprintf("value %q of %s", v, what)); err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "\t%s %s = %s\n", name+consts[i], name, strconv.Quote(v))
		}
		b.WriteString(")\n\n")
	}
	sorted := sortedBlocks(blocks)
	if len(sorted) > 0 {
		b.WriteString("// Block types.\nconst (\n")
		for _, bt := range sorted {
			k, err := goName(bt.Name)
			if err != nil {
				return "", err
			}
			if err := g.names.declare("Block"+k, fmt.Sprintf("the constant of block %q", bt.Name)); err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "\tBlock%s = %s\n", k, strconv.Quote(bt.Name))
		}
		b.WriteString(")\n\n")
	}
	for _, bt := range sorted {
		k, err := goName(bt.Name)
		if err != nil {
			return "", err
		}
		what := fmt.Sprintf("block %q", bt.Name)
		name := k + "Props"
		if err := g.names.declare(name, "the props of "+what); err != nil {
			return "", err
		}
		comment := name + " are the props of the \"" + bt.Name + "\" block."
		if bt.Description != "" {
			comment += "\n\n" + bt.Description
		}
		body, err := g.structDecl(name, comment, bt.Props, what)
		if err != nil {
			return "", err
		}
		b.WriteString(body)
	}
	return b.String(), nil
}
