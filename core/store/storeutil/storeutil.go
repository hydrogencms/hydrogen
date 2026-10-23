// Package storeutil holds the dialect-neutral logic the SQL adapters share when they implement
// store.ContentStore: field-name validation, condition value typing, search tokenising, paging defaults,
// search text merging and the result decoration algorithm. It contains no SQL; each adapter keeps only its
// statement building, bind conversion and row scanning.
package storeutil

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

const (
	DefaultLimit = 50
	MaxLimit     = 500

	// MaxSearchChars bounds the text indexed per item and locale. Postgres rejects a tsvector over 1 MB, and
	// 100,000 characters stay well under it whatever the words are.
	MaxSearchChars = 100_000
)

// ValidField reports whether s is a legal indexed field name (a-z first, then letters, digits and dots).
// Field names are bound as parameters, so this is defence in depth.
func ValidField(s string) bool {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		ch := s[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

// Normalize applies the paging defaults and bounds of the store contract.
func Normalize(q store.Query) store.Query {
	switch {
	case q.Limit <= 0:
		q.Limit = DefaultLimit
	case q.Limit > MaxLimit:
		q.Limit = MaxLimit
	}
	q.Offset = max(q.Offset, 0)
	return q
}

// SearchTerms splits user text into words made of letters and digits, the only characters both engines'
// full-text parsers treat alike.
func SearchTerms(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Scalar normalises one condition value to the canonical Go type of the column: string (text), float64
// (num), time.Time (time), bool (bool), ids.UUID (ref; ids may be given as strings). Adapters convert the
// result to their bind representation.
func Scalar(col store.Column, v any) (any, error) {
	switch col {
	case store.ColText:
		if s, ok := v.(string); ok {
			return s, nil
		}
	case store.ColNum:
		switch n := v.(type) {
		case float64:
			return n, nil
		case float32:
			return float64(n), nil
		case int:
			return float64(n), nil
		case int32:
			return float64(n), nil
		case int64:
			return float64(n), nil
		}
	case store.ColTime:
		if t, ok := v.(time.Time); ok {
			return t, nil
		}
	case store.ColBool:
		if b, ok := v.(bool); ok {
			return b, nil
		}
	case store.ColRef:
		switch u := v.(type) {
		case ids.UUID:
			return u, nil
		case string:
			if p, err := ids.Parse(u); err == nil {
				return p, nil
			}
		}
	}
	return nil, fmt.Errorf("value %v (%T) does not fit column %q", v, v, col)
}

// List converts the Value of an OpIn condition with Scalar.
func List(col store.Column, v any) ([]any, error) {
	var items []any
	switch l := v.(type) {
	case []any:
		items = l
	case []string:
		items = anySlice(l)
	case []float64:
		items = anySlice(l)
	case []int:
		items = anySlice(l)
	case []int64:
		items = anySlice(l)
	case []time.Time:
		items = anySlice(l)
	case []bool:
		items = anySlice(l)
	case []ids.UUID:
		items = anySlice(l)
	default:
		return nil, fmt.Errorf("value %v (%T) is not a list", v, v)
	}
	out := make([]any, len(items))
	for i, item := range items {
		x, err := Scalar(col, item)
		if err != nil {
			return nil, err
		}
		out[i] = x
	}
	return out, nil
}

func anySlice[T any](l []T) []any {
	out := make([]any, len(l))
	for i, v := range l {
		out[i] = v
	}
	return out
}

// SearchTexts merges docs of the same locale (joined by a newline), caps each text at MaxSearchChars and
// returns the locales in sorted order with their texts alongside.
func SearchTexts(docs []store.SearchDoc) (locales, texts []string) {
	by := make(map[string]string, len(docs))
	for _, d := range docs {
		if prev, ok := by[d.Locale]; ok {
			d.Text = prev + "\n" + d.Text
		}
		by[d.Locale] = capChars(StripControls(d.Text), MaxSearchChars)
	}
	locales = make([]string, 0, len(by))
	for l := range by {
		locales = append(locales, l)
	}
	slices.Sort(locales)
	texts = make([]string, len(locales))
	for i, l := range locales {
		texts[i] = by[l]
	}
	return locales, texts
}

// StripControls removes C0 control characters other than tab, newline and carriage return, so indexed text can
// never carry the snippet markers (\x02, \x03).
func StripControls(s string) string {
	if !strings.ContainsFunc(s, isControl) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isControl(r) {
			return -1
		}
		return r
	}, s)
}

func isControl(r rune) bool { return r < 0x20 && r != '\t' && r != '\n' && r != '\r' }

// CapSearchText strips control characters and truncates text to MaxSearchChars characters at a word boundary.
func CapSearchText(text string) string {
	text = StripControls(text)
	if utf8.RuneCountInString(text) <= MaxSearchChars {
		return text
	}
	cut := 0
	for i := range text {
		if cut == MaxSearchChars {
			text = text[:i]
			break
		}
		cut++
	}
	if i := strings.LastIndexAny(text, " \t\n"); i > 0 {
		text = text[:i]
	}
	return text
}

func capChars(s string, n int) string {
	if len(s) <= n { // bytes >= characters
		return s
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	cut := 0
	for i := range s {
		if cut == n {
			return s[:i]
		}
		cut++
	}
	return s
}

// Decorator fills ItemSummary.Name, Drafts and Locales for a page from the published locale rows and draft
// parts an adapter reads with set-based queries.
type Decorator struct {
	// Working picks names the way the working projection reads: a draft name beats the published one.
	Working bool

	locale string
	items  []store.ItemSummary
	index  map[ids.UUID]int
	names  []names
}

type names struct{ pubLoc, pubMain, draftLoc, draftMain string }

// NewDecorator starts decorating items (modified in place) for the query locale.
func NewDecorator(locale string, items []store.ItemSummary) *Decorator {
	d := &Decorator{locale: locale, items: items, index: make(map[ids.UUID]int, len(items)), names: make([]names, len(items))}
	for i, it := range items {
		d.index[it.ID] = i
	}
	return d
}

// IDs lists the page's item ids in page order.
func (d *Decorator) IDs() []ids.UUID {
	out := make([]ids.UUID, len(d.items))
	for i, it := range d.items {
		out[i] = it.ID
	}
	return out
}

// Published records one locale row of an item's published version.
func (d *Decorator) Published(id ids.UUID, locale, name string) {
	i := d.index[id]
	d.items[i].Locales = append(d.items[i].Locales, locale)
	if locale == d.locale {
		d.names[i].pubLoc = name
	}
	if locale == d.items[i].MainLocale {
		d.names[i].pubMain = name
	}
}

// Draft records one draft part of an item.
func (d *Decorator) Draft(id ids.UUID, part, name string) {
	i := d.index[id]
	d.items[i].Drafts = append(d.items[i].Drafts, part)
	if part == d.locale {
		d.names[i].draftLoc = name
	}
	if part == d.items[i].MainLocale {
		d.names[i].draftMain = name
	}
}

// Finish sorts the locale and draft lists and picks each name: published in the query locale, published in
// the main locale, then the same two for drafts. With Working, the draft and published names of each locale
// swap places.
func (d *Decorator) Finish() {
	for i := range d.items {
		it := &d.items[i]
		slices.Sort(it.Locales)
		slices.Sort(it.Drafts)
		n := d.names[i]
		order := [4]string{n.pubLoc, n.pubMain, n.draftLoc, n.draftMain}
		if d.Working {
			order = [4]string{n.draftLoc, n.pubLoc, n.draftMain, n.pubMain}
		}
		for _, c := range order {
			if c != "" {
				it.Name = c
				break
			}
		}
	}
}

// MaxWalkConds is the most field conditions a query may have to be served by walking the sort field's value
// index. More conditions make a walk likely to cross much of the index before the page fills, and the
// compiled plan (filters first, then sort) is the better bet.
const MaxWalkConds = 3

// IndexWalk reports whether q should be answered by walking the value index of its sort field: a single sort
// key on an indexed field, no text search, few conditions and none that usually matches only a few items
// (equality, membership, prefix). With such a condition it is cheaper to build the matching set and sort it
// than to walk an index that may hold few matches; the walk wins when the conditions are ranges or tests
// that most items pass, because the page then fills after a short walk. q must be normalised.
func IndexWalk(q store.Query) bool {
	if len(q.Sort) != 1 || q.Sort[0].Column == "" || leafCount(q.Where) > MaxWalkConds || strings.TrimSpace(q.Search) != "" {
		return false
	}
	for _, c := range q.Where {
		if Selective(c.Op) || SelectiveOr(c) {
			return false
		}
	}
	return true
}

func leafCount(conds []store.Cond) int {
	n := 0
	for _, c := range conds {
		if !IsGroup(c.Op) {
			n++
		}
		for _, g := range c.Groups {
			n += leafCount(g)
		}
	}
	return n
}

// SelectiveOr reports whether c is an or group in which every group has a selective condition, so that the
// union of those conditions' matches is small.
func SelectiveOr(c store.Cond) bool {
	if c.Op != store.OpOr || len(c.Groups) == 0 {
		return false
	}
	for _, g := range c.Groups {
		if !slices.ContainsFunc(g, func(l store.Cond) bool { return Selective(l.Op) }) {
			return false
		}
	}
	return true
}

// MaxGroupDepth bounds how deep boolean groups nest in a compiled query; the content service allows less.
const MaxGroupDepth = 3

// IsGroup reports whether op is a boolean group (store.OpOr, store.OpNot).
func IsGroup(op store.Op) bool { return op == store.OpOr || op == store.OpNot }

// Group compiles a boolean group condition to a boolean expression: leaf compiles the field conditions inside,
// yes and no are the dialect's constants true and false. Adapters call it from their condition compiler, so
// every group form compiles to plain AND/OR/NOT over the leaves' index-row subqueries, never null-valued.
func Group(c store.Cond, leaf func(store.Cond) (string, error), yes, no string) (string, error) {
	return group(c, leaf, yes, no, 1)
}

func group(c store.Cond, leaf func(store.Cond) (string, error), yes, no string, depth int) (string, error) {
	if depth > MaxGroupDepth {
		return "", fmt.Errorf("boolean groups nest too deep (at most %d)", MaxGroupDepth)
	}
	terms := make([]string, 0, len(c.Groups))
	for _, g := range c.Groups {
		parts := make([]string, 0, len(g))
		for _, sub := range g {
			var p string
			var err error
			if IsGroup(sub.Op) {
				p, err = group(sub, leaf, yes, no, depth+1)
			} else {
				p, err = leaf(sub)
			}
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		if len(parts) == 0 {
			parts = append(parts, yes)
		}
		terms = append(terms, "("+strings.Join(parts, " AND ")+")")
	}
	or := no
	if len(terms) > 0 {
		or = "(" + strings.Join(terms, " OR ") + ")"
	}
	if c.Op == store.OpNot {
		return "(NOT " + or + ")", nil
	}
	if c.Op != store.OpOr {
		return "", fmt.Errorf("unknown group operator %q", c.Op)
	}
	return or, nil
}

// UnderBounds splits an OpUnder value into the exact path and the prefix of its descendants: "/a" and "/a/",
// "/" and "/" (the trailing slash is dropped first, so "/a/" means "/a").
func UnderBounds(v string) (exact, prefix string) {
	base := strings.TrimRight(v, "/")
	if base == "" {
		return "/", "/"
	}
	return base, base + "/"
}

// Selective reports whether an operator usually matches few items.
func Selective(op store.Op) bool {
	return op == store.OpEq || op == store.OpIn || op == store.OpContains || op == store.OpPrefix || op == store.OpUnder
}

// WalkPhases are the statements an index-ordered query runs. Items with a value for the sort field come in
// value order (a list-valued field counts once, at its smallest value ascending or largest descending),
// then items without one in id order, in the direction of the sort.
type WalkPhases struct {
	// Ordered returns a page of the items that have a value.
	Ordered func(limit, offset int) ([]store.ItemSummary, error)
	// Missing returns a page of the matching items that have no value.
	Missing func(limit, offset int) ([]store.ItemSummary, error)
	// CountOrdered counts the matching items that have a value; it runs only when the page starts beyond them.
	CountOrdered func() (int, error)
}

// RunWalk assembles the page [q.Offset, q.Offset+q.Limit) of the two sequences. q must be normalised.
func RunWalk(q store.Query, p WalkPhases) ([]store.ItemSummary, error) {
	items, err := p.Ordered(q.Limit, q.Offset)
	if err != nil || len(items) == q.Limit {
		return items, err
	}
	// The ordered sequence ended inside or before this page.
	skip := 0
	if len(items) == 0 && q.Offset > 0 {
		n, err := p.CountOrdered()
		if err != nil {
			return nil, err
		}
		skip = max(q.Offset-n, 0)
	}
	more, err := p.Missing(q.Limit-len(items), skip)
	if err != nil {
		return nil, err
	}
	return append(items, more...), nil
}

// LiveTerms returns the scope terms that can match something: a term with an empty "in" list never does.
// Adapters drop the others, and answer a scoped query without terms with an empty result, because a constant
// false in the middle of a compiled index walk is not something every planner accepts.
func LiveTerms(terms []store.ScopeTerm) []store.ScopeTerm {
	out := make([]store.ScopeTerm, 0, len(terms))
terms:
	for _, t := range terms {
		for _, c := range t.Where {
			if l, err := List(c.Column, c.Value); c.Op == store.OpIn && err == nil && len(l) == 0 {
				continue terms
			}
		}
		out = append(out, t)
	}
	return out
}
