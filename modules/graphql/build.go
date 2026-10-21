package graphql

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	gql "github.com/graphql-go/graphql"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/schema"
)

// caps are the optional delivery routes that add root fields.
type caps struct {
	route  bool
	search bool
}

// chanSchema is a channel's GraphQL schema with what the resolvers and the limits need to know about it.
type chanSchema struct {
	schema  gql.Schema
	objects map[string]*gql.Object // content type name → object type
	meta    map[string]map[string]fieldMeta
	sdl     string
	allowed []string // the channel's content types, sorted

	srcs     map[string]map[string]string // content object name → GraphQL field → stored field
	wrappers map[string]bool              // list types: their items field holds the content items
	cheap    map[string]string            // content type → a small field to project when a selection needs none
	anyCheap string
}

// fieldMeta says how a field of a generated type leads to other items, for sizing the include of a request.
type fieldMeta struct {
	target string // GraphQL type name of the composite the field returns ("" for unions and Item)
	ref    bool   // following the field resolves a reference
}

// convFn turns a stored value into what the field's GraphQL type serialises.
type convFn func(rc *reqCtx, n *node, v any, info gql.ResolveInfo) (any, error)

type ftype struct {
	out  gql.Output
	conv convFn
	meta *fieldMeta
}

// Names the generated schema reserves.
var reservedTypes = []string{"Query", "Item", "Asset", "File", "JSON", "Route", "SearchResult", "Long", "String", "Int", "Float", "Boolean", "ID"}

// itemFields are the fields every content type has; a content field with the same name is renamed.
var itemFields = []string{"id", "type", "name", "key", "locale", "url", "publishedAt"}

var validName = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

type builder struct {
	sc   *schema.Schema
	ch   *channel.Channel
	caps caps

	used    map[string]bool
	objects map[string]*gql.Object
	names   map[string]string // content type → GraphQL name
	types   []*schema.Type

	json   *gql.Scalar
	long   *gql.Scalar
	item   *gql.Interface
	asset  *gql.Object
	file   *gql.Object
	unions map[string]*gql.Union
	meta   map[string]map[string]fieldMeta
	srcs   map[string]map[string]string
	lists  map[string]bool
	extra  []gql.Type
}

func buildSchema(sc *schema.Schema, ch *channel.Channel, c caps) (*chanSchema, error) {
	b := &builder{sc: sc, ch: ch, caps: c, used: map[string]bool{}, objects: map[string]*gql.Object{},
		names: map[string]string{}, unions: map[string]*gql.Union{}, meta: map[string]map[string]fieldMeta{},
		srcs: map[string]map[string]string{}, lists: map[string]bool{}}
	for _, n := range reservedTypes {
		b.used[n] = true
	}
	for _, t := range sc.Types() {
		if ch.AllowsCollection(t.Name) {
			b.types = append(b.types, t)
		}
	}
	slices.SortFunc(b.types, func(x, y *schema.Type) int { return strings.Compare(x.Name, y.Name) })

	b.json = jsonScalar()
	b.long = longScalar()
	b.asset = b.assetType()
	b.file = b.fileType()
	b.item = gql.NewInterface(gql.InterfaceConfig{
		Name:        "Item",
		Description: "A content item.",
		Fields:      b.itemInterfaceFields(),
		ResolveType: func(p gql.ResolveTypeParams) *gql.Object {
			if n, ok := p.Value.(*node); ok {
				return b.objects[n.it.typ]
			}
			return nil
		},
	})

	prepared := map[string]gql.Fields{}
	for _, t := range b.types {
		name := b.unique(pascal(t.Name))
		b.names[t.Name] = name
		typ := t.Name
		b.objects[typ] = gql.NewObject(gql.ObjectConfig{
			Name:        name,
			Description: describe(t),
			Interfaces:  []*gql.Interface{b.item},
			Fields:      gql.FieldsThunk(func() gql.Fields { return prepared[typ] }),
		})
	}
	lists := map[string]*gql.Object{}
	for _, t := range b.types {
		if !t.Singleton {
			lists[t.Name] = b.listType(t)
		}
	}
	for _, t := range b.types {
		prepared[t.Name] = b.typeFields(t)
	}

	query, err := b.queryType(lists)
	if err != nil {
		return nil, err
	}
	var all []gql.Type
	for _, t := range b.types {
		all = append(all, b.objects[t.Name])
	}
	all = append(all, b.extra...)
	s, err := gql.NewSchema(gql.SchemaConfig{Query: query, Types: all})
	if err != nil {
		return nil, fmt.Errorf("build schema: %w", err)
	}
	cs := &chanSchema{schema: s, objects: b.objects, meta: b.meta, wrappers: b.lists, srcs: map[string]map[string]string{}, cheap: map[string]string{}}
	for _, t := range b.types {
		cs.allowed = append(cs.allowed, t.Name)
		cs.srcs[b.objects[t.Name].Name()] = b.srcs[b.objects[t.Name].Name()]
		if c := cheapField(t); c != "" {
			cs.cheap[t.Name] = c
			if cs.anyCheap == "" {
				cs.anyCheap = c
			}
		}
	}
	cs.sdl = printSDL(&s)
	return cs, nil
}

