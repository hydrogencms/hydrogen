package collab

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Update is one entry of a session's log as clients see it.
type Update struct {
	Seq    int64    `json:"seq"`
	Data   []byte   `json:"data"` // base64 in JSON
	Client string   `json:"client"`
	Init   []string `json:"init,omitempty"`
}

func updateOf(u store.CollabUpdate) Update {
	return Update{Seq: u.Seq, Data: u.Data, Client: u.Client, Init: u.Init}
}

// JoinInput is the body of a join.
type JoinInput struct {
	Locale string `json:"locale"`
	Edit   bool   `json:"edit"`
	// Client is the id of a client that re-joins (after a gap or a reset); the server chooses one otherwise.
	Client string `json:"client"`
}

// Joined is the answer to a join.
type Joined struct {
	Client  string `json:"client"`
	Session string `json:"session"`
	// Seq is the highest sequence number of Updates; with More it is the cursor to fetch the next page from.
	Seq     int64         `json:"seq"`
	Updates []Update      `json:"updates"`
	More    bool          `json:"more"`
	Init    []string      `json:"init"`
	Pending []string      `json:"pending"`
	Present []Participant `json:"presence"`
	Etag    string        `json:"etag"`
}

func errUnknownClient() *manage.Error {
	return &manage.Error{Status: http.StatusNotFound, Code: "unknown_client",
		Message: "this client is not in the session (it was dropped): join again"}
}

func forbidden(policy string) error { return fmt.Errorf("%w: %s", access.ErrForbidden, policy) }

func partsOf(locale string) []string { return []string{SharedPart, "l:" + locale} }

func validParts(parts []string, max int) error {
	if len(parts) > max {
		return manage.BadRequest("too many parts")
	}
	for _, p := range parts {
		l, ok := strings.CutPrefix(p, "l:")
		if p != SharedPart && (!ok || !localeRE.MatchString(l)) {
			return manage.BadRequest("part %q must be shared or l:<locale>", p)
		}
	}
	return nil
}

// newer reports whether the working state a is later than b (both content.StateOf values). States that do not
// parse are compared for difference only.
func newer(a, b string) bool {
	ta, ea := time.Parse(time.RFC3339Nano, a)
	tb, eb := time.Parse(time.RFC3339Nano, b)
	if ea != nil || eb != nil {
		return a != b
	}
	return ta.After(tb)
}

// page cuts a log read (fetched with one extra update) to a page of at most pageUpdates updates and pageBytes
// bytes, but at least one update; more reports that updates were left out.
func page(ups []store.CollabUpdate) (out []Update, more bool) {
	size := 0
	for i, u := range ups {
		if i == PageUpdates || (i > 0 && size+len(u.Data) > PageBytes) {
			return out, true
		}
		size += len(u.Data)
		out = append(out, updateOf(u))
	}
	return out, false
}

// pageOf reads the log after a seq as a page: the updates, the cursor to continue from and whether more follow.
func pageOf(ctx context.Context, c store.CollabStore, item ids.UUID, after int64) ([]Update, int64, bool, error) {
	ups, err := c.UpdatesSince(ctx, item, after, PageUpdates+1)
	if err != nil {
		return nil, 0, false, err
	}
	out, more := page(ups)
	if out == nil {
		out = []Update{}
	}
	if more {
		return out, out[len(out)-1].Seq, true, nil
	}
	head, err := c.LastSeq(ctx, item)
	if err != nil {
		return nil, 0, false, err
	}
	if n := len(out); n > 0 {
		head = max(head, out[n-1].Seq)
	}
	return out, max(head, after), false, nil
}

