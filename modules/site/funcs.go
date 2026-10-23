package site

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

var funcNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// predeclared are the names text/template defines itself.
var predeclared = []string{"and", "call", "html", "index", "slice", "js", "len", "not", "or", "print", "printf", "println",
	"urlquery", "eq", "ge", "gt", "le", "lt", "ne"}

// builtinFuncs are the generic template functions of every site: pure and deterministic. The subject of a
// function is its last argument, so they chain in pipelines ({{.Title | truncate 40}}). None marks a string safe.
func builtinFuncs() template.FuncMap {
	return template.FuncMap{
		"dict": dictFn, "list": func(v ...any) []any { return v },
		"add": func(a, b any) (any, error) { return arith("add", a, b) },
		"sub": func(a, b any) (any, error) { return arith("sub", a, b) },
		"mul": func(a, b any) (any, error) { return arith("mul", a, b) },
		"div": func(a, b any) (any, error) { return arith("div", a, b) },
		"mod": func(a, b any) (any, error) { return arith("mod", a, b) },
		"seq": seqFn, "default": defaultFn,
		"lower": strings.ToLower, "upper": strings.ToUpper, "title": titleFn, "trim": func(s any) string { return strings.TrimSpace(str(s)) },
		"contains":  containsFn,
		"hasPrefix": func(prefix string, s any) bool { return strings.HasPrefix(str(s), prefix) },
		"hasSuffix": func(suffix string, s any) bool { return strings.HasSuffix(str(s), suffix) },
		"split":     func(sep string, s any) []string { return strings.Split(str(s), sep) },
		"join":      joinFn,
		"replace":   func(old, repl string, s any) string { return strings.ReplaceAll(str(s), old, repl) },
		"truncate":  truncateFn, "date": dateFn, "json": jsonFn, "first": firstFn, "last": lastFn,
		"get": getFn, "append": appendFn, "ternary": ternaryFn, "isLast": isLastFn,
	}
}

func dictFn(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, errors.New("dict: odd number of arguments")
	}
	m := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key %v is %T, not a string", kv[i], kv[i])
		}
		m[k] = kv[i+1]
	}
	return m, nil
}

// num reads any Go number (JSON numbers are float64). integral is true for integers and whole floats.
func num(v any) (f float64, integral, ok bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true, true
	case reflect.Float32, reflect.Float64:
		f = rv.Float()
		return f, f == math.Trunc(f) && math.Abs(f) < 1<<53, true
	}
	return 0, false, false
}

// arith returns an int when both operands are integral (and a division is exact), else a float64.
func arith(op string, a, b any) (any, error) {
	x, xi, ok1 := num(a)
	y, yi, ok2 := num(b)
	if !ok1 || !ok2 {
		return nil, fmt.Errorf("%s: operands must be numbers, got %T and %T", op, a, b)
	}
	whole := xi && yi
	switch op {
	case "add":
		x += y
	case "sub":
		x -= y
	case "mul":
		x *= y
	case "div", "mod":
		if y == 0 {
			return nil, fmt.Errorf("%s: division by zero", op)
		}
		if op == "mod" {
			if !whole {
				return nil, errors.New("mod: operands must be integers")
			}
			return int(x) % int(y), nil
		}
		x /= y
		whole = whole && x == math.Trunc(x)
	}
	if whole {
		return int(x), nil
	}
	return x, nil
}

const maxSeq = 10000

// seqFn is 0..n-1 for one argument and a..b (both included, descending when a > b) for two.
func seqFn(args ...any) ([]int, error) {
	var from, to int
	ints := make([]int, len(args))
	for i, a := range args {
		f, integral, ok := num(a)
		if !ok || !integral {
			return nil, fmt.Errorf("seq: %v is not an integer", a)
		}
		ints[i] = int(f)
	}
	switch len(ints) {
	case 1:
		from, to = 0, ints[0]-1
	case 2:
		from, to = ints[0], ints[1]
	default:
		return nil, errors.New("seq: expects one or two arguments")
	}
	n := to - from
	if n < 0 {
		n = -n
	}
	if n >= maxSeq {
		return nil, fmt.Errorf("seq: more than %d numbers", maxSeq)
	}
	out := make([]int, 0, n+1)
	step := 1
	if to < from {
		step = -1
	}
	if len(ints) == 1 && to < from {
		return out, nil
	}
	for i := from; ; i += step {
		out = append(out, i)
		if i == to {
			break
		}
	}
	return out, nil
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	if f, _, ok := num(v); ok {
		return f == 0
	}
	if t, ok := v.(time.Time); ok {
		return t.IsZero()
	}
	return false
}

func defaultFn(def any, given ...any) any {
	if len(given) == 0 || isEmpty(given[0]) {
		return def
	}
	return given[0]
}

func str(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case template.HTML:
		return string(x)
	case fmt.Stringer:
		return x.String()
	}
	if f, integral, ok := num(v); ok && integral {
		return fmt.Sprint(int64(f))
	}
	return fmt.Sprint(v)
}

func titleFn(s any) string {
	b := []rune(str(s))
	start := true
	for i, r := range b {
		if start {
			b[i] = []rune(strings.ToUpper(string(r)))[0]
		}
		start = r == ' ' || r == '\t' || r == '\n' || r == '-'
	}
	return string(b)
}