func describe(t *schema.Type) string {
	return t.Description
}

// unique reserves a type name; a taken name gets the first free numeric suffix, so names are stable for a schema.
func (b *builder) unique(name string) string {
	if !b.used[name] {
		b.used[name] = true
		return name
	}
	for i := 2; ; i++ {
		if n := name + strconv.Itoa(i); !b.used[n] {
			b.used[n] = true
			return n
		}
	}
}

func (b *builder) setMeta(typ, field string, m fieldMeta) {
	if b.meta[typ] == nil {
		b.meta[typ] = map[string]fieldMeta{}
	}
	b.meta[typ][field] = m
}

func (b *builder) itemInterfaceFields() gql.Fields {
	return gql.Fields{
		"id":          {Type: gql.NewNonNull(gql.ID), Description: "The item's id.", Resolve: itemKey("id")},
		"type":        {Type: gql.NewNonNull(gql.String), Description: "The content type's name.", Resolve: itemKey("type")},
		"name":        {Type: gql.String, Description: "The item's display name.", Resolve: itemKey("name")},
		"key":         {Type: gql.String, Description: "The item's key, when it has one.", Resolve: itemKey("key")},
		"locale":      {Type: gql.NewNonNull(gql.String), Description: "The locale the item is shown in.", Resolve: itemKey("locale")},
		"url":         {Type: gql.String, Description: "The item's URL on the channel, when it has one.", Resolve: itemKey("url")},
		"publishedAt": {Type: gql.String, Description: "When the shown version was published (ISO 8601).", Resolve: itemKey("publishedAt")},
	}
}

func itemKey(k string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		n, ok := p.Source.(*node)
		if !ok || n.it == nil {
			return nil, nil
		}
		return n.it.m[k], nil
	}
}

// typeFields builds the fields of a content type: the Item fields, then the type's own.
func (b *builder) typeFields(t *schema.Type) gql.Fields {
	name := b.names[t.Name]
	fs := b.itemInterfaceFields()
	used := map[string]bool{}
	for _, n := range itemFields {
		used[n] = true
	}
	b.addFields(fs, used, name, t.Fields)
	return fs
}

// addFields adds one GraphQL field per schema field. A field named like a taken name (the Item fields) gets a
// "Field" suffix, then a number.
func (b *builder) addFields(out gql.Fields, used map[string]bool, owner string, fields []*schema.Field) {
	for _, f := range fields {
		gname := f.Name
		if used[gname] {
			gname = f.Name + "Field"
			for i := 2; used[gname]; i++ {
				gname = f.Name + "Field" + strconv.Itoa(i)
			}
		}
		used[gname] = true
		if b.srcs[owner] == nil {
			b.srcs[owner] = map[string]string{}
		}
		b.srcs[owner][gname] = f.Name
		ft := b.fieldType(f, owner+"_"+f.Name, owner, gname)
		src := f.Name
		out[gname] = &gql.Field{Type: ft.out, Description: f.Description, Resolve: func(p gql.ResolveParams) (any, error) {
			n, ok := p.Source.(*node)
			if !ok {
				return nil, nil
			}
			v := n.v[src]
			if v == nil {
				return nil, nil
			}
			return ft.conv(reqOf(p.Context), n, v, p.Info)
		}}
		if ft.meta != nil {
			b.setMeta(owner, gname, *ft.meta)
		}
	}
}

