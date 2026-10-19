package site

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// layer is a source of files: the theme chain (base first) under the project's directory. A later layer's
// file replaces an earlier one's of the same name.
type layer struct {
	fsys fs.FS
	root string // the file system's directory for messages
}

// layers returns the themes' and the project's file systems for a sub directory ("templates", "assets",
// "translations"); a layer is absent when its directory does not exist.
func layers(chain []Theme, dir, sub string) []layer {
	var out []layer
	for _, t := range chain {
		if st, err := fs.Stat(t.FS, sub); err != nil || !st.IsDir() {
			continue
		}
		if sf, err := fs.Sub(t.FS, sub); err == nil {
			out = append(out, layer{sf, "theme " + t.Name + "/" + sub})
		}
	}
	p := filepath.Join(dir, sub)
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		out = append(out, layer{os.DirFS(p), p})
	}
	return out
}

type layered struct {
	name  string
	fsys  fs.FS
	where string
}

// files lists the files with an extension in the layers, later layers replacing earlier ones, sorted by name.
func files(ls []layer, ext ...string) ([]layered, error) {
	byName := map[string]layered{}
	for _, l := range ls {
		err := fs.WalkDir(l.fsys, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if len(ext) > 0 && !slices.Contains(ext, path.Ext(p)) {
				return nil
			}
			byName[p] = layered{p, l.fsys, l.root + "/" + p}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", l.root, err)
		}
	}
	out := make([]layered, 0, len(byName))
	for _, f := range byName {
		out = append(out, f)
	}
	slices.SortFunc(out, func(a, b layered) int { return strings.Compare(a.name, b.name) })
	return out, nil
}

// assetSet serves the files of site/assets under fingerprinted names.
type assetSet struct {
	byURL  map[string]layered // fingerprinted name → file
	byName map[string]string  // name → fingerprinted name
}

func loadAssets(chain []Theme, dir string) (*assetSet, error) {
	list, err := files(layers(chain, dir, "assets"))
	if err != nil {
		return nil, err
	}
	a := &assetSet{byURL: map[string]layered{}, byName: map[string]string{}}
	for _, f := range list {
		b, err := fs.ReadFile(f.fsys, f.name)
		if err != nil {
			return nil, fmt.Errorf("read asset %s: %w", f.where, err)
		}
		sum := sha256.Sum256(b)
		ext := path.Ext(f.name)
		fp := strings.TrimSuffix(f.name, ext) + "." + hex.EncodeToString(sum[:5]) + ext
		a.byURL[fp] = f
		a.byName[f.name] = fp
	}
	return a, nil
}

// url is the fingerprinted URL of an asset; false when there is no such file.
func (a *assetSet) url(name string) (string, bool) {
	fp, ok := a.byName[strings.TrimPrefix(name, "/")]
	return assetsURL + fp, ok
}

func (a *assetSet) serve(w http.ResponseWriter, r *http.Request, name string) {
	f, immutable := a.byURL[name]
	if !immutable {
		fp, ok := a.byName[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f = a.byURL[fp]
	}
	b, err := fs.ReadFile(f.fsys, f.name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Access-Control-Allow-Origin", "*")
	if immutable {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, path.Base(f.name), time.Time{}, bytes.NewReader(b))
}

// translations are the flat key → text maps of site/translations/<locale>.yaml over the themes', per key.
type translations map[string]map[string]string

func loadTranslations(chain []Theme, dir string) (translations, error) {
	out := translations{}
	// Layers merge key by key (a project's en.yaml adds to the theme's rather than replacing it).
	for _, l := range layers(chain, dir, "translations") {
		list, err := files([]layer{l}, ".yaml", ".yml")
		if err != nil {
			return nil, err
		}
		for _, f := range list {
			b, err := fs.ReadFile(f.fsys, f.name)
			if err != nil {
				return nil, err
			}
			var m map[string]string
			if err := yaml.Unmarshal(b, &m); err != nil {
				return nil, fmt.Errorf("translations %s: %w", f.where, err)
			}
			loc := strings.TrimSuffix(path.Base(f.name), path.Ext(f.name))
			if out[loc] == nil {
				out[loc] = map[string]string{}
			}
			for k, v := range m {
				out[loc][k] = v
			}
		}
	}
	return out, nil
}

// text looks a key up in the locale, then its language (de-CH → de), then the default locale; a missing key
// is returned as is. pairs replace {name} placeholders.
func (t translations) text(locale, def, key string, pairs ...any) string {
	s, ok := "", false
	base, _, _ := strings.Cut(locale, "-")
	for _, l := range []string{locale, base, def} {
		if s, ok = t[l][key]; ok {
			break
		}
	}
	if !ok {
		s = key
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		s = strings.ReplaceAll(s, "{"+fmt.Sprint(pairs[i])+"}", fmt.Sprint(pairs[i+1]))
	}
	return s
}

// dirSignature changes when a file under the project's directory is added, removed or modified.
func dirSignature(dir string) string {
	var sb strings.Builder
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			fmt.Fprintf(&sb, "%s:%d:%d;", p, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return sb.String()
}
