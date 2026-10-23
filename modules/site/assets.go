package site

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	// maxCSSBytes bounds a style sheet that is read to rewrite its url() references; a larger one is served as is.
	maxCSSBytes = 4 << 20
	// cssCacheBytes bounds the rewritten style sheets kept in memory; beyond it they are recomputed per request.
	cssCacheBytes = 8 << 20
)

// assetSet serves the files of site/assets under fingerprinted names. Files are listed at load, hashed on first
// need and read from their file system per request. A dev reload builds a new set, which drops what was hashed.
type assetSet struct {
	files map[string]layered // name → file

	mu       sync.Mutex
	prints   map[string]assetPrint // name → hash, once computed
	css      map[string][]byte     // name → rewritten style sheet (bounded by cssCacheBytes)
	cssBytes int
}

type assetPrint struct {
	sum string // hex SHA-256 of the served bytes
	fp  string // the fingerprinted name
	// cyclic is set on a style sheet that references itself through others; references to it stay unrewritten,
	// so that no fingerprint depends on which sheet was asked for first.
	cyclic bool
}

func loadAssets(chain []Theme, dir string) (*assetSet, error) {
	list, err := files(layers(chain, dir, "assets"))
	if err != nil {
		return nil, err
	}
	a := &assetSet{files: make(map[string]layered, len(list)), prints: map[string]assetPrint{}, css: map[string][]byte{}}
	for _, f := range list {
		a.files[f.name] = f
	}
	return a, nil
}

func isCSS(name string) bool { return strings.EqualFold(path.Ext(name), ".css") }

// url is the fingerprinted URL of an asset; false when there is no such file.
func (a *assetSet) url(name string) (string, bool, error) {
	name = strings.TrimPrefix(name, "/")
	if _, ok := a.files[name]; !ok {
		return "", false, nil
	}
	p, _, err := a.print(name, nil)
	if err != nil {
		return "", false, err
	}
	return assetsURL + p.fp, true, nil
}

// print hashes an asset, once. stack holds the style sheets being rewritten above it; a reference back into
// the stack stays unrewritten, and low is the shallowest stack index such a reference reached (len(stack)+1 when
// none). A result that depends on a sheet above it is not memoised and its callers leave the reference as is.
func (a *assetSet) print(name string, stack []string) (assetPrint, int, error) {
	low := len(stack) + 1
	a.mu.Lock()
	p, ok := a.prints[name]
	a.mu.Unlock()
	if ok {
		return p, low, nil
	}
	f := a.files[name]
	var sum [sha256.Size]byte
	var body []byte
	cyclic := false
	if isCSS(name) {
		var err error
		if body, low, err = a.rewrite(f, stack); err != nil {
			return p, low, err
		}
	}
	if body != nil {
		sum = sha256.Sum256(body)
		if low >= len(stack) {
			a.keepCSS(name, body)
		}
		cyclic = low <= len(stack)
	} else {
		var err error
		if sum, err = hashFile(f); err != nil {
			return p, low, err
		}
	}
	ext := path.Ext(name)
	p = assetPrint{sum: hex.EncodeToString(sum[:]), fp: strings.TrimSuffix(name, ext) + "." + hex.EncodeToString(sum[:5]) + ext, cyclic: cyclic}
	if low >= len(stack) {
		a.mu.Lock()
		a.prints[name] = p
		a.mu.Unlock()
	}
	return p, low, nil
}

func (a *assetSet) keepCSS(name string, body []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.css[name]; !ok && a.cssBytes+len(body) <= cssCacheBytes {
		a.css[name] = body
		a.cssBytes += len(body)
	}
}

func hashFile(f layered) ([sha256.Size]byte, error) {
	var sum [sha256.Size]byte
	r, err := f.fsys.Open(f.name)
	if err != nil {
		return sum, err
	}
	defer func() { _ = r.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return sum, err
	}
	h.Sum(sum[:0])
	return sum, nil
}

