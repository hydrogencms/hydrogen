package codegen

import (
	"bytes"
	"cmp"
	"fmt"
	"go/format"
	"slices"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// GoOptions configures Go.
type GoOptions struct {
	// Package is the generated package name; default "content".
	Package string
	// Blocks and Tokens are the schema's block types and design token sets (Schema.Blocks, Schema.Tokens).
	Blocks []*schema.BlockType
	Tokens []*schema.TokenSet
}

const (
	goImportContent = `"github.com/hydrogencms/hydrogen/core/content"`
	goImportJSON    = `"encoding/json"`
)

// Go returns gofmt'ed Go source files (file name to content) for the generated package: one file per type
// (article.go), groups.go with the named groups, and doc.go with the package comment and the Types list.
// Output is deterministic and independent of the order of the inputs.
func Go(types []*schema.Type, groups []*schema.Group, o GoOptions) (map[string][]byte, error) {
	pkg := o.Package
	if pkg == "" {
		pkg = "content"
	}
	if err := checkTypes(types); err != nil {
		return nil, err
	}
	gm, err := groupSet(groups)
	if err != nil {
		return nil, err
	}
	if err := checkBlocks(o.Blocks, o.Tokens); err != nil {
		return nil, err
	}
	g := &goGen{names: newNames("Types"), groups: gm}
	files := map[string]string{}
	if len(o.Blocks) > 0 || len(o.Tokens) > 0 {
		body, err := g.blocks(o.Blocks, o.Tokens)
		if err != nil {
			return nil, err
		}
		files["blocks.go"] = body
	}

	var groupBody strings.Builder
	for _, grp := range sortedGroups(groups) {
		name, err := goName(grp.Name)
		if err != nil {
			return nil, err
		}
		name += "Group"
		if err := g.names.declare(name, fmt.Sprintf("group %q", grp.Name)); err != nil {
			return nil, err
		}
		comment := name + " is the reusable \"" + grp.Name + "\" field group."
		if grp.Description != "" {
			comment += "\n\n" + grp.Description
		}
		body, err := g.structDecl(name, comment, grp.Fields, fmt.Sprintf("group %q", grp.Name))
		if err != nil {
			return nil, err
		}
		groupBody.WriteString(body)
	}
	if groupBody.Len() > 0 {
		files["groups.go"] = groupBody.String()
	}

	typeNames := make([]string, 0, len(types))
	for _, t := range sortedTypes(types) {
		name, err := goName(t.Name)
		if err != nil {
			return nil, err
		}
		what := fmt.Sprintf("type %q", t.Name)
		if err := g.names.declare(name, what); err != nil {
			return nil, err
		}
		comment := name + " is the \"" + t.Name + "\" content type."
		if t.Description != "" {
			comment += "\n\n" + t.Description
		}
		body, err := g.structDecl(name, comment, t.Fields, what)
		if err != nil {
			return nil, err
		}
		handle := name + "Type"
		if err := g.names.declare(handle, "the handle of "+what); err != nil {
			return nil, err
		}
		body += fmt.Sprintf(`
// %[1]s is the typed handle for the "%[3]s" content type. Register hooks and access rules and use the Local API
// through it:
//
//	%[1]s.BeforeSave(h, func(c *hooks.Context, doc *%[2]s) error { return nil })
//	%[1]s.Access(h, access.Rules{Read: func(ctx context.Context, p *access.Principal) access.Result { return access.Allow() }})
var %[1]s = content.Handle[%[2]s]{Type: %[4]s}
`, handle, name, t.Name, strconv.Quote(t.Name))
		file := fileBase(t.Name)
		if file == "doc" || file == "groups" || file == "blocks" || strings.HasSuffix(file, "_test") {
			file += "_type"
		}
		file += ".go"
		if _, dup := files[file]; dup {
			return nil, fmt.Errorf("codegen: type %q generates file %s twice", t.Name, file)
		}
		files[file] = body
		typeNames = append(typeNames, t.Name)
	}

	var doc strings.Builder
	doc.WriteString("// Package " + pkg + " holds the typed content model generated from the project's schema.\n")
	doc.WriteString("package " + pkg + "\n\n// Types lists the content type names in this package, sorted.\nvar Types = []string{")
	for i, n := range typeNames {
		if i > 0 {
			doc.WriteString(", ")
		}
		doc.WriteString(strconv.Quote(n))
	}
	doc.WriteString("}\n")
	files["doc.go"] = doc.String()

	out := make(map[string][]byte, len(files))
	for name, body := range files {
		src, err := finishGo(pkg, name, body)
		if err != nil {
			return nil, err
		}
		out[name] = src
	}
	return out, nil
}

// finishGo adds the header, package clause and imports to body and formats the result.
func finishGo(pkg, file, body string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("// " + header + "\n\n")
	if file == "doc.go" {
		b.WriteString(body)
	} else {
		b.WriteString("package " + pkg + "\n\n")
		var imports []string
		if strings.Contains(body, "json.RawMessage") {
			imports = append(imports, goImportJSON)
		}
		if strings.Contains(body, "content.") {
			imports = append(imports, goImportContent)
		}
		if len(imports) > 0 {
			b.WriteString("import (\n\t" + strings.Join(imports, "\n\n\t") + "\n)\n\n")
		}
		b.WriteString(body)
	}
	src, err := format.Source(b.Bytes())
	if err != nil {
		return nil, fmt.Errorf("codegen: format %s: %w", file, err)
	}
	return src, nil
}

type goGen struct {
	names  *names
	groups map[string]*schema.Group
}

// structDecl renders a struct (plus the nested struct and enum declarations its fields need). owner
// describes the struct for error messages.
func (g *goGen) structDecl(name, comment string, fields []*schema.Field, owner string) (string, error) {
	var b, nested strings.Builder
	b.WriteString(lineComment("", comment))
	b.WriteString("type " + name + " struct {\n")
	seen := map[string]*schema.Field{}
	for _, f := range fields {
		if err := checkName("field", f.Name); err != nil {
			return "", fmt.Errorf("%w (in %s)", err, owner)
		}
		fname, err := goName(f.Name)
		if err != nil {
			return "", fmt.Errorf("%w (in %s)", err, owner)
		}
		if prev := seen[fname]; prev != nil {
			return "", fmt.Errorf("codegen: fields %q and %q of %s both become %s.%s", prev.Name, f.Name, owner, name, fname)
		}
		seen[fname] = f
		typ, err := g.fieldType(name, f.Name, f, &nested)
		if err != nil {
			return "", fmt.Errorf("%w (field %q of %s)", err, f.Name, owner)
		}
		if f.Description != "" {
			b.WriteString(lineComment("\t", f.Description))
		}
		tag := f.Name
		if !keepZero(f) {
			tag += ",omitempty"
		}
		if optional(f) && pointerable(f.Shape.Base) {
			typ = "*" + typ
		}
		fmt.Fprintf(&b, "\t%s %s `json:%s`\n", fname, typ, strconv.Quote(tag))
	}
	b.WriteString("}\n")
	if nested.Len() > 0 {
		b.WriteString("\n" + nested.String())
	}
	return b.String() + "\n", nil
}

// pointerable reports whether optional fields of this base use a pointer (the rest have a usable nil/empty).
func pointerable(b schema.Base) bool {
	switch b {
	case schema.BaseList, schema.BaseRichText, schema.BaseJSON, schema.BaseBlocks:
		return false
	}
	return true
}

// fieldType returns the non-pointer Go type of f, appending declarations it needs to nested. prefix and
// key name any nested type: prefix + Go-cased key.
func (g *goGen) fieldType(prefix, key string, f *schema.Field, nested *strings.Builder) (string, error) {
	switch f.Shape.Base {
	case schema.BaseString:
		switch f.Shape.Format {
		case "", "text", "slug", "email", "url":
			return "string", nil
		case "date":
			return "content.Date", nil
		case "datetime":
			return "content.DateTime", nil
		case "token":
			return tokenTypeName(f.Shape.Set)
		case "enum":
			return g.enumDecl(prefix, key, f, nested)
		}
		return "", fmt.Errorf("codegen: unknown string format %q", f.Shape.Format)
	case schema.BaseNumber:
		if f.Shape.Integer {
			return "int64", nil
		}
		return "float64", nil
	case schema.BaseBoolean:
		return "bool", nil
	case schema.BaseRef:
		return "content.Ref", nil
	case schema.BaseFile:
		return "content.File", nil
	case schema.BaseRichText:
		return "content.RichText", nil
	case schema.BaseBlocks:
		return "[]Block", nil
	case schema.BaseJSON:
		return "json.RawMessage", nil
	case schema.BaseList:
		if f.Of == nil {
			return "", fmt.Errorf("codegen: list without an element type")
		}
		elem, err := g.fieldType(prefix, key, f.Of, nested)
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
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
		body, err := g.structDecl(name, name+" is the inline \""+key+"\" group.", f.Fields, what)
		if err != nil {
			return "", err
		}
		nested.WriteString(body)
		return name, nil
	}
	return "", fmt.Errorf("codegen: unsupported field base %q", f.Shape.Base)
}

func (g *goGen) enumDecl(prefix, key string, f *schema.Field, nested *strings.Builder) (string, error) {
	k, err := goName(key)
	if err != nil {
		return "", err
	}
	name := prefix + k
	what := fmt.Sprintf("the enum %q", key)
	if err := g.names.declare(name, what); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(lineComment("", name+" is the allowed values of the \""+key+"\" field."))
	b.WriteString("type " + name + " string\n\nconst (\n")
	consts, err := enumConstNames(f.Shape.Enum)
	if err != nil {
		return "", err
	}
	for i, v := range f.Shape.Enum {
		if err := g.names.declare(name+consts[i], fmt.Sprintf("enum value %q of %s", v, what)); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "\t%s %s = %s\n", name+consts[i], name, strconv.Quote(v))
	}
	b.WriteString(")\n\n")
	nested.WriteString(b.String())
	return name, nil
}

