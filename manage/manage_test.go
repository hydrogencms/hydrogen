package manage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

const token = "s3cret"

func newAPI(t testing.TB, f *fake, mod ...func(*Options)) http.Handler {
	t.Helper()
	o := Options{Content: f, Token: token}
	for _, m := range mod {
		m(&o)
	}
	h, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func do(h http.Handler, method, target, body string, hdr ...string) *httptest.ResponseRecorder {
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, strings.NewReader(body))
	}
	r.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			r.Header.Del(hdr[i])
		} else {
			r.Header.Set(hdr[i], hdr[i+1])
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var env struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || env.Error == nil {
		t.Fatalf("not an error envelope: %q (%v)", w.Body.String(), err)
	}
	return env.Error
}

func TestNewValidation(t *testing.T) {
	f := newFake(t)
	if _, err := New(Options{Content: f}); err == nil {
		t.Error("missing token accepted")
	}
	if _, err := New(Options{Token: "x"}); err == nil {
		t.Error("missing content accepted")
	}
	if _, err := New(Options{Content: &fake{}, Token: "x"}); err == nil {
		t.Error("nil schema accepted")
	}
}

func TestAuth(t *testing.T) {
	f := newFake(t)
	h := newAPI(t, f)
	for _, rt := range routes {
		if rt.policy == Public {
			continue
		}
		path := strings.NewReplacer("{type}", "article", "{id}", testID.String(), "{user}", testID.String()).Replace(rt.path)
		for _, auth := range []string{"", "Bearer nope", "Basic " + token, "Bearer", token} {
			w := do(h, rt.method, path, "", "Authorization", auth)
			if w.Code != 401 || w.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("%s %s auth=%q: %d %v", rt.method, rt.path, auth, w.Code, w.Header())
			}
			if errCode(t, w)["code"] != "unauthorized" {
				t.Errorf("code %v", w.Body)
			}
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("service called without auth: %v", f.calls)
	}
}

func TestActorInContext(t *testing.T) {
	f := newFake(t)
	var got audit.Actor
	cf := &ctxFake{fake: f, on: func(a audit.Actor) { got = a }}
	h := newAPI(t, f, func(o *Options) { o.Content = cf })
	do(h, "GET", "/items/"+testID.String(), "")
	if got.Kind != "token" || got.ID != "config" {
		t.Errorf("actor %+v", got)
	}
}

