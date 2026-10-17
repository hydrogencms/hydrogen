// Package images is the image transforms module: named, versioned variants (presets) of uploaded raster
// images, made in process or by imgproxy, served at /images, listed in delivery responses as `variants` and
// used for the admin's thumbnails. Without upload types in the schema it does nothing. See docs/images.md.
package images

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/features/media"
)

// ChannelOption names the channel option that limits the presets a channel's assets list.
const ChannelOption = "image_presets"

const (
	// JobName is the scheduled removal of stored variants that no preset can request any more.
	JobName     = "images.gc"
	jobSchedule = "@daily"

	waitForWorker = 30 * time.Second
	transformCap  = 60 * time.Second
	putTimeout    = 30 * time.Second

	focalTTL     = 30 * time.Second
	focalRefresh = 2 * time.Second
	lookupTTL    = 5 * time.Minute
	missTTL      = 10 * time.Second
	cacheSlots   = 1024
)

// Module returns the images module.
func Module() kernel.Module {
	return &module{wait: waitForWorker, limit: transformCap, clock: clock.System{}, transform: Transform}
}

// mediaAPI is the part of the media service the module uses.
type mediaAPI interface {
	Lookup(ctx context.Context, sha string) (media.File, error)
	Open(ctx context.Context, sha string) (io.ReadCloser, media.File, error)
	PutDerived(ctx context.Context, sha, suffix string, r io.Reader, size int64, mime string) error
	OpenDerived(ctx context.Context, sha, suffix string) (io.ReadCloser, media.Info, error)
	DeleteDerived(ctx context.Context, sha, suffix string) error
	ListDerived(ctx context.Context, after string, limit int) ([]media.Entry, error)
}

type module struct {
	cfg   Config
	s     *setup
	log   *slog.Logger
	media mediaAPI
	// focals lists the focal point tokens of the items whose file is sha (draft or published).
	focals     func(ctx context.Context, sha string) (map[string]bool, error)
	transform  func(ctx context.Context, src io.Reader, p Preset, w int, focal *Focal, maxPixels int64) ([]byte, string, error)
	clock      clock.Clock
	focalCache *ttlCache[focalSet]
	fileCache  *ttlCache[fileEntry]
	versions   map[string]string
	sem        chan struct{}
	wait       time.Duration // how long a request waits for a worker
	limit      time.Duration // how long one transform may run

	mu      sync.Mutex
	flights map[string]*flight
}

func (*module) Name() string  { return "images" }
func (m *module) Config() any { return &m.cfg }

func (m *module) Register(r *kernel.Registry) error {
	s, err := m.cfg.build(runtime.GOMAXPROCS(0))
	if err != nil {
		return err
	}
	m.init(s)
	r.ChannelOption(channel.Option{Name: ChannelOption, Decode: m.decodePresets})
	r.AssetDecorator(m)
	r.AdminImages(m.adminImages)
	r.Handle("GET "+URLBase+"/{preset}/{v}/{w}/{focal}/{sha}/{name}", m.handler())
	return r.Job(JobName, jobSchedule, m.gc)
}

func (m *module) init(s *setup) {
	m.s = s
	m.versions = make(map[string]string, len(s.presets))
	for n, p := range s.presets {
		m.versions[n] = p.Version(s.driver)
	}
	m.sem = make(chan struct{}, s.workers)
	m.flights = map[string]*flight{}
	m.focalCache = newTTLCache[focalSet](m.clock, cacheSlots)
	m.fileCache = newTTLCache[fileEntry](m.clock, cacheSlots)
}

func (m *module) Boot(_ context.Context, s *kernel.Services) error {
	m.log = s.Log
	if s.Media != nil {
		m.media = s.Media
		m.focals = func(ctx context.Context, sha string) (map[string]bool, error) { return itemFocals(ctx, s, sha) }
	}
	return nil
}

// decodePresets reads a channel's image_presets: a list of preset names.
func (m *module) decodePresets(n *config.Node) (any, error) {
	if n.Kind() != "list" {
		return nil, n.Errorf("%s must be a list of preset names", ChannelOption)
	}
	var out []string
	for _, item := range n.Items() {
		name, err := item.String()
		if err != nil {
			return nil, err
		}
		if _, ok := m.s.presets[name]; !ok || name == AdminPreset {
			return nil, item.Errorf("%s: unknown preset %q (modules.images.presets lists %v)", ChannelOption, name, m.s.names)
		}
		out = append(out, name)
	}
	return out, nil
}

// adminImages tells the admin where thumbnails are and which origins images come from.
func (m *module) adminImages() kernel.AdminImages {
	if m.media == nil {
		return kernel.AdminImages{}
	}
	p := m.s.presets[AdminPreset]
	im := kernel.AdminImages{ThumbBase: m.base() + "/" + AdminPreset + "/" + m.versions[AdminPreset] + "/" + itoa(p.Width) + "/c"}
	if m.s.publicURL != "" {
		im.Origins = append(im.Origins, originOf(m.s.publicURL))
	}
	if m.s.driver == driverImgproxy {
		im.Origins = append(im.Origins, originOf(m.s.proxy.Base))
	}
	return im
}

var errBusy = errors.New("images: no worker free")
