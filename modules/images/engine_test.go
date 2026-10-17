package images

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

var bg = context.Background()

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func encodeJPEG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// halves is left red, right blue.
func halves(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, color.NRGBA{255, 0, 0, 255})
			} else {
				img.Set(x, y, color.NRGBA{0, 0, 255, 255})
			}
		}
	}
	return img
}

func near(c color.Color, r, g, b uint8) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(a uint32, want uint8) bool {
		v := int(a>>8) - int(want)
		return v > -40 && v < 40
	}
	return d(cr, r) && d(cg, g) && d(cb, b)
}

func decodeOut(t *testing.T, data []byte, mime string) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"image/jpeg": "jpeg", "image/png": "png"}[mime]
	if format != want {
		t.Fatalf("format %s for %s", format, mime)
	}
	return img
}

func TestTransformBasics(t *testing.T) {
	src := encodePNG(t, halves(1200, 800))
	p := Preset{Name: "card", Width: 640, Height: 360, Fit: Cover}
	out, mime, err := Transform(bg, bytes.NewReader(src), p, 640, nil, 0)
	if err != nil || mime != "image/png" {
		t.Fatal(mime, err)
	}
	img := decodeOut(t, out, mime)
	if img.Bounds().Dx() != 640 || img.Bounds().Dy() != 360 {
		t.Fatalf("size %v", img.Bounds())
	}
	if !near(img.At(10, 180), 255, 0, 0) || !near(img.At(630, 180), 0, 0, 255) {
		t.Error("content changed")
	}

	hero := Preset{Name: "hero", Width: 300, Format: "jpeg", Quality: 70}
	out, mime, err = Transform(bg, bytes.NewReader(src), hero, 300, nil, 0)
	if err != nil || mime != "image/jpeg" {
		t.Fatal(mime, err)
	}
	if b := decodeOut(t, out, mime).Bounds(); b.Dx() != 300 || b.Dy() != 200 {
		t.Fatalf("size %v", b)
	}
}

func TestTransformFocalPicksHalf(t *testing.T) {
	src := encodePNG(t, halves(1000, 500)) // red left, blue right
	p := Preset{Name: "sq", Width: 100, Height: 100, Fit: Cover}
	for _, tc := range []struct {
		fx      float64
		r, g, b uint8
	}{{0, 255, 0, 0}, {1, 0, 0, 255}} {
		out, mime, err := Transform(bg, bytes.NewReader(src), p, 100, &Focal{tc.fx, 0.5}, 0)
		if err != nil {
			t.Fatal(err)
		}
		img := decodeOut(t, out, mime)
		if !near(img.At(10, 50), tc.r, tc.g, tc.b) || !near(img.At(90, 50), tc.r, tc.g, tc.b) {
			t.Errorf("focal %v: wrong half", tc.fx)
		}
	}
}