func listOf(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

func same(a, b any) bool {
	if x, _, ok := num(a); ok {
		y, _, ok := num(b)
		return ok && x == y
	}
	return reflect.DeepEqual(a, b)
}

// containsFn: needle in a string, or an element in a list.
func containsFn(needle, haystack any) bool {
	if s, ok := haystack.(string); ok {
		return strings.Contains(s, str(needle))
	}
	l, _ := listOf(haystack)
	return slices.ContainsFunc(l, func(e any) bool { return same(e, needle) })
}

func joinFn(sep string, v any) string {
	l, _ := listOf(v)
	parts := make([]string, len(l))
	for i, e := range l {
		parts[i] = str(e)
	}
	return strings.Join(parts, sep)
}

// truncateFn is truncate n s or truncate n ellipsis s: at most n runes including the ellipsis.
func truncateFn(n int, args ...any) (string, error) {
	var ell, s string
	switch len(args) {
	case 1:
		s = str(args[0])
	case 2:
		ell, s = str(args[0]), str(args[1])
	default:
		return "", errors.New("truncate: expects n, an optional ellipsis and the string")
	}
	r := []rune(s)
	if n < 0 || len(r) <= n {
		return s, nil
	}
	e := []rune(ell)
	if len(e) >= n {
		return string(e[:n]), nil
	}
	return strings.TrimRight(string(r[:n-len(e)]), " ") + ell, nil
}

var dateLayouts = []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"}

// dateFn formats a time or a date string (RFC3339 or 2006-01-02) with a Go layout; "" when it is neither.
func dateFn(layout string, v any) string {
	switch x := v.(type) {
	case time.Time:
		return x.Format(layout)
	case *time.Time:
		if x != nil {
			return x.Format(layout)
		}
	case string:
		for _, l := range dateLayouts {
			if t, err := time.Parse(l, x); err == nil {
				return t.Format(layout)
			}
		}
	}
	return ""
}

func jsonFn(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

func firstFn(v any) any {
	if l, _ := listOf(v); len(l) > 0 {
		return l[0]
	}
	return nil
}

func lastFn(v any) any {
	if l, _ := listOf(v); len(l) > 0 {
		return l[len(l)-1]
	}
	return nil
}

// getFn reads a dot path from maps, slices (numeric steps), structs (exported field or json name) and pointers:
// get "seo.image.alt" .Fields. Every step is nil-safe: a missing key, a bad index or a nil value gives nil. The
// subject is the last argument; the path may also come last when the subject is not a string.
func getFn(a, b any) any {
	path, v := "", b
	if p, ok := a.(string); ok {
		path = p
	} else if p, ok := b.(string); ok {
		path, v = p, a
	}
	if path == "" {
		return v
	}
	for _, step := range strings.Split(path, ".") {
		if v = getStep(v, step); v == nil {
			return nil
		}
	}
	return v
}

func getStep(v any, step string) any {
	rv := reflect.ValueOf(v)
	for rv.IsValid() && (rv.Kind() == reflect.Pointer || rv.Kind() == reflect.Interface) {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil
		}
		e := rv.MapIndex(reflect.ValueOf(step).Convert(rv.Type().Key()))
		if !e.IsValid() {
			return nil
		}
		return nilable(e)
	case reflect.Slice, reflect.Array:
		i, err := strconv.Atoi(step)
		if err != nil || i < 0 || i >= rv.Len() {
			return nil
		}
		return nilable(rv.Index(i))
	case reflect.Struct:
		t := rv.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.IsExported() && (f.Name == step || name == step) {
				return nilable(rv.Field(i))
			}
		}
	}
	return nil
}

// nilable returns the value as an any, or nil for a nil pointer, map, slice or interface.
func nilable(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		if v.IsNil() {
			return nil
		}
	}
	return v.Interface()
}

// appendFn returns a new list: append x list, or append x y list for several elements.
func appendFn(args ...any) ([]any, error) {
	if len(args) < 2 {
		return nil, errors.New("append: expects elements and the list")
	}
	l, ok := listOf(args[len(args)-1])
	if !ok && args[len(args)-1] != nil {
		return nil, fmt.Errorf("append: %T is not a list", args[len(args)-1])
	}
	return append(l, args[:len(args)-1]...), nil
}

// ternaryFn is ternary a b cond: a when cond is not empty, else b.
func ternaryFn(a, b, cond any) any {
	if isEmpty(cond) {
		return b
	}
	return a
}

// isLastFn tells whether index i is the end of the list, for range: isLast $i $list.
func isLastFn(i int, v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return i == rv.Len()-1
	}
	return false
}

// AddFuncs registers project template functions. A name must be an identifier, must not be a page helper, a
// built-in or one of Go's predeclared functions, and registers once.
func (x *Extensions) AddFuncs(m template.FuncMap) error {
	builtins := builtinFuncs()
	for _, name := range sortedKeys(m) {
		fn := m[name]
		t := reflect.TypeOf(fn)
		switch {
		case !funcNameRE.MatchString(name):
			return fmt.Errorf("site: template function %q is not a valid name", name)
		case slices.Contains(helpers, name) || slices.Contains(predeclared, name) || builtins[name] != nil:
			return fmt.Errorf("site: template function %s collides with a built-in function or page helper", name)
		case x.funcs[name] != nil:
			return fmt.Errorf("site: template function %s is registered twice", name)
		case t == nil || t.Kind() != reflect.Func || t.NumOut() < 1 || t.NumOut() > 2 ||
			(t.NumOut() == 2 && t.Out(1) != reflect.TypeFor[error]()):
			return fmt.Errorf("site: template function %s must be a function returning one value, or a value and an error", name)
		}
	}
	for k, v := range m {
		x.funcs[k] = v
	}
	return nil
}

// Funcs registers template functions for every template of the site (parse and render time), including those
// of themes. Names that collide with a page helper, a built-in function or another registration fail start-up
// and config:check.
func Funcs(h Registrar, m template.FuncMap) {
	h.Use(&extModule{name: "site-funcs-" + suffix(strings.Join(sortedKeys(m), ",")), apply: func(x *Extensions) error { return x.AddFuncs(m) }})
}
