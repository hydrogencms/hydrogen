package manage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/config"
)

func channelSet(t *testing.T) *channel.Set {
	t.Helper()
	dir := t.TempDir()
	yaml := `
defaults: { settings: { cache_ttl: 300 } }
channels:
  web:
    languages: [en, de]
    frontend: https://acme.com
    preview: { article: "/n/{title}" }
    links: { article: "/a/{title}" }
    site: main
  app:
    languages: [en]
    collections: [person]
    access: token
    tokens: { v1: "${env:APP_TOKEN}" }
`
	if err := os.WriteFile(filepath.Join(dir, "channels.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(config.Options{Dir: dir, LookupEnv: func(string) (string, bool) { return "very-secret-token", true }})
	if err != nil {
		t.Fatal(err)
	}
	n, err := c.Node("channels")
	if err != nil {
		t.Fatal(err)
	}
	site := channel.Option{Name: "site", Decode: func(n *config.Node) (any, error) { return n.String() }}
	s, err := channel.Compile(n, channel.CompileOptions{Schema: testSch(t), Options: []channel.Option{site}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestChannelsRoute(t *testing.T) {
	h := newAPI(t, newFake(t), func(o *Options) { o.Channels = channelSet(t) })
	w := do(h, "GET", "/channels", "")
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "very-secret-token") || strings.Contains(w.Body.String(), "tokens") {
		t.Fatalf("tokens leaked: %s", w.Body)
	}
	var got struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || got.Items[0]["name"] != "app" || got.Items[1]["name"] != "web" {
		t.Fatalf("%v", got.Items)
	}
	app, web := got.Items[0], got.Items[1]
	if app["access"] != "token" || web["access"] != "public" || web["frontend"] != "https://acme.com" {
		t.Fatalf("%v", got.Items)
	}
	if web["collections"] != nil || len(app["collections"].([]any)) != 1 {
		t.Fatalf("collections: %v %v", web["collections"], app["collections"])
	}
	if web["options"].(map[string]any)["site"] != "main" || web["settings"].(map[string]any)["cache_ttl"] != float64(300) {
		t.Fatalf("%v", web)
	}
	if web["links"].(map[string]any)["article"] != "/a/{title}" {
		t.Fatalf("%v", web["links"])
	}
	if web["preview"].(map[string]any)["article"] != "/n/{title}" {
		t.Fatalf("%v", web["preview"])
	}
	for _, k := range []string{"name", "group", "languages", "collections", "frontend", "preview", "access", "settings", "options"} {
		if _, ok := web[k]; !ok {
			t.Errorf("missing %s", k)
		}
	}
	if w := do(h, "GET", "/channels", "", "Authorization", ""); w.Code != 401 {
		t.Errorf("auth: %d", w.Code)
	}
}

func TestChannelsRouteWithoutSet(t *testing.T) {
	w := do(newAPI(t, newFake(t)), "GET", "/channels", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"items":[]}` {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestChannelsOpenAPI(t *testing.T) {
	doc, _ := openapiDoc(t, newFake(t))
	op := doc["paths"].(map[string]any)["/channels"].(map[string]any)["get"].(map[string]any)
	if op["operationId"] != "listChannels" {
		t.Fatalf("%v", op)
	}
	schemas := doc["components"].(map[string]any)["schemas"].(map[string]any)
	props := schemas["Channel"].(map[string]any)["properties"].(map[string]any)
	if _, ok := props["tokens"]; ok {
		t.Fatal("tokens in schema")
	}
	if _, ok := schemas["ChannelList"]; !ok {
		t.Fatal("ChannelList")
	}
}

func TestChannelOptionNotEncodable(t *testing.T) {
	// A set whose option cannot be encoded makes New fail instead of the route.
	bad := channelSetWithOption(t, func() any { return make(chan int) })
	if _, err := New(Options{Content: newFake(t), Token: token, Channels: bad}); err == nil || !strings.Contains(err.Error(), "JSON-encodable") {
		t.Fatalf("got %v", err)
	}
}

func channelSetWithOption(t *testing.T, v func() any) *channel.Set {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "channels.yaml"), []byte("channels: { a: { languages: [en], x: 1 } }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(config.Options{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	n, _ := c.Node("channels")
	s, err := channel.Compile(n, channel.CompileOptions{Options: []channel.Option{{Name: "x", Decode: func(*config.Node) (any, error) { return v(), nil }}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