func TestTransformNoUpscale(t *testing.T) {
	src := encodePNG(t, halves(200, 100))
	out, mime, err := Transform(bg, bytes.NewReader(src), Preset{Name: "h", Width: 800}, 800, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if b := decodeOut(t, out, mime).Bounds(); b.Dx() != 200 || b.Dy() != 100 {
		t.Fatalf("size %v", b)
	}
	out, mime, err = Transform(bg, bytes.NewReader(src), Preset{Name: "h", Width: 800, Upscale: true}, 800, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if b := decodeOut(t, out, mime).Bounds(); b.Dx() != 800 || b.Dy() != 400 {
		t.Fatalf("upscaled size %v", b)
	}
}

func TestTransformMaxPixels(t *testing.T) {
	src := encodePNG(t, halves(100, 100))
	if _, _, err := Transform(bg, bytes.NewReader(src), Preset{Width: 50}, 50, nil, 9999); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := Transform(bg, bytes.NewReader(src), Preset{Width: 50}, 50, nil, 10000); err != nil {
		t.Fatal(err)
	}
	// A header that claims a huge size is refused without decoding the (missing) pixel data.
	var hdr bytes.Buffer
	hdr.Write(encodePNG(t, halves(1, 1))[:8])
	chunk := make([]byte, 13)
	binary.BigEndian.PutUint32(chunk[0:], 60000)
	binary.BigEndian.PutUint32(chunk[4:], 60000)
	chunk[8], chunk[9] = 8, 2
	hdr.Write(binary.BigEndian.AppendUint32(nil, 13))
	hdr.WriteString("IHDR")
	hdr.Write(chunk)
	hdr.Write(binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(append([]byte("IHDR"), chunk...))))
	if _, _, err := Transform(bg, &hdr, Preset{Width: 50}, 50, nil, 50_000_000); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransformUnsupportedAndCancelled(t *testing.T) {
	if _, _, err := Transform(bg, bytes.NewReader([]byte("not an image")), Preset{Width: 50}, 50, nil, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := Transform(bg, bytes.NewReader(nil), Preset{Width: 50}, 50, nil, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("empty: %v", err)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, _, err := Transform(ctx, bytes.NewReader(encodePNG(t, halves(10, 10))), Preset{Width: 5}, 5, nil, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := Transform(bg, bytes.NewReader(encodePNG(t, halves(10, 10))), Preset{Width: 5, Format: "webp"}, 5, nil, 0); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("webp out: %v", err)
	}
}

func TestTransformGIFFirstFrame(t *testing.T) {
	pal := color.Palette{color.White, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}}
	frame := func(c uint8) *image.Paletted {
		f := image.NewPaletted(image.Rect(0, 0, 40, 20), pal)
		for i := range f.Pix {
			f.Pix[i] = c
		}
		return f
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{frame(1), frame(2)}, Delay: []int{1, 1}}); err != nil {
		t.Fatal(err)
	}
	out, mime, err := Transform(bg, &b, Preset{Name: "g", Width: 20}, 20, nil, 0)
	if err != nil || mime != "image/png" {
		t.Fatal(mime, err)
	}
	img := decodeOut(t, out, mime)
	if img.Bounds().Dx() != 20 || img.Bounds().Dy() != 10 || !near(img.At(5, 5), 255, 0, 0) {
		t.Fatalf("gif: %v %v", img.Bounds(), img.At(5, 5))
	}
}

func TestTransformWebP(t *testing.T) {
	// 16x8 lossless, left half red, right half blue (made with cwebp), so the test needs no encoder
	data := []byte{0x52, 0x49, 0x46, 0x46, 0x26, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50, 0x56, 0x50, 0x38, 0x4c, 0x19, 0x00, 0x00, 0x00, 0x2f, 0x0f, 0xc0, 0x01, 0x00, 0x0f, 0x10, 0xf3, 0xbf, 0xff, 0xf3, 0x1f, 0x0e, 0xc4, 0x82, 0xc9, 0xfc, 0xa5, 0x3b, 0x53, 0x88, 0xe8, 0x7f, 0xe4, 0x00, 0x00}
	out, mime, err := Transform(bg, bytes.NewReader(data), Preset{Name: "w", Width: 8}, 8, nil, 0)
	if err != nil || mime != "image/png" {
		t.Fatal(mime, err)
	}
	img := decodeOut(t, out, mime)
	if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 4 || !near(img.At(1, 2), 255, 0, 0) || !near(img.At(6, 2), 0, 0, 255) {
		t.Fatalf("webp: %v", b)
	}
}

// withOrientation inserts an EXIF APP1 segment declaring orientation o after the SOI marker.
func withOrientation(t testing.TB, jpg []byte, o uint16) []byte {
	t.Helper()
	tiff := []byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, byte(o >> 8), byte(o), 0, 0, 0, 0, 0, 0}
	seg := append([]byte("Exif\x00\x00"), tiff...)
	out := []byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(out[4:], uint16(len(seg)+2))
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestTransformOrientations(t *testing.T) {
	// stored 60x30: red top-left quadrant, green top-right, blue bottom-left, yellow bottom-right
	stored := image.NewNRGBA(image.Rect(0, 0, 60, 30))
	cols := [2][2]color.NRGBA{{{255, 0, 0, 255}, {0, 255, 0, 255}}, {{0, 0, 255, 255}, {255, 255, 0, 255}}}
	for y := range 30 {
		for x := range 60 {
			stored.Set(x, y, cols[y/15][x/30])
		}
	}
	base := encodeJPEG(t, stored)
	// where each stored corner (TL, TR, BL, BR) shows up in the displayed image: [x][y] of the displayed
	// corner as 0 = low, 1 = high
	type pos [2]int
	want := map[uint16][4]pos{
		1: {{0, 0}, {1, 0}, {0, 1}, {1, 1}},
		2: {{1, 0}, {0, 0}, {1, 1}, {0, 1}},
		3: {{1, 1}, {0, 1}, {1, 0}, {0, 0}},
		4: {{0, 1}, {1, 1}, {0, 0}, {1, 0}},
		5: {{0, 0}, {0, 1}, {1, 0}, {1, 1}},
		6: {{1, 0}, {1, 1}, {0, 0}, {0, 1}},
		7: {{1, 1}, {1, 0}, {0, 1}, {0, 0}},
		8: {{0, 1}, {0, 0}, {1, 1}, {1, 0}},
	}
	corner := [4]color.NRGBA{cols[0][0], cols[0][1], cols[1][0], cols[1][1]}
	p := Preset{Name: "o", Width: 60, Height: 60}
	for o := uint16(1); o <= 8; o++ {
		src := base
		if o > 1 {
			src = withOrientation(t, base, o)
		}
		out, mime, err := Transform(bg, bytes.NewReader(src), p, 60, nil, 0)
		if err != nil {
			t.Fatalf("o=%d: %v", o, err)
		}
		img := decodeOut(t, out, mime)
		b := img.Bounds()
		wantW, wantH := 60, 30
		if o >= 5 {
			wantW, wantH = 30, 60
		}
		if b.Dx() != wantW || b.Dy() != wantH {
			t.Fatalf("o=%d: size %v", o, b)
		}
		for i, c := range corner {
			x, y := 3+want[o][i][0]*(wantW-7), 3+want[o][i][1]*(wantH-7)
			if !near(img.At(x, y), c.R, c.G, c.B) {
				t.Errorf("o=%d: corner %d at (%d,%d) = %v, want %v", o, i, x, y, img.At(x, y), c)
			}
		}
	}
}

func TestTransformOrientedCrop(t *testing.T) {
	// stored 60x30 with orientation 6 shows 30x60: stored left half (red) is displayed on top
	stored := halves(60, 30)
	src := withOrientation(t, encodeJPEG(t, stored), 6)
	p := Preset{Name: "sq", Width: 30, Height: 30, Fit: Cover}
	for _, tc := range []struct {
		fy      float64
		r, g, b uint8
	}{{0, 255, 0, 0}, {1, 0, 0, 255}} {
		out, mime, err := Transform(bg, bytes.NewReader(src), p, 30, &Focal{0.5, tc.fy}, 0)
		if err != nil {
			t.Fatal(err)
		}
		img := decodeOut(t, out, mime)
		if img.Bounds().Dx() != 30 || img.Bounds().Dy() != 30 || !near(img.At(15, 15), tc.r, tc.g, tc.b) {
			t.Errorf("fy=%v: %v %v", tc.fy, img.Bounds(), img.At(15, 15))
		}
	}
}

func TestTransformStripsMetadata(t *testing.T) {
	src := withOrientation(t, encodeJPEG(t, halves(40, 20)), 1)
	out, _, err := Transform(bg, bytes.NewReader(src), Preset{Name: "x", Width: 40}, 40, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("Exif")) {
		t.Fatal("EXIF survived")
	}
}

func TestTransformJPEGFromTransparentPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20)) // fully transparent
	out, mime, err := Transform(bg, bytes.NewReader(encodePNG(t, img)), Preset{Name: "j", Width: 10, Format: "jpeg"}, 10, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeOut(t, out, mime).At(5, 5); !near(got, 255, 255, 255) {
		t.Fatalf("background %v", got)
	}
}

