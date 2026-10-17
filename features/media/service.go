package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/outbox"
	"github.com/hydrogencms/hydrogen/core/store"
)

// DefaultMaxUpload is the largest accepted upload when media.max_upload is not set.
const DefaultMaxUpload = 50 << 20

// DefaultGCAge is how long an upload must be untouched before garbage collection may remove it.
const DefaultGCAge = 24 * time.Hour

// DefaultMaxConcurrentUploads is how many uploads one server accepts at once when Options leaves it unset.
const DefaultMaxConcurrentUploads = 8

// DefaultUploadTimeout bounds how long one upload request may take to send its body.
const DefaultUploadTimeout = 10 * time.Minute

// MinGCAge is the shortest age garbage collection may be configured with. Files that only live in unsaved
// collaborative state or in a relayed draft are not seen by the collection; their windows (see the
// collaboration and share-link settings) are far shorter than this.
const MinGCAge = 24 * time.Hour

// Errors of Upload. Each wraps one of them with detail for the caller.
var (
	ErrTooLarge        = errors.New("media: file is too large")
	ErrUnsupportedType = errors.New("media: file type is not accepted")
	ErrEmptyFile       = errors.New("media: file is empty")
	ErrUnknownType     = errors.New("media: not an upload type")
	ErrBusy            = errors.New("media: too many uploads in progress")
)

// UploadError carries a user-facing message and wraps the sentinel error that classifies it.
type UploadError struct {
	Kind error
	Msg  string
}

func (e *UploadError) Error() string { return e.Msg }
func (e *UploadError) Unwrap() error { return e.Kind }