// Join puts the caller in the item's session. It needs read access to the item, and edit access to the locale
// when edit is asked. An editing client is told which parts of the shared document it must seed (claimed for it
// alone) and which another client is seeding. A session seeded from an older draft than the current one is
// dropped first (its clients are told to reset), so a new session never mixes drafts.
func (s *Service) Join(ctx context.Context, item ids.UUID, in JoinInput) (*Joined, error) {
	if !localeRE.MatchString(in.Locale) {
		return nil, manage.BadRequest("locale %q is not a locale code", in.Locale)
	}
	if in.Client != "" && !clientRE.MatchString(in.Client) {
		return nil, manage.BadRequest("client must be 8-64 letters, digits, _ or -")
	}
	v, err := s.o.Content.Get(ctx, item, content.GetOptions{Locale: in.Locale, Draft: true})
	if err != nil {
		return nil, err
	}
	if in.Edit && v.Actions != nil && !v.Actions.Edit {
		return nil, forbidden(access.PolicyContentEdit)
	}
	principal, name := who(ctx)
	client := in.Client
	if client == "" {
		client = s.o.IDs.New().String()
	}
	state := content.StateOf(v.UpdatedAt)
	out := &Joined{Client: client, Session: item.String(), Etag: Etag(v.UpdatedAt), Init: []string{}, Pending: []string{}}
	now := s.o.Clock.Now()
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		// Others see a user's display name, not the login the principal carries.
		if p := access.PrincipalFrom(ctx); p != nil && p.Kind == access.KindUser {
			if u, err := tx.Auth().GetUser(ctx, p.ID); err == nil {
				name = cmp.Or(u.Name, u.Login)
			}
		}
		// Joins of one item are serialized: the client limit and the claims are decided one at a time.
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		sess, created, err := c.EnsureSession(ctx, item, state, now)
		if err != nil {
			return err
		}
		if !created && newer(state, sess.Etag) {
			if err := c.DeleteSession(ctx, item); err != nil {
				return err
			}
			if err := notify(tx, item, "reset", "external"); err != nil {
				return err
			}
			if _, _, err = c.EnsureSession(ctx, item, state, now); err != nil {
				return err
			}
		}
		cur, err := c.GetClient(ctx, client)
		switch {
		case err == nil && (cur.Item != item || cur.Principal != principal):
			return manage.Conflict("client_taken", "this client id belongs to another session")
		case err != nil && !errors.Is(err, store.ErrNotFound):
			return err
		}
		clients, err := c.Clients(ctx, item)
		if err != nil {
			return err
		}
		if len(clients) >= MaxClients && !slices.ContainsFunc(clients, func(x store.CollabClient) bool { return x.Client == client }) {
			return &manage.Error{Status: http.StatusTooManyRequests, Code: "session_full",
				Message: fmt.Sprintf("this item already has %d editors", MaxClients)}
		}
		if err := c.PutClient(ctx, store.CollabClient{Client: client, Item: item, Principal: principal, Name: name,
			Locale: in.Locale, Edit: in.Edit, Color: colorOf(principal), JoinedAt: now, SeenAt: now}); err != nil {
			return err
		}
		parts := partsOf(in.Locale)
		if in.Edit {
			cl, err := c.ClaimParts(ctx, item, parts, client, now, ClaimExpiry)
			if err != nil {
				return err
			}
			out.Init, out.Pending = nonNil(cl.Claimed), nonNil(cl.Pending)
		} else {
			rows, err := c.Parts(ctx, item)
			if err != nil {
				return err
			}
			for _, p := range parts {
				if !slices.ContainsFunc(rows, func(r store.CollabPart) bool { return r.Part == p && r.Seeded }) {
					out.Pending = append(out.Pending, p)
				}
			}
		}
		if out.Updates, out.Seq, out.More, err = pageOf(ctx, c, item, 0); err != nil {
			return err
		}
		if clients, err = c.Clients(ctx, item); err != nil {
			return err
		}
		out.Present = participants(clients)
		return notify(tx, item, "presence")
	})
	if err != nil {
		return nil, err
	}
	s.markChecked(client, now)
	return out, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// own returns the client row of an id, which must belong to the caller and the item.
func (s *Service) own(ctx context.Context, tx store.Tx, item ids.UUID, client string) (store.CollabClient, error) {
	cl, err := tx.Collab().GetClient(ctx, client)
	if errors.Is(err, store.ErrNotFound) {
		return cl, errUnknownClient()
	}
	if err != nil {
		return cl, err
	}
	if principal, _ := who(ctx); cl.Item != item || cl.Principal != principal {
		return cl, errUnknownClient()
	}
	return cl, nil
}

