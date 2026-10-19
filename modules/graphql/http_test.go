package graphql

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func extCode(r result) any { return path(r.errors(), 0, "extensions", "code") }

func TestLimits(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		e := newEnv(t, envOpts{app: "modules:\n  graphql: { max_depth: 3 }\n"})
		e.fixture()
		r := e.post("web", `{ article(key: "hello") { related { title } } }`, nil)
		r.noErrors(t)
		r = e.post("web", `{ article(key: "hello") { related { author { fullName } } } }`, nil)
		if r.Code != 400 || extCode(r) != "QUERY_TOO_DEEP" || r.data() != nil {
			t.Fatalf("deep: %d %s", r.Code, r.Body)
		}
		// fragments count
		r = e.post("web", `{ article(key: "hello") { ...a } } fragment a on Article { related { ...b } } fragment b on Article { author { fullName } }`, nil)
		if r.Code != 400 || extCode(r) != "QUERY_TOO_DEEP" {
			t.Fatalf("deep through fragments: %d %s", r.Code, r.Body)
		}
	})
	t.Run("fields", func(t *testing.T) {
		e := newEnv(t, envOpts{app: "modules:\n  graphql: { max_fields: 5 }\n"})
		e.fixture()
		r := e.post("web", `{ articleList { items { title slug summary } } }`, nil)
		r.noErrors(t)
		r = e.post("web", `{ articleList { items { title slug summary rating } } }`, nil)
		if r.Code != 400 || extCode(r) != "QUERY_TOO_LARGE" {
			t.Fatalf("too many: %d %s", r.Code, r.Body)
		}
		// the same fragment used twice counts twice
		r = e.post("web", `{ a: article(key: "hello") { ...f } b: article(key: "second") { ...f } } fragment f on Article { title slug }`, nil)
		if r.Code != 400 || extCode(r) != "QUERY_TOO_LARGE" {
			t.Fatalf("fragment twice: %d %s", r.Code, r.Body)
		}
		// an exponential fragment chain stops at the limit without blowing up
		var sb strings.Builder
		sb.WriteString(`{ articleList { items { ...f0 } } }`)
		for i := range 40 {
			fmt.Fprintf(&sb, " fragment f%d on Article { related { ...f%d } author { fullName } related2: related { ...f%d } }", i, i+1, i+1)
		}
		sb.WriteString(" fragment f40 on Article { title }")
		e2 := newEnv(t, envOpts{app: "modules:\n  graphql: { max_fields: 100, max_query_bytes: 65536, max_depth: 1000 }\n"})
		r = e2.post("web", sb.String(), nil)
		if r.Code != 400 {
			t.Fatalf("chain: %d %.200s", r.Code, r.Body)
		}
	})
	t.Run("bytes", func(t *testing.T) {
		e := newEnv(t, envOpts{app: "modules:\n  graphql: { max_query_bytes: 40 }\n"})
		r := e.post("web", `{ articleList { items { title } } }`, nil)
		r.noErrors(t)
		r = e.post("web", `{ articleList { items { title slug summary rating price } } }`, nil)
		if r.Code != 400 || extCode(r) != "QUERY_TOO_LARGE" {
			t.Fatalf("long: %d %s", r.Code, r.Body)
		}
	})
	t.Run("calls", func(t *testing.T) {
		e := newEnv(t, envOpts{app: "modules:\n  graphql: { max_calls: 2 }\n"})
		e.fixture()
		r := e.post("web", `{ a: article(key: "hello") { title } b: article(key: "second") { title } c: article(key: "third") { title } }`, nil)
		if r.Code != 200 || len(r.errors()) != 1 || path(r.errors(), 0, "extensions", "code") != "TOO_MANY_CALLS" {
			t.Fatalf("calls: %d %s", r.Code, r.Body)
		}
		// graphql-go may run the root fields at once: two of the three get their data
		got := 0
		for _, k := range []string{"a", "b", "c"} {
			if r.data()[k] != nil {
				got++
			}
		}
		if got != 2 {
			t.Errorf("%d root fields have data, want 2: %s", got, r.Body)
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("a response with errors is cached: %q", r.Header().Get("Cache-Control"))
		}
	})
	t.Run("introspection", func(t *testing.T) {
		on := newEnv(t, envOpts{})
		q := `{ __schema { queryType { name } types { name kind fields { name type { name kind ofType { name kind ofType { name kind ofType { name kind ofType { name } } } } } } } } }`
		r := on.post("web", q, nil)
		r.noErrors(t)
		eq(t, "query type", path(r.data(), "__schema", "queryType", "name"), "Query")
		r = on.post("web", `{ __type(name: "Article") { name interfaces { name } } }`, nil)
		r.noErrors(t)
		eq(t, "interface", path(r.data(), "__type", "interfaces", 0, "name"), "Item")

		off := newEnv(t, envOpts{app: "modules:\n  graphql: { introspection: false }\n"})
		for _, q := range []string{`{ __schema { queryType { name } } }`, `{ __type(name: "Article") { name } }`, `query { a: __schema { types { name } } }`} {
			r = off.post("web", q, nil)
			if r.Code != 400 || extCode(r) != "INTROSPECTION_DISABLED" {
				t.Fatalf("%s: %d %s", q, r.Code, r.Body)
			}
		}
		r = off.post("web", `{ articleList { __typename items { __typename } } }`, nil)
		r.noErrors(t)
	})
}

