package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

// compiler turns a store.Query into SQL. Values only ever travel as numbered bind parameters, so the text of
// the statement depends on the shape of the query, never on its data (which keeps statements cacheable).
type compiler struct {
	q    store.Query
	args []any
	loc  string // placeholder of q.Locale, bound on first use

	searching bool // the FROM clause joins the search results as srch
}

func (c *compiler) bind(v any) string {
	c.args = append(c.args, v)
	return "$" + strconv.Itoa(len(c.args))
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

// columnInfo maps a Column to its field_index column and the array type used to bind OpIn lists.
func columnInfo(col store.Column) (name, arrayType string, err error) {
	switch col {
	case store.ColText:
		return "v_text", "text[]", nil
	case store.ColNum:
		return "v_num", "float8[]", nil
	case store.ColTime:
		return "v_time", "timestamptz[]", nil
	case store.ColBool:
		return "v_bool", "bool[]", nil
	case store.ColRef:
		return "v_ref", "uuid[]", nil
	}
	return "", "", fmt.Errorf("unknown column %q", col)
}

// scalar converts one condition value to a bind value of the column's type.
func scalar(col store.Column, v any) (any, error) {
	x, err := storeutil.Scalar(col, v)
	if err != nil {
		return nil, err
	}
	switch t := x.(type) {
	case time.Time:
		return tz(t), nil
	case ids.UUID:
		return uid(t), nil
	}
	return x, nil
}

// list converts the Value of an OpIn condition into a typed slice for = ANY($n).
func list(col store.Column, v any) (any, int, error) {
	items, err := storeutil.List(col, v)
	if err != nil {
		return nil, 0, err
	}
	vals := make([]any, len(items))
	for i, x := range items {
		if vals[i], err = scalar(col, x); err != nil {
			return nil, 0, err
		}
	}
	return typed(col, vals), len(vals), nil
}

// typed gathers converted values into the slice type pgx maps to the column's array type.
func typed(col store.Column, vals []any) any {
	switch col {
	case store.ColText:
		return collect[string](vals)
	case store.ColNum:
		return collect[float64](vals)
	case store.ColTime:
		return collect[time.Time](vals)
	case store.ColBool:
		return collect[bool](vals)
	}
	return collect[[16]byte](vals)
}

func collect[T any](vals []any) []T {
	out := make([]T, len(vals))
	for i, v := range vals {
		out[i] = v.(T)
	}
	return out
}

// likePrefix returns a LIKE pattern (escape character \) matching text that starts with prefix.
func likePrefix(prefix string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix) + "%"
}

var compareOps = map[store.Op]string{
	store.OpEq: "=", store.OpLt: "<", store.OpLte: "<=", store.OpGt: ">", store.OpGte: ">=",
}

// cond compiles one field condition into a predicate on item.
func (c *compiler) cond(cd store.Cond) (string, error) {
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
			name, _, err := columnInfo(cd.Column)
			if err != nil {
				return "", err
			}
			pred = " AND fi." + name + " IS NOT NULL"
		}
	} else {
		name, arrayType, err := columnInfo(cd.Column)
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
			vals, n, err := list(cd.Column, cd.Value)
			if err != nil {
				return "", fmt.Errorf("field %q: %w", cd.Field, err)
			}
			if n == 0 {
				return "false", nil
			}
			pred = " AND " + col + " = ANY(" + c.bind(vals) + "::" + arrayType + ")"
		case store.OpPrefix:
			s, ok := cd.Value.(string)
			if cd.Column != store.ColText || !ok {
				return "", fmt.Errorf("prefix on %q needs a text column and a string", cd.Field)
			}
			pred = " AND " + col + ` LIKE ` + c.bind(likePrefix(s)) + ` ESCAPE '\'`
		case store.OpUnder:
			s, ok := cd.Value.(string)
			if cd.Column != store.ColText || !ok {
				return "", fmt.Errorf("under on %q needs a text column and a string", cd.Field)
			}
			exact, prefix := storeutil.UnderBounds(s)
			pred = " AND (" + col + " = " + c.bind(exact) + " OR " + col + ` LIKE ` + c.bind(likePrefix(prefix)) + ` ESCAPE '\')`
		default:
			return "", fmt.Errorf("unknown operator %q", cd.Op)
		}
	}
	return exists + " (SELECT 1 FROM field_index fi WHERE fi.item_id = item.id AND fi.scope = " + c.projection() + " AND fi.field = " + c.bind(cd.Field) +
		" AND fi.locale IN ('', " + c.locale() + ")" + pred + ")", nil
}

