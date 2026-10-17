package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

var sqlTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"SQLTypes", testSQLTypes},
	{"SQLNulls", testSQLNulls},
	{"SQLPlaceholders", testSQLPlaceholders},
	{"SQLRowsAffected", testSQLRowsAffected},
	{"SQLNotFound", testSQLNotFound},
	{"SQLConstraints", testSQLConstraints},
	{"SQLQueryIteration", testSQLQueryIteration},
	{"SQLScanRetention", testSQLScanRetention},
	{"SQLViewAndTx", testSQLViewAndTx},
	{"SQLTxError", testSQLTxError},
	{"SQLRead", testSQLRead},
}

// sqlDDL is the module migration of the SQL tests, per dialect.
var sqlDDL = map[string]string{
	"sqlite": `CREATE TABLE storetest_sql (
		id     BLOB PRIMARY KEY,
		name   TEXT NOT NULL UNIQUE,
		n      INTEGER,
		f      REAL,
		flag   INTEGER,
		ts     INTEGER,
		doc    TEXT,
		data   BLOB,
		parent BLOB REFERENCES storetest_sql (id),
		qty    INTEGER CONSTRAINT storetest_qty_nonneg CHECK (qty >= 0)
	);`,
	"postgres": `CREATE TABLE storetest_sql (
		id     uuid PRIMARY KEY,
		name   text NOT NULL UNIQUE,
		n      bigint,
		f      double precision,
		flag   boolean,
		ts     timestamptz,
		doc    jsonb,
		data   bytea,
		parent uuid REFERENCES storetest_sql (id),
		qty    integer CONSTRAINT storetest_qty_nonneg CHECK (qty >= 0)
	);`,
}

