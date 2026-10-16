package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/store/storeutil"
)

type contentStore struct{ tx pgx.Tx }

func (t *tx) Content() store.ContentStore { return contentStore{t.tx} }

// uid converts an id to a bind value; the unnamed array type is what pgx maps to uuid.
func uid(u ids.UUID) [16]byte { return u }

func idArgs(list []ids.UUID) [][16]byte {
	out := make([][16]byte, len(list))
	for i, u := range list {
		out[i] = uid(u)
	}
	return out
}

// tz keeps times at the millisecond precision every adapter stores.
func tz(t time.Time) time.Time { return t.UTC().Truncate(time.Millisecond) }

func utc(t time.Time) time.Time { return t.UTC() }

// nullable maps the empty string to NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// jsonDoc makes a missing document an empty object (a nil []byte would bind as NULL).
func jsonDoc(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

func isKeyConflict(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "item_key"
}

// wrapNotFound passes ErrNotFound through and wraps anything else.
func wrapNotFound(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return store.ErrNotFound
	}
	return fmt.Errorf("postgres: %s: %w", op, err)
}

// Schema revisions

func (s contentStore) SchemaRev(ctx context.Context, hash string) (int, bool, error) {
	var rev int
	err := s.tx.QueryRow(ctx, `SELECT rev FROM schema_rev WHERE hash = $1`, hash).Scan(&rev)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("postgres: schema rev: %w", err)
	}
	return rev, true, nil
}

// AddSchemaRev looks first so that a known hash does not burn an identity value.
func (s contentStore) AddSchemaRev(ctx context.Context, hash string, compiled []byte, at time.Time) (int, error) {
	if rev, ok, err := s.SchemaRev(ctx, hash); ok || err != nil {
		return rev, err
	}
	var rev int
	err := s.tx.QueryRow(ctx, `INSERT INTO schema_rev (hash, compiled, created_at) VALUES ($1, $2, $3)
		ON CONFLICT (hash) DO UPDATE SET hash = excluded.hash RETURNING rev`, hash, jsonDoc(compiled), tz(at)).Scan(&rev)
	if err != nil {
		return 0, fmt.Errorf("postgres: add schema rev: %w", err)
	}
	return rev, nil
}

func (s contentStore) LatestSchemaRev(ctx context.Context) (int, string, []byte, error) {
	var rev int
	var hash string
	var compiled []byte
	err := s.tx.QueryRow(ctx, `SELECT rev, hash, compiled FROM schema_rev ORDER BY rev DESC LIMIT 1`).
		Scan(&rev, &hash, &compiled)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", nil, nil
	}
	if err != nil {
		return 0, "", nil, fmt.Errorf("postgres: latest schema rev: %w", err)
	}
	return rev, hash, compiled, nil
}

// Items

const itemColumns = `item.id, item.key, item.type, item.main_locale, item.always_available, item.published_version,
	item.created_at, item.updated_at, item.created_by, item.updated_by, item.deleted_at, item.deleted_by`

// scanItem reads itemColumns followed by any extra destinations.
func scanItem(r pgx.Row, extra ...any) (store.Item, error) {
	var (
		it  store.Item
		id  [16]byte
		key *string
	)
	dest := append([]any{&id, &key, &it.Type, &it.MainLocale, &it.AlwaysAvailable, &it.PublishedVersion,
		&it.CreatedAt, &it.UpdatedAt, &it.CreatedBy, &it.UpdatedBy, &it.DeletedAt, &it.DeletedBy}, extra...)
	if err := r.Scan(dest...); err != nil {
		return it, err
	}
	it.ID = id
	if key != nil {
		it.Key = *key
	}
	it.CreatedAt, it.UpdatedAt, it.DeletedAt = utc(it.CreatedAt), utc(it.UpdatedAt), optUTC(it.DeletedAt)
	return it, nil
}

