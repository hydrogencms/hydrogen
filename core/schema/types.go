package schema

import (
	"encoding/json"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	slugRun        = `[\p{Ll}\p{Lo}\p{Lm}\p{Nd}]+`
	slugPattern    = `^` + slugRun + `(-` + slugRun + `)*$`
	dateLayout     = "2006-01-02"
	datetimeLayout = "2006-01-02T15:04:05.000Z07:00"
	defaultSlugMax = 120
)

func coreTypes() []FieldType {
	return []FieldType{
		stringType{name: "string"}, stringType{name: "text", multiline: true},
		richtextType{}, numberType{}, booleanType{}, enumType{}, dateType{name: "date"},
		dateType{name: "datetime", datetime: true}, slugType{}, emailType{}, urlType{}, refType{}, listType{}, fileType{}, assetType{},
		groupType{}, jsonType{}, blocksType{}, tokenType{},
	}
}

// hasNUL reports a NUL character in s, which Postgres cannot store.
func hasNUL(s string, r *Report) bool {
	if strings.IndexByte(s, 0) < 0 {
		return false
	}
	r.Fail("format", "must not contain NUL characters")
	return true
}

func typeErr(r *Report, want string) {
	r.Fail("type", "must be %s", want)
}

// limits are min/max bounds on lengths or item counts.
type limits struct {
	min, max int
	hasMax   bool
}

func readLimits(o *Options, maxDefault int) limits {
	var l limits
	if v, ok := o.Int("min"); ok {
		if v < 0 {
			o.Errorf("min", "min must not be negative")
		}
		l.min = v
	}
	if v, ok := o.Int("max"); ok {
		if v < 0 {
			o.Errorf("max", "max must not be negative")
		}
		l.max, l.hasMax = v, true
	} else if maxDefault > 0 {
		l.max, l.hasMax = maxDefault, true
	}
	if l.hasMax && l.min > l.max {
		o.Errorf("min", "min must not exceed max")
	}
	return l
}

func (l limits) schema(m map[string]any, minKey, maxKey string) {
	if l.min > 0 {
		m[minKey] = l.min
	}
	if l.hasMax {
		m[maxKey] = l.max
	}
}

// checkLength applies the limits to a string of n runes.
func (l limits) checkLength(s string, r *Report) {
	if l.min == 0 && !l.hasMax {
		return
	}
	n := utf8.RuneCountInString(s)
	if l.hasMax && n > l.max {
		r.Fail("max_length", "must be at most %d characters", l.max)
	}
	if n < l.min {
		r.Fail("min_length", "must be at least %d characters", l.min)
	}
}

// ---- string, text

type stringType struct {
	name      string
	multiline bool
}

func (t stringType) Name() string { return t.name }

func (t stringType) Compile(o *Options) (Shape, Config, error) {
	cfg := &stringConfig{multiline: t.multiline, lim: readLimits(o, 0)}
	if !t.multiline {
		if p, ok := o.String("pattern"); ok {
			re, err := regexp.Compile(p)
			if err != nil {
				o.Errorf("pattern", "pattern is not a valid regular expression: %v", err)
			}
			cfg.pattern = re
		}
	}
	shape := Shape{Base: BaseString}
	if t.multiline {
		shape.Format = "text"
	}
	return shape, cfg, nil
}

type stringConfig struct {
	multiline bool
	lim       limits
	pattern   *regexp.Regexp
}

func (c *stringConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	if hasNUL(s, r) {
		return
	}
	if !c.multiline && strings.ContainsAny(s, "\r\n") {
		r.Fail("format", "must be a single line")
	}
	c.lim.checkLength(s, r)
	if c.pattern != nil && !c.pattern.MatchString(s) {
		r.Fail("pattern", "must match %s", c.pattern)
	}
}

func (c *stringConfig) IsEmpty(v any) bool { return v == "" }

func (c *stringConfig) JSONSchema() map[string]any {
	m := map[string]any{"type": "string"}
	c.lim.schema(m, "minLength", "maxLength")
	if c.pattern != nil {
		m["pattern"] = c.pattern.String()
	}
	return m
}

// ---- slug

type slugType struct{}

