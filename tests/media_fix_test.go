package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/exportfmt"
	"github.com/hydrogencms/hydrogen/features/media"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// spyStorage counts calls and runs a hook right after the first Stat.
type spyStorage struct {
	media.Storage
	mu                sync.Mutex
	stats, gets, puts int
	afterStat         func()
	afterList         func()
}

func (s *spyStorage) List(ctx context.Context, prefix, after string, limit int) ([]media.Entry, error) {
	out, err := s.Storage.List(ctx, prefix, after, limit)
	s.mu.Lock()
	f := s.afterList
	s.afterList = nil
	s.mu.Unlock()
	if f != nil {
		f()
	}
	return out, err
}

func (s *spyStorage) Stat(ctx context.Context, key string) (media.Info, error) {
	info, err := s.Storage.Stat(ctx, key)
	s.mu.Lock()
	s.stats++
	f := s.afterStat
	s.afterStat = nil
	s.mu.Unlock()
	if f != nil {
		f()
	}
	return info, err
}

func (s *spyStorage) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	s.mu.Lock()
	s.puts++
	s.mu.Unlock()
	return s.Storage.Put(ctx, key, r, size, mime)
}

func (s *spyStorage) Get(ctx context.Context, key string) (io.ReadCloser, media.Info, error) {
	s.mu.Lock()
	s.gets++
	s.mu.Unlock()
	return s.Storage.Get(ctx, key)
}

func (s *spyStorage) GetRange(ctx context.Context, key string, off, n int64) (io.ReadCloser, media.Info, error) {
	s.mu.Lock()
	s.gets++
	s.mu.Unlock()
	return s.Storage.GetRange(ctx, key, off, n)
}

func (s *spyStorage) counts() (stats, gets int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats, s.gets
}

// hookDB runs a function before the next write transaction, and can make the first reads of upload records
// report that the record is gone.
type hookDB struct {
	store.DB
	mu       sync.Mutex
	beforeTx func()
	failGets int
}

func (d *hookDB) Tx(ctx context.Context, fn func(store.Tx) error) error {
	d.mu.Lock()
	f := d.beforeTx
	d.beforeTx = nil
	d.mu.Unlock()
	if f != nil {
		f()
	}
	return d.DB.Tx(ctx, func(tx store.Tx) error { return fn(hookTx{tx, d}) })
}

type hookTx struct {
	store.Tx
	d *hookDB
}

func (t hookTx) Uploads() store.UploadStore { return hookUploads{t.Tx.Uploads(), t.d} }

type hookUploads struct {
	store.UploadStore
	d *hookDB
}

func (u hookUploads) Get(ctx context.Context, sha string) (store.Upload, error) {
	u.d.mu.Lock()
	fail := u.d.failGets > 0
	if fail {
		u.d.failGets--
	}
	u.d.mu.Unlock()
	if fail {
		return store.Upload{}, store.ErrNotFound
	}
	return u.UploadStore.Get(ctx, sha)
}

// mediaHooks builds a media environment whose storage and database are wrapped, plus a second service on the
// same database and bytes (another server) with its own locks.
type mediaHooks struct {
	*mediaEnv
	st    *spyStorage
	db    *hookDB
	other *media.Service
}

func newMediaHooks(t *testing.T, b backend, opts ...func(*media.Options)) *mediaHooks {
	t.Helper()
	h := &mediaHooks{}
	var base media.Options
	me := newMediaEnv(t, b, append([]func(*media.Options){func(o *media.Options) {
		h.st = &spyStorage{Storage: o.Storage}
		h.db = &hookDB{DB: o.DB}
		base = *o
		o.Storage, o.DB = h.st, h.db
	}}, opts...)...)
	h.mediaEnv = me
	other, err := media.New(base)
	if err != nil {
		t.Fatal(err)
	}
	h.other = other
	return h
}

func hasRecord(me *mediaEnv, sha string) bool {
	for _, r := range me.records() {
		if r.SHA == sha {
			return true
		}
	}
	return false
}

func hasBytes(me *mediaEnv, sha string) bool {
	for _, n := range me.stored() {
		if n == sha {
			return true
		}
	}
	return false
}

