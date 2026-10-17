package media

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	_ "image/gif" // register decoders for image.DecodeConfig
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"unicode"

	_ "golang.org/x/image/webp"
)

// Detected is what the server learned from a file's bytes.
type Detected struct {
	MIME          string // media type without parameters
	Width, Height int    // 0 when unknown or not an image
}

// ErrInvalidImage is returned for a file whose content says image but does not parse as one.
var ErrInvalidImage = errors.New("media: not a valid image")

// SVG is the media type of scalable vector graphics.
const SVG = "image/svg+xml"

// decodable are the image types whose header is parsed for dimensions; a file that sniffs as one of them but
// does not parse is rejected.
var decodable = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true}

// Detect determines a file's type from its content (never from what the client claims; name is only a hint for
// content that has no signature, such as CSV or Office files) and reads image dimensions. r must be positioned
// at the start; Detect reads at most a header (SVG: the first 64 KiB).
func Detect(r io.ReadSeeker, name string) (Detected, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(r, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return Detected{}, err
	}
	head = head[:n]
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return Detected{}, err
	}
	typ, _, _ := mime.ParseMediaType(http.DetectContentType(head))
	if typ == "" {
		typ = "application/octet-stream"
	}
	var d Detected
	switch typ {
	case "text/xml", "text/plain", "text/html", "application/octet-stream":
		if w, h, ok := svgSize(r); ok {
			d = Detected{MIME: SVG, Width: w, Height: h}
		}
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return Detected{}, err
		}
	}
	if d.MIME != "" {
		return d, nil
	}
	typ = refine(typ, name)
	d.MIME = typ
	if decodable[typ] {
		cfg, format, err := image.DecodeConfig(r)
		if err != nil || "image/"+format != typ || cfg.Width <= 0 || cfg.Height <= 0 {
			return Detected{}, ErrInvalidImage
		}
		d.Width, d.Height = cfg.Width, cfg.Height
		if _, err := r.Seek(0, io.SeekStart); err != nil {
			return Detected{}, err
		}
	}
	return d, nil
}

// refine uses the extension for content the signature sniffer can only call generic, but never to produce a
// type that browsers execute or render as a document.
func refine(typ, name string) string {
	if typ != "text/plain" && typ != "application/octet-stream" && typ != "application/zip" {
		return typ
	}
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		return typ
	}
	byExt, _, err := mime.ParseMediaType(mime.TypeByExtension(ext))
	if err != nil || byExt == "" {
		return typ
	}
	if !nameMayChoose(byExt) {
		return typ
	}
	return byExt
}

// nameMayChoose reports whether a file name may give a file this type: never one that browsers execute or
// render as a document or media.
func nameMayChoose(typ string) bool {
	switch {
	case strings.Contains(typ, "html"), strings.Contains(typ, "xml"), strings.Contains(typ, "javascript"),
		strings.Contains(typ, "svg"), strings.HasPrefix(typ, "image/"), strings.HasPrefix(typ, "video/"),
		strings.HasPrefix(typ, "audio/"), typ == "application/pdf":
		return false
	}
	return true
}

// svgSize reports whether r holds an SVG document (its root element is <svg>) and its size in pixels, taken
// from width and height, else from the viewBox; 0 when it declares neither.
func svgSize(r io.Reader) (w, h int, ok bool) {
	br := bufio.NewReader(io.LimitReader(r, 64<<10))
	if head, _ := br.Peek(3); bytes.Equal(head, []byte("\xef\xbb\xbf")) {
		_, _ = br.Discard(3)
	}
	dec := xml.NewDecoder(br)
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, false
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local != "svg" || (t.Name.Space != "" && t.Name.Space != "http://www.w3.org/2000/svg") {
				return 0, 0, false
			}
			var width, height string
			var viewBox []string
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "width":
					width = a.Value
				case "height":
					height = a.Value
				case "viewBox":
					viewBox = strings.FieldsFunc(a.Value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
				}
			}
			w, h = svgLength(width), svgLength(height)
			if (w == 0 || h == 0) && len(viewBox) == 4 {
				vw, _ := strconv.ParseFloat(viewBox[2], 64)
				vh, _ := strconv.ParseFloat(viewBox[3], 64)
				if w == 0 && h == 0 {
					w, h = int(vw+0.5), int(vh+0.5)
				} else if w == 0 && vh > 0 {
					w = int(float64(h)*vw/vh + 0.5)
				} else if h == 0 && vw > 0 {
					h = int(float64(w)*vh/vw + 0.5)
				}
			}
			return w, h, true
		case xml.ProcInst, xml.Comment, xml.Directive:
		case xml.CharData:
			if len(bytes.TrimSpace(t)) > 0 {
				return 0, 0, false
			}
		}
	}
}

// svgLength reads a plain or px length ("120", "120.5px"); other units and percentages are unknown.
func svgLength(v string) int {
	v = strings.TrimSuffix(strings.TrimSpace(v), "px")
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 || f > 1<<20 {
		return 0
	}
	return int(f + 0.5)
}

// matchAccept reports whether typ matches one of the patterns ("image/*", "application/pdf", "*/*"); no
// patterns accept everything.
func matchAccept(patterns []string, typ string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "*" || p == "*/*", p == typ:
			return true
		case strings.HasSuffix(p, "/*") && strings.HasPrefix(typ, p[:len(p)-1]):
			return true
		}
	}
	return false
}
