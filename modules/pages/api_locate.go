package pages

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/api/manage"
)

const headerParam = "header."

func (h *handlers) locate(w http.ResponseWriter, r *http.Request) {
	a := h.api(w, r)
	if a == nil {
		return
	}
	q := r.URL.Query()
	if q.Get("url") == "" {
		manage.Fail(w, r, manage.BadRequest("url is required"))
		return
	}
	res, err := a.Locate(r.Context(), q.Get("url"), simulatedHeaders(q))
	if err != nil {
		manage.Fail(w, r, err)
		return
	}
	manage.WriteJSON(w, http.StatusOK, res)
}

// simulatedHeaders turns header.<Name>=value query parameters into request headers. Keys are visited in sorted
// order, so when two keys canonicalise to the same header name (header.x-a and header.X-A) the last in sorted
// order wins, whatever the query order.
func simulatedHeaders(q url.Values) http.Header {
	h := http.Header{}
	for _, k := range slices.Sorted(maps.Keys(q)) {
		if name, ok := strings.CutPrefix(k, headerParam); ok && name != "" && len(q[k]) > 0 {
			h.Set(name, q[k][0])
		}
	}
	return h
}