// A removed or renamed file field must not lose the files of the versions that had it: collection and export
// read every item in the database, whatever the schema says about its type today.
func TestMediaGCAndExportScanEveryItemType(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		me := newMediaEnv(t, b, func(o *media.Options) { o.FileTypes = func() []string { return []string{"image", "manual"} } })
		kept := me.mustUpload("", "doc.bin", []byte("kept by an article, a type the service no longer lists"))
		gone := me.mustUpload("image", "gone.png", mtPNG(7, 7, 40))
		art := me.create("article", "en", fmt.Sprintf(`{"title":"A","attachment":{"id":%q,"name":"doc.bin"}}`, kept.SHA))
		me.clk.Advance(25 * time.Hour)
		res, err := me.m.GC(ctx)
		if err != nil || res.Uploads != 1 {
			t.Fatalf("gc: %+v %v", res, err)
		}
		if !hasRecord(me, kept.SHA) || !hasBytes(me, kept.SHA) {
			t.Fatal("the file of an item of a type outside FileTypes was collected")
		}
		if hasRecord(me, gone.SHA) {
			t.Fatal("the unreferenced file survived")
		}

		dir := filepath.Join(t.TempDir(), "export")
		compiled, _ := me.svc.Schema().MarshalJSON()
		man, err := exportfmt.Export(ctx, exportfmt.ExportOptions{DB: me.db, Dir: dir, Clock: me.clk, Version: "test",
			SchemaHash: me.svc.Schema().Hash(), Schema: compiled, Files: me.m.Files()})
		if err != nil {
			t.Fatal(err)
		}
		if man.Counts.Uploads != 1 {
			t.Fatalf("manifest = %+v", man.Counts)
		}
		if got, err := os.ReadFile(filepath.Join(dir, "files", kept.SHA)); err != nil || string(got) != "kept by an article, a type the service no longer lists" {
			t.Fatalf("exported file: %q %v", got, err)
		}
		_ = art
	})
}

// A collection in another process removes the record and bytes of a stale file between an upload's check of the
// bytes and its write of the record: the upload must end with both the record and the bytes.
func TestMediaIngestSurvivesAConcurrentCollection(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		h := newMediaHooks(t, b)
		data := mtPNG(9, 9, 41)
		first := h.mustUpload("image", "a.png", data)
		h.clk.Advance(25 * time.Hour)

		h.st.afterStat = func() {
			res, err := h.other.GC(ctx)
			if err != nil || res.Uploads != 1 {
				t.Errorf("concurrent gc: %+v %v", res, err)
			}
		}
		again, err := h.upload("image", "a.png", data)
		if err != nil {
			t.Fatal(err)
		}
		if again.SHA != first.SHA || !again.Created {
			t.Fatalf("second upload = %+v", again)
		}
		if !hasRecord(h.mediaEnv, first.SHA) || !hasBytes(h.mediaEnv, first.SHA) {
			t.Fatalf("record %v, bytes %v: the upload lost its file", hasRecord(h.mediaEnv, first.SHA), hasBytes(h.mediaEnv, first.SHA))
		}
		rc, _, err := h.m.Open(ctx, first.SHA)
		if err != nil {
			t.Fatal(err)
		}
		_ = rc.Close()
		// The same collection in this process waits for the upload and sees its record.
		h.clk.Advance(25 * time.Hour)
		if res, err := h.m.GC(ctx); err != nil || res.Uploads != 1 {
			t.Fatalf("later gc: %+v %v", res, err)
		}
	})
}

// Bytes without a record are removed only if the record is still missing right before the delete: an upload
// that records the file after the listing keeps them.
func TestMediaGCKeepsOrphanBytesRecordedMeanwhile(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		h := newMediaHooks(t, b)
		key := keyFor("raced")
		p := filepath.Join(h.dir, key[:2], key[2:4], key)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil || os.WriteFile(p, []byte("x"), 0o640) != nil {
			t.Fatal("cannot create an orphan")
		}
		past := time.Now().Add(-72 * time.Hour)
		_ = os.Chtimes(p, past, past)
		h.clk.Set(time.Now().Add(-12 * time.Hour))

		h.st.afterList = func() { // the file is uploaded after the listing and records itself
			err := h.db.DB.Tx(ctx, func(tx store.Tx) error {
				_, err := tx.Uploads().Put(ctx, store.Upload{SHA: key, MIME: "text/plain", Size: 1, CreatedAt: h.clk.Now(), TouchedAt: h.clk.Now()})
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}
		res, err := h.m.GC(ctx)
		if err != nil || res.Orphans != 0 || !hasBytes(h.mediaEnv, key) {
			t.Fatalf("gc: %+v %v, bytes %v", res, err, hasBytes(h.mediaEnv, key))
		}
		// Without a record they go.
		if err := h.db.DB.Tx(ctx, func(tx store.Tx) error { _, err := tx.Uploads().Delete(ctx, key); return err }); err != nil {
			t.Fatal(err)
		}
		res, err = h.m.GC(ctx)
		if err != nil || res.Orphans != 1 || hasBytes(h.mediaEnv, key) {
			t.Fatalf("second gc: %+v %v", res, err)
		}
	})
}