func TestConfigValidation(t *testing.T) {
	good := defaultConfig()
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	bad := Config{MaxDepth: 0, MaxFields: -1, MaxQueryBytes: 1 << 20, MaxCalls: 0}
	err := bad.validate()
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"modules.graphql.max_depth", "modules.graphql.max_fields", "modules.graphql.max_calls", "max_query_bytes must be at most"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	defaults := Module().(*module).cfg
	eq(t, "defaults", fmt.Sprint(defaults.MaxDepth, defaults.MaxFields, defaults.MaxQueryBytes, defaults.MaxCalls, defaults.Introspection), "10 500 16384 50 true")
}

func TestCacheHeadersAndTags(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()
	e.drain()
	q := `{ article(key: "hello") { title author { fullName } } personList { items { fullName } } }`

	r := e.get("web", q)
	r.noErrors(t)
	if cc := r.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "public") || !strings.Contains(cc, "s-maxage=") {
		t.Errorf("Cache-Control = %q", cc)
	}
	etag := r.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	sk := r.Header().Get("Surrogate-Key")
	for _, tag := range []string{"i:" + f.a1.String(), "i:" + f.alice.String(), "c:person"} {
		if !strings.Contains(" "+sk+" ", " "+tag+" ") {
			t.Errorf("Surrogate-Key %q lacks %s", sk, tag)
		}
	}
	if r.Header().Get("Cache-Tag") == "" {
		t.Error("no Cache-Tag")
	}

	again := e.get("web", q)
	eq(t, "same etag", again.Header().Get("ETag"), etag)
	nm := e.do("GET", base+"?channel=web&query="+urlEscape(q), map[string]string{"If-None-Match": etag}, "")
	if nm.Code != http.StatusNotModified {
		t.Errorf("If-None-Match: %d", nm.Code)
	}

	// publishing an item the answer shows through a reference purges the cached answer
	e.update(f.alice, "en", `{"fullName":"Alice B"}`)
	e.publish(f.alice)
	e.drain()
	r = e.get("web", q)
	r.noErrors(t)
	eq(t, "refreshed author", path(r.data(), "article", "author", "fullName"), "Alice B")
	if r.Header().Get("ETag") == etag {
		t.Error("etag did not change")
	}
	// and a new person purges the listing
	e.create("person", "carl", "en", `{"fullName":"Carl"}`, true)
	e.drain()
	r = e.get("web", q)
	eq(t, "list size", len(path(r.data(), "personList", "items").([]any)), 3)

	// POST is never cached
	p := e.post("web", q, nil)
	p.noErrors(t)
	if p.Header().Get("Cache-Control") != "no-store" || p.Header().Get("ETag") != "" {
		t.Errorf("POST headers: %v", p.Header())
	}

	// errors in the answer are never cached
	bad := e.get("web", `{ article { title } }`)
	if bad.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("answer with errors: %v", bad.Header())
	}
	// neither are request errors
	bad = e.get("web", `{ nope }`)
	if bad.Code != 400 || bad.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("validation error: %d %v", bad.Code, bad.Header())
	}
}