// load reads the caller's client row.
func (s *Service) load(ctx context.Context, item ids.UUID, client string) (cl store.CollabClient, err error) {
	err = s.o.DB.View(ctx, func(tx store.Tx) (err error) {
		cl, err = s.own(ctx, tx, item, client)
		return err
	})
	return cl, err
}

// rights reports what the caller may do with the item in a locale: read it, and (when edit) edit it. It queries
// through the pool, so call it outside transactions.
func (s *Service) rights(ctx context.Context, item ids.UUID, locale string, edit bool) (read, canEdit bool, err error) {
	if !edit {
		ok, err := s.o.Content.CanRead(ctx, item)
		if errors.Is(err, content.ErrNotFound) || errors.Is(err, access.ErrForbidden) {
			return false, false, nil
		}
		return ok, false, err
	}
	v, err := s.o.Content.Get(ctx, item, content.GetOptions{Locale: locale, Draft: true})
	if errors.Is(err, content.ErrNotFound) || errors.Is(err, access.ErrForbidden) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, v.Actions == nil || v.Actions.Edit, nil
}

// markChecked, dueCheck and forget keep when a client's rights were last checked (per node).
func (s *Service) markChecked(client string, now time.Time) {
	s.mu.Lock()
	s.checked[client] = now
	s.mu.Unlock()
}

func (s *Service) dueCheck(client string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	at, ok := s.checked[client]
	return !ok || now.Sub(at) >= RightsRecheck
}

func (s *Service) forget(client string) {
	s.mu.Lock()
	delete(s.checked, client)
	delete(s.buckets, client)
	s.mu.Unlock()
}

// verify re-checks the rights of a client whose last check is older than RightsRecheck: a client that lost
// read access to the item is dropped (its stream is told to reset, "expired", and the caller gets 404
// unknown_client), one that lost edit access to its locale becomes a viewer (its updates get 403 read_only).
func (s *Service) verify(ctx context.Context, cl store.CollabClient) error {
	now := s.o.Clock.Now()
	if !s.dueCheck(cl.Client, now) {
		return nil
	}
	read, edit, err := s.rights(ctx, cl.Item, cl.Locale, cl.Edit)
	if err != nil {
		return err
	}
	s.markChecked(cl.Client, now)
	if read && (edit || !cl.Edit) {
		return nil
	}
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if !read {
			if err := c.RemoveClient(ctx, cl.Item, cl.Client); err != nil {
				return err
			}
		} else {
			if err := c.SetClientEdit(ctx, cl.Client, false); err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			if err := c.ReleaseClaims(ctx, cl.Item, cl.Client); err != nil {
				return err
			}
		}
		return notify(tx, cl.Item, "presence")
	})
	if err != nil {
		return err
	}
	if !read {
		s.forget(cl.Client)
		return errUnknownClient()
	}
	return nil
}

// UpdateInput posts an update.
type UpdateInput struct {
	Client string   `json:"client"`
	Data   []byte   `json:"data"` // base64 in JSON
	Init   []string `json:"init"`
}

var errCompactRequired = &manage.Error{Status: http.StatusConflict, Code: "compact_required",
	Message: "the session's log is full: a compaction was requested, retry once it is done"}

