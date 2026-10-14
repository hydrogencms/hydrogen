package codegen

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hydrogencms/hydrogen/schema"
)

// TSOptions configures TypeScript. It has no options yet.
type TSOptions struct{}

const tsPrelude = `/** An item id (UUID string). */
export type Ref = string & { readonly __ref?: never };

/** A calendar date, "2026-10-14". */
export type ISODate = string & { readonly __isoDate?: never };

/** An RFC 3339 timestamp in UTC, "2026-10-14T09:30:00Z". */
export type ISODateTime = string & { readonly __isoDateTime?: never };

/** A node of a ProseMirror document. */
export interface RichTextNode {
  type: string;
  attrs?: Record<string, unknown>;
  content?: RichTextNode[];
  text?: string;
  marks?: { type: string; attrs?: Record<string, unknown> }[];
}

/** A rich text value: a ProseMirror document node. */
export type RichText = RichTextNode;
`

const tsEnvelope = `/** An item as the management and delivery APIs return it. */
export interface ItemView<T> {
  id: string;
  type: string;
  key?: string;
  locale: string;
  mainLocale: string;
  alwaysAvailable: boolean;
  status: "draft" | "published" | "changed";
  version: number;
  draft: boolean;
  name: string;
  fields: T;
  locales: string[];
  draftParts: string[];
  createdAt: string;
  updatedAt: string;
  publishedAt?: string;
}
`

// TypeScript returns one .ts file with an interface per content type and named group, the HydrogenTypes
// map and the item envelope types. Output is deterministic and independent of the order of the inputs.
func TypeScript(types []*schema.Type, groups []*schema.Group, _ TSOptions) ([]byte, error) {
	if err := checkTypes(types); err != nil {
		return nil, err
	}
	gm, err := groupSet(groups)
	if err != nil {
		return nil, err
	}
	g := &tsGen{
		names:  newNames("Ref", "ISODate", "ISODateTime", "RichTextNode", "RichText", "ItemView", "HydrogenTypes", "HydrogenTypeName"),
		groups: gm,
	}
	var b strings.Builder
	b.WriteString("// " + header + "\n\n" + tsPrelude + "\n" + tsEnvelope)

	for _, grp := range sortedGroups(groups) {
		name, err := goName(grp.Name)
		if err != nil {
			return nil, err
		}
		name += "Group"
		what := fmt.Sprintf("group %q", grp.Name)
		if err := g.names.declare(name, what); err != nil {
			return nil, err
		}
		doc := "The reusable \"" + grp.Name + "\" field group."
		if grp.Description != "" {
			doc += "\n\n" + grp.Description
		}
		body, err := g.interfaceDecl(name, doc, grp.Fields, what)
		if err != nil {
			return nil, err
		}
		b.WriteString("\n" + body)
	}

	sorted := sortedTypes(types)
	tnames := make([]string, len(sorted))
	for i, t := range sorted {
		name, err := goName(t.Name)
		if err != nil {
			return nil, err
		}
		what := fmt.Sprintf("type %q", t.Name)
		if err := g.names.declare(name, what); err != nil {
			return nil, err
		}
		doc := "The \"" + t.Name + "\" content type."
		if t.Description != "" {
			doc += "\n\n" + t.Description
		}
		body, err := g.interfaceDecl(name, doc, t.Fields, what)
		if err != nil {
			return nil, err
		}
		b.WriteString("\n" + body)
		tnames[i] = name
	}

	b.WriteString("\n/** Field types by content type name. */\nexport interface HydrogenTypes {\n")
	for i, t := range sorted {
		fmt.Fprintf(&b, "  %s: %s;\n", tsKey(t.Name), tnames[i])
	}
	b.WriteString("}\n\n/** The name of a content type. */\nexport type HydrogenTypeName = keyof HydrogenTypes;\n")
	return []byte(b.String()), nil
}

type tsGen struct {
	names  *names
	groups map[string]*schema.Group
}

func (g *tsGen) interfaceDecl(name, doc string, fields []*schema.Field, owner string) (string, error) {
	var b, nested strings.Builder
	b.WriteString(jsDoc("", doc))
	b.WriteString("export interface " + name + " {\n")
	seen := map[string]string{}
	for _, f := range fields {
		if err := checkName("field", f.Name); err != nil {
			return "", fmt.Errorf("%w (in %s)", err, owner)
		}
		if _, dup := seen[f.Name]; dup {
			return "", fmt.Errorf("codegen: field %q is defined twice in %s", f.Name, owner)
		}
		seen[f.Name] = ""
		typ, err := g.fieldType(name, f.Name, f, &nested)
		if err != nil {
			return "", fmt.Errorf("%w (field %q of %s)", err, f.Name, owner)
		}
		if f.Description != "" {
			b.WriteString(jsDoc("  ", f.Description))
		}
		q := ""
		if optional(f) {
			q = "?"
		}
		fmt.Fprintf(&b, "  %s%s: %s;\n", tsKey(f.Name), q, typ)
	}
	b.WriteString("}\n")
	if nested.Len() > 0 {
		b.WriteString("\n" + nested.String())
	}
	return b.String(), nil
}

func (g *tsGen) fieldType(prefix, key string, f *schema.Field, nested *strings.Builder) (string, error) {
	switch f.Shape.Base {
	case schema.BaseString:
		switch f.Shape.Format {
		case "", "text", "slug", "email", "url":
			return "string", nil
		case "date":
			return "ISODate", nil
		case "datetime":
			return "ISODateTime", nil
		case "enum":
			if len(f.Shape.Enum) == 0 {
				return "never", nil
			}
			vals := make([]string, len(f.Shape.Enum))
			for i, v := range f.Shape.Enum {
				vals[i] = tsString(v)
			}
			return strings.Join(vals, " | "), nil
		}
		return "", fmt.Errorf("codegen: unknown string format %q", f.Shape.Format)
	case schema.BaseNumber:
		return "number", nil
	case schema.BaseBoolean:
		return "boolean", nil
	case schema.BaseRef:
		return "Ref", nil
	case schema.BaseRichText:
		return "RichText", nil
	case schema.BaseJSON:
		return "unknown", nil
	case schema.BaseList:
		if f.Of == nil {
			return "", fmt.Errorf("codegen: list without an element type")
		}
		elem, err := g.fieldType(prefix, key, f.Of, nested)
		if err != nil {
			return "", err
		}
		if strings.Contains(elem, " ") {
			elem = "(" + elem + ")"
		}
		return elem + "[]", nil
	case schema.BaseGroup:
		if f.Group != "" {
			grp := g.groups[f.Group]
			if grp == nil {
				return "", fmt.Errorf("codegen: unknown group %q", f.Group)
			}
			n, err := goName(grp.Name)
			return n + "Group", err
		}
		k, err := goName(key)
		if err != nil {
			return "", err
		}
		name := prefix + k
		what := fmt.Sprintf("the inline group %q", key)
		if err := g.names.declare(name, what); err != nil {
			return "", err
		}
		body, err := g.interfaceDecl(name, "The inline \""+key+"\" group.", f.Fields, what)
		if err != nil {
			return "", err
		}
		nested.WriteString(body)
		return name, nil
	}
	return "", fmt.Errorf("codegen: unsupported field base %q", f.Shape.Base)
}

// tsString renders s as a TypeScript string literal. JSON strings are valid TS literals; strconv.Quote is
// not (it emits \U escapes).
func tsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
