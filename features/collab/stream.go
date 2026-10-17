package collab

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/store"
)

// events serves GET …/events: the item's session as a Server-Sent Events stream for one joined client.
//
// Updates after the cursor are replayed first: the Last-Event-ID header a reconnecting browser sends, else the
// since parameter (the seq of the join answer), else the log's head. A client's own updates are not echoed.
func (s *Service) events(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	fail := func(err error) { manage.Fail(w, r, err) }
	if !s.hub.isStarted() {
		fail(&manage.Error{Status: http.StatusServiceUnavailable, Code: "unavailable", Message: "collaboration events are not available"})
		return
	}
	item, err := itemID(r)
	if err != nil {
		fail(err)
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	client := q.Get("client")
	if !clientRE.MatchString(client) {
		fail(manage.BadRequest("client is required"))
		return
	}
	var (
		cl   store.CollabClient
		head int64
	)
	err = s.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		if cl, err = s.own(ctx, tx, item, client); err != nil {
			return err
		}
		head, err = tx.Collab().LastSeq(ctx, item)
		return err
	})
	if err != nil {
		fail(err)
		return
	}
	if ok, err := s.o.Content.CanRead(ctx, item); err != nil || !ok {
		if err == nil {
			err = manage.NotFound("item not found")
		}
		fail(err)
		return
	}
	cursor := head
	for _, v := range []string{r.Header.Get("Last-Event-ID"), q.Get("since")} {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			cursor = n
			break
		}
	}
	st := newStream(item, client, cl.Principal, cursor)
	if err := s.hub.add(st, head, cl.Name); err != nil {
		h.Set("Retry-After", "30")
		fail(&manage.Error{Status: http.StatusTooManyRequests, Code: "too_many_streams",
			Message: "too many open collaboration streams; close one and retry"})
		return
	}
	defer s.hub.remove(st)
	h.Set("Content-Type", "text/event-stream")
	h.Set("X-Accel-Buffering", "no")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if rc.Flush() != nil {
		return
	}
	write := func(format string, args ...any) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if _, err := fmt.Fprintf(w, format, args...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	send := func(f *frame) bool {
		if f.id > 0 {
			return write("event: %s\nid: %d\ndata: %s\n\n", f.event, f.id, f.data)
		}
		return write("event: %s\ndata: %s\n\n", f.event, f.data)
	}
	// flush delivers what is due; false ends the stream.
	flush := func() bool {
		ctrl := st.take()
		if n := len(ctrl); n == 0 || !ctrl[n-1].final {
			for {
				ups, err := s.hub.pull(ctx, st)
				if err != nil {
					if ctx.Err() == nil {
						s.o.Log.Warn("collab: pull updates", "item", item, "err", err)
					}
					break
				}
				if len(ups) == 0 {
					break
				}
				for _, u := range ups {
					st.last = u.Seq
					if u.Client != st.client && !send(updateFrame(u)) {
						return false
					}
				}
			}
		}
		for _, f := range ctrl {
			if f.skip || !send(f) || f.final {
				return false
			}
		}
		return true
	}
	s.hub.markPresence(item)
	st.poke()
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-st.wake:
			if !flush() {
				return
			}
		case <-ping.C:
			if !write(": ping\n\n") {
				return
			}
		}
	}
}
