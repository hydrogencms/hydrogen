package manage

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/schema"
)

type createBody struct {
	Locale          string         `json:"locale"`
	Key             string         `json:"key"`
	AlwaysAvailable bool           `json:"alwaysAvailable"`
	Fields          map[string]any `json:"fields"`
}

type updateBody struct {
	Locale          string         `json:"locale"`
	Fields          map[string]any `json:"fields"`
	Key             *string        `json:"key"`
	AlwaysAvailable *bool          `json:"alwaysAvailable"`
}

type publishBody struct {
	Locales []string `json:"locales"`
	Force   bool     `json:"force"`
}

func (u updateBody) input() content.UpdateInput {
	return content.UpdateInput{Locale: u.Locale, Fields: u.Fields, Key: u.Key, AlwaysAvailable: u.AlwaysAvailable}
}

// typeOf finds a content type by path name; singleton says which kind of route asks.
func (a *api) typeOf(r *http.Request, singleton bool) (*schema.Type, error) {
	name := r.PathValue("type")
	t, ok := a.sc.Type(name)
	if !ok || t.Singleton != singleton {
		kind := "collection"
		if singleton {
			kind = "singleton"
		}
		return nil, NotFound("unknown %s type %q", kind, name)
	}
	return t, nil
}

func getOptions(r *http.Request) (content.GetOptions, error) {
	q := r.URL.Query()
	o := content.GetOptions{}
	var err error
	if o.Locale, err = single(q, "locale"); err != nil {
		return o, err
	}
	d, err := single(q, "draft")
	if err != nil {
		return o, err
	}
	if d != "" {
		if o.Draft, err = strconv.ParseBool(d); err != nil {
			return o, BadRequest("draft must be true or false")
		}
	}
	o.Trashed, err = boolParam(q, "trashed")
	return o, err
}

func (a *api) listItems(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, false)
	if err != nil {
		return err
	}
	o, err := parseList(t.Name, r.URL.Query())
	if err != nil {
		return err
	}
	res, err := a.c.List(r.Context(), o)
	if err != nil {
		return err
	}
	items := res.Items
	if items == nil {
		items = []content.Summary{}
	}
	WriteJSON(w, http.StatusOK, content.ListResult{Items: items, Total: res.Total})
	return nil
}

func (a *api) createItem(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, false)
	if err != nil {
		return err
	}
	var b createBody
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	if b.Fields == nil {
		b.Fields = map[string]any{}
	}
	v, err := a.c.Create(r.Context(), content.CreateInput{Type: t.Name, Locale: b.Locale, Key: b.Key,
		AlwaysAvailable: b.AlwaysAvailable, Fields: b.Fields})
	if err != nil {
		return err
	}
	w.Header().Set("Location", a.prefix+"/items/"+v.ID.String())
	writeView(w, http.StatusCreated, v)
	return nil
}

func (a *api) getItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	o, err := getOptions(r)
	if err != nil {
		return err
	}
	v, err := a.c.Get(r.Context(), id, o)
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) updateItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var b updateBody
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	v, err := a.c.Update(preconditionCtx(r), id, b.input())
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) publishItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var b publishBody
	if err := decodeBody(w, r, &b, true); err != nil {
		return err
	}
	v, err := a.c.Publish(preconditionCtx(r), id, content.PublishOptions{Locales: b.Locales, Force: b.Force})
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) discardDraft(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	raw, err := single(q, "parts")
	if err != nil {
		return err
	}
	var parts []string
	if raw != "" {
		for _, p := range strings.Split(raw, ",") {
			if p = strings.TrimSpace(p); p == "" {
				return BadRequest("parts has an empty entry")
			}
			parts = append(parts, p)
		}
	}
	if err := a.c.Discard(preconditionCtx(r), id, parts); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *api) deleteItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	perm, err := boolParam(r.URL.Query(), "permanent")
	if err != nil {
		return err
	}
	if perm {
		err = a.c.Purge(preconditionCtx(r), id)
	} else {
		err = a.c.Delete(preconditionCtx(r), id)
	}
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *api) unpublishItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	v, err := a.c.Unpublish(preconditionCtx(r), id)
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) restoreItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	v, err := a.c.Restore(preconditionCtx(r), id)
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) listTrash(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	for k := range q {
		switch k {
		case "type", "locale", "limit", "offset":
		default:
			return BadRequest("unknown query parameter %q", k)
		}
	}
	o := content.TrashOptions{}
	var err error
	if o.Type, err = single(q, "type"); err != nil {
		return err
	}
	if o.Locale, err = single(q, "locale"); err != nil {
		return err
	}
	if o.Limit, err = parseInt(q, "limit", defaultLimit, 1, maxLimit); err != nil {
		return err
	}
	if o.Offset, err = parseInt(q, "offset", 0, 0, maxOffset); err != nil {
		return err
	}
	res, err := a.c.ListTrash(r.Context(), o)
	if err != nil {
		return err
	}
	if res.Items == nil {
		res.Items = []content.TrashSummary{}
	}
	WriteJSON(w, http.StatusOK, res)
	return nil
}

