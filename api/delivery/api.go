// Package delivery is the read-only delivery API (/api/deliver/v1): published content as stored JSON snapshots,
// selected by channel, with an in-process response cache that cache-tag purges invalidate, and draft previews.
// Contract: planning/design/delivery-h4.md.
//
// Each item is shown in one locale, chosen from the channel's languages in order (with ?locale= moved to the
// front). An item carries "fallback": true when it is shown in a locale other than the requested one: the
// ?locale= value when the request has one, else the channel's first language. Listings only contain items the
// channel can show in one of its languages, so a page is short only at the end. A listing answers
// {"items", "hasMore"}; "total" (an exact count, which costs more) only with ?total=true.
//
// Cache tags: a response is tagged with every item id it looked up, including ones that turned out to be
// unavailable (publishing them changes the response). Beyond 64 item tags (MaxItemTags) a response is tagged
// with the listing tag of each type involved instead, which keeps the Surrogate-Key and Cache-Tag headers
// within a few kilobytes. Tags added by modules come on top and are theirs to keep few.
//
// Query strings are canonical in the cache key: parameters are ordered by name with the order of repeated
// values kept; locale, channel, include, limit, offset, q, sort, total and preview may appear once.
package delivery

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/audit"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Prefix is where the delivery API is mounted.
const Prefix = "/api/deliver/v1"

// MinPreviewSecret is the shortest preview signing secret New accepts, in bytes.
const MinPreviewSecret = 32

// DefaultCacheBytes is the response cache size when none is configured.
const DefaultCacheBytes = 32 << 20

// Settings the delivery API reads from a channel.
const settingCacheTTL = "delivery.cache_ttl"

const (
	defaultTTL  = 60
	maxInclude  = 3
	maxIncluded = 100
	unavailable = "not available"
)

// Decorator adds keys to items before they are written. Decorate runs once per response with every item of it
// (the requested items and the included ones), so a decorator can fetch what it needs in one batch. Keys of
// Item.Extra are spliced into the payload sorted; they may not reuse a payload key.
type Decorator interface {
	Decorate(ctx context.Context, ch *channel.Channel, items []*Item) error
}

// Item is one item of a response, as a Decorator sees it.
type Item struct {
	ID, Type, Locale string
	Refs             []ids.UUID
	Extra            map[string]any
}

// Route is a delivery endpoint a module adds. Method must be GET. The handler runs only after the channel was
// selected and, for token channels, authenticated; use Channel(r), Locale(r), Tag, NoStore, Fail and
// WriteJSON. Successful responses are cached by the in-process cache and carry ETag and cache headers like the
// core routes.
type Route struct {
	Method  string
	Path    string // relative to /api/deliver/v1, ServeMux syntax
	Handler http.HandlerFunc
	Summary string
	// OpenAPI is the operation object for the document; optional.
	OpenAPI map[string]any
	// Module names the module that registered the route, for error messages.
	Module string
}

// Varier lists extra request header names a channel's responses depend on (sent as Vary, and part of the
// in-process cache key). Authorization is always listed.
type Varier func(ch *channel.Channel) []string

// PreviewURLer resolves the URL a frontend shows an item at, for preview links. It returns a path or an
// absolute URL without a preview token, and false when it has no URL for the item in that channel and locale.
type PreviewURLer func(ctx context.Context, ch *channel.Channel, item ids.UUID, typ, locale string) (string, bool, error)

// PatternURLer turns the path a channel's link or preview pattern produced into the URLs a frontend serves it
// at: rel is relative to the channel (the pages module adds a web channel's prefix), abs adds the origin. It
// reports false for channels it does not handle; without a handler rel is the path and abs is the channel's
// frontend plus the path.
type PatternURLer func(ch *channel.Channel, path string) (rel, abs string, ok bool)

