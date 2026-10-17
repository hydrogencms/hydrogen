package storetest

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/store"
)

var uploadTests = []struct {
	name string
	fn   func(*testing.T, store.DB)
}{
	{"UploadsPutGet", testUploadsPutGet},
	{"UploadsTouch", testUploadsTouch},
	{"UploadsTouchExisting", testUploadsTouchExisting},
	{"UploadsListStale", testUploadsListStale},
	{"UploadsDelete", testUploadsDelete},
}

func uploadsDo(t *testing.T, db store.DB, fn func(store.UploadStore) error) error {
	t.Helper()
	return db.Tx(context.Background(), func(tx store.Tx) error { return fn(tx.Uploads()) })
}

func uploadsMust(t *testing.T, db store.DB, fn func(store.UploadStore) error) {
	t.Helper()
	if err := uploadsDo(t, db, fn); err != nil {
		t.Fatal(err)
	}
}

func sha(c string) string { return strings.Repeat(c, 64) }

func newUpload(c string, at time.Time) store.Upload {
	return store.Upload{SHA: sha(c), MIME: "image/png", Size: 1234, Width: 640, Height: 480, CreatedBy: "user:" + c,
		CreatedAt: at, TouchedAt: at}
}

func testUploadsPutGet(t *testing.T, db store.DB) {
	ctx := context.Background()
	u := newUpload("a", t0.Add(123*time.Millisecond))
	uploadsMust(t, db, func(s store.UploadStore) error {
		created, err := s.Put(ctx, u)
		if err != nil || !created {
			t.Fatalf("put: %v %v", created, err)
		}
		got, err := s.Get(ctx, u.SHA)
		if err != nil {
			return err
		}
		if got != u || got.CreatedAt.Location() != time.UTC {
			t.Fatalf("round trip: %+v, want %+v", got, u)
		}
		_, err = s.Get(ctx, sha("b"))
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("get missing: %v", err)
		}
		return nil
	})
	pdf := store.Upload{SHA: sha("c"), MIME: "application/pdf", Size: 9_000_000_000, CreatedAt: t0, TouchedAt: t0}
	uploadsMust(t, db, func(s store.UploadStore) error {
		if _, err := s.Put(ctx, pdf); err != nil {
			return err
		}
		got, err := s.Get(ctx, pdf.SHA)
		if err != nil || got != pdf {
			t.Fatalf("pdf: %+v %v", got, err)
		}
		return nil
	})
}

func testUploadsTouch(t *testing.T, db store.DB) {
	ctx := context.Background()
	u := newUpload("a", t0)
	uploadsMust(t, db, func(s store.UploadStore) error {
		_, err := s.Put(ctx, u)
		return err
	})
	again := newUpload("a", t0.Add(time.Hour))
	again.MIME, again.CreatedBy = "image/jpeg", "other"
	uploadsMust(t, db, func(s store.UploadStore) error {
		created, err := s.Put(ctx, again)
		if err != nil || created {
			t.Fatalf("second put: %v %v", created, err)
		}
		got, err := s.Get(ctx, u.SHA)
		if err != nil {
			return err
		}
		want := u
		want.TouchedAt = t0.Add(time.Hour)
		if got != want {
			t.Fatalf("after touch: %+v, want %+v", got, want)
		}
		return nil
	})
	// An older touch never moves the time back.
	old := newUpload("a", t0.Add(-time.Hour))
	uploadsMust(t, db, func(s store.UploadStore) error {
		if _, err := s.Put(ctx, old); err != nil {
			return err
		}
		got, err := s.Get(ctx, u.SHA)
		if err != nil || !got.TouchedAt.Equal(t0.Add(time.Hour)) {
			t.Fatalf("older touch: %+v %v", got, err)
		}
		return nil
	})
}