func TestCollapsedTags(t *testing.T) {
	e := newEnv(t, envOpts{})
	for i := range 70 {
		e.create("company", "", "en", fmt.Sprintf(`{"title":"Co %d"}`, i), true)
	}
	r := e.get("web", `{ companyList(limit: 100) { items { title } } }`)
	r.noErrors(t)
	sk := strings.Fields(r.Header().Get("Surrogate-Key"))
	if len(sk) > 70 {
		t.Errorf("%d tags are not collapsed", len(sk))
	}
	if !strings.Contains(r.Header().Get("Surrogate-Key"), "c:company") {
		t.Errorf("Surrogate-Key = %s", r.Header().Get("Surrogate-Key"))
	}
}

func TestPreview(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()
	e.update(f.a1, "en", `{"title":"Hello draft"}`)
	tok := e.manage("POST", "/preview-tokens", map[string]any{"channel": "web", "item": f.a1.String()})
	if tok.Code != 200 && tok.Code != 201 {
		t.Fatalf("token: %d %s", tok.Code, tok.Body)
	}
	token, _ := tok.json()["token"].(string)

	q := `{ article(key: "hello") { title } }`
	pub := e.get("web", q)
	eq(t, "published", path(pub.data(), "article", "title"), "Hello")

	for name, r := range map[string]result{
		"GET param":   e.get("web", q, "preview="+token),
		"POST param":  e.post("web", q, nil, "preview="+token),
		"POST header": e.do("POST", base+"?channel=web", map[string]string{"X-Hydrogen-Preview": token, "Content-Type": "application/json"}, `{"query":"`+strings.ReplaceAll(q, `"`, `\"`)+`"}`),
	} {
		r.noErrors(t)
		if got := path(r.data(), "article", "title"); got != "Hello draft" {
			t.Errorf("%s: title = %v", name, got)
		}
		if r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q", name, r.Header().Get("Cache-Control"))
		}
	}
	// the cached public answer is unaffected
	eq(t, "still published", path(e.get("web", q).data(), "article", "title"), "Hello")

	// a bad token is the delivery API's 401
	r := e.get("web", q, "preview=nope")
	if r.Code != 401 {
		t.Errorf("bad token: %d", r.Code)
	}
}

func TestTokenChannel(t *testing.T) {
	e := newEnv(t, envOpts{})
	e.fixture()
	q := `{ articleList { items { title } } }`
	r := e.do("POST", base+"?channel=app", map[string]string{"Content-Type": "application/json"}, `{"query":"`+q+`"}`)
	if r.Code != 401 {
		t.Fatalf("no token: %d %s", r.Code, r.Body)
	}
	r = e.do("GET", base+"?query="+urlEscape(q), map[string]string{"Authorization": "Bearer " + appToken}, "")
	r.noErrors(t)
	eq(t, "items", len(path(r.data(), "articleList", "items").([]any)), 3)
	if cc := r.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Errorf("Cache-Control = %q", cc)
	}
	r = e.do("POST", base, map[string]string{"Authorization": "Bearer " + appToken, "Content-Type": "application/json"}, `{"query":"`+q+`"}`)
	r.noErrors(t)
	// the channel's SDL needs the token too
	if got := e.do("GET", base+"/schema.graphql?channel=app", nil, "").Code; got != 401 {
		t.Errorf("sdl without token: %d", got)
	}
	if got := e.do("GET", base+"/schema.graphql", map[string]string{"Authorization": "Bearer " + appToken}, "").Code; got != 200 {
		t.Errorf("sdl with token: %d", got)
	}
}

