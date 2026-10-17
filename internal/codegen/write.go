package codegen

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hydrogencms/hydrogen/core/schema"
)

// WriteGo generates the Go package into dir (created if missing). It writes only files whose content
// changed, so file watchers do not loop, removes stale files that carry the generated header but are no
// longer produced, never touches files without the header, and returns how many files it wrote.
func WriteGo(dir string, types []*schema.Type, groups []*schema.Group, o GoOptions) (int, error) {
	files, err := Go(types, groups, o)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("codegen: %w", err)
	}
	written := 0
	for name, src := range files {
		changed, err := writeIfChanged(filepath.Join(dir, name), src)
		if err != nil {
			return written, err
		}
		if changed {
			written++
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return written, fmt.Errorf("codegen: %w", err)
	}
	for _, e := range entries {
		name := e.Name()
		if _, ok := files[name]; ok || e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		path := filepath.Join(dir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			return written, fmt.Errorf("codegen: %w", err)
		}
		if bytes.HasPrefix(src, []byte("// "+header)) {
			if err := os.Remove(path); err != nil {
				return written, fmt.Errorf("codegen: %w", err)
			}
		}
	}
	return written, nil
}

// WriteTypeScript generates the TypeScript file at path (parent directories created) and writes it only if
// its content changed.
func WriteTypeScript(path string, types []*schema.Type, groups []*schema.Group, o TSOptions) error {
	src, err := TypeScript(types, groups, o)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("codegen: %w", err)
	}
	_, err = writeIfChanged(path, src)
	return err
}

// writeIfChanged atomically replaces path with src unless it already holds exactly src.
func writeIfChanged(path string, src []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, src) {
		return false, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("codegen: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codegen-*.tmp")
	if err != nil {
		return false, fmt.Errorf("codegen: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(src); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("codegen: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("codegen: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("codegen: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return false, fmt.Errorf("codegen: %w", err)
	}
	return true, nil
}