// PostUpdate appends an update to the log and tells the other participants. An update with init finishes the
// seeding of those parts, which the client must still hold the claim of (409 claim_lost otherwise: join again).
// Only editing clients may post, at most RateBurst updates at once and RateSustained per second (429
// rate_limited), and not into a log past the hard cap (409 compact_required).
func (s *Service) PostUpdate(ctx context.Context, item ids.UUID, in UpdateInput) (int64, error) {
	switch {
	case len(in.Data) == 0:
		return 0, manage.BadRequest("data is empty")
	case len(in.Data) > MaxUpdateBytes:
		return 0, &manage.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("an update may be at most %d KB", MaxUpdateBytes>>10)}
	}
	if err := validParts(in.Init, maxParts); err != nil {
		return 0, err
	}
	in.Init = slices.Compact(slices.Sorted(slices.Values(in.Init)))
	cl, err := s.load(ctx, item, in.Client)
	if err != nil {
		return 0, err
	}
	if wait := s.take(in.Client, s.o.Clock.Now()); wait > 0 {
		return 0, &manage.Error{Status: http.StatusTooManyRequests, Code: "rate_limited", RetryAfter: wait,
			Message: "too many updates: slow down"}
	}
	if err := s.verify(ctx, cl); err != nil {
		return 0, err
	}
	now := s.o.Clock.Now()
	var seq int64
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		// The item's lock comes first, as in DeleteSession: seeding marks a part row and a reset deletes it.
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		cl, err := s.own(ctx, tx, item, in.Client)
		if err != nil {
			return err
		}
		if !cl.Edit {
			return &manage.Error{Status: http.StatusForbidden, Code: "read_only", Message: "this client joined as a viewer and cannot post updates"}
		}
		if n, size, err := c.LogSize(ctx, item); err != nil {
			return err
		} else if n >= HardCapCount || size+int64(len(in.Data)) > HardCapBytes {
			return errCompactRequired
		}
		if len(in.Init) > 0 {
			done, err := c.MarkSeeded(ctx, item, in.Init, in.Client)
			if err != nil {
				return err
			}
			if len(done) != len(in.Init) {
				return manage.Conflict("claim_lost", "the claim to seed these parts expired or went to another client: join again")
			}
		}
		if cl.Idle {
			if err := c.TouchClient(ctx, in.Client, cl.Locale, cl.Focus, false, now); err != nil {
				return err
			}
			if err := notify(tx, item, "presence"); err != nil {
				return err
			}
		}
		if seq, err = c.AppendUpdate(ctx, store.CollabUpdate{Item: item, Client: in.Client, Data: in.Data, Init: in.Init, At: now}); err != nil {
			return err
		}
		return notify(tx, item, "update", fmt.Sprint(seq))
	})
	if errors.Is(err, errCompactRequired) {
		if rerr := s.requestCompaction(ctx, item, true); rerr != nil {
			s.o.Log.Warn("collab: compaction request", "item", item, "err", rerr)
		}
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	s.maybeCompact(ctx, item)
	return seq, nil
}

// take spends one update of the client's budget and returns how long to wait when there is none left.
func (s *Service) take(client string, now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.buckets[client]
	if b == nil {
		b = &bucket{tokens: RateBurst, at: now}
		s.buckets[client] = b
	}
	b.tokens = min(RateBurst, b.tokens+now.Sub(b.at).Seconds()*RateSustained)
	b.at = now
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) / RateSustained * float64(time.Second))
	}
	b.tokens--
	return 0
}

type bucket struct {
	tokens float64
	at     time.Time
}

// Page is a page of the log after a seq.
type Page struct {
	Updates []Update `json:"updates"`
	Seq     int64    `json:"seq"`
	More    bool     `json:"more"`
}

// Updates reads the log after a seq as a page (updates, the seq to continue from, and whether more follow). It
// is for the client that joined with More, and for catching up after a gap.
func (s *Service) Updates(ctx context.Context, item ids.UUID, client string, since int64) (*Page, error) {
	cl, err := s.load(ctx, item, client)
	if err != nil {
		return nil, err
	}
	if err := s.verify(ctx, cl); err != nil {
		return nil, err
	}
	p := &Page{}
	err = s.o.DB.View(ctx, func(tx store.Tx) (err error) {
		p.Updates, p.Seq, p.More, err = pageOf(ctx, tx.Collab(), item, max(since, 0))
		return err
	})
	return p, err
}

// Reset drops the item's whole session (log, claims and clients) and tells every client to reset (external): the
// way out for a client that cannot apply the log (a corrupt update). Editing clients only.
func (s *Service) Reset(ctx context.Context, item ids.UUID, client string) error {
	cl, err := s.load(ctx, item, client)
	if err != nil {
		return err
	}
	if err := s.verify(ctx, cl); err != nil {
		return err
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		if cl, err = s.own(ctx, tx, item, client); err != nil {
			return err
		}
		if !cl.Edit {
			return &manage.Error{Status: http.StatusForbidden, Code: "read_only", Message: "this client joined as a viewer and cannot reset the session"}
		}
		if err := c.DeleteSession(ctx, item); err != nil {
			return err
		}
		return notify(tx, item, "reset", "external")
	})
}

