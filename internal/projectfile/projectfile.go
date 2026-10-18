// Package projectfile reads and writes a project's hydrogen.yaml (its module list) and generates the
// modules_gen.go that registers those modules.
package projectfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/hydrogencms/hydrogen/internal/modcat"
)

const (
	FileName = "hydrogen.yaml"
	GenName  = "modules_gen.go"
)

const header = `# Modules compiled into this project. Edit with ` + "`hydrogen add` / `hydrogen remove`" + `, then ` + "`hydrogen generate`" + `.
`

var pathRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+(/[A-Za-z0-9._~-]+)+$`)

// Validate checks one hydrogen.yaml entry: a catalog name or a Go package path.
func Validate(entry string) error {
	if _, ok := modcat.Lookup(entry); ok {
		return nil
	}
	if strings.Contains(entry, "@") {
		return fmt.Errorf("%q: versions belong in go.mod; hydrogen add path@version records one there", entry)
	}
	if m, ok := modcat.ByPackage(entry); ok {
		return fmt.Errorf("%q is a catalog module; write %q", entry, m.Name)
	}
	if !pathRe.MatchString(entry) {
		return fmt.Errorf("unknown module %q: use a catalog name (hydrogen modules) or a Go package path such as github.com/acme/hydrogen-stripe", entry)
	}
	for _, el := range strings.Split(entry, "/") {
		if el == "." || el == ".." || strings.HasSuffix(el, ".") {
			return fmt.Errorf("invalid module path %q", entry)
		}
	}
	return nil
}

// SplitVersion splits "path@version" as given to hydrogen add.
func SplitVersion(arg string) (name, version string) {
	name, version, _ = strings.Cut(arg, "@")
	return name, version
}

// ImportPath is the Go package an entry registers.
func ImportPath(entry string) string {
	if m, ok := modcat.Lookup(entry); ok {
		return m.Package
	}
	return entry
}

type file struct {
	Modules []string `yaml:"modules"`
}

// Parse reads hydrogen.yaml. Unknown keys, invalid entries and duplicates fail; order is kept.
func Parse(b []byte) ([]string, error) {
	var f file
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	seen := map[string]bool{}
	for _, e := range f.Modules {
		if err := Validate(e); err != nil {
			return nil, fmt.Errorf("%s: modules: %w", FileName, err)
		}
		if seen[e] {
			return nil, fmt.Errorf("%s: modules: %q is listed twice", FileName, e)
		}
		seen[e] = true
	}
	return f.Modules, nil
}

// Marshal writes the file with its fixed header.
func Marshal(modules []string) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	if len(modules) == 0 {
		b.WriteString("modules: []\n")
		return b.Bytes()
	}
	b.WriteString("modules:\n")
	for _, m := range modules {
		fmt.Fprintf(&b, "  - %s\n", m)
	}
	return b.Bytes()
}

func Exists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil
}

func Load(dir string) ([]string, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// WriteFileAtomic writes through a temporary file in the same directory and renames it into place.
func WriteFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o644)
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// Sync regenerates modules_gen.go from hydrogen.yaml when the file exists, writing only on change. It
// reports whether modules_gen.go was written.
func Sync(dir string) (bool, error) {
	if !Exists(dir) {
		return false, nil
	}
	mods, err := Load(dir)
	if err != nil {
		return false, err
	}
	want, err := Generate(mods)
	if err != nil {
		return false, err
	}
	path := filepath.Join(dir, GenName)
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, want) {
		return false, nil
	}
	return true, WriteFileAtomic(path, want)
}