func (slugType) Name() string { return "slug" }

func (slugType) Compile(o *Options) (Shape, Config, error) {
	cfg := &slugConfig{max: defaultSlugMax}
	if v, ok := o.Int("max"); ok {
		if v < 1 {
			o.Errorf("max", "max must be at least 1")
		}
		cfg.max = v
	}
	if v, ok := o.String("allow"); ok {
		cfg.allow = slugAllow(o, v)
	}
	if from, ok := o.String("from"); ok {
		typ, found := o.SiblingType(from)
		if !found || (typ != "string" && typ != "text") {
			o.Errorf("from", "from must name a sibling string or text field, not %q", from)
		}
		cfg.from = from
	}
	shape := Shape{Base: BaseString, Format: "slug"}
	for i := range cfg.allow {
		shape.Allow = append(shape.Allow, cfg.allow[i:i+1])
	}
	return shape, cfg, nil
}

// slugAllow normalises the allow option: any of "." and ",", each once.
func slugAllow(o *Options, v string) string {
	var out []byte
	for _, c := range []byte(".,") {
		if strings.IndexByte(v, c) >= 0 {
			out = append(out, c)
		}
	}
	if len(out) != len(v) {
		o.Errorf("allow", "allow takes any of %q and %q, each once", ".", ",")
	}
	return string(out)
}

type slugConfig struct {
	max   int
	from  string
	allow string // extra joining characters, a subset of ".," in that order
}

func (c *slugConfig) allowList() string {
	parts := make([]string, len(c.allow))
	for i := range c.allow {
		parts[i] = strconv.Quote(c.allow[i : i+1])
	}
	return strings.Join(parts, ", ")
}

// slugRune reports whether r may appear in a slug: a lower-case letter, another letter without case, a
// modifier letter or a decimal digit.
func slugRune(r rune) bool {
	if r < utf8.RuneSelf {
		return r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
	}
	return unicode.In(r, unicode.Ll, unicode.Lo, unicode.Lm, unicode.Nd)
}

// IsSlug reports whether s is runs of slug characters joined by single dashes. Characters of allow (a subset
// of ".,") may also join runs, alone or followed by a dash: "b.v", "acme,-inc".
func IsSlug(s, allow string) bool {
	start := true
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case slugRune(r):
			start = false
		case start:
			return false
		case r == '-':
			start = true
		case r < utf8.RuneSelf && strings.IndexByte(allow, byte(r)) >= 0:
			start = true
			if i+n < len(s) && s[i+n] == '-' {
				n++
			}
		default:
			return false
		}
		i += n
	}
	return !start
}

func (c *slugConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	if hasNUL(s, r) {
		return
	}
	if !IsSlug(s, c.allow) {
		if c.allow != "" {
			r.Fail("format", "must be a slug (lowercase letters, digits, single dashes and %s between them)", c.allowList())
		} else {
			r.Fail("format", "must be a slug (lowercase letters, digits and single dashes)")
		}
		return
	}
	if utf8.RuneCountInString(s) > c.max {
		r.Fail("max_length", "must be at most %d characters", c.max)
	}
}

func (c *slugConfig) IsEmpty(v any) bool { return v == "" }

func (c *slugConfig) JSONSchema() map[string]any {
	pattern := slugPattern
	if c.allow != "" {
		pattern = slugRun + `(?:(?:-|[` + regexp.QuoteMeta(c.allow) + `]-?)` + slugRun + `)*$`
		pattern = "^" + pattern
	}
	return map[string]any{"type": "string", "pattern": pattern, "maxLength": c.max}
}

// ---- email, url

type emailType struct{}

func (emailType) Name() string { return "email" }

func (emailType) Compile(*Options) (Shape, Config, error) {
	return Shape{Base: BaseString, Format: "email"}, emailConfig{}, nil
}

type emailConfig struct{}

func (emailConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	if hasNUL(s, r) {
		return
	}
	if a, err := mail.ParseAddress(s); err != nil || a.Address != s {
		r.Fail("format", "must be an email address")
	}
}

func (emailConfig) IsEmpty(v any) bool { return v == "" }

func (emailConfig) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "format": "email"}
}

