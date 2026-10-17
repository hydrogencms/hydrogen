package delivery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// The relay event stream (GET /preview/events?proof=) tells a share link's viewers what the item looks like
// now: a "draft" event on connect and after every relay change of the item, "error" when the item cannot be
// built right now, "end" when the link expires or is revoked or the item is gone, and a ": ping" comment every
// 25 seconds. A draft event is {"rev": n, "v": "<hash>", "item": {...}}: rev is the relay applied in item (0
// when none) and v identifies the item JSON sent, so viewers can skip a repeat.
const (
	pingEvery = 25 * time.Second
	// minGap keeps a stream to four events per second.
	minGap = 250 * time.Millisecond
	// recheckEvery re-reads the share of a stream, in case a revoke notification was missed.
	recheckEvery = 5 * time.Minute

	// DefaultMaxStreamsPerShare and DefaultMaxStreams cap the open event streams of one share link and of one
	// server.
	DefaultMaxStreamsPerShare = 20
	DefaultMaxStreams         = 1000
)

var errTooManyStreams = errors.New("delivery: too many open preview streams")

type streamKey struct {
	item            ids.UUID
	channel, locale string
}

// frame is one event, built once and shared by every stream it is delivered to.
type frame struct {
	event string
	data  []byte
	final bool // the stream ends after it
}

type stream struct {
	share store.ShareSession
	key   streamKey
	wake  chan struct{}

	mu      sync.Mutex
	pending *frame
}

// deliver makes f the stream's next event, replacing an undelivered one (the latest state wins), but never a
// final one.
func (s *stream) deliver(f *frame) {
	s.mu.Lock()
	if s.pending == nil || !s.pending.final {
		s.pending = f
	}
	s.mu.Unlock()
	s.poke()
}

func (s *stream) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *stream) take() *frame {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.pending
	s.pending = nil
	return f
}

// hub keeps the open streams of this server and the items whose relay changed since the last refresh.
type hub struct {
	mu       sync.Mutex
	streams  map[*stream]struct{}
	viewers  map[ids.UUID]int
	started  bool
	perShare int
	total    int

	dmu      sync.Mutex
	dirtyAll bool
	dirty    map[ids.UUID]struct{}
	kick     chan struct{}
}

func newHub(perShare, total int) *hub {
	return &hub{streams: map[*stream]struct{}{}, viewers: map[ids.UUID]int{}, perShare: perShare, total: total,
		dirty: map[ids.UUID]struct{}{}, kick: make(chan struct{}, 1)}
}

func (h *hub) add(s *stream) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.streams) >= h.total || h.viewers[s.share.ID] >= h.perShare {
		return errTooManyStreams
	}
	h.streams[s] = struct{}{}
	h.viewers[s.share.ID]++
	return nil
}

func (h *hub) remove(s *stream) {
	h.mu.Lock()
	delete(h.streams, s)
	if h.viewers[s.share.ID]--; h.viewers[s.share.ID] <= 0 {
		delete(h.viewers, s.share.ID)
	}
	h.mu.Unlock()
}

func (h *hub) viewersOf(share ids.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.viewers[share]
}

func (h *hub) isStarted() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.started
}

// endShare ends the streams of a share.
func (h *hub) endShare(id ids.UUID, reason string) {
	f := &frame{event: "end", data: []byte(`{"reason":"` + reason + `"}`), final: true}
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.streams {
		if s.share.ID == id {
			s.deliver(f)
		}
	}
}

func (h *hub) markItem(id ids.UUID) {
	h.dmu.Lock()
	h.dirty[id] = struct{}{}
	h.dmu.Unlock()
	h.wakeRefresh()
}

func (h *hub) markAll() {
	h.dmu.Lock()
	h.dirtyAll = true
	h.dmu.Unlock()
	h.wakeRefresh()
}

