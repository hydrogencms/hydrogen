package sqlite

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

// compiler turns a store.Query into SQL. Values only ever travel as numbered bind parameters (?N), so the
// text of the statement depends on the shape of the query, never on its data.
type compiler struct {
	q    store.Query
	args []any
	loc  string // placeholder of q.Locale, bound on first use

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
		default:
			return "", fmt.Errorf("unknown operator %q", cd.Op)
		}
	}
	field, loc := c.bind(cd.Field), c.locale()
	if exists == "EXISTS" {
		// A semi-join: SQLite builds the matching item ids once from the value index (a plain EXISTS would be
		// planned as one probe per item, and without table statistics it picks a range scan per probe).
		return "item.id IN (SELECT fi.item_id FROM field_index fi WHERE fi.field = " + field +
			" AND fi.locale IN ('', " + loc + ")" + pred + ")", nil
	}
	return "NOT EXISTS (SELECT 1 FROM field_index fi INDEXED BY field_index_item WHERE fi.item_id = item.id AND fi.field = " +
		field + " AND fi.locale IN ('', " + loc + ")" + pred + ")", nil
}

// match builds an FTS5 query in which every word is a quoted prefix term.
func match(terms []string) string {
	parts := make([]string, len(terms))
	for i, t := range terms {
		parts[i] = `"` + t + `"*`
	}
	return strings.Join(parts, " ")
}

// filter compiles everything before ORDER BY: the FROM clause (with the search join) and the WHERE clause.
func (c *compiler) filter() (from, where string, err error) {
	q := c.q
	from = "item"
	var conds []string
	if q.Type != "" {
		conds = append(conds, "item.type = "+c.bind(q.Type))
	}
	switch q.Status {
	case store.StatusAny:
	case store.StatusPublished:
		conds = append(conds, "item.published_version > 0")
	case store.StatusDraft:
		conds = append(conds, "EXISTS (SELECT 1 FROM draft_part dp WHERE dp.item_id = item.id)")
	default:
		return "", "", fmt.Errorf("unknown status %d", q.Status)
	}
	for _, cd := range q.Where {
		p, err := c.cond(cd)
		if err != nil {
			return "", "", err
		}
		conds = append(conds, p)
	}
	if terms := storeutil.SearchTerms(q.Search); len(terms) > 0 {
		c.searching = true
		// The rank column is bm25 with default weights (lower is better). The bm25() function itself cannot
		// be used through this join, and the inner select keeps the aggregate away from the FTS cursor.
		from += ` JOIN (
		SELECT item_id, MIN(rank) AS rank FROM (
			SELECT sd.item_id AS item_id, content_search.rank AS rank
			FROM content_search JOIN content_search_doc sd ON sd.id = content_search.rowid
			WHERE content_search MATCH ` + c.bind(match(terms)) + ` AND sd.locale IN ('', ` + c.locale() + `))
		GROUP BY item_id) srch ON srch.item_id = item.id`
	} else if strings.TrimSpace(q.Search) != "" {
		conds = append(conds, "0") // only punctuation: nothing can match
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
	return "(SELECT fi." + name + " FROM field_index fi INDEXED BY field_index_item WHERE fi.item_id = item.id AND fi.field = " + c.bind(s.Field) +
		" AND fi.locale IN ('', " + c.locale() + ") AND fi." + name + " IS NOT NULL ORDER BY fi." + name + dir +
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

func (s contentStore) Query(ctx context.Context, q store.Query) (store.QueryResult, error) {
	q = storeutil.Normalize(q)
	sqlText, args, err := selectSQL(q)
	if err != nil {
		return store.QueryResult{}, fmt.Errorf("sqlite: query: %w", err)
	}
	rows, err := s.tx.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return store.QueryResult{}, fmt.Errorf("sqlite: query: %w", err)
	}
	var res store.QueryResult
	for rows.Next() {
		var searched float64
		it, err := scanItem(rows, &searched)
		if err != nil {
			_ = rows.Close()
			return store.QueryResult{}, fmt.Errorf("sqlite: query: %w", err)
		}
		res.Items = append(res.Items, store.ItemSummary{Item: it, Searched: searched})
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return store.QueryResult{}, fmt.Errorf("sqlite: query: %w", err)
	}
	if err := s.decorate(ctx, q.Locale, res.Items); err != nil {
		return store.QueryResult{}, err
	}
	if res.Total, err = s.total(ctx, q, len(res.Items)); err != nil {
		return store.QueryResult{}, err
	}
	return res, nil
}

// total counts the matches. A short page answers it without a query; otherwise a separate COUNT(*) is used
// rather than COUNT(*) OVER (), because the window function would force the engine to produce every matching
// row before LIMIT, defeating index-ordered early termination of the main query.
func (s contentStore) total(ctx context.Context, q store.Query, got int) (int, error) {
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
	if err := s.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+from+where, c.args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlite: query count: %w", err)
	}
	return n, nil
}

// decorate fills Name, Drafts and Locales for a page of items with two set-based queries.
func (s contentStore) decorate(ctx context.Context, locale string, items []store.ItemSummary) error {
	if len(items) == 0 {
		return nil
	}
	d := storeutil.NewDecorator(locale, items)
	idList := d.IDs()
	in := placeholders(len(items))

	rows, err := s.tx.QueryContext(ctx, `SELECT vl.item_id, vl.locale, vl.name FROM item_version_locale vl
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

	rows, err = s.tx.QueryContext(ctx, `SELECT item_id, part, name FROM draft_part WHERE item_id IN (`+in+`)`, idArgs(idList)...)
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