// An item write that attaches a stale file touches it in its transaction: a collection that already computed
// its set of referenced files cannot remove it.
func TestMediaAttachTouchesTheUploadAgainstGC(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		h := newMediaHooks(t, b)
		up := h.mustUpload("", "doc.bin", []byte("attached while a collection runs"))
		h.clk.Advance(25 * time.Hour)
		h.db.beforeTx = func() { // after the collection read the references, before it deletes
			h.create("article", "en", fmt.Sprintf(`{"title":"A","attachment":{"id":%q,"name":"doc.bin"}}`, up.SHA))
		}
		res, err := h.m.GC(ctx)
		if err != nil || res.Uploads != 0 {
			t.Fatalf("gc: %+v %v", res, err)
		}
		if !hasRecord(h.mediaEnv, up.SHA) || !hasBytes(h.mediaEnv, up.SHA) {
			t.Fatal("the file attached during the collection was removed")
		}
		// It is referenced now, so later runs keep it.
		h.clk.Advance(25 * time.Hour)
		if _, err := h.m.GC(ctx); err != nil {
			t.Fatal(err)
		}
		if !hasRecord(h.mediaEnv, up.SHA) {
			t.Fatal("a referenced file was collected")
		}
	})
}

func TestMediaAttachOfAMissingFileFails(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		_, err := me.svc.Create(ctx, content.CreateInput{Type: "article", Locale: "en",
			Fields: doc(t, fmt.Sprintf(`{"title":"A","attachment":{"id":%q,"name":"x"}}`, shaOf("9")))})
		var ve *content.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("attaching an unknown file: %v", err)
		}
	})
}

// The upload transaction is repeated once when the record vanished between the bytes and the write.
func TestMediaIngestRetriesWhenTheRecordVanishes(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		h := newMediaHooks(t, b)
		h.db.failGets = 1
		r, err := h.upload("image", "a.png", mtPNG(5, 5, 43))
		if err != nil || !r.Created {
			t.Fatalf("upload with one vanished record: %+v %v", r, err)
		}
		if recs := h.records(); len(recs) != 1 || recs[0].SHA != r.SHA {
			t.Fatalf("records = %+v", recs)
		}
		var uploads int
		_ = h.db.View(ctx, func(tx store.Tx) error {
			evs, err := tx.Audit().List(ctx, store.AuditQuery{Action: "media.upload"})
			uploads = len(evs)
			return err
		})
		if uploads != 1 {
			t.Fatalf("%d media.upload audit entries", uploads)
		}
		// It does not loop: a record that vanishes twice is an error, not a hang.
		h.db.failGets = 2
		if _, err := h.upload("image", "b.png", mtPNG(5, 5, 44)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("upload with a record that keeps vanishing: %v", err)
		}
	})
}

