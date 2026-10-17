package main

import (
	"context"
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/client"
	"github.com/hydrogencms/hydrogen/config"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"

	gen "github.com/hydrogencms/hydrogen/examples/multisite/gen/content"
)

// clientScenario drives the Go client against a running multisite server: the app channel with its token, typed
// decoding into gen/content, the pages routes, and a preview with a minted token.
func clientScenario(t *testing.T, s *server) {
	ctx := context.Background()
	f := buildSites(s)
	host := strings.TrimSuffix(s.base, "/api/manage/v1")

	t.Run("app channel with its token", func(t *testing.T) {
		app := client.NewDelivery(host, client.WithToken(iosToken))
		res, err := app.Item(ctx, f.productEN, nil)
		if err != nil {
			t.Fatal(err)
		}
		p, err := client.DecodeItem[gen.Product](res.Item)
		if err != nil {
			t.Fatal(err)
		}
		if p.ID != f.productEN || p.Type != "product" || p.Fields.Title != "Kettle" || p.Fields.Sku != "K1" || p.Fallback {
			t.Errorf("product %+v", p)
		}
		if res.ETag == "" || !strings.HasPrefix(res.CacheControl, "private") {
			t.Errorf("meta %+v", res.Meta)
		}
		if !slices.Contains(res.Tags, "i:"+f.productEN) {
			t.Errorf("tags %v", res.Tags)
		}
		again, err := app.Item(ctx, f.productEN, &client.GetOptions{IfNoneMatch: res.ETag})
		if err != nil || !again.NotModified() {
			t.Errorf("revalidation: %+v %v", again, err)
		}

		list, err := app.List(ctx, "product", &client.ListOptions{
			Where: []client.Filter{{Field: "sku", Op: client.Prefix, Value: "K"}},
			Sort:  []client.Sort{client.Desc("sku")},
			Limit: 10, Total: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		items, err := client.DecodeItems[gen.Product](list.Items)
		if err != nil {
			t.Fatal(err)
		}
		var skus []string
		for _, it := range items {
			skus = append(skus, it.Fields.Sku)
		}
		if list.Total != 3 || strings.Join(skus, ",") != "K3,K2,K1" {
			t.Errorf("list total %d skus %v", list.Total, skus)
		}

		de, err := app.Item(ctx, f.productFR, &client.GetOptions{Locale: "de"})
		if err != nil {
			t.Fatal(err)
		}
		if it, _ := client.DecodeItem[gen.Product](de.Item); it.Locale != "fr" || !it.Fallback {
			t.Errorf("always-available fallback: %+v", it)
		}

		// Pages are not part of the app channel; neither is the channel without its token.
		if _, err := app.Item(ctx, f.about, nil); !client.IsNotFound(err) {
			t.Errorf("page on the app channel: %v", err)
		}
		_, err = client.NewDelivery(host, client.WithChannel("ios_app")).Item(ctx, f.productEN, nil)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 401 || ce.Code != "unauthorized" {
			t.Errorf("without token: %#v", err)
		}
		info, err := app.Channel(ctx, "ios_app")
		if err != nil || info.Name != "ios_app" || strings.Join(info.Collections, ",") != "product" {
			t.Errorf("channel %+v %v", info, err)
		}
	})

	t.Run("web channel routes and navigation", func(t *testing.T) {
		web := client.NewDelivery(host, client.WithChannel("corporate_en"))
		r, err := web.Route(ctx, "https://www.acme.com/en/about", nil)
		if err != nil {
			t.Fatal(err)
		}
		it, err := client.DecodeItem[gen.Page](r.Item)
		if err != nil {
			t.Fatal(err)
		}
		if r.Located.Kind != "page" || r.Located.Canonical != "https://www.acme.com/en/about" || len(r.Located.Alternates) != 4 ||
			it.ID != f.about || it.Fields.Title != "About" || it.URL != "https://www.acme.com/en/about" {
			t.Errorf("route %+v item %+v", r.Located, it)
		}
		if !slices.Contains(r.Tags, "r:corporate") {
			t.Errorf("tags %v", r.Tags)
		}
		nf, err := web.Route(ctx, "https://www.acme.com/en/nowhere", nil)
		if err != nil || nf.Located.Kind != "not_found" {
			t.Errorf("not found: %+v %v", nf, err)
		}
		if _, err := web.Route(ctx, "https://other.test/en/about", nil); !client.IsNotFound(err) {
			t.Errorf("foreign url: %v", err)
		}
		tree, err := web.Tree(ctx, &client.TreeOptions{Depth: 2})
		if err != nil || tree.Site != "corporate" {
			t.Fatalf("tree %+v %v", tree, err)
		}
		names := map[string]client.TreeNode{}
		for _, n := range tree.Nodes {
			names[n.Name] = n
		}
		if a := names["About"]; a.URL != "https://www.acme.com/en/about" || len(a.Nodes) != 1 || a.Nodes[0].Name != "Team" || names["news"].Kind != "mount" {
			t.Errorf("tree nodes %+v", tree.Nodes)
		}
		// Several channels need a name.
		_, err = client.NewDelivery(host).Tree(ctx, nil)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 400 {
			t.Errorf("no channel: %#v", err)
		}
	})

	t.Run("manage and preview", func(t *testing.T) {
		m := client.NewManage(host, token)
		v, err := m.Create(ctx, "page", client.CreateInput{Locale: "en", Fields: gen.Page{Title: "Client draft"}})
		if err != nil {
			t.Fatal(err)
		}
		fields, err := client.Fields[gen.Page](v)
		if err != nil || fields.Title != "Client draft" || v.Status != "draft" {
			t.Fatalf("created %+v %+v %v", v, fields, err)
		}
		web := client.NewDelivery(host, client.WithChannel("corporate_en"))
		if _, err := web.Item(ctx, v.ID, nil); !client.IsNotFound(err) {
			t.Fatalf("draft is public: %v", err)
		}
		tok, err := m.PreviewToken(ctx, client.PreviewTokenInput{Channel: "corporate_en", Item: v.ID})
		if err != nil || tok.Token == "" || tok.ExpiresAt.IsZero() {
			t.Fatalf("token %+v %v", tok, err)
		}
		// A page that was never published has no route yet: the channel's "*" pattern gives its preview URL, with the channel prefix.
		if want := "https://www.acme.com/en/preview/page/" + v.ID + "?preview=" + url.QueryEscape(tok.Token); tok.URL != want {
			t.Errorf("draft page url %q, want %q", tok.URL, want)
		}
		// A routed page previews at its route on that channel (frontend, prefix and locale included).
		for _, c := range []struct{ channel, locale, path string }{
			{"corporate_en", "", "/en/about"}, {"corporate_de", "de", "/de/uber-uns"}, {"corporate_dech", "", "/de-ch/about"},
		} {
			routed, err := m.PreviewToken(ctx, client.PreviewTokenInput{Channel: c.channel, Item: f.about, Locale: c.locale})
			if want := "https://www.acme.com" + c.path + "?preview=" + url.QueryEscape(routed.Token); err != nil || routed.URL != want {
				t.Errorf("%s: url %q, want %q (%v)", c.channel, routed.URL, want, err)
			}
		}
		if _, err := m.PreviewToken(ctx, client.PreviewTokenInput{Channel: "corporate_en", Item: f.about, Locale: "de"}); err == nil {
			t.Error("a locale the channel does not serve was accepted")
		}
		prev := web.With(client.WithPreview(tok.Token))
		res, err := prev.Item(ctx, v.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if it, _ := client.DecodeItem[gen.Page](res.Item); it.Fields.Title != "Client draft" || res.ETag != "" || len(res.Tags) != 0 || res.CacheControl != "no-store" {
			t.Errorf("preview %s %+v", res.Item, res.Meta)
		}
		// The token covers that item only.
		if _, err := prev.Item(ctx, f.about, nil); err != nil {
			t.Errorf("published item under a scoped token: %v", err)
		}
		if _, err := prev.With(client.WithChannel("careers_en")).Item(ctx, v.ID, nil); err == nil {
			t.Error("token of another channel accepted")
		}

		// Update (merge patch), publish, list, get and delete.
		if v, err = m.Update(ctx, v.ID, client.UpdateInput{Locale: "en", Fields: map[string]any{"intro": "Hello"}}); err != nil {
			t.Fatal(err)
		}
		if v, err = m.Publish(ctx, v.ID, nil); err != nil || v.Status != "published" {
			t.Fatalf("publish %+v %v", v, err)
		}
		eventuallyEq(t, "published intro", func() any {
			r, err := web.Item(ctx, v.ID, nil)
			if err != nil {
				return err.Error()
			}
			it, _ := client.DecodeItem[gen.Page](r.Item)
			if it.Fields.Intro == nil {
				return ""
			}
			return *it.Fields.Intro
		}, "Hello")
		list, err := m.List(ctx, "page", &client.ManageListOptions{Status: "published", ListOptions: client.ListOptions{Search: "Client"}})
		if err != nil || list.Total != 1 || list.Items[0].ID != v.ID {
			t.Errorf("list %+v %v", list, err)
		}
		got, err := m.Get(ctx, v.ID, "en", false)
		if err != nil || got.ID != v.ID {
			t.Errorf("get %+v %v", got, err)
		}
		untitled, err := m.Create(ctx, "page", client.CreateInput{Locale: "en", Fields: map[string]any{"intro": "x"}})
		if err != nil {
			t.Fatalf("drafts need no title: %v", err)
		}
		_, err = m.Publish(ctx, untitled.ID, nil)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 422 || len(ce.Fields) == 0 || ce.Fields[0].Path != "title" {
			t.Errorf("invalid publish: %#v", err)
		}
		if err := m.Delete(ctx, v.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Get(ctx, v.ID, "", false); !client.IsNotFound(err) {
			t.Errorf("deleted item: %v", err)
		}
		if _, err := client.NewManage(host, "wrong").Get(ctx, f.about, "", false); err == nil {
			t.Error("wrong management token accepted")
		}
	})
}

func TestMultisiteClientSQLite(t *testing.T) {
	clientScenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configureWithProbe))
}

func TestMultisiteClientPostgres(t *testing.T) {
	clientScenario(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configureWithProbe))
}
