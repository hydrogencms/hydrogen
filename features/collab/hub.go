package collab

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

const (
	pingEvery = 25 * time.Second
	// minGap keeps a node's refresh rounds apart: bursts of notifications are merged into one read per item.
	minGap = 40 * time.Millisecond
	// resyncEvery re-reads every open session, in case a notification was missed.
	resyncEvery = 30 * time.Second

	feedFrames = 256
	feedBytes  = 2 << 20
	pullBatch  = 200
	ctrlCap    = 64
)

var errTooManyStreams = errors.New("collab: too many open streams")

// frame is one SSE event.
type frame struct {
	event string
	id    int64 // 0: no id line
	data  []byte
	final bool // the stream ends after it
	skip  bool // a final frame that is not written: the stream was replaced by a newer one
}

// stream is one open event stream: a client of an item. Updates are pulled from the item's feed (or the store)
// by the stream's own goroutine; other events are queued.
type stream struct {
	item      ids.UUID
	client    string
	principal string
	wake      chan struct{}
	last      int64 // the last update seq handled; owned by the stream's goroutine

	mu   sync.Mutex
	ctrl []*frame
}

func newStream(item ids.UUID, client, principal string, last int64) *stream {
	return &stream{item: item, client: client, principal: principal, last: last, wake: make(chan struct{}, 1)}
}

func (st *stream) poke() {
	select {
	case st.wake <- struct{}{}:
	default:
	}
}

// push queues a control event. A newer presence replaces an undelivered one; nothing follows a final event.
func (st *stream) push(f *frame) {
	st.mu.Lock()
	if n := len(st.ctrl); n == 0 || !st.ctrl[n-1].final {
		if f.event == "presence" {
			for i, p := range st.ctrl {
				if p.event == "presence" {
					st.ctrl = append(st.ctrl[:i], st.ctrl[i+1:]...)
					break
				}
			}
		}
		if len(st.ctrl) >= ctrlCap {
			st.ctrl = st.ctrl[1:]
		}
		st.ctrl = append(st.ctrl, f)
	}
	st.mu.Unlock()
	st.poke()
}

func (st *stream) take() []*frame {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.ctrl
	st.ctrl = nil
	return out
}

// feed is the node's cache of the newest updates of an item with open streams, so that N streams cost one
// read. It holds every update with floor < seq <= head, in order; the hub bounds the bytes of all feeds, evicting
// the oldest frames (a stream behind the floor reads from the store).
type feed struct {
	mu     sync.Mutex
	floor  int64
	head   int64
	frames []store.CollabUpdate
	bytes  int
	dead   bool // dropped by the hub: adds and evictions are ignored
}

func (f *feed) since(after int64, limit int) ([]store.CollabUpdate, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if after < f.floor {
		return nil, false
	}
	var out []store.CollabUpdate
	for _, u := range f.frames {
		if u.Seq > after {
			out = append(out, u)
			if len(out) == limit {
				break
			}
		}
	}
	return out, true
}

// add appends updates, trims the feed to its own limits and returns the change of its byte size.
func (f *feed) add(ups []store.CollabUpdate) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead {
		return 0
	}
	before := f.bytes
	for _, u := range ups {
		f.frames = append(f.frames, u)
		f.bytes += len(u.Data)
		f.head = max(f.head, u.Seq)
	}
	for len(f.frames) > feedFrames || f.bytes > feedBytes {
		f.evict()
	}
	return f.bytes - before
}

// evict drops the oldest frame; the caller holds f.mu.
func (f *feed) evict() int {
	n := len(f.frames[0].Data)
	f.floor = f.frames[0].Seq
	f.bytes -= n
	f.frames = f.frames[1:]
	return n
}

// evictOldest drops the oldest frame and returns the bytes freed (0: none left or the feed is dropped).
func (f *feed) evictOldest() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead || len(f.frames) == 0 {
		return 0
	}
	return f.evict()
}