func TestRoutes(t *testing.T) {
	id := testID.String()
	f := newFake(t)
	f.versions = []store.VersionMeta{{Version: 2, SchemaRev: 1, Locales: []string{"en"}}, {Version: 1}}
	f.ver = &content.VersionView{Version: 2, Shared: content.VersionDoc{Fields: map[string]any{}},
		Locales: map[string]content.VersionDoc{"en": {Name: "Hello", Fields: map[string]any{"title": "Hello"}}}}
	f.refs = []content.Referrer{{ItemID: testID, Type: "article", Name: "x", FieldPath: "author", Kind: "field", OnDelete: "block"}}
	f.list = &content.ListResult{Items: []content.Summary{{ID: testID, Type: "article", Name: "Hello"}}, Total: 7}
	h := newAPI(t, f)

	tests := []struct {
		name, method, path, body string
		status                   int
		call                     string
		check                    func(t *testing.T, w *httptest.ResponseRecorder, c call)
	}{
		{"types", "GET", "/types", "", 200, "", func(t *testing.T, w *httptest.ResponseRecorder, _ call) {
			var ts []map[string]any
			mustJSON(t, w, &ts)
			if len(ts) != 3 || ts[0]["name"] != "article" || ts[2]["singleton"] != true || ts[0]["fields"] == nil {
				t.Errorf("types %v", ts)
			}
		}},
		{"type", "GET", "/types/article", "", 200, "", func(t *testing.T, w *httptest.ResponseRecorder, _ call) {
			var m map[string]map[string]any
			mustJSON(t, w, &m)
			if m["type"]["name"] != "article" || m["jsonSchema"]["type"] != "object" {
				t.Errorf("type %v", m)
			}
			for _, f := range m["type"]["fields"].([]any) {
				if f := f.(map[string]any); f["name"] == "seo" && (f["group"] != "seo" || len(f["fields"].([]any)) != 1) {
					t.Errorf("seo field %v", f)
				}
			}
		}},
		{"type unknown", "GET", "/types/nope", "", 404, "", nil},
		{"list", "GET", "/collections/article/items", "", 200, "List", func(t *testing.T, w *httptest.ResponseRecorder, c call) {
			o := c.arg.(content.ListOptions)
			if o.Type != "article" || o.Limit != 50 || o.Status != store.StatusAny {
				t.Errorf("opts %+v", o)
			}
			var m map[string]any
			mustJSON(t, w, &m)
			if m["total"] != float64(7) || len(m["items"].([]any)) != 1 {
				t.Errorf("list %v", m)
			}
		}},
		{"list singleton type", "GET", "/collections/settings/items", "", 404, "", nil},
		{"list unknown type", "GET", "/collections/nope/items", "", 404, "", nil},
		{"create", "POST", "/collections/article/items", `{"locale":"de","key":"k","alwaysAvailable":true,"fields":{"title":"T","rating":3}}`,
			201, "Create", func(t *testing.T, w *httptest.ResponseRecorder, c call) {
				in := c.arg.(content.CreateInput)
				if in.Type != "article" || in.Locale != "de" || in.Key != "k" || !in.AlwaysAvailable {
					t.Errorf("input %+v", in)
				}
				if n, ok := in.Fields["rating"].(json.Number); !ok || n != "3" {
					t.Errorf("number not kept exact: %#v", in.Fields["rating"])
				}
			}},
		{"create no fields", "POST", "/collections/article/items", `{"locale":"en"}`, 201, "Create",
			func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
				if c.arg.(content.CreateInput).Fields == nil {
					t.Error("nil fields")
				}
			}},
		{"create on singleton", "POST", "/collections/settings/items", `{}`, 404, "", nil},
		{"get", "GET", "/items/" + id + "?locale=de&draft=true", "", 200, "Get", func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
			if c.id != testID || c.arg != (content.GetOptions{Locale: "de", Draft: true}) {
				t.Errorf("call %+v", c)
			}
		}},
		{"get bad draft", "GET", "/items/" + id + "?draft=maybe", "", 400, "", nil},
		{"get bad id", "GET", "/items/not-a-uuid", "", 404, "", nil},
		{"patch", "PATCH", "/items/" + id, `{"locale":"en","fields":{"title":"x","rating":null},"key":"k2","alwaysAvailable":false}`,
			200, "Update", func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
				in := c.arg.(content.UpdateInput)
				v, present := in.Fields["rating"]
				if !present || v != nil {
					t.Errorf("null dropped: %#v", in.Fields)
				}
				if in.Fields["title"] != "x" || in.Key == nil || *in.Key != "k2" || in.AlwaysAvailable == nil || *in.AlwaysAvailable {
					t.Errorf("input %+v", in)
				}
			}},
		{"patch bad id", "PATCH", "/items/zzz", `{"fields":{}}`, 404, "", nil},
		{"publish body", "POST", "/items/" + id + "/publish", `{"locales":["en","de"],"force":true}`, 200, "Publish",
			func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
				want := content.PublishOptions{Locales: []string{"en", "de"}, Force: true}
				if !reflect.DeepEqual(c.arg, want) {
					t.Errorf("opts %+v", c.arg)
				}
			}},
		{"publish no body", "POST", "/items/" + id + "/publish", "", 200, "Publish", nil},
		{"discard all", "DELETE", "/items/" + id + "/draft", "", 204, "Discard", func(t *testing.T, w *httptest.ResponseRecorder, c call) {
			if c.arg.([]string) != nil || w.Body.Len() != 0 {
				t.Errorf("parts %v body %q", c.arg, w.Body)
			}
		}},
		{"discard parts", "DELETE", "/items/" + id + "/draft?parts=shared,de", "", 204, "Discard", func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
			if !reflect.DeepEqual(c.arg, []string{"shared", "de"}) {
				t.Errorf("parts %v", c.arg)
			}
		}},
		{"discard empty part", "DELETE", "/items/" + id + "/draft?parts=shared,", "", 400, "", nil},
		{"delete", "DELETE", "/items/" + id, "", 204, "Delete", nil},
		{"versions", "GET", "/items/" + id + "/versions", "", 200, "Versions", func(t *testing.T, w *httptest.ResponseRecorder, _ call) {
			var m struct {
				Items []map[string]any `json:"items"`
				Total int              `json:"total"`
			}
			mustJSON(t, w, &m)
			if m.Total != 2 || m.Items[0]["version"] != float64(2) || m.Items[0]["schemaRev"] != float64(1) || m.Items[1]["locales"] == nil {
				t.Errorf("versions %v", m)
			}
		}},
		{"version", "GET", "/items/" + id + "/versions/2", "", 200, "VersionContent", func(t *testing.T, w *httptest.ResponseRecorder, c call) {
			if c.arg != 2 || !strings.Contains(w.Body.String(), `"locales":{"en":{"name":"Hello","fields":{"title":"Hello"}}}`) {
				t.Errorf("version %v %s", c.arg, w.Body)
			}
		}},
		{"version bad number", "GET", "/items/" + id + "/versions/x", "", 404, "", nil},
		{"list fields", "GET", "/collections/article/items?fields=title,%20rating", "", 200, "List", func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
			if o := c.arg.(content.ListOptions); len(o.Fields) != 2 || o.Fields[1] != "rating" {
				t.Errorf("fields %v", o.Fields)
			}
		}},
		{"list fields empty name", "GET", "/collections/article/items?fields=title,", "", 400, "", nil},
		{"references", "GET", "/items/" + id + "/references", "", 200, "References", func(t *testing.T, w *httptest.ResponseRecorder, _ call) {
			if !strings.Contains(w.Body.String(), `"field":"author"`) {
				t.Errorf("refs %s", w.Body)
			}
		}},
		{"singleton get", "GET", "/singletons/settings?locale=en", "", 200, "Singleton", func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
			if c.typ != "settings" || c.arg != (content.GetOptions{Locale: "en"}) {
				t.Errorf("call %+v", c)
			}
		}},
		{"singleton on collection", "GET", "/singletons/article", "", 404, "", nil},
		{"singleton patch", "PATCH", "/singletons/settings", `{"fields":{"siteName":"S","x":null}}`, 200, "UpdateSingleton",
			func(t *testing.T, _ *httptest.ResponseRecorder, c call) {
				in := c.arg.(content.UpdateInput)
				if v, ok := in.Fields["x"]; !ok || v != nil || c.typ != "settings" {
					t.Errorf("call %+v", c)
				}
			}},
		{"singleton publish", "POST", "/singletons/settings/publish", `{}`, 200, "PublishSingleton", nil},
		{"singleton publish unknown", "POST", "/singletons/nope/publish", `{}`, 404, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f.calls = nil
			w := do(h, tc.method, tc.path, tc.body)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body)
			}
			if tc.status != 204 && !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json; charset=utf-8") {
				t.Errorf("content type %q", w.Header().Get("Content-Type"))
			}
			if tc.status >= 400 {
				errCode(t, w)
			}
			if tc.call == "" {
				if len(f.calls) != 0 {
					t.Errorf("unexpected calls %v", f.calls)
				}
				return
			}
			c := f.last()
			if c.name != tc.call {
				t.Fatalf("call %q, want %q", c.name, tc.call)
			}
			if tc.check != nil {
				tc.check(t, w, c)
			}
		})
	}
}

