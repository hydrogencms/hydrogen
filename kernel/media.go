package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/exportfmt"
	"github.com/hydrogencms/hydrogen/jobs"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/media"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

// MediaConfig configures file storage for the schema's upload types (types with `upload:`) and `file` fields. It
// is read always and used only when the schema has either.
type MediaConfig struct {
	// MaxUpload bounds every upload (default 50MB); a type's upload.max_size can only lower it.
	MaxUpload config.Size  `yaml:"max_upload"`
	Storage   MediaStorage `yaml:"storage"`
	// PublicURL is the base of file URLs in delivery responses when a CDN serves /assets (empty: this server).
	PublicURL string `yaml:"public_url"`
}

// MediaStorage selects and configures the storage driver.
type MediaStorage struct {
	Driver string `yaml:"driver"` // local (default) or s3
	// Dir is the local driver's directory (default data/media).
	Dir string `yaml:"dir"`
	// The s3 driver: any S3-compatible service. Endpoint defaults to AWS; MinIO and most self-hosted
	// services need path_style.
	Bucket       string `yaml:"bucket"`
	Region       string `yaml:"region"`
	Endpoint     string `yaml:"endpoint"`
	PathStyle    bool   `yaml:"path_style"`
	Prefix       string `yaml:"prefix"`
	AccessKey    string `yaml:"access_key" config:"secret"`
	SecretKey    string `yaml:"secret_key" config:"secret"`
	SessionToken string `yaml:"session_token" config:"secret"`
}

// check validates what does not need the network.
func (c *MediaConfig) check() error {
	if c.MaxUpload < 0 {
		return fmt.Errorf("media.max_upload must not be negative")
	}
	if c.PublicURL != "" {
		if u, err := url.Parse(c.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("media.public_url %q must be an http(s) URL", c.PublicURL)
		}
	}
	switch c.Storage.Driver {
	case "", "local":
		if c.Storage.Bucket != "" || c.Storage.Endpoint != "" || c.Storage.AccessKey != "" {
			return fmt.Errorf("media.storage: bucket, endpoint and keys belong to driver s3 (driver is local)")
		}
	case "s3":
		if c.Storage.Bucket == "" {
			return fmt.Errorf("media.storage.bucket is required for driver s3")
		}
		if c.Storage.AccessKey == "" || c.Storage.SecretKey == "" {
			return fmt.Errorf("media.storage.access_key and secret_key are required for driver s3 (use ${env:…})")
		}
		if _, err := media.NewS3(media.S3Config{Bucket: c.Storage.Bucket, Region: c.Storage.Region, Endpoint: c.Storage.Endpoint,
			AccessKey: c.Storage.AccessKey, SecretKey: c.Storage.SecretKey}); err != nil {
			return fmt.Errorf("media.storage: %w", err)
		}
	default:
		return fmt.Errorf("media.storage.driver %q is not local or s3", c.Storage.Driver)
	}
	return nil
}

func (c *MediaConfig) storage(clk clock.Clock) (media.Storage, error) {
	st := c.Storage
	if st.Driver == "s3" {
		return media.NewS3(media.S3Config{Bucket: st.Bucket, Region: st.Region, Endpoint: st.Endpoint, PathStyle: st.PathStyle,
			Prefix: st.Prefix, AccessKey: st.AccessKey, SecretKey: st.SecretKey, SessionToken: st.SessionToken, Clock: clk})
	}
	dir := st.Dir
	if dir == "" {
		dir = "data/media"
	}
	return media.NewLocal(dir)
}

// uploadTypes lists the schema's upload types: the collections whose items are files.
func uploadTypes(s *schema.Schema) []media.UploadType {
	var out []media.UploadType
	for _, t := range s.Types() {
		if t.Upload != nil {
			out = append(out, media.UploadType{Name: t.Name, Accept: t.Upload.Accept, MaxSize: t.Upload.MaxSize})
		}
	}
	return out
}

// fileTypes lists the types whose documents can hold file values: upload types and any type with a `file`
// field (in a group, a list or block props).
func fileTypes(s *schema.Schema) []string {
	var out []string
	for _, t := range s.Types() {
		if t.Upload != nil || s.HasFields(t, func(f *schema.Field) bool { return f.Shape.Base == schema.BaseFile }) {
			out = append(out, t.Name)
		}
	}
	return out
}

// mediaEnabled: media is on when the schema has an upload type or a file field.
func mediaEnabled(s *schema.Schema) bool { return len(fileTypes(s)) > 0 }

// newMedia builds the media service; nil when the schema has no upload type.
func newMedia(core *CoreConfig, sc *schema.Schema, db store.DB, clk clock.Clock, log *slog.Logger, al *audit.Log,
	ob *outbox.Outbox, gate media.Gate) (*media.Service, error) {
	fileTypeNames := fileTypes(sc)
	if len(fileTypeNames) == 0 {
		return nil, nil
	}
	types := uploadTypes(sc)
	st, err := core.Media.storage(clk)
	if err != nil {
		return nil, fmt.Errorf("media.storage: %w", err)
	}
	return media.New(media.Options{DB: db, Storage: st, Clock: clk, Log: log, Audit: al, Outbox: ob, Access: gate,
		Types: func() []media.UploadType { return types }, FileTypes: func() []string { return fileTypeNames }, MaxUpload: int64(core.Media.MaxUpload), PublicURL: core.Media.PublicURL})
}

func (s *Services) mediaRoutes() []manage.Route {
	if s.Media == nil {
		return nil
	}
	return s.Media.ManageRoutes()
}

func mediaJob(s *Services) jobs.Job {
	return jobs.Job{Name: media.JobName, Schedule: media.JobSchedule, Run: func(ctx context.Context) error {
		_, err := s.Media.GC(ctx)
		return err
	}}
}

func mediaWarnings(c *MediaConfig, s *schema.Schema) []string {
	if mediaEnabled(s) || (c.Storage == MediaStorage{} && c.MaxUpload == 0 && c.PublicURL == "") {
		return nil
	}
	return []string{"media is configured but no type has `upload:` or a file field: files are not stored"}
}

// fileRegistry adapts the media service to the content service's file registry.
type fileRegistry struct{ m *media.Service }

func (f fileRegistry) Lookup(ctx context.Context, sha string) (content.FileInfo, error) {
	file, err := f.m.Lookup(ctx, sha)
	if errors.Is(err, media.ErrNotFound) {
		return content.FileInfo{}, content.ErrFileNotFound
	}
	return content.FileInfo{ID: file.SHA, Mime: file.MIME, Size: file.Size, Width: file.Width, Height: file.Height}, err
}

func (f fileRegistry) Touch(ctx context.Context, sha string) error {
	err := f.m.Touch(ctx, sha)
	if errors.Is(err, media.ErrNotFound) {
		return content.ErrFileNotFound
	}
	return err
}

// files is the registry for content.Options.Files; nil without media.
func files(m *media.Service) content.Files {
	if m == nil {
		return nil
	}
	return fileRegistry{m}
}

// mediaFiles is the export/import file store; nil without media (an interface holding nil would not be nil).
func mediaFiles(m *media.Service) exportfmt.FileStorage {
	if m == nil {
		return nil
	}
	return m.Files()
}
