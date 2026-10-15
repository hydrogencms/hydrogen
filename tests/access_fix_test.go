package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/access"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/modules/postgres"
	"github.com/hydrogencms/hydrogen/modules/postgres/pgtest"
	"github.com/hydrogencms/hydrogen/store"
)

// principalFor makes a token with the policies (and optional limitations) and returns a context acting as it.
func (n *authNode) principalFor(name string, policies ...access.RolePolicy) context.Context {
	n.t.Helper()
	s := n.app.Services()
	role, err := s.Admin.CreateRole(ctx, access.RoleInput{Name: name, Policies: policies})
	if err != nil {
		n.t.Fatal(err)
	}
	_, secret, err := s.Admin.CreateToken(ctx, access.TokenInput{Name: name, Roles: []ids.UUID{role.ID}})
	if err != nil {
		n.t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+secret)
	an, err := s.Auth.Authenticate(r)
	if err != nil {
		n.t.Fatal(err)
	}
	return access.WithPrincipal(context.Background(), an.Principal)
}

func rolePolicy(name string, lim map[string][]string) access.RolePolicy {
	return access.RolePolicy{Policy: name, Limitations: lim}
}

// Changing the key or availability of a published item goes live at once, so it needs content.publish for every
// locale the item has, not just content.edit.
func TestMetaChangeNeedsPublish(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		pub := n.item
		contributor := n.principalFor("contrib", rolePolicy("content.read", nil), rolePolicy("content.create", nil), rolePolicy("content.edit", nil))
		key, always := "taken-over", true
		for _, in := range []content.UpdateInput{{Key: &key}, {AlwaysAvailable: &always}} {
			if _, err := s.Content.Update(contributor, pub, in); !errors.Is(err, access.ErrForbidden) || !strings.Contains(err.Error(), "content.publish") {
				t.Errorf("%+v on a published item: %v", in, err)
			}
		}
		if _, err := s.Content.Update(contributor, pub, content.UpdateInput{Fields: doc(t, `{"rank":9}`)}); err != nil {
			t.Errorf("a draft edit must work: %v", err)
		}
		// A draft that was never published is not live: the key may change.
		v, err := s.Content.Create(contributor, content.CreateInput{Type: "article", Fields: doc(t, `{"title":"Draft"}`)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content.Update(contributor, v.ID, content.UpdateInput{Key: &key}); err != nil {
			t.Errorf("key of a draft: %v", err)
		}
		// With publish rights it works, and only for a principal who may write every locale of the item.
		de := n.principalFor("de-only", rolePolicy("content.read", nil), rolePolicy("content.edit", nil), rolePolicy("content.publish", map[string][]string{"locale": {"de"}}))
		if _, err := s.Content.Update(context.Background(), pub, content.UpdateInput{Locale: "de", Fields: doc(t, `{"title":"Ein Artikel"}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content.Update(de, pub, content.UpdateInput{Key: &key}); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("a locale-limited publisher changed the key of a multi-locale item: %v", err)
		}
		editor := n.principalFor("full", rolePolicy("content.read", nil), rolePolicy("content.edit", nil), rolePolicy("content.publish", nil))
		live := "live-key"
		if _, err := s.Content.Update(editor, pub, content.UpdateInput{Key: &live}); err != nil {
			t.Errorf("an editor with publish rights: %v", err)
		}
	})
}

// What a principal cannot read is not found, before any state-dependent answer; what it can read but not do is
// forbidden.
func TestUnreadableItemsAreNotFound(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		draft := n.create("article", "", "en", `{"title":"Never published"}`, false)
		people := n.principalFor("people", rolePolicy("content.read", map[string][]string{"collection": {"person"}}),
			rolePolicy("content.edit", nil), rolePolicy("content.publish", nil), rolePolicy("content.delete", nil), rolePolicy("content.preview", nil))
		viewer := n.principalFor("watcher", rolePolicy("content.read", nil))
		notFound := func(what string, err error) {
			t.Helper()
			if !errors.Is(err, content.ErrNotFound) || errors.Is(err, access.ErrForbidden) {
				t.Errorf("%s: %v, want not found", what, err)
			}
		}
		for _, id := range []ids.UUID{n.item, draft} {
			_, err := s.Content.Get(people, id, content.GetOptions{})
			notFound("get", err)
			_, err = s.Content.Get(people, id, content.GetOptions{Draft: true})
			notFound("get draft", err)
			_, err = s.Content.Update(people, id, content.UpdateInput{Fields: doc(t, `{"rank":1}`)})
			notFound("update", err)
			_, err = s.Content.Publish(people, id, content.PublishOptions{})
			notFound("publish", err)
			notFound("discard", s.Content.Discard(people, id, nil))
			notFound("delete", s.Content.Delete(people, id))
			_, err = s.Content.Versions(people, id)
			notFound("versions", err)
			_, _, err = s.Content.ReferencesVisible(people, id)
			notFound("references", err)
		}
		ch, _ := s.Channels.Get("web")
		notFound("preview", s.Content.CanPreview(people, ch, n.item))
		// Readable but not permitted: forbidden.
		_, err := s.Content.Update(viewer, n.item, content.UpdateInput{Fields: doc(t, `{"rank":1}`)})
		if !errors.Is(err, access.ErrForbidden) {
			t.Errorf("update by a reader: %v", err)
		}
		if err := s.Content.Delete(viewer, draft); !errors.Is(err, access.ErrForbidden) {
			t.Errorf("delete by a reader: %v", err)
		}
		// The channel must serve the item's collection.
		people2 := n.principalFor("all-preview", rolePolicy("content.read", nil), rolePolicy("content.preview", nil))
		peopleCh, _ := s.Channels.Get("people")
		if err := s.Content.CanPreview(people2, peopleCh, n.item); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("preview in a channel without the collection: %v", err)
		}
		if err := s.Content.CanPreview(people2, ch, n.item); err != nil {
			t.Errorf("preview: %v", err)
		}
		// A listing needs a type.
		if _, err := s.Content.List(viewer, content.ListOptions{}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("untyped listing: %v", err)
		}
	})
}

// Keys named after singleton types belong to the singleton.
func TestSingletonKeyReserved(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		n := newAuthNode(t, be)
		s := n.app.Services()
		if _, err := s.Content.Create(ctx, content.CreateInput{Type: "article", Key: "settings", Fields: doc(t, `{"title":"Squatter"}`)}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("create with a singleton's key: %v", err)
		}
		key := "settings"
		if _, err := s.Content.Update(ctx, n.item, content.UpdateInput{Key: &key}); !errors.Is(err, content.ErrBadRequest) {
			t.Errorf("rename to a singleton's key: %v", err)
		}
		// An item that took the key before the rule existed does not pass for the singleton.
		now := time.Now().UTC()
		err := s.DB.Tx(ctx, func(tx store.Tx) error {
			return tx.Content().InsertItem(ctx, store.Item{ID: s.IDs.New(), Key: "settings", Type: "article", MainLocale: "en",
				CreatedAt: now, UpdatedAt: now, CreatedBy: "system", UpdatedBy: "system"})
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Content.Singleton(ctx, "settings", content.GetOptions{Draft: true}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("singleton read: %v", err)
		}
		if _, err := s.Content.UpdateSingleton(ctx, "settings", content.UpdateInput{Fields: doc(t, `{"siteName":"x"}`)}); !errors.Is(err, content.ErrDuplicateKey) {
			t.Errorf("singleton write: %v", err)
		}
		if _, err := s.Content.PublishSingleton(ctx, "settings", content.PublishOptions{}); !errors.Is(err, content.ErrNotFound) {
			t.Errorf("singleton publish: %v", err)
		}
	})
}

const indexedLater = `
types:
  note:
    name: "{title}"
    fields:
      title:    { type: string, required: true }
      subtitle: { type: string, %s }
`

// Rows missing after a schema change (a field newly indexed) are rebuilt once at startup, not trusted by edits.
func TestProjectionsRebuiltWhenSchemaChanges(t *testing.T) {
	forEachBackend(t, func(t *testing.T, be backend) {
		clk := newClock()
		e := newEnv(t, be.open(t, clk), clk, envConfig{schema: fmt.Sprintf(indexedLater, "")})
		v := e.create("note", "en", `{"title":"Plain","subtitle":"hello"}`)
		e.publish(v.ID)
		if built, err := e.svc.EnsureWorkingProjections(ctx); err != nil || !built {
			t.Fatalf("first build: %v %v", built, err)
		}
		if built, err := e.svc.EnsureWorkingProjections(ctx); err != nil || built {
			t.Fatalf("same schema must not rebuild: %v %v", built, err)
		}
		next, err := content.New(content.Options{DB: e.db, Schema: buildSchema(t, fmt.Sprintf(indexedLater, "index: true")), Outbox: e.ob,
			Clock: clk, IDs: ids.NewGenerator(clk, nil), DefaultLocale: "en"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := next.EnsureSchemaRev(ctx); err != nil {
			t.Fatal(err)
		}
		found := func(status store.Status) int {
			t.Helper()
			res, err := next.List(ctx, content.ListOptions{Type: "note", Status: status,
				Where: []content.Filter{{Field: "subtitle", Op: store.OpEq, Value: "hello"}}})
			if err != nil {
				t.Fatal(err)
			}
			return res.Total
		}
		if found(store.StatusAny) != 0 {
			t.Fatal("the new index has rows before any rebuild")
		}
		if built, err := next.EnsureWorkingProjections(ctx); err != nil || !built {
			t.Fatalf("schema change: %v %v", built, err)
		}
		if found(store.StatusAny) != 1 || found(store.StatusPublished) != 1 {
			t.Errorf("rows after the rebuild: working %d, published %d", found(store.StatusAny), found(store.StatusPublished))
		}
	})
}

// Authorization inside transactions must not take a second pooled connection: with a small pool, concurrent
// requests of a limited principal each held one connection and waited for another.
func TestSmallPoolDoesNotDeadlock(t *testing.T) {
	clk := newClock()
	url := pgtest.Scratch(t)
	sep := "?"
	if strings.Contains(url, "?") {
		sep = "&"
	}
	db, err := postgres.Open(ctx, url+sep+"pool_max_conns=4", store.OpenOptions{Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(ctx, nil); err != nil {
		t.Fatal(err)
	}
	n := &authNode{dnode: newDeliveryNode(t, db, clk)}
	s := n.app.Services()
	if err := s.Admin.SeedTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	limited := n.principalFor("limited", rolePolicy("content.read", map[string][]string{"collection": {"article"}}),
		rolePolicy("content.create", map[string][]string{"collection": {"article"}}),
		rolePolicy("content.edit", map[string][]string{"collection": {"article"}}),
		rolePolicy("content.publish", map[string][]string{"collection": {"article"}}),
		rolePolicy("content.delete", map[string][]string{"collection": {"article"}}))

	done := make(chan struct{})
	var wg sync.WaitGroup
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				s.Access.Invalidate() // grants reload inside the transactions
				time.Sleep(time.Millisecond)
			}
		}
	}()
	fail := make(chan error, 16)
	for w := range 12 {
		wg.Go(func() {
			cctx, cancel := context.WithTimeout(limited, 30*time.Second)
			defer cancel()
			for i := range 15 {
				v, err := s.Content.Create(cctx, content.CreateInput{Type: "article", Fields: doc(t, fmt.Sprintf(`{"title":"w%d-%d"}`, w, i))})
				if err == nil {
					_, err = s.Content.Update(cctx, v.ID, content.UpdateInput{Fields: doc(t, `{"rank":2}`)})
				}
				if err == nil {
					_, err = s.Content.Publish(cctx, v.ID, content.PublishOptions{})
				}
				if err == nil {
					_, err = s.Content.Get(cctx, v.ID, content.GetOptions{})
				}
				if err == nil {
					_, err = s.Content.List(cctx, content.ListOptions{Type: "article", Limit: 5})
				}
				if err == nil {
					_, _, err = s.Content.ReferencesVisible(cctx, v.ID)
				}
				if err == nil {
					err = s.Content.Delete(cctx, v.ID)
				}
				if err != nil {
					fail <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(done)
	select {
	case err := <-fail:
		t.Fatal(err)
	default:
	}
}