func testUploadsListStale(t *testing.T, db store.DB) {
	ctx := context.Background()
	uploadsMust(t, db, func(s store.UploadStore) error {
		for i, c := range []string{"a", "b", "c", "d"} {
			if _, err := s.Put(ctx, newUpload(c, t0.Add(time.Duration(i)*time.Hour))); err != nil {
				return err
			}
		}
		return nil
	})
	uploadsMust(t, db, func(s store.UploadStore) error {
		all, err := s.List(ctx, "", 10)
		if err != nil || len(all) != 4 || all[0].SHA != sha("a") || all[3].SHA != sha("d") {
			t.Fatalf("list: %+v %v", all, err)
		}
		page, err := s.List(ctx, sha("a"), 2)
		if err != nil || len(page) != 2 || page[0].SHA != sha("b") || page[1].SHA != sha("c") {
			t.Fatalf("list page: %+v %v", page, err)
		}
		stale, err := s.ListStale(ctx, t0.Add(2*time.Hour), "", 10)
		if err != nil || len(stale) != 2 || stale[1].SHA != sha("b") {
			t.Fatalf("stale: %+v %v", stale, err)
		}
		stale, err = s.ListStale(ctx, t0.Add(2*time.Hour), sha("a"), 10)
		if err != nil || len(stale) != 1 || stale[0].SHA != sha("b") {
			t.Fatalf("stale after: %+v %v", stale, err)
		}
		none, err := s.ListStale(ctx, t0, "", 10)
		if err != nil || len(none) != 0 {
			t.Fatalf("stale before everything: %+v %v", none, err)
		}
		return nil
	})
}

func testUploadsDelete(t *testing.T, db store.DB) {
	ctx := context.Background()
	uploadsMust(t, db, func(s store.UploadStore) error {
		for _, c := range []string{"a", "b"} {
			if _, err := s.Put(ctx, newUpload(c, t0)); err != nil {
				return err
			}
		}
		return nil
	})
	uploadsMust(t, db, func(s store.UploadStore) error {
		if ok, err := s.DeleteStale(ctx, sha("a"), t0); err != nil || ok {
			t.Fatalf("delete stale at the touch time: %v %v", ok, err)
		}
		if ok, err := s.DeleteStale(ctx, sha("a"), t0.Add(time.Second)); err != nil || !ok {
			t.Fatalf("delete stale: %v %v", ok, err)
		}
		if ok, err := s.Delete(ctx, sha("b")); err != nil || !ok {
			t.Fatalf("delete: %v %v", ok, err)
		}
		if ok, err := s.Delete(ctx, sha("b")); err != nil || ok {
			t.Fatalf("delete twice: %v %v", ok, err)
		}
		all, err := s.List(ctx, "", 10)
		if err != nil || len(all) != 0 {
			t.Fatalf("after deletes: %+v %v", all, err)
		}
		return nil
	})
}

func testUploadsTouchExisting(t *testing.T, db store.DB) {
	ctx := context.Background()
	u := newUpload("a", t0)
	uploadsMust(t, db, func(s store.UploadStore) error {
		ok, err := s.Touch(ctx, u.SHA, t0.Add(time.Hour))
		if err != nil || ok {
			t.Fatalf("touch of a missing upload: %v %v", ok, err)
		}
		_, err = s.Put(ctx, u)
		return err
	})
	uploadsMust(t, db, func(s store.UploadStore) error {
		if ok, err := s.Touch(ctx, u.SHA, t0.Add(2*time.Hour)); err != nil || !ok {
			t.Fatalf("touch: %v %v", ok, err)
		}
		got, err := s.Get(ctx, u.SHA)
		if err != nil || !got.TouchedAt.Equal(t0.Add(2*time.Hour)) || !got.CreatedAt.Equal(t0) {
			t.Fatalf("after touch: %+v %v", got, err)
		}
		// An earlier time never moves it back, and a touched record is not stale.
		if ok, err := s.Touch(ctx, u.SHA, t0.Add(time.Hour)); err != nil || !ok {
			t.Fatalf("older touch: %v %v", ok, err)
		}
		got, _ = s.Get(ctx, u.SHA)
		if !got.TouchedAt.Equal(t0.Add(2 * time.Hour)) {
			t.Fatalf("older touch moved the time back: %+v", got)
		}
		if deleted, err := s.DeleteStale(ctx, u.SHA, t0.Add(time.Hour)); err != nil || deleted {
			t.Fatalf("touched upload deleted as stale: %v %v", deleted, err)
		}
		return nil
	})
}