func (f *feed) size() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead {
		return 0
	}
	return f.bytes
}

// kill marks the feed dropped and returns the bytes it held.
func (f *feed) kill() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.bytes
	f.dead, f.frames, f.bytes = true, nil, 0
	return n
}

func (f *feed) headSeq() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head
}

// hub keeps the open streams of this node and the items whose state changed since the last refresh.
type hub struct {
	s *Service

	mu      sync.Mutex
	started bool
	total   int
	byItem  map[ids.UUID]map[*stream]struct{}
	feeds   map[ids.UUID]*feed
	names   map[ids.UUID]map[string]string
	// feedBytes is the total of the feeds' bytes (bounded by the service's FeedBudget).
	feedBytes atomic.Int64

	dmu       sync.Mutex
	dirtyAll  bool
	dirtyUpd  map[ids.UUID]struct{}
	dirtyPres map[ids.UUID]struct{}
	kick      chan struct{}
}

func newHub(s *Service) *hub {
	return &hub{s: s, byItem: map[ids.UUID]map[*stream]struct{}{}, feeds: map[ids.UUID]*feed{},
		names: map[ids.UUID]map[string]string{}, dirtyUpd: map[ids.UUID]struct{}{}, dirtyPres: map[ids.UUID]struct{}{},
		kick: make(chan struct{}, 1)}
}

func (h *hub) isStarted() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.started
}

// add registers a stream, creating the item's feed from head when it has none. A client has one stream: an
// older one of the same client is closed. A principal may hold MaxStreamsPerPrincipal streams of an item.
func (h *hub) add(st *stream, head int64, name string) error {
	h.mu.Lock()
	set := h.byItem[st.item]
	var old *stream
	mine := 0
	for o := range set {
		switch {
		case o.client == st.client:
			old = o
		case o.principal == st.principal:
			mine++
		}
	}
	n := len(set)
	if old != nil {
		n--
	}
	if mine >= MaxStreamsPerPrincipal || n >= h.s.o.MaxStreamsPerItem || h.total-b2i(old != nil) >= h.s.o.MaxStreams {
		h.mu.Unlock()
		return errTooManyStreams
	}
	if set == nil {
		set = map[*stream]struct{}{}
		h.byItem[st.item] = set
	}
	if old != nil {
		delete(set, old)
		h.total--
	}
	set[st] = struct{}{}
	h.total++
	created := false
	if h.feeds[st.item] == nil {
		h.feeds[st.item] = &feed{floor: head, head: head}
		created = true
	}
	if h.names[st.item] == nil {
		h.names[st.item] = map[string]string{}
	}
	h.names[st.item][st.client] = name
	h.mu.Unlock()
	if old != nil {
		old.push(&frame{final: true, skip: true})
	}
	if created {
		h.markUpdates(st.item)
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (h *hub) remove(st *stream) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.byItem[st.item]
	if _, ok := set[st]; !ok {
		return
	}
	delete(set, st)
	h.total--
	if len(set) == 0 {
		delete(h.byItem, st.item)
		h.dropFeed(st.item)
		delete(h.names, st.item)
	}
}

// dropFeed forgets the item's feed and its bytes; the caller holds h.mu.
func (h *hub) dropFeed(item ids.UUID) {
	if f := h.feeds[item]; f != nil {
		h.feedBytes.Add(-int64(f.kill()))
		delete(h.feeds, item)
	}
}

// enforceBudget evicts the oldest frames of the largest feeds until all feeds fit the node's budget.
func (h *hub) enforceBudget() {
	budget := int64(h.s.o.FeedBudget)
	if h.feedBytes.Load() <= budget {
		return
	}
	h.mu.Lock()
	feeds := make([]*feed, 0, len(h.feeds))
	for _, f := range h.feeds {
		feeds = append(feeds, f)
	}
	h.mu.Unlock()
	for h.feedBytes.Load() > budget {
		var big *feed
		most := 0
		for _, f := range feeds {
			if n := f.size(); n > most {
				big, most = f, n
			}
		}
		if big == nil {
			return
		}
		h.feedBytes.Add(-int64(big.evictOldest()))
	}
}

func (h *hub) streamsOf(item ids.UUID) []*stream {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*stream, 0, len(h.byItem[item]))
	for st := range h.byItem[item] {
		out = append(out, st)
	}
	return out
}