func mustJSON(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("%v: %s", err, w.Body)
	}
}

func TestCreateLocation(t *testing.T) {
	f := newFake(t)
	mounted := http.StripPrefix("/api/manage/v1", newAPI(t, f, func(o *Options) { o.Prefix = "/api/manage/v1/" }))
	h := newAPI(t, f)
	w := do(mounted, "POST", "/api/manage/v1/collections/article/items", `{}`)
	if got, want := w.Header().Get("Location"), "/api/manage/v1/items/"+testID.String(); got != want {
		t.Errorf("Location %q, want %q", got, want)
	}
	w = do(h, "POST", "/collections/article/items?x=1", `{}`)
	if got, want := w.Header().Get("Location"), "/items/"+testID.String(); got != want {
		t.Errorf("unmounted Location %q, want %q", got, want)
	}
}

func TestBodyRules(t *testing.T) {
	f := newFake(t)
	h := newAPI(t, f, func(o *Options) { o.MaxBody = 64 })
	id := testID.String()
	tests := []struct {
		name, body, ct string
		status         int
		code           string
	}{
		{"unknown key", `{"fields":{},"bogus":1}`, "application/json", 400, "bad_request"},
		{"trailing data", `{"fields":{}} {}`, "application/json", 400, "bad_request"},
		{"trailing garbage", `{"fields":{}} x`, "application/json", 400, "bad_request"},
		{"not json", `nope`, "application/json", 400, "bad_request"},
		{"wrong type", `{"fields":[]}`, "application/json", 400, "bad_request"},
		{"too large", `{"fields":{"title":"` + strings.Repeat("a", 100) + `"}}`, "application/json", 413, "too_large"},
		{"content type", `{"fields":{}}`, "text/plain", 415, "unsupported_media_type"},
		{"no content type", `{"fields":{}}`, "-", 415, "unsupported_media_type"},
		{"json suffix ok", `{"fields":{}}`, "application/merge-patch+json", 200, ""},
		{"charset ok", `{"fields":{}}`, "application/json; charset=utf-8", 200, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ct := tc.ct
			if ct == "-" {
				ct = ""
			}
			w := do(h, "PATCH", "/items/"+id, tc.body, "Content-Type", ct)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if tc.code != "" && errCode(t, w)["code"] != tc.code {
				t.Errorf("code %v", w.Body)
			}
		})
	}
	// An empty body on a body-required route.
	r := httptest.NewRequest("PATCH", "/items/"+id, strings.NewReader(" "))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Errorf("empty body: %d", w.Code)
	}
	// Empty bodies, chunked or not, are empty rather than a media type error.
	for _, cl := range []int64{0, -1} {
		r := httptest.NewRequest("POST", "/items/"+id+"/publish", io.NopCloser(strings.NewReader("")))
		r.ContentLength = cl
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Errorf("empty optional body (length %d): %d %s", cl, w.Code, w.Body)
		}
		r = httptest.NewRequest("PATCH", "/items/"+id, io.NopCloser(strings.NewReader("")))
		r.ContentLength = cl
		r.Header.Set("Authorization", "Bearer "+token)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Errorf("empty required body (length %d): %d", cl, w.Code)
		}
	}
	// Publish accepts an absent body but still checks a present one.
	if w := do(h, "POST", "/items/"+id+"/publish", `{"force":"yes"}`); w.Code != 400 {
		t.Errorf("bad publish body: %d", w.Code)
	}
	// Default limit is 4 MiB.
	h2 := newAPI(t, f)
	big := `{"fields":{"title":"` + strings.Repeat("a", 5<<20) + `"}}`
	if w := do(h2, "PATCH", "/items/"+id, big); w.Code != 413 {
		t.Errorf("default limit: %d", w.Code)
	}
}

