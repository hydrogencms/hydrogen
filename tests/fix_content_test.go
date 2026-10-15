package tests

import (
	"bytes"
	"context"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/store"
)

// rebuildItems rebuilds snapshots and projections of the items from their published versions, as an import does.
func rebuildItems(db store.DB, svc *content.Service, list ...ids.UUID) error {
	return db.Tx(context.Background(), func(tx store.Tx) error {
		c := tx.Content()
		for _, id := range list {
			it, err := c.GetItem(context.Background(), id, true)
			if err != nil {
				return err
			}
			if it.PublishedVersion == 0 {
				if err := c.DeleteSnapshots(context.Background(), id); err != nil {
					return err
				}
				continue
			}
			v, err := c.GetVersion(context.Background(), id, it.PublishedVersion)
			if err != nil {
				return err
			}
			if err := svc.RebuildProjections(context.Background(), c, it, &v); err != nil {
				return err
			}
		}
		return nil
	})
}

// stampHook sets a shared field (rating) and a translatable one (summary) on publish.
func stampHook(r interface{ Add(hooks.Hook) error }) error {
	return r.Add(hooks.Hook{Type: "article", Event: hooks.BeforePublish, Fn: func(c *hooks.Context) error {
		c.Doc["rating"] = 4
		c.Doc["summary"] = "stamped " + c.Locale
		return nil
	}})
}

type stampModule struct{}

func (stampModule) Name() string { return "stamp" }
func (stampModule) Register(r *kernel.Registry) error {
	return r.AddHook(hooks.Hook{Type: "article", Event: hooks.BeforePublish, Fn: func(c *hooks.Context) error {
		c.Doc["rating"] = 4
		c.Doc["summary"] = "stamped " + c.Locale
		return nil
	}})
}

func TestBeforePublishChangesArePersisted(t *testing.T) {
	run2 := func(t *testing.T, b backend) {
		e := newEnvFor(t, b, envConfig{setup: func(r *hooks.Registry) {
			if err := stampHook(r); err != nil {
				t.Fatal(err)
			}
		}})
		a := e.article("A")
		e.update(a.ID, "de", `{"title":"Ah"}`)
		e.publish(a.ID)
		before := e.snapshotsOf(a.ID)
		if len(before) != 2 {
			t.Fatalf("snapshots: %d", len(before))
		}
		for k, s := range before {
			loc := k[strings.Index(k, "/")+1:]
			if !bytes.Contains(s.Payload, []byte(`"summary":"stamped `+loc+`"`)) || !bytes.Contains(s.Payload, []byte(`"rating":4`)) {
				t.Errorf("%s: hook changes missing: %s", k, s.Payload)
			}
		}
		// The stored version carries them: shared in the shared part, summary per locale.
		err := e.db.View(ctx, func(tx store.Tx) error {
			v, err := tx.Content().GetVersion(ctx, a.ID, 1)
			if err != nil {
				return err
			}
			shared := doc(t, string(v.Shared))
			if asJSON(shared["rating"]) != "4" {
				t.Errorf("shared: %s", v.Shared)
			}
			for _, l := range v.Locales {
				if d := doc(t, string(l.Data)); d["summary"] != "stamped "+l.Locale {
					t.Errorf("%s data: %s", l.Locale, l.Data)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		// A key change rebuilds the snapshots; so does a full rebuild: nothing but the key changes.
		k := "ann"
		if _, err := e.svc.Update(ctx, a.ID, content.UpdateInput{Key: &k}); err != nil {
			t.Fatal(err)
		}
		if err := rebuildItems(e.db, e.svc, a.ID); err != nil {
			t.Fatal(err)
		}
		after := e.snapshotsOf(a.ID)
		for key, s := range before {
			want := bytes.Replace(s.Payload, []byte(`"type":"article",`), []byte(`"type":"article","key":"ann",`), 1)
			if !bytes.Equal(after[key].Payload, want) {
				t.Errorf("%s changed:\n%s\n%s", key, want, after[key].Payload)
			}
		}
	}
	forEachBackend(t, run2)
}

func TestBeforePublishChangesSurviveExport(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		src := newPagesEnv(t, b.open(t, clk), clk, pagesOpts{schema: standardSchema, extra: []kernel.Module{stampModule{}}})
		a := src.create("article", "en", `{"title":"A"}`)
		src.mustPublish(a)
		want, err := src.db.Reader().Snapshots(ctx, []ids.UUID{a}, nil)
		if err != nil || len(want) != 1 {
			t.Fatalf("snapshots %v %v", want, err)
		}
		dir, _ := exportTo(t, src, false)
		clk2 := newClock()
		clk2.Advance(48 * 3600e9)
		dst := newPagesEnv(t, b.open(t, clk2), clk2, pagesOpts{schema: standardSchema, extra: []kernel.Module{stampModule{}}})
		if _, err := importFrom(t, dst, dir, false); err != nil {
			t.Fatal(err)
		}
		got, err := dst.db.Reader().Snapshots(ctx, []ids.UUID{a}, nil)
		if err != nil || len(got) != 1 {
			t.Fatalf("imported snapshots %v %v", got, err)
		}
		if !bytes.Equal(got[0].Payload, want[0].Payload) {
			t.Errorf("payload differs:\n%s\n%s", want[0].Payload, got[0].Payload)
		}
		if !bytes.Contains(got[0].Payload, []byte(`"rating":4`)) {
			t.Errorf("hook change lost: %s", got[0].Payload)
		}
	})
}

func TestExportFilesArePrivate(t *testing.T) {
	clk := newClock()
	e := newPagesEnv(t, openSQLite(t, clk), clk, pagesOpts{schema: standardSchema})
	a := e.create("article", "en", `{"title":"A"}`)
	e.mustPublish(a)
	dir, _ := exportTo(t, e, false)
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		want := fs.FileMode(0o600)
		if d.IsDir() {
			want = 0o700
		}
		if path != dir && info.Mode().Perm() != want { // the top directory is the caller's (a temp dir)
			t.Errorf("%s: mode %v, want %v", path, info.Mode().Perm(), want)
		}
		n++
		return nil
	})
	if err != nil || n < 3 {
		t.Fatalf("walk: %v (%d entries)", err, n)
	}
}
