package schema

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// Upload marks a type whose items are uploaded files (assets): the type gets the built-in shared fields file,
// folder and tags, and its items are what `asset` fields reference.
type Upload struct {
	// Accept lists MIME patterns ("image/*", "application/pdf") an upload must match; empty accepts anything.
	Accept []string
	// MaxSize is the largest file in bytes; 0 means only the server's own limit applies.
	MaxSize int64
}

// Matches reports whether mime (without parameters) is accepted.
func (u *Upload) Matches(mime string) bool {
	if u == nil || len(u.Accept) == 0 {
		return true
	}
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = strings.TrimSpace(mime[:i])
	}
	for _, p := range u.Accept {
		switch {
		case p == "*/*" || p == mime:
			return true
		case strings.HasSuffix(p, "/*") && strings.HasPrefix(mime, p[:len(p)-1]):
			return true
		}
	}
	return false
}

// Built-in field names of upload types.
const (
	UploadFile   = "file"
	UploadFolder = "folder"
	UploadTags   = "tags"
)

func isUploadField(name string) bool {
	return name == UploadFile || name == UploadFolder || name == UploadTags
}

// folderPattern accepts "/" and "/a/b": absolute, no empty segments, no trailing slash.
const folderPattern = `^/$|^(/[^/]+)+$`

func uploadFieldSpecs() []struct {
	name string
	spec Spec
} {
	return []struct {
		name string
		spec Spec
	}{
		{UploadFile, Spec{"type": "file", "required": true, "index": true, "label": "File"}},
		{UploadFolder, Spec{"type": "string", "default": "/", "index": true, "max": 500, "pattern": folderPattern, "label": "Folder"}},
		{UploadTags, Spec{"type": "list", "of": Spec{"type": "string", "max": 100}, "index": true, "max": 50, "label": "Tags"}},
	}
}

// uploadSpec reads the `upload:` key of a type.
func (s src) uploadSpec(def *typeDef, e entry) {
	def.upload = &Upload{}
	def.uploadPos = s.pos(e.k)
	n := deref(e.v)
	if b, ok := asBool(n); ok {
		if !b {
			def.upload = nil
		}
		return
	}
	if isNull(n) {
		return
	}
	ents, _ := s.mapping(e.v, "upload")
	for _, a := range ents {
		switch a.key {
		case "accept":
			list, ok := asStrings(a.v)
			if !ok {
				s.errorf(a.k, "upload.accept must be a list of MIME patterns")
				continue
			}
			for _, p := range list {
				if !validMIMEPattern(p) {
					s.errorf(a.k, "upload.accept: %q is not a MIME type or pattern such as image/* or application/pdf", p)
				}
			}
			def.upload.Accept = uniqueSorted(list)
		case "max_size":
			size, err := parseSize(a.v)
			if err != nil {
				s.errorf(a.k, "upload.max_size %v", err)
				continue
			}
			def.upload.MaxSize = size
		default:
			s.errorf(a.k, "unknown key %q in upload (known: accept, max_size)", a.key)
		}
	}
}

func validMIMEPattern(p string) bool {
	typ, sub, ok := strings.Cut(p, "/")
	if !ok || typ == "" || sub == "" || p != strings.ToLower(p) {
		return false
	}
	for _, part := range []string{typ, sub} {
		for _, r := range part {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && !strings.ContainsRune("!#$&-^_.+*", r) {
				return false
			}
		}
	}
	return sub != "*" || typ != "*" || p == "*/*"
}

// parseSize reads a byte count: a whole number, or a string such as "20MB", "512 KB", "1.5GB" (binary multiples).
func parseSize(n *yaml.Node) (int64, error) {
	n = deref(n)
	if v, ok := asInt(n); ok {
		if v <= 0 {
			return 0, fmt.Errorf("must be positive")
		}
		return int64(v), nil
	}
	str, ok := asString(n)
	if !ok {
		return 0, fmt.Errorf("must be a size such as 20MB or a number of bytes")
	}
	return ParseSize(str)
}

