package exportfmt

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// FormatVersion is the format this package writes. Format 2 added uploaded files (files/<sha> and
// uploads.ndjson); format 1 exports, which have none, are still read.
const FormatVersion = 2

const manifestName = "manifest.json"

// Manifest describes an export. Files covers every file except the manifest itself.
type Manifest struct {
	Format        int       `json:"format"`
	Hydrogen      string    `json:"hydrogen"`
	Dialect       string    `json:"dialect"`
	SchemaHash    string    `json:"schemaHash"`
	CreatedAt     time.Time `json:"createdAt"`
	PublishedOnly bool      `json:"publishedOnly,omitempty"`
	Counts        Counts    `json:"counts"`
	Files         []File    `json:"files"`
}

// Counts are the numbers of records exported. Modules holds the record count of each module file.
type Counts struct {
	Items             int            `json:"items"`
	Versions          int            `json:"versions"`
	Drafts            int            `json:"drafts"`
	SchemaRevisions   int            `json:"schemaRevisions"`
	ContentMigrations int            `json:"contentMigrations"`
	Modules           map[string]int `json:"modules,omitempty"`
	Uploads           int            `json:"uploads,omitempty"`
}

// File is one exported file. Records counts the lines of an .ndjson file.
type File struct {
	Name    string `json:"name"`
	SHA256  string `json:"sha256"`
	Bytes   int64  `json:"bytes"`
	Records int    `json:"records,omitempty"`
}

var fileName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)

func checkName(name string) error {
	if !fileName.MatchString(name) || strings.Contains(name, "..") || name == manifestName || path.Clean(name) != name {
		return fmt.Errorf("exportfmt: invalid file name %q", name)
	}
	return nil
}

// Writer creates an export directory. Files are written one at a time; Finish writes the manifest.
type Writer struct {
	dir   string
	files []File
	open  *FileWriter
}