func uploadErr(kind error, format string, args ...any) error {
	return &UploadError{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// UploadType is a content type that holds uploaded files (`upload:` in the schema).
type UploadType struct {
	Name    string
	Accept  []string // MIME patterns ("image/*"); none accepts every type
	MaxSize int64    // bytes; 0: only media.max_upload applies
}

// Gate is the access check the Service makes; *access.Checker satisfies it.
type Gate interface {
	Screen(ctx context.Context, policy string, t access.Target) error
}

// Options configures New.
type Options struct {
	DB      store.DB
	Storage Storage
	Clock   clock.Clock
	Log     *slog.Logger
	Audit   *audit.Log
	Outbox  *outbox.Outbox
	// Access is required: uploading needs content.create or content.edit on an upload type.
	Access Gate
	// Types lists the upload types of the schema with their accept patterns and size limits.
	Types func() []UploadType
	// FileTypes lists the content types whose documents can hold file values (upload types and any other type
	// with a file field): uploads are authorized against them. Garbage collection reads every item in the
	// database regardless. Default: the upload types.
	FileTypes func() []string
	// MaxUpload bounds every upload (default 50 MiB).
	MaxUpload int64
	// PublicURL, when set, replaces "/assets" as the base of file URLs: a CDN that serves {sha}/{name} (usually
	// by fetching GET /assets/{sha}/{name} from this server).
	PublicURL string
	// GCAge is the minimum untouched time before an unreferenced upload is collected (default 24h, at least
	// MinGCAge unless AllowShortGC is set).
	GCAge time.Duration
	// AllowShortGC lets tests use a GCAge below MinGCAge.
	AllowShortGC bool
	// MaxConcurrentUploads bounds the upload requests handled at once (default 8); more get 429.
	MaxConcurrentUploads int
	// UploadTimeout bounds the time to receive one upload request's body (default 10 minutes).
	UploadTimeout time.Duration
	// TempDir holds uploads while they are checked (default os.TempDir()).
	TempDir string
}

// Service stores uploads, serves them and collects the unreferenced ones.
type Service struct {
	o     Options
	slots chan struct{}
	// locks serialise storing a file with removing its bytes (garbage collection) within this process.
	locks [64]sync.Mutex
}

// New validates the options.
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("media: DB is required")
	case o.Storage == nil:
		return nil, errors.New("media: Storage is required")
	case o.Access == nil:
		return nil, errors.New("media: Access is required")
	case o.Types == nil:
		return nil, errors.New("media: Types is required")
	case o.Audit == nil || o.Outbox == nil:
		return nil, errors.New("media: Audit and Outbox are required")
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.MaxUpload <= 0 {
		o.MaxUpload = DefaultMaxUpload
	}
	if o.GCAge <= 0 {
		o.GCAge = DefaultGCAge
	}
	if o.GCAge < MinGCAge && !o.AllowShortGC {
		return nil, fmt.Errorf("media: GCAge %s is below the minimum of %s", o.GCAge, MinGCAge)
	}
	if o.MaxConcurrentUploads <= 0 {
		o.MaxConcurrentUploads = DefaultMaxConcurrentUploads
	}
	if o.UploadTimeout <= 0 {
		o.UploadTimeout = DefaultUploadTimeout
	}
	if o.TempDir == "" {
		o.TempDir = os.TempDir()
	}
	o.PublicURL = strings.TrimRight(o.PublicURL, "/")
	return &Service{o: o, slots: make(chan struct{}, o.MaxConcurrentUploads)}, nil
}

// lock serialises work on one file within this process; the returned function unlocks.
func (s *Service) lock(sha string) func() {
	var n int
	for i := 0; i < 2 && i < len(sha); i++ {
		c := sha[i]
		switch {
		case c >= 'a':
			n = n*16 + int(c-'a') + 10
		default:
			n = n*16 + int(c-'0')
		}
	}
	m := &s.locks[n%len(s.locks)]
	m.Lock()
	return m.Unlock
}

func (s *Service) fileTypes() []string {
	if s.o.FileTypes != nil {
		return s.o.FileTypes()
	}
	var out []string
	for _, t := range s.o.Types() {
		out = append(out, t.Name)
	}
	return out
}

// File is a stored file as the server knows it.
type File struct {
	SHA    string
	MIME   string
	Size   int64
	Width  int
	Height int
}

func fileOf(u store.Upload) File {
	return File{SHA: u.SHA, MIME: u.MIME, Size: u.Size, Width: u.Width, Height: u.Height}
}

// IsSha reports whether s is a lowercase hex SHA-256.
func IsSha(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// Lookup returns the record of a file; ErrNotFound when there is none.
func (s *Service) Lookup(ctx context.Context, sha string) (File, error) {
	u, err := s.record(ctx, sha)
	return fileOf(u), err
}

func (s *Service) record(ctx context.Context, sha string) (store.Upload, error) {
	if !IsSha(sha) {
		return store.Upload{}, ErrNotFound
	}
	var u store.Upload
	get := func(tx store.Tx) (err error) {
		u, err = tx.Uploads().Get(ctx, sha)
		return err
	}
	var err error
	if tx, ok := store.TxFrom(ctx); ok { // inside a write transaction: read through it
		err = get(tx)
	} else {
		err = s.o.DB.View(ctx, get)
	}
	if errors.Is(err, store.ErrNotFound) {
		return u, ErrNotFound
	}
	return u, err
}

// Open streams a stored file's bytes.
func (s *Service) Open(ctx context.Context, sha string) (io.ReadCloser, File, error) {
	u, err := s.record(ctx, sha)
	if err != nil {
		return nil, File{}, err
	}
	rc, _, err := s.o.Storage.Get(ctx, sha)
	if err != nil {
		return nil, File{}, err
	}
	return rc, fileOf(u), nil
}

// Records lists upload records in SHA order after the given one (export).
func (s *Service) Records(ctx context.Context, after string, limit int) ([]store.Upload, error) {
	var out []store.Upload
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Uploads().List(ctx, after, limit)
		return err
	})
	return out, err
}

// AssetPath is where this server serves files.
const AssetPath = "/assets"

// URL is where a file is served: the public base (PublicURL, else "/assets") + "/<sha>/<name>".
func (s *Service) URL(sha, name string) string {
	base := s.o.PublicURL
	if base == "" {
		base = AssetPath
	}
	return base + "/" + sha + "/" + url.PathEscape(cleanName(name))
}

