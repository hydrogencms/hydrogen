package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// A relayed draft is an editor's unsaved working document of one item and locale. Preview requests show it
// instead of the stored draft while it is newer than the draft and younger than RelayTTL, so share-link viewers
// and the admin's own preview frame see edits that are not saved.

// prefetchRelays reads the relayed drafts of the previewed items in list with one query, so a channel-wide
// preview listing does not cost one query per item. A single item is left to relayed's own lookup.
func (st *state) prefetchRelays(ctx context.Context, list []ids.UUID) {
	if st.a.o.DB == nil || len(list) < 2 {
		return
	}
	var want []ids.UUID
	for _, id := range list {
		if st.previews(id) && !st.relaysOf[id] {
			want = append(want, id)
		}
	}
	if len(want) < 2 {
		return
	}
	var rows []store.RelayDraft
	err := st.a.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		rows, err = tx.Preview().GetRelays(ctx, want)
		return err
	})
	if err != nil {
		st.a.log.Error("delivery: read relayed drafts", "err", err)
		return
	}
	if st.relays == nil {
		st.relays, st.relaysOf = map[ids.UUID][]store.RelayDraft{}, map[ids.UUID]bool{}
	}
	for _, id := range want {
		st.relaysOf[id] = true
	}
	for _, rd := range rows {
		st.relays[rd.Item] = append(st.relays[rd.Item], rd)
	}
}

func (st *state) relayFor(ctx context.Context, v *content.View) (store.RelayDraft, error) {
	if st.relaysOf[v.ID] {
		for _, rd := range st.relays[v.ID] {
			if rd.Locale == v.Locale {
				return rd, nil
			}
		}
		return store.RelayDraft{}, store.ErrNotFound
	}
	var rd store.RelayDraft
	err := st.a.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		rd, err = tx.Preview().GetRelay(ctx, v.ID, v.Locale)
		return err
	})
	return rd, err
}

// relayed returns v with the relayed working document of its item and locale in place of the stored draft's
// fields, when one applies. It compares with the item's update time, so any save of the item ends the overlay.
// Any failure leaves the stored draft: a preview must not break on the relay. The applied rev goes to the
// request's relayProbe, if it carries one.
func (st *state) relayed(ctx context.Context, v *content.View) *content.View {
	a := st.a
	if a.o.DB == nil {
		return v
	}
	rd, err := st.relayFor(ctx, v)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			a.log.Error("delivery: read relayed draft", "item", v.ID, "err", err)
		}
		return v
	}
	if !rd.UpdatedAt.After(v.UpdatedAt) || a.o.Clock.Now().Sub(rd.UpdatedAt) >= RelayTTL {
		return v
	}
	t, ok := a.o.Schema.Type(v.Type)
	doc, err := decodeFields(rd.Fields)
	if !ok || err != nil {
		return v
	}
	if p, ok := ctx.Value(probeKey{}).(*relayProbe); ok && p.item == v.ID {
		p.rev.Store(rd.Rev)
	}
	nv := *v
	nv.Fields = doc
	nv.Name = a.o.Schema.Name(t, doc)
	return &nv
}

func decodeFields(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func (a *API) relayTarget(r *http.Request) (item ids.UUID, typ, locale string, err error) {
	item, perr := ids.Parse(r.PathValue("item"))
	if perr != nil {
		return item, "", "", manage.NotFound("no such item")
	}
	typ, locale, err = a.o.Content.CheckRelay(r.Context(), item, r.URL.Query().Get("locale"))
	return item, typ, locale, err
}

func (a *API) putRelay(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := manage.DecodeJSON(w, r, &in); err != nil {
		manage.Fail(w, r, err)
		return
	}
	item, typ, locale, err := a.relayTarget(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	if len(in.Fields) == 0 || in.Fields[0] != '{' {
		manage.Fail(w, r, manage.BadRequest("fields must be an object"))
		return
	}
	doc, err := decodeFields(in.Fields)
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("fields: %v", err))
		return
	}
	clean, err := a.o.Content.ValidateRelay(typ, locale, doc)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	stored, err := json.Marshal(clean)
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("fields: %v", err))
		return
	}
	ctx := r.Context()
	var rev int64
	err = a.o.DB.Tx(ctx, func(tx store.Tx) (err error) {
		rev, err = tx.Preview().PutRelay(ctx, store.RelayDraft{Item: item, Locale: locale, Fields: stored,
			UpdatedBy: actorString(ctx), UpdatedAt: a.o.Clock.Now().UTC()})
		if err != nil {
			return err
		}
		return tx.Notify(store.PreviewChannel, item.String()+"|"+locale+"|"+strconv.FormatInt(rev, 10))
	})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"rev": rev})
}