// CompactInput replaces the log up to a sequence number by one state update.
type CompactInput struct {
	Client string `json:"client"`
	Data   []byte `json:"data"`
	Upto   int64  `json:"upto"`
}

// Compact replaces the updates with seq <= upto by the client's state, which must include them all. Only the
// client the server asked (a compact event) may compact, up to the seq it was asked for: 409
// compact_not_requested otherwise. It returns the sequence number the state took (0 when there was nothing to
// compact).
func (s *Service) Compact(ctx context.Context, item ids.UUID, in CompactInput) (int64, error) {
	switch {
	case len(in.Data) == 0 || in.Upto <= 0:
		return 0, manage.BadRequest("data and upto are required")
	case len(in.Data) > MaxCompactBytes:
		return 0, &manage.Error{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
			Message: fmt.Sprintf("a compacted state may be at most %d KB", MaxCompactBytes>>10)}
	}
	cl, err := s.load(ctx, item, in.Client)
	if err != nil {
		return 0, err
	}
	if err := s.verify(ctx, cl); err != nil {
		return 0, err
	}
	var got int64
	err = s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		cl, err := s.own(ctx, tx, item, in.Client)
		if err != nil {
			return err
		}
		if !cl.Edit {
			return &manage.Error{Status: http.StatusForbidden, Code: "read_only", Message: "this client joined as a viewer and cannot compact"}
		}
		sess, err := c.GetSession(ctx, item)
		if errors.Is(err, store.ErrNotFound) {
			return manage.Conflict("compact_not_requested", "no compaction was requested from this client")
		}
		if err != nil {
			return err
		}
		if sess.CompactClient != in.Client || in.Upto > sess.CompactUpto || s.o.Clock.Now().Sub(sess.CompactAt) > CompactWindow {
			return manage.Conflict("compact_not_requested", "no compaction was requested from this client (or not that far)")
		}
		last, err := c.LastSeq(ctx, item)
		if err != nil {
			return err
		}
		if in.Upto > last {
			return manage.BadRequest("upto %d is beyond the log (%d)", in.Upto, last)
		}
		if got, err = c.Compact(ctx, item, in.Upto, in.Data, in.Client, s.o.Clock.Now()); err != nil {
			return err
		}
		sess.CompactClient, sess.CompactUpto = "", 0
		return c.UpdateSession(ctx, sess)
	})
	return got, err
}

// PresenceInput is a heartbeat.
type PresenceInput struct {
	Client string `json:"client"`
	Locale string `json:"locale"`
	Focus  string `json:"focus"`
	Idle   bool   `json:"idle"`
}

// Presence records a heartbeat and the client's locale, focused field and idleness; the others are told only
// when one of them changed. A new locale must be one the client may read (and edit, for an editing client): 403
// otherwise. The heartbeat also re-checks the client's rights (see verify).
func (s *Service) Presence(ctx context.Context, item ids.UUID, in PresenceInput) error {
	if !localeRE.MatchString(in.Locale) {
		return manage.BadRequest("locale %q is not a locale code", in.Locale)
	}
	if len(in.Focus) > maxFocus {
		return manage.BadRequest("focus is longer than %d characters", maxFocus)
	}
	cl, err := s.load(ctx, item, in.Client)
	if err != nil {
		return err
	}
	if in.Locale != cl.Locale {
		read, edit, err := s.rights(ctx, item, in.Locale, cl.Edit)
		switch {
		case err != nil:
			return err
		case !read:
			return forbidden(access.PolicyContentRead)
		case cl.Edit && !edit:
			return forbidden(access.PolicyContentEdit)
		}
	}
	if err := s.verify(ctx, cl); err != nil {
		return err
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		cl, err := s.own(ctx, tx, item, in.Client)
		if err != nil {
			return err
		}
		if err := tx.Collab().TouchClient(ctx, in.Client, in.Locale, in.Focus, in.Idle, s.o.Clock.Now()); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return errUnknownClient()
			}
			return err
		}
		if cl.Locale != in.Locale || cl.Focus != in.Focus || cl.Idle != in.Idle {
			return notify(tx, item, "presence")
		}
		return nil
	})
}

