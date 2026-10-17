package tests

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/features/media"
)

func TestMediaDerivedWithGC(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		kept := me.mustUpload("image", "kept.png", mtPNG(8, 8, 40))
		gone := me.mustUpload("image", "gone.png", mtPNG(8, 8, 41))
		me.create("image", "en", fileJSON(kept.SHA, "kept.png"))
		for _, sha := range []string{kept.SHA, gone.SHA} {
			for _, sfx := range []string{"card-1a2b3c4d-640", "card-1a2b3c4d-320"} {
				if err := me.m.PutDerived(ctx, sha, sfx, bytes.NewReader([]byte("variant")), 7, "image/png"); err != nil {
					t.Fatal(err)
				}
			}
		}
		rc, info, err := me.m.OpenDerived(ctx, kept.SHA, "card-1a2b3c4d-640")
		if err != nil {
			t.Fatal(err)
		}
		if b, _ := io.ReadAll(rc); string(b) != "variant" || info.Size != 7 {
			t.Fatalf("derived = %q %+v", b, info)
		}
		_ = rc.Close()
		if got, err := me.m.ListDerived(ctx, "", 10); err != nil || len(got) != 4 {
			t.Fatalf("list = %v %v", got, err)
		}

		me.clk.Advance(25 * time.Hour)
		res, err := me.m.GC(ctx)
		if err != nil || res.Uploads != 1 || res.Derived != 2 || res.Orphans != 0 {
			t.Fatalf("gc: %+v %v", res, err)
		}
		list, err := me.m.ListDerived(ctx, "", 10)
		if err != nil || len(list) != 2 {
			t.Fatalf("list after gc = %v %v", list, err)
		}
		for _, e := range list {
			if sha, _, ok := media.SplitDerived(e.Key); !ok || sha != kept.SHA {
				t.Errorf("unexpected derived %s", e.Key)
			}
		}
		if len(me.stored()) != 3 {
			t.Errorf("stored = %v", me.stored())
		}
		if err := me.m.DeleteDerived(ctx, kept.SHA, "card-1a2b3c4d-640"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := me.m.OpenDerived(ctx, kept.SHA, "card-1a2b3c4d-640"); err == nil {
			t.Fatal("deleted derived file opens")
		}
	})
}

func TestMediaDerivedOrphanSweep(t *testing.T) {
	runMedia(t, func(t *testing.T, me *mediaEnv) {
		live := me.mustUpload("image", "live.png", mtPNG(8, 8, 42))
		me.create("image", "en", fileJSON(live.SHA, "live.png"))
		old, young := keyFor("derived-old"), keyFor("derived-young")
		put := func(sha string) string {
			if err := me.m.PutDerived(ctx, sha, "card-1a2b3c4d-640", bytes.NewReader([]byte("v")), 1, "image/png"); err != nil {
				t.Fatal(err)
			}
			return sha + "-card-1a2b3c4d-640"
		}
		oldKey, youngKey, liveKey := put(old), put(young), put(live.SHA)
		past := time.Now().Add(-72 * time.Hour)
		for _, k := range []string{oldKey, liveKey} {
			_ = os.Chtimes(filepath.Join(me.dir, k[:2], k[2:4], k), past, past)
		}
		me.clk.Set(time.Now().Add(-12 * time.Hour))
		res, err := me.m.GC(ctx)
		if err != nil || res.Derived != 1 || res.Orphans != 0 || res.Uploads != 0 {
			t.Fatalf("gc: %+v %v", res, err)
		}
		got := map[string]bool{}
		for _, n := range me.stored() {
			got[n] = true
		}
		if got[oldKey] || !got[youngKey] || !got[liveKey] || !got[live.SHA] {
			t.Fatalf("stored = %v", got)
		}
	})
}
