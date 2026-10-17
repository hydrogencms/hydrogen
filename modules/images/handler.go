package images

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/features/media"
)

// maxVariantBytes bounds a stored variant read back into memory.
const maxVariantBytes = 64 << 20

func itoa(n int) string { return strconv.Itoa(n) }

func originOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// base is where variant URLs start: public_url, else /images.
func (m *module) base() string {
	if m.s.publicURL != "" {
		return m.s.publicURL
	}
	return URLBase
}

// builtinURL is the module's own URL of a variant; focal is the URL token of the focal point ("c" or four digits).
func (m *module) builtinURL(p Preset, w int, focal, sha, name, srcMIME string) string {
	ext, _ := OutputFormat(p, srcMIME)
	return m.base() + "/" + p.Name + "/" + m.versions[p.Name] + "/" + itoa(w) + "/" + focal + "/" + sha + "/" +
		url.PathEscape(OutputName(name, ext))
}

func suffixOf(preset, v string, w int, focal string) string {
	return preset + "-" + v + "-" + itoa(w) + "-" + focal
}

// focalToken quantizes a focal point to the "XXYY" URL token (hundredths, each 0–99); no focal is "c".
func focalToken(f *Focal) string {
	if f == nil {
		return "c"
	}
	q := func(v float64) int { return min(max(int(math.Round(v*100)), 0), 99) }
	return fmt.Sprintf("%02d%02d", q(f.X), q(f.Y))
}

// parseFocal reads a URL token: "c" is the centre (nil), "XXYY" a quantized focal point.
func parseFocal(tok string) (f *Focal, ok bool) {
	if tok == "c" {
		return nil, true
	}
	if len(tok) != 4 {
		return nil, false
	}
	for i := range 4 {
		if tok[i] < '0' || tok[i] > '9' {
			return nil, false
		}
	}
	x, _ := strconv.Atoi(tok[:2])
	y, _ := strconv.Atoi(tok[2:])
	return &Focal{X: float64(x) / 100, Y: float64(y) / 100}, true
}

// focalFor is the URL token a delivered asset uses with a preset: only cover crops depend on the focal point.
func focalFor(p Preset, f *Focal) string {
	if p.fit() != Cover {
		return "c"
	}
	return focalToken(f)
}

// focalKnown reports whether some item whose file is sha has the focal point (draft or published). The
// tokens of a file are loaded together and remembered; an unknown token reloads them at most every
// focalRefresh, so made-up tokens stay cheap and a focal point just set is accepted within seconds.
func (m *module) focalKnown(ctx context.Context, sha, tok string) (bool, error) {
	if m.focals == nil {
		return false, nil
	}
	set, ok := m.focalCache.get(sha)
	if ok && (set.tokens[tok] || m.clock.Now().Sub(set.at) < focalRefresh) {
		return set.tokens[tok], nil
	}
	tokens, err := m.focals(ctx, sha)
	if err != nil {
		return false, err
	}
	m.focalCache.put(sha, focalSet{tokens: tokens, at: m.clock.Now()}, focalTTL)
	return tokens[tok], nil
}

// lookup is media.Lookup remembered for a while: a file's record does not change.
func (m *module) lookup(ctx context.Context, sha string) (media.File, error) {
	if e, ok := m.fileCache.get(sha); ok {
		return e.file, e.err
	}
	file, err := m.media.Lookup(ctx, sha)
	switch {
	case err == nil:
		m.fileCache.put(sha, fileEntry{file: file}, lookupTTL)
	case errors.Is(err, media.ErrNotFound):
		m.fileCache.put(sha, fileEntry{err: err}, missTTL)
	}
	return file, err
}

type fileEntry struct {
	file media.File
	err  error
}

// canonicalName is the name imgproxy gets for a source: requests may carry any name, the file has one.
func canonicalName(mime string) string {
	switch mime {
	case "image/jpeg":
		return "image.jpg"
	case "image/gif":
		return "image.gif"
	case "image/webp":
		return "image.webp"
	}
	return "image.png"
}

// flight is a transform that concurrent requests for the same variant share.
type flight struct {
	done      chan struct{}
	data      []byte
	generated bool
	err       error
}

func (m *module) handler() http.Handler {
	return http.HandlerFunc(m.serve)
}