// tsquery builds a query in which every word is a required prefix term.
func tsquery(terms []string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = "'" + t + "':*"
	}
	return strings.Join(parts, " & ")
}

// available compiles Query.Available: one probe of the snapshot primary key (item_id, locale) per row.
func (c *compiler) available() string {
	pub := "item.published_version > 0 AND "
	if len(c.q.Available) == 0 {
		return pub + "item.always_available"
	}
	return "(" + pub + "(item.always_available OR EXISTS (SELECT 1 FROM published_snapshot ps WHERE ps.item_id = item.id AND ps.locale = ANY(" +
		c.bind(c.q.Available) + "::text[]))))"
}

// scope compiles Query.Scope: an OR of terms, each an AND of its creator list and conditions.
func (c *compiler) scope() (string, error) {
	for _, t := range c.q.Scope {
		if len(t.CreatedBy) == 0 && len(t.Where) == 0 {
			return "true", nil // an unrestricted term admits everything; bind nothing for the others
		}
	}
	terms := make([]string, 0, len(c.q.Scope))
	for _, t := range c.q.Scope {
		var parts []string
		if len(t.CreatedBy) > 0 {
			parts = append(parts, "item.created_by = ANY("+c.bind(t.CreatedBy)+"::text[])")
		}
		for _, cd := range t.Where {
			p, err := c.cond(cd)
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		terms = append(terms, "("+strings.Join(parts, " AND ")+")")
	}
	if len(terms) == 0 {
		return "false", nil
	}
	return "(" + strings.Join(terms, " OR ") + ")", nil
}

// itemConds compiles the item-level and field conditions (no search) into predicates on item.
func (c *compiler) itemConds() ([]string, error) {
	q := c.q
	var conds []string
	if q.Type != "" {
		conds = append(conds, "item.type = "+c.bind(q.Type))
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
		p, err := c.scope()
		if err != nil {
			return nil, err
		}
		conds = append(conds, p)
	}
	for _, cd := range q.Where {
		p, err := c.cond(cd)
		if err != nil {
			return nil, err
		}
		conds = append(conds, p)
	}
	return conds, nil
}

// filter compiles everything before ORDER BY: the FROM clause (with the search join) and the WHERE clause.
func (c *compiler) filter() (from, where string, err error) {
	q := c.q
	from = "item"
	conds, err := c.itemConds()
	if err != nil {
		return "", "", err
	}
	if terms := storeutil.SearchTerms(q.Search); len(terms) > 0 {
		c.searching = true
		from += ` JOIN (
		SELECT cs.item_id, MAX(ts_rank(cs.tsv, query.q))::float8 AS rank
		FROM content_search cs, to_tsquery('simple', ` + c.bind(tsquery(terms)) + `) AS query(q)
		WHERE cs.scope = ` + c.projection() + ` AND cs.tsv @@ query.q AND cs.locale IN ('', ` + c.locale() + `)
		GROUP BY cs.item_id) srch ON srch.item_id = item.id`
	} else if strings.TrimSpace(q.Search) != "" {
		conds = append(conds, "false") // only punctuation: nothing can match
	}
	if len(conds) > 0 {
		where = " WHERE " + strings.Join(conds, " AND ")
	}
	return from, where, nil
}

func (c *compiler) nameExpr() string {
	loc := c.locale()
	pub := `(SELECT NULLIF(vl.name, '') FROM item_version_locale vl WHERE vl.item_id = item.id
		AND vl.version = item.published_version AND vl.locale = `
	draft := `(SELECT NULLIF(dp.name, '') FROM draft_part dp WHERE dp.item_id = item.id AND dp.part = `
	if c.q.Working {
		return "(COALESCE(" + draft + loc + "), " + pub + loc + "), " + draft + "item.main_locale), " +
			pub + "item.main_locale), '') COLLATE \"C\")"
	}
	return "(COALESCE(" + pub + loc + "), " + pub + "item.main_locale), " + draft + loc + "), " +
		draft + "item.main_locale), '') COLLATE \"C\")"
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
	name, _, err := columnInfo(s.Column)
	if err != nil {
		return "", err
	}
	// An item may have several rows for a field (list values, shared plus locale): order by its first or last.
	return "(SELECT fi." + name + " FROM field_index fi WHERE fi.item_id = item.id AND fi.scope = " + c.projection() + " AND fi.field = " + c.bind(s.Field) +
		" AND fi.locale IN ('', " + c.locale() + ") AND fi." + name + " IS NOT NULL ORDER BY fi." + name + dir +
		" LIMIT 1)" + dir + " NULLS LAST", nil
}

// order compiles ORDER BY. The id tiebreak runs in the direction of the last sort key, so the default
// order (updated_at DESC, id DESC) is one backward scan of item_type_updated.
func (c *compiler) order() (string, error) {
	q := c.q
	if len(q.Sort) == 0 {
		if c.searching {
			return " ORDER BY srch.rank DESC, item.id", nil
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
	rank := "0::float8"
	if c.searching {
		rank = "srch.rank"
	}
	text = "SELECT " + itemColumns + ", " + rank + " FROM " + from + where + order +
		" LIMIT " + c.bind(q.Limit) + " OFFSET " + c.bind(q.Offset)
	return text, c.args, nil
}

// queryer is what the page queries need of a connection: a transaction or a pool.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s contentStore) Query(ctx context.Context, q store.Query) (store.QueryResult, error) {
	return runQuery(ctx, s.tx, q)
}

func (s contentStore) Facet(ctx context.Context, q store.Query, field string, limit int) ([]store.FacetCount, error) {
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
	conds, err := c.itemConds()
	if err != nil {
		return nil, err
	}
	conds = append([]string{"fi.scope = " + c.projection(), "fi.field = " + c.bind(field),
		"fi.locale IN ('', " + c.locale() + ")", "fi.v_text IS NOT NULL"}, conds...)
	text := "SELECT fi.v_text, COUNT(DISTINCT fi.item_id) FROM field_index fi JOIN item ON item.id = fi.item_id WHERE " +
		strings.Join(conds, " AND ") + ` GROUP BY fi.v_text ORDER BY 2 DESC, fi.v_text COLLATE "C" LIMIT ` + c.bind(max(limit, 1))
	rows, err := s.tx.Query(ctx, text, c.args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: facet %q: %w", field, err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.FacetCount, error) {
		var f store.FacetCount
		var n int64
		err := r.Scan(&f.Value, &n)
		f.Count = int(n)
		return f, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: facet %q: %w", field, err)
	}
	return out, nil
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
	name, _, err := columnInfo(s.Column)
	if err != nil {
		return "", err
	}
	conds, err := c.itemConds()
	if err != nil {
		return "", err
	}
	field, loc, dir := c.bind(s.Field), c.locale(), walkDir(s)
	has := func(a string) string {
		return a + ".scope = " + c.projection() + " AND " + a + ".field = " + field + " AND " + a + ".locale IN ('', " + loc + ") AND " + a + "." + name + " IS NOT NULL"
	}
	// The dedupe runs last so that it only probes rows that passed every other condition. It compares
	// (value, ctid) with the item's extreme one by an inequality: the planner then guesses a third of the rows
	// pass, where an equality would look so selective that it abandons the ordered index scan for a full read.
	cmp, tie := "<=", "ASC"
	if c.q.Sort[0].Desc {
		cmp, tie = ">=", "DESC"
	}
	conds = append(conds, "(fi."+name+", fi.ctid) "+cmp+" (SELECT f2."+name+", f2.ctid FROM field_index f2 WHERE f2.item_id = fi.item_id AND "+
		has("f2")+" ORDER BY f2."+name+" "+dir+", f2.ctid "+tie+" LIMIT 1)")
	return "SELECT " + itemColumns + ", 0::float8 FROM field_index fi JOIN item ON item.id = fi.item_id WHERE " +
		has("fi") + " AND " + strings.Join(conds, " AND ") +
		" ORDER BY fi." + name + " " + dir + ", fi.item_id " + dir +
		" LIMIT " + c.bind(limit) + " OFFSET " + c.bind(offset), nil
}

// missingSQL compiles the second phase: the matching items without a value for the sort field, in id order.
func (c *compiler) missingSQL(limit, offset int) (string, error) {
	s := c.q.Sort[0]
	if !storeutil.ValidField(s.Field) {
		return "", fmt.Errorf("invalid field name %q", s.Field)
	}
	name, _, err := columnInfo(s.Column)
	if err != nil {
		return "", err
	}
	conds, err := c.itemConds()
	if err != nil {
		return "", err
	}
	conds = append(conds, "NOT EXISTS (SELECT 1 FROM field_index fi WHERE fi.item_id = item.id AND fi.scope = "+c.projection()+" AND fi.field = "+
		c.bind(s.Field)+" AND fi.locale IN ('', "+c.locale()+") AND fi."+name+" IS NOT NULL)")
	return "SELECT " + itemColumns + ", 0::float8 FROM item WHERE " + strings.Join(conds, " AND ") +
		" ORDER BY item.id " + walkDir(s) + " LIMIT " + c.bind(limit) + " OFFSET " + c.bind(offset), nil
}

// countWalkSQL counts the matching items that have a value for the sort field.
func (c *compiler) countWalkSQL() (string, error) {
	s := c.q.Sort[0]
	name, _, err := columnInfo(s.Column)
	if err != nil {
		return "", err
	}
	conds, err := c.itemConds()
	if err != nil {
		return "", err
	}
	conds = append(conds, "EXISTS (SELECT 1 FROM field_index fi WHERE fi.item_id = item.id AND fi.scope = "+c.projection()+" AND fi.field = "+c.bind(s.Field)+
		" AND fi.locale IN ('', "+c.locale()+") AND fi."+name+" IS NOT NULL)")
	return "SELECT count(*) FROM item WHERE " + strings.Join(conds, " AND "), nil
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
				err = db.QueryRow(ctx, text, c.args...).Scan(&n)
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
		return store.QueryResult{}, fmt.Errorf("postgres: query: %w", err)
	}
	if err := decorate(ctx, db, q, res.Items); err != nil {
		return store.QueryResult{}, err
	}
	if res.Total, err = total(ctx, db, q, len(res.Items)); err != nil {
		return store.QueryResult{}, err
	}
	return res, nil
}

// pageRows runs a page statement whose last column is the search rank.
func pageRows(ctx context.Context, db queryer, text string, args []any) ([]store.ItemSummary, error) {
	rows, err := db.Query(ctx, text, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.ItemSummary, error) {
		var searched float64
		it, err := scanItem(r, &searched)
		return store.ItemSummary{Item: it, Searched: searched}, err
	})
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
		return 0, fmt.Errorf("postgres: query: %w", err)
	}
	var n int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM "+from+where, c.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("postgres: query count: %w", err)
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

	rows, err := db.Query(ctx, `SELECT vl.item_id, vl.locale, vl.name FROM item_version_locale vl
		JOIN item ON item.id = vl.item_id AND item.published_version = vl.version
		WHERE vl.item_id = ANY($1)`, idArgs(idList))
	if err != nil {
		return fmt.Errorf("postgres: query locales: %w", err)
	}
	var id [16]byte
	var loc, name string
	if _, err := pgx.ForEachRow(rows, []any{&id, &loc, &name}, func() error {
		d.Published(ids.UUID(id), loc, name)
		return nil
	}); err != nil {
		return fmt.Errorf("postgres: query locales: %w", err)
	}

	rows, err = db.Query(ctx, `SELECT item_id, part, name FROM draft_part WHERE item_id = ANY($1)`, idArgs(idList))
	if err != nil {
		return fmt.Errorf("postgres: query drafts: %w", err)
	}
	if _, err := pgx.ForEachRow(rows, []any{&id, &loc, &name}, func() error {
		d.Draft(ids.UUID(id), loc, name)
		return nil
	}); err != nil {
		return fmt.Errorf("postgres: query drafts: %w", err)
	}
	d.Finish()
	return nil
}
