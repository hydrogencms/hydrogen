package tests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/client"
	"github.com/hydrogencms/hydrogen/kernel"
)

// TestGoClient runs the Go client against real handlers on every adapter: delivery reads with their cache
// metadata, token channels, listing queries and previews, and management writes.
func TestGoClient(t *testing.T) {
	runDelivery(t, func(t *testing.T, n *dnode) {
		ctx := context.Background()
		srv := httptest.NewServer(n.h)
		t.Cleanup(srv.Close)

		ann := n.create("person", "ann", "en", `{"fullName":"Ann"}`, true)
		art := n.create("article", "hello", "en", `{"title":"Hello","rank":3,"author":"`+ann.String()+`"}`, false)
		n.update(art, "de", `{"title":"Hallo"}`)
		n.publish(art)
		n.create("article", "second", "en", `{"title":"Second","rank":1}`, true)
		n.create("article", "third", "en", `{"title":"Third","rank":2}`, true)

		web := client.NewDelivery(srv.URL, client.WithChannel("web"))

		t.Run("item, tags and revalidation", func(t *testing.T) {
			res, err := web.Item(ctx, art.String(), &client.GetOptions{Include: 1})
			if err != nil {
				t.Fatal(err)
			}
			it, err := client.DecodeItem[clientArticle](res.Item)
			if err != nil {
				t.Fatal(err)
			}
			if it.ID != art.String() || it.Key != "hello" || it.Fields.Title != "Hello" || it.Fields.Rank != 3 || it.Fallback || it.Locale != "en" {
				t.Errorf("item %+v", it)
			}
			if len(res.Included) != 1 || res.ETag == "" || res.Status != 200 || !slices.Contains(res.Tags, "i:"+art.String()) ||
				!slices.Contains(res.Tags, "i:"+ann.String()) {
				t.Errorf("meta %+v included %d", res.Meta, len(res.Included))
			}
			same, err := web.Item(ctx, art.String(), &client.GetOptions{Include: 1, IfNoneMatch: res.ETag})
			if err != nil || !same.NotModified() {
				t.Fatalf("revalidate %+v %v", same, err)
			}
			n.update(art, "en", `{"title":"Hello 2"}`)
			n.publish(art)
			n.drain()
			eventually(t, "the changed item", func() bool {
				r, err := web.Item(ctx, art.String(), &client.GetOptions{Include: 1, IfNoneMatch: res.ETag})
				return err == nil && !r.NotModified() && r.ETag != res.ETag
			})

			de, err := web.Item(ctx, art.String(), &client.GetOptions{Locale: "de"})
			if err != nil {
				t.Fatal(err)
			}
			if d, _ := client.DecodeItem[clientArticle](de.Item); d.Locale != "de" || d.Fields.Title != "Hallo" {
				t.Errorf("de %+v", d)
			}
			if _, err := web.Item(ctx, art.String(), &client.GetOptions{Locale: "xx"}); err == nil {
				t.Error("unknown locale accepted")
			}
		})

		t.Run("listing", func(t *testing.T) {
			res, err := web.List(ctx, "article", &client.ListOptions{
				Where: []client.Filter{{Field: "rank", Op: client.Gte, Value: 2}},
				Sort:  []client.Sort{client.Asc("rank")}, Total: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			titles := titlesOf(t, res.Items)
			if res.Total != 2 || res.HasMore || !slices.Equal(titles, []string{"Third", "Hello 2"}) || !slices.Contains(res.Tags, "c:article") {
				t.Errorf("list %v total %d hasMore %v tags %v", titles, res.Total, res.HasMore, res.Tags)
			}
			page, err := web.List(ctx, "article", &client.ListOptions{Sort: []client.Sort{client.Desc("rank")}, Limit: 2, Offset: 1})
			if err != nil || !slices.Equal(titlesOf(t, page.Items), []string{"Third", "Second"}) || page.HasMore || page.Total != -1 {
				t.Errorf("page %v %+v %v", titlesOf(t, page.Items), page, err)
			}
			in, err := web.List(ctx, "article", &client.ListOptions{Where: []client.Filter{{Field: "rank", Op: client.In, Value: []int{1, 3}}}, Sort: []client.Sort{client.Asc("rank")}})
			if err != nil || !slices.Equal(titlesOf(t, in.Items), []string{"Second", "Hello 2"}) {
				t.Errorf("in %v %v", titlesOf(t, in.Items), err)
			}
			var ce *client.Error
			if _, err := web.List(ctx, "article", &client.ListOptions{Where: []client.Filter{{Field: "bogus", Value: "1"}}}); !errors.As(err, &ce) || ce.Status != 400 {
				t.Errorf("bad filter: %#v", err)
			}
			if _, err := web.List(ctx, "nope", nil); !client.IsNotFound(err) {
				t.Errorf("unknown type: %v", err)
			}
		})

		t.Run("singleton, by key and channel", func(t *testing.T) {
			if _, err := web.Singleton(ctx, "settings", nil); !client.IsNotFound(err) {
				t.Errorf("unpublished singleton: %v", err)
			}
			if _, err := web.ItemByKey(ctx, "nothing", nil); !client.IsNotFound(err) {
				t.Errorf("missing key: %v", err)
			}
			_, err := client.NewDelivery(srv.URL).Item(ctx, art.String(), nil)
			var ce *client.Error
			if !errors.As(err, &ce) || ce.Status != 400 || ce.Code != "bad_request" {
				t.Errorf("several channels, none chosen: %#v", err)
			}
			info, err := web.Channel(ctx, "")
			if err != nil || info.Name != "web" || !slices.Equal(info.Languages, []string{"en", "de"}) {
				t.Errorf("channel %+v %v", info, err)
			}
		})

		t.Run("token channel", func(t *testing.T) {
			app := client.NewDelivery(srv.URL, client.WithToken(appToken))
			res, err := app.ItemByKey(ctx, "ann", nil)
			if err != nil {
				t.Fatal(err)
			}
			if p, _ := client.DecodeItem[struct {
				FullName string `json:"fullName"`
			}](res.Item); p.Fields.FullName != "Ann" || p.Locale != "de" && p.Locale != "en" {
				t.Errorf("person %+v", p)
			}
			if !strings.HasPrefix(res.CacheControl, "private") {
				t.Errorf("cache-control %q", res.CacheControl)
			}
			var ce *client.Error
			if _, err := client.NewDelivery(srv.URL, client.WithToken("wrong")).ItemByKey(ctx, "ann", nil); !errors.As(err, &ce) || ce.Status != 401 {
				t.Errorf("wrong token: %#v", err)
			}
			if _, err := client.NewDelivery(srv.URL, client.WithChannel("app")).ItemByKey(ctx, "ann", nil); !errors.As(err, &ce) || ce.Status != 401 {
				t.Errorf("no token: %#v", err)
			}
		})

		t.Run("manage and preview", func(t *testing.T) {
			m := client.NewManage(srv.URL, kernel.DevManageToken)
			v, err := m.Create(ctx, "article", client.CreateInput{Locale: "en", Key: "draft", Fields: map[string]any{"title": "Draft", "rank": 9}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := web.ItemByKey(ctx, "draft", nil); !client.IsNotFound(err) {
				t.Fatalf("draft is public: %v", err)
			}
			tok, err := m.PreviewToken(ctx, client.PreviewTokenInput{Channel: "web"})
			if err != nil {
				t.Fatal(err)
			}
			res, err := web.With(client.WithPreview(tok.Token)).ItemByKey(ctx, "draft", nil)
			if err != nil {
				t.Fatal(err)
			}
			if it, _ := client.DecodeItem[clientArticle](res.Item); it.Fields.Rank != 9 || res.CacheControl != "no-store" || res.ETag != "" {
				t.Errorf("preview %s %+v", res.Item, res.Meta)
			}
			if _, err := m.Update(ctx, v.ID, client.UpdateInput{Locale: "en", Fields: map[string]any{"rank": nil}}); err != nil {
				t.Fatal(err)
			}
			got, err := m.Get(ctx, v.ID, "en", true)
			if err != nil {
				t.Fatal(err)
			}
			if f, _ := client.Fields[map[string]any](got); f["rank"] != nil || f["title"] != "Draft" {
				t.Errorf("merge patch: %v", f)
			}
			if _, err := m.Publish(ctx, v.ID, &client.PublishInput{Locales: []string{"en"}}); err != nil {
				t.Fatal(err)
			}
			n.drain()
			eventually(t, "the published draft", func() bool {
				_, err := web.ItemByKey(ctx, "draft", nil)
				return err == nil
			})
			l, err := m.List(ctx, "article", &client.ManageListOptions{Status: "published", ListOptions: client.ListOptions{Sort: []client.Sort{client.Asc("name")}}})
			if err != nil || l.Total != 4 || l.Items[0].Name != "Draft" {
				t.Errorf("manage list %+v %v", l, err)
			}
			if err := m.Delete(ctx, v.ID); err != nil {
				t.Fatal(err)
			}
			n.drain()
			eventually(t, "the deleted item to vanish", func() bool {
				_, err := web.ItemByKey(ctx, "draft", nil)
				return client.IsNotFound(err)
			})
			var ce *client.Error
			if _, err := client.NewManage(srv.URL, "wrong").Get(ctx, v.ID, "", false); !errors.As(err, &ce) || ce.Status != 401 {
				t.Errorf("wrong manage token: %#v", err)
			}
		})
	})
}

type clientArticle struct {
	Title string `json:"title"`
	Rank  int    `json:"rank"`
}

func titlesOf(t *testing.T, raws []json.RawMessage) []string {
	t.Helper()
	items, err := client.DecodeItems[clientArticle](raws)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		out = append(out, it.Fields.Title)
	}
	return out
}