// boolParam reads an optional true/false query parameter.
func boolParam(q url.Values, name string) (bool, error) {
	v, err := single(q, name)
	if err != nil || v == "" {
		return false, err
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, BadRequest("%s must be true or false", name)
	}
	return b, nil
}

type versionMeta struct {
	Version   int       `json:"version"`
	SchemaRev int       `json:"schemaRev"`
	Locales   []string  `json:"locales"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
}

func (a *api) versions(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	vs, err := a.c.Versions(r.Context(), id)
	if err != nil {
		return err
	}
	items := make([]versionMeta, len(vs))
	for i, v := range vs {
		locales := v.Locales
		if locales == nil {
			locales = []string{}
		}
		items[i] = versionMeta{v.Version, v.SchemaRev, locales, v.CreatedAt, v.CreatedBy}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
	return nil
}

func (a *api) version(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		return NotFound("version not found")
	}
	v, err := a.c.VersionContent(r.Context(), id, n)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, v)
	return nil
}

func (a *api) restoreVersion(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		return NotFound("version not found")
	}
	var b struct {
		Locales []string `json:"locales"`
	}
	if err := decodeBody(w, r, &b, true); err != nil {
		return err
	}
	v, err := a.c.RestoreVersion(preconditionCtx(r), id, n, b.Locales)
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) references(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	refs, hidden, err := a.c.ReferencesVisible(r.Context(), id)
	if err != nil {
		return err
	}
	if refs == nil {
		refs = []content.Referrer{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": refs, "total": len(refs), "hidden": hidden})
	return nil
}

func (a *api) getSingleton(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, true)
	if err != nil {
		return err
	}
	o, err := getOptions(r)
	if err != nil {
		return err
	}
	v, err := a.c.Singleton(r.Context(), t.Name, o)
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) updateSingleton(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, true)
	if err != nil {
		return err
	}
	var b updateBody
	if err := decodeBody(w, r, &b, false); err != nil {
		return err
	}
	v, err := a.c.UpdateSingleton(preconditionCtx(r), t.Name, b.input())
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

func (a *api) publishSingleton(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, true)
	if err != nil {
		return err
	}
	var b publishBody
	if err := decodeBody(w, r, &b, true); err != nil {
		return err
	}
	v, err := a.c.PublishSingleton(preconditionCtx(r), t.Name, content.PublishOptions{Locales: b.Locales, Force: b.Force})
	if err != nil {
		return err
	}
	writeView(w, http.StatusOK, v)
	return nil
}

// preconditionCtx applies If-Match: the write fails with 412 unless the item's ETag still matches. "*" and no
// header mean no check.
func preconditionCtx(r *http.Request) context.Context {
	v := strings.TrimSpace(r.Header.Get("If-Match"))
	if v == "" || v == "*" {
		return r.Context()
	}
	v = strings.TrimPrefix(v, "W/")
	return content.WithExpectedState(r.Context(), strings.Trim(v, `"`))
}

func writeView(w http.ResponseWriter, status int, v *content.View) {
	w.Header().Set("ETag", `"`+content.StateOf(v.UpdatedAt)+`"`)
	WriteJSON(w, status, v)
}