func TestListQuery(t *testing.T) {
	tests := []struct {
		name, query string
		want        content.ListOptions
		bad         bool
	}{
		{name: "defaults", want: content.ListOptions{Type: "article", Limit: 50}},
		{name: "all", query: "locale=de&status=draft&q=hello+world&sort=-rating,title&limit=10&offset=20",
			want: content.ListOptions{Type: "article", Locale: "de", Status: store.StatusDraft, Search: "hello world",
				Sort:  []content.SortSpec{{Field: "rating", Desc: true}, {Field: "title"}},
				Limit: 10, Offset: 20}},
		{name: "published", query: "status=published", want: content.ListOptions{Type: "article", Status: store.StatusPublished, Limit: 50}},
		{name: "any", query: "status=any", want: content.ListOptions{Type: "article", Limit: 50}},
		{name: "where", query: "where[rating][gte]=3&where[rating][lt]=5&where[author]=abc&where[tags][in]=a,b&where[title][exists]=true",
			want: content.ListOptions{Type: "article", Limit: 50, Where: []content.Filter{
				{Field: "author", Op: store.OpEq, Value: "abc"},
				{Field: "rating", Op: store.OpGte, Value: "3"},
				{Field: "rating", Op: store.OpLt, Value: "5"},
				{Field: "tags", Op: store.OpIn, Value: "a,b"},
				{Field: "title", Op: store.OpExists, Value: true},
			}}},
		{name: "repeated where", query: "where[tags][contains]=a&where[tags][contains]=b",
			want: content.ListOptions{Type: "article", Limit: 50, Where: []content.Filter{
				{Field: "tags", Op: store.OpContains, Value: "a"}, {Field: "tags", Op: store.OpContains, Value: "b"}}}},
		{name: "limit max", query: "limit=500", want: content.ListOptions{Type: "article", Limit: 500}},
		{name: "bad status", query: "status=gone", bad: true},
		{name: "limit zero", query: "limit=0", bad: true},
		{name: "limit high", query: "limit=501", bad: true},
		{name: "limit text", query: "limit=ten", bad: true},
		{name: "offset negative", query: "offset=-1", bad: true},
		{name: "offset huge", query: "offset=99999999", bad: true},
		{name: "bad op", query: "where[rating][like]=3", bad: true},
		{name: "empty field", query: "where[][eq]=3", bad: true},
		{name: "unclosed", query: "where[rating=3", bad: true},
		{name: "extra bracket", query: "where[rating][eq][x]=3", bad: true},
		{name: "bad exists", query: "where[rating][exists]=maybe", bad: true},
		{name: "empty sort field", query: "sort=a,,b", bad: true},
		{name: "unknown param", query: "page=2", bad: true},
		{name: "repeated limit", query: "limit=1&limit=2", bad: true},
		{name: "repeated status", query: "status=draft&status=any", bad: true},
		{name: "33 filters", query: strings.Repeat("where[rating][gte]=1&", 33), bad: true},
		{name: "1001 in values", query: "where[tags][in]=" + strings.Repeat("a,", 1000) + "a", bad: true},
		{name: "17 search terms", query: "q=" + strings.Repeat("a+", 16) + "a", bad: true},
		{name: "5 sort keys", query: "sort=a,b,c,d,e", bad: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			h := newAPI(t, f)
			w := do(h, "GET", "/collections/article/items?"+tc.query, "")
			if tc.bad {
				if w.Code != 400 || errCode(t, w)["code"] != "bad_request" || len(f.calls) != 0 {
					t.Fatalf("%d %s calls=%v", w.Code, w.Body, f.calls)
				}
				return
			}
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if got := f.last().arg; !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
			if !strings.Contains(w.Body.String(), `"items":[]`) {
				t.Errorf("nil items not encoded as []: %s", w.Body)
			}
		})
	}
}

