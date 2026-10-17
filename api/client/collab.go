package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// CollabUpdate is an opaque Yjs update of an item's co-editing session.
type CollabUpdate struct {
	Seq    int64    `json:"seq"`
	Data   []byte   `json:"data"`
	Client string   `json:"client"`
	Init   []string `json:"init,omitempty"`
}

// CollabParticipant is a presence entry of a session.
type CollabParticipant struct {
	Client    string `json:"client"`
	Principal string `json:"principal"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	Locale    string `json:"locale"`
	Focus     string `json:"focus"`
	Idle      bool   `json:"idle"`
	Edit      bool   `json:"edit"`
}

// CollabSession is the answer to [Manage.JoinCollab].
type CollabSession struct {
	Client   string              `json:"client"`
	Session  string              `json:"session"`
	Seq      int64               `json:"seq"`
	Updates  []CollabUpdate      `json:"updates"`
	More     bool                `json:"more"`    // fetch the rest with [Manage.CollabUpdates] from Seq
	Init     []string            `json:"init"`    // parts this client must seed
	Pending  []string            `json:"pending"` // parts another client is seeding
	Presence []CollabParticipant `json:"presence"`
	Etag     string              `json:"etag"`
}

// JoinCollab joins the co-editing session of an item. client names a client that re-joins ("" lets the server
// choose); edit asks to post updates (needs content.edit). The event stream (GET …/events) is for browsers and
// is not wrapped here.
func (m *Manage) JoinCollab(ctx context.Context, item, locale string, edit bool, client string) (*CollabSession, error) {
	body := map[string]any{"locale": locale, "edit": edit}
	if client != "" {
		body["client"] = client
	}
	raw, err := m.collab(ctx, item, "join", body)
	if err != nil {
		return nil, err
	}
	var s CollabSession
	if err := decode(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// PostCollabUpdate posts an update and returns its seq; init lists the parts it seeds.
func (m *Manage) PostCollabUpdate(ctx context.Context, item, client string, data []byte, init ...string) (int64, error) {
	body := map[string]any{"client": client, "data": data}
	if len(init) > 0 {
		body["init"] = init
	}
	raw, err := m.collab(ctx, item, "updates", body)
	if err != nil {
		return 0, err
	}
	var out struct {
		Seq int64 `json:"seq"`
	}
	err = decode(raw, &out)
	return out.Seq, err
}

// CompactCollab replaces the session's updates up to upto by one state.
func (m *Manage) CompactCollab(ctx context.Context, item, client string, state []byte, upto int64) error {
	_, err := m.collab(ctx, item, "compact", map[string]any{"client": client, "data": state, "upto": upto})
	return err
}

// CollabPresence sends a heartbeat with the client's locale and focused field.
func (m *Manage) CollabPresence(ctx context.Context, item, client, locale, focus string, idle bool) error {
	_, err := m.collab(ctx, item, "presence", map[string]any{"client": client, "locale": locale, "focus": focus, "idle": idle})
	return err
}

// CollabPage is a page of a session's log.
type CollabPage struct {
	Updates []CollabUpdate `json:"updates"`
	Seq     int64          `json:"seq"`
	More    bool           `json:"more"`
}

// CollabUpdates reads a page of the session's log after since.
func (m *Manage) CollabUpdates(ctx context.Context, item, client string, since int64) (*CollabPage, error) {
	q := url.Values{"client": {client}, "since": {strconv.FormatInt(since, 10)}}
	_, raw, err := m.c.call(ctx, http.MethodGet, "/collab/items/"+url.PathEscape(item)+"/updates", q.Encode(), nil, nil, false)
	if err != nil {
		return nil, err
	}
	var p CollabPage
	return &p, decode(raw, &p)
}

// ResetCollab drops the item's session for everyone (editing clients only), for a client that cannot apply
// the log; join again afterwards.
func (m *Manage) ResetCollab(ctx context.Context, item, client string) error {
	_, err := m.collab(ctx, item, "reset", map[string]any{"client": client})
	return err
}

// LeaveCollab leaves the session.
func (m *Manage) LeaveCollab(ctx context.Context, item, client string) error {
	_, err := m.collab(ctx, item, "leave", map[string]any{"client": client})
	return err
}

func (m *Manage) collab(ctx context.Context, item, verb string, body any) ([]byte, error) {
	_, raw, err := m.c.call(ctx, http.MethodPost, "/collab/items/"+url.PathEscape(item)+"/"+verb, "", body, nil, false)
	return raw, err
}