// ParseSize parses "20MB", "512 KiB", "1.5GB" or a plain number of bytes (K, M and G are binary multiples).
func ParseSize(str string) (int64, error) {
	s := strings.ToUpper(strings.TrimSpace(str))
	s = strings.TrimSuffix(s, "IB")
	s = strings.TrimSuffix(s, "B")
	mult := float64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult, s = 1<<10, strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult, s = 1<<20, strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		mult, s = 1<<30, strings.TrimSuffix(s, "G")
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 || math.IsInf(f, 0) || math.IsNaN(f) || f*mult > math.MaxInt64/2 {
		return 0, fmt.Errorf("must be a positive size such as 20MB, not %q", str)
	}
	return int64(f * mult), nil
}

// prepareUpload checks an upload type and puts the built-in fields in front of its own.
func (s src) prepareUpload(def *typeDef) {
	if def.upload == nil {
		return
	}
	// Type.Options["upload"] mirrors Type.Upload for code that reads type options generically.
	opt := map[string]any{}
	if len(def.upload.Accept) > 0 {
		accept := make([]any, len(def.upload.Accept))
		for i, a := range def.upload.Accept {
			accept[i] = a
		}
		opt["accept"] = accept
	}
	if def.upload.MaxSize > 0 {
		opt["max_size"] = json.Number(strconv.FormatInt(def.upload.MaxSize, 10))
	}
	if def.options == nil {
		def.options = map[string]optionDef{}
	}
	def.options["upload"] = optionDef{value: opt, pos: def.uploadPos}
	for _, what := range []struct {
		on   bool
		name string
	}{{def.singleton, "singleton"}, {def.page, "page"}, {def.blockset, "blockset"}} {
		if what.on {
			s.errs.add(def.uploadPos, "type %q: an upload type cannot also be %s", def.name, what.name)
		}
	}
	var own []*fieldDef
	for _, f := range def.fields {
		if isUploadField(f.name) {
			s.errs.add(Pos{File: f.file, Line: f.line}, "type %q: field %q is built into upload types and cannot be declared", def.name, f.name)
			continue
		}
		own = append(own, f)
	}
	var built []*fieldDef
	for _, b := range uploadFieldSpecs() {
		n, err := toNode(b.spec)
		if err != nil {
			panic(err) // the specs are constants
		}
		built = append(built, &fieldDef{name: b.name, node: n, file: def.pos.File, line: def.pos.Line})
	}
	def.fields = append(built, own...)
}

// ---- file

type fileType struct{}

func (fileType) Name() string { return "file" }

func (fileType) Compile(*Options) (Shape, Config, error) {
	return Shape{Base: BaseFile}, fileConfig{}, nil
}

type fileConfig struct{}

const maxFileName = 255

// shaPattern is a lower-case hex SHA-256.
func isSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// FileNameProblem returns why name is not a valid file name, or "".
func FileNameProblem(name string) string {
	switch {
	case strings.TrimSpace(name) == "":
		return "must not be empty"
	case utf8.RuneCountInString(name) > maxFileName:
		return fmt.Sprintf("must be at most %d characters", maxFileName)
	case strings.ContainsAny(name, "/\\"):
		return "must not contain slashes"
	case name == "." || name == "..":
		return "is not a file name"
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "must not contain control characters"
		}
	}
	return ""
}

func wholeNumber(v any) (int64, bool) {
	f, exact, ok := numberOf(v)
	if !ok || !exact || f < 0 || f >= 1<<53 {
		return 0, false
	}
	return int64(f), true
}

func (fileConfig) Validate(v any, _ Mode, r *Report) {
	m, ok := v.(map[string]any)
	if !ok {
		typeErr(r, "a file object")
		return
	}
	id, ok := m["id"].(string)
	if _, has := m["id"]; !has || !ok || !isSHA256(id) {
		r.Enter("id")
		r.Fail("format", "must be the SHA-256 of the file as 64 lower-case hex digits")
		r.Leave()
	}
	if name, ok := m["name"].(string); !ok {
		r.Enter("name")
		r.Fail("type", "must be the file name")
		r.Leave()
	} else if msg := FileNameProblem(name); msg != "" {
		r.Enter("name")
		r.Fail("format", "%s", msg)
		r.Leave()
	}
	if mime, has := m["mime"]; has {
		if s, ok := mime.(string); !ok || hasNUL(s, &Report{}) || len(s) > 255 {
			r.Enter("mime")
			r.Fail("type", "must be a MIME type")
			r.Leave()
		}
	}
	for _, k := range []string{"size", "width", "height"} {
		if x, has := m[k]; has {
			if _, ok := wholeNumber(x); !ok {
				r.Enter(k)
				r.Fail("type", "must be a non-negative whole number")
				r.Leave()
			}
		}
	}
	if x, has := m["focal"]; has {
		validateFocal(x, r)
	}
	for _, k := range sortedKeys(m) {
		switch k {
		case "id", "name", "mime", "size", "width", "height", "focal":
		default:
			r.Enter(k)
			r.Fail("unknown_field", "unknown field %q", k)
			r.Leave()
		}
	}
}