func (h *hub) wakeRefresh() {
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

// groups returns the streams to refresh, by what they show: every stream after a gap, else those of the changed
// items.
func (h *hub) groups() map[streamKey][]*stream {
	h.dmu.Lock()
	all, items := h.dirtyAll, h.dirty
	h.dirtyAll, h.dirty = false, map[ids.UUID]struct{}{}
	h.dmu.Unlock()
	out := map[streamKey][]*stream{}
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.streams {
		if _, ok := items[s.key.item]; all || ok {
			out[s.key] = append(out[s.key], s)
		}
	}
	return out
}

// startEvents subscribes to relay notifications until ctx ends.
func (a *API) startEvents(ctx context.Context) error {
	if a.o.Notifier == nil || a.o.DB == nil {
		return nil
	}
	ch, err := a.o.Notifier.Listen(ctx, store.PreviewChannel)
	if err != nil {
		return err
	}
	a.hub.mu.Lock()
	a.hub.started = true
	a.hub.mu.Unlock()
	go func() {
		for n := range ch {
			a.note(n.Payload)
		}
	}()
	go a.refreshLoop(ctx)
	return nil
}

// note handles a notification: "<item>|<locale>|<rev>" (a relay changed), "share|<id>" (revoked), "relays|purged"
// or an empty payload (a gap: anything may have changed).
func (a *API) note(payload string) {
	kind, rest, _ := strings.Cut(payload, "|")
	switch kind {
	case "":
		a.shares.clear()
		a.hub.markAll()
	case "share":
		a.shares.clear()
		if id, err := ids.Parse(rest); err == nil {
			a.hub.endShare(id, "revoked")
		}
	case "relays":
		a.hub.markAll()
	default:
		if id, err := ids.Parse(kind); err == nil {
			a.hub.markItem(id)
		}
	}
}

// refreshLoop builds the changed items once per (item, channel, locale) and hands the result to every stream
// that shows it. Changes arriving while it works, and within minGap after, are merged into the next round.
func (a *API) refreshLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.hub.kick:
		}
		for _, list := range a.hub.groups() {
			var rep *stream
			for _, s := range list {
				if endReason(a, s.share) == "" {
					rep = s
					break
				}
			}
			if rep == nil {
				for _, s := range list {
					s.poke() // each finds its own link ended
				}
				continue
			}
			bctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			f := a.buildFrame(bctx, rep.share)
			cancel()
			for _, s := range list {
				s.deliver(f)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(minGap):
		}
	}
}

// normOrigin returns scheme://host[:port] of an origin or URL, lower case, without the scheme's default port.
func normOrigin(s string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port == "80" && scheme == "http" || port == "443" && scheme == "https" {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return scheme + "://" + host, true
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	o, _ := normOrigin(scheme + "://" + r.Host)
	return o
}

// corsAllowed reports whether a browser page at origin may read the stream: the request's own origin (scheme
// and host), the channels' frontends and the other configured origins. Never every origin.
func (a *API) corsAllowed(r *http.Request, origin string) bool {
	o, ok := normOrigin(origin)
	if !ok {
		return false
	}
	return o == requestOrigin(r) || slices.Contains(a.corsOrigins, o)
}

func (a *API) serveEvents(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	if origin := r.Header.Get("Origin"); origin != "" && a.corsAllowed(r, origin) {
		h.Set("Access-Control-Allow-Origin", origin)
		h.Add("Vary", "Origin")
	}
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	if !a.hub.isStarted() {
		a.plainError(w, &manage.Error{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "preview events are not available"})
		return
	}
	sess, cl, err := a.verifyProof(r.Context(), r.URL.Query().Get("proof"))
	if err != nil {
		a.previewFailed(w, r, err)
		return
	}
	m := a.meta[cl.Channel]
	flusher, ok := w.(http.Flusher)
	if m == nil || !ok {
		a.plainError(w, unauthorized("invalid or expired preview token"))
		return
	}
	st := &stream{share: sess, key: streamKey{sess.Item, sess.Channel, sess.Locale}, wake: make(chan struct{}, 1)}
	if err := a.hub.add(st); err != nil {
		h.Set("Retry-After", "30")
		a.plainError(w, &manage.Error{Status: http.StatusTooManyRequests, Code: "too_many_streams",
			Message: "too many open preview streams; close one and retry"})
		return
	}
	defer a.hub.remove(st)
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	rc := http.NewResponseController(w)
	write := func(format string, args ...any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	// send writes an event; false ends the stream.
	send := func(f *frame) bool {
		return write("event: %s\ndata: %s\n\n", f.event, f.data) && !f.final
	}
	ctx := r.Context()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	recheck := time.NewTicker(recheckEvery)
	defer recheck.Stop()
	expiry := time.NewTimer(max(sess.ExpiresAt.Sub(a.o.Clock.Now()), 0))
	defer expiry.Stop()
	ended := func() bool {
		if reason := endReason(a, sess); reason != "" {
			send(endFrame(reason))
			return true
		}
		return false
	}

	if ended() || !send(a.buildFrame(ctx, sess)) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-st.wake:
			if ended() {
				return
			}
			f := st.take()
			if f == nil {
				continue
			}
			if !send(f) {
				return
			}
			select {
			case <-time.After(minGap):
			case <-ctx.Done():
				return
			}
		case <-expiry.C:
			if ended() {
				return
			}
			expiry.Reset(time.Second)
		case <-recheck.C:
			if cur, found, err := a.freshShare(ctx, sess.ID); err == nil && (!found || endReason(a, cur) != "") {
				reason := "revoked"
				if found {
					reason = endReason(a, cur)
				}
				send(endFrame(reason))
				return
			}
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		}
	}
}

