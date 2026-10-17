package tests

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/exportfmt"
	"github.com/hydrogencms/hydrogen/core/store"
)

// memFiles is an in-memory exportfmt.FileStorage.
type memFiles struct {
	mu    sync.Mutex
	files map[string][]byte
	db    store.DB // Restore records the upload here
}

func (m *memFiles) Open(_ context.Context, sha string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[sha]
	if !ok {
		return nil, fmt.Errorf("no file %s", sha)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memFiles) Restore(ctx context.Context, rec store.Upload, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil || int64(len(b)) != rec.Size {
		return fmt.Errorf("restore %s: %d bytes, want %d, %v", rec.SHA, len(b), rec.Size, err)
	}
	m.mu.Lock()
	m.files[rec.SHA] = b
	m.mu.Unlock()
	rec.TouchedAt = rec.CreatedAt
	return m.db.Tx(ctx, func(tx store.Tx) error {
		_, err := tx.Uploads().Put(ctx, rec)
		return err
	})
}

func TestAssetExportImportRoundTrip(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		sclk := clock.NewManual(newClock().Now())
		src := newEnv(t, b.open(t, sclk), sclk, envConfig{schema: assetSchema})
		src.svc.SetFiles(newFakeFiles())
		content := map[string][]byte{shaCat: []byte("cat bytes"), shaDog: []byte("dog bytes"), shaPDF: []byte("pdf bytes")}
		srcFiles := &memFiles{files: map[string][]byte{}}
		for sha, data := range content {
			srcFiles.files[sha] = data
			info := newFakeFiles().known[sha]
			err := src.db.Tx(ctx, func(tx store.Tx) error {
				_, err := tx.Uploads().Put(ctx, store.Upload{SHA: sha, MIME: info.Mime, Size: int64(len(data)), Width: info.Width,
					Height: info.Height, CreatedBy: "test", CreatedAt: src.clk.Now(), TouchedAt: src.clk.Now()})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		// A stale upload that no item uses is left out.
		stale := shaOf("9")
		srcFiles.files[stale] = []byte("stale")
		_ = src.db.Tx(ctx, func(tx store.Tx) error {
			_, err := tx.Uploads().Put(ctx, store.Upload{SHA: stale, MIME: "image/png", Size: 5, CreatedAt: src.clk.Now(), TouchedAt: src.clk.Now()})
			return err
		})

		img := src.publish(src.image(shaCat, "cat.png").ID)
		src.update(img.ID, "en", `{"alt":"A cat"}`)
		src.publish(img.ID)
		draftOnly := src.image(shaDog, "dog.jpg") // only a draft refers to this file
		m := src.create("manual", "en", fileJSON(shaPDF, "m.pdf"))
		a := src.create("article", "en", fmt.Sprintf(`{"title":"A","hero":%q,"attachment":{"id":%q,"name":"x.png"}}`, img.ID, shaCat))
		src.publish(a.ID)
		_ = m

		dir := filepath.Join(t.TempDir(), "export")
		compiled, _ := src.svc.Schema().MarshalJSON()
		man, err := exportfmt.Export(ctx, exportfmt.ExportOptions{DB: src.db, Dir: dir, Clock: sclk, Version: "test",
			SchemaHash: src.svc.Schema().Hash(), Schema: compiled, Files: srcFiles})
		if err != nil {
			t.Fatal(err)
		}
		if man.Format != exportfmt.FormatVersion || man.Counts.Uploads != 3 {
			t.Fatalf("manifest = %+v", man)
		}
		for sha, data := range content {
			got, err := os.ReadFile(filepath.Join(dir, "files", sha))
			if err != nil || !bytes.Equal(got, data) {
				t.Errorf("files/%s = %q, %v", sha, got, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "files", stale)); err == nil {
			t.Error("an unreferenced upload was exported")
		}

		dclk := clock.NewManual(newClock().Now())
		dst := newEnv(t, b.open(t, dclk), dclk, envConfig{schema: assetSchema, skipRev: true})
		dstFiles := &memFiles{files: map[string][]byte{}, db: dst.db}
		// Without a file store the import refuses rather than dropping the files.
		_, err = exportfmt.Import(ctx, exportfmt.ImportOptions{DB: dst.db, Content: dst.svc, Outbox: dst.ob, Clock: dclk, Dir: dir})
		if err == nil || !strings.Contains(err.Error(), "uploaded files") {
			t.Fatalf("import without storage: %v", err)
		}
		res, err := exportfmt.Import(ctx, exportfmt.ImportOptions{DB: dst.db, Content: dst.svc, Outbox: dst.ob, Clock: dclk, Dir: dir, Files: dstFiles})
		if err != nil {
			t.Fatal(err)
		}
		if res.Uploads != 3 || len(dstFiles.files) != 3 {
			t.Fatalf("restored %d uploads, %d files", res.Uploads, len(dstFiles.files))
		}
		for sha, data := range content {
			if !bytes.Equal(dstFiles.files[sha], data) {
				t.Errorf("restored %s = %q", sha, dstFiles.files[sha])
			}
			err := dst.db.View(ctx, func(tx store.Tx) error {
				u, err := tx.Uploads().Get(ctx, sha)
				if err == nil && (u.MIME != newFakeFiles().known[sha].Mime || u.Size != int64(len(data)) || u.CreatedBy != "test") {
					t.Errorf("upload %s = %+v", sha, u)
				}
				return err
			})
			if err != nil {
				t.Errorf("upload %s: %v", sha, err)
			}
		}
		// The content came along: the article still points at the image, and usage works.
		refs, err := dst.svc.References(ctx, img.ID)
		if err != nil || len(refs) != 1 || refs[0].ItemID != a.ID {
			t.Fatalf("usage after import = %+v, %v", refs, err)
		}
		_ = draftOnly
	})
}

func TestAssetExportWithoutFilesWritesNoUploads(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := clock.NewManual(newClock().Now())
		src := newEnv(t, b.open(t, clk), clk, envConfig{schema: assetSchema})
		src.create("article", "en", `{"title":"Plain"}`)
		dir := filepath.Join(t.TempDir(), "export")
		compiled, _ := src.svc.Schema().MarshalJSON()
		if _, err := exportfmt.Export(ctx, exportfmt.ExportOptions{DB: src.db, Dir: dir, Clock: clk, Version: "test",
			SchemaHash: src.svc.Schema().Hash(), Schema: compiled}); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "uploads.ndjson")); err == nil {
			t.Error("uploads.ndjson written without a file store")
		}
		r, err := exportfmt.Open(dir)
		if err != nil || r.Manifest().Format != 2 {
			t.Fatalf("open: %v", err)
		}
	})
}
