package collab

import (
	"net/http"
	"strconv"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/ids"
)

func itemID(r *http.Request) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		return ids.Nil, manage.NotFound("item not found")
	}
	return id, nil
}

// post serves a POST that takes a JSON body of T and answers with fn's result (204 when it is nil).
func post[T any](fn func(r *http.Request, item ids.UUID, b T) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := itemID(r)
		if err != nil {
			manage.Fail(w, r, err)
			return
		}
		var b T
		if err := manage.DecodeJSON(w, r, &b); err != nil {
			manage.Fail(w, r, err)
			return
		}
		out, err := fn(r, item, b)
		switch {
		case err != nil:
			manage.Fail(w, r, err)
		case out == nil:
			w.WriteHeader(http.StatusNoContent)
		default:
			manage.WriteJSON(w, http.StatusOK, out)
		}
	}
}

// page serves GET …/updates?client=&since=: a page of the log after a seq.
func (s *Service) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	item, err := itemID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	q := r.URL.Query()
	client := q.Get("client")
	if !clientRE.MatchString(client) {
		manage.Fail(w, r, manage.BadRequest("client is required"))
		return
	}
	since, err := strconv.ParseInt(q.Get("since"), 10, 64)
	if err != nil || since < 0 {
		manage.Fail(w, r, manage.BadRequest("since must be a sequence number"))
		return
	}
	p, err := s.Updates(r.Context(), item, client, since)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, p)
}

// ManageRoutes are the management API routes of collaboration, for the kernel to mount. Every route needs
// content.read at the gate; the service checks the item (read, and edit for an editing client).
func (s *Service) ManageRoutes() []manage.Route {
	if s == nil {
		return nil
	}
	rt := func(method, path string, h http.HandlerFunc, op map[string]any) manage.Route {
		return manage.Route{Method: method, Path: "/collab/items/{id}/" + path, Handler: h, Policy: access.PolicyContentRead,
			Module: "collab", OpenAPI: op}
	}
	return []manage.Route{
		rt(http.MethodPost, "join", post(func(r *http.Request, item ids.UUID, b JoinInput) (any, error) {
			return s.Join(r.Context(), item, b)
		}), opJoin()),
		rt(http.MethodGet, "events", s.events, opEvents()),
		rt(http.MethodPost, "updates", post(func(r *http.Request, item ids.UUID, b UpdateInput) (any, error) {
			seq, err := s.PostUpdate(r.Context(), item, b)
			return map[string]any{"seq": seq}, err
		}), opUpdates()),
		rt(http.MethodGet, "updates", s.page, opPage()),
		rt(http.MethodPost, "reset", post(func(r *http.Request, item ids.UUID, b struct {
			Client string `json:"client"`
		}) (any, error) {
			return nil, s.Reset(r.Context(), item, b.Client)
		}), opReset()),
		rt(http.MethodPost, "compact", post(func(r *http.Request, item ids.UUID, b CompactInput) (any, error) {
			_, err := s.Compact(r.Context(), item, b)
			return nil, err
		}), opCompact()),
		rt(http.MethodPost, "presence", post(func(r *http.Request, item ids.UUID, b PresenceInput) (any, error) {
			return nil, s.Presence(r.Context(), item, b)
		}), opPresence()),
		rt(http.MethodPost, "leave", post(func(r *http.Request, item ids.UUID, b struct {
			Client string `json:"client"`
		}) (any, error) {
			return nil, s.Leave(r.Context(), item, b.Client)
		}), opLeave()),
	}
}