func TestMediaRestoreChecksTheContent(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		src := newMediaEnv(t, b)
		data := mtPNG(9, 7, 45)
		r := src.mustUpload("image", "a.png", data)
		rec := src.records()[0]
		other := []byte("some other bytes")

		cases := []struct {
			name string
			rec  func(store.Upload) store.Upload
			body []byte
		}{
			{"wrong bytes", func(u store.Upload) store.Upload { return u }, other},
			{"upper-case sha", func(u store.Upload) store.Upload { u.SHA = strings.ToUpper(u.SHA); return u }, data},
			{"short sha", func(u store.Upload) store.Upload { u.SHA = u.SHA[:40]; return u }, data},
			{"wrong size", func(u store.Upload) store.Upload { u.Size++; return u }, data},
			{"wrong type", func(u store.Upload) store.Upload { u.MIME = "application/pdf"; return u }, data},
			{"wrong width", func(u store.Upload) store.Upload { u.Width++; return u }, data},
			{"wrong height", func(u store.Upload) store.Upload { u.Height = 0; return u }, data},
			{"no size", func(u store.Upload) store.Upload { u.Size = 0; return u }, data},
		}
		for _, c := range cases {
			dst := newMediaEnv(t, b)
			if err := dst.m.Restore(ctx, c.rec(rec), bytes.NewReader(c.body)); err == nil {
				t.Errorf("%s: restored", c.name)
			}
			if len(dst.records()) != 0 || len(dst.stored()) != 0 {
				t.Errorf("%s: left %d records, %v", c.name, len(dst.records()), dst.stored())
			}
		}
		// An export's file store restores through the same checks.
		dst := newMediaEnv(t, b)
		if err := dst.m.Files().Restore(ctx, rec, bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		if got := dst.records(); len(got) != 1 || got[0].SHA != r.SHA || !got[0].CreatedAt.Equal(rec.CreatedAt) {
			t.Fatalf("restored = %+v", got)
		}

		// Content with no signature keeps the recorded type, unless it is one a name cannot give.
		txt := []byte("a,b\n1,2\n")
		csv := dst.mustUpload("", "t.csv", txt)
		csvRec := dst.records()
		var csvU store.Upload
		for _, u := range csvRec {
			if u.SHA == csv.SHA {
				csvU = u
			}
		}
		if csvU.MIME != "text/csv" {
			t.Fatalf("csv recorded as %q", csvU.MIME)
		}
		again := newMediaEnv(t, b)
		if err := again.m.Restore(ctx, csvU, bytes.NewReader(txt)); err != nil {
			t.Fatalf("restore of a csv: %v", err)
		}
		csvU.MIME = "text/html"
		if err := newMediaEnv(t, b).m.Restore(ctx, csvU, bytes.NewReader(txt)); err == nil {
			t.Fatal("plain text restored as html")
		}
	})
}

// The file name refines the type of a new file only; a known file keeps the type of its first record, and that
// type is what a later upload is checked against.
func TestMediaNameRefinesOnlyNewFiles(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		me := newMediaEnv(t, b, func(o *media.Options) {
			o.Types = func() []media.UploadType {
				return []media.UploadType{{Name: "image", Accept: []string{"image/*"}}, {Name: "data", Accept: []string{"text/csv"}}}
			}
			o.FileTypes = func() []string { return []string{"image", "data"} }
		})
		csv := []byte("a,b\n1,2\n")
		first := me.mustUpload("", "x.csv", csv)
		if first.MIME != "text/csv" {
			t.Fatalf("first = %+v", first)
		}
		// The same bytes under a name that would not refine to csv are still the csv of the first record.
		second, err := me.upload("data", "x.txt", csv)
		if err != nil || second.MIME != "text/csv" || second.Created {
			t.Fatalf("second = %+v %v", second, err)
		}
		// And the other way round: a file first stored as plain text is not csv because of a later name.
		txt := []byte("just some words\n")
		plain := me.mustUpload("", "x.txt", txt)
		if plain.MIME != "text/plain" {
			t.Fatalf("plain = %+v", plain)
		}
		if _, err := me.upload("data", "x.csv", txt); !errors.Is(err, media.ErrUnsupportedType) {
			t.Fatalf("a later name turned plain text into csv: %v", err)
		}
	})
}

func TestMediaConcurrentUploadsAreCapped(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		me := newMediaEnv(t, b, func(o *media.Options) { o.MaxConcurrentUploads = 1 })
		pr, pw := io.Pipe()
		mw := multipart.NewWriter(pw)
		started := make(chan struct{})
		var first *httptest.ResponseRecorder
		done := make(chan struct{})
		go func() {
			defer close(done)
			first = me.post(mw.FormDataContentType(), pr, "tok")
		}()
		go func() {
			hdr := textproto.MIMEHeader{}
			hdr.Set("Content-Disposition", `form-data; name="file"; filename="a.png"`)
			w, _ := mw.CreatePart(hdr)
			close(started)
			_, _ = w.Write(mtPNG(6, 6, 46)[:20])
		}()
		<-started
		var busy *httptest.ResponseRecorder
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			ct, body := multipartBody(t, [3]string{"file", "b.png", string(mtPNG(6, 6, 47))})
			if busy = me.post(ct, body, "tok"); busy.Code == http.StatusTooManyRequests {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if busy.Code != http.StatusTooManyRequests || mtErrCode(t, busy) != "busy" || busy.Header().Get("Retry-After") == "" {
			t.Fatalf("second upload while the first holds the slot: %d %s", busy.Code, busy.Body)
		}
		_ = pw.CloseWithError(io.ErrClosedPipe) // the first upload fails; its slot is free again
		<-done
		_ = first
		ct, body := multipartBody(t, [3]string{"file", "c.png", string(mtPNG(6, 6, 48))})
		if rec := me.post(ct, body, "tok"); rec.Code != http.StatusCreated {
			t.Fatalf("upload after the slot was freed: %d %s", rec.Code, rec.Body)
		}
	})
}