func TestTransformLargeReduction(t *testing.T) {
	out, mime, err := Transform(bg, bytes.NewReader(encodePNG(t, halves(2000, 1000))), Preset{Name: "t", Width: 100}, 100, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeOut(t, out, mime)
	if b := img.Bounds(); b.Dx() != 100 || b.Dy() != 50 || !near(img.At(5, 25), 255, 0, 0) || !near(img.At(95, 25), 0, 0, 255) {
		t.Fatalf("%v", b)
	}
}

func BenchmarkTransformLargeJPEG(b *testing.B) {
	w, h := 6000, 4000 // 24 MP
	img := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	for i := range img.Y {
		img.Y[i] = uint8(i * 7)
	}
	for i := range img.Cb {
		img.Cb[i], img.Cr[i] = uint8(i), uint8(i>>3)
	}
	src := encodeJPEG(b, img)
	p := Preset{Name: "hero", Width: 1600, Format: "jpeg", Quality: 80}
	b.ResetTimer()
	for range b.N {
		if _, _, err := Transform(bg, bytes.NewReader(src), p, 1600, nil, 50_000_000); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTransformDefaultsMaxPixels(t *testing.T) {
	var hdr bytes.Buffer
	hdr.Write(encodePNG(t, halves(1, 1))[:8])
	chunk := make([]byte, 13)
	binary.BigEndian.PutUint32(chunk[0:], 60000)
	binary.BigEndian.PutUint32(chunk[4:], 60000)
	chunk[8], chunk[9] = 8, 2
	hdr.Write(binary.BigEndian.AppendUint32(nil, 13))
	hdr.WriteString("IHDR")
	hdr.Write(chunk)
	hdr.Write(binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(append([]byte("IHDR"), chunk...))))
	if _, _, err := Transform(bg, &hdr, Preset{Width: 50}, 50, nil, 0); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestTransformGIFOffsetFrame(t *testing.T) {
	pal := color.Palette{color.White, color.RGBA{255, 0, 0, 255}}
	f := image.NewPaletted(image.Rect(20, 10, 40, 20), pal)
	for i := range f.Pix {
		f.Pix[i] = 1
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{f}, Delay: []int{1}, Config: image.Config{ColorModel: pal, Width: 40, Height: 20}}); err != nil {
		t.Fatal(err)
	}
	out, mime, err := Transform(bg, &b, Preset{Name: "g", Width: 40}, 40, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	img := decodeOut(t, out, mime)
	if img.Bounds().Dx() != 40 || img.Bounds().Dy() != 20 || !near(img.At(30, 15), 255, 0, 0) {
		t.Fatalf("%v", img.Bounds())
	}
	if _, _, _, a := img.At(5, 5).RGBA(); a != 0 {
		t.Fatalf("outside the frame is not transparent: alpha %d", a)
	}
}