func endFrame(reason string) *frame {
	return &frame{event: "end", data: []byte(`{"reason":"` + reason + `"}`), final: true}
}

// freshShare reads a session past the lookup cache.
func (a *API) freshShare(ctx context.Context, id ids.UUID) (s store.ShareSession, found bool, err error) {
	err = a.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		s, err = tx.Preview().GetShare(ctx, id)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		return s, false, nil
	}
	return s, err == nil, err
}

func endReason(a *API, s store.ShareSession) string {
	now := a.o.Clock.Now()
	switch {
	case s.RevokedAt != nil && !s.RevokedAt.After(now), !a.o.ShareLinks:
		return "revoked"
	case !now.Before(s.ExpiresAt):
		return "expired"
	}
	return ""
}

// buildFrame renders the event of a share session: its item as it is now, "end" (unavailable) when the item is
// gone, or "error" when it cannot be built right now.
func (a *API) buildFrame(ctx context.Context, s store.ShareSession) *frame {
	body, status := a.itemFor(ctx, s)
	switch status {
	case http.StatusOK:
		return &frame{event: "draft", data: body}
	case http.StatusNotFound:
		return endFrame("unavailable")
	case http.StatusUnauthorized:
		if reason := endReason(a, s); reason != "" {
			return endFrame(reason)
		}
	}
	a.log.Error("delivery: build preview event", "share", s.ID, "item", s.Item, "status", status)
	return &frame{event: "error", data: []byte(`{"message":"the preview could not be built"}`)}
}

// relayProbe collects which relay the overlay applied to an item while one request is built.
type relayProbe struct {
	item ids.UUID
	rev  atomic.Int64
}

type probeKey struct{}

// itemFor renders the draft event data of a share session: what GET /items/{id} answers for its token now.
func (a *API) itemFor(ctx context.Context, s store.ShareSession) ([]byte, int) {
	cl, err := a.shareClaims(s)
	if err != nil {
		return nil, http.StatusUnauthorized
	}
	q := url.Values{"channel": {s.Channel}}
	if s.Locale != "" {
		q.Set("locale", s.Locale)
	}
	probe := &relayProbe{item: s.Item}
	ctx = context.WithValue(context.WithValue(ctx, claimsKey{}, cl), probeKey{}, probe)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/items/"+s.Item.String()+"?"+q.Encode(), nil)
	if err != nil {
		return nil, http.StatusInternalServerError
	}
	c := &capture{h: http.Header{}}
	a.handler.ServeHTTP(c, req)
	if c.status != http.StatusOK && c.status != 0 {
		return nil, c.status
	}
	var doc struct {
		Item json.RawMessage `json:"item"`
	}
	if err := json.Unmarshal(c.body.Bytes(), &doc); err != nil || len(doc.Item) == 0 {
		return nil, http.StatusInternalServerError
	}
	var item bytes.Buffer
	if err := json.Compact(&item, doc.Item); err != nil {
		return nil, http.StatusInternalServerError
	}
	sum := sha256.Sum256(item.Bytes())
	var out bytes.Buffer
	fmt.Fprintf(&out, `{"rev":%d,"v":"%s","item":`, probe.rev.Load(), hex.EncodeToString(sum[:8]))
	out.Write(item.Bytes())
	out.WriteByte('}')
	return out.Bytes(), http.StatusOK
}
