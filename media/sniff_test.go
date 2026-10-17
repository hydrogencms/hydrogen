package media

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func jpegBytes(w, h int) []byte {
	var b bytes.Buffer
	_ = jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil)
	return b.Bytes()
}

func gifBytes(w, h int) []byte {
	var b bytes.Buffer
	_ = gif.Encode(&b, image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White}), nil)
	return b.Bytes()
}

// A 1x1 lossless WebP.
var webp1x1, _ = base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")

func TestDetect(t *testing.T) {
	pdf := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n")
	tests := []struct {
		name, file string
		data       []byte
		want       Detected
	}{
		{"png", "a.png", pngBytes(30, 20), Detected{"image/png", 30, 20}},
		{"png wrong name", "a.pdf", pngBytes(3, 2), Detected{"image/png", 3, 2}},
		{"jpeg", "a.jpg", jpegBytes(64, 48), Detected{"image/jpeg", 64, 48}},
		{"gif", "a.gif", gifBytes(7, 9), Detected{"image/gif", 7, 9}},
		{"webp", "a.webp", webp1x1, Detected{"image/webp", 1, 1}},
		{"pdf", "a.pdf", pdf, Detected{MIME: "application/pdf"}},
		{"text", "notes.txt", []byte("plain words"), Detected{MIME: "text/plain"}},
		{"csv by extension", "data.csv", []byte("a,b\n1,2\n"), Detected{MIME: "text/csv"}},
		{"unknown binary", "blob.bin", []byte{0, 1, 2, 3, 0xff, 0xfe}, Detected{MIME: "application/octet-stream"}},
		{"html stays html", "page.html", []byte("<!DOCTYPE html><html><body>x</body></html>"), Detected{MIME: "text/html"}},
		{"script in a .txt-less file is not promoted", "x.js", []byte("alert(1)"), Detected{MIME: "text/plain"}},
		{"svg", "a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="80"><rect/></svg>`), Detected{SVG, 120, 80}},
		{"svg with prolog", "a.bin", []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<!-- c -->\n<!DOCTYPE svg PUBLIC \"-//W3C//DTD SVG 1.1//EN\" \"x\">\n<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 24 12\"></svg>"), Detected{SVG, 24, 12}},
		{"svg height from viewBox", "a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="100" viewBox="0 0 50 25"/>`), Detected{SVG, 100, 50}},
		{"svg without size", "a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="50%"/>`), Detected{SVG, 0, 0}},
		{"xml is not svg", "a.xml", []byte(`<?xml version="1.0"?><root><svg/></root>`), Detected{MIME: "text/xml"}},
		{"text before svg is not svg", "a.svg", []byte(`hello <svg xmlns="http://www.w3.org/2000/svg"/>`), Detected{MIME: "text/plain"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewReader(tc.data)
			got, err := Detect(r, tc.file)
			if err != nil || got != tc.want {
				t.Fatalf("Detect = %+v, %v; want %+v", got, err, tc.want)
			}
			if pos, _ := r.Seek(0, 1); pos != 0 {
				t.Fatalf("reader left at %d", pos)
			}
		})
	}
}

func TestDetectBrokenImage(t *testing.T) {
	data := pngBytes(10, 10)
	for name, d := range map[string][]byte{"truncated header": data[:12], "garbage after magic": append(data[:8:8], bytes.Repeat([]byte{0xAB}, 40)...)} {
		if _, err := Detect(bytes.NewReader(d), "a.png"); !errors.Is(err, ErrInvalidImage) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestMatchAccept(t *testing.T) {
	for _, tc := range []struct {
		patterns []string
		typ      string
		want     bool
	}{
		{nil, "anything/else", true},
		{[]string{"image/*"}, "image/png", true},
		{[]string{"image/*"}, "application/pdf", false},
		{[]string{"image/*", "application/pdf"}, "application/pdf", true},
		{[]string{"image/png"}, "image/jpeg", false},
		{[]string{"*/*"}, "text/plain", true},
		{[]string{"Image/*"}, "image/gif", true},
		{[]string{"imag/*"}, "image/gif", false},
	} {
		if got := matchAccept(tc.patterns, tc.typ); got != tc.want {
			t.Errorf("matchAccept(%v, %q) = %v", tc.patterns, tc.typ, got)
		}
	}
}

func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"photo.jpg": "photo.jpg", "../../etc/passwd": "passwd", `C:\Users\me\a b.png`: "a b.png", "": "file", "..": "file",
		"a\x00b\nc.txt": "abc.txt", "  spaced.txt ": "spaced.txt", "dir/": "dir",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
	long := cleanName(string(bytes.Repeat([]byte("é"), 300)))
	if len(long) > 200 || long == "" {
		t.Errorf("long name = %d bytes", len(long))
	}
}