func (h *hub) wakeLoop() {
	select {
	case h.kick <- struct{}{}:
	default:
	}
}

func (h *hub) markUpdates(item ids.UUID) {
	h.dmu.Lock()
	h.dirtyUpd[item] = struct{}{}
	h.dmu.Unlock()
	h.wakeLoop()
}

func (h *hub) markPresence(item ids.UUID) {
	h.dmu.Lock()
	h.dirtyPres[item] = struct{}{}
	h.dmu.Unlock()
	h.wakeLoop()
}

func (h *hub) markAll() {
	h.dmu.Lock()
	h.dirtyAll = true
	h.dmu.Unlock()
	h.wakeLoop()
}

// Start subscribes to the collaboration notifications of every server until ctx ends.
func (s *Service) Start(ctx context.Context) error {
	h := s.hub
	ch, err := s.o.DB.Notifier().Listen(ctx, store.CollabChannel)
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.started = true
	h.mu.Unlock()
	go func() {
		for n := range ch {
			h.note(ctx, n.Payload)
		}
	}()
	go h.loop(ctx)
	return nil
}

// note handles a notification (formats: store.CollabChannel).
func (h *hub) note(ctx context.Context, payload string) {
	if payload == "" {
		h.markAll()
		return
	}
	parts := strings.Split(payload, "|")
	item, err := ids.Parse(parts[0])
	if err != nil || len(parts) < 2 {
		return
	}
	arg := func(i int) string {
		if i < len(parts) {
			return parts[i]
		}
		return ""
	}
	switch parts[1] {
	case "update":
		h.markUpdates(item)
	case "presence":
		h.markPresence(item)
	case "saved":
		seq, _ := strconv.ParseInt(arg(4), 10, 64)
		h.saved(ctx, item, arg(2), arg(3), seq)
	case "reset":
		h.reset(item, arg(2))
	case "compact":
		upto, _ := strconv.ParseInt(arg(3), 10, 64)
		if upto > 0 {
			f := &frame{event: "compact", data: mustJSON(map[string]any{"upto": upto})}
			for _, st := range h.streamsOf(item) {
				if st.client == arg(2) {
					st.push(f)
				}
			}
		}
	}
}

func (h *hub) saved(ctx context.Context, item ids.UUID, client, etag string, seq int64) {
	streams := h.streamsOf(item)
	if len(streams) == 0 {
		return
	}
	h.mu.Lock()
	name, ok := h.names[item][client]
	h.mu.Unlock()
	if !ok {
		name = h.s.clientName(ctx, client)
	}
	f := &frame{event: "saved", data: mustJSON(map[string]any{"etag": etag, "client": client, "name": name, "seq": seq})}
	for _, st := range streams {
		st.push(f)
	}
}

func (h *hub) reset(item ids.UUID, reason string) {
	f := &frame{event: "reset", data: mustJSON(map[string]any{"reason": reason}), final: true}
	h.mu.Lock()
	h.dropFeed(item)
	delete(h.names, item)
	h.mu.Unlock()
	for _, st := range h.streamsOf(item) {
		st.push(f)
	}
}

func (s *Service) clientName(ctx context.Context, client string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var name string
	_ = s.o.DB.View(ctx, func(tx store.Tx) error {
		c, err := tx.Collab().GetClient(ctx, client)
		name = c.Name
		return err
	})
	return name
}