const insertRow = `INSERT INTO storetest_sql (id, name, n, f, flag, ts, doc, data, parent, qty) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// sqlDB migrates the SQL tests' table and returns the generator for row ids.
func sqlDB(t *testing.T, db store.DB) *ids.Generator {
	t.Helper()
	ddl, ok := sqlDDL[db.Dialect()]
	if !ok {
		t.Skipf("no SQL test DDL for dialect %q", db.Dialect())
	}
	set := store.MigrationSet{Source: "storetest_sql", FS: fstest.MapFS{"0001_sql.sql": {Data: []byte(ddl)}}}
	if err := db.Migrate(context.Background(), []store.MigrationSet{set}); err != nil {
		t.Fatal(err)
	}
	return newIDs()
}

func inSQL(t *testing.T, db store.DB, fn func(store.SQL) error) {
	t.Helper()
	if err := db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.SQL()) }); err != nil {
		t.Fatal(err)
	}
}

func txErr(db store.DB, fn func(store.SQL) error) error {
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.SQL()) })
}

func testSQLTypes(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	id, parent := g.New(), g.New()
	// Sub-millisecond precision and a zone: storage keeps milliseconds, times come back in UTC.
	at := time.Date(2026, 10, 14, 11, 30, 15, 123_456_789, time.FixedZone("x", 2*3600))
	doc := json.RawMessage(`{"a": [1, 2], "b": "x"}`)
	data := []byte{0, 1, 2, 255}

	inSQL(t, db, func(q store.SQL) error {
		if _, err := q.Exec(ctx, insertRow, parent, "parent", 1, 1.5, false, at, nil, nil, nil, 0); err != nil {
			return err
		}
		_, err := q.Exec(ctx, insertRow, id, "all", int64(1)<<40, 2.25, true, at, doc, data, &parent, 7)
		return err
	})

	var (
		gotID, gotParent ids.UUID
		n                int64
		f                float64
		flag             bool
		ts               time.Time
		gotDoc           json.RawMessage
		gotData          []byte
		name             string
		qty              int
	)
	err := db.View(ctx, func(tx store.Tx) error {
		return tx.SQL().QueryRow(ctx, `SELECT id, name, n, f, flag, ts, doc, data, parent, qty FROM storetest_sql WHERE id = ?`, id).
			Scan(&gotID, &name, &n, &f, &flag, &ts, &gotDoc, &gotData, &gotParent, &qty)
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotID != id || gotParent != parent || name != "all" || n != 1<<40 || f != 2.25 || !flag || qty != 7 {
		t.Errorf("scalars: %v %v %q %d %v %v %d", gotID, gotParent, name, n, f, flag, qty)
	}
	if want := at.UTC().Truncate(time.Millisecond); !ts.Equal(want) || ts.Location() != time.UTC {
		t.Errorf("time = %v (%v), want %v UTC", ts, ts.Location(), want)
	}
	if !jsonEqual(gotDoc, string(doc)) {
		t.Errorf("doc = %s", gotDoc)
	}
	if string(gotData) != string(data) {
		t.Errorf("data = %v", gotData)
	}

	// Pointers to non-NULL values bind and scan like the values, through pointer-to-pointer destinations.
	var pID, pParent *ids.UUID
	var pName *string
	var pN *int64
	var pF *float64
	var pFlag *bool
	var pTs *time.Time
	var pDoc *json.RawMessage
	var pData *[]byte
	var pQty *int
	err = db.View(ctx, func(tx store.Tx) error {
		return tx.SQL().QueryRow(ctx, `SELECT id, name, n, f, flag, ts, doc, data, parent, qty FROM storetest_sql WHERE id = ?`, &id).
			Scan(&pID, &pName, &pN, &pF, &pFlag, &pTs, &pDoc, &pData, &pParent, &pQty)
	})
	if err != nil {
		t.Fatal(err)
	}
	if pID == nil || *pID != id || pParent == nil || *pParent != parent || *pName != "all" || *pN != 1<<40 ||
		*pF != 2.25 || !*pFlag || *pQty != 7 || !pTs.Equal(ts) || pTs.Location() != time.UTC ||
		!jsonEqual(*pDoc, string(doc)) || string(*pData) != string(data) {
		t.Errorf("pointer scan: %v %v %v %v", pID, pName, pTs, pQty)
	}
}

func testSQLNulls(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	id := g.New()
	var (
		nilUUID *ids.UUID
		nilInt  *int64
		nilF    *float64
		nilBool *bool
		nilTime *time.Time
		nilDoc  *json.RawMessage
		nilData *[]byte
		nilQty  *int
	)
	inSQL(t, db, func(q store.SQL) error {
		_, err := q.Exec(ctx, insertRow, id, "nulls", nilInt, nilF, nilBool, nilTime, nilDoc, nilData, nilUUID, nilQty)
		return err
	})
	// Untyped nil and nil slices are NULL too.
	id2 := g.New()
	inSQL(t, db, func(q store.SQL) error {
		_, err := q.Exec(ctx, insertRow, id2, "nulls2", nil, nil, nil, nil, json.RawMessage(nil), []byte(nil), nil, nil)
		return err
	})

	for _, rid := range []ids.UUID{id, id2} {
		var (
			pParent *ids.UUID
			pN      *int64
			pF      *float64
			pFlag   *bool
			pTs     *time.Time
			pDoc    *json.RawMessage
			pData   *[]byte
			pQty    *int
			pStr    *string
		)
		err := db.View(ctx, func(tx store.Tx) error {
			return tx.SQL().QueryRow(ctx, `SELECT n, f, flag, ts, doc, data, parent, qty, CAST(NULL AS TEXT) FROM storetest_sql WHERE id = ?`, rid).
				Scan(&pN, &pF, &pFlag, &pTs, &pDoc, &pData, &pParent, &pQty, &pStr)
		})
		if err != nil {
			t.Fatal(err)
		}
		if pN != nil || pF != nil || pFlag != nil || pTs != nil || pDoc != nil || pData != nil || pParent != nil || pQty != nil || pStr != nil {
			t.Errorf("NULLs came back as values: %v %v %v %v %v %v %v %v", pN, pF, pFlag, pTs, pDoc, pData, pParent, pQty)
		}
	}

	// NULL into a plain destination is an error, not a zero value.
	for name, dest := range map[string]any{"uuid": new(ids.UUID), "time": new(time.Time), "string": new(string),
		"int": new(int64), "bool": new(bool), "float": new(float64)} {
		col := map[string]string{"uuid": "parent", "time": "ts", "string": "CAST(NULL AS TEXT)", "int": "n", "bool": "flag", "float": "f"}[name]
		err := db.View(ctx, func(tx store.Tx) error {
			return tx.SQL().QueryRow(ctx, `SELECT `+col+` FROM storetest_sql WHERE id = ?`, id).Scan(dest)
		})
		if err == nil {
			t.Errorf("NULL scanned into %s without an error", name)
		}
	}
}

func testSQLPlaceholders(t *testing.T, db store.DB) {
	ctx := context.Background()
	sqlDB(t, db)
	var a, b string
	var lit string
	err := db.View(ctx, func(tx store.Tx) error {
		return tx.SQL().QueryRow(ctx, `SELECT CAST(? AS TEXT) AS "q?", 'it''s ? ' /* ? */, CAST(? AS TEXT) -- ?`, "one", "two").
			Scan(&a, &lit, &b)
	})
	if err != nil {
		t.Fatal(err)
	}
	if a != "one" || lit != "it's ? " || b != "two" {
		t.Errorf("got %q %q %q", a, lit, b)
	}
}

func testSQLRowsAffected(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	inSQL(t, db, func(q store.SQL) error {
		for _, name := range []string{"a", "b", "c"} {
			n, err := q.Exec(ctx, `INSERT INTO storetest_sql (id, name, qty) VALUES (?, ?, ?)`, g.New(), name, 1)
			if err != nil || n != 1 {
				t.Errorf("insert %s: %d %v", name, n, err)
			}
		}
		if n, err := q.Exec(ctx, `UPDATE storetest_sql SET qty = ? WHERE name IN (?, ?)`, 5, "a", "b"); err != nil || n != 2 {
			t.Errorf("update: %d %v", n, err)
		}
		if n, err := q.Exec(ctx, `UPDATE storetest_sql SET qty = 1 WHERE name = ?`, "none"); err != nil || n != 0 {
			t.Errorf("update none: %d %v", n, err)
		}
		if n, err := q.Exec(ctx, `DELETE FROM storetest_sql`); err != nil || n != 3 {
			t.Errorf("delete: %d %v", n, err)
		}
		return nil
	})
}

func testSQLNotFound(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	err := db.View(ctx, func(tx store.Tx) error {
		var n int
		return tx.SQL().QueryRow(ctx, `SELECT qty FROM storetest_sql WHERE id = ?`, g.New()).Scan(&n)
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	// An aggregate always returns a row.
	inSQL(t, db, func(q store.SQL) error {
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM storetest_sql`).Scan(&n); err != nil || n != 0 {
			t.Errorf("count: %d %v", n, err)
		}
		return nil
	})
}

