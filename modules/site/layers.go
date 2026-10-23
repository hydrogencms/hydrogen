package site

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// layer is a source of files: the theme chain (base first) under the project's directory. A later layer's
// file replaces an earlier one's of the same name.
type layer struct {
	fsys fs.FS
	root string // the file system's directory for messages
	os   bool   // root is a directory on disk, whose symbolic links to directories are followed
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
			out = append(out, layer{sf, "theme " + t.Name + "/" + sub, false})
		}
	}
	p := filepath.Join(dir, sub)
	if st, err := os.Stat(p); err == nil && st.IsDir() {
		out = append(out, layer{os.DirFS(p), p, true})
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
		add := func(p string) {
			if len(ext) == 0 || slices.Contains(ext, path.Ext(p)) {
				byName[p] = layered{p, l.fsys, l.root + "/" + p}
			}
		}
		var err error
		if l.os {
			err = walkFollow(l.root, func(p string, _ fs.FileInfo) { add(filepath.ToSlash(p)) })
		} else {
			err = fs.WalkDir(l.fsys, ".", func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() {
					add(p)
				}
				return err
			})
		}
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

// walkFollow visits the files under root (paths relative to it, in lexical order), following symbolic links to
// directories; a link back to a directory being walked is skipped. A broken link is skipped.
func walkFollow(root string, visit func(rel string, info fs.FileInfo)) error {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	return walkDir(root, "", []string{real}, visit)
}

func walkDir(root, rel string, ancestors []string, visit func(string, fs.FileInfo)) error {
	entries, err := os.ReadDir(filepath.Join(root, rel))
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(rel, e.Name())
		info, err := os.Stat(filepath.Join(root, p))
		if err != nil {
			continue
		}
		if !info.IsDir() {
			visit(p, info)
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(root, p))
		if err != nil || slices.Contains(ancestors, real) {
			continue
		}
		if err := walkDir(root, p, append(ancestors, real), visit); err != nil {
			return err
		}
	}
	return nil
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
func dirSignature(dir string) uint64 {
	h := fnv.New64a()
	var buf [16]byte
	_ = walkFollow(dir, func(p string, info fs.FileInfo) {
		_, _ = h.Write([]byte(p))
		binary.LittleEndian.PutUint64(buf[:8], uint64(info.Size()))
		binary.LittleEndian.PutUint64(buf[8:], uint64(info.ModTime().UnixNano()))
		_, _ = h.Write(buf[:])
	})
	return h.Sum64()
}
