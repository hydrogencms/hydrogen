package delivery

import (
	"cmp"
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/store"
)

// Share links: a share token (SharePrefix plus 32 random bytes) opens one item's preview for anyone who holds
// it, until it expires or is revoked. Only SHA-256(SHA-256(token)) is stored; frontends publish
// SHA-256(token) as the preview proof, so the event stream can look a session up from the proof alone.
const (
	SharePrefix = "hps_"
	// DefaultShareTTL and MaxShareTTL are the default lifetime of a share link and the default cap.
	DefaultShareTTL = 24 * time.Hour
	MaxShareTTL     = 7 * 24 * time.Hour
	// ShareTTLLimit is the longest cap delivery.share_max_ttl may name.
	ShareTTLLimit = 30 * 24 * time.Hour
	// RelayTTL is how long a relayed draft applies after its last update.
	RelayTTL = time.Hour

	shareCacheTTL = 5 * time.Second
	shareCacheMax = 4096
	// shareCacheNegMax bounds the entries for tokens that matched no share.
	shareCacheNegMax = 256
)

type claimsKey struct{}

type shareEntry struct {
	s  store.ShareSession
	ok bool
	at time.Time
}

type lruItem struct {
	k [32]byte
	e shareEntry
}

// lru is a bounded map that drops its least recently used entry when full. Not safe for concurrent use.
type lru struct {
	max int
	l   *list.List
	m   map[[32]byte]*list.Element
}

func (c *lru) get(k [32]byte) (shareEntry, bool) {
	el, ok := c.m[k]
	if !ok {
		return shareEntry{}, false
	}
	c.l.MoveToFront(el)
	return el.Value.(*lruItem).e, true
}

func (c *lru) remove(k [32]byte) {
	if el, ok := c.m[k]; ok {
		c.l.Remove(el)
		delete(c.m, k)
	}
}

func (c *lru) put(k [32]byte, e shareEntry) {
	if c.m == nil {
		c.l, c.m = list.New(), map[[32]byte]*list.Element{}
	}
	if el, ok := c.m[k]; ok {
		el.Value.(*lruItem).e = e
		c.l.MoveToFront(el)
		return
	}
	for len(c.m) >= c.max {
		last := c.l.Back()
		delete(c.m, last.Value.(*lruItem).k)
		c.l.Remove(last)
	}
	c.m[k] = c.l.PushFront(&lruItem{k, e})
}

// shareCache remembers share lookups for a few seconds, so a revoke reaches every node quickly and a flood of
// requests with one token costs one query per interval. Found and unknown tokens are kept apart, each in a
// bounded LRU, so a flood of random tokens only evicts other unknown ones.
type shareCache struct {
	mu       sync.Mutex
	pos, neg lru
}

func (c *shareCache) get(h [32]byte, now time.Time) (shareEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range []*lru{&c.pos, &c.neg} {
		e, ok := m.get(h)
		if !ok {
			continue
		}
		if now.Sub(e.at) >= shareCacheTTL || now.Before(e.at) {
			m.remove(h)
			return shareEntry{}, false
		}
		return e, true
	}
	return shareEntry{}, false
}

func (c *shareCache) put(h [32]byte, e shareEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pos.max, c.neg.max = shareCacheMax, shareCacheNegMax
	if e.ok {
		c.pos.put(h, e)
	} else {
		c.neg.put(h, e)
	}
}

func (c *shareCache) clear() {
	c.mu.Lock()
	c.pos, c.neg = lru{}, lru{}
	c.mu.Unlock()
}

func shareHash(proof [32]byte) [32]byte { return sha256.Sum256(proof[:]) }

// lookupShare finds a session by its stored hash, through the short-lived cache.
func (a *API) lookupShare(ctx context.Context, hash [32]byte) (store.ShareSession, bool, error) {
	now := a.o.Clock.Now()
	if e, ok := a.shares.get(hash, now); ok {
		return e.s, e.ok, nil
	}
	var s store.ShareSession
	err := a.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		s, err = tx.Preview().ShareByHash(ctx, hash[:])
		return err
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		a.shares.put(hash, shareEntry{at: now})
		return s, false, nil
	case err != nil:
		return s, false, err
	}
	a.shares.put(hash, shareEntry{s: s, ok: true, at: now})
	return s, true, nil
}

// shareClaims turns a session into the claims of a preview request, or errPreviewToken when it is not usable.
func (a *API) shareClaims(s store.ShareSession) (*claims, error) {
	if !a.o.ShareLinks || s.RevokedAt != nil || !a.o.Clock.Now().Before(s.ExpiresAt) {
		return nil, errPreviewToken
	}
	return &claims{Channel: s.Channel, Item: s.Item.String(), Exp: s.ExpiresAt.Unix(), share: s.ID.String()}, nil
}

// verifyShare accepts a share token.
func (a *API) verifyShare(ctx context.Context, tok string) (*claims, error) {
	if a.o.DB == nil || !a.o.ShareLinks {
		return nil, errPreviewToken
	}
	s, ok, err := a.lookupShare(ctx, shareHash(sha256.Sum256([]byte(tok))))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errPreviewToken
	}
	return a.shareClaims(s)
}

