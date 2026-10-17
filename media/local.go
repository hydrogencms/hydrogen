package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Local keeps files in a directory, sharded by key (ab/cd/<key>) so no directory grows large. Writes go to a
// temporary file in the same file system and are renamed into place, so a crash or a concurrent reader never
// sees a partial file.
type Local struct {
	dir string
	tmp string
}

// NewLocal returns a local driver rooted at dir, which it creates. Temporary files left by crashed writes
// (older than a day) are removed.
func NewLocal(dir string) (*Local, error) {
	if dir == "" {
		return nil, errors.New("media: local storage needs a directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	l := &Local{dir: abs, tmp: filepath.Join(abs, ".tmp")}
	if err := os.MkdirAll(l.tmp, 0o750); err != nil {
		return nil, fmt.Errorf("media: create %s: %w", l.dir, err)
	}
	if entries, err := os.ReadDir(l.tmp); err == nil {
		for _, e := range entries {
			if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(filepath.Join(l.tmp, e.Name()))
			}
		}
	}
	return l, nil
}

func (l *Local) path(key string) (string, error) {
	if !validKey(key) {
		return "", errBadKey(key)
	}
	return filepath.Join(l.dir, key[:2], key[2:4], key), nil
}

func (l *Local) Put(ctx context.Context, key string, r io.Reader, size int64, _ string) error {
	dst, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(l.tmp, "put-*")
	if err != nil {
		return err
	}
	name := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(name)
		}
	}()
	n, err := io.Copy(f, &ctxReader{ctx, r})
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("media: put %s: got %d bytes, want %d", quote(key), n, size)
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o640); err != nil {
		return err
	}
	if err := os.Rename(name, dst); err != nil {
		return err
	}
	ok = true
	return nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func (l *Local) open(key string) (*os.File, Info, error) {
	p, err := l.path(key)
	if err != nil {
		return nil, Info{}, err
	}
	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Info{}, ErrNotFound
		}
		return nil, Info{}, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, Info{}, err
	}
	return f, Info{Size: st.Size(), ModTime: st.ModTime().UTC()}, nil
}

func (l *Local) Get(_ context.Context, key string) (io.ReadCloser, Info, error) {
	f, info, err := l.open(key)
	if err != nil {
		return nil, Info{}, err
	}
	return f, info, nil
}

type sectionCloser struct {
	*io.SectionReader
	f *os.File
}

func (s sectionCloser) Close() error { return s.f.Close() }

func (l *Local) GetRange(_ context.Context, key string, offset, length int64) (io.ReadCloser, Info, error) {
	f, info, err := l.open(key)
	if err != nil {
		return nil, Info{}, err
	}
	offset = min(max(offset, 0), info.Size)
	length = min(max(length, 0), info.Size-offset)
	return sectionCloser{io.NewSectionReader(f, offset, length), f}, info, nil
}

func (l *Local) Stat(_ context.Context, key string) (Info, error) {
	f, info, err := l.open(key)
	if err != nil {
		return Info{}, err
	}
	_ = f.Close()
	return info, nil
}

func (l *Local) Delete(_ context.Context, key string) error {
	p, err := l.path(key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (l *Local) List(ctx context.Context, prefix, after string, limit int) ([]Entry, error) {
	var out []Entry
	if limit <= 0 {
		return nil, nil
	}
	err := filepath.WalkDir(l.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		if d.IsDir() {
			if p == l.tmp {
				return filepath.SkipDir
			}
			if rel, err := filepath.Rel(l.dir, p); err == nil && len(after) >= 4 && rel != "." {
				if shard := filepath.ToSlash(rel); len(shard) <= 5 && shard < (after[:2] + "/" + after[2:4])[:len(shard)] {
					return filepath.SkipDir
				}
			}
			return nil
		}
		key := d.Name()
		if !validKey(key) || !strings.HasPrefix(key, prefix) || key <= after {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		out = append(out, Entry{Key: key, Size: info.Size(), ModTime: info.ModTime().UTC()})
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	return out, err
}
