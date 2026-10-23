package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// compiler turns a store.Query into SQL. Values only ever travel as numbered bind parameters (?N), so the
// text of the statement depends on the shape of the query, never on its data.
type compiler struct {
	q    store.Query
	args []any
	loc  string // placeholder of q.Locale, bound on first use

	driver string // select of the item ids that drive the FROM clause, see itemConds

	searching bool // the FROM clause joins the search results as srch
}

func (c *compiler) bind(v any) string {
	c.args = append(c.args, v)
	return "?" + strconv.Itoa(len(c.args))
}

func (c *compiler) locale() string {
	if c.loc == "" {
		c.loc = c.bind(c.q.Locale)
	}
	return c.loc
}

// projection is the SQL literal of the field_index and search scope the query reads.
func (c *compiler) projection() string {
	if c.q.Working {
		return "'w'"
	}
	return "'p'"
}

// columnName maps a Column to its field_index column.
func columnName(col store.Column) (string, error) {
	switch col {
	case store.ColText, store.ColNum, store.ColTime, store.ColBool, store.ColRef:
		return "v_" + string(col), nil
	}
	return "", fmt.Errorf("unknown column %q", col)
}

// scalar converts one condition value to a bind value of the column's storage type.
func scalar(col store.Column, v any) (any, error) {
	x, err := storeutil.Scalar(col, v)
	if err != nil {
		return nil, err
	}
	switch t := x.(type) {
	case time.Time:
		return ms(t), nil
	case ids.UUID:
		return uid(t), nil
	}
	return x, nil
}

// list converts the Value of an OpIn condition.
func list(col store.Column, v any) ([]any, error) {
	items, err := storeutil.List(col, v)
	if err != nil {
		return nil, err
	}
	for i, x := range items {
		switch t := x.(type) {
		case time.Time:
			items[i] = ms(t)
		case ids.UUID:
			items[i] = uid(t)
		}
	}
	return items, nil
}

