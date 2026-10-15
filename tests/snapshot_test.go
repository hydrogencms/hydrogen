package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// snapshotsOf reads the snapshots of items in every locale, keyed by item and locale.
func (e *env) snapshotsOf(items ...ids.UUID) map[string]store.Snapshot {
	e.t.Helper()
	got, err := e.db.Reader().Snapshots(ctx, items, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	m := map[string]store.Snapshot{}
	for _, s := range got {
		m[s.ItemID.String()+"/"+s.Locale] = s
	}
	return m
}

func (e *env) snapshot(id ids.UUID, locale string) store.Snapshot {
	e.t.Helper()
	s, ok := e.snapshotsOf(id)[id.String()+"/"+locale]
	if !ok {
		e.t.Fatalf("no %s snapshot of %s", locale, id)
	}
	return s
}

// checkSnapshotsComplete verifies that every published item has snapshots for exactly its published locales, and
// that each snapshot is the right version with a valid payload and ETag.
func (e *env) checkSnapshotsComplete() {
	e.t.Helper()
	res, err := e.svc.List(ctx, content.ListOptions{Status: store.StatusPublished, Limit: 500})
	if err != nil {
		e.t.Fatal(err)
	}
	var all []ids.UUID
	for _, it := range res.Items {
		all = append(all, it.ID)
	}
	snaps := e.snapshotsOf(all...)
	want := 0
	for _, it := range res.Items {
		for _, l := range it.Locales {
			want++
			s, ok := snaps[it.ID.String()+"/"+l]
			if !ok {
				e.t.Errorf("item %s (%s) has no %s snapshot", it.ID, it.Type, l)
				continue
			}
			sum := sha256.Sum256(s.Payload)
			if s.Version != it.Version || s.Type != it.Type || s.Key != it.Key || s.ETag != hex.EncodeToString(sum[:]) || !json.Valid(s.Payload) {
				e.t.Errorf("snapshot %s/%s = v%d %s key %q etag %s; item v%d %s key %q", it.ID, l, s.Version, s.Type, s.Key, s.ETag, it.Version, it.Type, it.Key)
			}
		}
	}
	if len(snaps) != want {
		e.t.Errorf("%d snapshots for %d published locales", len(snaps), want)
	}
}

func TestSnapshotPayload(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		author := e.person("Ada")
		editor := e.person("Bob")
		other := e.article("Other")
		key := "hello"
		e.tick()
		v, err := e.svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en", Key: key, Fields: doc(t, fmt.Sprintf(
			`{"tags":["b","a"],"address":{"city":"Paris","street":"Rue 1"},"rating":4,"title":"Hello <World> & more","author":%q,
			"editor":%q,"related":[%q,%q],"summary":"Quote \" é",
			"body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"x","marks":[{"type":"link","attrs":{"href":"item:%s"}}]}]}]}}`,
			author.ID, editor.ID, other.ID, author.ID, other.ID))})
		if err != nil {
			t.Fatal(err)
		}
		e.update(v.ID, "de", `{"title":"Hallo"}`)
		pub := e.publish(v.ID)
		eqSlice(t, "locales", pub.Locales, []string{"de", "en"})
		en := e.snapshot(v.ID, "en")
		p := string(en.Payload)
		if !bytes.HasPrefix(en.Payload, []byte(fmt.Sprintf(`{"id":%q,"type":"article","key":"hello","locale":"en","version":1,"publishedAt":"`, v.ID))) {
			t.Fatalf("payload head: %s", p)
		}
		var m struct {
			PublishedAt string          `json:"publishedAt"`
			Name        string          `json:"name"`
			Fields      json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(en.Payload, &m); err != nil {
			t.Fatal(err)
		}
		if _, err := time.Parse("2006-01-02T15:04:05.000Z", m.PublishedAt); err != nil {
			t.Errorf("publishedAt %q: %v", m.PublishedAt, err)
		}
		eq(t, "publishedAt", m.PublishedAt, en.PublishedAt.UTC().Format("2006-01-02T15:04:05.000Z"))
		eq(t, "name", m.Name, "Hello <World> & more")
		// Fields follow the schema order, groups included, and the characters are not HTML-escaped.
		order := []string{`"title":`, `"slug":`, `"summary":`, `"body":`, `"kind":`, `"rating":4`, `"featured":`, `"author":`, `"editor":`,
			`"related":`, `"tags":["b","a"]`, `"address":{"street":"Rue 1","city":"Paris"}`}
		pos := 0
		for _, o := range order {
			i := bytes.Index(m.Fields[pos:], []byte(o))
			if i < 0 {
				t.Fatalf("fields lack %s after offset %d: %s", o, pos, m.Fields)
			}
			pos += i + len(o)
		}
		if !bytes.Contains(en.Payload, []byte(`"title":"Hello <World> & more"`)) || !bytes.Contains(en.Payload, []byte(`"summary":"Quote \" é"`)) {
			t.Errorf("escaping: %s", p)
		}
		sum := sha256.Sum256(en.Payload)
		eq(t, "etag", en.ETag, hex.EncodeToString(sum[:]))
		eq(t, "key", en.Key, "hello")
		eq(t, "type", en.Type, "article")
		eq(t, "main locale", en.MainLocale, "en")
		eq(t, "version", en.Version, 1)
		want := []ids.UUID{author.ID, editor.ID, other.ID}
		slices.SortFunc(want, func(a, b ids.UUID) int { return bytes.Compare(a[:], b[:]) })
		if !slices.Equal(en.Refs, want) {
			t.Errorf("refs = %v, want %v", en.Refs, want)
		}
		de := e.snapshot(v.ID, "de")
		if !bytes.Contains(de.Payload, []byte(`"locale":"de"`)) || !bytes.Contains(de.Payload, []byte(`"name":"Hallo"`)) {
			t.Errorf("de payload: %s", de.Payload)
		}
		// An item without a key has no key member.
		noKey := e.snapshot(other.ID, "en")
		if bytes.Contains(noKey.Payload, []byte(`"key"`)) {
			t.Errorf("keyless payload has a key: %s", noKey.Payload)
		}
		e.checkSnapshotsComplete()
	})
}