// enumConstNames maps each enum value to a Go constant suffix. Values that map to the same name keep the
// first (in value order) as is; later ones get a numeric suffix, "EnUS", "EnUS2", "EnUS3".
func enumConstNames(values []string) ([]string, error) {
	base := make([]string, len(values))
	used := make(map[string]bool, len(values))
	for i, v := range values {
		c := "Empty"
		if v != "" {
			var err error
			if c, err = goName(v); err != nil {
				return nil, fmt.Errorf("enum value %q: %w", v, err)
			}
		}
		base[i] = c
	}
	order := make([]int, len(values))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return cmp.Compare(values[a], values[b]) })
	out := make([]string, len(values))
	var dups []int
	for _, i := range order {
		if used[base[i]] {
			dups = append(dups, i)
			continue
		}
		used[base[i]] = true
		out[i] = base[i]
	}
	for _, i := range dups {
		for n := 2; ; n++ {
			if c := base[i] + strconv.Itoa(n); !used[c] {
				used[c] = true
				out[i] = c
				break
			}
		}
	}
	return out, nil
}

// keepZero reports whether a field's zero value is meaningful and must not be dropped by omitempty.
func keepZero(f *schema.Field) bool {
	return f.Required && f.When == nil && (f.Shape.Base == schema.BaseNumber || f.Shape.Base == schema.BaseBoolean)
}
