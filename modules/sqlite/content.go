package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
)

// maxVars stays below SQLite's default limit of 32766 bound variables per statement.
const maxVars = 30000

type contentStore struct{ tx *sql.Tx }

type scanner interface{ Scan(dest ...any) error }

func uid(u ids.UUID) []byte { return u[:] }

func toUUID(b []byte) (ids.UUID, error) {
	var u ids.UUID
	if len(b) != len(u) {
		return u, fmt.Errorf("sqlite: uuid of %d bytes", len(b))
	}
	copy(u[:], b)
	return u, nil
}

// nullable maps the empty string to NULL.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// jsonText stores a JSON document as text; a missing document is an empty object.
func jsonText(b []byte) string {
	if len(b) == 0 {
		return "{}"
	}
	return string(b)
}

func mapNoRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func isKeyConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: item.key")
}

// placeholders returns "?,?,?" for n values.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// idArgs converts ids to bind values.
func idArgs(list []ids.UUID) []any {
	out := make([]any, len(list))
	for i, u := range list {
		out[i] = uid(u)
	}
	return out
}

// insertRows inserts len(args)/cols rows with multi-row INSERT statements that stay within SQLite's variable limit.
// head is "INSERT INTO t (a, b) VALUES".
func (s contentStore) insertRows(ctx context.Context, head string, cols int, args []any) error {
	group := "(" + placeholders(cols) + ")"
	perStmt := maxVars / cols
	for len(args) > 0 {
		n := min(len(args)/cols, perStmt)
		var b strings.Builder
		b.WriteString(head)
		b.WriteByte(' ')
		for i := range n {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(group)
		}
		if _, err := s.tx.ExecContext(ctx, b.String(), args[:n*cols]...); err != nil {
			return err
		}
		args = args[n*cols:]
	}
	return nil
}

// Schema revisions

func (s contentStore) SchemaRev(ctx context.Context, hash string) (int, bool, error) {
	var rev int
	err := s.tx.QueryRowContext(ctx, `SELECT rev FROM schema_rev WHERE hash = ?`, hash).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("sqlite: schema rev: %w", err)
	}
	return rev, true, nil
}

func (s contentStore) AddSchemaRev(ctx context.Context, hash string, compiled []byte, at time.Time) (int, error) {
	if rev, ok, err := s.SchemaRev(ctx, hash); ok || err != nil {
		return rev, err
	}
	var rev int
	if err := s.tx.QueryRowContext(ctx, `INSERT INTO schema_rev (hash, compiled, created_at) VALUES (?, ?, ?)
		RETURNING rev`, hash, jsonText(compiled), ms(at)).Scan(&rev); err != nil {
		return 0, fmt.Errorf("sqlite: add schema rev: %w", err)
	}
	return rev, nil
}

func (s contentStore) LatestSchemaRev(ctx context.Context) (int, string, []byte, error) {
	var rev int
	var hash string
	var compiled []byte
	err := s.tx.QueryRowContext(ctx, `SELECT rev, hash, compiled FROM schema_rev ORDER BY rev DESC LIMIT 1`).
		Scan(&rev, &hash, &compiled)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil, nil
	}
	if err != nil {
		return 0, "", nil, fmt.Errorf("sqlite: latest schema rev: %w", err)
	}
	return rev, hash, compiled, nil
}

// Items

const itemColumns = `item.id, item.key, item.type, item.main_locale, item.always_available, item.published_version,
	item.created_at, item.updated_at, item.created_by, item.updated_by, item.deleted_at, item.deleted_by`

// scanItem reads itemColumns followed by any extra destinations.
func scanItem(r scanner, extra ...any) (store.Item, error) {
	var (
		it               store.Item
		id               []byte
		key              sql.NullString
		created, updated int64
		deleted          sql.NullInt64
	)
	dest := append([]any{&id, &key, &it.Type, &it.MainLocale, &it.AlwaysAvailable, &it.PublishedVersion,
		&created, &updated, &it.CreatedBy, &it.UpdatedBy, &deleted, &it.DeletedBy}, extra...)
	if err := r.Scan(dest...); err != nil {
		return it, err
	}
	var err error
	if it.ID, err = toUUID(id); err != nil {
		return it, err
	}
	it.Key = key.String
	it.CreatedAt, it.UpdatedAt = fromMs(created), fromMs(updated)
	it.DeletedAt = fromOptMs(deleted)
	return it, nil
}