// globPrefix returns a GLOB pattern matching text that starts with prefix. GLOB is case-sensitive like the
// text columns' BINARY collation and can use their indexes, which LIKE (case-insensitive) cannot.
func globPrefix(prefix string) string {
	var b strings.Builder
	for _, r := range prefix {
		switch r {
		case '*', '?', '[':
			b.WriteByte('[')
			b.WriteRune(r)
			b.WriteByte(']')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('*')
	return b.String()
}

var compareOps = map[store.Op]string{
	store.OpEq: "=", store.OpLt: "<", store.OpLte: "<=", store.OpGt: ">", store.OpGte: ">=",
}

// cond compiles one field condition into a predicate on item. Positive conditions are semi-joins on the
// field's value index, negative ones probe the item's own rows.
func (c *compiler) cond(cd store.Cond, probe, drive bool) (string, error) {
	if storeutil.IsGroup(cd.Op) {
		return storeutil.Group(cd, func(l store.Cond) (string, error) { return c.cond(l, probe, false) }, "1", "0")
	}
	if !storeutil.ValidField(cd.Field) {
		return "", fmt.Errorf("invalid field name %q", cd.Field)
	}
	exists := "EXISTS"
	var pred string
	if cd.Op == store.OpExists {
		want, ok := cd.Value.(bool)
		if !ok {
			return "", fmt.Errorf("exists on %q needs a bool, got %T", cd.Field, cd.Value)
		}
		if !want {
			exists = "NOT EXISTS"
		}
		if cd.Column != "" {
			name, err := columnName(cd.Column)
			if err != nil {
				return "", err
			}
			pred = " AND fi." + name + " IS NOT NULL"
		}
	} else {
		name, err := columnName(cd.Column)
		if err != nil {
			return "", err
		}
		col := "fi." + name
		switch op := cd.Op; op {
		case store.OpEq, store.OpLt, store.OpLte, store.OpGt, store.OpGte, store.OpNe, store.OpContains:
			v, err := scalar(cd.Column, cd.Value)
			if err != nil {
				return "", fmt.Errorf("field %q: %w", cd.Field, err)
			}
			sym := compareOps[op]
			switch op {
			case store.OpNe:
				exists, sym = "NOT EXISTS", "="
			case store.OpContains:
				sym = "="
			}
			pred = " AND " + col + " " + sym + " " + c.bind(v)
		case store.OpIn:
			vals, err := list(cd.Column, cd.Value)
			if err != nil {
				return "", fmt.Errorf("field %q: %w", cd.Field, err)
			}
			if len(vals) == 0 {
				return "0", nil
			}
			ph := make([]string, len(vals))
			for i, v := range vals {
				ph[i] = c.bind(v)
			}
			pred = " AND " + col + " IN (" + strings.Join(ph, ", ") + ")"
		case store.OpPrefix:
			s, ok := cd.Value.(string)
			if cd.Column != store.ColText || !ok {
				return "", fmt.Errorf("prefix on %q needs a text column and a string", cd.Field)
			}
			pred = " AND " + col + " GLOB " + c.bind(globPrefix(s))
		case store.OpUnder:
			s, ok := cd.Value.(string)
			if cd.Column != store.ColText || !ok {
				return "", fmt.Errorf("under on %q needs a text column and a string", cd.Field)
			}
			exact, prefix := storeutil.UnderBounds(s)
			pred = " AND (" + col + " = " + c.bind(exact) + " OR " + col + " GLOB " + c.bind(globPrefix(prefix)) + ")"
		default:
			return "", fmt.Errorf("unknown operator %q", cd.Op)
		}
	}
	field, loc := c.bind(cd.Field), c.locale()
	if exists == "EXISTS" && !probe {
		// A semi-join: SQLite builds the matching item ids once from the value index (a plain EXISTS would be
		// planned as one probe per item, and without table statistics it picks a range scan per probe).
		set := "SELECT fi.item_id FROM field_index fi WHERE fi.scope = " + c.projection() + " AND fi.field = " + field +
			" AND fi.locale IN ('', " + loc + ")" + pred
		if drive {
			return set, nil
		}
		return "item.id IN (" + set + ")", nil
	}
	return exists + " (SELECT 1 FROM field_index fi INDEXED BY field_index_item WHERE fi.item_id = item.id AND fi.scope = " +
		c.projection() + " AND fi.field = " + field + " AND fi.locale IN ('', " + loc + ")" + pred + ")", nil
}

// match builds an FTS5 query in which every word is a quoted prefix term.
func match(terms []string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = `"` + t + `"*`
	}
	return strings.Join(parts, " ")
}

// available compiles Query.Available: one probe of the snapshot primary key (item_id, locale) per row.
func (c *compiler) available() string {
	pub := "item.published_version > 0 AND "
	if len(c.q.Available) == 0 {
		return pub + "item.always_available = 1"
	}
	ph := make([]string, len(c.q.Available))
	for i, l := range c.q.Available {
		ph[i] = c.bind(l)
	}
	return "(" + pub + "(item.always_available = 1 OR EXISTS (SELECT 1 FROM published_snapshot ps WHERE ps.item_id = item.id AND ps.locale IN (" +
		strings.Join(ph, ", ") + "))))"
}

// scope compiles Query.Scope: an OR of terms, each an AND of its creator list and conditions.
func (c *compiler) scope(probes bool) (string, error) {
	for _, t := range c.q.Scope {
		if len(t.CreatedBy) == 0 && len(t.Where) == 0 {
			return "1", nil // an unrestricted term admits everything; bind nothing for the others
		}
	}
	terms := make([]string, 0, len(c.q.Scope))
	for _, t := range c.q.Scope {
		var parts []string
		if len(t.CreatedBy) > 0 {
			ph := make([]string, len(t.CreatedBy))
			for i, by := range t.CreatedBy {
				ph[i] = c.bind(by)
			}
			parts = append(parts, "item.created_by IN ("+strings.Join(ph, ", ")+")")
		}
		for _, cd := range t.Where {
			p, err := c.cond(cd, probes, false)
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		terms = append(terms, "("+strings.Join(parts, " AND ")+")")
	}
	if len(terms) == 0 {
		return "0", nil
	}
	return "(" + strings.Join(terms, " OR ") + ")", nil
}

// itemConds compiles the item-level and field conditions (no search) into predicates on item.
//
// A positive field condition is either a semi-join (the matching item ids are built once from the value
// index) or a probe of the item's own rows. One semi-join may drive the query, the most selective looking
// one (equality before ranges); the rest are probes, since each extra set would be built in full. With
// probes set (index walks stop early, so building sets costs more than it saves) all are probes.
func (c *compiler) itemConds(probes bool) ([]string, error) {
	q := c.q
	var conds []string
	if q.Type != "" {
		conds = append(conds, "item.type = "+c.bind(q.Type))
	}
	if len(q.Types) > 0 {
		marks := make([]string, len(q.Types))
		for i, t := range q.Types {
			marks[i] = c.bind(t)
		}
		conds = append(conds, "item.type IN ("+strings.Join(marks, ",")+")")
	}
	switch q.Trashed {
	case store.TrashedExclude:
		conds = append(conds, "item.deleted_at IS NULL")
	case store.TrashedOnly:
		conds = append(conds, "item.deleted_at IS NOT NULL")
	case store.TrashedInclude:
	default:
		return nil, fmt.Errorf("unknown trashed filter %d", q.Trashed)
	}
	switch q.Status {
	case store.StatusAny:
	case store.StatusPublished:
		conds = append(conds, "item.published_version > 0")
	case store.StatusDraft:
		conds = append(conds, "EXISTS (SELECT 1 FROM draft_part dp WHERE dp.item_id = item.id)")
	default:
		return nil, fmt.Errorf("unknown status %d", q.Status)
	}
	if q.Available != nil {
		conds = append(conds, c.available())
	}
	if q.Scoped {
		p, err := c.scope(probes)
		if err != nil {
			return nil, err
		}
		conds = append(conds, p)
	}
	driver := -1
	if !probes {
		for i, cd := range q.Where {
			switch cd.Op {
			case store.OpNe, store.OpExists, store.OpOr, store.OpNot:
			case store.OpEq, store.OpIn, store.OpContains, store.OpPrefix, store.OpUnder:
				if driver < 0 || !storeutil.Selective(q.Where[driver].Op) {
					driver = i
				}
			default:
				if driver < 0 {
					driver = i
				}
			}
		}
	}
	if driver < 0 && !probes {
		for _, cd := range q.Where {
			if cd.Op != store.OpOr {
				continue
			}
			set, ok, err := c.orDriver(cd)
			if err != nil {
				return nil, err
			}
			if ok {
				c.driver = set
				break
			}
		}
	}
	for i, cd := range q.Where {
		// A selective driver becomes the outer table of the join: SQLite would otherwise scan every item of
		// the type and test membership. Groups probe the rows a driver leaves; without one they build sets.
		drive := i == driver && storeutil.Selective(cd.Op) && cd.Op != store.OpExists
		probe := i != driver
		if storeutil.IsGroup(cd.Op) {
			probe = probes || driver >= 0 || c.driver != ""
		}
		p, err := c.cond(cd, probe, drive)
		if err != nil {
			return nil, err
		}
		if drive && p != "0" {
			c.driver = p
			continue
		}
		conds = append(conds, p)
	}
	return conds, nil
}

// orDriver builds the item ids that can satisfy an or group when each of its groups has a selective
// condition: the union of those conditions' value-index sets. The group's predicate still filters the rows.
func (c *compiler) orDriver(or store.Cond) (string, bool, error) {
	sets := make([]string, 0, len(or.Groups))
	for _, g := range or.Groups {
		i := slices.IndexFunc(g, func(l store.Cond) bool { return storeutil.Selective(l.Op) })
		if i < 0 {
			return "", false, nil
		}
		set, err := c.cond(g[i], false, true)
		if err != nil {
			return "", false, err
		}
		if set == "0" {
			return "", false, nil
		}
		sets = append(sets, set)
	}
	if len(sets) == 0 {
		return "", false, nil
	}
	return strings.Join(sets, " UNION "), true, nil
}

// itemFrom is the FROM clause the conditions of itemConds expect.
func (c *compiler) itemFrom() string {
	if c.driver == "" {
		return "item"
	}
	return "(SELECT DISTINCT * FROM (" + c.driver + ")) drv CROSS JOIN item ON item.id = drv.item_id"
}

// filter compiles everything before ORDER BY: the FROM clause (with the search join) and the WHERE clause.
func (c *compiler) filter() (from, where string, err error) {
	q := c.q
	conds, err := c.itemConds(false)
	if err != nil {
		return "", "", err
	}
	from = c.itemFrom()
	if terms := storeutil.SearchTerms(q.Search); len(terms) > 0 {
		c.searching = true
		from += " JOIN " + c.searchJoin(terms) + " srch ON srch.item_id = item.id"
	} else if strings.TrimSpace(q.Search) != "" {
		conds = append(conds, "0") // only punctuation: nothing can match
	}
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return from, where, nil
}

// searchLocale is the predicate on a content_search_doc row sd (joined to item it) that picks the documents a
// query may match: the shared and the query locale's, and with SearchFallback the main-locale document of an
// always available item that has none in the locale.
func (c *compiler) searchLocale() string {
	loc := c.locale()
	p := "sd.locale IN ('', " + loc + ")"
	if c.q.SearchFallback {
		p = "(" + p + " OR (it.always_available = 1 AND sd.locale = it.main_locale AND NOT EXISTS (" +
			"SELECT 1 FROM content_search_doc o WHERE o.item_id = sd.item_id AND o.scope = sd.scope AND o.locale = " + loc + ")))"
	}
	return p
}

// searchJoin is the subquery of (item_id, rank) for the items matching the search terms.
// The rank column is bm25 with default weights (lower is better). The bm25() function itself cannot be used
// through this join, and the inner select keeps the aggregate away from the FTS cursor.
func (c *compiler) searchJoin(terms []string) string {
	return `(
		SELECT item_id, MIN(rank) AS rank FROM (
			SELECT sd.item_id AS item_id, content_search.rank AS rank
			FROM content_search JOIN content_search_doc sd ON sd.id = content_search.rowid JOIN item it ON it.id = sd.item_id
			WHERE content_search MATCH ` + c.bind(match(terms)) + ` AND sd.scope = ` + c.projection() + ` AND ` + c.searchLocale() + `)
		GROUP BY item_id)`
}

func (c *compiler) nameExpr() string {
	loc := c.locale()
	pub := `(SELECT NULLIF(vl.name, '') FROM item_version_locale vl WHERE vl.item_id = item.id
		AND vl.version = item.published_version AND vl.locale = `
	draft := `(SELECT NULLIF(dp.name, '') FROM draft_part dp WHERE dp.item_id = item.id AND dp.part = `
	if c.q.Working {
		return "COALESCE(" + draft + loc + "), " + pub + loc + "), " + draft + "item.main_locale), " + pub + "item.main_locale), '')"
	}
	return "COALESCE(" + pub + loc + "), " + pub + "item.main_locale), " + draft + loc + "), " + draft + "item.main_locale), '')"
}

func (c *compiler) sortKey(s store.Sort) (string, error) {
	dir := " ASC"
	if s.Desc {
		dir = " DESC"
	}
	if s.Column == "" {
		switch s.Field {
		case "name":
			return c.nameExpr() + dir, nil
		case "createdAt":
			return "item.created_at" + dir, nil
		case "updatedAt":
			return "item.updated_at" + dir, nil
		case "publishedAt":
			return "(SELECT MAX(ps.published_at) FROM published_snapshot ps WHERE ps.item_id = item.id)" + dir + " NULLS LAST", nil
		case "deletedAt":
			return "item.deleted_at" + dir + " NULLS LAST", nil
		case "key":
			return "item.key" + dir + " NULLS LAST", nil
		}
		return "", fmt.Errorf("unknown meta sort field %q", s.Field)
	}
	if !storeutil.ValidField(s.Field) {
		return "", fmt.Errorf("invalid field name %q", s.Field)
	}
	name, err := columnName(s.Column)
	if err != nil {
		return "", err
	}
	// An item may have several rows for a field (list values, shared plus locale): order by its first or last.
	return "(SELECT fi." + name + " FROM field_index fi INDEXED BY field_index_item WHERE fi.item_id = item.id AND fi.scope = " + c.projection() +
		" AND fi.field = " + c.bind(s.Field) + " AND fi.locale IN ('', " + c.locale() + ") AND fi." + name + " IS NOT NULL ORDER BY fi." + name + dir +
		" LIMIT 1)" + dir + " NULLS LAST", nil
}

// order compiles ORDER BY. The id tiebreak runs in the direction of the last sort key, so the default
// order (updated_at DESC, id DESC) is one backward scan of item_type_updated.
func (c *compiler) order() (string, error) {
	q := c.q
	if len(q.Sort) == 0 {
		if c.searching {
			return " ORDER BY srch.rank, item.id", nil
		}
		return " ORDER BY item.updated_at DESC, item.id DESC", nil
	}
	keys := make([]string, 0, len(q.Sort)+1)
	for _, s := range q.Sort {
		k, err := c.sortKey(s)
		if err != nil {
			return "", err
		}
		keys = append(keys, k)
	}
	if q.Sort[len(q.Sort)-1].Desc {
		keys = append(keys, "item.id DESC")
	} else {
		keys = append(keys, "item.id")
	}
	return " ORDER BY " + strings.Join(keys, ", "), nil
}

// selectSQL compiles the page query; the last selected column is the search rank (0 without a search).
func selectSQL(q store.Query) (text string, args []any, err error) {
	c := &compiler{q: q}
	from, where, err := c.filter()
	if err != nil {
		return "", nil, err
	}
	order, err := c.order()
	if err != nil {
		return "", nil, err
	}
	rank := "0"
	if c.searching {
		rank = "-srch.rank"
	}
	text = "SELECT " + itemColumns + ", " + rank + " FROM " + from + where + order +
		" LIMIT " + c.bind(q.Limit) + " OFFSET " + c.bind(q.Offset)
	return text, c.args, nil
}

// fillSnippets sets Snippet on a page of a searched query: snippet() only works on the FTS cursor, so the
// page's matching rows are read again, best rank first, and each item keeps its best one.
func fillSnippets(ctx context.Context, db queryer, q store.Query, items []store.ItemSummary) error {
	terms := storeutil.SearchTerms(q.Search)
	if !q.Highlight || len(terms) == 0 || len(items) == 0 {
		return nil
	}
	c := &compiler{q: q}
	marks := make([]string, len(items))
	for i, it := range items {
		marks[i] = c.bind(uid(it.ID))
	}
	text := `SELECT sd.item_id, snippet(content_search, 0, char(2), char(3), '…', 20)
		FROM content_search JOIN content_search_doc sd ON sd.id = content_search.rowid JOIN item it ON it.id = sd.item_id
		WHERE content_search MATCH ` + c.bind(match(terms)) + ` AND sd.scope = ` + c.projection() +
		` AND ` + c.searchLocale() + ` AND sd.item_id IN (` + strings.Join(marks, ",") + `)
		ORDER BY content_search.rank`
	rows, err := db.QueryContext(ctx, text, c.args...)
	if err != nil {
		return err
	}
	best := map[ids.UUID]string{}
	for rows.Next() {
		var id []byte
		var snip string
		if err := rows.Scan(&id, &snip); err != nil {
			_ = rows.Close()
			return err
		}
		k := ids.UUID(id)
		if _, ok := best[k]; !ok {
			best[k] = snip
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for i := range items {
		items[i].Snippet = best[items[i].ID]
	}
	return nil
}

// queryer is what the page queries need of a connection: a transaction or a pool.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s contentStore) Query(ctx context.Context, q store.Query) (store.QueryResult, error) {
	return runQuery(ctx, s.tx, q)
}

func (s contentStore) Facet(ctx context.Context, q store.Query, field string, limit int) ([]store.FacetCount, error) {
	return runFacet(ctx, s.tx, q, field, limit)
}

func runFacet(ctx context.Context, db queryer, q store.Query, field string, limit int) ([]store.FacetCount, error) {
	if !storeutil.ValidField(field) {
		return nil, fmt.Errorf("invalid field name %q", field)
	}
	q = storeutil.Normalize(q)
	if q.Scoped {
		if q.Scope = storeutil.LiveTerms(q.Scope); len(q.Scope) == 0 {
			return nil, nil
		}
	}
	c := &compiler{q: q}
	conds, err := c.itemConds(true)
	if err != nil {
		return nil, err
	}
	if terms := storeutil.SearchTerms(q.Search); len(terms) > 0 {
		conds = append(conds, "item.id IN (SELECT item_id FROM "+c.searchJoin(terms)+")")
	} else if strings.TrimSpace(q.Search) != "" {
		return nil, nil
	}
	conds = append([]string{"fi.scope = " + c.projection(), "fi.field = " + c.bind(field),
		"fi.locale IN ('', " + c.locale() + ")", "fi.v_text IS NOT NULL"}, conds...)
	text := "SELECT fi.v_text, COUNT(DISTINCT fi.item_id) FROM field_index fi CROSS JOIN item ON item.id = fi.item_id WHERE " +
		strings.Join(conds, " AND ") + " GROUP BY fi.v_text ORDER BY 2 DESC, 1 LIMIT " + c.bind(max(limit, 1))
	rows, err := db.QueryContext(ctx, text, c.args...)
	if err != nil {
		return nil, fmt.Errorf("sqlite: facet %q: %w", field, err)
	}
	defer rows.Close()
	var out []store.FacetCount
	for rows.Next() {
		var f store.FacetCount
		if err := rows.Scan(&f.Value, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// walkDir is the index walk's direction keyword.
func walkDir(s store.Sort) string {
	if s.Desc {
		return "DESC"
	}
	return "ASC"
}

// walkSQL compiles the ordered phase of an index-ordered query: the sort field's value index in value
// order, joined to item for the other conditions. A row counts only if it is the item's first (ascending)
// or last (descending) value, so list fields yield each item once, exactly where the sort key puts it.
func (c *compiler) walkSQL(limit, offset int) (string, error) {
	s := c.q.Sort[0]
	if !storeutil.ValidField(s.Field) {
		return "", fmt.Errorf("invalid field name %q", s.Field)
	}
	name, err := columnName(s.Column)
	if err != nil {
		return "", err
	}
	conds, err := c.itemConds(true)
	if err != nil {
		return "", err
	}
	field, loc, dir := c.bind(s.Field), c.locale(), walkDir(s)
	has := func(a string) string {
		return a + ".scope = " + c.projection() + " AND " + a + ".field = " + field + " AND " + a + ".locale IN ('', " + loc + ") AND " + a + "." + name + " IS NOT NULL"
	}
	// The dedupe runs last so that it only probes rows that passed every other condition.
	conds = append(conds, "fi.rowid = (SELECT f2.rowid FROM field_index f2 INDEXED BY field_index_item WHERE f2.item_id = fi.item_id AND "+
		has("f2")+" ORDER BY f2."+name+" "+dir+", f2.rowid LIMIT 1)")
	where := has("fi") + " AND " + strings.Join(conds, " AND ")
	return "SELECT " + itemColumns + ", 0 FROM field_index fi INDEXED BY field_index_" + string(s.Column) +
		" CROSS JOIN item ON item.id = fi.item_id WHERE " + where +
		" ORDER BY fi." + name + " " + dir + ", fi.item_id " + dir +
		" LIMIT " + c.bind(limit) + " OFFSET " + c.bind(offset), nil
}

// missingSQL compiles the second phase: the matching items without a value for the sort field, in id order.
func (c *compiler) missingSQL(limit, offset int) (string, error) {
	s := c.q.Sort[0]
	if !storeutil.ValidField(s.Field) {
		return "", fmt.Errorf("invalid field name %q", s.Field)
	}
	name, err := columnName(s.Column)
	if err != nil {
		return "", err
	}
	conds, err := c.itemConds(false)
	if err != nil {
		return "", err
	}
	conds = append(conds, "NOT EXISTS (SELECT 1 FROM field_index fi INDEXED BY field_index_item WHERE fi.item_id = item.id AND fi.scope = "+c.projection()+" AND fi.field = "+
		c.bind(s.Field)+" AND fi.locale IN ('', "+c.locale()+") AND fi."+name+" IS NOT NULL)")
	return "SELECT " + itemColumns + ", 0 FROM " + c.itemFrom() + " WHERE " + strings.Join(conds, " AND ") +
		" ORDER BY item.id " + walkDir(s) + " LIMIT " + c.bind(limit) + " OFFSET " + c.bind(offset), nil
}

// countWalkSQL counts the matching items that have a value for the sort field.
func (c *compiler) countWalkSQL() (string, error) {
	s := c.q.Sort[0]
	name, err := columnName(s.Column)
	if err != nil {
		return "", err
	}
	conds, err := c.itemConds(false)
	if err != nil {
		return "", err
	}
	conds = append(conds, "item.id IN (SELECT fi.item_id FROM field_index fi WHERE fi.scope = "+c.projection()+" AND fi.field = "+c.bind(s.Field)+
		" AND fi.locale IN ('', "+c.locale()+") AND fi."+name+" IS NOT NULL)")
	return "SELECT COUNT(*) FROM " + c.itemFrom() + " WHERE " + strings.Join(conds, " AND "), nil
}

func runQuery(ctx context.Context, db queryer, q store.Query) (store.QueryResult, error) {
	q = storeutil.Normalize(q)
	var res store.QueryResult
	if q.Scoped {
		if q.Scope = storeutil.LiveTerms(q.Scope); len(q.Scope) == 0 {
			return res, nil
		}
	}
	var err error
	if storeutil.IndexWalk(q) {
		res.Items, err = storeutil.RunWalk(q, storeutil.WalkPhases{
			Ordered: func(limit, offset int) ([]store.ItemSummary, error) {
				c := &compiler{q: q}
				text, err := c.walkSQL(limit, offset)
				if err != nil {
					return nil, err
				}
				return pageRows(ctx, db, text, c.args)
			},
			Missing: func(limit, offset int) ([]store.ItemSummary, error) {
				c := &compiler{q: q}
				text, err := c.missingSQL(limit, offset)
				if err != nil {
					return nil, err
				}
				return pageRows(ctx, db, text, c.args)
			},
			CountOrdered: func() (n int, err error) {
				c := &compiler{q: q}
				text, err := c.countWalkSQL()
				if err != nil {
					return 0, err
				}
				err = db.QueryRowContext(ctx, text, c.args...).Scan(&n)
				return n, err
			},
		})
	} else {
		var sqlText string
		var args []any
		if sqlText, args, err = selectSQL(q); err == nil {
			res.Items, err = pageRows(ctx, db, sqlText, args)
		}
	}
	if err != nil {
		return store.QueryResult{}, fmt.Errorf("sqlite: query: %w", err)
	}
	if err := fillSnippets(ctx, db, q, res.Items); err != nil {
		return store.QueryResult{}, fmt.Errorf("sqlite: query: %w", err)
	}
	if err := decorate(ctx, db, q, res.Items); err != nil {
		return store.QueryResult{}, err
	}
	if q.NoTotal {
		res.Total = q.Offset + len(res.Items)
		return res, nil
	}
	if res.Total, err = total(ctx, db, q, len(res.Items)); err != nil {
		return store.QueryResult{}, err
	}
	return res, nil
}

// pageRows runs a page statement whose last column is the search rank.
func pageRows(ctx context.Context, db queryer, text string, args []any) ([]store.ItemSummary, error) {
	rows, err := db.QueryContext(ctx, text, args...)
	if err != nil {
		return nil, err
	}
	var out []store.ItemSummary
	for rows.Next() {
		var searched float64
		it, err := scanItem(rows, &searched)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, store.ItemSummary{Item: it, Searched: searched})
	}
	return out, errors.Join(rows.Err(), rows.Close())
}

// total counts the matches. A short page answers it without a query; otherwise a separate COUNT(*) is used
// rather than COUNT(*) OVER (), because the window function would force the engine to produce every matching
// row before LIMIT, defeating index-ordered early termination of the main query.
func total(ctx context.Context, db queryer, q store.Query, got int) (int, error) {
	if got > 0 && got < q.Limit {
		return q.Offset + got, nil
	}
	if got == 0 && q.Offset == 0 {
		return 0, nil
	}
	c := &compiler{q: q}
	from, where, err := c.filter()
	if err != nil {
		return 0, fmt.Errorf("sqlite: query: %w", err)
	}
	var n int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+from+where, c.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: query count: %w", err)
	}
	return n, nil
}

// decorate fills Name, Drafts and Locales for a page of items with two set-based queries.
func decorate(ctx context.Context, db queryer, q store.Query, items []store.ItemSummary) error {
	if len(items) == 0 {
		return nil
	}
	d := storeutil.NewDecorator(q.Locale, items)
	d.Working = q.Working
	idList := d.IDs()
	in := placeholders(len(items))

	rows, err := db.QueryContext(ctx, `SELECT vl.item_id, vl.locale, vl.name FROM item_version_locale vl
		JOIN item ON item.id = vl.item_id AND item.published_version = vl.version
		WHERE vl.item_id IN (`+in+`)`, idArgs(idList)...)
	if err != nil {
		return fmt.Errorf("sqlite: query locales: %w", err)
	}
	for rows.Next() {
		var b []byte
		var loc, name string
		if err := rows.Scan(&b, &loc, &name); err != nil {
			_ = rows.Close()
			return fmt.Errorf("sqlite: query locales: %w", err)
		}
		id, err := toUUID(b)
		if err != nil {
			_ = rows.Close()
			return err
		}
		d.Published(id, loc, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("sqlite: query locales: %w", err)
	}

	rows, err = db.QueryContext(ctx, `SELECT item_id, part, name FROM draft_part WHERE item_id IN (`+in+`)`, idArgs(idList)...)
	if err != nil {
		return fmt.Errorf("sqlite: query drafts: %w", err)
	}
	for rows.Next() {
		var b []byte
		var part, name string
		if err := rows.Scan(&b, &part, &name); err != nil {
			_ = rows.Close()
			return fmt.Errorf("sqlite: query drafts: %w", err)
		}
		id, err := toUUID(b)
		if err != nil {
			_ = rows.Close()
			return err
		}
		d.Draft(id, part, name)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("sqlite: query drafts: %w", err)
	}
	d.Finish()
	return nil
}