// fieldType maps a schema field (or list element) to its GraphQL type. base names generated group and enum
// types; owner and gname locate the field for the include analysis.
func (b *builder) fieldType(f *schema.Field, base, owner, gname string) ftype {
	switch f.Shape.Base {
	case schema.BaseString:
		if f.Shape.Format == "enum" && enumOK(f.Shape.Enum) {
			return ftype{out: b.enum(base, f), conv: convString}
		}
		return ftype{out: gql.String, conv: convString}
	case schema.BaseNumber:
		if f.Shape.Integer {
			if int32Bounded(f) {
				return ftype{out: gql.Int, conv: convInt}
			}
			return ftype{out: b.long, conv: convLong}
		}
		return ftype{out: gql.Float, conv: convFloat}
	case schema.BaseBoolean:
		return ftype{out: gql.Boolean, conv: convPlain}
	case schema.BaseRichText, schema.BaseJSON, schema.BaseBlocks:
		return ftype{out: b.json, conv: convPlain}
	case schema.BaseFile:
		return ftype{out: b.file, conv: convMap}
	case schema.BaseGroup:
		g := b.groupType(base, f)
		return ftype{out: g, conv: convGroup, meta: &fieldMeta{target: g.Name()}}
	case schema.BaseRef:
		if f.Shape.Format == "asset" {
			return ftype{out: b.asset, conv: convMap}
		}
		return b.refType(f, base)
	case schema.BaseList:
		if f.Of == nil {
			return ftype{out: gql.NewList(b.json), conv: convPlain}
		}
		el := b.fieldType(f.Of, base, owner, gname)
		conv := el.conv
		// Elements that can be unavailable (references, assets) may be null; the others cannot.
		elemOut := el.out
		if f.Of.Shape.Base != schema.BaseRef {
			elemOut = gql.NewNonNull(el.out)
		}
		return ftype{out: gql.NewList(elemOut), meta: el.meta, conv: func(rc *reqCtx, n *node, v any, info gql.ResolveInfo) (any, error) {
			list, ok := v.([]any)
			if !ok {
				return nil, nil
			}
			if err := rc.spendNodes(len(list)); err != nil {
				return nil, err
			}
			out := make([]any, len(list))
			for i, x := range list {
				if x == nil {
					continue
				}
				y, err := conv(rc, n, x, info)
				if err != nil {
					return nil, err
				}
				out[i] = y
			}
			return out, nil
		}}
	}
	return ftype{out: b.json, conv: convPlain}
}

// int32Bounded reports whether the field declares both min and max within GraphQL's Int.
func int32Bounded(f *schema.Field) bool {
	if f.Config == nil {
		return false
	}
	js := f.Config.JSONSchema()
	lo, ok1 := numberOf(js["minimum"])
	hi, ok2 := numberOf(js["maximum"])
	return ok1 && ok2 && lo >= math.MinInt32 && hi <= math.MaxInt32
}

func numberOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

func enumOK(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, v := range values {
		if !validName.MatchString(v) || strings.HasPrefix(v, "__") || v == "true" || v == "false" || v == "null" {
			return false
		}
	}
	return true
}

func (b *builder) enum(base string, f *schema.Field) *gql.Enum {
	vals := gql.EnumValueConfigMap{}
	for _, v := range f.Shape.Enum {
		vals[v] = &gql.EnumValueConfig{Value: v}
	}
	e := gql.NewEnum(gql.EnumConfig{Name: b.unique(base), Description: f.Description, Values: vals})
	b.extra = append(b.extra, e)
	return e
}

func (b *builder) groupType(base string, f *schema.Field) *gql.Object {
	name := b.unique(base)
	fs := gql.Fields{}
	b.addFields(fs, map[string]bool{}, name, f.Fields)
	if len(fs) == 0 {
		fs["_"] = &gql.Field{Type: gql.Boolean, Description: "The group has no fields."}
	}
	o := gql.NewObject(gql.ObjectConfig{Name: name, Description: f.Description, Fields: fs})
	b.extra = append(b.extra, o)
	return o
}

// refType is the type of a reference: the referenced type, a union of several, or Item for any.
func (b *builder) refType(f *schema.Field, base string) ftype {
	var targets []string
	for _, t := range f.Shape.To {
		if _, ok := b.objects[t]; ok {
			targets = append(targets, t)
		}
	}
	slices.Sort(targets)
	allowed := targets
	conv := func(rc *reqCtx, n *node, v any, info gql.ResolveInfo) (any, error) {
		id, ok := v.(string)
		if !ok {
			return nil, nil
		}
		return rc.resolveRef(n, id, allowed, info)
	}
	switch {
	case len(f.Shape.To) > 0 && len(targets) == 0, len(f.Shape.To) == 0:
		return ftype{out: b.item, conv: conv, meta: &fieldMeta{ref: true}}
	case len(targets) == 1:
		o := b.objects[targets[0]]
		return ftype{out: o, conv: conv, meta: &fieldMeta{ref: true, target: o.Name()}}
	}
	return ftype{out: b.union(targets), conv: conv, meta: &fieldMeta{ref: true}}
}

