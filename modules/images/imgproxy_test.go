package images

import (
	"encoding/hex"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSign(t *testing.T) {
	key := mustHex(t, "943b421c9eb07c830af81030552c86009268de4e532ba2ee2eab8247c6da0881")
	salt := mustHex(t, "520f986b998545b4785e0defbc4f3c1203f22de2374a3d53cb7a7fe9fea309c5")
	// computed independently with Python's hmac
	got := Sign(key, salt, "/rs:fill:300:400:0/g:sm/aHR0cDovL2ltZy5leGFtcGxlLmNvbS9wcmV0dHkvaW1hZ2UuanBn.png")
	if want := "Doh68Kklui8ULk4PZhaEESn42s4sw40HsXB7pNJvuvI"; got != want {
		t.Fatalf("signature %s, want %s", got, want)
	}
}

func TestImgproxyURL(t *testing.T) {
	key := mustHex(t, "943b421c9eb07c830af81030552c86009268de4e532ba2ee2eab8247c6da0881")
	salt := mustHex(t, "520f986b998545b4785e0defbc4f3c1203f22de2374a3d53cb7a7fe9fea309c5")
	x := Imgproxy{Base: "https://img.example.com/", Key: key, Salt: salt, Source: "https://cms.example.com/assets/{sha}/{name}"}
	card := Preset{Name: "card", Width: 640, Height: 360, Fit: "cover", Quality: 70}
	sha := strings.Repeat("ab", 32)

	got := x.URL(sha, "my cat.png", card, 320, &Focal{0.25, 0.123456}, "image/png")
	path := "/rs:fill:320:180:0/g:fp:0.25:0.1235/q:70/plain/" +
		"https%3A%2F%2Fcms.example.com%2Fassets%2F" + sha + "%2Fmy%2520cat.png@png"
	want := "https://img.example.com/" + Sign(key, salt, path) + path
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}

	if got := x.URL(sha, "a.jpg", card, 640, nil, "image/jpeg"); !strings.Contains(got, "/rs:fill:640:360:0/g:ce/q:70/plain/") || !strings.HasSuffix(got, "@jpg") {
		t.Fatalf("no focal: %s", got)
	}
	hero := Preset{Name: "hero", Width: 1600, Format: "webp", Upscale: true}
	got = x.URL(sha, "a.png", hero, 800, &Focal{1, 1}, "image/png")
	if !strings.Contains(got, "/rs:fit:800:0:1/q:82/plain/") || strings.Contains(got, "g:") || !strings.HasSuffix(got, "@webp") {
		t.Fatalf("contain: %s", got)
	}

	s3 := Imgproxy{Base: "https://i", Source: "s3://bucket/media/{sha}"}
	if got := s3.URL(sha, "x.gif", hero, 800, nil, "image/gif"); !strings.HasPrefix(got, "https://i/insecure/rs:fit:800:0:1/q:82/plain/s3%3A%2F%2Fbucket%2Fmedia%2F"+sha+"@webp") {
		t.Fatalf("insecure: %s", got)
	}
}