func TestSnapshotNumbersKeepDigits(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{schema: `
types:
  measure:
    name: "{label}"
    fields:
      label: { type: string }
      value: { type: number }
      big:   { type: number }
`})
		v := e.create("measure", "en", `{"label":"m","value":1.50,"big":12345678901234567890123}`)
		e.publish(v.ID)
		s := e.snapshot(v.ID, "en")
		if !bytes.Contains(s.Payload, []byte(`"value":1.50,"big":12345678901234567890123}`)) {
			t.Errorf("numbers changed: %s", s.Payload)
		}
	})
}

func TestSnapshotLifecycle(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("One")
		eqSlice(t, "locale count", []string{fmt.Sprint(len(e.snapshotsOf(a.ID)))}, []string{"1"})
		e.update(a.ID, "en", `{"title":"One again"}`)
		eq(t, "unpublished edit leaves the snapshot", e.snapshot(a.ID, "en").Version, 1)
		e.update(a.ID, "fr", `{"title":"Un"}`)
		e.publish(a.ID, "fr")
		got := e.snapshotsOf(a.ID)
		eq(t, "locales after partial publish", len(got), 2)
		eq(t, "en version", got[a.ID.String()+"/en"].Version, 2)
		if !bytes.Contains(got[a.ID.String()+"/en"].Payload, []byte(`"title":"One"`)) {
			t.Errorf("en payload changed by publishing fr: %s", got[a.ID.String()+"/en"].Payload)
		}
		e.publish(a.ID)
		if !bytes.Contains(e.snapshot(a.ID, "en").Payload, []byte(`"title":"One again"`)) {
			t.Error("en snapshot did not follow the publish")
		}
		// Changing the key of a published item reaches the snapshots without a publish.
		key := "uno"
		e.tick()
		if _, err := e.svc.Update(ctx, a.ID, content.UpdateInput{Key: &key}); err != nil {
			t.Fatal(err)
		}
		for _, l := range []string{"en", "fr"} {
			s := e.snapshot(a.ID, l)
			if s.Key != "uno" || !bytes.Contains(s.Payload, []byte(`"key":"uno"`)) {
				t.Errorf("%s snapshot after key change: %q %s", l, s.Key, s.Payload)
			}
			eq(t, l+" version", s.Version, 3)
		}
		byKey, err := e.db.Reader().SnapshotsByKey(ctx, "uno", []string{"fr"})
		if err != nil || len(byKey) != 1 || byKey[0].ItemID != a.ID {
			t.Fatalf("by key = %+v, %v", byKey, err)
		}
		e.checkSnapshotsComplete()
		if err := e.svc.Delete(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		eq(t, "snapshots after delete", len(e.snapshotsOf(a.ID)), 0)
	})
}

