package release

import (
	"bufio"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/api/manage"
	"github.com/hydrogencms/hydrogen/ids"
)

type (
	itemBody struct {
		Action  string   `json:"action"`
		Locales []string `json:"locales"`
	}
	createItemBody struct {
		Item    string   `json:"item"`
		Action  string   `json:"action"`
		Locales []string `json:"locales"`
	}
	createBody struct {
		Name        string           `json:"name"`
		Description string           `json:"description"`
		Kind        string           `json:"kind"`
		Items       []createItemBody `json:"items"`
		ScheduledAt *time.Time       `json:"scheduledAt"`
	}
	updateBody struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
	}
	scheduleBody struct {
		At *time.Time `json:"at"`
	}
	publishBody struct {
		Force bool `json:"force"`
	}
)

func releaseID(r *http.Request) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		return ids.Nil, notFound()
	}
	return id, nil
}

// optionalBody decodes a JSON object body that may be absent.
func optionalBody(w http.ResponseWriter, r *http.Request, dst any) error {
	br := bufio.NewReader(r.Body)
	if _, err := br.Peek(1); errors.Is(err, io.EOF) {
		return nil
	}
	r.Body = io.NopCloser(br)
	return manage.DecodeJSON(w, r, dst)
}

func (s *Service) listHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	o := ListOptions{Kind: q.Get("kind")}
	if v := q.Get("status"); v != "" {
		o.Statuses = strings.Split(v, ",")
	}
	if v := q.Get("item"); v != "" {
		id, err := ids.Parse(v)
		if err != nil {
			manage.Fail(w, r, manage.BadRequest("item must be an item id"))
			return
		}
		o.Item = id
	}
	var err error
	if o.Limit, err = intParam(q.Get("limit")); err != nil {
		manage.Fail(w, r, manage.BadRequest("limit must be a number"))
		return
	}
	if o.Offset, err = intParam(q.Get("offset")); err != nil {
		manage.Fail(w, r, manage.BadRequest("offset must be a number"))
		return
	}
	list, total, err := s.List(r.Context(), o)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"items": list, "total": total, "hasMore": o.Offset+len(list) < total})
}

func intParam(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, errors.New("bad number")
	}
	return n, nil
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var b createBody
	if err := manage.DecodeJSON(w, r, &b); err != nil {
		manage.Fail(w, r, err)
		return
	}
	in := CreateInput{Name: b.Name, Description: b.Description, Kind: b.Kind, ScheduledAt: b.ScheduledAt}
	for _, it := range b.Items {
		id, err := ids.Parse(it.Item)
		if err != nil {
			manage.Fail(w, r, manage.BadRequest("items: %q is not an item id", it.Item))
			return
		}
		in.Items = append(in.Items, ItemInput{Item: id, Action: it.Action, Locales: it.Locales})
	}
	v, err := s.Create(r.Context(), in)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	w.Header().Set("Location", "releases/"+v.ID)
	manage.WriteJSON(w, http.StatusCreated, v)
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.Get(r.Context(), id)
	reply(w, r, v, err)
}

func (s *Service) patch(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	var b updateBody
	if err := manage.DecodeJSON(w, r, &b); err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.Update(r.Context(), id, UpdateInput(b))
	reply(w, r, v, err)
}

func (s *Service) remove(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err == nil {
		err = s.Delete(r.Context(), id)
	}
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) putItemHandler(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	item, err := ids.Parse(r.PathValue("item"))
	if err != nil {
		manage.Fail(w, r, manage.NotFound("item not found"))
		return
	}
	var b itemBody
	if err := manage.DecodeJSON(w, r, &b); err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.AddItem(r.Context(), id, ItemInput{Item: item, Action: b.Action, Locales: b.Locales})
	reply(w, r, v, err)
}

func (s *Service) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	item, err := ids.Parse(r.PathValue("item"))
	if err != nil {
		manage.Fail(w, r, manage.NotFound("item not found"))
		return
	}
	if _, err := s.RemoveItem(r.Context(), id, item); err != nil {
		manage.Fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) schedule(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	var b scheduleBody
	if err := manage.DecodeJSON(w, r, &b); err != nil {
		manage.Fail(w, r, err)
		return
	}
	if b.At == nil {
		manage.Fail(w, r, manage.BadRequest("at is required: an RFC 3339 time"))
		return
	}
	v, err := s.Schedule(r.Context(), id, *b.At)
	reply(w, r, v, err)
}

func (s *Service) unschedule(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.Unschedule(r.Context(), id)
	reply(w, r, v, err)
}

func (s *Service) cancel(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.Cancel(r.Context(), id)
	reply(w, r, v, err)
}

func (s *Service) publish(w http.ResponseWriter, r *http.Request) {
	id, err := releaseID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	var b publishBody
	if err := optionalBody(w, r, &b); err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.Publish(r.Context(), id, b.Force)
	var pf *PublishFailedError
	if errors.As(err, &pf) {
		manage.WriteJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":   map[string]any{"code": "release_failed", "message": "nothing was published: " + strconv.Itoa(len(pf.Release.Errors)) + " error(s); see the release"},
			"release": pf.Release})
		return
	}
	reply(w, r, v, err)
}

func (s *Service) ofItem(w http.ResponseWriter, r *http.Request) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		manage.Fail(w, r, manage.NotFound("item not found"))
		return
	}
	list, err := s.OfItem(r.Context(), id)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"items": list, "total": len(list), "hasMore": false})
}

func reply(w http.ResponseWriter, r *http.Request, v *View, err error) {
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, v)
}

// ManageRoutes are the management API routes of releases, for the kernel to mount.
func (s *Service) ManageRoutes() []manage.Route {
	read, write := access.PolicyContentRead, access.PolicyReleaseManage
	rt := func(method, path, policy string, h http.HandlerFunc, op map[string]any) manage.Route {
		return manage.Route{Method: method, Path: path, Handler: h, Policy: policy, Module: "releases", OpenAPI: op}
	}
	return []manage.Route{
		rt(http.MethodGet, "/releases", read, s.listHandler, opList()),
		rt(http.MethodPost, "/releases", write, s.create, opCreate()),
		rt(http.MethodGet, "/releases/{id}", read, s.get, opGet()),
		rt(http.MethodPatch, "/releases/{id}", write, s.patch, opPatch()),
		rt(http.MethodDelete, "/releases/{id}", write, s.remove, opDelete()),
		rt(http.MethodPut, "/releases/{id}/items/{item}", write, s.putItemHandler, opPutItem()),
		rt(http.MethodDelete, "/releases/{id}/items/{item}", write, s.deleteItem, opDeleteItem()),
		rt(http.MethodPost, "/releases/{id}/schedule", write, s.schedule, opSchedule()),
		rt(http.MethodPost, "/releases/{id}/unschedule", write, s.unschedule, opState("unscheduleRelease", "Unschedule a release",
			"Turns a scheduled release back into an open one.")),
		rt(http.MethodPost, "/releases/{id}/cancel", write, s.cancel, opState("cancelRelease", "Cancel a release",
			"Retires an open or scheduled release for good (a cancelled release can still be deleted).")),
		rt(http.MethodPost, "/releases/{id}/publish", write, s.publish, opPublish()),
		rt(http.MethodGet, "/items/{id}/releases", read, s.ofItem, opOfItem()),
	}
}
