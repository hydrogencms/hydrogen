package images

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// Imgproxy builds signed URLs for an imgproxy server that reads the original through Source.
type Imgproxy struct {
	Base      string // the imgproxy server's base URL
	Key, Salt []byte
	Source    string // with {sha} and {name}
}

// URL is the imgproxy URL of the variant of width w. Without a key the URL is unsigned ("insecure").
func (x Imgproxy) URL(sha, name string, p Preset, w int, focal *Focal, srcMIME string) string {
	rs := "fit"
	if p.fit() == Cover {
		rs = "fill"
	}
	h := 0
	if p.Height > 0 {
		h = max(int(math.Round(float64(p.Height)*float64(w)/float64(p.Width))), 1)
	}
	enlarge := "0"
	if p.Upscale {
		enlarge = "1"
	}
	opts := "rs:" + rs + ":" + strconv.Itoa(w) + ":" + strconv.Itoa(h) + ":" + enlarge
	if p.fit() == Cover && p.Height > 0 {
		if focal != nil {
			opts += "/g:fp:" + num(clamp01(focal.X)) + ":" + num(clamp01(focal.Y))
		} else {
			opts += "/g:ce"
		}
	}
	opts += "/q:" + strconv.Itoa(p.quality())
	src := strings.NewReplacer("{sha}", sha, "{name}", url.PathEscape(name)).Replace(x.Source)
	ext, _ := OutputFormat(p, srcMIME)
	path := "/" + opts + "/plain/" + escapeSource(src) + "@" + ext
	sig := "insecure"
	if len(x.Key) > 0 {
		sig = Sign(x.Key, x.Salt, path)
	}
	return strings.TrimRight(x.Base, "/") + "/" + sig + path
}

// Sign is imgproxy's URL signature: base64url without padding of HMAC-SHA256(key, salt ‖ path).
func Sign(key, salt []byte, path string) string {
	m := hmac.New(sha256.New, key)
	m.Write(salt)
	m.Write([]byte(path))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func num(f float64) string { return strconv.FormatFloat(math.Round(f*10000)/10000, 'f', -1, 64) }

// escapeSource percent-escapes everything but unreserved characters, as imgproxy expects of plain sources.
func escapeSource(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := range len(s) {
		switch c := s[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}
