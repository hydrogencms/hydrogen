// Package schema compiles a project's YAML schema files (types, fields, reusable groups) into an immutable
// *Schema, and validates, normalises and projects documents against it.
//
// Contract: planning/design/content-core.md. Types and method sets in this file are shared with the content
// service, the storage adapters, codegen and the management API; change them only with the architect.
package schema

import (
	"fmt"
	"strconv"
	"strings"
)

// Pos is a position in a schema file, or the module that added a definition programmatically.
type Pos struct {
	File string
	Line int
}

func (p Pos) String() string {
	if p.Line == 0 {
		return p.File
	}
	return p.File + ":" + strconv.Itoa(p.Line)
}

// Schema is a compiled, immutable schema. Build one with a Builder.
type Schema struct {
	types  []*Type // sorted by name
	byName map[string]*Type
	groups []*Group     // sorted by name
	blocks []*BlockType // sorted by name
	tokens []*TokenSet  // sorted by name
	lk     *blockLookup
	hash   string
	opts   map[*Field]map[string]any // type-specific options by field, for the canonical encoding
}

func (s *Schema) Types() []*Type { return s.types }

func (s *Schema) Type(name string) (*Type, bool) {
	t, ok := s.byName[name]
	return t, ok
}

// Groups are the named, reusable field groups (referenced with `use:`).
func (s *Schema) Groups() []*Group { return s.groups }

// Hash identifies the schema's content: same definitions, same hash, on every machine.
func (s *Schema) Hash() string { return s.hash }

type Type struct {
	Name        string
	Label       string
	Description string
	Singleton   bool
	Page        bool
	Review      bool    // publishing needs an approved review (the review workflow)
	Upload      *Upload // non-nil for upload types (assets)
	Blockset    bool    // its single blocks field can be embedded with the built-in shared block
	NamePattern string  // e.g. "{title}"; "" means the first string field
	Admin       TypeAdmin
	AI          TypeAI
	Fields      []*Field       // file order
	Options     map[string]any // module options by key (Registry.TypeOption); nil when none
	Pos         Pos
}

func (t *Type) Field(name string) (*Field, bool) {
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}

type TypeAdmin struct {
	List  []string
	Group string
	Icon  string
}

type TypeAI struct {
	Instructions string
	Generate     []string
}

type Group struct {
	Name        string
	Description string
	Fields      []*Field
	Pos         Pos
}

type Field struct {
	Name         string
	Label        string
	Description  string
	Type         string // field type name, e.g. "string", "ref", "list"
	Required     bool
	Translatable bool // top-level fields only
	Index        bool
	Default      any // JSON-compatible; numbers are json.Number
	When         *Condition
	AI           FieldAI
	Admin        FieldAdmin
	Shape        Shape
	Config       Config   // compiled type-specific behaviour
	Of           *Field   // list element
	Fields       []*Field // group members
	Group        string   // named group this field uses, if any
	Pos          Pos
}

type FieldAI struct {
	Exclude  bool // ai: false — never sent to a model or indexed for retrieval
	Describe string
}

type FieldAdmin struct {
	Widget      string
	Placeholder string
	Hidden      bool
	ReadOnly    bool
}

// Condition shows a field only when a sibling field matches.
type Condition struct {
	Field string
	Op    string // "eq", "ne", "in", "empty"
	Value any    // for "in": []any; for "empty": bool
}

// Base is the JSON shape of a field's value, used by codegen, JSON Schema and storage projections.
type Base string

const (
	BaseString   Base = "string"
	BaseNumber   Base = "number"
	BaseBoolean  Base = "boolean"
	BaseRef      Base = "ref"      // item id string
	BaseRichText Base = "richtext" // ProseMirror document
	BaseList     Base = "list"
	BaseGroup    Base = "group"
	BaseJSON     Base = "json"
	BaseBlocks   Base = "blocks" // a tree of blocks
	BaseFile     Base = "file"   // a stored file: {id, name, mime, size, width?, height?, focal?}
)

type Shape struct {
	Base    Base
	Format  string   // strings: "", "text", "slug", "email", "url", "date", "datetime", "enum", "token"; refs: "" or "asset"
	Integer bool     // numbers
	Enum    []string // Format "enum" or "token"
	Set     string   // Format "token": the token set
	To      []string // BaseRef: allowed collections
	Allow   []string // BaseBlocks: allowed block types
}

// Mode selects which rules Validate applies.
type Mode int

const (
	// ModeDraft checks shape and constraints but not required fields; it runs on every draft write.
	ModeDraft Mode = iota
	// ModePublish adds required fields and list minimums.
	ModePublish
)