func TestMediaServeDoesNotTouchStorageForRevalidation(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		h := newMediaHooks(t, b)
		data := mtPNG(10, 10, 49)
		r := h.mustUpload("image", "a.png", data)
		path := "/assets/" + r.SHA + "/a.png"
		etag := `"` + r.SHA + `"`

		stats0, gets0 := h.st.counts()
		if rec := h.get(path, "If-None-Match", etag); rec.Code != http.StatusNotModified || rec.Body.Len() != 0 || rec.Header().Get("ETag") != etag {
			t.Fatalf("revalidation: %d %v", rec.Code, rec.Header())
		}
		if rec := h.get(path, "If-None-Match", `W/"other", `+etag); rec.Code != http.StatusNotModified {
			t.Fatalf("revalidation among several: %d", rec.Code)
		}
		if stats, gets := h.st.counts(); stats != stats0 || gets != gets0 {
			t.Fatalf("a revalidation used the storage: stats %d->%d, gets %d->%d", stats0, stats, gets0, gets)
		}
		// A plain GET is one read of the object, with no separate existence check.
		rec := h.get(path)
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), data) {
			t.Fatalf("get: %d", rec.Code)
		}
		if stats, gets := h.st.counts(); stats != stats0 || gets != gets0+1 {
			t.Fatalf("a plain GET: stats %d->%d, gets %d->%d", stats0, stats, gets0, gets)
		}
		// A non-matching validator, a range and a HEAD still work.
		if rec := h.get(path, "If-None-Match", `"nope"`); rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), data) {
			t.Fatalf("mismatching validator: %d", rec.Code)
		}
		if rec := h.get(path, "Range", "bytes=2-5"); rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), data[2:6]) {
			t.Fatalf("range: %d %q", rec.Code, rec.Body)
		}
		if rec := h.do(http.MethodHead, path); rec.Code != 200 || rec.Body.Len() != 0 || rec.Header().Get("Content-Length") == "" {
			t.Fatalf("head: %d %v", rec.Code, rec.Header())
		}
		// Missing bytes are a 404 without the headers of a file.
		for _, n := range h.stored() {
			if err := os.Remove(filepath.Join(h.dir, n[:2], n[2:4], n)); err != nil {
				t.Fatal(err)
			}
		}
		for _, m := range []string{http.MethodGet, http.MethodHead} {
			rec := h.do(m, path)
			if rec.Code != 404 || rec.Header().Get("ETag") != "" || strings.Contains(rec.Header().Get("Cache-Control"), "immutable") ||
				rec.Header().Get("Content-Disposition") != "" {
				t.Fatalf("%s with missing bytes: %d %v", m, rec.Code, rec.Header())
			}
		}
	})
}

// --- facets and the folder filter ---

func (me *mediaEnv) img(folder string, tags ...string) *content.View {
	me.t.Helper()
	up := me.mustUpload("image", "f.png", mtPNG(4, 4, byte(len(me.records())+60)))
	tj, _ := json.Marshal(tags)
	return me.create("image", "en", fmt.Sprintf(`{"file":{"id":%q,"name":"f.png"},"folder":%q,"tags":%s}`, up.SHA, folder, tj))
}

func (me *mediaEnv) getJSON(path string) (int, map[string]any) {
	me.t.Helper()
	rec := me.do(http.MethodGet, "/api/manage/v1"+path, "Authorization", "Bearer tok")
	var v map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	return rec.Code, v
}