func (s contentStore) InsertItem(ctx context.Context, it store.Item) error {
	tag, err := s.tx.Exec(ctx, `INSERT INTO item (id, key, type, main_locale, always_available, published_version,
			created_at, updated_at, created_by, updated_by, deleted_at, deleted_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (key) WHERE deleted_at IS NULL DO NOTHING`,
		uid(it.ID), nullable(it.Key), it.Type, it.MainLocale, it.AlwaysAvailable, it.PublishedVersion,
		tz(it.CreatedAt), tz(it.UpdatedAt), it.CreatedBy, it.UpdatedBy, optTz(it.DeletedAt), it.DeletedBy)
	if err != nil {
		return fmt.Errorf("postgres: insert item %s: %w", it.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrDuplicateKey
	}
	return nil
}

func (s contentStore) GetItem(ctx context.Context, id ids.UUID, forUpdate bool) (store.Item, error) {
	query := `SELECT ` + itemColumns + ` FROM item WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	it, err := scanItem(s.tx.QueryRow(ctx, query, uid(id)))
	if err != nil {
		return it, wrapNotFound(fmt.Sprintf("get item %s", id), err)
	}
	return it, nil
}

func (s contentStore) GetItemByKey(ctx context.Context, key string) (store.Item, error) {
	if key == "" {
		return store.Item{}, store.ErrNotFound
	}
	it, err := scanItem(s.tx.QueryRow(ctx, `SELECT `+itemColumns+` FROM item WHERE key = $1 AND deleted_at IS NULL`, key))
	if err != nil {
		return it, wrapNotFound(fmt.Sprintf("get item by key %q", key), err)
	}
	return it, nil
}

func (s contentStore) GetItems(ctx context.Context, list []ids.UUID) ([]store.Item, error) {
	if len(list) == 0 {
		return nil, nil
	}
	rows, err := s.tx.Query(ctx, `SELECT `+itemColumns+` FROM item WHERE id = ANY($1)`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: get items: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Item, error) { return scanItem(r) })
	if err != nil {
		return nil, fmt.Errorf("postgres: get items: %w", err)
	}
	return out, nil
}

// UpdateItem rejects a taken key in the statement itself, because a unique violation would abort the transaction.
func (s contentStore) UpdateItem(ctx context.Context, it store.Item) error {
	tag, err := s.tx.Exec(ctx, `UPDATE item SET key = $2, main_locale = $3, always_available = $4,
			published_version = $5, updated_at = $6, updated_by = $7
		WHERE id = $1 AND ($2::text IS NULL OR deleted_at IS NOT NULL
			OR NOT EXISTS (SELECT 1 FROM item o WHERE o.key = $2 AND o.id <> $1 AND o.deleted_at IS NULL))`,
		uid(it.ID), nullable(it.Key), it.MainLocale, it.AlwaysAvailable, it.PublishedVersion, tz(it.UpdatedAt), it.UpdatedBy)
	if isKeyConflict(err) {
		return store.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("postgres: update item %s: %w", it.ID, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := s.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM item WHERE id = $1)`, uid(it.ID)).Scan(&exists); err != nil {
		return fmt.Errorf("postgres: update item %s: %w", it.ID, err)
	}
	if exists {
		return store.ErrDuplicateKey
	}
	return store.ErrNotFound
}