func testSQLConstraints(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	id := g.New()
	inSQL(t, db, func(q store.SQL) error {
		_, err := q.Exec(ctx, `INSERT INTO storetest_sql (id, name, qty) VALUES (?, ?, ?)`, id, "first", 1)
		return err
	})
	cases := []struct {
		name    string
		query   string
		args    []any
		want    error
		mention string // part of the driver's message that must survive
	}{
		{"duplicate primary key", `INSERT INTO storetest_sql (id, name) VALUES (?, ?)`, []any{id, "other"}, store.ErrDuplicateKey, "storetest_sql"},
		{"duplicate unique column", `INSERT INTO storetest_sql (id, name) VALUES (?, ?)`, []any{g.New(), "first"}, store.ErrDuplicateKey, "name"},
		{"foreign key", `INSERT INTO storetest_sql (id, name, parent) VALUES (?, ?, ?)`, []any{g.New(), "orphan", g.New()}, store.ErrConstraint, "constraint"},
		{"check", `INSERT INTO storetest_sql (id, name, qty) VALUES (?, ?, ?)`, []any{g.New(), "neg", -1}, store.ErrConstraint, "storetest_qty_nonneg"},
		{"not null", `INSERT INTO storetest_sql (id, name) VALUES (?, ?)`, []any{g.New(), nil}, store.ErrConstraint, "name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, via := range []string{"Exec", "QueryRow"} {
				err := txErr(db, func(q store.SQL) error {
					if via == "Exec" {
						_, err := q.Exec(ctx, tc.query, tc.args...)
						return err
					}
					var got ids.UUID
					return q.QueryRow(ctx, tc.query+` RETURNING id`, tc.args...).Scan(&got)
				})
				if !errors.Is(err, tc.want) {
					t.Fatalf("%s: err = %v, want %v", via, err, tc.want)
				}
				other := store.ErrConstraint
				if tc.want == store.ErrConstraint {
					other = store.ErrDuplicateKey
				}
				if errors.Is(err, other) {
					t.Errorf("%s: err also matches %v", via, other)
				}
				if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tc.mention)) {
					t.Errorf("%s: message %q lost the detail %q", via, err, tc.mention)
				}
			}
		})
	}
}

