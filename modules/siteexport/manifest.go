package siteexport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ManifestName is the manifest's file name at the root of the tree.
const ManifestName = "manifest.json"

// File kinds besides the site.Target kinds.
const (
	kindAsset     = "asset"     // a fingerprinted theme or project asset
	kindRedirect  = "redirect"  // an HTML refresh page
	kindRedirects = "redirects" // a redirect file in a server's format
)

// Manifest lists every file of the tree with its content hash and the cache tags it depends on.
type Manifest struct {
	Version   int              `json:"version"`
	Generated string           `json:"generated"`
	Files     map[string]Entry `json:"files"`
}

// Entry describes one file of the tree.
type Entry struct {
	Hash string `json:"hash"` // hex SHA-256 of the content
	Size int    `json:"size"`
	Kind string `json:"kind"`
	// Channel and URL identify the target a page file renders; Root is the host directory of assets and redirect
	// files.
	Channel string   `json:"channel,omitempty"`
	URL     string   `json:"url,omitempty"`
	Root    string   `json:"root,omitempty"`
	Tags    []string `json:"tags,omitempty"`
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func newManifest() *Manifest { return &Manifest{Version: 1, Files: map[string]Entry{}} }

// LoadManifest reads <dir>/manifest.json; a missing file gives nil.
func LoadManifest(dir string) (*Manifest, error) {
	return loadManifestFile(filepath.Join(dir, ManifestName))
}

func loadManifestFile(file string) (*Manifest, error) {
	b, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m := newManifest()
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("site-export: %s: %w", file, err)
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("site-export: %s has version %d, want 1", file, m.Version)
	}
	if m.Files == nil {
		m.Files = map[string]Entry{}
	}
	return m, nil
}

func (m *Manifest) save(dir string) error { return m.saveFile(filepath.Join(dir, ManifestName)) }

func (m *Manifest) saveFile(file string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(file, append(b, '\n'))
}

// writeAtomic writes the file next to its final name and renames it, so a reader never sees a partial file.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := os.Chmod(f.Name(), 0o644); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		_ = os.Remove(f.Name())
		return err
	}
	return nil
}

// safeRel reports whether name is a clean relative slash path that stays inside its root.
func safeRel(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// digest identifies the content of the tree: the files and their hashes.
func (m *Manifest) digest() string {
	h := sha256.New()
	for _, f := range slices.Sorted(maps.Keys(m.Files)) {
		h.Write([]byte(f + "\x00" + m.Files[f].Hash + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// publishedState records what the publisher last delivered successfully. It lives beside the work tree
// (<out>.published.json), not in it, so publishers never deliver it.
type publishedState struct {
	Publisher string `json:"publisher"`
	Digest    string `json:"digest"`
}

func publishedPath(out string) string {
	return filepath.Join(filepath.Dir(out), "."+filepath.Base(out)+".published.json")
}

func loadPublished(out string) publishedState {
	var s publishedState
	if b, err := os.ReadFile(publishedPath(out)); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s
}

func savePublished(out string, s publishedState) error {
	b, _ := json.Marshal(s)
	return writeAtomic(publishedPath(out), b)
}