type urlType struct{}

func (urlType) Name() string { return "url" }

func (urlType) Compile(o *Options) (Shape, Config, error) {
	relative, _ := o.Bool("relative")
	return Shape{Base: BaseString, Format: "url"}, urlConfig{relative: relative}, nil
}

// urlConfig validates a url; relative also accepts a root-relative path such as /docs/start?x=1#top.
type urlConfig struct{ relative bool }

// isRootRelative reports whether s is a path starting with a single slash, with an optional query and fragment,
// and no whitespace, control characters or backslashes.
func isRootRelative(s string) bool {
	if len(s) < 1 || s[0] != '/' || (len(s) > 1 && (s[1] == '/' || s[1] == '\\')) {
		return false
	}
	for _, r := range s {
		if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "" && u.Host == ""
}

func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F'):
			return false
		}
	}
	return true
}

func (c urlConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	if hasNUL(s, r) {
		return
	}
	if rest, found := strings.CutPrefix(s, "item:"); found {
		if !isUUID(rest) {
			r.Fail("format", "item: links need an item id")
		}
		return
	}
	if c.relative && strings.HasPrefix(s, "/") {
		if !isRootRelative(s) {
			r.Fail("format", "must be a root-relative path (one leading slash, no spaces or backslashes)")
		}
		return
	}
	u, err := url.Parse(s)
	if err != nil || strings.ContainsAny(s, "\\\x00\t\r\n") || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		if c.relative {
			r.Fail("format", "must be an absolute http(s) URL, a root-relative path or item:<id>")
		} else {
			r.Fail("format", "must be an absolute http(s) URL or item:<id>")
		}
	}
}

func (urlConfig) IsEmpty(v any) bool { return v == "" }

func (c urlConfig) JSONSchema() map[string]any {
	if c.relative {
		return map[string]any{"type": "string", "description": "Absolute http(s) URL, root-relative path, or item:<id>."}
	}
	return map[string]any{"type": "string", "description": "Absolute http(s) URL, or item:<id>."}
}

// ---- enum

type enumType struct{}

func (enumType) Name() string { return "enum" }

func (enumType) Compile(o *Options) (Shape, Config, error) {
	values, ok := o.Strings("values")
	switch {
	case !o.Has("values"):
		o.Errorf("", "values is required")
	case ok && len(values) == 0:
		o.Errorf("values", "values must not be empty")
	case ok && len(uniqueSorted(values)) != len(values):
		o.Errorf("values", "values must be unique")
	}
	return Shape{Base: BaseString, Format: "enum", Enum: values}, &enumConfig{values: values}, nil
}

type enumConfig struct{ values []string }

func (c *enumConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	if hasNUL(s, r) {
		return
	}
	for _, x := range c.values {
		if x == s {
			return
		}
	}
	r.Fail("enum", "must be one of %s", strings.Join(c.values, ", "))
}

func (c *enumConfig) IsEmpty(v any) bool { return v == "" }

func (c *enumConfig) JSONSchema() map[string]any {
	return map[string]any{"type": "string", "enum": append([]string(nil), c.values...)}
}

// ---- number

type numberType struct{}

func (numberType) Name() string { return "number" }

func (numberType) Compile(o *Options) (Shape, Config, error) {
	cfg := &numberConfig{}
	cfg.integer, _ = o.Bool("integer")
	if v, ok := o.Float("min"); ok {
		cfg.min = &v
	}
	if v, ok := o.Float("max"); ok {
		cfg.max = &v
	}
	if cfg.min != nil && cfg.max != nil && *cfg.min > *cfg.max {
		o.Errorf("min", "min must not exceed max")
	}
	return Shape{Base: BaseNumber, Integer: cfg.integer}, cfg, nil
}

type numberConfig struct {
	integer  bool
	min, max *float64
}