func (s contentStore) InsertItem(ctx context.Context, it store.Item) error {
	_, err := s.tx.ExecContext(ctx, `INSERT INTO item (id, key, type, main_locale, always_available, published_version,
			created_at, updated_at, created_by, updated_by, deleted_at, deleted_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid(it.ID), nullable(it.Key), it.Type, it.MainLocale, it.AlwaysAvailable, it.PublishedVersion,
		ms(it.CreatedAt), ms(it.UpdatedAt), it.CreatedBy, it.UpdatedBy, optMs(it.DeletedAt), it.DeletedBy)
	if isKeyConflict(err) {
		return store.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("sqlite: insert item %s: %w", it.ID, err)
	}
	return nil
}

func (s contentStore) GetItem(ctx context.Context, id ids.UUID, _ bool) (store.Item, error) {
	it, err := scanItem(s.tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM item WHERE id = ?`, uid(id)))
	if err != nil {
		return it, wrapNotFound(fmt.Sprintf("get item %s", id), err)
	}
	return it, nil
}

func (s contentStore) GetItemByKey(ctx context.Context, key string) (store.Item, error) {
	if key == "" {
		return store.Item{}, store.ErrNotFound
	}
	it, err := scanItem(s.tx.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM item WHERE key = ? AND deleted_at IS NULL`, key))
	if err != nil {
		return it, wrapNotFound(fmt.Sprintf("get item by key %q", key), err)
	}
	return it, nil
}

// wrapNotFound passes ErrNotFound through and wraps anything else.
func wrapNotFound(op string, err error) error {
	if err = mapNoRows(err); errors.Is(err, store.ErrNotFound) {
		return err
	}
	return fmt.Errorf("sqlite: %s: %w", op, err)
}

func (s contentStore) GetItems(ctx context.Context, list []ids.UUID) ([]store.Item, error) {
	out := make([]store.Item, 0, len(list))
	for chunk := range slices.Chunk(list, maxVars) {
		rows, err := s.tx.QueryContext(ctx, `SELECT `+itemColumns+` FROM item WHERE id IN (`+placeholders(len(chunk))+`)`,
			idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: get items: %w", err)
		}
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: get items: %w", err)
			}
			out = append(out, it)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: get items: %w", err)
		}
	}
	return out, nil
}

func (s contentStore) UpdateItem(ctx context.Context, it store.Item) error {
	res, err := s.tx.ExecContext(ctx, `UPDATE item SET key = ?, main_locale = ?, always_available = ?,
			published_version = ?, updated_at = ?, updated_by = ?
		WHERE id = ?`, nullable(it.Key), it.MainLocale, it.AlwaysAvailable, it.PublishedVersion, ms(it.UpdatedAt),
		it.UpdatedBy, uid(it.ID))
	if isKeyConflict(err) {
		return store.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("sqlite: update item %s: %w", it.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// SetDeleted writes only the trash columns. Restoring into a key a live item has taken fails on the partial
// unique index.
func (s contentStore) SetDeleted(ctx context.Context, id ids.UUID, at *time.Time, by string) error {
	if at == nil {
		by = ""
	}
	res, err := s.tx.ExecContext(ctx, `UPDATE item SET deleted_at = ?, deleted_by = ? WHERE id = ?`, optMs(at), by, uid(id))
	if isKeyConflict(err) {
		return store.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("sqlite: set deleted %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s contentStore) DeleteItem(ctx context.Context, id ids.UUID) error {
	res, err := s.tx.ExecContext(ctx, `DELETE FROM item WHERE id = ?`, uid(id))
	if err != nil {
		return fmt.Errorf("sqlite: delete item %s: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Versions

func (s contentStore) InsertVersion(ctx context.Context, v store.Version) error {
	if _, err := s.tx.ExecContext(ctx, `INSERT INTO item_version (item_id, version, schema_rev, shared, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?)`, uid(v.ItemID), v.Version, v.SchemaRev, jsonText(v.Shared), ms(v.CreatedAt),
		v.CreatedBy); err != nil {
		return fmt.Errorf("sqlite: insert version %s/%d: %w", v.ItemID, v.Version, err)
	}
	args := make([]any, 0, len(v.Locales)*5)
	for _, l := range v.Locales {
		args = append(args, uid(v.ItemID), v.Version, l.Locale, l.Name, jsonText(l.Data))
	}
	if err := s.insertRows(ctx, `INSERT INTO item_version_locale (item_id, version, locale, name, data) VALUES`, 5, args); err != nil {
		return fmt.Errorf("sqlite: insert version locales %s/%d: %w", v.ItemID, v.Version, err)
	}
	return nil
}

func (s contentStore) GetVersion(ctx context.Context, id ids.UUID, version int) (store.Version, error) {
	v := store.Version{ItemID: id, Version: version}
	var created int64
	err := s.tx.QueryRowContext(ctx, `SELECT schema_rev, shared, created_at, created_by FROM item_version
		WHERE item_id = ? AND version = ?`, uid(id), version).Scan(&v.SchemaRev, &v.Shared, &created, &v.CreatedBy)
	if err != nil {
		return v, wrapNotFound(fmt.Sprintf("get version %s/%d", id, version), err)
	}
	v.CreatedAt = fromMs(created)
	rows, err := s.tx.QueryContext(ctx, `SELECT locale, name, data FROM item_version_locale
		WHERE item_id = ? AND version = ? ORDER BY locale`, uid(id), version)
	if err != nil {
		return v, fmt.Errorf("sqlite: get version locales: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l store.VersionLocale
		if err := rows.Scan(&l.Locale, &l.Name, &l.Data); err != nil {
			return v, fmt.Errorf("sqlite: get version locales: %w", err)
		}
		v.Locales = append(v.Locales, l)
	}
	return v, rows.Err()
}

func (s contentStore) ListVersions(ctx context.Context, id ids.UUID) ([]store.VersionMeta, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT v.version, v.schema_rev, v.created_at, v.created_by, l.locale
		FROM item_version v
		LEFT JOIN item_version_locale l ON l.item_id = v.item_id AND l.version = v.version
		WHERE v.item_id = ?
		ORDER BY v.version DESC, l.locale`, uid(id))
	if err != nil {
		return nil, fmt.Errorf("sqlite: list versions: %w", err)
	}
	defer rows.Close()
	var out []store.VersionMeta
	for rows.Next() {
		var m store.VersionMeta
		var created int64
		var locale sql.NullString
		if err := rows.Scan(&m.Version, &m.SchemaRev, &created, &m.CreatedBy, &locale); err != nil {
			return nil, fmt.Errorf("sqlite: list versions: %w", err)
		}
		if n := len(out); n > 0 && out[n-1].Version == m.Version {
			m = out[n-1]
			out = out[:n-1]
		} else {
			m.CreatedAt = fromMs(created)
		}
		if locale.Valid {
			m.Locales = append(m.Locales, locale.String)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Drafts

func (s contentStore) PutDraftPart(ctx context.Context, p store.DraftPart) error {
	if _, err := s.tx.ExecContext(ctx, `INSERT INTO draft_part (item_id, part, base_version, name, data, updated_at, updated_by)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id, part) DO UPDATE SET base_version = excluded.base_version, name = excluded.name,
			data = excluded.data, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		uid(p.ItemID), p.Part, p.BaseVersion, p.Name, jsonText(p.Data), ms(p.UpdatedAt), p.UpdatedBy); err != nil {
		return fmt.Errorf("sqlite: put draft part %s/%q: %w", p.ItemID, p.Part, err)
	}
	return nil
}

func (s contentStore) GetDraftParts(ctx context.Context, id ids.UUID) ([]store.DraftPart, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT part, base_version, name, data, updated_at, updated_by FROM draft_part
		WHERE item_id = ? ORDER BY part`, uid(id))
	if err != nil {
		return nil, fmt.Errorf("sqlite: get draft parts: %w", err)
	}
	defer rows.Close()
	var out []store.DraftPart
	for rows.Next() {
		p := store.DraftPart{ItemID: id}
		var updated int64
		if err := rows.Scan(&p.Part, &p.BaseVersion, &p.Name, &p.Data, &updated, &p.UpdatedBy); err != nil {
			return nil, fmt.Errorf("sqlite: get draft parts: %w", err)
		}
		p.UpdatedAt = fromMs(updated)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s contentStore) DraftPartsOf(ctx context.Context, list []ids.UUID) (map[ids.UUID][]string, error) {
	out := map[ids.UUID][]string{}
	for chunk := range slices.Chunk(list, maxVars) {
		rows, err := s.tx.QueryContext(ctx, `SELECT item_id, part FROM draft_part WHERE item_id IN (`+placeholders(len(chunk))+`)
			ORDER BY item_id, part`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: draft parts of: %w", err)
		}
		for rows.Next() {
			var raw []byte
			var part string
			if err := rows.Scan(&raw, &part); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: draft parts of: %w", err)
			}
			var id ids.UUID
			copy(id[:], raw)
			out[id] = append(out[id], part)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: draft parts of: %w", err)
		}
	}
	return out, nil
}

func (s contentStore) PublishedLocalesOf(ctx context.Context, list []ids.UUID) (map[ids.UUID][]string, error) {
	out := map[ids.UUID][]string{}
	for chunk := range slices.Chunk(list, maxVars) {
		rows, err := s.tx.QueryContext(ctx, `SELECT vl.item_id, vl.locale FROM item_version_locale vl
			JOIN item ON item.id = vl.item_id AND item.published_version = vl.version
			WHERE vl.item_id IN (`+placeholders(len(chunk))+`) ORDER BY vl.item_id, vl.locale`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: published locales of: %w", err)
		}
		for rows.Next() {
			var raw []byte
			var loc string
			if err := rows.Scan(&raw, &loc); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: published locales of: %w", err)
			}
			var id ids.UUID
			copy(id[:], raw)
			out[id] = append(out[id], loc)
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: published locales of: %w", err)
		}
	}
	return out, nil
}

func (s contentStore) MainNamesOf(ctx context.Context, list []ids.UUID) (map[ids.UUID]string, error) {
	out := map[ids.UUID]string{}
	for chunk := range slices.Chunk(list, maxVars) {
		rows, err := s.tx.QueryContext(ctx, `SELECT i.id, COALESCE(NULLIF(d.name, ''), vl.name, '') FROM item i
			LEFT JOIN draft_part d ON d.item_id = i.id AND d.part = i.main_locale
			LEFT JOIN item_version_locale vl ON vl.item_id = i.id AND vl.version = i.published_version AND vl.locale = i.main_locale
			WHERE i.id IN (`+placeholders(len(chunk))+`)`, idArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("sqlite: main names of: %w", err)
		}
		for rows.Next() {
			var raw []byte
			var name string
			if err := rows.Scan(&raw, &name); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("sqlite: main names of: %w", err)
			}
			var id ids.UUID
			copy(id[:], raw)
			out[id] = name
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, fmt.Errorf("sqlite: main names of: %w", err)
		}
	}
	return out, nil
}

func (s contentStore) DeleteDraftParts(ctx context.Context, id ids.UUID, parts []string) error {
	var err error
	if parts == nil {
		_, err = s.tx.ExecContext(ctx, `DELETE FROM draft_part WHERE item_id = ?`, uid(id))
	} else if len(parts) > 0 {
		args := make([]any, 0, len(parts)+1)
		args = append(args, uid(id))
		for _, p := range parts {
			args = append(args, p)
		}
		_, err = s.tx.ExecContext(ctx, `DELETE FROM draft_part WHERE item_id = ? AND part IN (`+placeholders(len(parts))+`)`, args...)
	}
	if err != nil {
		return fmt.Errorf("sqlite: delete draft parts %s: %w", id, err)
	}
	return nil
}

// Projections

func (s contentStore) ReplaceRefs(ctx context.Context, from ids.UUID, refs []store.Ref) error {
	if _, err := s.tx.ExecContext(ctx, `DELETE FROM ref WHERE from_item = ?`, uid(from)); err != nil {
		return fmt.Errorf("sqlite: replace refs %s: %w", from, err)
	}
	args := make([]any, 0, len(refs)*5)
	for _, r := range refs {
		args = append(args, uid(from), r.FieldPath, uid(r.To), r.Kind, r.OnDelete)
	}
	if err := s.insertRows(ctx, `INSERT INTO ref (from_item, field_path, to_item, kind, on_delete) VALUES`, 5, args); err != nil {
		return fmt.Errorf("sqlite: replace refs %s: %w", from, err)
	}
	return nil
}

func (s contentStore) IncomingRefs(ctx context.Context, to ids.UUID) ([]store.Ref, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT from_item, field_path, kind, on_delete FROM ref
		WHERE to_item = ? ORDER BY from_item, field_path`, uid(to))
	if err != nil {
		return nil, fmt.Errorf("sqlite: incoming refs: %w", err)
	}
	defer rows.Close()
	var out []store.Ref
	for rows.Next() {
		r := store.Ref{To: to}
		var from []byte
		if err := rows.Scan(&from, &r.FieldPath, &r.Kind, &r.OnDelete); err != nil {
			return nil, fmt.Errorf("sqlite: incoming refs: %w", err)
		}
		if r.From, err = toUUID(from); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s contentStore) ReplaceIndex(ctx context.Context, item ids.UUID, proj store.Projection, rows []store.IndexRow) error {
	scope, err := scopeOf(proj)
	if err != nil {
		return err
	}
	if _, err := s.tx.ExecContext(ctx, `DELETE FROM field_index WHERE item_id = ? AND scope = ? AND module = ''`,
		uid(item), scope); err != nil {
		return fmt.Errorf("sqlite: replace index %s: %w", item, err)
	}
	args, err := indexArgs(item, "", rows, scope)
	if err != nil {
		return fmt.Errorf("sqlite: replace index %s: %w", item, err)
	}
	if err := s.insertIndex(ctx, args); err != nil {
		return fmt.Errorf("sqlite: replace index %s: %w", item, err)
	}
	return nil
}

// ReplaceModuleIndex stores a module's rows once per scope, so every query sees them whatever its projection.
func (s contentStore) ReplaceModuleIndex(ctx context.Context, item ids.UUID, module string, rows []store.IndexRow) error {
	if module == "" {
		return errors.New("sqlite: replace module index: empty module name")
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.Field, module+".") {
			return fmt.Errorf("sqlite: replace module index %s/%s: field %q is not prefixed with the module name", item, module, r.Field)
		}
	}
	if _, err := s.tx.ExecContext(ctx, `DELETE FROM field_index WHERE item_id = ? AND module = ?`, uid(item), module); err != nil {
		return fmt.Errorf("sqlite: replace module index %s/%s: %w", item, module, err)
	}
	args, err := indexArgs(item, module, rows, "p", "w")
	if err != nil {
		return fmt.Errorf("sqlite: replace module index %s/%s: %w", item, module, err)
	}
	if err := s.insertIndex(ctx, args); err != nil {
		return fmt.Errorf("sqlite: replace module index %s/%s: %w", item, module, err)
	}
	return nil
}

func scopeOf(p store.Projection) (string, error) {
	switch p {
	case store.ProjectionPublished, store.ProjectionWorking:
		return string(p), nil
	}
	return "", fmt.Errorf("sqlite: unknown projection %q", p)
}

const indexCols = 10

func (s contentStore) insertIndex(ctx context.Context, args []any) error {
	return s.insertRows(ctx, `INSERT INTO field_index (item_id, scope, module, locale, field, v_text, v_num, v_time, v_bool, v_ref) VALUES`,
		indexCols, args)
}

// indexArgs flattens rows into insert arguments, one copy per scope.
func indexArgs(item ids.UUID, module string, rows []store.IndexRow, scopes ...string) ([]any, error) {
	args := make([]any, 0, len(rows)*len(scopes)*indexCols)
	for _, r := range rows {
		set := 0
		row := [5]any{}
		if r.Text != nil {
			row[0] = *r.Text
			set++
		}
		if r.Num != nil {
			row[1] = *r.Num
			set++
		}
		if r.Time != nil {
			row[2] = ms(*r.Time)
			set++
		}
		if r.Bool != nil {
			row[3] = *r.Bool
			set++
		}
		if r.Ref != nil {
			row[4] = uid(*r.Ref)
			set++
		}
		if set != 1 {
			return nil, fmt.Errorf("field %q has %d value columns set, want 1", r.Field, set)
		}
		for _, scope := range scopes {
			args = append(args, uid(item), scope, module, r.Locale, r.Field)
			args = append(args, row[:]...)
		}
	}
	return args, nil
}

func (s contentStore) ReplaceSearch(ctx context.Context, item ids.UUID, proj store.Projection, docs []store.SearchDoc) error {
	scope, err := scopeOf(proj)
	if err != nil {
		return err
	}
	if _, err := s.tx.ExecContext(ctx, `DELETE FROM content_search_doc WHERE item_id = ? AND scope = ?`, uid(item), scope); err != nil {
		return fmt.Errorf("sqlite: replace search %s: %w", item, err)
	}
	if len(docs) == 0 {
		return nil
	}
	merged := storeutil.SearchRows(docs)
	args := make([]any, 0, len(merged)*3)
	for _, r := range merged {
		args = append(args, uid(item), scope, r.Locale)
	}
	// A page of locales is far below the variable limit, so one statement inserts them all.
	rows, err := s.tx.QueryContext(ctx, `INSERT INTO content_search_doc (item_id, scope, locale) VALUES `+
		strings.TrimSuffix(strings.Repeat("(?,?,?),", len(merged)), ",")+` RETURNING id, locale`, args...)
	if err != nil {
		return fmt.Errorf("sqlite: replace search %s: %w", item, err)
	}
	var fts []any
	for rows.Next() {
		var id int64
		var locale string
		if err := rows.Scan(&id, &locale); err != nil {
			_ = rows.Close()
			return fmt.Errorf("sqlite: replace search %s: %w", item, err)
		}
		r := merged[slices.IndexFunc(merged, func(r storeutil.SearchRow) bool { return r.Locale == locale })]
		fts = append(fts, id, r.High, r.Text, r.Low)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("sqlite: replace search %s: %w", item, err)
	}
	if err := s.insertRows(ctx, `INSERT INTO content_search (rowid, high, text, low) VALUES`, 4, fts); err != nil {
		return fmt.Errorf("sqlite: replace search %s: %w", item, err)
	}
	return nil
}

// Content migrations

func (s contentStore) AppliedContentMigrations(ctx context.Context) ([]string, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT name FROM content_migration ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("sqlite: applied content migrations: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func (s contentStore) RecordContentMigration(ctx context.Context, name string, at time.Time) error {
	if _, err := s.tx.ExecContext(ctx, `INSERT INTO content_migration (name, applied_at) VALUES (?, ?)
		ON CONFLICT (name) DO NOTHING`, name, ms(at)); err != nil {
		return fmt.Errorf("sqlite: record content migration %q: %w", name, err)
	}
	return nil
}

func (s contentStore) ItemIDsOfType(ctx context.Context, typ string, after ids.UUID, limit int) ([]ids.UUID, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT id FROM item WHERE type = ? AND id > ? ORDER BY id LIMIT ?`,
		typ, uid(after), limit)
	if err != nil {
		return nil, fmt.Errorf("sqlite: item ids of type %q: %w", typ, err)
	}
	defer rows.Close()
	var out []ids.UUID
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		u, err := toUUID(b)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s contentStore) SearchIndexEmpty(ctx context.Context) (bool, error) {
	var empty bool
	err := s.tx.QueryRowContext(ctx, `SELECT
		(EXISTS (SELECT 1 FROM item WHERE published_version > 0 AND deleted_at IS NULL)
			AND NOT EXISTS (SELECT 1 FROM content_search_doc WHERE scope = 'p'))
		OR (EXISTS (SELECT 1 FROM item WHERE deleted_at IS NULL)
			AND NOT EXISTS (SELECT 1 FROM content_search_doc WHERE scope = 'w'))`).Scan(&empty)
	if err != nil {
		return false, fmt.Errorf("sqlite: search index empty: %w", err)
	}
	return empty, nil
}

// LockName does nothing: SQLite has a single writer.
func (contentStore) LockName(context.Context, string) error { return nil }