func testSQLQueryIteration(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	inSQL(t, db, func(q store.SQL) error {
		for i := range 5 {
			if _, err := q.Exec(ctx, `INSERT INTO storetest_sql (id, name, n) VALUES (?, ?, ?)`, g.New(), string(rune('a'+i)), i); err != nil {
				return err
			}
		}
		rows, err := q.Query(ctx, `SELECT name, n FROM storetest_sql WHERE n >= ? ORDER BY n`, 1)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var name string
			var n int
			if err := rows.Scan(&name, &n); err != nil {
				return err
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if strings.Join(names, "") != "bcde" {
			t.Errorf("names = %v", names)
		}

		// Closing early frees the transaction for the next statement.
		rows, err = q.Query(ctx, `SELECT name FROM storetest_sql ORDER BY name`)
		if err != nil {
			return err
		}
		if !rows.Next() {
			t.Error("no first row")
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if rows.Next() {
			t.Error("Next after Close")
		}
		if err := rows.Err(); err != nil {
			t.Errorf("Err after early Close: %v", err)
		}
		var n int
		return q.QueryRow(ctx, `SELECT count(*) FROM storetest_sql`).Scan(&n)
	})

	// A failing statement reports through Query or Err, never silently.
	err := txErr(db, func(q store.SQL) error {
		rows, err := q.Query(ctx, `SELECT * FROM storetest_missing_table`)
		if err != nil {
			return err
		}
		for rows.Next() {
		}
		defer func() { _ = rows.Close() }()
		return rows.Err()
	})
	if err == nil {
		t.Error("query on a missing table succeeded")
	}
}

func testSQLViewAndTx(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	id := g.New()
	inSQL(t, db, func(q store.SQL) error {
		_, err := q.Exec(ctx, `INSERT INTO storetest_sql (id, name, qty) VALUES (?, ?, ?)`, id, "view", 3)
		return err
	})
	err := db.View(ctx, func(tx store.Tx) error {
		var qty int
		if err := tx.SQL().QueryRow(ctx, `SELECT qty FROM storetest_sql WHERE id = ?`, id).Scan(&qty); err != nil || qty != 3 {
			t.Errorf("view read: %d %v", qty, err)
		}
		_, err := tx.SQL().Exec(ctx, `UPDATE storetest_sql SET qty = 4 WHERE id = ?`, id)
		return err
	})
	if err == nil {
		t.Error("write in View succeeded")
	}

	// The module table and the core stores share one transaction: a rollback undoes both.
	boom := errors.New("boom")
	err = db.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.SQL().Exec(ctx, `UPDATE storetest_sql SET qty = 9 WHERE id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.Audit().Write(ctx, store.AuditEvent{At: t0, ActorKind: "system", Action: "sql.test", Subject: "x", Data: []byte(`{}`)}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	_ = db.View(ctx, func(tx store.Tx) error {
		var qty int
		if err := tx.SQL().QueryRow(ctx, `SELECT qty FROM storetest_sql WHERE id = ?`, id).Scan(&qty); err != nil || qty != 3 {
			t.Errorf("after rollback: %d %v", qty, err)
		}
		if evs, err := tx.Audit().List(ctx, store.AuditQuery{Action: "sql.test"}); err != nil || len(evs) != 0 {
			t.Errorf("audit after rollback: %d %v", len(evs), err)
		}
		return nil
	})
}

func testSQLTxError(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	id := g.New()
	err := txErr(db, func(q store.SQL) error {
		if _, err := q.Exec(ctx, `INSERT INTO storetest_sql (id, name) VALUES (?, ?)`, id, "before"); err != nil {
			return err
		}
		_, err := q.Exec(ctx, `INSERT INTO storetest_sql (id, name) VALUES (?, ?)`, id, "dup")
		return err
	})
	if !errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("err = %v, want ErrDuplicateKey", err)
	}
	err = txErr(db, func(q store.SQL) error {
		_, err := q.Exec(ctx, `NOT SQL AT ALL ?`, 1)
		return err
	})
	if err == nil || errors.Is(err, store.ErrConstraint) || errors.Is(err, store.ErrDuplicateKey) {
		t.Fatalf("syntax error = %v", err)
	}
	inSQL(t, db, func(q store.SQL) error {
		var n int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM storetest_sql`).Scan(&n); err != nil || n != 0 {
			t.Errorf("rows after failed tx: %d %v", n, err)
		}
		return nil
	})
}