// numberOf reads any JSON number representation; exact reports whether it is an integer without fraction.
func numberOf(v any) (f float64, exact, ok bool) {
	switch x := v.(type) {
	case json.Number:
		s := string(x)
		f, err := strconv.ParseFloat(s, 64)
		return f, isIntegerLiteral(s), err == nil
	case float64:
		return x, x == math.Trunc(x) && math.Abs(x) < 1<<53, !math.IsNaN(x) && !math.IsInf(x, 0)
	case float32:
		return float64(x), x == float32(math.Trunc(float64(x))), true
	case int:
		return float64(x), true, true
	case int32:
		return float64(x), true, true
	case int64:
		return float64(x), true, true
	case uint64:
		return float64(x), true, true
	}
	return 0, false, false
}

func fmtNum(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func (c *numberConfig) Validate(v any, _ Mode, r *Report) {
	if c.integer {
		c.validateInteger(v, r)
		return
	}
	f, _, ok := numberOf(v)
	if !ok {
		typeErr(r, "a number")
		return
	}
	if c.min != nil && f < *c.min {
		r.Fail("min", "must be at least %s", fmtNum(*c.min))
	}
	if c.max != nil && f > *c.max {
		r.Fail("max", "must be at most %s", fmtNum(*c.max))
	}
}

func (c *numberConfig) IsEmpty(any) bool { return false }

func (c *numberConfig) JSONSchema() map[string]any {
	m := map[string]any{"type": "number"}
	if c.integer {
		m["type"] = "integer"
	}
	if c.min != nil {
		m["minimum"] = *c.min
	}
	if c.max != nil {
		m["maximum"] = *c.max
	}
	return m
}

// ---- boolean

type booleanType struct{}

func (booleanType) Name() string { return "boolean" }

func (booleanType) Compile(*Options) (Shape, Config, error) {
	return Shape{Base: BaseBoolean}, booleanConfig{}, nil
}

type booleanConfig struct{}

func (booleanConfig) Validate(v any, _ Mode, r *Report) {
	if _, ok := v.(bool); !ok {
		typeErr(r, "true or false")
	}
}

func (booleanConfig) IsEmpty(any) bool { return false }

func (booleanConfig) JSONSchema() map[string]any { return map[string]any{"type": "boolean"} }

// ---- date, datetime

type dateType struct {
	name     string
	datetime bool
}

func (t dateType) Name() string { return t.name }

func (t dateType) Compile(o *Options) (Shape, Config, error) {
	cfg := &dateConfig{datetime: t.datetime}
	cfg.min = t.bound(o, "min")
	cfg.max = t.bound(o, "max")
	if !cfg.min.IsZero() && !cfg.max.IsZero() && cfg.min.After(cfg.max) {
		o.Errorf("min", "min must not be after max")
	}
	return Shape{Base: BaseString, Format: t.name}, cfg, nil
}

func (t dateType) bound(o *Options, key string) time.Time {
	s, ok := o.String(key)
	if !ok {
		return time.Time{}
	}
	d, err := parseTime(s, t.datetime)
	if err != nil {
		o.Errorf(key, "%s must be a %s", key, t.name)
	}
	return d
}

func parseTime(s string, datetime bool) (time.Time, error) {
	if datetime {
		return time.Parse(time.RFC3339, s)
	}
	return time.Parse(dateLayout, s)
}

type dateConfig struct {
	datetime bool
	min, max time.Time
}

func (c *dateConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "a string")
		return
	}
	t, err := parseTime(s, c.datetime)
	if err != nil {
		if c.datetime {
			r.Fail("format", "must be an RFC 3339 date and time")
		} else {
			r.Fail("format", "must be a date (YYYY-MM-DD)")
		}
		return
	}
	if !c.min.IsZero() && t.Before(c.min) {
		r.Fail("min", "must not be before %s", c.min.Format(c.layout()))
	}
	if !c.max.IsZero() && t.After(c.max) {
		r.Fail("max", "must not be after %s", c.max.Format(c.layout()))
	}
}

func (c *dateConfig) layout() string {
	if c.datetime {
		return time.RFC3339
	}
	return dateLayout
}

func (c *dateConfig) IsEmpty(v any) bool { return v == "" }

