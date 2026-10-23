package pages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// chunkSize bounds the rows or ids one statement carries, well below SQLite's and Postgres' parameter limits.
const chunkSize = 400

const nodeCols = "id, site, parent_id, path, depth, sort_key, kind, item_id, hidden, data, main_locale, collection, created_at, updated_at"

// nodeRow is a page_node row.
type nodeRow struct {
	ID         ids.UUID
	Site       string
	Parent     *ids.UUID
	Path       string // ancestry "/<id>/<id>/" including the node
	Depth      int
	SortKey    string
	Kind       string
	Item       *ids.UUID
	Hidden     bool
	Data       json.RawMessage
	MainLocale string
	Collection string
	Created    time.Time
	Updated    time.Time
}

type scanner interface{ Scan(dest ...any) error }

func scanNode(sc scanner) (nodeRow, error) {
	var n nodeRow
	var data *json.RawMessage
	err := sc.Scan(&n.ID, &n.Site, &n.Parent, &n.Path, &n.Depth, &n.SortKey, &n.Kind, &n.Item, &n.Hidden, &data,
		&n.MainLocale, &n.Collection, &n.Created, &n.Updated)
	if data != nil {
		n.Data = *data
	}
	return n, err
}

func notFound(format string, args ...any) error {
	return fmt.Errorf("%w: %s", content.ErrNotFound, fmt.Sprintf(format, args...))
}

func badRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", content.ErrBadRequest, fmt.Sprintf(format, args...))
}

// childrenError is a conflict the API reports with the code has_children.
type childrenError struct{ msg string }

func (e *childrenError) Error() string        { return content.ErrConflict.Error() + ": " + e.msg }
func (e *childrenError) Is(target error) bool { return target == content.ErrConflict }
func (e *childrenError) APICode() string      { return "has_children" }

func hasChildren(format string, args ...any) error {
	return &childrenError{fmt.Sprintf(format, args...)}
}

func conflict(format string, args ...any) error {
	return fmt.Errorf("%w: %s", content.ErrConflict, fmt.Sprintf(format, args...))
}

// each runs a query and calls fn for every row.
func each(ctx context.Context, q store.SQL, query string, args []any, fn func(store.Rows) error) error {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// chunks calls fn with consecutive parts of items, at most chunkSize each.
func chunks[T any](items []T, fn func([]T) error) error {
	for len(items) > 0 {
		n := min(len(items), chunkSize)
		if err := fn(items[:n]); err != nil {
			return err
		}
		items = items[n:]
	}
	return nil
}

func marks(n int) string {
	if n == 0 {
		return "(NULL)"
	}
	return "(" + strings.Repeat("?,", n-1) + "?)"
}

// inArgs returns the "(?,?,…)" list for an IN clause and the statement arguments: prefix first, then the
// list.
func inArgs[T any](list []T, prefix ...any) (string, []any) {
	args := make([]any, 0, len(prefix)+len(list))
	args = append(args, prefix...)
	for _, v := range list {
		args = append(args, v)
	}
	return marks(len(list)), args
}

// queryNodes runs a query that selects nodeCols.
func queryNodes(ctx context.Context, q store.SQL, query string, args ...any) ([]nodeRow, error) {
	var out []nodeRow
	err := each(ctx, q, query, args, func(r store.Rows) error {
		n, err := scanNode(r)
		out = append(out, n)
		return err
	})
	return out, err
}

// pathUpper is the exclusive upper bound of the range holding every ancestry path that starts with prefix.
func pathUpper(prefix string) string { return prefix[:len(prefix)-1] + "0" }

func loadNode(ctx context.Context, q store.SQL, id ids.UUID) (nodeRow, error) {
	n, err := scanNode(q.QueryRow(ctx, "SELECT "+nodeCols+" FROM page_node WHERE id = ?", id))
	if errors.Is(err, store.ErrNotFound) {
		return n, notFound("page node %s", id)
	}
	return n, err
}

func loadNodeByItem(ctx context.Context, q store.SQL, item ids.UUID) (nodeRow, bool, error) {
	n, err := scanNode(q.QueryRow(ctx, "SELECT "+nodeCols+" FROM page_node WHERE item_id = ?", item))
	if errors.Is(err, store.ErrNotFound) {
		return n, false, nil
	}
	return n, err == nil, err
}

func loadNodes(ctx context.Context, q store.SQL, list []ids.UUID) ([]nodeRow, error) {
	out := make([]nodeRow, 0, len(list))
	err := chunks(list, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		rows, err := queryNodes(ctx, q, "SELECT "+nodeCols+" FROM page_node WHERE id IN "+ph, args...)
		out = append(out, rows...)
		return err
	})
	return out, err
}