// testSQLScanRetention keeps byte-backed scan results from many rows and checks them after iteration: drivers
// may reuse their row buffers, so adapters must copy.
func testSQLScanRetention(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	const rows = 50
	inSQL(t, db, func(q store.SQL) error {
		for i := range rows {
			doc := json.RawMessage(fmt.Sprintf(`{"i":%d,"pad":%q}`, i, strings.Repeat("x", 2000+i))) // larger than the driver buffer
			if _, err := q.Exec(ctx, insertRow, g.New(), fmt.Sprintf("row%03d", i), i, nil, nil, nil, doc,
				[]byte(fmt.Sprintf("data-%d", i)), nil, nil); err != nil {
				return err
			}
		}
		it, err := q.Query(ctx, `SELECT name, doc, doc, data, data FROM storetest_sql ORDER BY name`)
		if err != nil {
			return err
		}
		type kept struct {
			name    *string
			doc     json.RawMessage
			optDoc  *json.RawMessage
			data    []byte
			optData *[]byte
		}
		var all []kept
		for it.Next() {
			var k kept
			if err := it.Scan(&k.name, &k.doc, &k.optDoc, &k.data, &k.optData); err != nil {
				return err
			}
			all = append(all, k)
		}
		if err := it.Err(); err != nil {
			return err
		}
		if err := it.Close(); err != nil {
			return err
		}
		if len(all) != rows {
			t.Fatalf("got %d rows, want %d", len(all), rows)
		}
		for i, k := range all {
			var d struct{ I int }
			if err := json.Unmarshal(k.doc, &d); err != nil || d.I != i {
				t.Errorf("row %d: doc %q kept badly: %v", i, k.doc, err)
			}
			if k.optDoc == nil || !jsonEqual(*k.optDoc, string(k.doc)) {
				t.Errorf("row %d: nullable doc kept badly", i)
			}
			want := fmt.Sprintf("data-%d", i)
			if string(k.data) != want || k.optData == nil || string(*k.optData) != want {
				t.Errorf("row %d: data kept badly", i)
			}
			if k.name == nil || *k.name != fmt.Sprintf("row%03d", i) {
				t.Errorf("row %d: name kept badly", i)
			}
		}
		return nil
	})
}

func testSQLRead(t *testing.T, db store.DB) {
	ctx := context.Background()
	g := sqlDB(t, db)
	id, parent := g.New(), g.New()
	ts := t0.Add(1234 * time.Millisecond)
	inSQL(t, db, func(q store.SQL) error {
		if _, err := q.Exec(ctx, insertRow, parent, "parent", nil, nil, nil, nil, nil, nil, nil, 1); err != nil {
			return err
		}
		_, err := q.Exec(ctx, insertRow, id, "read", 7, 1.5, true, ts, json.RawMessage(`{"a":1}`), []byte{1, 2}, &parent, 2)
		return err
	})
	r := db.Read()
	var (
		gotID  ids.UUID
		n      int
		flag   bool
		gotTS  time.Time
		doc    json.RawMessage
		data   []byte
		gotPar *ids.UUID
	)
	if err := r.QueryRow(ctx, `SELECT id, n, flag, ts, doc, data, parent FROM storetest_sql WHERE name = ?`, "read").
		Scan(&gotID, &n, &flag, &gotTS, &doc, &data, &gotPar); err != nil {
		t.Fatal(err)
	}
	if gotID != id || n != 7 || !flag || !gotTS.Equal(ts) || gotTS.Location() != time.UTC || !jsonEqual(doc, `{"a":1}`) ||
		len(data) != 2 || gotPar == nil || *gotPar != parent {
		t.Fatalf("read: %v %d %v %v %s %v %v", gotID, n, flag, gotTS, doc, data, gotPar)
	}
	rows, err := r.Query(ctx, `SELECT name FROM storetest_sql WHERE id = ? OR id = ? ORDER BY name`, id, parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		names = append(names, s)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil || len(names) != 2 {
		t.Fatalf("rows: %v %v", names, err)
	}
	if err := r.QueryRow(ctx, `SELECT qty FROM storetest_sql WHERE id = ?`, g.New()).Scan(&n); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("missing row: %v", err)
	}
	if _, err := r.Exec(ctx, `UPDATE storetest_sql SET qty = 5 WHERE id = ?`, id); err == nil {
		t.Error("Exec on Read succeeded")
	}
	// A write committed by Tx is visible to a following Read.
	inSQL(t, db, func(q store.SQL) error {
		_, err := q.Exec(ctx, `UPDATE storetest_sql SET qty = 9 WHERE id = ?`, id)
		return err
	})
	var qty int
	if err := db.Read().QueryRow(ctx, `SELECT qty FROM storetest_sql WHERE id = ?`, id).Scan(&qty); err != nil || qty != 9 {
		t.Errorf("after commit: %d %v", qty, err)
	}
}
