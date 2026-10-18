package manage

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store/storeutil"
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

// searchParams are the query parameters of GET /search.
const (
	defaultSearchLimit = 20
	maxSearchLimit     = content.MaxSearchLimit
	maxSearchText      = 200
)

// search finds items by text across types, as the principal may read them (working projection, drafts
// included): {"items":[summaries],"snippets":{id: html}}.
func (a *api) search(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	for k := range q {
		switch k {
		case "q", "type", "locale", "limit":
		default:
			return BadRequest("unknown query parameter %q", k)
		}
	}
	o := content.SearchOptions{}
	var err error
	if o.Text, err = single(q, "q"); err != nil {
		return err
	}
	if strings.TrimSpace(o.Text) == "" {
		return BadRequest("q is required")
	}
	if utf8.RuneCountInString(o.Text) > maxSearchText {
		return BadRequest("q is too long (at most %d characters)", maxSearchText)
	}
	if n := len(storeutil.SearchTerms(o.Text)); n > content.MaxSearchTerms {
		return BadRequest("too many search terms: %d (at most %d)", n, content.MaxSearchTerms)
	}
	if o.Locale, err = single(q, "locale"); err != nil {
		return err
	}
	if o.Limit, err = parseInt(q, "limit", defaultSearchLimit, 1, maxSearchLimit); err != nil {
		return err
	}
	raw, err := single(q, "type")
	if err != nil {
		return err
	}
	if raw == "" {
		for _, t := range a.sc.Types() {
			if !t.Singleton {
				o.Types = append(o.Types, t.Name)
			}
		}
	} else {
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			t, ok := a.sc.Type(name)
			if !ok || t.Singleton {
				return BadRequest("unknown collection type %q", name)
			}
			if !slices.Contains(o.Types, name) {
				o.Types = append(o.Types, name)
			}
		}
	}
	res := &content.SearchResult{}
	if len(o.Types) > 0 {
		if res, err = a.c.Search(r.Context(), o); err != nil {
			return err
		}
	}
	items := res.Items
	if items == nil {
		items = []content.Summary{}
	}
	snippets := make(map[string]string, len(res.Snippets))
	for id, s := range res.Snippets {
		snippets[id.String()] = s
	}
	WriteJSON(w, http.StatusOK, struct {
		Items    []content.Summary `json:"items"`
		Snippets map[string]string `json:"snippets"`
	}{items, snippets})
	return nil
}

func (a *api) facets(w http.ResponseWriter, r *http.Request) error {
	t, err := a.typeOf(r, false)
	if err != nil {
		return err
	}
	o, err := parseFacets(t.Name, r.URL.Query())
	if err != nil {
		return err
	}
	res, err := a.c.Facets(r.Context(), o)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, res)
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
	v, err := a.c.Update(collabCtx(r), id, b.input())
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
	v, err := a.c.Publish(collabCtx(r), id, content.PublishOptions{Locales: b.Locales, Force: b.Force})
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
	if err := a.c.Discard(collabCtx(r), id, parts); err != nil {
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
	v, err := a.c.UpdateSingleton(collabCtx(r), t.Name, b.input())
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
	v, err := a.c.PublishSingleton(collabCtx(r), t.Name, content.PublishOptions{Locales: b.Locales, Force: b.Force})
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

// CollabClientHeader names the request header with which the admin's editor identifies its co-editing session
// on draft saves, publishes and discards; the content events then carry it (content.WithCollabClient).
const CollabClientHeader = "X-Hydrogen-Collab-Client"

// CollabSeqHeader names the request header with which a collaboration client tells, on a draft save, the
// highest update seq it has applied (its own acknowledged updates included); the "saved" notification carries it.
const CollabSeqHeader = "X-Hydrogen-Collab-Seq"

// collabCtx is preconditionCtx for the draft writes of a collaboration participant.
func collabCtx(r *http.Request) context.Context {
	ctx := preconditionCtx(r)
	if c := r.Header.Get(CollabClientHeader); validCollabClient(c) {
		ctx = content.WithCollabClient(ctx, c)
		if n, err := strconv.ParseInt(r.Header.Get(CollabSeqHeader), 10, 64); err == nil && n > 0 {
			ctx = content.WithCollabSeq(ctx, n)
		}
	}
	return ctx
}

var collabClientRE = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func validCollabClient(c string) bool { return collabClientRE.MatchString(c) }

func writeView(w http.ResponseWriter, status int, v *content.View) {
	w.Header().Set("ETag", `"`+content.StateOf(v.UpdatedAt)+`"`)
	WriteJSON(w, status, v)
}