// loadRange returns the node at prefix and all its descendants.
func loadRange(ctx context.Context, q store.SQL, prefix string) ([]nodeRow, error) {
	return queryNodes(ctx, q, "SELECT "+nodeCols+" FROM page_node WHERE path >= ? AND path < ?", prefix, pathUpper(prefix))
}

func loadMounts(ctx context.Context, q store.SQL, site, collection string) ([]nodeRow, error) {
	query := "SELECT " + nodeCols + " FROM page_node WHERE kind = 'mount' AND collection = ?"
	args := []any{collection}
	if site != "" {
		query += " AND site = ?"
		args = append(args, site)
	}
	return queryNodes(ctx, q, query+" ORDER BY site, id", args...)
}

// ancestry lists the node ids of an ancestry path, root first, the node itself last.
func ancestry(path string) ([]ids.UUID, error) {
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	out := make([]ids.UUID, len(parts))
	for i, p := range parts {
		id, err := ids.Parse(p)
		if err != nil {
			return nil, fmt.Errorf("pages: bad ancestry path %q: %w", path, err)
		}
		out[i] = id
	}
	return out, nil
}

func ancestryPath(parentPath string, id ids.UUID) string { return parentPath + id.String() + "/" }

// slugRow is a page's published slug, name, vanity path and canonical path override in one locale.
type slugRow struct {
	Slug      string
	Name      string
	Vanity    string
	Canonical string
}

type slugKey struct {
	Node   ids.UUID
	Locale string
}

func loadSlugsOf(ctx context.Context, q store.SQL, list []ids.UUID, into map[slugKey]slugRow) error {
	return chunks(list, func(part []ids.UUID) error {
		ph, args := inArgs(part)
		return each(ctx, q, "SELECT node_id, locale, slug, name, vanity, canonical FROM page_slug WHERE node_id IN "+ph,
			args, func(r store.Rows) error { return scanSlug(r, into) })
	})
}

func loadSlugRange(ctx context.Context, q store.SQL, prefix string, into map[slugKey]slugRow) error {
	return each(ctx, q, `SELECT s.node_id, s.locale, s.slug, s.name, s.vanity, s.canonical FROM page_slug s
JOIN page_node n ON n.id = s.node_id WHERE n.path >= ? AND n.path < ?`, []any{prefix, pathUpper(prefix)},
		func(r store.Rows) error { return scanSlug(r, into) })
}

func scanSlug(r store.Rows, into map[slugKey]slugRow) error {
	var k slugKey
	var s slugRow
	if err := r.Scan(&k.Node, &k.Locale, &s.Slug, &s.Name, &s.Vanity, &s.Canonical); err != nil {
		return err
	}
	into[k] = s
	return nil
}

func putSlug(ctx context.Context, q store.SQL, node ids.UUID, locale string, s slugRow) error {
	_, err := q.Exec(ctx, `INSERT INTO page_slug (node_id, locale, slug, name, vanity, canonical) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (node_id, locale) DO UPDATE SET slug = excluded.slug, name = excluded.name, vanity = excluded.vanity, canonical = excluded.canonical`,
		node, locale, s.Slug, s.Name, s.Vanity, s.Canonical)
	return err
}

// insertRows runs a multi-row INSERT of width columns, chunk by chunk, with an optional trailing clause.
func insertRows(ctx context.Context, q store.SQL, table, cols, tail string, rows [][]any) (int64, error) {
	width := strings.Count(cols, ",") + 1
	row := "(" + strings.Repeat("?,", width-1) + "?)"
	var total int64
	err := chunks(rows, func(part [][]any) error {
		var sb strings.Builder
		args := make([]any, 0, len(part)*width)
		sb.WriteString("INSERT INTO " + table + " (" + cols + ") VALUES ")
		for i, r := range part {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(row)
			args = append(args, r...)
		}
		sb.WriteString(" " + tail)
		n, err := q.Exec(ctx, sb.String(), args...)
		total += n
		return err
	})
	return total, err
}

func isNoRows(err error) bool { return errors.Is(err, store.ErrNotFound) }

func isDuplicate(err error) bool { return errors.Is(err, store.ErrDuplicateKey) }

func jsonUnmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("pages: bad node data: %w", err)
	}
	return nil
}