// FieldError is one validation problem, addressed by a JSON path such as "tags[2]" or "address.city".
type FieldError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e FieldError) Error() string { return e.Path + ": " + e.Message }

// maxReportErrors caps a Report: after this many errors it stops collecting and appends one too_many_errors.
const maxReportErrors = 100

type pathSeg struct {
	key string
	idx int // >= 0: an array index, key unused
}

// Report collects field errors in document order, at most maxReportErrors of them. Validators track their
// position with Enter/EnterIndex/Leave, so the path of a field is only built when an error is reported.
type Report struct {
	errs []FieldError
	segs []pathSeg
	cut  bool
	adds int // attempts to add an error, including dropped ones

	blockIDs map[string]bool // ids of the blocks seen so far in the document
	blocks   int             // blocks seen so far in the document
}

// Add records an error at an absolute path.
func (r *Report) Add(path Path, code, format string, args ...any) {
	if !r.accept() {
		return
	}
	r.errs = append(r.errs, FieldError{Path: string(path), Code: code, Message: fmt.Sprintf(format, args...)})
}

// Fail records an error at the current position (see Enter).
func (r *Report) Fail(code, format string, args ...any) {
	if !r.accept() {
		return
	}
	r.errs = append(r.errs, FieldError{Path: string(r.here()), Code: code, Message: fmt.Sprintf(format, args...)})
}

// Enter descends into the object member name; pair it with Leave.
func (r *Report) Enter(name string) { r.segs = append(r.segs, pathSeg{key: name, idx: -1}) }

// EnterIndex descends into the array element i; pair it with Leave.
func (r *Report) EnterIndex(i int) { r.segs = append(r.segs, pathSeg{idx: i}) }

// Leave undoes the last Enter or EnterIndex.
func (r *Report) Leave() { r.segs = r.segs[:len(r.segs)-1] }

func (r *Report) here() Path {
	var p Path
	for _, sg := range r.segs {
		if sg.idx >= 0 {
			p = p.Index(sg.idx)
		} else {
			p = p.Key(sg.key)
		}
	}
	return p
}

// accept reports whether one more error may be recorded; at the cap it appends the too_many_errors marker.
func (r *Report) accept() bool {
	r.adds++
	return !r.full()
}

// full reports whether the report holds its maximum; validators stop descending once it does.
func (r *Report) full() bool {
	if len(r.errs) < maxReportErrors {
		return false
	}
	if !r.cut {
		r.cut = true
		r.errs = append(r.errs, FieldError{Code: "too_many_errors",
			Message: fmt.Sprintf("too many errors; only the first %d are reported", maxReportErrors)})
	}
	return true
}

func (r *Report) Errors() []FieldError { return r.errs }

func (r *Report) OK() bool { return len(r.errs) == 0 }

// Path is a JSON path into a document.
type Path string

func (p Path) Key(name string) Path {
	if p == "" {
		return Path(name)
	}
	return Path(string(p) + "." + name)
}

func (p Path) Index(i int) Path { return Path(string(p) + "[" + strconv.Itoa(i) + "]") }

// Config is a compiled field type: what the rest of Hydrogen needs from one field. Values passed in are
// decoded JSON (objects map[string]any, arrays []any, numbers json.Number) and never nil.
type Config interface {
	// Validate reports problems with v with r.Fail, and wraps nested values in r.Enter/EnterIndex and
	// r.Leave. Once the report is full, Fail does nothing.
	Validate(v any, mode Mode, r *Report)
	// IsEmpty reports whether v counts as missing for `required`.
	IsEmpty(v any) bool
	// JSONSchema describes valid values (draft 2020-12).
	JSONSchema() map[string]any
}

// IndexValue is one projection of an indexed field into storage columns. Exactly one value is set.
type IndexValue struct {
	Field string // top-level field name
	Text  *string
	Num   *float64
	Time  *int64 // Unix milliseconds, UTC
	Bool  *bool
	Ref   string // item id
}

// RefValue is an item reference found in a document.
type RefValue struct {
	Path     string
	To       string   // item id
	Kind     string   // "field" or "link"
	OnDelete string   // "block", "nullify", "warn"
	Types    []string // for fields: the types the target may have
}

// SplitName turns "blogPost" into "Blog post" for default labels.
func SplitName(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case i == 0:
			b.WriteString(strings.ToUpper(string(r)))
		case r >= 'A' && r <= 'Z':
			b.WriteByte(' ')
			b.WriteString(strings.ToLower(string(r)))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
