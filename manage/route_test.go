package manage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/schema"
)

func moduleRoutes() []Route {
	return []Route{
		{Method: "GET", Path: "/pages/sites/{site}/tree", Summary: "Site tree", Module: "pages", Policy: Authenticated,
			Handler: func(w http.ResponseWriter, r *http.Request) {
				WriteJSON(w, http.StatusOK, map[string]any{"site": r.PathValue("site")})
			}},
		{Method: "POST", Path: "/pages/move", Summary: "Move a page", Module: "pages", Policy: Authenticated,
			OpenAPI: map[string]any{"operationId": "movePage", "responses": map[string]any{"204": map[string]any{"description": "Moved."}}},
			Handler: func(w http.ResponseWriter, r *http.Request) {
				var in struct {
					To string `json:"to"`
				}
				if err := DecodeJSON(w, r, &in); err != nil {
					Fail(w, r, err)
					return
				}
				switch in.To {
				case "taken":
					Fail(w, r, Conflict("path_taken", "path %q is taken", in.To))
				case "gone":
					Fail(w, r, NotFound("no such page"))
				case "bad":
					Fail(w, r, &Error{Status: 422, Code: "invalid", Message: "bad", Fields: []schema.FieldError{{Path: "to", Code: "x", Message: "m"}}})
				case "invalid":
					Fail(w, r, &content.ValidationError{Errors: []schema.FieldError{{Path: "a", Code: "required", Message: "m"}}})
				case "boom":
					Fail(w, r, errors.New("secret detail"))
				case "panic":
					panic("kaboom")
				default:
					w.WriteHeader(http.StatusNoContent)
				}
			}},
	}
}

func withRoutes(rts ...Route) func(*Options) {
	return func(o *Options) { o.Routes = rts }
}