// PublicBase reports the configured public base URL ("" when files are served by this server).
func (s *Service) PublicBase() string { return s.o.PublicURL }

// MaxUpload is the configured upload limit in bytes.
func (s *Service) MaxUpload() int64 { return s.o.MaxUpload }

// UploadInput is one file to store.
type UploadInput struct {
	// Name is the client's file name; it is cleaned (no directories, no control characters).
	Name string
	Body io.Reader
	// Type names the content type the file is meant for; an upload type's accept patterns and size limit are
	// checked. Empty checks only the global limit, and the principal needs create or edit on some type that
	// holds files.
	Type string
}

// Result is a stored file with the name to use in the item's `file` value.
type Result struct {
	File
	Name string
	// Created is false when the same bytes were already stored (the upload only refreshed the record).
	Created bool
}

// Upload checks and stores a file. The principal in ctx needs content.create or content.edit on the named type
// (which must hold files), or on any such type when none is named.
func (s *Service) Upload(ctx context.Context, in UploadInput) (Result, error) {
	typ, err := s.authorize(ctx, in.Type)
	if err != nil {
		return Result{}, err
	}
	return s.ingest(ctx, in.Name, in.Body, typ, ingestOptions{createdBy: access.PrincipalFrom(ctx).Actor().String()})
}