// NewWriter creates dir (and parents) for a new export; it must not exist or be empty.
func NewWriter(dir string) (*Writer, error) {
	entries, err := os.ReadDir(dir)
	switch {
	case err == nil && len(entries) > 0:
		return nil, fmt.Errorf("exportfmt: %s is not empty", dir)
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	return &Writer{dir: dir}, nil
}

// Create starts a file; the previous one must be closed.
func (w *Writer) Create(name string) (*FileWriter, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	if w.open != nil {
		return nil, fmt.Errorf("exportfmt: %s is still open", w.open.name)
	}
	if slices.ContainsFunc(w.files, func(f File) bool { return f.Name == name }) {
		return nil, fmt.Errorf("exportfmt: %s written twice", name)
	}
	full := filepath.Join(w.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	f, err := os.OpenFile(full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	fw := &FileWriter{w: w, name: name, f: f, sum: sha256.New()}
	fw.bw = bufio.NewWriterSize(io.MultiWriter(f, fw.sum), 256<<10)
	w.open = fw
	return fw, nil
}

// WriteFile writes a whole small file.
func (w *Writer) WriteFile(name string, data []byte) error {
	f, err := w.Create(name)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Finish writes the manifest, filling in the format version and the file list, and returns it.
func (w *Writer) Finish(m Manifest) (Manifest, error) {
	if w.open != nil {
		return m, fmt.Errorf("exportfmt: %s is still open", w.open.name)
	}
	m.Format = FormatVersion
	m.Files = slices.Clone(w.files)
	slices.SortFunc(m.Files, func(a, b File) int { return strings.Compare(a.Name, b.Name) })
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, fmt.Errorf("exportfmt: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(w.dir, manifestName), data, 0o600); err != nil {
		return m, fmt.Errorf("exportfmt: %w", err)
	}
	return m, nil
}

// Files lists the files written so far.
func (w *Writer) Files() []File { return slices.Clone(w.files) }

// FileWriter writes one file, hashing it as it goes.
type FileWriter struct {
	w       *Writer
	name    string
	f       *os.File
	bw      *bufio.Writer
	sum     hash.Hash
	bytes   int64
	records int
	buf     bytes.Buffer
}

func (f *FileWriter) Write(p []byte) (int, error) {
	n, err := f.bw.Write(p)
	f.bytes += int64(n)
	return n, err
}

// WriteRecord appends v as one JSON line.
func (f *FileWriter) WriteRecord(v any) error {
	f.buf.Reset()
	enc := json.NewEncoder(&f.buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("exportfmt: %s: %w", f.name, err)
	}
	// Encode ends the line; a record never contains a raw newline because JSON escapes them.
	if _, err := f.Write(f.buf.Bytes()); err != nil {
		return err
	}
	f.records++
	return nil
}

// Close flushes the file and records its checksum.
func (f *FileWriter) Close() error {
	err := errors.Join(f.bw.Flush(), f.f.Sync(), f.f.Close())
	if f.w.open == f {
		f.w.open = nil
	}
	if err != nil {
		return fmt.Errorf("exportfmt: %s: %w", f.name, err)
	}
	f.w.files = append(f.w.files, File{Name: f.name, SHA256: hex.EncodeToString(f.sum.Sum(nil)), Bytes: f.bytes, Records: f.records})
	return nil
}

// Reader reads an export directory.
type Reader struct {
	dir string
	m   Manifest
}

// Open reads the manifest of the export in dir.
func Open(dir string) (*Reader, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("exportfmt: %s: %w", manifestName, err)
	}
	if m.Format < 1 || m.Format > FormatVersion {
		return nil, fmt.Errorf("exportfmt: export format %d is not supported (this build reads formats 1 to %d)", m.Format, FormatVersion)
	}
	for i, f := range m.Files {
		if err := checkName(f.Name); err != nil {
			return nil, err
		}
		if slices.ContainsFunc(m.Files[:i], func(g File) bool { return g.Name == f.Name }) {
			return nil, fmt.Errorf("exportfmt: %s lists %s twice", manifestName, f.Name)
		}
	}
	return &Reader{dir: dir, m: m}, nil
}

func (r *Reader) Manifest() Manifest { return r.m }

// Has reports whether the manifest lists the file.
func (r *Reader) Has(name string) bool { _, ok := r.file(name); return ok }

func (r *Reader) file(name string) (File, bool) {
	for _, f := range r.m.Files {
		if f.Name == name {
			return f, true
		}
	}
	return File{}, false
}

// Names lists the files whose names start with prefix, sorted.
func (r *Reader) Names(prefix string) []string {
	var out []string
	for _, f := range r.m.Files {
		if strings.HasPrefix(f.Name, prefix) {
			out = append(out, f.Name)
		}
	}
	return out
}

// Verify streams every listed file and compares size, record count and sha-256 with the manifest. It reports
// every problem found.
func (r *Reader) Verify() error {
	var errs []error
	for _, f := range r.m.Files {
		if err := r.verify(f); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("exportfmt: export is damaged: %w", errors.Join(errs...))
	}
	return nil
}

type countWriter struct {
	n     int64
	lines int
}

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	c.lines += bytes.Count(p, []byte{'\n'})
	return len(p), nil
}

func (r *Reader) verify(f File) error {
	file, err := os.Open(filepath.Join(r.dir, filepath.FromSlash(f.Name)))
	if err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	defer func() { _ = file.Close() }()
	h, cw := sha256.New(), &countWriter{}
	if _, err := io.Copy(io.MultiWriter(h, cw), file); err != nil {
		return fmt.Errorf("%s: %w", f.Name, err)
	}
	switch {
	case cw.n != f.Bytes:
		return fmt.Errorf("%s: %d bytes, manifest says %d", f.Name, cw.n, f.Bytes)
	case hex.EncodeToString(h.Sum(nil)) != f.SHA256:
		return fmt.Errorf("%s: checksum mismatch", f.Name)
	case strings.HasSuffix(f.Name, ".ndjson") && cw.lines != f.Records:
		return fmt.Errorf("%s: %d records, manifest says %d", f.Name, cw.lines, f.Records)
	}
	return nil
}

// ReadFile reads a whole small file (listed in the manifest).
func (r *Reader) ReadFile(name string) ([]byte, error) {
	if _, ok := r.file(name); !ok {
		return nil, fmt.Errorf("exportfmt: %s is not part of the export", name)
	}
	data, err := os.ReadFile(filepath.Join(r.dir, filepath.FromSlash(name)))
	if err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	return data, nil
}

// OpenFile opens a listed file for streaming; the caller closes it.
func (r *Reader) OpenFile(name string) (*os.File, error) {
	if _, ok := r.file(name); !ok {
		return nil, fmt.Errorf("exportfmt: %s is not part of the export", name)
	}
	f, err := os.Open(filepath.Join(r.dir, filepath.FromSlash(name)))
	if err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	return f, nil
}

// Records opens an .ndjson file for streaming.
func (r *Reader) Records(name string) (*RecordReader, error) {
	f, ok := r.file(name)
	if !ok {
		return nil, fmt.Errorf("exportfmt: %s is not part of the export", name)
	}
	file, err := os.Open(filepath.Join(r.dir, filepath.FromSlash(name)))
	if err != nil {
		return nil, fmt.Errorf("exportfmt: %w", err)
	}
	return &RecordReader{name: name, f: file, br: bufio.NewReaderSize(file, 256<<10), total: f.Records}, nil
}

// RecordReader streams the lines of an .ndjson file.
type RecordReader struct {
	name  string
	f     *os.File
	br    *bufio.Reader
	line  int
	total int
}

// Total is the number of records the manifest lists for the file.
func (rr *RecordReader) Total() int { return rr.total }

// Next decodes the next record into v; ok is false at the end.
func (rr *RecordReader) Next(v any) (ok bool, err error) {
	line, err := rr.br.ReadBytes('\n')
	if err == io.EOF && len(line) == 0 {
		return false, nil
	}
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("exportfmt: %s: %w", rr.name, err)
	}
	rr.line++
	if err := json.Unmarshal(line, v); err != nil {
		return false, fmt.Errorf("exportfmt: %s line %d: %w", rr.name, rr.line, err)
	}
	return true, nil
}

func (rr *RecordReader) Close() error { return rr.f.Close() }

// ModuleWriter is what a module exporter writes its records to (modules/<name>.ndjson).
type ModuleWriter struct{ f *FileWriter }

// Write appends one record.
func (w *ModuleWriter) Write(v any) error { return w.f.WriteRecord(v) }

// ModuleReader is what a module importer reads its records from.
type ModuleReader struct{ rr *RecordReader }

// Next decodes the next record into v; ok is false at the end.
func (r *ModuleReader) Next(v any) (ok bool, err error) { return r.rr.Next(v) }

// Total is the number of records in the file.
func (r *ModuleReader) Total() int { return r.rr.Total() }