func TestSnapshotNullifyRepublish(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		ed := e.person("Eve")
		v := e.create("article", "en", fmt.Sprintf(`{"title":"T","editor":%q}`, ed.ID))
		e.publish(v.ID)
		s := e.snapshot(v.ID, "en")
		if !slices.Equal(s.Refs, []ids.UUID{ed.ID}) || !bytes.Contains(s.Payload, []byte(ed.ID.String())) {
			t.Fatalf("before: refs %v %s", s.Refs, s.Payload)
		}
		if err := e.svc.Delete(ctx, ed.ID); err != nil {
			t.Fatal(err)
		}
		s = e.snapshot(v.ID, "en")
		eq(t, "version", s.Version, 2)
		if len(s.Refs) != 0 || bytes.Contains(s.Payload, []byte(ed.ID.String())) || bytes.Contains(s.Payload, []byte(`"editor"`)) {
			t.Errorf("after nullify: refs %v %s", s.Refs, s.Payload)
		}
		e.checkSnapshotsComplete()
	})
}

func TestSnapshotContentMigration(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		old := newEnv(t, db, clk, envConfig{})
		p := old.person("Ada Lovelace")
		const v2 = `
types:
  person:
    name: "{fullName}"
    fields:
      fullName: { type: string, required: true }
      shout:    { type: string }
`
		m := content.Migration{Name: "0001_shout", Type: "person", Up: func(d *content.MigrationDoc) error {
			d.Fields["shout"] = "ADA"
			return nil
		}}
		e := newEnv(t, db, clk, envConfig{schema: v2, migrations: []content.Migration{m}})
		if _, err := e.svc.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		s := e.snapshot(p.ID, "en")
		eq(t, "version", s.Version, 2)
		if !bytes.Contains(s.Payload, []byte(`"shout":"ADA"`)) {
			t.Errorf("migrated payload: %s", s.Payload)
		}
		e.checkSnapshotsComplete()
	})
}

func TestRebuildSnapshots(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		a := e.article("A")
		e.update(a.ID, "de", `{"title":"Ah"}`)
		e.publish(a.ID)
		draftOnly := e.create("article", "en", `{"title":"Draft"}`)
		before := e.snapshotsOf(a.ID)
		// Lose the snapshots, then rebuild them from the versions.
		if err := e.db.Tx(ctx, func(tx store.Tx) error {
			if err := tx.Content().DeleteSnapshots(ctx, a.ID); err != nil {
				return err
			}
			return tx.Content().PutSnapshots(ctx, draftOnly.ID, []store.Snapshot{{ItemID: draftOnly.ID, Locale: "en", Type: "article",
				MainLocale: "en", Payload: []byte(`{}`), ETag: "x", PublishedAt: e.clk.Now()}})
		}); err != nil {
			t.Fatal(err)
		}
		eq(t, "lost", len(e.snapshotsOf(a.ID)), 0)
		if err := rebuildItems(e.db, e.svc, a.ID, draftOnly.ID); err != nil {
			t.Fatal(err)
		}
		after := e.snapshotsOf(a.ID, draftOnly.ID)
		eq(t, "rebuilt count", len(after), 2)
		for k, s := range before {
			if !bytes.Equal(after[k].Payload, s.Payload) || after[k].ETag != s.ETag || !after[k].PublishedAt.Equal(s.PublishedAt) {
				t.Errorf("%s differs after rebuild:\n%s\n%s", k, s.Payload, after[k].Payload)
			}
		}
		if err := rebuildItems(e.db, e.svc, newIDs()); err == nil {
			t.Error("rebuilding an unknown item succeeded")
		}
	})
}

func newIDs() ids.UUID { return ids.NewGenerator(newClock(), nil).New() }

func BenchmarkPublishPostgres(b *testing.B) {
	clk := newClock()
	e := newEnv(b, openPostgres(b, clk), clk, envConfig{schema: benchSchema()})
	v, err := e.svc.Create(ctx, content.CreateInput{Type: "doc", Fields: benchFields(0)})
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := range b.N {
		b.StopTimer()
		if _, err := e.svc.Update(ctx, v.ID, content.UpdateInput{Fields: benchFields(i + 1)}); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		if _, err := e.svc.Publish(ctx, v.ID, content.PublishOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}