// Options configures New.
type Options struct {
	Content  *content.Service
	Schema   *schema.Schema
	Channels *channel.Set
	Reader   store.Reader
	Notifier store.Notifier
	// DB serves share links and relayed drafts (see share.go); nil turns both off.
	DB    store.DB
	Audit *audit.Log
	IDs   *ids.Generator
	// Rand is the source of share tokens; default crypto/rand.
	Rand  io.Reader
	Clock clock.Clock
	Log   *slog.Logger
	// CacheBytes bounds the response cache; 0 disables it.
	CacheBytes int64
	// PreviewSecret signs preview tokens; required, at least MinPreviewSecret bytes.
	PreviewSecret []byte
	Routes        []Route
	Decorators    []Decorator
	// PreviewURLs are asked in order for the URL of an item when a preview token is minted for it.
	PreviewURLs []PreviewURLer
	// PatternURLs are asked in order for the URL of a path made from a channel pattern.
	PatternURLs []PatternURLer
	// PublicOptions are the channel options shown by GET /channels/{name}.
	PublicOptions []string
	Varies        []Varier
	// ShareLinks enables share links: creating them, and accepting their tokens.
	ShareLinks bool
	// ShareDefaultTTL and ShareMaxTTL bound the lifetime of a new share link (default 24h and 168h).
	ShareDefaultTTL, ShareMaxTTL time.Duration
	// CORSOrigins are the origins (scheme://host[:port]) that may read the relay event stream, besides the
	// request's own origin.
	CORSOrigins []string
	// AssetBase is the base URL files are served from (the media public URL, or empty for the /assets route of
	// this server); an asset's url is AssetBase/{sha}/{name}. AssetURL overrides it when a CDN lays files out
	// differently.
	AssetBase string
	AssetURL  func(sha, name string) string
	// AssetDecorators add keys to every asset a response shows (an image module adds resized variants).
	AssetDecorators []AssetDecorator
	// MaxStreamsPerShare and MaxStreams cap the open relay event streams of one share link and of the server
	// (defaults 20 and 1000).
	MaxStreamsPerShare, MaxStreams int
}

// API is the delivery API.
type API struct {
	o       Options
	parents map[string]string // language → language it inherits from
	log     *slog.Logger
	cache   *cache
	meta    map[string]*chanMeta
	handler http.Handler
	openapi []byte
	oaETag  []string
	single  *chanMeta           // the only channel, when there is exactly one
	shared  map[string][]string // type → blocks fields that can hold shared blocks
	assets  map[string]bool     // types whose documents can hold asset fields
	shares  shareCache
	hub     *hub
	// corsOrigins are Options.CORSOrigins normalized (see normOrigin).
	corsOrigins []string
}

type chanMeta struct {
	ch    *channel.Channel
	token bool
	vary  []string // extra header names (canonical), beyond Authorization
	hVary []string
	hCC   []string
	info  []byte
}

type route struct {
	Route
	byName bool         // the channel comes from the path ({name}), not the request
	raw    http.Handler // served as is, without channel selection
}

// New builds the API: it validates the module routes, precomputes per-channel headers, channel info and the
// OpenAPI document.
func New(o Options) (*API, error) {
	switch {
	case o.Schema == nil || o.Channels == nil || o.Reader == nil || o.Content == nil:
		return nil, errors.New("delivery: Content, Schema, Channels and Reader are required")
	case len(o.PreviewSecret) < MinPreviewSecret:
		return nil, fmt.Errorf("delivery: PreviewSecret is required and must be at least %d bytes", MinPreviewSecret)
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Rand == nil {
		o.Rand = crand.Reader
	}
	if o.IDs == nil {
		o.IDs = ids.NewGenerator(o.Clock, o.Rand)
	}
	if o.ShareDefaultTTL == 0 {
		o.ShareDefaultTTL = DefaultShareTTL
	}
	if o.ShareMaxTTL == 0 {
		o.ShareMaxTTL = MaxShareTTL
	}
	if o.MaxStreamsPerShare <= 0 {
		o.MaxStreamsPerShare = DefaultMaxStreamsPerShare
	}
	if o.MaxStreams <= 0 {
		o.MaxStreams = DefaultMaxStreams
	}
	a := &API{o: o, parents: parentsOf(o.Channels), log: o.Log, meta: map[string]*chanMeta{}, hub: newHub(o.MaxStreamsPerShare, o.MaxStreams)}
	for _, origin := range o.CORSOrigins {
		if n, ok := normOrigin(origin); ok {
			a.corsOrigins = append(a.corsOrigins, n)
		}
	}
	a.shared = a.sharedFields()
	a.assets = a.assetTypes()
	if a.log == nil {
		a.log = slog.New(slog.DiscardHandler)
	}
	if o.CacheBytes > 0 {
		a.cache = newCache(o.CacheBytes)
	}
	if err := a.buildMeta(); err != nil {
		return nil, err
	}
	routes := a.coreRoutes()
	for _, rt := range o.Routes {
		if rt.Method != http.MethodGet || !strings.HasPrefix(rt.Path, "/") || strings.ContainsAny(rt.Path, " \t") || rt.Handler == nil {
			return nil, fmt.Errorf("delivery: %s: a route needs method GET, a path starting with / and a handler", describe(rt))
		}
		routes = append(routes, route{Route: rt})
	}
	mux := http.NewServeMux()
	for _, rt := range routes {
		if err := handleRoute(mux, rt, a); err != nil {
			return nil, err
		}
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		a.plainError(w, &manage.Error{Status: http.StatusNotFound, Code: "not_found", Message: "no such endpoint"})
	})
	a.handler = mux
	doc, err := marshal(openAPI(o.Schema, o.Routes))
	if err != nil {
		return nil, fmt.Errorf("delivery: encode openapi: %w", err)
	}
	a.openapi = doc
	sum := sha256.Sum256(doc)
	a.oaETag = []string{`"` + hex.EncodeToString(sum[:]) + `"`}
	return a, nil
}