func (a *API) deleteRelay(w http.ResponseWriter, r *http.Request) {
	item, _, locale, err := a.relayTarget(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	ctx := r.Context()
	err = a.o.DB.Tx(ctx, func(tx store.Tx) error {
		if err := tx.Preview().DeleteRelay(ctx, item, locale); err != nil {
			return err
		}
		return tx.Notify(store.PreviewChannel, item.String()+"|"+locale+"|0")
	})
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func actorString(ctx context.Context) string { return access.PrincipalFrom(ctx).Actor().String() }

// ManageRoutes are the management routes delivery serves: preview tokens, share links and relayed drafts.
func (a *API) ManageRoutes() []manage.Route {
	rts := []manage.Route{a.PreviewTokenRoute()}
	if a.o.DB == nil {
		return rts
	}
	type m = map[string]any
	errRef := func(code string) m { return m{"$ref": "#/components/responses/" + code} }
	itemParam := m{"name": "item", "in": "path", "required": true, "schema": m{"type": "string", "format": "uuid"}}
	localeParam := m{"name": "locale", "in": "query", "schema": m{"type": "string"},
		"description": "A locale; default the item's main locale."}
	shareProps := m{"id": m{"type": "string", "format": "uuid"}, "channel": m{"type": "string"}, "locale": m{"type": "string"},
		"expiresAt": m{"type": "string", "format": "date-time"}}
	return append(rts,
		manage.Route{Method: http.MethodPost, Path: "/preview-shares", Summary: "Create a share link to an item's preview",
			Handler: a.createShare, Policy: access.PolicyContentPreviewShare,
			OpenAPI: m{
				"operationId": "createPreviewShare",
				"summary":     "Create a share link to an item's preview",
				"description": "A share token (hps_…) opens the preview of one item in one channel for anyone who holds it, until " +
					"it expires or is revoked; it is shown only in this response. url is the item's preview URL with " +
					"?preview=token, absent when none resolves. Needs content.preview.share; 403 when delivery.share_links is off.",
				"requestBody": m{"required": true, "content": m{"application/json": m{"schema": m{
					"type": "object", "required": []any{"item", "channel"}, "additionalProperties": false,
					"properties": m{
						"item":    m{"type": "string", "format": "uuid"},
						"channel": m{"type": "string"},
						"locale":  m{"type": "string", "description": "Default the item's main locale if the channel serves it, else its first language."},
						"ttl": m{"oneOf": []any{m{"type": "integer", "minimum": 1}, m{"type": "string"}},
							"description": "Seconds or a duration such as 24h; at most delivery.share_max_ttl, default delivery.share_default_ttl."},
					}}}}},
				"responses": m{
					"201": m{"description": "The share link.", "content": m{"application/json": m{"schema": m{
						"type": "object", "required": []any{"id", "token", "expiresAt", "channel", "locale"},
						"properties": merge(shareProps, m{"token": m{"type": "string"}, "url": m{"type": "string"}})}}}},
					"400": errRef("BadRequest"), "401": errRef("Unauthorized"), "403": errRef("Forbidden"), "404": errRef("NotFound"),
					"413": errRef("TooLarge"), "415": errRef("UnsupportedMedia"),
				},
			}},
		manage.Route{Method: http.MethodGet, Path: "/preview-shares", Summary: "List an item's share links",
			Handler: a.listShares, Policy: access.PolicyContentPreview,
			OpenAPI: m{
				"operationId": "listPreviewShares",
				"summary":     "List an item's share links",
				"description": "Newest first, including expired and revoked links. Tokens are never listed. viewers counts the event " +
					"streams open on the server that answers.",
				"parameters": []any{m{"name": "item", "in": "query", "required": true, "schema": m{"type": "string", "format": "uuid"}}},
				"responses": m{
					"200": m{"description": "The links.", "content": m{"application/json": m{"schema": m{
						"type": "object", "required": []any{"shares"},
						"properties": m{"shares": m{"type": "array", "items": m{
							"type": "object", "required": []any{"id", "channel", "locale", "createdBy", "createdByName", "createdAt", "expiresAt", "viewers"},
							"properties": merge(shareProps, m{"createdBy": m{"type": "string"},
								"createdByName": m{"type": "string", "description": "The creator's display name (user name, else login; token name), empty when unknown."},
								"createdAt":     m{"type": "string", "format": "date-time"},
								"revokedAt":     m{"type": "string", "format": "date-time"}, "viewers": m{"type": "integer"}})}}}}}}},
					"400": errRef("BadRequest"), "401": errRef("Unauthorized"), "403": errRef("Forbidden"), "404": errRef("NotFound"),
				},
			}},
		manage.Route{Method: http.MethodDelete, Path: "/preview-shares/{id}", Summary: "Revoke a share link",
			Handler: a.revokeShare, Policy: access.PolicyContentPreviewShare,
			OpenAPI: m{
				"operationId": "revokePreviewShare",
				"summary":     "Revoke a share link",
				"description": "Open event streams of the link end with reason revoked; requests with its token fail within seconds on every server.",
				"parameters":  []any{m{"name": "id", "in": "path", "required": true, "schema": m{"type": "string", "format": "uuid"}}},
				"responses": m{"204": m{"description": "Revoked."}, "401": errRef("Unauthorized"), "403": errRef("Forbidden"),
					"404": errRef("NotFound")},
			}},
		manage.Route{Method: http.MethodPut, Path: "/preview-relay/{item}", Summary: "Relay an unsaved working document",
			Handler: a.putRelay, Policy: access.PolicyContentEdit,
			OpenAPI: m{
				"operationId": "putPreviewRelay",
				"summary":     "Relay an unsaved working document",
				"description": "Stores the editor's unsaved fields of an item and locale, replacing the previous ones. Previews of the item " +
					"(share links and the admin's frame) show them instead of the saved draft while they are newer than it and " +
					"less than an hour old; nothing is saved. Needs content.edit on the item; fields are validated like a draft save " +
					"(required fields may be missing).",
				"parameters": []any{itemParam, localeParam},
				"requestBody": m{"required": true, "content": m{"application/json": m{"schema": m{
					"type": "object", "required": []any{"fields"}, "additionalProperties": false,
					"properties": m{"fields": m{"type": "object", "additionalProperties": true}}}}}},
				"responses": m{
					"200": m{"description": "Stored.", "content": m{"application/json": m{"schema": m{
						"type": "object", "required": []any{"rev"}, "properties": m{"rev": m{"type": "integer", "format": "int64"}}}}}},
					"400": errRef("BadRequest"), "401": errRef("Unauthorized"), "403": errRef("Forbidden"), "404": errRef("NotFound"),
					"413": errRef("TooLarge"), "415": errRef("UnsupportedMedia"), "422": errRef("Invalid"),
				},
			}},
		manage.Route{Method: http.MethodDelete, Path: "/preview-relay/{item}", Summary: "Drop a relayed working document",
			Handler: a.deleteRelay, Policy: access.PolicyContentEdit,
			OpenAPI: m{
				"operationId": "deletePreviewRelay",
				"summary":     "Drop a relayed working document",
				"description": "Previews fall back to the saved draft.",
				"parameters":  []any{itemParam, localeParam},
				"responses": m{"204": m{"description": "Dropped."}, "401": errRef("Unauthorized"), "403": errRef("Forbidden"),
					"404": errRef("NotFound")},
			}},
	)
}

func merge(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