func facetString(v any) string {
	var out []string
	list, _ := v.([]any)
	for _, e := range list {
		m := e.(map[string]any)
		out = append(out, fmt.Sprintf("%v=%v", m["value"], m["count"]))
	}
	return strings.Join(out, " ")
}

func TestMediaFacetsAndFolderFilter(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		me.img("/", "x")
		me.img("/a", "x", "y")
		me.img("/a/b", "y")
		me.img("/ab", "z")
		published := me.img("/a/c")
		me.publish(published.ID)
		trashed := me.img("/a")
		if err := me.svc.Trash(ctx, trashed.ID); err != nil {
			t.Fatal(err)
		}

		code, v := me.getJSON("/collections/image/facets?fields=folder,tags")
		if code != 200 {
			t.Fatalf("facets: %d %v", code, v)
		}
		if got := facetString(v["folder"]); got != "/=1 /a=1 /a/b=1 /a/c=1 /ab=1" {
			t.Errorf("folders = %q", got)
		}
		if got := facetString(v["tags"]); got != "x=2 y=2 z=1" {
			t.Errorf("tags = %q", got)
		}

		// The same filters as a list narrow the counts; status=published counts the published projection.
		_, v = me.getJSON("/collections/image/facets?fields=tags&where[folder][under]=/a")
		if got := facetString(v["tags"]); got != "y=2 x=1" {
			t.Errorf("tags under /a = %q", got)
		}
		_, v = me.getJSON("/collections/image/facets?fields=folder&status=published")
		if got := facetString(v["folder"]); got != "/a/c=1" {
			t.Errorf("published folders = %q", got)
		}
		// Nothing matching gives empty lists, not missing keys.
		_, v = me.getJSON("/collections/image/facets?fields=folder,tags&where[folder][under]=/none")
		if l, ok := v["folder"].([]any); !ok || len(l) != 0 {
			t.Errorf("empty facet = %v", v["folder"])
		}
		for _, bad := range []string{"", "?fields=alt", "?fields=nope", "?fields=folder&limit=2", "?fields=a,b,c,d,e,f"} {
			if code, _ := me.getJSON("/collections/image/facets" + bad); code != 400 {
				t.Errorf("facets%s = %d, want 400", bad, code)
			}
		}

		// where[folder][under]= matches the folder itself and its descendants, never a sibling with the same prefix.
		list := func(q string) string {
			t.Helper()
			code, v := me.getJSON("/collections/image/items?fields=folder&sort=name&limit=100&" + q)
			if code != 200 {
				t.Fatalf("list %s: %d %v", q, code, v)
			}
			var out []string
			for _, it := range v["items"].([]any) {
				out = append(out, it.(map[string]any)["fields"].(map[string]any)["folder"].(string))
			}
			slicesSort(out)
			return strings.Join(out, " ")
		}
		if got := list("where[folder][under]=/a"); got != "/a /a/b /a/c" {
			t.Errorf("under /a = %q", got)
		}
		if got := list("where[folder][under]=/a/"); got != "/a /a/b /a/c" {
			t.Errorf("under /a/ = %q", got)
		}
		if got := list("where[folder][under]=/ab"); got != "/ab" {
			t.Errorf("under /ab = %q", got)
		}
		if got := list("where[folder][under]=/"); got != "/ /a /a/b /a/c /ab" {
			t.Errorf("under / = %q", got)
		}
		if code, _ := me.getJSON("/collections/image/items?where[alt][under]=/a"); code != 400 {
			t.Errorf("under on an unindexed field = %d", code)
		}
	})
}