func describe(rt Route) string {
	if rt.Module != "" {
		return fmt.Sprintf("module %s: route %s %s", rt.Module, rt.Method, rt.Path)
	}
	return fmt.Sprintf("route %s %s", rt.Method, rt.Path)
}

func handleRoute(mux *http.ServeMux, rt route, a *API) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("delivery: %s: %v", describe(rt.Route), v)
		}
	}()
	if rt.raw != nil {
		mux.Handle(rt.Method+" "+rt.Path, rt.raw)
		return nil
	}
	mux.Handle(rt.Method+" "+rt.Path, a.wrap(rt))
	return nil
}

// Handler serves the API; mount it with http.StripPrefix(Prefix, h).
func (a *API) Handler() http.Handler { return a.handler }

func (a *API) buildMeta() error {
	chans := a.o.Channels.All()
	for _, ch := range chans {
		m := &chanMeta{ch: ch, token: ch.Access == channel.AccessToken}
		names := map[string]bool{}
		for _, v := range a.o.Varies {
			for _, n := range v(ch) {
				if n = http.CanonicalHeaderKey(strings.TrimSpace(n)); n != "" && n != "Authorization" {
					names[n] = true
				}
			}
		}
		for n := range names {
			m.vary = append(m.vary, n)
		}
		slices.Sort(m.vary)
		m.hVary = []string{strings.Join(append([]string{"Authorization"}, m.vary...), ", ")}
		scope := "public"
		if m.token {
			scope = "private"
		}
		ttl := cacheTTL(ch)
		m.hCC = []string{scope + ", max-age=0, s-maxage=" + strconv.Itoa(ttl) + ", stale-while-revalidate=" + strconv.Itoa(ttl)}
		info, err := a.channelInfo(ch)
		if err != nil {
			return err
		}
		m.info = info
		a.meta[ch.Name] = m
	}
	if len(chans) == 1 {
		a.single = a.meta[chans[0].Name]
	}
	return nil
}

func cacheTTL(ch *channel.Channel) int {
	v, ok := ch.Setting(settingCacheTTL)
	if !ok {
		return defaultTTL
	}
	var n float64
	switch x := v.(type) {
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case uint64:
		n = float64(x)
	case float64:
		n = x
	default:
		return defaultTTL
	}
	return max(int(n), 0)
}

func (a *API) channelInfo(ch *channel.Channel) ([]byte, error) {
	colls := ch.Collections
	if colls == nil {
		for _, t := range a.o.Schema.Types() {
			colls = append(colls, t.Name)
		}
	}
	slices.Sort(colls)
	opts := map[string]any{}
	for _, n := range a.o.PublicOptions {
		if v, ok := ch.Options[n]; ok {
			opts[n] = v
		}
	}
	b, err := marshal(map[string]any{"name": ch.Name, "languages": ch.Languages, "collections": colls, "options": opts})
	if err != nil {
		return nil, fmt.Errorf("delivery: encode info of channel %s (a public channel option must be JSON-encodable): %w", ch.Name, err)
	}
	return b, nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// Purge drops cached responses carrying any of the tags; "*" drops all.
func (a *API) Purge(tags ...string) {
	if a.cache != nil {
		a.cache.purge(tags...)
	}
}

// CacheStats reports the response cache's entries and bytes.
func (a *API) CacheStats() (entries int, bytes int64) {
	if a.cache == nil {
		return 0, 0
	}
	return a.cache.stats()
}