// verifyProof finds the session whose token has the given SHA-256 (hex).
func (a *API) verifyProof(ctx context.Context, proof string) (store.ShareSession, *claims, error) {
	var raw [32]byte
	b, err := hex.DecodeString(proof)
	if err != nil || len(b) != len(raw) || a.o.DB == nil || !a.o.ShareLinks {
		return store.ShareSession{}, nil, errPreviewToken
	}
	copy(raw[:], b)
	s, ok, err := a.lookupShare(ctx, shareHash(raw))
	if err != nil {
		return s, nil, err
	}
	if !ok {
		return s, nil, errPreviewToken
	}
	cl, err := a.shareClaims(s)
	return s, cl, err
}

// previewFailed answers a request whose preview token did not verify.
func (a *API) previewFailed(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, errPreviewToken) {
		a.plainError(w, unauthorized("invalid or expired preview token"))
		return
	}
	a.log.Error("delivery: preview token lookup failed", "err", err)
	a.plainError(w, &manage.Error{Status: http.StatusInternalServerError, Code: "internal", Message: "internal error"})
}

// PreviewTokenInfo checks a preview token of either kind (signed or share) and returns the channel it belongs
// to, the item it is limited to ("" for the whole channel) and when it expires. Modules that serve pages
// themselves use it to accept ?preview=.
func (a *API) PreviewTokenInfo(tok string) (chanName, item string, expires time.Time, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cl, err := a.verifyToken(ctx, tok)
	if err != nil {
		return "", "", time.Time{}, err
	}
	return cl.Channel, cl.Item, time.Unix(cl.Exp, 0).UTC(), nil
}

func shareForbidden(msg string) *manage.Error {
	return &manage.Error{Status: http.StatusForbidden, Code: "forbidden", Message: msg}
}

func (a *API) createShare(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Channel string          `json:"channel"`
		Item    string          `json:"item"`
		Locale  string          `json:"locale"`
		TTL     json.RawMessage `json:"ttl"`
	}
	if err := manage.DecodeJSON(w, r, &in); err != nil {
		manage.Fail(w, r, err)
		return
	}
	if !a.o.ShareLinks {
		manage.Fail(w, r, shareForbidden("share links are turned off (delivery.share_links)"))
		return
	}
	item, err := ids.Parse(in.Item)
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("item %q is not an item id", in.Item))
		return
	}
	ttl, err := parseTTL(in.TTL, a.o.ShareMaxTTL)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if ttl == 0 {
		ttl = min(a.o.ShareDefaultTTL, a.o.ShareMaxTTL)
	}
	ch, ok := a.o.Channels.Get(in.Channel)
	if !ok {
		manage.Fail(w, r, manage.BadRequest("unknown channel %q", in.Channel))
		return
	}
	if in.Locale != "" && !slices.Contains(ch.Languages, in.Locale) {
		manage.Fail(w, r, manage.BadRequest("channel %q does not serve locale %q", ch.Name, in.Locale))
		return
	}
	ctx := r.Context()
	if err := a.o.Content.CanPreviewItem(ctx, ch, item, true); err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := a.o.Content.Get(ctx, item, content.GetOptions{Draft: true})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if !ch.AllowsCollection(v.Type) {
		manage.Fail(w, r, manage.NotFound("%s is not available on channel %s", item, ch.Name))
		return
	}
	locale := in.Locale
	if locale == "" {
		locale = v.MainLocale
		if !slices.Contains(ch.Languages, locale) && len(ch.Languages) > 0 {
			locale = ch.Languages[0]
		}
	}
	tok, err := a.newShareToken()
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	now := a.o.Clock.Now().UTC().Truncate(time.Millisecond)
	proof := sha256.Sum256([]byte(tok))
	hash := shareHash(proof)
	s := store.ShareSession{ID: a.o.IDs.New(), TokenHash: hash[:], Item: item, Channel: ch.Name, Locale: locale,
		CreatedBy: audit.ActorFrom(ctx).String(), CreatedAt: now, ExpiresAt: now.Add(ttl)}
	err = a.o.DB.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Preview().CreateShare(ctx, s); err != nil {
			return err
		}
		return a.audit(ctx, tx, "preview.share.create", item, map[string]any{
			"share": s.ID.String(), "channel": s.Channel, "locale": s.Locale, "expiresAt": s.ExpiresAt.Format(time.RFC3339)})
	})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	out := map[string]any{"id": s.ID.String(), "token": tok, "expiresAt": s.ExpiresAt.Format(time.RFC3339),
		"channel": s.Channel, "locale": s.Locale}
	u, found, err := a.previewURL(ctx, ch, item, locale)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if found {
		out["url"] = withPreview(u, tok)
	}
	manage.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) audit(ctx context.Context, tx store.Tx, action string, item ids.UUID, data any) error {
	if a.o.Audit == nil {
		return nil
	}
	return a.o.Audit.Write(ctx, tx, audit.Entry{Actor: audit.ActorFrom(ctx), Action: action, Subject: item.String(), Data: data})
}