func (m *module) serve(w http.ResponseWriter, r *http.Request) {
	if m.media == nil {
		http.NotFound(w, r)
		return
	}
	p, ok := m.s.presets[r.PathValue("preset")]
	width, err := strconv.Atoi(r.PathValue("w"))
	if !ok || err != nil || !slices.Contains(p.Allowed(), width) {
		http.NotFound(w, r)
		return
	}
	focal, ok := parseFocal(r.PathValue("focal"))
	if !ok || focal != nil && p.fit() != Cover {
		http.NotFound(w, r)
		return
	}
	tok := r.PathValue("focal")
	sha, name := r.PathValue("sha"), r.PathValue("name")
	file, err := m.lookup(r.Context(), sha)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if !Decodable(file.MIME) || file.Width < 1 || file.Height < 1 || int64(file.Width)*int64(file.Height) > m.s.maxPixels {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	v := m.versions[p.Name]
	if r.PathValue("v") != v {
		h.Set("Cache-Control", "public, max-age=3600")
		http.Redirect(w, r, m.builtinURL(p, width, tok, sha, name, file.MIME), http.StatusFound)
		return
	}
	if m.s.driver == driverImgproxy {
		if focal != nil {
			if ok, err := m.focalKnown(r.Context(), sha, tok); err != nil || !ok {
				m.fail(w, r, errOr(err))
				return
			}
		}
		h.Set("Cache-Control", "public, max-age=86400")
		http.Redirect(w, r, m.s.proxy.URL(sha, canonicalName(file.MIME), p, width, focal, file.MIME), http.StatusFound)
		return
	}
	suffix := suffixOf(p.Name, v, width, tok)
	etag := `"` + sha + "-" + suffix + `"`
	_, typ := OutputFormat(p, file.MIME)
	h.Set("ETag", etag)
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	if r.Header.Get("Range") == "" && matches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, how, err := m.variant(r.Context(), sha, suffix, p, width, focal, tok)
	if err != nil {
		h.Del("ETag")
		h.Del("Cache-Control")
		m.fail(w, r, err)
		return
	}
	h.Set("Content-Type", typ)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Hydrogen-Image", how)
	h.Set("Content-Disposition", disposition(name))
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// errOr is err, or media.ErrNotFound (a 404) when a check simply said no.
func errOr(err error) error {
	if err != nil {
		return err
	}
	return media.ErrNotFound
}

func (m *module) fail(w http.ResponseWriter, r *http.Request, err error) {
	// A 404 can turn into an image soon (a focal point just set), and the success headers may already be set.
	w.Header().Del("ETag")
	w.Header().Set("Cache-Control", "no-store")
	switch {
	case errors.Is(err, media.ErrNotFound), errors.Is(err, ErrTooLarge), errors.Is(err, ErrUnsupported):
		http.NotFound(w, r)
	case errors.Is(err, errBusy):
		w.Header().Set("Retry-After", "5")
		http.Error(w, "busy, try again", http.StatusServiceUnavailable)
	case r.Context().Err() != nil:
		// the client went away
	default:
		if m.log != nil {
			m.log.Warn("images: serve failed", "path", r.URL.Path, "err", err)
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// variant returns the bytes of a variant and how it came: "stored" from media storage or "generated" now.
func (m *module) variant(ctx context.Context, sha, suffix string, p Preset, width int, focal *Focal, tok string) ([]byte, string, error) {
	if data, ok, err := m.stored(ctx, sha, suffix); err != nil {
		return nil, "", err
	} else if ok {
		return data, "stored", nil
	}
	if focal != nil {
		if ok, err := m.focalKnown(ctx, sha, tok); err != nil || !ok {
			return nil, "", errOr(err)
		}
	}
	key := sha + "-" + suffix
	m.mu.Lock()
	f, running := m.flights[key]
	if !running {
		f = &flight{done: make(chan struct{})}
		m.flights[key] = f
		go m.lead(context.WithoutCancel(ctx), f, key, sha, suffix, p, width, focal)
	}
	m.mu.Unlock()
	select {
	case <-f.done:
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	if f.err != nil {
		return nil, "", f.err
	}
	how := "stored"
	if f.generated {
		how = "generated"
	}
	return f.data, how, nil
}

// stored reads a stored variant; only a missing one is a miss, other storage errors are returned.
func (m *module) stored(ctx context.Context, sha, suffix string) ([]byte, bool, error) {
	rc, _, err := m.media.OpenDerived(ctx, sha, suffix)
	if errors.Is(err, media.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxVariantBytes))
	if err != nil {
		return nil, false, err
	}
	return data, len(data) > 0, nil
}

// lead makes a variant detached from the request that asked first: the transform finishes and is stored even
// when that client leaves, and everyone waiting for the same key gets the result.
func (m *module) lead(ctx context.Context, f *flight, key, sha, suffix string, p Preset, width int, focal *Focal) {
	defer func() {
		m.mu.Lock()
		delete(m.flights, key)
		m.mu.Unlock()
		close(f.done)
	}()
	defer func() {
		if v := recover(); v != nil {
			f.err = fmt.Errorf("images: transform panicked: %v", v)
			if m.log != nil {
				m.log.Error("images: transform panicked", "key", key, "panic", v)
			}
		}
	}()
	if data, ok, err := m.stored(ctx, sha, suffix); err != nil || ok {
		f.data, f.err = data, err
		return
	}
	if f.err = m.acquire(); f.err != nil {
		return
	}
	defer func() { <-m.sem }()
	tctx, cancel := context.WithTimeout(ctx, m.limit)
	defer cancel()
	src, _, err := m.media.Open(tctx, sha)
	if err != nil {
		f.err = err
		return
	}
	defer func() { _ = src.Close() }()
	data, typ, err := m.transform(tctx, src, p, width, focal, m.s.maxPixels)
	if err != nil {
		f.err = err
		return
	}
	f.data, f.generated = data, true
	pctx, pcancel := context.WithTimeout(ctx, putTimeout)
	defer pcancel()
	if err := m.media.PutDerived(pctx, sha, suffix, bytes.NewReader(data), int64(len(data)), typ); err != nil && m.log != nil {
		m.log.Warn("images: storing a variant failed", "key", key, "err", err)
	}
}

// acquire waits for a free worker, at most m.wait.
func (m *module) acquire() error {
	select {
	case m.sem <- struct{}{}:
		return nil
	default:
	}
	t := time.NewTimer(m.wait)
	defer t.Stop()
	select {
	case m.sem <- struct{}{}:
		return nil
	case <-t.C:
		return errBusy
	}
}

// matches reports whether an If-None-Match header names etag (weak validators compare equal).
func matches(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, c := range strings.Split(header, ",") {
		c = strings.TrimPrefix(strings.TrimSpace(c), "W/")
		if c == "*" || c == etag {
			return true
		}
	}
	return false
}

func disposition(name string) string {
	name = path.Base(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, name))
	if name == "." || name == "/" || name == "" {
		return "inline"
	}
	if v := mime.FormatMediaType("inline", map[string]string{"filename": name}); v != "" {
		return v
	}
	return "inline"
}