func TestRequestShapes(t *testing.T) {
	e := newEnv(t, envOpts{})
	f := e.fixture()

	// GET with variables and operationName
	r := e.get("web", `query A($id: ID) { article(id: $id) { title } } query B { settings { siteName } }`,
		"operationName=A", "variables="+urlEscape(fmt.Sprintf(`{"id":%q}`, f.a1)))
	r.noErrors(t)
	eq(t, "A", path(r.data(), "article", "title"), "Hello")
	r = e.get("web", `query A { article(key: "hello") { title } } query B { settings { siteName } }`, "operationName=B")
	r.noErrors(t)
	eq(t, "B", path(r.data(), "settings", "siteName"), "Hydrogen")
	r = e.get("web", `query A { article(key: "hello") { title } } query B { settings { siteName } }`)
	if r.Code != 400 {
		t.Errorf("two operations without a name: %d", r.Code)
	}

	// POST with operationName and variables in the body
	r = e.do("POST", base+"?channel=web", map[string]string{"Content-Type": "application/json; charset=utf-8"},
		fmt.Sprintf(`{"query":"query A($id: ID){ article(id: $id){ title } } query B { settings { siteName } }","operationName":"A","variables":{"id":%q}}`, f.a1))
	r.noErrors(t)
	eq(t, "post A", path(r.data(), "article", "title"), "Hello")

	// application/graphql
	r = e.do("POST", base+"?channel=web", map[string]string{"Content-Type": "application/graphql"}, `{ settings { siteName } }`)
	r.noErrors(t)

	cases := []struct {
		name, method, path, ct, body string
		status                       int
	}{
		{"text/plain", "POST", base + "?channel=web", "text/plain", `{ settings { siteName } }`, 415},
		{"bad json", "POST", base + "?channel=web", "application/json", `{`, 400},
		{"unknown body key", "POST", base + "?channel=web", "application/json", `{"query":"{ settings { siteName } }","x":1}`, 400},
		{"batch", "POST", base + "?channel=web", "application/json", `[{"query":"{ settings { siteName } }"}]`, 400},
		{"trailing", "POST", base + "?channel=web", "application/json", `{"query":"{ settings { siteName } }"} {}`, 400},
		{"empty query", "POST", base + "?channel=web", "application/json", `{"query":" "}`, 400},
		{"too big", "POST", base + "?channel=web", "application/json", `{"query":"` + strings.Repeat("a", 70000) + `"}`, 413},
		{"parse error", "POST", base + "?channel=web", "application/json", `{"query":"{ article( }"}`, 400},
		{"unknown field", "POST", base + "?channel=web", "application/json", `{"query":"{ nope }"}`, 400},
		{"mutation", "POST", base + "?channel=web", "application/json", `{"query":"mutation { x }"}`, 400},
		{"bad variables", "GET", base + "?channel=web&query=%7Bsettings%7BsiteName%7D%7D&variables=%5B%5D", "", "", 400},
		{"unknown param", "GET", base + "?channel=web&query=%7Bsettings%7BsiteName%7D%7D&x=1", "", "", 400},
		{"missing variable type", "POST", base + "?channel=web", "application/json", `{"query":"query($id: ID!){ item(id: $id){ id } }"}`, 400},
		{"unknown channel", "POST", base + "?channel=zzz", "application/json", `{"query":"{ settings { siteName } }"}`, 400},
	}
	for _, c := range cases {
		hdr := map[string]string{}
		if c.ct != "" {
			hdr["Content-Type"] = c.ct
		}
		r := e.do(c.method, c.path, hdr, c.body)
		if r.Code != c.status {
			t.Errorf("%s: %d, want %d: %.200s", c.name, r.Code, c.status, r.Body)
		}
	}
	// the errors keep the GraphQL shape
	r = e.post("web", `{ article( }`, nil)
	if msg, _ := path(r.errors(), 0, "message").(string); msg == "" || path(r.errors(), 0, "locations") == nil {
		t.Errorf("parse error shape: %s", r.Body)
	}
	if r.json()["data"] != nil {
		t.Error("data in a request error")
	}

	// other methods
	if got := e.do("PUT", base+"?channel=web", nil, "{}").Code; got == 200 {
		t.Errorf("PUT: %d", got)
	}
}