// Leave removes the caller's client (idempotent: an unknown client is fine).
func (s *Service) Leave(ctx context.Context, item ids.UUID, client string) error {
	defer s.forget(client)
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		if _, err := s.own(ctx, tx, item, client); err != nil {
			var me *manage.Error
			if errors.As(err, &me) && me.Code == "unknown_client" {
				return nil
			}
			return err
		}
		if err := c.RemoveClient(ctx, item, client); err != nil {
			return err
		}
		// The last one out ends the session: saving is manual, so unsaved changes go with the people who left
		// them, as when a single editor closes without saving (a crashed tab still waits for the stale timeout).
		left, err := c.Clients(ctx, item)
		if err != nil {
			return err
		}
		if len(left) == 0 {
			return c.DeleteSession(ctx, item)
		}
		return notify(tx, item, "presence")
	})
}

// maybeCompact asks the most recently active editing client to compact a long log, checking at most every 10
// seconds per item.
func (s *Service) maybeCompact(ctx context.Context, item ids.UUID) {
	now := s.o.Clock.Now()
	s.mu.Lock()
	if now.Sub(s.lastCheck[item]) < 10*time.Second {
		s.mu.Unlock()
		return
	}
	s.lastCheck[item] = now
	s.mu.Unlock()
	if err := s.requestCompaction(ctx, item, false); err != nil {
		s.o.Log.Warn("collab: compaction request", "item", item, "err", err)
	}
}

// requestCompaction records and broadcasts a compaction request to the most recently active editing client
// when the log passed the soft limits (or always, when forced by the hard cap), at most every 30 seconds per
// item (5 when forced). Only the client asked may compact (Compact).
func (s *Service) requestCompaction(ctx context.Context, item ids.UUID, force bool) error {
	now := s.o.Clock.Now()
	gap := 30 * time.Second
	if force {
		gap = 5 * time.Second
	}
	return s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		if err := c.LockSession(ctx, item); err != nil {
			return err
		}
		n, size, err := c.LogSize(ctx, item)
		if err != nil || (!force && n <= CompactCount && size <= CompactBytes) {
			return err
		}
		sess, err := c.GetSession(ctx, item)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil || (sess.CompactClient != "" && now.Sub(sess.CompactAt) < gap) {
			return err
		}
		clients, err := c.Clients(ctx, item)
		if err != nil {
			return err
		}
		var pick *store.CollabClient
		for i := range clients {
			if clients[i].Edit && (pick == nil || clients[i].SeenAt.After(pick.SeenAt)) {
				pick = &clients[i]
			}
		}
		if pick == nil {
			return nil
		}
		last, err := c.LastSeq(ctx, item)
		if err != nil {
			return err
		}
		sess.CompactClient, sess.CompactUpto, sess.CompactAt = pick.Client, last, now
		if err := c.UpdateSession(ctx, sess); err != nil {
			return err
		}
		return notify(tx, item, "compact", pick.Client, fmt.Sprint(last))
	})
}

// Cleanup drops clients without a heartbeat, releases expired claims and drops sessions that have been empty
// for a while. It is the collab.cleanup job.
func (s *Service) Cleanup(ctx context.Context) error {
	now := s.o.Clock.Now()
	err := s.o.DB.Tx(ctx, func(tx store.Tx) error {
		c := tx.Collab()
		stale, err := c.RemoveStaleClients(ctx, now.Add(-ClientTimeout))
		if err != nil {
			return err
		}
		expired, err := c.ReleaseExpiredClaims(ctx, now.Add(-ClaimExpiry))
		if err != nil {
			return err
		}
		for _, it := range slices.Concat(stale, expired) {
			if err := notify(tx, it, "presence"); err != nil {
				return err
			}
		}
		_, err = c.DropIdleSessions(ctx, now.Add(-SessionIdle))
		return err
	})
	s.mu.Lock()
	for it, at := range s.lastCheck {
		if now.Sub(at) > time.Minute {
			delete(s.lastCheck, it)
		}
	}
	for cl, at := range s.checked {
		if now.Sub(at) > 2*ClientTimeout {
			delete(s.checked, cl)
			delete(s.buckets, cl)
		}
	}
	s.mu.Unlock()
	return err
}