func (c *dateConfig) JSONSchema() map[string]any {
	if c.datetime {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	return map[string]any{"type": "string", "format": "date"}
}

// ---- ref

type refType struct{}

func (refType) Name() string { return "ref" }

func (refType) Compile(o *Options) (Shape, Config, error) {
	cfg := &refConfig{onDelete: "block"}
	to, ok := o.Strings("to")
	switch {
	case !o.Has("to"):
		o.Errorf("", "to is required")
	case ok && len(to) == 0:
		o.Errorf("to", "to must name at least one type")
	}
	for _, name := range to {
		if !o.TypeExists(name) {
			o.Errorf("to", "to names unknown type %q", name)
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
	return Shape{Base: BaseRef, To: to}, cfg, nil
}

type refConfig struct {
	to       []string
	onDelete string
	asset    bool // an `asset` field: delivery expands it
}

func (c *refConfig) Validate(v any, _ Mode, r *Report) {
	s, ok := v.(string)
	if !ok {
		typeErr(r, "an item id")
		return
	}
	if !isUUID(s) {
		r.Fail("format", "must be an item id")
	}
}

func (c *refConfig) IsEmpty(v any) bool { return v == "" }

func (c *refConfig) JSONSchema() map[string]any {
	m := map[string]any{"type": "string", "format": "uuid", "x-hydrogen-to": append([]string(nil), c.to...)}
	if c.asset {
		m["x-hydrogen-asset"] = true
	}
	return m
}

// ---- json

type jsonType struct{}

func (jsonType) Name() string { return "json" }

func (jsonType) Compile(*Options) (Shape, Config, error) {
	return Shape{Base: BaseJSON}, jsonConfig{}, nil
}

type jsonConfig struct{}

func (jsonConfig) Validate(any, Mode, *Report) {}
func (jsonConfig) IsEmpty(any) bool            { return false }
func (jsonConfig) JSONSchema() map[string]any  { return map[string]any{} }

// ---- list

type listType struct{}

func (listType) Name() string { return "list" }

func (listType) Compile(o *Options) (Shape, Config, error) {
	cfg := &listConfig{lim: readLimits(o, 0)}
	if !o.Has("of") {
		o.Errorf("", "of is required")
	}
	cfg.of = o.SubField("of")
	return Shape{Base: BaseList}, cfg, nil
}

type listConfig struct {
	of  *Field
	lim limits
}

func (c *listConfig) Validate(v any, mode Mode, r *Report) {
	list, ok := v.([]any)
	if !ok {
		typeErr(r, "a list")
		return
	}
	if c.lim.hasMax && len(list) > c.lim.max {
		r.Fail("max_items", "must have at most %d items", c.lim.max)
	}
	if mode == ModePublish && len(list) < c.lim.min {
		r.Fail("min_items", "must have at least %d items", c.lim.min)
	}
	for i, e := range list {
		if r.full() {
			return
		}
		r.EnterIndex(i)
		if e == nil {
			r.Fail("type", "must not be null")
		} else {
			c.of.Config.Validate(e, mode, r)
		}
		r.Leave()
	}
}

func (c *listConfig) IsEmpty(v any) bool {
	list, ok := v.([]any)
	return ok && len(list) == 0
}

func (c *listConfig) JSONSchema() map[string]any {
	m := map[string]any{"type": "array", "items": fieldSchema(c.of)}
	c.lim.schema(m, "minItems", "maxItems")
	return m
}

// ---- group

type groupType struct{}

func (groupType) Name() string { return "group" }

func (groupType) Compile(o *Options) (Shape, Config, error) {
	switch {
	case o.Has("fields") && o.Has("use"):
		o.Errorf("use", "use and fields are mutually exclusive")
	case o.Has("fields"):
		o.SubFields("fields")
	case o.Has("use"):
		o.UseGroup("use")
	default:
		o.Errorf("", "a group needs fields or use")
	}
	return Shape{Base: BaseGroup}, &groupConfig{fields: o.fields}, nil
}

type groupConfig struct{ fields []*Field }

func (c *groupConfig) Validate(v any, mode Mode, r *Report) {
	m, ok := v.(map[string]any)
	if !ok {
		typeErr(r, "an object")
		return
	}
	validateFields(c.fields, m, mode, r)
}

func (c *groupConfig) IsEmpty(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

func (c *groupConfig) JSONSchema() map[string]any { return objectSchema(c.fields) }