func TestOpenAPI(t *testing.T) {
	e := newEnv(t, envOpts{})
	r := e.do("GET", "/api/deliver/v1/openapi.json", nil, "")
	doc := r.json()
	if path(doc, "paths", "/graphql", "get", "operationId") == nil || path(doc, "paths", "/graphql", "post", "operationId") == nil {
		t.Errorf("graphql operations: %v", path(doc, "paths", "/graphql"))
	}
	if path(doc, "paths", "/graphql/schema.graphql", "get") == nil {
		t.Error("sdl operation missing")
	}
}

func TestStartBuildsSchemas(t *testing.T) {
	e := newEnv(t, envOpts{})
	if err := e.mod.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"web", "lim", "app"} {
		if e.mod.schemas[name] == nil {
			t.Errorf("no schema for %s", name)
		}
	}
}

func TestIntrospectionIsBounded(t *testing.T) {
	e := newEnv(t, envOpts{})
	// possibleTypes inside itself multiplies the answer by the schema's size per level
	q := `{ __schema { types { possibleTypes { possibleTypes { possibleTypes { possibleTypes { name } } } } } } }`
	r := e.post("web", q, nil)
	if r.Code != 400 || extCode(r) != "INTROSPECTION_TOO_COMPLEX" {
		t.Fatalf("nested: %d %.200s", r.Code, r.Body)
	}
	deep := `{ __schema { types { fields { args { type` + strings.Repeat(` { ofType`, 8) + ` { name }` + strings.Repeat(` }`, 8) + ` } } } } }`
	r = e.post("web", deep, nil)
	if r.Code != 400 || extCode(r) != "QUERY_TOO_DEEP" {
		t.Fatalf("deep: %d %.200s", r.Code, r.Body)
	}
	small := newEnv(t, envOpts{app: "modules:\n  graphql: { max_fields: 5 }\n"})
	r = small.post("web", `{ __schema { types { name kind description fields { name } } } }`, nil)
	if r.Code != 400 || extCode(r) != "QUERY_TOO_LARGE" {
		t.Fatalf("large: %d %.200s", r.Code, r.Body)
	}
}

func TestSDLHiddenWithoutIntrospection(t *testing.T) {
	e := newEnv(t, envOpts{app: "modules:\n  graphql: { introspection: false }\n"})
	if r := e.do("GET", base+"/schema.graphql?channel=web", nil, ""); r.Code != 404 {
		t.Fatalf("sdl: %d", r.Code)
	}
}