func TestModuleRouteAuth(t *testing.T) {
	h := newAPI(t, newFake(t), withRoutes(moduleRoutes()...))
	if w := do(h, "GET", "/pages/sites/main/tree", ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"site":"main"`) {
		t.Errorf("%d %s", w.Code, w.Body)
	}
	w := do(h, "GET", "/pages/sites/main/tree", "", "Authorization", "")
	if w.Code != 401 || errCode(t, w)["code"] != "unauthorized" || w.Header().Get("WWW-Authenticate") == "" {
		t.Errorf("no token: %d %s", w.Code, w.Body)
	}
	if w := do(h, "GET", "/pages/sites/main/tree", "", "Authorization", "Bearer nope"); w.Code != 401 {
		t.Errorf("bad token: %d", w.Code)
	}
	if w := do(h, "DELETE", "/pages/sites/main/tree", ""); w.Code != 405 {
		t.Errorf("method: %d", w.Code)
	}
}

func TestModuleRouteDecodeRules(t *testing.T) {
	h := newAPI(t, newFake(t), withRoutes(moduleRoutes()...), func(o *Options) { o.MaxBody = 64 })
	tests := []struct {
		name, body, ct string
		code           int
		errCode        string
	}{
		{"ok", `{"to":"/x"}`, "application/json", 204, ""},
		{"empty", ``, "application/json", 400, "bad_request"},
		{"media type", `{"to":"/x"}`, "text/plain", 415, "unsupported_media_type"},
		{"unknown field", `{"nope":1}`, "application/json", 400, "bad_request"},
		{"trailing", `{"to":"/x"} {}`, "application/json", 400, "bad_request"},
		{"too large", `{"to":"` + strings.Repeat("a", 100) + `"}`, "application/json", 413, "too_large"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdr := []string{"Content-Type", tc.ct}
			var w = do(h, "POST", "/pages/move", tc.body, hdr...)
			if tc.body == "" {
				w = do(h, "POST", "/pages/move", "")
			}
			if w.Code != tc.code {
				t.Fatalf("status %d: %s", w.Code, w.Body)
			}
			if tc.errCode != "" && errCode(t, w)["code"] != tc.errCode {
				t.Errorf("code %v", errCode(t, w))
			}
		})
	}
}

func TestModuleRouteFail(t *testing.T) {
	var logs bytes.Buffer
	h := newAPI(t, newFake(t), withRoutes(moduleRoutes()...), func(o *Options) { o.Log = newTextLog(&logs) })
	tests := []struct {
		to      string
		status  int
		errCode string
	}{
		{"taken", 409, "path_taken"}, {"gone", 404, "not_found"}, {"bad", 422, "invalid"},
		{"invalid", 422, "invalid"}, {"boom", 500, "internal"}, {"panic", 500, "internal"},
	}
	for _, tc := range tests {
		w := do(h, "POST", "/pages/move", fmt.Sprintf(`{"to":%q}`, tc.to))
		if w.Code != tc.status || errCode(t, w)["code"] != tc.errCode {
			t.Errorf("%s: %d %s", tc.to, w.Code, w.Body)
		}
		if strings.Contains(w.Body.String(), "secret detail") {
			t.Errorf("%s leaked the error", tc.to)
		}
	}
	w := do(h, "POST", "/pages/move", `{"to":"bad"}`)
	var env struct {
		Error struct{ Fields []schema.FieldError }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || len(env.Error.Fields) != 1 {
		t.Errorf("fields: %s", w.Body)
	}
	if !strings.Contains(logs.String(), "secret detail") || !strings.Contains(logs.String(), "kaboom") {
		t.Errorf("500s not logged: %s", logs.String())
	}
}

func TestModuleRouteConflicts(t *testing.T) {
	h := func(http.ResponseWriter, *http.Request) {}
	tests := []struct {
		name string
		rts  []Route
		want string
	}{
		{"core", []Route{{Method: "GET", Path: "/types", Handler: h, Module: "evil", Policy: Authenticated}}, "module evil: route GET /types"},
		{"core wildcard", []Route{{Method: "GET", Path: "/items/{other}", Handler: h, Module: "evil", Policy: Authenticated}}, "module evil"},
		{"module", []Route{{Method: "GET", Path: "/a", Handler: h, Module: "one", Policy: Authenticated}, {Method: "GET", Path: "/a", Handler: h, Module: "two", Policy: Authenticated}}, "module two: route GET /a"},
		{"method", []Route{{Method: "TRACE", Path: "/a", Handler: h, Policy: Public}}, "method must be"},
		{"path", []Route{{Method: "GET", Path: "a", Handler: h, Policy: Public}}, "path must start with /"},
		{"handler", []Route{{Method: "GET", Path: "/a", Policy: Public}}, "Handler is required"},
		{"policy", []Route{{Method: "GET", Path: "/a", Handler: h, Module: "one"}}, "module one: route GET /a: Policy is required"},
	}
	for _, tc := range tests {
		_, err := New(Options{Content: newFake(t), Token: token, Routes: tc.rts})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
	// Different methods on one path are fine.
	if _, err := New(Options{Content: newFake(t), Token: token, Routes: []Route{
		{Method: "GET", Path: "/a", Handler: h, Policy: Authenticated}, {Method: "POST", Path: "/a", Handler: h, Policy: Authenticated}}}); err != nil {
		t.Error(err)
	}
}

func TestOpenAPIModuleRoutes(t *testing.T) {
	h := newAPI(t, newFake(t), withRoutes(moduleRoutes()...))
	w := do(h, "GET", "/openapi.json", "")
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	paths := doc["paths"].(map[string]any)
	get := paths["/pages/sites/{site}/tree"].(map[string]any)["get"].(map[string]any)
	if get["summary"] != "Site tree" || get["operationId"] != "getPagesSitesSiteTree" {
		t.Errorf("generic op: %v", get)
	}
	ps := get["parameters"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["name"] != "site" {
		t.Errorf("parameters: %v", ps)
	}
	if _, ok := get["responses"].(map[string]any)["401"]; !ok {
		t.Errorf("responses: %v", get["responses"])
	}
	post := paths["/pages/move"].(map[string]any)["post"].(map[string]any)
	if post["operationId"] != "movePage" || post["summary"] != "Move a page" {
		t.Errorf("custom op: %v", post)
	}
	if _, ok := post["responses"].(map[string]any)["204"]; !ok || len(post["responses"].(map[string]any)) != 1 {
		t.Errorf("custom responses: %v", post["responses"])
	}
	// Deterministic, and the route list does not alter the core document otherwise.
	w2 := do(newAPI(t, newFake(t), withRoutes(moduleRoutes()...)), "GET", "/openapi.json", "")
	if !bytes.Equal(w.Body.Bytes(), w2.Body.Bytes()) {
		t.Error("document differs between builds")
	}
	if rts := moduleRoutes(); rts[1].OpenAPI["operationId"] != "movePage" {
		t.Error("OpenAPI object was modified")
	}
}

func TestOpenAPIPathConversion(t *testing.T) {
	tests := map[string][2]string{
		"/a/{x}/b":      {"/a/{x}/b", "x"},
		"/files/{p...}": {"/files/{p}", "p"},
		"/exact/{$}":    {"/exact/", ""},
	}
	for in, want := range tests {
		got, names := openAPIPath(in)
		if got != want[0] || strings.Join(names, ",") != want[1] {
			t.Errorf("%s: %s %v", in, got, names)
		}
	}
}
