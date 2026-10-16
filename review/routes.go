package review

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
)

type (
	submitBody struct {
		Comment   string   `json:"comment"`
		Reviewers []string `json:"reviewers"`
	}
	commentBody struct {
		Comment string `json:"comment"`
	}
	approveBody struct {
		Comment   string `json:"comment"`
		DraftHash string `json:"draftHash"`
	}
)

func itemID(r *http.Request) (ids.UUID, error) {
	id, err := ids.Parse(r.PathValue("id"))
	if err != nil {
		return ids.Nil, manage.NotFound("item not found")
	}
	return id, nil
}

func reply(w http.ResponseWriter, r *http.Request, v *View, err error) {
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, v)
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	id, err := itemID(r)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	v, err := s.Get(r.Context(), id)
	reply(w, r, v, err)
}

// action serves a POST that takes an optional JSON body of T and runs fn.
func action[T any](fn func(r *http.Request, id ids.UUID, b T) (*View, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := itemID(r)
		if err != nil {
			manage.Fail(w, r, err)
			return
		}
		var b T
		if err := optionalBody(w, r, &b); err != nil {
			manage.Fail(w, r, err)
			return
		}
		v, err := fn(r, id, b)
		reply(w, r, v, err)
	}
}

func (s *Service) queue(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	o := QueueOptions{Type: q.Get("type")}
	if v := q.Get("state"); v != "" {
		o.States = strings.Split(v, ",")
	}
	switch q.Get("reviewer") {
	case "":
	case "me":
		o.Me = true
	default:
		manage.Fail(w, r, manage.BadRequest("reviewer must be me"))
		return
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
	rows, total, hasMore, err := s.Queue(r.Context(), o)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"items": rows, "total": total, "hasMore": hasMore})
}

func (s *Service) approversRoute(w http.ResponseWriter, r *http.Request) {
	id, err := ids.Parse(r.URL.Query().Get("item"))
	if err != nil {
		manage.Fail(w, r, manage.BadRequest("item must be an item id"))
		return
	}
	list, err := s.Approvers(r.Context(), id)
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, map[string]any{"items": list})
}

func intParam(v string) (int, error) {
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, manage.BadRequest("bad number")
	}
	return n, nil
}

// ManageRoutes are the management API routes of reviews, for the kernel to mount.
func (s *Service) ManageRoutes() []manage.Route {
	rt := func(method, path, policy string, h http.HandlerFunc, op map[string]any) manage.Route {
		return manage.Route{Method: method, Path: path, Handler: h, Policy: policy, Module: "reviews", OpenAPI: op}
	}
	return []manage.Route{
		rt(http.MethodGet, "/items/{id}/review", access.PolicyContentRead, s.get, opGet()),
		rt(http.MethodPost, "/items/{id}/review/submit", access.PolicyReviewSubmit, action(
			func(r *http.Request, id ids.UUID, b submitBody) (*View, error) {
				return s.Submit(r.Context(), id, SubmitInput(b))
			}), opSubmit()),
		rt(http.MethodPost, "/items/{id}/review/approve", access.PolicyReviewApprove, action(
			func(r *http.Request, id ids.UUID, b approveBody) (*View, error) {
				return s.Approve(r.Context(), id, ApproveInput(b))
			}), opApprove()),
		rt(http.MethodPost, "/items/{id}/review/request-changes", access.PolicyReviewApprove, action(
			func(r *http.Request, id ids.UUID, b commentBody) (*View, error) {
				return s.RequestChanges(r.Context(), id, b.Comment)
			}), opRequestChanges()),
		rt(http.MethodPost, "/items/{id}/review/withdraw", manage.Authenticated, action(
			func(r *http.Request, id ids.UUID, b commentBody) (*View, error) {
				return s.Withdraw(r.Context(), id, b.Comment)
			}), opWithdraw()),
		rt(http.MethodPost, "/items/{id}/review/comments", manage.Authenticated, action(
			func(r *http.Request, id ids.UUID, b commentBody) (*View, error) {
				return s.Comment(r.Context(), id, b.Comment)
			}), opComment()),
		rt(http.MethodGet, "/reviews", access.PolicyContentRead, s.queue, opQueue()),
		rt(http.MethodGet, "/reviews/approvers", access.PolicyReviewSubmit, s.approversRoute, opApprovers()),
	}
}