func slicesSort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// Facets cover what the principal may read: a read scope limited to own items counts own items only.
func TestAssetFacetsRespectReadAccess(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		s := n.app.Services()
		role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: "own", Policies: []access.RolePolicy{
			{Policy: "content.read", Limitations: map[string][]string{"owner": {access.OwnerSelf}}},
			{Policy: "content.create"}, {Policy: "content.edit"}}})
		if err != nil {
			t.Fatal(err)
		}
		_, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: "own", Roles: []ids.UUID{role.ID}})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+secret)
		an, err := s.Auth.Authenticate(r)
		if err != nil {
			t.Fatal(err)
		}
		me := access.WithPrincipal(context.Background(), an.Principal)

		mk := func(c context.Context, shade uint8, folder string) {
			file := n.upload(fmt.Sprintf("f%d.png", shade), pngBytes(t, 5, 5, shade))
			fb, _ := json.Marshal(file)
			n.tick()
			if _, err := s.Content.Create(c, content.CreateInput{Type: "image", Locale: "en",
				Fields: doc(t, fmt.Sprintf(`{"file":%s,"folder":%q,"tags":["t"]}`, fb, folder))}); err != nil {
				t.Fatal(err)
			}
		}
		mk(ctx, 1, "/system")
		mk(ctx, 2, "/system")
		mk(me, 3, "/mine")

		all, err := s.Content.Facets(ctx, content.FacetOptions{Type: "image", Fields: []string{"folder", "tags"}})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(all["folder"]) != "[{/system 2} {/mine 1}]" || fmt.Sprint(all["tags"]) != "[{t 3}]" {
			t.Fatalf("unrestricted facets = %v", all)
		}
		own, err := s.Content.Facets(me, content.FacetOptions{Type: "image", Fields: []string{"folder", "tags"}})
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(own["folder"]) != "[{/mine 1}]" || fmt.Sprint(own["tags"]) != "[{t 1}]" {
			t.Fatalf("facets of a principal who reads its own items = %v", own)
		}
		// A principal with no read grant for the type gets nothing counted, like an empty list.
		none, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: "people", Policies: []access.RolePolicy{
			{Policy: "content.read", Limitations: map[string][]string{"collection": {"article"}}}}})
		if err != nil {
			t.Fatal(err)
		}
		_, secret, err = s.Admin.CreateToken(ctx, access.TokenInput{Name: "people", Roles: []ids.UUID{none.ID}})
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+secret)
		an, _ = s.Auth.Authenticate(r)
		got, err := s.Content.Facets(access.WithPrincipal(context.Background(), an.Principal), content.FacetOptions{Type: "image", Fields: []string{"folder"}})
		if err == nil && len(got["folder"]) != 0 {
			t.Fatalf("a principal who cannot read images saw %v", got)
		}
	})
}

// A list of cards that show assets loads the assets together, not once per card.
func TestAssetDeliveryBatchesAssetLoads(t *testing.T) {
	runAssetNodes(t, func(t *testing.T, n *dnode) {
		shared, _ := n.image("shared.png", 1, `{"alt":"Shared"}`, true)
		for i := range 9 {
			img, _ := n.image(fmt.Sprintf("c%d.png", i), uint8(10+i), fmt.Sprintf(`{"alt":"Card %d"}`, i), true)
			n.create("article", fmt.Sprintf("card-%d", i), "en",
				fmt.Sprintf(`{"title":"Card %d","hero":%q,"cover":%q,"gallery":[%q,%q]}`, i, img, shared, img, shared), true)
		}
		reads := func(limit int) (int64, []any) {
			t.Helper()
			before := n.db.dbCalls()
			r := n.wantStatus(fmt.Sprintf("/collections/article/items?channel=web&limit=%d", limit), 200)
			items, _ := r.json()["items"].([]any)
			return n.db.dbCalls() - before, items
		}
		reads(1) // warm up whatever is loaded once per process
		few, items := reads(2)
		if len(items) != 2 {
			t.Fatalf("%d items", len(items))
		}
		many, items := reads(9)
		if len(items) != 9 {
			t.Fatalf("%d items", len(items))
		}
		if many != few {
			t.Fatalf("reads grew with the number of cards: %d for 2 cards, %d for 9", few, many)
		}
		for i, it := range items {
			f := asMap(asMap(it)["fields"])
			deq(t, "shared cover", asMap(f["cover"])["name"], "shared.png")
			g, _ := f["gallery"].([]any)
			if len(g) != 2 || asMap(g[0])["name"] == nil || asMap(g[1])["name"] != "shared.png" {
				t.Errorf("card %d gallery = %v", i, g)
			}
		}
	})
}

func TestMediaRefusesAShortGCAge(t *testing.T) {
	var base media.Options
	newMediaEnv(t, backends[0], func(o *media.Options) { base = *o })
	base.GCAge = time.Hour
	if _, err := media.New(base); err == nil {
		t.Fatal("a GC age below the minimum was accepted")
	}
	base.GCAge = 0
	if _, err := media.New(base); err != nil {
		t.Fatalf("default GC age: %v", err)
	}
}
