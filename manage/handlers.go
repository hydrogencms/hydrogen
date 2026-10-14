package manage

import (
	"net/http"
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
		return nil, notFound("unknown %s type %q", kind, name)
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
			return o, badRequest("draft must be true or false")
		}
	}
	return o, nil
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
	a.writeJSON(w, http.StatusOK, content.ListResult{Items: items, Total: res.Total})
	return nil
}

func (a *api) createItem(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, false)
	if err != nil {
		return err
	}
	var b createBody
	if err := a.decode(w, r, &b, false); err != nil {
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
	a.writeJSON(w, http.StatusCreated, v)
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
	a.writeJSON(w, http.StatusOK, v)
	return nil
}

func (a *api) updateItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var b updateBody
	if err := a.decode(w, r, &b, false); err != nil {
		return err
	}
	v, err := a.c.Update(r.Context(), id, b.input())
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, v)
	return nil
}

func (a *api) publishItem(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var b publishBody
	if err := a.decode(w, r, &b, true); err != nil {
		return err
	}
	v, err := a.c.Publish(r.Context(), id, content.PublishOptions{Locales: b.Locales, Force: b.Force})
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, v)
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
				return badRequest("parts has an empty entry")
			}
			parts = append(parts, p)
		}
	}
	if err := a.c.Discard(r.Context(), id, parts); err != nil {
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
	if err := a.c.Delete(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
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
	a.writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
	return nil
}

func (a *api) references(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	refs, err := a.c.References(r.Context(), id)
	if err != nil {
		return err
	}
	if refs == nil {
		refs = []content.Referrer{}
	}
	a.writeJSON(w, http.StatusOK, map[string]any{"items": refs, "total": len(refs)})
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
	a.writeJSON(w, http.StatusOK, v)
	return nil
}

func (a *api) updateSingleton(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, true)
	if err != nil {
		return err
	}
	var b updateBody
	if err := a.decode(w, r, &b, false); err != nil {
		return err
	}
	v, err := a.c.UpdateSingleton(r.Context(), t.Name, b.input())
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, v)
	return nil
}

func (a *api) publishSingleton(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, true)
	if err != nil {
		return err
	}
	var b publishBody
	if err := a.decode(w, r, &b, true); err != nil {
		return err
	}
	v, err := a.c.PublishSingleton(r.Context(), t.Name, content.PublishOptions{Locales: b.Locales, Force: b.Force})
	if err != nil {
		return err
	}
	a.writeJSON(w, http.StatusOK, v)
	return nil
}