// loop refreshes what changed: it reads each changed feed and presence list once and hands the result to the
// item's streams.
func (h *hub) loop(ctx context.Context) {
	resync := time.NewTicker(resyncEvery)
	defer resync.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-resync.C:
			h.markAll()
		case <-h.kick:
		}
		h.dmu.Lock()
		all, upd, pres := h.dirtyAll, h.dirtyUpd, h.dirtyPres
		h.dirtyAll, h.dirtyUpd, h.dirtyPres = false, map[ids.UUID]struct{}{}, map[ids.UUID]struct{}{}
		h.dmu.Unlock()
		if all {
			h.mu.Lock()
			for it := range h.byItem {
				upd[it], pres[it] = struct{}{}, struct{}{}
			}
			h.mu.Unlock()
		}
		for it := range upd {
			h.refreshFeed(ctx, it)
		}
		for it := range pres {
			h.refreshPresence(ctx, it)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(minGap):
		}
	}
}

func (h *hub) refreshFeed(ctx context.Context, item ids.UUID) {
	h.mu.Lock()
	f := h.feeds[item]
	h.mu.Unlock()
	if f == nil {
		return
	}
	for {
		ups, err := h.s.readUpdates(ctx, item, f.headSeq(), 500)
		if err != nil {
			if ctx.Err() == nil {
				h.s.o.Log.Warn("collab: read updates", "item", item, "err", err)
			}
			break
		}
		if len(ups) == 0 {
			break
		}
		h.feedBytes.Add(int64(f.add(ups)))
		h.enforceBudget()
	}
	for _, st := range h.streamsOf(item) {
		st.poke()
	}
}

func (h *hub) refreshPresence(ctx context.Context, item ids.UUID) {
	streams := h.streamsOf(item)
	if len(streams) == 0 {
		return
	}
	var clients []store.CollabClient
	err := h.s.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		clients, err = tx.Collab().Clients(ctx, item)
		return err
	})
	if err != nil {
		if ctx.Err() == nil {
			h.s.o.Log.Warn("collab: read clients", "item", item, "err", err)
		}
		return
	}
	names := make(map[string]string, len(clients))
	for _, c := range clients {
		names[c.Client] = c.Name
	}
	h.mu.Lock()
	if _, ok := h.byItem[item]; ok {
		// A copy: add() writes into the hub's map under the lock while this function keeps reading its own.
		h.names[item] = maps.Clone(names)
	}
	h.mu.Unlock()
	f := &frame{event: "presence", data: mustJSON(map[string]any{"clients": participants(clients)})}
	gone := &frame{event: "reset", data: mustJSON(map[string]any{"reason": "expired"}), final: true}
	for _, st := range streams {
		if _, ok := names[st.client]; ok {
			st.push(f)
		} else {
			st.push(gone)
		}
	}
}

// readUpdates reads a page of the log from the store.
func (s *Service) readUpdates(ctx context.Context, item ids.UUID, after int64, limit int) ([]store.CollabUpdate, error) {
	var out []store.CollabUpdate
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		out, err = tx.Collab().UpdatesSince(ctx, item, after, limit)
		return err
	})
	return out, err
}

// pull returns the updates after the stream's cursor: from the item's feed, or the store when the stream is
// further behind than the feed reaches.
func (h *hub) pull(ctx context.Context, st *stream) ([]store.CollabUpdate, error) {
	h.mu.Lock()
	f := h.feeds[st.item]
	h.mu.Unlock()
	if f != nil {
		if ups, ok := f.since(st.last, pullBatch); ok {
			return ups, nil
		}
	}
	return h.s.readUpdates(ctx, st.item, st.last, pullBatch)
}

func updateFrame(u store.CollabUpdate) *frame {
	m := map[string]any{"seq": u.Seq, "data": base64.StdEncoding.EncodeToString(u.Data), "client": u.Client}
	if len(u.Init) > 0 {
		m["init"] = u.Init
	}
	return &frame{event: "update", id: u.Seq, data: mustJSON(m)}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