func (s *Service) authorize(ctx context.Context, typeName string) (*UploadType, error) {
	names := s.fileTypes()
	if len(names) == 0 {
		return nil, uploadErr(ErrUnknownType, "the schema has no type that holds files")
	}
	allowed := func(name string) bool {
		return s.o.Access.Screen(ctx, access.PolicyContentCreate, access.Target{Type: name}) == nil ||
			s.o.Access.Screen(ctx, access.PolicyContentEdit, access.Target{Type: name}) == nil
	}
	if typeName != "" {
		if !slices.Contains(names, typeName) {
			return nil, uploadErr(ErrUnknownType, "%q is not a type that holds files", typeName)
		}
		if !allowed(typeName) {
			return nil, fmt.Errorf("%w: %s", access.ErrForbidden, access.PolicyContentCreate)
		}
		for _, t := range s.o.Types() {
			if t.Name == typeName {
				return &t, nil
			}
		}
		return nil, nil
	}
	for _, n := range names {
		if allowed(n) {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", access.ErrForbidden, access.PolicyContentCreate)
}

type ingestOptions struct {
	createdBy string
	createdAt time.Time // zero: now (import keeps the original)
	// restore, when set, is the record of an export: the content must hash to its SHA, have its size and
	// agree with its type and dimensions.
	restore *store.Upload
}

func (s *Service) ingest(ctx context.Context, name string, body io.Reader, typ *UploadType, o ingestOptions) (Result, error) {
	limit := s.o.MaxUpload
	if typ != nil && typ.MaxSize > 0 {
		limit = min(limit, typ.MaxSize)
	}
	if o.restore != nil {
		if o.restore.Size <= 0 {
			return Result{}, fmt.Errorf("media: restore %s: the record has no size", o.restore.SHA)
		}
		limit = o.restore.Size
	}
	name = cleanName(name)

	tmp, err := os.CreateTemp(s.o.TempDir, "hydrogen-upload-*")
	if err != nil {
		return Result{}, err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(body, limit+1))
	if err != nil {
		return Result{}, err
	}
	if size > limit {
		return Result{}, uploadErr(ErrTooLarge, "file exceeds the limit of %d bytes", limit)
	}
	if size == 0 {
		return Result{}, uploadErr(ErrEmptyFile, "file is empty")
	}
	sha := hex.EncodeToString(h.Sum(nil))

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return Result{}, err
	}
	det, err := Detect(tmp, name)
	if err != nil {
		if errors.Is(err, ErrInvalidImage) {
			if o.restore != nil {
				return Result{}, fmt.Errorf("media: restore %s: the file is not a valid image", sha)
			}
			return Result{}, uploadErr(ErrUnsupportedType, "the file is not a valid image")
		}
		return Result{}, err
	}
	if rec := o.restore; rec != nil {
		if err := checkRestore(rec, sha, size, det); err != nil {
			return Result{}, err
		}
		det.MIME = rec.MIME
	}
	// The name refines the type of a new file only: a known file keeps the type of its first record, and that
	// type is what the accept patterns are checked against.
	if known, err := s.record(ctx, sha); err == nil {
		det = Detected{MIME: known.MIME, Width: known.Width, Height: known.Height}
	} else if !errors.Is(err, ErrNotFound) {
		return Result{}, err
	}
	if typ != nil && !matchAccept(typ.Accept, det.MIME) {
		return Result{}, uploadErr(ErrUnsupportedType, "%s files are not accepted by %s (accepts %s)", det.MIME, typ.Name, strings.Join(typ.Accept, ", "))
	}

	now := s.o.Clock.Now()
	rec := store.Upload{SHA: sha, MIME: det.MIME, Size: size, Width: det.Width, Height: det.Height,
		CreatedBy: o.createdBy, CreatedAt: now, TouchedAt: now}
	if !o.createdAt.IsZero() {
		rec.CreatedAt = o.createdAt
	}

	// Storing and recording a file is serialised with removing its bytes (a collection in this process).
	unlock := s.lock(sha)
	defer unlock()

	// The bytes first, then the record: a record always has its bytes. A known file whose bytes went missing
	// is stored again.
	wrote, err := s.storeBytes(ctx, tmp, sha, size, det.MIME)
	if err != nil {
		return Result{}, err
	}

	var created bool
	var kept store.Upload
	// The record can vanish between the bytes check and the write when a collection removes it meanwhile
	// (another process, or the window of a concurrent insert): record it again once.
	for attempt := 0; ; attempt++ {
		created, kept, err = s.record1(ctx, rec)
		if errors.Is(err, store.ErrNotFound) && attempt == 0 {
			continue
		}
		break
	}
	if err != nil {
		return Result{}, err
	}
	// A collection in another process may have removed the bytes of the stale record before our record
	// appeared: look again, now that the record is committed.
	if !wrote {
		if _, err := s.storeBytes(ctx, tmp, sha, size, det.MIME); err != nil {
			return Result{}, err
		}
	}
	return Result{File: fileOf(kept), Name: name, Created: created}, nil
}

// storeBytes writes the file unless the storage has it; wrote reports whether it did.
func (s *Service) storeBytes(ctx context.Context, tmp *os.File, sha string, size int64, mime string) (wrote bool, err error) {
	if _, err := s.o.Storage.Stat(ctx, sha); err == nil {
		return false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	if err := s.o.Storage.Put(ctx, sha, tmp, size, mime); err != nil {
		return false, err
	}
	return true, nil
}

// record1 inserts or touches the record, with its audit entry and event when it is new.
func (s *Service) record1(ctx context.Context, rec store.Upload) (created bool, kept store.Upload, err error) {
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		var err error
		if created, err = tx.Uploads().Put(ctx, rec); err != nil {
			return err
		}
		if kept, err = tx.Uploads().Get(ctx, rec.SHA); err != nil {
			return err
		}
		if !created {
			return nil
		}
		actor := access.PrincipalFrom(ctx).Actor()
		data := map[string]any{"mime": rec.MIME, "size": rec.Size}
		if err := s.o.Audit.Write(ctx, tx, audit.Entry{Actor: actor, Action: "media.upload", Subject: rec.SHA, Data: data}); err != nil {
			return err
		}
		_, err = s.o.Outbox.Publish(ctx, tx, "media.uploaded", map[string]any{"sha": rec.SHA, "mime": rec.MIME, "size": rec.Size})
		return err
	})
	return created, kept, err
}