// rewrite returns the style sheet with its url() and @import references to assets replaced by their
// fingerprinted URLs; nil when there is nothing to change (or the file is too large to read).
func (a *assetSet) rewrite(f layered, stack []string) ([]byte, int, error) {
	low := len(stack) + 1
	a.mu.Lock()
	body, ok := a.css[f.name]
	a.mu.Unlock()
	if ok {
		return body, low, nil
	}
	if st, err := fs.Stat(f.fsys, f.name); err == nil && st.Size() > maxCSSBytes {
		return nil, low, nil
	}
	src, err := fs.ReadFile(f.fsys, f.name)
	if err != nil {
		return nil, low, err
	}
	if len(src) > maxCSSBytes {
		return nil, low, nil
	}
	stack = append(stack[:len(stack):len(stack)], f.name)
	var firstErr error
	out, changed := rewriteCSS(src, func(ref string) (string, bool) {
		target, suffix, ok := a.resolve(f.name, ref)
		if !ok {
			return "", false
		}
		if i := slices.Index(stack, target); i >= 0 {
			low = min(low, i)
			return "", false
		}
		p, l, err := a.print(target, stack)
		if err != nil {
			firstErr = err
			return "", false
		}
		low = min(low, l)
		if p.cyclic || l <= len(stack) {
			return "", false
		}
		return assetsURL + p.fp + suffix, true
	})
	if firstErr != nil {
		return nil, low, firstErr
	}
	if !changed {
		return nil, low, nil
	}
	return out, low, nil
}

// resolve maps a reference in a style sheet to the asset it names and the query/fragment suffix to keep.
func (a *assetSet) resolve(from, ref string) (name, suffix string, ok bool) {
	ref = strings.TrimSpace(ref)
	i := strings.IndexAny(ref, "?#")
	if i < 0 {
		i = len(ref)
	}
	p, suffix := ref[:i], ref[i:]
	if p == "" || strings.HasPrefix(p, "//") {
		return "", "", false
	}
	if c := strings.IndexByte(p, ':'); c >= 0 && !strings.Contains(p[:c], "/") {
		return "", "", false
	}
	switch {
	case strings.HasPrefix(p, assetsURL):
		p = strings.TrimPrefix(p, assetsURL)
	case strings.HasPrefix(p, "/"):
		p = strings.TrimPrefix(p, "/")
	default:
		p = path.Join(path.Dir(from), p)
	}
	cands := []string{p}
	if u, err := url.PathUnescape(p); err == nil && u != p {
		cands = append(cands, u)
	}
	for _, cand := range cands {
		if cand == ".." || strings.HasPrefix(cand, "../") {
			continue
		}
		if _, ok := a.files[cand]; ok {
			return cand, suffix, true
		}
	}
	return "", "", false
}

// rewriteCSS calls rep for every url() and @import reference outside comments and other strings, replacing
// the ones it answers for.
func rewriteCSS(src []byte, rep func(ref string) (string, bool)) ([]byte, bool) {
	var out bytes.Buffer
	changed, imp := false, false
	flush := 0
	emit := func(from, to int, repl string) {
		out.Write(src[flush:from])
		out.WriteString(repl)
		flush = to
		changed = true
	}
	n := len(src)
	for i := 0; i < n; {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '*':
			end := bytes.Index(src[i+2:], []byte("*/"))
			if end < 0 {
				i = n
			} else {
				i += end + 4
			}
		case c == '"' || c == '\'':
			end := stringEnd(src, i)
			if imp {
				imp = false
				if end-i >= 2 && src[end-1] == c {
					if r, ok := rep(string(src[i+1 : end-1])); ok {
						emit(i, end, string(c)+r+string(c))
					}
				}
			}
			i = end
		case c == ';':
			imp = false
			i++
		case c == '@' && hasFold(src, i, "@import"):
			imp = true
			i += len("@import")
		case (c == 'u' || c == 'U') && hasFold(src, i, "url(") && (i == 0 || !isIdent(src[i-1])):
			imp = false
			i = urlToken(src, i, rep, emit)
		default:
			i++
		}
	}
	if !changed {
		return src, false
	}
	out.Write(src[flush:])
	return out.Bytes(), true
}

