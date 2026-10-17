package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/api/client"
	"github.com/hydrogencms/hydrogen/core/config"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"

	gen "github.com/hydrogencms/hydrogen/examples/content/gen/content"
)

// clientFlow drives the Go client against the content example (one implicit channel without a web matcher):
// writes through the management API, typed reads and filters through the delivery API, and draft previews.
func clientFlow(t *testing.T, s *server) {
	ctx := context.Background()
	host := strings.TrimSuffix(s.base, "/api/manage/v1")
	m := client.NewManage(host, token)
	d := client.NewDelivery(host)

	ada, err := m.Create(ctx, "person", client.CreateInput{Locale: "en", Fields: map[string]any{"firstName": "Ada", "lastName": "Lovelace"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Publish(ctx, ada.ID, nil); err != nil {
		t.Fatal(err)
	}
	title, summary := "Hello Hydrogen", "One two three."
	art, err := m.Create(ctx, "article", client.CreateInput{Locale: "en", Key: "hello", Fields: gen.Article{
		Title: title, Summary: &summary, Tags: []string{"go", "cms"}, Author: refTo(ada.ID),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Publish(ctx, art.ID, nil); err != nil {
		t.Fatal(err)
	}
	other, err := m.Create(ctx, "article", client.CreateInput{Locale: "en", Fields: map[string]any{"title": "Other", "tags": []string{"rust"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Publish(ctx, other.ID, nil); err != nil {
		t.Fatal(err)
	}

	t.Run("typed reads", func(t *testing.T) {
		res, err := d.ItemByKey(ctx, "hello", &client.GetOptions{Include: 1})
		if err != nil {
			t.Fatal(err)
		}
		a, err := client.DecodeItem[gen.Article](res.Item)
		if err != nil {
			t.Fatal(err)
		}
		if a.ID != art.ID || a.Key != "hello" || a.Fields.Title != title || a.Fields.ReadingMinutes == nil || *a.Fields.ReadingMinutes != 1 ||
			a.Fields.Kind == nil || *a.Fields.Kind != "news" || a.Fields.Author == nil {
			t.Errorf("article %+v", a)
		}
		if p, err := client.DecodeItem[gen.Person](res.Included[ada.ID]); err != nil || p.Fields.FirstName != "Ada" {
			t.Errorf("included author %+v %v", p, err)
		}
		if a.URL != "" && !strings.HasPrefix(a.URL, "/") {
			t.Errorf("url %q", a.URL)
		}

		list, err := d.List(ctx, "article", &client.ListOptions{
			Where: []client.Filter{{Field: "tags", Op: client.Contains, Value: "go"}, {Field: "kind", Value: "news"}},
			Sort:  []client.Sort{client.Desc("publishDate")},
			Total: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		if list.Total != 1 || len(list.Items) != 1 || list.HasMore {
			t.Fatalf("list %d %d %v", list.Total, len(list.Items), list.HasMore)
		}
		all, err := d.List(ctx, "article", &client.ListOptions{Limit: 1})
		if err != nil || !all.HasMore || all.Total != -1 || len(all.Items) != 1 {
			t.Errorf("paging: %+v %v", all, err)
		}
		if _, err := d.List(ctx, "article", &client.ListOptions{Where: []client.Filter{{Field: "nope", Value: "x"}}}); err == nil {
			t.Error("unknown filter field accepted")
		}

		_, err = d.Singleton(ctx, "article", nil)
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 404 {
			t.Errorf("collection as singleton: %v", err)
		}
	})

	t.Run("previews", func(t *testing.T) {
		draft, err := m.Create(ctx, "article", client.CreateInput{Locale: "en", Fields: gen.Article{Title: "Not yet"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Item(ctx, draft.ID, nil); !client.IsNotFound(err) {
			t.Fatalf("draft is public: %v", err)
		}
		tok, err := m.PreviewToken(ctx, client.PreviewTokenInput{Channel: "default"})
		if err != nil {
			t.Fatal(err)
		}
		prev := d.With(client.WithPreview(tok.Token))
		res, err := prev.Item(ctx, draft.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		if a, _ := client.DecodeItem[gen.Article](res.Item); a.Fields.Title != "Not yet" || res.CacheControl != "no-store" || len(res.Tags) != 0 {
			t.Errorf("preview %s %+v", res.Item, res.Meta)
		}

		// A pending edit of a published item shows only under the token.
		if _, err := m.Update(ctx, art.ID, client.UpdateInput{Locale: "en", Fields: map[string]any{"title": "Hello again"}}); err != nil {
			t.Fatal(err)
		}
		titleOf := func(c *client.Delivery) string {
			t.Helper()
			r, err := c.ItemByKey(ctx, "hello", nil)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := client.DecodeItem[gen.Article](r.Item)
			return a.Fields.Title
		}
		if titleOf(d) != title || titleOf(prev) != "Hello again" {
			t.Errorf("published %q, preview %q", titleOf(d), titleOf(prev))
		}
		// Previews list drafts, too.
		l, err := prev.List(ctx, "article", &client.ListOptions{Total: true})
		if err != nil || l.Total != 3 {
			t.Errorf("preview list %+v %v", l, err)
		}
		if _, err := d.With(client.WithPreview(tok.Token+"x")).Item(ctx, art.ID, nil); err == nil {
			t.Error("tampered token accepted")
		}
	})

	t.Run("management errors", func(t *testing.T) {
		_, err := m.Create(ctx, "article", client.CreateInput{Locale: "en", Fields: map[string]any{"rating": 9}})
		var ce *client.Error
		if !errors.As(err, &ce) || ce.Status != 422 || len(ce.Fields) == 0 || ce.Fields[0].Path != "rating" {
			t.Errorf("validation: %#v", err)
		}
		err = m.Delete(ctx, ada.ID)
		if !errors.As(err, &ce) || ce.Code != "referenced" || len(ce.Referrers) != 1 || ce.Referrers[0].ID != art.ID {
			t.Errorf("referenced: %#v", err)
		}
	})
}

func refTo(id string) *content.Ref {
	r := content.Ref(id)
	return &r
}

func TestClientSQLite(t *testing.T) {
	clientFlow(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": "sqlite:" + filepath.Join(t.TempDir(), "h.db")}, configure))
}

func TestClientPostgres(t *testing.T) {
	clientFlow(t, start(t, config.DevEnv, map[string]string{"DATABASE_URL": pgtest.Scratch(t)}, configure))
}