func (b *builder) union(targets []string) *gql.Union {
	parts := make([]string, len(targets))
	members := make([]*gql.Object, len(targets))
	for i, t := range targets {
		parts[i] = b.names[t]
		members[i] = b.objects[t]
	}
	key := strings.Join(parts, "Or")
	if u := b.unions[key]; u != nil {
		return u
	}
	u := gql.NewUnion(gql.UnionConfig{
		Name:  b.unique(key),
		Types: members,
		ResolveType: func(p gql.ResolveTypeParams) *gql.Object {
			if n, ok := p.Value.(*node); ok {
				return b.objects[n.it.typ]
			}
			return nil
		},
	})
	b.unions[key] = u
	b.extra = append(b.extra, u)
	return u
}

func (b *builder) assetType() *gql.Object {
	m := func(k string, conv func(any) any) gql.FieldResolveFn {
		return func(p gql.ResolveParams) (any, error) {
			a, _ := p.Source.(map[string]any)
			v := a[k]
			if v == nil || conv == nil {
				return v, nil
			}
			return conv(v), nil
		}
	}
	return gql.NewObject(gql.ObjectConfig{Name: "Asset", Description: "An uploaded file as a delivery response shows it.", Fields: gql.Fields{
		"id":       {Type: gql.NewNonNull(gql.ID), Resolve: m("id", nil)},
		"url":      {Type: gql.String, Resolve: m("url", nil)},
		"name":     {Type: gql.String, Resolve: m("name", nil)},
		"mime":     {Type: gql.String, Resolve: m("mime", nil)},
		"size":     {Type: b.long, Resolve: m("size", longOf)},
		"width":    {Type: b.long, Resolve: m("width", longOf)},
		"height":   {Type: b.long, Resolve: m("height", longOf)},
		"focal":    {Type: b.json, Resolve: m("focal", nil)},
		"variants": {Type: b.json, Description: "Image renditions, when the images module is on.", Resolve: m("variants", nil)},
		"fields":   {Type: b.json, Description: "The asset item's own fields.", Resolve: m("fields", nil)},
		"alt": {Type: gql.String, Description: "fields.alt when it is a string.", Resolve: func(p gql.ResolveParams) (any, error) {
			a, _ := p.Source.(map[string]any)
			fs, _ := a["fields"].(map[string]any)
			s, _ := fs["alt"].(string)
			if s == "" {
				return nil, nil
			}
			return s, nil
		}},
	}})
}

func (b *builder) fileType() *gql.Object {
	m := func(k string, conv func(any) any) gql.FieldResolveFn {
		return func(p gql.ResolveParams) (any, error) {
			a, _ := p.Source.(map[string]any)
			v := a[k]
			if v == nil || conv == nil {
				return v, nil
			}
			return conv(v), nil
		}
	}
	return gql.NewObject(gql.ObjectConfig{Name: "File", Description: "A stored file value.", Fields: gql.Fields{
		"id":     {Type: gql.NewNonNull(gql.ID), Description: "The file's content hash.", Resolve: m("id", nil)},
		"name":   {Type: gql.String, Resolve: m("name", nil)},
		"mime":   {Type: gql.String, Resolve: m("mime", nil)},
		"size":   {Type: b.long, Resolve: m("size", longOf)},
		"width":  {Type: b.long, Resolve: m("width", longOf)},
		"height": {Type: b.long, Resolve: m("height", longOf)},
		"focal":  {Type: b.json, Resolve: m("focal", nil)},
	}})
}

// listType is <Type>List: the items of a page and what the listing knows about the rest.
func (b *builder) listType(t *schema.Type) *gql.Object {
	obj := b.objects[t.Name]
	name := b.unique(obj.Name() + "List")
	b.setMeta(name, "items", fieldMeta{target: obj.Name()})
	b.lists[name] = true
	l := gql.NewObject(gql.ObjectConfig{Name: name, Description: "A page of " + obj.Name() + " items.", Fields: gql.Fields{
		"items":   {Type: gql.NewNonNull(gql.NewList(gql.NewNonNull(obj))), Resolve: listKey("items")},
		"hasMore": {Type: gql.NewNonNull(gql.Boolean), Description: "Whether another page follows.", Resolve: listKey("hasMore")},
		"total":   {Type: gql.Int, Description: "The number of matches; counted only when selected.", Resolve: listKey("total")},
	}})
	b.extra = append(b.extra, l)
	return l
}

func listKey(k string) gql.FieldResolveFn {
	return func(p gql.ResolveParams) (any, error) {
		l, ok := p.Source.(*listVal)
		if !ok {
			return nil, nil
		}
		switch k {
		case "items":
			return l.items, nil
		case "hasMore":
			return l.hasMore, nil
		}
		if l.total < 0 {
			return nil, nil
		}
		return l.total, nil
	}
}

func pascal(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