func TestErrorMapping(t *testing.T) {
	refs := []content.Referrer{{ItemID: testID, FieldPath: "author", Type: "article"}}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
		extra  string
	}{
		{"unknown type", fmt.Errorf("wrap: %w", content.ErrUnknownType), 404, "not_found", ""},
		{"not found", content.ErrNotFound, 404, "not_found", ""},
		{"store not found", store.ErrNotFound, 404, "not_found", ""},
		{"bad request", fmt.Errorf("%w: locale", content.ErrBadRequest), 400, "bad_request", ""},
		{"validation", &content.ValidationError{Locale: "de", Errors: []schema.FieldError{{Path: "title", Code: "required", Message: "x"}}},
			422, "invalid", `"fields":[{"path":"title","code":"required","message":"x"}]`},
		{"validation locale", &content.ValidationError{Locale: "de", Errors: []schema.FieldError{{Path: "t"}}}, 422, "invalid", `"locale":"de"`},
		{"conflict", &content.ConflictError{Parts: []string{"shared", "de"}}, 409, "conflict", `"parts":["shared","de"]`},
		{"referenced", &content.ReferencedError{Referrers: refs}, 409, "referenced", `"referrers":[{"id":"` + testID.String()},
		{"duplicate", content.ErrDuplicateKey, 409, "duplicate_key", ""},
		{"store duplicate", fmt.Errorf("x: %w", store.ErrDuplicateKey), 409, "duplicate_key", ""},
		{"plain invalid", content.ErrInvalid, 422, "invalid", ""},
		{"plain conflict", content.ErrConflict, 409, "conflict", ""},
		{"plain referenced", content.ErrReferenced, 409, "referenced", ""},
		{"internal", errors.New("pq: password=hunter2"), 500, "internal", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t)
			f.err = tc.err
			var logs strings.Builder
			h := newAPI(t, f, func(o *Options) { o.Log = newTextLog(&logs) })
			w := do(h, "PATCH", "/items/"+testID.String(), `{"fields":{}}`)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			e := errCode(t, w)
			if e["code"] != tc.code {
				t.Errorf("code %v", e["code"])
			}
			if tc.extra != "" && !strings.Contains(w.Body.String(), tc.extra) {
				t.Errorf("body %s lacks %s", w.Body, tc.extra)
			}
			if tc.status == 500 {
				if strings.Contains(w.Body.String(), "hunter2") {
					t.Error("internal error leaked")
				}
				if !strings.Contains(logs.String(), "hunter2") || !strings.Contains(logs.String(), "PATCH") {
					t.Errorf("log %q", logs.String())
				}
			}
		})
	}
}