func validateFocal(v any, r *Report) {
	r.Enter("focal")
	defer r.Leave()
	f, ok := v.(map[string]any)
	if !ok {
		typeErr(r, "an object with x and y")
		return
	}
	for _, k := range []string{"x", "y"} {
		x, has := f[k]
		n, _, isNum := numberOf(x)
		if !has || !isNum || n < 0 || n > 1 {
			r.Enter(k)
			r.Fail("range", "must be a number from 0 to 1")
			r.Leave()
		}
	}
	for _, k := range sortedKeys(f) {
		if k != "x" && k != "y" {
			r.Enter(k)
			r.Fail("unknown_field", "unknown field %q", k)
			r.Leave()
		}
	}
}

func (fileConfig) IsEmpty(v any) bool {
	m, ok := v.(map[string]any)
	return ok && m["id"] == nil
}

func (fileConfig) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"id", "name"},
		"description": "A stored file. Only id, name and focal are taken from the client; the server fills in the rest.",
		"properties": map[string]any{
			"id":     map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$", "description": "SHA-256 of the file."},
			"name":   map[string]any{"type": "string", "minLength": 1, "maxLength": maxFileName},
			"mime":   map[string]any{"type": "string"},
			"size":   map[string]any{"type": "integer", "minimum": 0},
			"width":  map[string]any{"type": "integer", "minimum": 0},
			"height": map[string]any{"type": "integer", "minimum": 0},
			"focal": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"x", "y"},
				"properties": map[string]any{
					"x": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
					"y": map[string]any{"type": "number", "minimum": 0, "maximum": 1}}},
		},
	}
}

// ---- asset

type assetType struct{}

func (assetType) Name() string { return "asset" }

func (assetType) Compile(o *Options) (Shape, Config, error) {
	cfg := &refConfig{onDelete: "nullify", asset: true}
	to, ok := o.Strings("to")
	if !o.Has("to") {
		to = o.UploadTypes()
		if len(to) == 0 {
			o.Errorf("", "the schema has no upload types to reference")
		}
	} else if ok && len(to) == 0 {
		o.Errorf("to", "to must name at least one upload type")
	}
	for _, name := range to {
		switch {
		case !o.TypeExists(name):
			o.Errorf("to", "to names unknown type %q", name)
		case !slicesContains(o.UploadTypes(), name):
			o.Errorf("to", "to names type %q, which is not an upload type", name)
		}
	}
	if v, ok := o.String("on_delete"); ok {
		switch v {
		case "block", "nullify", "warn":
			cfg.onDelete = v
		default:
			o.Errorf("on_delete", "on_delete must be block, nullify or warn")
		}
	}
	cfg.to = to
	return Shape{Base: BaseRef, Format: "asset", To: to}, cfg, nil
}

func slicesContains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// UploadTypes lists the upload types of the schema, sorted.
func (o *Options) UploadTypes() []string {
	var out []string
	for _, name := range sortedKeys(o.c.b.types) {
		if o.c.b.types[name].upload != nil {
			out = append(out, name)
		}
	}
	return out
}

// moduleOptions are the type options that are not the upload mirror (which has its own place in the schema).
func (t *Type) moduleOptions() map[string]any {
	if t.Upload == nil {
		return t.Options
	}
	out := make(map[string]any, len(t.Options))
	for k, v := range t.Options {
		if k != "upload" {
			out[k] = v
		}
	}
	return out
}
