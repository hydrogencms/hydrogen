package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

// exifJPEG inserts an EXIF segment into a JPEG; order is "MM" or "II".
func exifJPEG(jpg []byte, order string, o uint16) []byte {
	var bo binary.ByteOrder = binary.BigEndian
	if order == "II" {
		bo = binary.LittleEndian
	}
	tiff := make([]byte, 26)
	copy(tiff, order)
	bo.PutUint16(tiff[2:], 42)
	bo.PutUint32(tiff[4:], 8)
	bo.PutUint16(tiff[8:], 1)
	bo.PutUint16(tiff[10:], 0x0112)
	bo.PutUint16(tiff[12:], 3)
	bo.PutUint32(tiff[14:], 1)
	bo.PutUint16(tiff[18:], o)
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(out[4:], uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestJPEGOrientation(t *testing.T) {
	base := jpegBytes(8, 4)
	if got := JPEGOrientation(bytes.NewReader(base)); got != 1 {
		t.Fatalf("no exif: %d", got)
	}
	for _, order := range []string{"MM", "II"} {
		for o := uint16(1); o <= 8; o++ {
			if got := JPEGOrientation(bytes.NewReader(exifJPEG(base, order, o))); got != int(o) {
				t.Errorf("%s %d: got %d", order, o, got)
			}
		}
	}
	for _, in := range [][]byte{nil, []byte("x"), pngBytes(4, 4), exifJPEG(base, "MM", 9), exifJPEG(base, "MM", 0), exifJPEG(base, "MM", 6)[:20]} {
		if got := JPEGOrientation(bytes.NewReader(in)); got != 1 {
			t.Errorf("bad input %q...: %d", in[:min(len(in), 8)], got)
		}
	}
}

func TestDetectOrientationSwapsSize(t *testing.T) {
	base := jpegBytes(8, 4)
	for o := uint16(1); o <= 8; o++ {
		d, err := Detect(bytes.NewReader(exifJPEG(base, "MM", o)), "a.jpg")
		if err != nil {
			t.Fatal(err)
		}
		w, h := 8, 4
		if o >= 5 {
			w, h = 4, 8
		}
		if d.MIME != "image/jpeg" || d.Width != w || d.Height != h {
			t.Errorf("o=%d: %+v", o, d)
		}
	}
	// the reader is left at the start
	r := bytes.NewReader(exifJPEG(base, "MM", 6))
	if _, err := Detect(r, "a.jpg"); err != nil {
		t.Fatal(err)
	}
	if n, _ := io.Copy(io.Discard, r); n != int64(len(exifJPEG(base, "MM", 6))) {
		t.Fatalf("reader not rewound: %d", n)
	}
}

func TestSplitDerived(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	for key, want := range map[string]string{sha + "-card-1a2b3c4d-640": "card-1a2b3c4d-640", sha + "-x": "x", sha + "-a_b": "a_b"} {
		if s, sfx, ok := SplitDerived(key); !ok || s != sha || sfx != want {
			t.Errorf("%s: %q %q %v", key, s, sfx, ok)
		}
	}
	for _, key := range []string{sha, sha + "-", sha + "-A", sha + "-a.b", sha + "-" + strings.Repeat("a", 57), "x" + sha[1:] + "-a", sha + "_a", strings.ToUpper(sha) + "-a", ""} {
		if _, _, ok := SplitDerived(key); ok {
			t.Errorf("%q accepted", key)
		}
	}
	if _, _, ok := SplitDerived(sha + "-" + strings.Repeat("a", 56)); !ok {
		t.Error("longest suffix refused")
	}
}

func TestDerivedFiles(t *testing.T) {
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{o: Options{Storage: st}}
	ctx := context.Background()
	sha, other := strings.Repeat("ab", 32), strings.Repeat("cd", 32)
	if err := st.Put(ctx, sha, strings.NewReader("orig"), 4, ""); err != nil {
		t.Fatal(err)
	}
	for _, sfx := range []string{"b-1", "a-1", "a-2"} {
		if err := s.PutDerived(ctx, sha, sfx, strings.NewReader("v"+sfx), int64(len(sfx))+1, "image/png"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutDerived(ctx, other, "a-1", strings.NewReader("z"), 1, ""); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "A", "a.b", "a/b", strings.Repeat("a", 57)} {
		if err := s.PutDerived(ctx, sha, bad, strings.NewReader("z"), 1, ""); !errors.Is(err, ErrNotFound) {
			t.Errorf("suffix %q: %v", bad, err)
		}
	}
	if err := s.PutDerived(ctx, "short", "a", strings.NewReader("z"), 1, ""); err == nil {
		t.Error("bad sha accepted")
	}
	rc, info, err := s.OpenDerived(ctx, sha, "a-2")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(rc); string(b) != "va-2" || info.Size != 4 {
		t.Fatalf("%q %+v", b, info)
	}
	_ = rc.Close()
	if _, _, err := s.OpenDerived(ctx, sha, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	all, err := s.ListDerived(ctx, "", 10)
	if err != nil || len(all) != 4 { // the original is not listed
		t.Fatalf("%v %v", all, err)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Key >= all[i].Key {
			t.Errorf("not in key order: %v", all)
		}
	}
	page, _ := s.ListDerived(ctx, all[0].Key, 2)
	if len(page) != 2 || page[0].Key != all[1].Key {
		t.Fatalf("page = %v", page)
	}
	if err := s.DeleteDerived(ctx, sha, "a-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDerived(ctx, sha, "a-2"); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	if all, _ = s.ListDerived(ctx, "", 10); len(all) != 3 {
		t.Fatalf("%v", all)
	}
}

func TestLocalListPrefixShard(t *testing.T) {
	st, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, b := "abcd"+strings.Repeat("0", 60), "abce"+strings.Repeat("0", 60)
	for _, k := range []string{a, a + "-x", a + "-y", b, b + "-x"} {
		if err := st.Put(ctx, k, strings.NewReader("1"), 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.List(ctx, a+"-", "", 10)
	if err != nil || len(got) != 2 || got[0].Key != a+"-x" || got[1].Key != a+"-y" {
		t.Fatalf("%v %v", got, err)
	}
	if got, _ = st.List(ctx, "abc", "", 10); len(got) != 5 {
		t.Fatalf("short prefix: %v", got)
	}
	if got, _ = st.List(ctx, a+"-", a+"-x", 10); len(got) != 1 {
		t.Fatalf("after: %v", got)
	}
}

func TestDeleteDerivedInvalid(t *testing.T) {
	st, _ := NewLocal(t.TempDir())
	s := &Service{o: Options{Storage: st}}
	err := s.DeleteDerived(context.Background(), "x", "a")
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