func TestPanicRecovery(t *testing.T) {
	f := newFake(t)
	f.panics = true
	var logs strings.Builder
	h := newAPI(t, f, func(o *Options) { o.Log = newTextLog(&logs) })
	w := do(h, "GET", "/items/"+testID.String(), "")
	if w.Code != 500 || errCode(t, w)["code"] != "internal" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !strings.Contains(logs.String(), "boom") || !strings.Contains(logs.String(), "level=ERROR") ||
		!strings.Contains(logs.String(), "stack=") || !strings.Contains(logs.String(), "goroutine") {
		t.Errorf("log %q", logs.String())
	}
}

func TestTypesPayload(t *testing.T) {
	f := newFake(t)
	h := newAPI(t, f)
	w := do(h, "GET", "/types/article", "")
	var m struct {
		Type       map[string]any `json:"type"`
		JSONSchema map[string]any `json:"jsonSchema"`
	}
	mustJSON(t, w, &m)
	sc := f.sc
	art, _ := sc.Type("article")
	want, _ := json.Marshal(sc.JSONSchema(art))
	got, _ := json.Marshal(m.JSONSchema)
	if string(want) != string(got) {
		t.Errorf("jsonSchema differs:\n%s\n%s", got, want)
	}
	fields := m.Type["fields"].([]any)
	if len(fields) != len(art.Fields) || fields[0].(map[string]any)["name"] != "title" {
		t.Errorf("fields %v", fields)
	}
	w = do(h, "GET", "/types", "")
	var ts []map[string]any
	mustJSON(t, w, &ts)
	for _, k := range []string{"name", "label", "description", "singleton", "page", "fields"} {
		if _, ok := ts[0][k]; !ok {
			t.Errorf("summary lacks %s", k)
		}
	}
	var one map[string]any
	if w = do(h, "GET", "/types/article", ""); w.Code == 200 {
		var doc struct {
			Type map[string]any `json:"type"`
		}
		mustJSON(t, w, &doc)
		one = doc.Type
	}
	for _, k := range []string{"admin", "namePattern", "options"} {
		for _, s := range ts {
			if s["name"] == "article" && fmt.Sprint(s[k]) != fmt.Sprint(one[k]) {
				t.Errorf("summary %s = %v, type document has %v", k, s[k], one[k])
			}
		}
	}
}

func TestWrongMethodIsNot200(t *testing.T) {
	h := newAPI(t, newFake(t))
	if w := do(h, "PUT", "/items/"+testID.String(), `{}`); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT: %d", w.Code)
	}
}

func TestPreconditionFailed(t *testing.T) {
	f := newFake(t)
	f.err = &content.StateMismatchError{State: "2026-10-14T12:00:00Z", UpdatedAt: testView().UpdatedAt, UpdatedBy: "user:x"}
	h := newAPI(t, f)
	w := do(h, "PATCH", "/items/"+testID.String(), `{"fields":{}}`)
	if w.Code != 412 || w.Header().Get("ETag") != `"2026-10-14T12:00:00Z"` ||
		!strings.Contains(w.Body.String(), `"precondition_failed"`) || !strings.Contains(w.Body.String(), `"updatedBy":"user:x"`) {
		t.Errorf("412: %d %v %s", w.Code, w.Header(), w.Body)
	}
	f.err = nil
	if w = do(h, "GET", "/items/"+testID.String(), ""); w.Header().Get("ETag") != `"2026-10-14T12:00:00Z"` {
		t.Errorf("get etag: %v", w.Header())
	}
}