// checkRestore refuses bytes that are not the file the export recorded.
func checkRestore(rec *store.Upload, sha string, size int64, det Detected) error {
	switch {
	case !IsSha(rec.SHA):
		return fmt.Errorf("media: restore: %q is not a SHA-256", rec.SHA)
	case sha != rec.SHA:
		return fmt.Errorf("media: restore: content hash %s does not match %s", sha, rec.SHA)
	case size != rec.Size:
		return fmt.Errorf("media: restore %s: %d bytes, the record says %d", sha, size, rec.Size)
	case !restoreMIMEOK(det.MIME, rec.MIME):
		return fmt.Errorf("media: restore %s: the content is %s, the record says %s", sha, det.MIME, rec.MIME)
	case det.Width != rec.Width || det.Height != rec.Height:
		return fmt.Errorf("media: restore %s: the image is %dx%d, the record says %dx%d", sha, det.Width, det.Height, rec.Width, rec.Height)
	}
	return nil
}

// restoreMIMEOK accepts the recorded type when it is what the content sniffs as, or when the content has no
// signature and the record names a type the file name could have chosen (a name never yields a document or
// image type).
func restoreMIMEOK(detected, recorded string) bool {
	if detected == recorded {
		return true
	}
	switch detected {
	case "text/plain", "application/octet-stream", "application/zip":
		return nameMayChoose(recorded)
	}
	return false
}

// Restore stores a file from an export under its recorded creation data. The bytes must hash to rec.SHA
// (lowercase hex), have the recorded size and agree with the recorded type and dimensions when sniffed again;
// anything else is refused and nothing is stored. It is meant for import and runs unchecked, like the system.
func (s *Service) Restore(ctx context.Context, rec store.Upload, body io.Reader) error {
	if !IsSha(rec.SHA) {
		return fmt.Errorf("media: %q is not a SHA-256", rec.SHA)
	}
	_, err := s.ingest(ctx, "", body, nil, ingestOptions{createdBy: rec.CreatedBy, createdAt: rec.CreatedAt, restore: &rec})
	return err
}

// cleanName reduces a client-supplied file name to a plain name: no directories, no control characters, at most
// 200 bytes.
func cleanName(n string) string {
	n = strings.ReplaceAll(n, `\`, "/")
	n = path.Base(n)
	n = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError || r == '/' {
			return -1
		}
		return r
	}, n)
	n = strings.TrimSpace(n)
	for len(n) > 200 {
		_, sz := utf8.DecodeLastRuneInString(n)
		n = n[:len(n)-sz]
	}
	if n == "" || n == "." || n == ".." {
		return "file"
	}
	return n
}

// Files adapts the service to the export format's file store: Open streams the bytes for an export, Restore
// stores and records a file on import (hash-checked, see Restore).
func (s *Service) Files() interface {
	Open(ctx context.Context, sha string) (io.ReadCloser, error)
	Restore(ctx context.Context, rec store.Upload, r io.Reader) error
} {
	return exportFiles{s}
}

type exportFiles struct{ s *Service }

func (e exportFiles) Open(ctx context.Context, sha string) (io.ReadCloser, error) {
	rc, _, err := e.s.o.Storage.Get(ctx, sha)
	return rc, err
}

func (e exportFiles) Restore(ctx context.Context, rec store.Upload, r io.Reader) error {
	return e.s.Restore(ctx, rec, r)
}

// Touch marks a file as used now, so garbage collection keeps it: an item write that attaches the file calls it
// in its transaction. It returns ErrNotFound when there is no such file.
func (s *Service) Touch(ctx context.Context, sha string) error {
	if !IsSha(sha) {
		return ErrNotFound
	}
	touch := func(tx store.Tx) error {
		ok, err := tx.Uploads().Touch(ctx, sha, s.o.Clock.Now())
		if err == nil && !ok {
			return ErrNotFound
		}
		return err
	}
	if tx, ok := store.TxFrom(ctx); ok {
		return touch(tx)
	}
	return s.o.DB.Tx(ctx, touch)
}
