package manage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
)

func openapiDoc(t testing.TB, f *fake) (map[string]any, []byte) {
	t.Helper()
	h := newAPI(t, f)
	w := do(h, "GET", "/openapi.json", "")
	if w.Code != 200 {
		t.Fatalf("status %d", w.Code)
	}
	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc, w.Body.Bytes()
}

func TestOpenAPIServed(t *testing.T) {
	f := newFake(t)
	h := newAPI(t, f)
	w := do(h, "GET", "/openapi.json", "")
	etag := w.Header().Get("ETag")
	if etag == "" || w.Header().Get("Content-Type") != jsonType {
		t.Fatalf("headers %v", w.Header())
	}
	w = do(h, "GET", "/openapi.json", "", "If-None-Match", etag)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Errorf("conditional: %d", w.Code)
	}
	if len(f.calls) != 0 {
		t.Error("service called")
	}
}

func TestOpenAPIPaths(t *testing.T) {
	doc, _ := openapiDoc(t, newFake(t))
	if doc["openapi"] != "3.1.0" {
		t.Errorf("version %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)
	for _, rt := range routes {
		item, ok := paths[rt.path].(map[string]any)
		if !ok {
			t.Errorf("missing path %s", rt.path)
			continue
		}
		op, ok := item[strings.ToLower(rt.method)].(map[string]any)
		if !ok {
			t.Errorf("missing %s %s", rt.method, rt.path)
			continue
		}
		// every {param} is declared
		for _, seg := range strings.Split(rt.path, "/") {
			if !strings.HasPrefix(seg, "{") {
				continue
			}
			name := strings.Trim(seg, "{}")
			if !declares(doc, op, name) {
				t.Errorf("%s %s does not declare %s", rt.method, rt.path, name)
			}
		}
	}
	// Per-type operations.
	for _, p := range []string{"/collections/article/items", "/collections/person/items", "/singletons/settings", "/singletons/settings/publish"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing %s", p)
		}
	}
	if _, ok := paths["/collections/settings/items"]; ok {
		t.Error("singleton listed as collection")
	}
	// A bearer token or the session cookie, global; only login opts out.
	schemes := doc["components"].(map[string]any)["securitySchemes"].(map[string]any)
	if schemes["bearerAuth"].(map[string]any)["scheme"] != "bearer" || schemes["cookieAuth"].(map[string]any)["in"] != "cookie" {
		t.Errorf("security %v", schemes)
	}
	if g, ok := doc["security"].([]any); !ok || len(g) != 2 {
		t.Errorf("global security %v", doc["security"])
	}
	for path, item := range paths {
		for method, op := range item.(map[string]any) {
			if _, ok := op.(map[string]any)["security"]; ok && path != "/auth/login" {
				t.Errorf("%s %s overrides security", method, path)
			}
		}
	}
}

func declares(doc map[string]any, op map[string]any, name string) bool {
	ps, _ := op["parameters"].([]any)
	for _, p := range ps {
		m := p.(map[string]any)
		if r, ok := m["$ref"].(string); ok {
			m = resolve(doc, r).(map[string]any)
		}
		if m["name"] == name && m["in"] == "path" {
			return true
		}
	}
	return false
}

func resolve(doc map[string]any, ref string) any {
	var cur any = doc
	for _, seg := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[seg]
	}
	return cur
}

func TestOpenAPIRefsResolve(t *testing.T) {
	doc, _ := openapiDoc(t, newFake(t))
	n := 0
	var walk func(v any, at string)
	walk = func(v any, at string) {
		switch x := v.(type) {
		case map[string]any:
			if r, ok := x["$ref"].(string); ok {
				n++
				if !strings.HasPrefix(r, "#/components/") || resolve(doc, r) == nil {
					t.Errorf("%s: unresolved %s", at, r)
				}
			}
			for k, c := range x {
				walk(c, at+"/"+k)
			}
		case []any:
			for i, c := range x {
				walk(c, fmt.Sprintf("%s/%d", at, i))
			}
		}
	}
	walk(doc, "")
	if n < 50 {
		t.Errorf("only %d refs checked", n)
	}
}

func TestOpenAPITypedSchemas(t *testing.T) {
	f := newFake(t)
	doc, _ := openapiDoc(t, f)
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"Error", "FieldError", "ListResult", "VersionMeta", "Referrer", "ItemView", "Summary"} {
		if _, ok := schemas[name]; !ok {
			t.Errorf("missing schema %s", name)
		}
	}
	for _, typ := range f.sc.Types() {
		want := f.sc.JSONSchema(typ)
		delete(want, "$schema")
		wb, _ := json.Marshal(want)
		gb, _ := json.Marshal(schemas["Fields_"+typ.Name])
		if !bytes.Equal(wb, gb) {
			t.Errorf("Fields_%s differs from the type's JSON Schema", typ.Name)
		}
		view := schemas["ItemView_"+typ.Name].(map[string]any)["properties"].(map[string]any)
		if view["fields"].(map[string]any)["$ref"] != "#/components/schemas/Fields_"+typ.Name {
			t.Errorf("ItemView_%s fields: %v", typ.Name, view["fields"])
		}
		if view["type"].(map[string]any)["const"] != typ.Name {
			t.Errorf("ItemView_%s type: %v", typ.Name, view["type"])
		}
		patch := schemas["Patch_"+typ.Name].(map[string]any)["properties"].(map[string]any)
		for fname, p := range patch {
			if len(p.(map[string]any)["anyOf"].([]any)) != 2 {
				t.Errorf("Patch_%s.%s not nullable", typ.Name, fname)
			}
		}
	}
	paths := doc["paths"].(map[string]any)
	post := paths["/collections/article/items"].(map[string]any)["post"].(map[string]any)
	if post["operationId"] != "createArticle" {
		t.Errorf("operationId %v", post["operationId"])
	}
	body := post["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if body["$ref"] != "#/components/schemas/Create_article" {
		t.Errorf("body %v", body)
	}
	resp := post["responses"].(map[string]any)["201"].(map[string]any)
	if _, ok := resp["headers"].(map[string]any)["Location"]; !ok {
		t.Error("201 lacks Location header")
	}
	ids := map[string]bool{}
	for p, item := range paths {
		for m, op := range item.(map[string]any) {
			id := op.(map[string]any)["operationId"].(string)
			if ids[id] {
				t.Errorf("duplicate operationId %s (%s %s)", id, m, p)
			}
			ids[id] = true
		}
	}
}

func TestOpenAPIDeterministic(t *testing.T) {
	var first []byte
	for i := 0; i < 5; i++ {
		_, b := openapiDoc(t, newFake(t))
		if first == nil {
			first = b
		} else if !bytes.Equal(first, b) {
			t.Fatal("two builds differ")
		}
	}
	// And the exported function without a handler.
	a, _ := json.Marshal(OpenAPI(testSch(t)))
	b, _ := json.Marshal(OpenAPI(testSch(t)))
	if !bytes.Equal(a, b) {
		t.Fatal("OpenAPI output differs")
	}
}

func TestOpenAPIEmptySchema(t *testing.T) {
	s := testSchEmpty(t)
	b, err := json.Marshal(OpenAPI(s))
	if err != nil || !json.Valid(b) {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	paths := doc["paths"].(map[string]any)
	keys := make([]string, 0, len(paths))
	for k := range paths {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Contains(strings.Join(keys, " "), "/collections") {
		t.Errorf("collection paths without types: %v", keys)
	}
}