// SetDeleted writes only the trash columns. A restore into a key a live item has taken is rejected in the
// statement, because a unique violation would abort the transaction.
func (s contentStore) SetDeleted(ctx context.Context, id ids.UUID, at *time.Time, by string) error {
	if at != nil {
		tag, err := s.tx.Exec(ctx, `UPDATE item SET deleted_at = $2, deleted_by = $3 WHERE id = $1`, uid(id), tz(*at), by)
		if err != nil {
			return fmt.Errorf("postgres: set deleted %s: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return nil
	}
	tag, err := s.tx.Exec(ctx, `UPDATE item SET deleted_at = NULL, deleted_by = '' WHERE id = $1
		AND (key IS NULL OR deleted_at IS NULL OR NOT EXISTS (SELECT 1 FROM item o WHERE o.key = item.key AND o.id <> item.id AND o.deleted_at IS NULL))`, uid(id))
	if isKeyConflict(err) {
		return store.ErrDuplicateKey
	}
	if err != nil {
		return fmt.Errorf("postgres: set deleted %s: %w", id, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	var exists bool
	if err := s.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM item WHERE id = $1)`, uid(id)).Scan(&exists); err != nil {
		return fmt.Errorf("postgres: set deleted %s: %w", id, err)
	}
	if exists {
		return store.ErrDuplicateKey
	}
	return store.ErrNotFound
}

func (s contentStore) DeleteItem(ctx context.Context, id ids.UUID) error {
	tag, err := s.tx.Exec(ctx, `DELETE FROM item WHERE id = $1`, uid(id))
	if err != nil {
		return fmt.Errorf("postgres: delete item %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

// Versions

func (s contentStore) InsertVersion(ctx context.Context, v store.Version) error {
	if _, err := s.tx.Exec(ctx, `INSERT INTO item_version (item_id, version, schema_rev, shared, created_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)`, uid(v.ItemID), v.Version, v.SchemaRev, jsonDoc(v.Shared), tz(v.CreatedAt),
		v.CreatedBy); err != nil {
		return fmt.Errorf("postgres: insert version %s/%d: %w", v.ItemID, v.Version, err)
	}
	if len(v.Locales) == 0 {
		return nil
	}
	locales, names, data := make([]string, len(v.Locales)), make([]string, len(v.Locales)), make([]string, len(v.Locales))
	for i, l := range v.Locales {
		locales[i], names[i], data[i] = l.Locale, l.Name, string(jsonDoc(l.Data))
	}
	if _, err := s.tx.Exec(ctx, `INSERT INTO item_version_locale (item_id, version, locale, name, data)
		SELECT $1, $2, l, n, d::jsonb FROM unnest($3::text[], $4::text[], $5::text[]) AS u(l, n, d)`,
		uid(v.ItemID), v.Version, locales, names, data); err != nil {
		return fmt.Errorf("postgres: insert version locales %s/%d: %w", v.ItemID, v.Version, err)
	}
	return nil
}

func (s contentStore) GetVersion(ctx context.Context, id ids.UUID, version int) (store.Version, error) {
	v := store.Version{ItemID: id, Version: version}
	err := s.tx.QueryRow(ctx, `SELECT schema_rev, shared, created_at, created_by FROM item_version
		WHERE item_id = $1 AND version = $2`, uid(id), version).Scan(&v.SchemaRev, &v.Shared, &v.CreatedAt, &v.CreatedBy)
	if err != nil {
		return v, wrapNotFound(fmt.Sprintf("get version %s/%d", id, version), err)
	}
	v.CreatedAt = utc(v.CreatedAt)
	rows, err := s.tx.Query(ctx, `SELECT locale, name, data FROM item_version_locale
		WHERE item_id = $1 AND version = $2 ORDER BY locale COLLATE "C"`, uid(id), version)
	if err != nil {
		return v, fmt.Errorf("postgres: get version locales: %w", err)
	}
	v.Locales, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.VersionLocale, error) {
		var l store.VersionLocale
		err := r.Scan(&l.Locale, &l.Name, &l.Data)
		return l, err
	})
	if err != nil {
		return v, fmt.Errorf("postgres: get version locales: %w", err)
	}
	return v, nil
}

func (s contentStore) ListVersions(ctx context.Context, id ids.UUID) ([]store.VersionMeta, error) {
	rows, err := s.tx.Query(ctx, `SELECT v.version, v.schema_rev, v.created_at, v.created_by, l.locale
		FROM item_version v
		LEFT JOIN item_version_locale l ON l.item_id = v.item_id AND l.version = v.version
		WHERE v.item_id = $1
		ORDER BY v.version DESC, l.locale COLLATE "C"`, uid(id))
	if err != nil {
		return nil, fmt.Errorf("postgres: list versions: %w", err)
	}
	defer rows.Close()
	var out []store.VersionMeta
	for rows.Next() {
		var m store.VersionMeta
		var locale *string
		if err := rows.Scan(&m.Version, &m.SchemaRev, &m.CreatedAt, &m.CreatedBy, &locale); err != nil {
			return nil, fmt.Errorf("postgres: list versions: %w", err)
		}
		if n := len(out); n > 0 && out[n-1].Version == m.Version {
			m = out[n-1]
			out = out[:n-1]
		} else {
			m.CreatedAt = utc(m.CreatedAt)
		}
		if locale != nil {
			m.Locales = append(m.Locales, *locale)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Drafts

func (s contentStore) PutDraftPart(ctx context.Context, p store.DraftPart) error {
	if _, err := s.tx.Exec(ctx, `INSERT INTO draft_part (item_id, part, base_version, name, data, updated_at, updated_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (item_id, part) DO UPDATE SET base_version = excluded.base_version, name = excluded.name,
			data = excluded.data, updated_at = excluded.updated_at, updated_by = excluded.updated_by`,
		uid(p.ItemID), p.Part, p.BaseVersion, p.Name, jsonDoc(p.Data), tz(p.UpdatedAt), p.UpdatedBy); err != nil {
		return fmt.Errorf("postgres: put draft part %s/%q: %w", p.ItemID, p.Part, err)
	}
	return nil
}

func (s contentStore) GetDraftParts(ctx context.Context, id ids.UUID) ([]store.DraftPart, error) {
	rows, err := s.tx.Query(ctx, `SELECT part, base_version, name, data, updated_at, updated_by FROM draft_part
		WHERE item_id = $1 ORDER BY part COLLATE "C"`, uid(id))
	if err != nil {
		return nil, fmt.Errorf("postgres: get draft parts: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.DraftPart, error) {
		p := store.DraftPart{ItemID: id}
		err := r.Scan(&p.Part, &p.BaseVersion, &p.Name, &p.Data, &p.UpdatedAt, &p.UpdatedBy)
		p.UpdatedAt = utc(p.UpdatedAt)
		return p, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: get draft parts: %w", err)
	}
	return out, nil
}

func (s contentStore) DraftPartsOf(ctx context.Context, list []ids.UUID) (map[ids.UUID][]string, error) {
	out := map[ids.UUID][]string{}
	if len(list) == 0 {
		return out, nil
	}
	rows, err := s.tx.Query(ctx, `SELECT item_id, part FROM draft_part WHERE item_id = ANY($1)
		ORDER BY item_id, part COLLATE "C"`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: draft parts of: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id [16]byte
		var part string
		if err := rows.Scan(&id, &part); err != nil {
			return nil, fmt.Errorf("postgres: draft parts of: %w", err)
		}
		out[ids.UUID(id)] = append(out[ids.UUID(id)], part)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: draft parts of: %w", err)
	}
	return out, nil
}

func (s contentStore) PublishedLocalesOf(ctx context.Context, list []ids.UUID) (map[ids.UUID][]string, error) {
	out := map[ids.UUID][]string{}
	if len(list) == 0 {
		return out, nil
	}
	rows, err := s.tx.Query(ctx, `SELECT vl.item_id, vl.locale FROM item_version_locale vl
		JOIN item ON item.id = vl.item_id AND item.published_version = vl.version
		WHERE vl.item_id = ANY($1) ORDER BY vl.item_id, vl.locale COLLATE "C"`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: published locales of: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id [16]byte
		var loc string
		if err := rows.Scan(&id, &loc); err != nil {
			return nil, fmt.Errorf("postgres: published locales of: %w", err)
		}
		out[ids.UUID(id)] = append(out[ids.UUID(id)], loc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: published locales of: %w", err)
	}
	return out, nil
}

func (s contentStore) MainNamesOf(ctx context.Context, list []ids.UUID) (map[ids.UUID]string, error) {
	out := map[ids.UUID]string{}
	if len(list) == 0 {
		return out, nil
	}
	rows, err := s.tx.Query(ctx, `SELECT i.id, COALESCE(NULLIF(d.name, ''), vl.name, '') FROM item i
		LEFT JOIN draft_part d ON d.item_id = i.id AND d.part = i.main_locale
		LEFT JOIN item_version_locale vl ON vl.item_id = i.id AND vl.version = i.published_version AND vl.locale = i.main_locale
		WHERE i.id = ANY($1)`, idArgs(list))
	if err != nil {
		return nil, fmt.Errorf("postgres: main names of: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id [16]byte
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, fmt.Errorf("postgres: main names of: %w", err)
		}
		out[ids.UUID(id)] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: main names of: %w", err)
	}
	return out, nil
}

func (s contentStore) DeleteDraftParts(ctx context.Context, id ids.UUID, parts []string) error {
	var err error
	if parts == nil {
		_, err = s.tx.Exec(ctx, `DELETE FROM draft_part WHERE item_id = $1`, uid(id))
	} else if len(parts) > 0 {
		_, err = s.tx.Exec(ctx, `DELETE FROM draft_part WHERE item_id = $1 AND part = ANY($2)`, uid(id), parts)
	}
	if err != nil {
		return fmt.Errorf("postgres: delete draft parts %s: %w", id, err)
	}
	return nil
}

// Projections

func (s contentStore) ReplaceRefs(ctx context.Context, from ids.UUID, refs []store.Ref) error {
	if _, err := s.tx.Exec(ctx, `DELETE FROM ref WHERE from_item = $1`, uid(from)); err != nil {
		return fmt.Errorf("postgres: replace refs %s: %w", from, err)
	}
	if len(refs) == 0 {
		return nil
	}
	paths, to := make([]string, len(refs)), make([][16]byte, len(refs))
	kinds, onDelete := make([]string, len(refs)), make([]string, len(refs))
	for i, r := range refs {
		paths[i], to[i], kinds[i], onDelete[i] = r.FieldPath, uid(r.To), r.Kind, r.OnDelete
	}
	if _, err := s.tx.Exec(ctx, `INSERT INTO ref (from_item, field_path, to_item, kind, on_delete)
		SELECT $1, p, t, k, o FROM unnest($2::text[], $3::uuid[], $4::text[], $5::text[]) AS u(p, t, k, o)`,
		uid(from), paths, to, kinds, onDelete); err != nil {
		return fmt.Errorf("postgres: replace refs %s: %w", from, err)
	}
	return nil
}

func (s contentStore) IncomingRefs(ctx context.Context, to ids.UUID) ([]store.Ref, error) {
	rows, err := s.tx.Query(ctx, `SELECT from_item, field_path, kind, on_delete FROM ref
		WHERE to_item = $1 ORDER BY from_item, field_path COLLATE "C"`, uid(to))
	if err != nil {
		return nil, fmt.Errorf("postgres: incoming refs: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (store.Ref, error) {
		ref := store.Ref{To: to}
		var from [16]byte
		err := r.Scan(&from, &ref.FieldPath, &ref.Kind, &ref.OnDelete)
		ref.From = from
		return ref, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: incoming refs: %w", err)
	}
	return out, nil
}

func scopeOf(p store.Projection) (string, error) {
	switch p {
	case store.ProjectionPublished, store.ProjectionWorking:
		return string(p), nil
	}
	return "", fmt.Errorf("postgres: unknown projection %q", p)
}

func (s contentStore) ReplaceIndex(ctx context.Context, item ids.UUID, proj store.Projection, rows []store.IndexRow) error {
	scope, err := scopeOf(proj)
	if err != nil {
		return err
	}
	cols, err := indexColumns(rows)
	if err != nil {
		return fmt.Errorf("postgres: replace index %s: %w", item, err)
	}
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM field_index WHERE item_id = $1 AND scope = $2 AND module = ''`, uid(item), scope)
	queueIndex(b, item, "", []string{scope}, cols)
	if err := s.sendBatch(ctx, b); err != nil {
		return fmt.Errorf("postgres: replace index %s: %w", item, err)
	}
	return nil
}

// ReplaceModuleIndex stores a module's rows once per scope, so every query sees them whatever its projection.
func (s contentStore) ReplaceModuleIndex(ctx context.Context, item ids.UUID, module string, rows []store.IndexRow) error {
	if module == "" {
		return errors.New("postgres: replace module index: empty module name")
	}
	for _, r := range rows {
		if !strings.HasPrefix(r.Field, module+".") {
			return fmt.Errorf("postgres: replace module index %s/%s: field %q is not prefixed with the module name", item, module, r.Field)
		}
	}
	cols, err := indexColumns(rows)
	if err != nil {
		return fmt.Errorf("postgres: replace module index %s/%s: %w", item, module, err)
	}
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM field_index WHERE item_id = $1 AND module = $2`, uid(item), module)
	queueIndex(b, item, module, []string{"p", "w"}, cols)
	if err := s.sendBatch(ctx, b); err != nil {
		return fmt.Errorf("postgres: replace module index %s/%s: %w", item, module, err)
	}
	return nil
}

// indexCols are the rows of an index write as parallel arrays for unnest.
type indexCols struct {
	locales, fields []string
	texts           []*string
	nums            []*float64
	times           []*time.Time
	bools           []*bool
	refs            []*[16]byte
}

func indexColumns(rows []store.IndexRow) (indexCols, error) {
	n := len(rows)
	c := indexCols{locales: make([]string, n), fields: make([]string, n), texts: make([]*string, n), nums: make([]*float64, n),
		times: make([]*time.Time, n), bools: make([]*bool, n), refs: make([]*[16]byte, n)}
	for i, r := range rows {
		c.locales[i], c.fields[i], c.texts[i], c.nums[i], c.bools[i] = r.Locale, r.Field, r.Text, r.Num, r.Bool
		set := 0
		for _, ok := range []bool{r.Text != nil, r.Num != nil, r.Time != nil, r.Bool != nil, r.Ref != nil} {
			if ok {
				set++
			}
		}
		if set != 1 {
			return c, fmt.Errorf("field %q has %d value columns set, want 1", r.Field, set)
		}
		if r.Time != nil {
			t := tz(*r.Time)
			c.times[i] = &t
		}
		if r.Ref != nil {
			u := uid(*r.Ref)
			c.refs[i] = &u
		}
	}
	return c, nil
}

// sendBatch runs the statements in one round trip.
func (s contentStore) sendBatch(ctx context.Context, b *pgx.Batch) error {
	br := s.tx.SendBatch(ctx, b)
	for range b.Len() {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	return br.Close()
}

func queueIndex(b *pgx.Batch, item ids.UUID, module string, scopes []string, c indexCols) {
	if len(c.fields) == 0 {
		return
	}
	b.Queue(`INSERT INTO field_index (item_id, scope, module, locale, field, v_text, v_num, v_time, v_bool, v_ref)
		SELECT $1, sc, $2, l, f, t, n, ts, b, r
		FROM unnest($3::text[]) AS s(sc),
			unnest($4::text[], $5::text[], $6::text[], $7::float8[], $8::timestamptz[], $9::bool[], $10::uuid[])
			AS u(l, f, t, n, ts, b, r)`, uid(item), module, scopes, c.locales, c.fields, c.texts, c.nums, c.times, c.bools, c.refs)
}

func (s contentStore) ReplaceSearch(ctx context.Context, item ids.UUID, proj store.Projection, docs []store.SearchDoc) error {
	scope, err := scopeOf(proj)
	if err != nil {
		return err
	}
	b := &pgx.Batch{}
	b.Queue(`DELETE FROM content_search WHERE item_id = $1 AND scope = $2`, uid(item), scope)
	if len(docs) > 0 {
		// left(…, 100000) = storeutil.MaxSearchChars: the statement bounds its input even if a caller skips the Go cap.
		locales, plain := storeutil.SearchTexts(docs)
		b.Queue(`INSERT INTO content_search (item_id, scope, locale, tsv, plain)
		SELECT $1, $4, l, to_tsvector('simple', left(t, 100000)), left(t, 100000)
		FROM unnest($2::text[], $3::text[]) AS u(l, t)`,
			uid(item), locales, plain, scope)
	}
	if err := s.sendBatch(ctx, b); err != nil {
		return fmt.Errorf("postgres: replace search %s: %w", item, err)
	}
	return nil
}

// Content migrations

func (s contentStore) AppliedContentMigrations(ctx context.Context) ([]string, error) {
	rows, err := s.tx.Query(ctx, `SELECT name FROM content_migration ORDER BY name COLLATE "C"`)
	if err != nil {
		return nil, fmt.Errorf("postgres: applied content migrations: %w", err)
	}
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("postgres: applied content migrations: %w", err)
	}
	return out, nil
}

func (s contentStore) RecordContentMigration(ctx context.Context, name string, at time.Time) error {
	if _, err := s.tx.Exec(ctx, `INSERT INTO content_migration (name, applied_at) VALUES ($1, $2)
		ON CONFLICT (name) DO NOTHING`, name, tz(at)); err != nil {
		return fmt.Errorf("postgres: record content migration %q: %w", name, err)
	}
	return nil
}

func (s contentStore) ItemIDsOfType(ctx context.Context, typ string, after ids.UUID, limit int) ([]ids.UUID, error) {
	rows, err := s.tx.Query(ctx, `SELECT id FROM item WHERE type = $1 AND id > $2 ORDER BY id LIMIT $3`,
		typ, uid(after), limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: item ids of type %q: %w", typ, err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ids.UUID, error) {
		var id [16]byte
		err := r.Scan(&id)
		return id, err
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: item ids of type %q: %w", typ, err)
	}
	return out, nil
}