func TestMissingItemIsNotCached(t *testing.T) {
	e := newEnv(t, envOpts{})
	q := `{ article(key: "later") { title } ghost: article(id: "018f0000-0000-7000-8000-000000000000") { title } personList(where: {fullName: "Zed"}) { items { fullName } } }`
	r := e.get("web", q)
	r.noErrors(t)
	if r.data()["article"] != nil {
		t.Fatalf("article = %v", r.data()["article"])
	}
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", r.Header().Get("Cache-Control"))
	}
	e.create("article", "later", "en", `{"title":"Later"}`, true)
	e.drain()
	r = e.get("web", q)
	eq(t, "published since", path(r.data(), "article", "title"), "Later")

	// an unpublished reference
	draft := e.create("person", "ghost", "en", `{"fullName":"Ghost"}`, false)
	e.create("article", "haunted", "en", fmt.Sprintf(`{"title":"Haunted","author":%q}`, draft), true)
	q = `{ article(key: "haunted") { author { fullName } } }`
	r = e.get("web", q)
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("absent ref cached: %q", r.Header().Get("Cache-Control"))
	}
	e.publish(draft)
	e.drain()
	eq(t, "ref published", path(e.get("web", q).data(), "article", "author", "fullName"), "Ghost")

	// a missing singleton
	r = e.get("web", `{ settings { siteName } }`)
	if r.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("missing singleton cached")
	}
}

func TestResponseBudget(t *testing.T) {
	e := newEnv(t, envOpts{app: "modules:\n  graphql: { max_nodes: 200 }\n"})
	a := e.create("article", "a", "en", `{"title":"A"}`, true)
	b := e.create("article", "b", "en", fmt.Sprintf(`{"title":"B","related":[%q,%q]}`, a, a), true)
	e.update(a, "en", fmt.Sprintf(`{"related":[%q,%q,%q,%q]}`, a, b, a, b))
	e.publish(a)
	// related fans out 4 ways and is cyclic: 4^depth objects from a single call
	cyc := `{ article(key: "a") {` + strings.Repeat(" related {", 7) + " title" + strings.Repeat(" }", 7) + ` } }`
	r := e.post("web", cyc, nil)
	if r.Code != 200 || len(r.errors()) == 0 || path(r.errors(), 0, "extensions", "code") != "QUERY_TOO_EXPENSIVE" {
		t.Fatalf("cyclic: %d %.300s", r.Code, r.Body)
	}
	if len(r.Body.Bytes()) > 1<<20 {
		t.Errorf("response is %d bytes", r.Body.Len())
	}
}

func TestItemBudgetAndLong(t *testing.T) {
	e := newEnv(t, envOpts{app: "modules:\n  graphql: { max_items: 150 }\n"})
	e.fixture()
	r := e.post("web", `{ a: articleList(limit: 100) { items { title } } b: articleList(limit: 100) { items { title } } }`, nil)
	if len(r.errors()) != 1 || path(r.errors(), 0, "extensions", "code") != "QUERY_TOO_EXPENSIVE" {
		t.Fatalf("items: %d %s", r.Code, r.Body)
	}
	// include depth weighs a page: 50 x (1+1)
	r = e.post("web", `{ articleList(limit: 80) { items { author { fullName } } } }`, nil)
	if len(r.errors()) != 1 {
		t.Fatalf("include weight: %s", r.Body)
	}
	if !strings.Contains(e.mod.sdlOf(t, "web"), "rating: Long") {
		t.Error("unbounded integer is not Long")
	}
	r = e.post("web", `{ imageList(limit: 5) { items { file { size } } } }`, nil)
	r.noErrors(t)
}

func TestStandardIntrospectionQuery(t *testing.T) {
	e := newEnv(t, envOpts{})
	q := `query IntrospectionQuery {
  __schema { queryType { name } mutationType { name } subscriptionType { name }
    types { ...FullType }
    directives { name description locations args { ...InputValue } } } }
fragment FullType on __Type { kind name description
  fields(includeDeprecated: true) { name description args { ...InputValue } type { ...TypeRef } isDeprecated deprecationReason }
  inputFields { ...InputValue } interfaces { ...TypeRef }
  enumValues(includeDeprecated: true) { name description isDeprecated deprecationReason }
  possibleTypes { ...TypeRef } }
fragment InputValue on __InputValue { name description type { ...TypeRef } defaultValue }
fragment TypeRef on __Type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } } }`
	r := e.post("web", q, nil)
	r.noErrors(t)
}
