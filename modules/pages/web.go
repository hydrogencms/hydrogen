package pages

import (
	"context"
	"net/http"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
)

// Located kinds beyond the Resolution kinds.
const (
	LocatedReserved  = "reserved"
	LocatedNoChannel = "no_channel"
)

// Located is a URL resolved through the channels: which channel and locale, what it shows, its canonical URL
// and its alternates (hreflang). Contract: planning/design/channels-h3.md.
type Located struct {
	Channel    string      `json:"channel,omitempty"`
	Kind       string      `json:"kind"`
	Locale     string      `json:"locale,omitempty"`
	Resolution *Resolution `json:"resolution,omitempty"`
	Canonical  string      `json:"canonical,omitempty"`
	Alternates []Alternate `json:"alternates,omitempty"`
}

type Alternate struct {
	Channel string `json:"channel"`
	Locale  string `json:"locale"`
	URL     string `json:"url"`
}

// Match finds the web channel for a URL and returns the site path after the channel's prefix. Candidates are
// the web channels whose host, prefix (on a segment boundary) and header fit; the most specific wins: more
// matcher parts, then the longer prefix, then the name.
func (s *Service) Match(rawURL string, h http.Header) (*channel.Channel, string, bool) {
	if s.impl == nil {
		return nil, "", false
	}
	w, path, ok := s.impl.matchURL(rawURL, h)
	if !ok {
		return nil, "", false
	}
	return w.ch, path, true
}

// Locate resolves a URL through the channels: it picks the channel, tries the path in each of the channel's
// languages in order, and adds the canonical URL and the alternates of a page or item.
func (s *Service) Locate(ctx context.Context, rawURL string, h http.Header) (*Located, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	res, err := impl.locate(ctx, rawURL, h)
	if err != nil {
		return nil, err
	}
	if hidden, err := impl.limitResolution(ctx, res.Resolution); err != nil {
		return nil, err
	} else if hidden {
		return &Located{Channel: res.Channel, Kind: ResolvedNotFound, Resolution: notFoundResolution()}, nil
	}
	return res, nil
}

// URL is the item's absolute URL in a channel; false when it has no route there.
func (s *Service) URL(ctx context.Context, ch *channel.Channel, item ids.UUID, locale string) (string, bool, error) {
	impl, err := s.get()
	if err != nil {
		return "", false, err
	}
	return impl.itemURL(ctx, ch, item, locale)
}

// LinkURL links to an item from a channel: relative within the channel, absolute elsewhere.
func (s *Service) LinkURL(ctx context.Context, from *channel.Channel, item ids.UUID) (string, error) {
	impl, err := s.get()
	if err != nil {
		return "", err
	}
	return impl.linkURL(ctx, from, item)
}

// SiteURL is the URL of a site path in a web channel: relative (the channel's prefix plus the path) and
// absolute (with the channel's origin, empty when it has none).
func (s *Service) SiteURL(ch *channel.Channel, path string) (rel, abs string) {
	if s.impl == nil {
		return path, ""
	}
	w := s.impl.web.of(ch)
	return w.rel(path), w.abs(path)
}
