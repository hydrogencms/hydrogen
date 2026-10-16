package client_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/client"
)

type seen struct {
	method, path, rawQuery string
	header                 http.Header
	body                   string
}

// stub answers every request with the given status and body and records the request.
func stub(t *testing.T, status int, body string, hdr map[string]string) (*httptest.Server, *seen) {
	t.Helper()
	var got seen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = seen{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(b)}
		for k, v := range hdr {
			w.Header().Set(k, v)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestListQuerySyntax(t *testing.T) {
	srv, got := stub(t, 200, `{"items":[],"hasMore":true,"total":0}`, nil)
	d := client.NewDelivery(srv.URL+"/", client.WithChannel("web"), client.WithToken("tok"), client.WithPreview("pv"),
		client.WithHeader("X-Trace", "1"))
	lr, err := d.List(context.Background(), "article", &client.ListOptions{
		Locale:  "de",
		Include: 2,
		Where: []client.Filter{
			{Field: "rank", Op: client.Gte, Value: 3},
			{Field: "title", Value: "a b&c"},
			{Field: "tag", Op: client.In, Value: []string{"x", "y"}},
			{Field: "cover", Op: client.Exists, Value: true},
			{Field: "at", Op: client.Lt, Value: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
		},
		Search: "go cms",
		Sort:   []client.Sort{client.Desc("rank"), client.Asc("name")},
		Limit:  10, Offset: 20, Total: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !lr.HasMore || lr.Total != 0 {
		t.Errorf("list result %+v", lr)
	}
	want := "channel=web&locale=de&include=2&where[rank][gte]=3&where[title]=a+b%26c&where[tag][in]=x%2Cy&where[cover][exists]=true" +
		"&where[at][lt]=2026-01-02T03%3A04%3A05Z&q=go+cms&sort=-rank%2Cname&limit=10&offset=20&total=true"
	if got.path != "/api/deliver/v1/collections/article/items" || got.rawQuery != want {
		t.Errorf("request %s?%s\nwant ?%s", got.path, got.rawQuery, want)
	}
	if got.header.Get("Authorization") != "Bearer tok" || got.header.Get("X-Hydrogen-Preview") != "pv" || got.header.Get("X-Trace") != "1" {
		t.Errorf("headers %v", got.header)
	}
	if lr, _ := client.NewDelivery(srv.URL).List(context.Background(), "article", nil); lr == nil {
		t.Error("nil options")
	}
	if _, err := d.List(context.Background(), "article", &client.ListOptions{Where: []client.Filter{{Field: "a]", Value: 1}}}); err == nil {
		t.Error("bad field accepted")
	}
	if _, err := d.List(context.Background(), "article", &client.ListOptions{Where: []client.Filter{{Field: "a", Value: struct{}{}}}}); err == nil {
		t.Error("bad value accepted")
	}
}

func TestMetaAndNotModified(t *testing.T) {
	srv, got := stub(t, 200, `{"item":{"id":"i1","type":"page","locale":"en","version":2,"publishedAt":"2026-10-01T10:00:00Z","name":"N","fields":{"title":"T"},"fallback":true,"url":"https://x/y","links":{"a":"/b"}}}`,
		map[string]string{"Etag": `"abc"`, "Surrogate-Key": "i:i1 c:page", "Cache-Control": "public, s-maxage=60"})
	d := client.NewDelivery(srv.URL)
	res, err := d.Item(context.Background(), "i1", &client.GetOptions{Locale: "en", Include: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != 200 || res.ETag != `"abc"` || !slices.Equal(res.Tags, []string{"i:i1", "c:page"}) || res.CacheControl == "" {
		t.Errorf("meta %+v", res.Meta)
	}
	if got.path != "/api/deliver/v1/items/i1" || got.rawQuery != "locale=en&include=1" {
		t.Errorf("request %s?%s", got.path, got.rawQuery)
	}
	type page struct {
		Title string `json:"title"`
	}
	it, err := client.DecodeItem[page](res.Item)
	if err != nil {
		t.Fatal(err)
	}
	if it.ID != "i1" || it.Fields.Title != "T" || !it.Fallback || it.URL != "https://x/y" || it.Links["a"] != "/b" ||
		it.PublishedAt.Year() != 2026 || it.Version != 2 {
		t.Errorf("item %+v", it)
	}
	if _, err := client.DecodeItem[page](nil); err == nil {
		t.Error("empty payload decoded")
	}

	srv2, got2 := stub(t, 304, "", map[string]string{"Etag": `"abc"`})
	res, err = client.NewDelivery(srv2.URL).ItemByKey(context.Background(), "a/b", &client.GetOptions{IfNoneMatch: `"abc"`})
	if err != nil || !res.NotModified() || res.Item != nil {
		t.Fatalf("304: %+v %v", res, err)
	}
	if got2.header.Get("If-None-Match") != `"abc"` || got2.path != "/api/deliver/v1/items/by-key/a%2Fb" && got2.path != "/api/deliver/v1/items/by-key/a/b" {
		t.Errorf("request %s %v", got2.path, got2.header)
	}
	// Without If-None-Match a 304 is an error.
	if _, err := client.NewDelivery(srv2.URL).Item(context.Background(), "x", nil); err == nil {
		t.Error("unexpected 304 accepted")
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	srv, _ := stub(t, 422, `{"error":{"code":"invalid","message":"validation failed","locale":"en","fields":[{"path":"title","code":"required","message":"is required"}]}}`, nil)
	_, err := client.NewManage(srv.URL, "t").Create(ctx, "page", client.CreateInput{})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != 422 || ce.Code != "invalid" || ce.Locale != "en" || len(ce.Fields) != 1 || ce.Fields[0].Path != "title" {
		t.Fatalf("error %#v", err)
	}
	if ce.Error() == "" || client.IsNotFound(err) {
		t.Error("error text or IsNotFound")
	}
	srv, _ = stub(t, 404, `{"error":{"code":"not_found","message":"not available"}}`, nil)
	if _, err = client.NewDelivery(srv.URL).Item(ctx, "x", nil); !client.IsNotFound(err) {
		t.Errorf("404: %v", err)
	}
	srv, _ = stub(t, 502, "<html>bad gateway</html>", nil)
	if _, err = client.NewDelivery(srv.URL).Item(ctx, "x", nil); !errors.As(err, &ce) || ce.Status != 502 || ce.Code != "http_error" {
		t.Errorf("non-envelope error: %#v", err)
	}
	srv, _ = stub(t, 409, `{"error":{"code":"referenced","message":"m","referrers":[{"id":"r1","type":"article","name":"N","field":"author","kind":"ref","onDelete":"restrict"}]}}`, nil)
	if err = client.NewManage(srv.URL, "t").Delete(ctx, "x"); !errors.As(err, &ce) || len(ce.Referrers) != 1 || ce.Referrers[0].Field != "author" {
		t.Errorf("referenced: %#v", err)
	}
	// Cancelled context and unreachable servers are plain errors, not *Error.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	srv, _ = stub(t, 200, `{}`, nil)
	if _, err = client.NewDelivery(srv.URL).Item(cctx, "x", nil); err == nil || errors.As(err, &ce) || !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestManageRequests(t *testing.T) {
	ctx := context.Background()
	view := `{"id":"i1","type":"page","locale":"en","status":"draft","version":0,"name":"N","fields":{"title":"T"},"locales":["en"],"draftParts":["shared"],"createdAt":"2026-10-01T10:00:00Z","updatedAt":"2026-10-01T10:00:00Z"}`
	srv, got := stub(t, 201, view, nil)
	m := client.NewManage(srv.URL, "secret")
	type page struct {
		Title string `json:"title"`
	}
	v, err := m.Create(ctx, "page", client.CreateInput{Locale: "en", Key: "k", AlwaysAvailable: true, Fields: page{"T"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != "POST" || got.path != "/api/manage/v1/collections/page/items" || got.header.Get("Authorization") != "Bearer secret" ||
		got.header.Get("Content-Type") != "application/json" {
		t.Errorf("create request %+v", got)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(got.body), &sent)
	if sent["locale"] != "en" || sent["key"] != "k" || sent["alwaysAvailable"] != true || sent["fields"].(map[string]any)["title"] != "T" {
		t.Errorf("create body %s", got.body)
	}
	if f, err := client.Fields[page](v); err != nil || f.Title != "T" || v.ID != "i1" || v.Status != "draft" {
		t.Errorf("view %+v %v", v, err)
	}

	key := "new"
	if _, err = m.Update(ctx, "i1", client.UpdateInput{Locale: "de", Fields: map[string]any{"title": nil, "x": 1}, Key: &key}); err != nil {
		t.Fatal(err)
	}
	if got.method != "PATCH" || got.path != "/api/manage/v1/items/i1" || got.body != `{"fields":{"title":null,"x":1},"key":"new","locale":"de"}` {
		t.Errorf("update %s %s %s", got.method, got.path, got.body)
	}
	if _, err = m.Publish(ctx, "i1", nil); err != nil || got.path != "/api/manage/v1/items/i1/publish" || got.body != `{}` {
		t.Errorf("publish %s %s %v", got.path, got.body, err)
	}
	if _, err = m.Publish(ctx, "i1", &client.PublishInput{Locales: []string{"de"}, Force: true}); err != nil || got.body != `{"force":true,"locales":["de"]}` {
		t.Errorf("publish body %s %v", got.body, err)
	}
	if _, err = m.Get(ctx, "i1", "de", true); err != nil || got.method != "GET" || got.rawQuery != "locale=de&draft=true" {
		t.Errorf("get %s?%s %v", got.path, got.rawQuery, err)
	}

	srv, got = stub(t, 200, `{"items":[{"id":"i1","type":"page","name":"N","status":"published","version":1}],"total":7}`, nil)
	l, err := client.NewManage(srv.URL, "t").List(ctx, "page", &client.ManageListOptions{Status: "draft", ListOptions: client.ListOptions{Limit: 5, Include: 3}})
	if err != nil || l.Total != 7 || l.Items[0].ID != "i1" || got.rawQuery != "status=draft&limit=5" {
		t.Errorf("list %+v %s %v", l, got.rawQuery, err)
	}

	srv, got = stub(t, 204, "", nil)
	if err = client.NewManage(srv.URL, "t").Delete(ctx, "i1"); err != nil || got.method != "DELETE" {
		t.Errorf("delete %s %v", got.method, err)
	}

	srv, got = stub(t, 200, `{"token":"abc.def","expiresAt":"2026-10-15T10:15:00Z"}`, nil)
	tok, err := client.NewManage(srv.URL, "t").PreviewToken(ctx, client.PreviewTokenInput{Channel: "web", Item: "i1", TTL: 10 * time.Minute})
	if err != nil || tok.Token != "abc.def" || tok.ExpiresAt.Minute() != 15 || got.path != "/api/manage/v1/preview-tokens" ||
		got.body != `{"channel":"web","item":"i1","ttl":"600s"}` {
		t.Errorf("preview token %+v %s %v", tok, got.body, err)
	}

	mk := func(status int, body string) (*client.Manage, *seen) {
		srv, got := stub(t, status, body, nil)
		return client.NewManage(srv.URL, "t"), got
	}
	mg, got := mk(201, `{"id":"s1","token":"hps_x","url":"https://x/y?preview=hps_x","channel":"web","locale":"en","expiresAt":"2026-10-15T10:15:00Z"}`)
	sh, err := mg.CreateShare(ctx, client.ShareInput{Item: "i1", Channel: "web", TTL: time.Hour})
	if err != nil || sh.Token != "hps_x" || sh.ID != "s1" || got.path != "/api/manage/v1/preview-shares" || got.method != "POST" ||
		got.body != `{"channel":"web","item":"i1","ttl":"3600s"}` {
		t.Errorf("create share %+v %s %v", sh, got.body, err)
	}
	mg, got = mk(200, `{"shares":[{"id":"s1","channel":"web","locale":"en","createdBy":"user:1","createdByName":"Ada","expiresAt":"2026-10-15T10:15:00Z","viewers":2}]}`)
	list, err := mg.Shares(ctx, "i1")
	if err != nil || len(list) != 1 || list[0].Viewers != 2 || list[0].CreatedByName != "Ada" || got.rawQuery != "item=i1" {
		t.Errorf("shares %+v %s %v", list, got.rawQuery, err)
	}
	mg, got = mk(204, "")
	if err = mg.RevokeShare(ctx, "s1"); err != nil || got.method != "DELETE" || got.path != "/api/manage/v1/preview-shares/s1" {
		t.Errorf("revoke %s %s %v", got.method, got.path, err)
	}
	mg, got = mk(200, `{"rev":3}`)
	rev, err := mg.RelayDraft(ctx, "i1", "de", map[string]any{"title": "T"})
	if err != nil || rev != 3 || got.method != "PUT" || got.path != "/api/manage/v1/preview-relay/i1" || got.rawQuery != "locale=de" ||
		got.body != `{"fields":{"title":"T"}}` {
		t.Errorf("relay %d %s %s %v", rev, got.rawQuery, got.body, err)
	}
	mg, got = mk(204, "")
	if err = mg.DropRelay(ctx, "i1", ""); err != nil || got.method != "DELETE" || got.path != "/api/manage/v1/preview-relay/i1" || got.rawQuery != "" {
		t.Errorf("drop %s %s %v", got.method, got.path, err)
	}
}

func TestWithCopiesAndChannel(t *testing.T) {
	srv, got := stub(t, 200, `{"name":"web","languages":["en"],"collections":["page"],"options":{"site":"corporate"}}`, nil)
	d := client.NewDelivery(srv.URL, client.WithChannel("web"), client.WithHeader("A", "1"))
	p := d.With(client.WithPreview("pv"), client.WithHeader("B", "2"))
	if _, err := d.Channel(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got.path != "/api/deliver/v1/channels/web" || got.header.Get("X-Hydrogen-Preview") != "" || got.header.Get("B") != "" {
		t.Errorf("original changed: %s %v", got.path, got.header)
	}
	info, err := p.Channel(context.Background(), "web")
	if err != nil || got.header.Get("X-Hydrogen-Preview") != "pv" || got.header.Get("A") != "1" || got.header.Get("B") != "2" {
		t.Errorf("copy: %v %v", got.header, err)
	}
	if info.Name != "web" || string(info.Options["site"]) != `"corporate"` {
		t.Errorf("info %+v", info)
	}
	if _, err := client.NewDelivery(srv.URL).Channel(context.Background(), ""); err == nil {
		t.Error("Channel without a name")
	}
}

func TestRouteAndTree(t *testing.T) {
	srv, got := stub(t, 200, `{"located":{"channel":"web","kind":"page","locale":"en","canonical":"https://a/b","alternates":[{"channel":"c","locale":"de","url":"https://a/de"}],"resolution":{"kind":"page","status":200,"path":"/b"}},"item":{"id":"i1"}}`, nil)
	d := client.NewDelivery(srv.URL, client.WithChannel("web"))
	r, err := d.Route(context.Background(), "https://a/b?x=1", nil)
	if err != nil || r.Located.Kind != "page" || r.Located.Alternates[0].Locale != "de" || r.Located.Resolution.Status != 200 || string(r.Item) != `{"id":"i1"}` {
		t.Fatalf("route %+v %v", r, err)
	}
	if got.rawQuery != "channel=web&url=https%3A%2F%2Fa%2Fb%3Fx%3D1" {
		t.Errorf("route query %s", got.rawQuery)
	}
	srv, got = stub(t, 200, `{"site":"s","locale":"en","nodes":[{"id":"n1","kind":"page","name":"A","nodes":[{"id":"n2","kind":"mount","name":"news"}]}]}`, map[string]string{"Cache-Tag": "nav:s, r:s"})
	tr, err := client.NewDelivery(srv.URL).Tree(context.Background(), &client.TreeOptions{Site: "s", Depth: 2})
	if err != nil || tr.Site != "s" || tr.Nodes[0].Nodes[0].Kind != "mount" || !slices.Equal(tr.Tags, []string{"nav:s", "r:s"}) || got.rawQuery != "site=s&depth=2" {
		t.Errorf("tree %+v %v %s", tr, err, got.rawQuery)
	}
}