func (a *API) newShareToken() (string, error) {
	var b [32]byte
	if _, err := io.ReadFull(a.o.Rand, b[:]); err != nil {
		return "", err
	}
	return SharePrefix + b64.EncodeToString(b[:]), nil
}

func (a *API) listShares(w http.ResponseWriter, r *http.Request) {
	item, err := ids.Parse(r.URL.Query().Get("item"))
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("item is required: an item id"))
		return
	}
	ctx := r.Context()
	if err := a.o.Content.CanPreviewItem(ctx, nil, item, false); err != nil {
		manage.Fail(w, r, err)
		return
	}
	var list []store.ShareSession
	err = a.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		list, err = tx.Preview().ListShares(ctx, item)
		return err
	})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	names, err := a.creatorNames(ctx, list)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, s := range list {
		e := map[string]any{"id": s.ID.String(), "channel": s.Channel, "locale": s.Locale, "createdBy": s.CreatedBy,
			"createdByName": names[s.CreatedBy],
			"createdAt":     s.CreatedAt.UTC().Format(time.RFC3339), "expiresAt": s.ExpiresAt.UTC().Format(time.RFC3339),
			"viewers": a.hub.viewersOf(s.ID)}
		if s.RevokedAt != nil {
			e["revokedAt"] = s.RevokedAt.UTC().Format(time.RFC3339)
		}
		out = append(out, e)
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"shares": out})
}

// creatorNames resolves the display names of the creators of shares: a user's name (else login), an API
// token's name; "" when the creator is unknown or gone. Each lookup finishes before the next starts.
func (a *API) creatorNames(ctx context.Context, list []store.ShareSession) (map[string]string, error) {
	names := map[string]string{}
	err := a.o.DB.View(ctx, func(tx store.Tx) error {
		for _, s := range list {
			if _, done := names[s.CreatedBy]; done {
				continue
			}
			names[s.CreatedBy] = ""
			kind, id, ok := strings.Cut(s.CreatedBy, ":")
			uid, perr := ids.Parse(id)
			if !ok || perr != nil {
				continue
			}
			switch kind {
			case "user":
				u, err := tx.Auth().GetUser(ctx, uid)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
				}
				names[s.CreatedBy] = cmp.Or(u.Name, u.Login)
			case "token", "agent":
				t, err := tx.Auth().GetToken(ctx, uid)
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return err
				}
				names[s.CreatedBy] = t.Name
			}
		}
		return nil
	})
	return names, err
}

func (a *API) revokeShare(w http.ResponseWriter, r *http.Request) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		manage.Fail(w, r, manage.NotFound("no such share link"))
		return
	}
	ctx := r.Context()
	var s store.ShareSession
	err = a.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		s, err = tx.Preview().GetShare(ctx, id)
		return err
	})
	if errors.Is(err, store.ErrNotFound) {
		manage.Fail(w, r, manage.NotFound("no such share link"))
		return
	}
	if err == nil {
		err = a.o.Content.CanPreviewItem(ctx, nil, s.Item, true)
		if errors.Is(err, content.ErrNotFound) {
			// A link to a deleted item can still be revoked by anyone holding the policy; one to an item
			// the principal may not see stays hidden.
			if a.itemGone(ctx, s.Item) {
				err = nil
			} else {
				err = manage.NotFound("no such share link")
			}
		}
	}
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	err = a.o.DB.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Preview().RevokeShare(ctx, id, a.o.Clock.Now().UTC()); err != nil {
			return err
		}
		if err := a.audit(ctx, tx, "preview.share.revoke", s.Item, map[string]any{"share": id.String()}); err != nil {
			return err
		}
		return tx.Notify(store.PreviewChannel, "share|"+id.String())
	})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	a.shares.clear()
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) itemGone(ctx context.Context, id ids.UUID) bool {
	err := a.o.DB.View(ctx, func(tx store.Tx) error {
		_, err := tx.Content().GetItem(ctx, id, false)
		return err
	})
	return errors.Is(err, store.ErrNotFound)
}

// CleanupPreview deletes expired and revoked share sessions of more than a day ago and stale relayed drafts.
func (a *API) CleanupPreview(ctx context.Context) error {
	if a.o.DB == nil {
		return nil
	}
	now := a.o.Clock.Now()
	return a.o.DB.Tx(ctx, func(tx store.Tx) error {
		if _, err := tx.Preview().DeleteExpiredShares(ctx, now.Add(-24*time.Hour)); err != nil {
			return err
		}
		n, err := tx.Preview().DeleteStaleRelays(ctx, now.Add(-RelayTTL))
		if err != nil || n == 0 {
			return err
		}
		// Streams still show a relay that has gone stale: tell every server to rebuild them.
		return tx.Notify(store.PreviewChannel, "relays|purged")
	})
}