// urlToken handles the url( that starts at i and returns the index after it.
func urlToken(src []byte, i int, rep func(string) (string, bool), emit func(from, to int, repl string)) int {
	n := len(src)
	j := i + 4
	for j < n && isSpace(src[j]) {
		j++
	}
	if j < n && (src[j] == '"' || src[j] == '\'') {
		end := stringEnd(src, j)
		k := end
		for k < n && isSpace(src[k]) {
			k++
		}
		if end-j >= 2 && src[end-1] == src[j] && k < n && src[k] == ')' {
			if r, ok := rep(string(src[j+1 : end-1])); ok {
				emit(j, end, string(src[j])+r+string(src[j]))
			}
			return k + 1
		}
		return end
	}
	k := j
	for k < n && src[k] != ')' && !isSpace(src[k]) {
		k++
	}
	end := k
	for end < n && isSpace(src[end]) {
		end++
	}
	if end >= n || src[end] != ')' {
		return max(k, i+4)
	}
	if r, ok := rep(string(src[j:k])); ok {
		if strings.ContainsAny(r, " \t\n\r\"'()") {
			r = `"` + r + `"`
		}
		emit(j, k, r)
	}
	return end + 1
}

// stringEnd returns the index after the string that opens at i (at a line break or the end when it is unclosed).
func stringEnd(src []byte, i int) int {
	q := src[i]
	for j := i + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			j++
		case q:
			return j + 1
		case '\n':
			return j
		}
	}
	return len(src)
}

func hasFold(src []byte, i int, word string) bool {
	return len(src)-i >= len(word) && strings.EqualFold(string(src[i:i+len(word)]), word)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

func isIdent(c byte) bool {
	return c == '-' || c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// body is what a request for the asset gets: the file, or the rewritten style sheet. done releases it.
func (a *assetSet) body(name string) (r io.ReadSeeker, mod time.Time, done func(), err error) {
	f := a.files[name]
	if isCSS(name) {
		a.mu.Lock()
		b, ok := a.css[name]
		a.mu.Unlock()
		if !ok {
			if b, _, err = a.rewrite(f, nil); err != nil {
				return nil, mod, nil, err
			}
		}
		if b != nil {
			return bytes.NewReader(b), modTime(f), func() {}, nil
		}
	}
	file, err := f.fsys.Open(f.name)
	if err != nil {
		return nil, mod, nil, err
	}
	if rs, ok := file.(io.ReadSeeker); ok {
		return rs, modTime(f), func() { _ = file.Close() }, nil
	}
	b, err := io.ReadAll(file)
	_ = file.Close()
	if err != nil {
		return nil, mod, nil, err
	}
	return bytes.NewReader(b), modTime(f), func() {}, nil
}

func modTime(f layered) time.Time {
	if st, err := fs.Stat(f.fsys, f.name); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

type assetEntry struct{ name, fp string }

// all lists every asset with its fingerprinted name, hashing all of them; the static export needs the lot.
func (a *assetSet) all() ([]assetEntry, error) {
	out := make([]assetEntry, 0, len(a.files))
	for name := range a.files {
		p, _, err := a.print(name, nil)
		if err != nil {
			return nil, err
		}
		out = append(out, assetEntry{name: name, fp: p.fp})
	}
	return out, nil
}

// lookup finds the asset a request names: a current fingerprinted name (immutable) or a plain file name.
func (a *assetSet) lookup(req string) (name string, immutable, ok bool) {
	ext := path.Ext(req)
	stem := strings.TrimSuffix(req, ext)
	if i := strings.LastIndexByte(stem, '.'); i >= 0 && len(stem)-i-1 == 10 {
		cand := stem[:i] + ext
		if _, found := a.files[cand]; found {
			if p, _, err := a.print(cand, nil); err == nil && p.fp == req {
				return cand, true, true
			}
		}
	}
	if _, found := a.files[req]; found {
		return req, false, true
	}
	return "", false, false
}

func (a *assetSet) serve(w http.ResponseWriter, r *http.Request, req string) {
	name, immutable, ok := a.lookup(req)
	if !ok {
		http.NotFound(w, r)
		return
	}
	p, _, err := a.print(name, nil)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body, mod, done, err := a.body(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer done()
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("ETag", `"`+p.sum+`"`)
	if immutable {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, path.Base(name), mod, body)
}
