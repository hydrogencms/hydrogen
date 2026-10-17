package tests

import (
	"strings"
	"testing"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
)

func (e *env) forChannel(id ids.UUID, ch *channel.Channel, draft bool) (*content.View, error) {
	e.t.Helper()
	return e.svc.GetForChannel(ctx, id, ch, content.ChannelGetOptions{Draft: draft})
}

// translated publishes an article in en and adds published de and fr translations.
func (e *env) translated(title string, locales ...string) ids.UUID {
	e.t.Helper()
	v := e.create("article", "en", `{"title":"`+title+` en"}`)
	for _, l := range locales {
		e.update(v.ID, l, `{"title":"`+title+` `+l+`"}`)
	}
	e.publish(v.ID)
	return v.ID
}

func TestGetForChannelPicksLocale(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		id := e.translated("A", "de")
		dech := &channel.Channel{Name: "dech", Languages: []string{"de-CH", "de", "en"}}
		v, err := e.forChannel(id, dech, false)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "locale", v.Locale, "de")
		eq(t, "name", v.Name, "A de")

		en := &channel.Channel{Name: "en", Languages: []string{"en"}}
		v, err = e.forChannel(id, en, false)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "locale", v.Locale, "en")

		fr := &channel.Channel{Name: "fr", Languages: []string{"fr"}}
		_, err = e.forChannel(id, fr, false)
		wantErr(t, err, content.ErrNotFound)
	})
}

func TestGetForChannelAlwaysAvailable(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		id := e.translated("A")
		fr := &channel.Channel{Name: "fr", Languages: []string{"fr"}}
		_, err := e.forChannel(id, fr, false)
		wantErr(t, err, content.ErrNotFound)

		always := true
		e.tick()
		if _, err := e.svc.Update(ctx, id, content.UpdateInput{AlwaysAvailable: &always}); err != nil {
			t.Fatal(err)
		}
		v, err := e.forChannel(id, fr, false)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "locale", v.Locale, "en")
		eq(t, "mainLocale", v.MainLocale, "en")
	})
}

func TestGetForChannelCollections(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		art := e.article("Hi")
		per := e.person("Ann")
		app := &channel.Channel{Name: "app", Languages: []string{"en"}, Collections: []string{"person"}}
		_, err := e.forChannel(art.ID, app, false)
		wantErr(t, err, content.ErrNotFound)
		v, err := e.forChannel(per.ID, app, false)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "type", v.Type, "person")
		all := &channel.Channel{Name: "all", Languages: []string{"en"}}
		if _, err := e.forChannel(art.ID, all, false); err != nil {
			t.Fatal(err)
		}
		wantErr(t, func() error { _, err := e.forChannel(ids.UUID{}, all, false); return err }(), content.ErrNotFound)
	})
}

func TestGetForChannelDrafts(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		never := e.create("article", "en", `{"title":"Draft only"}`)
		ch := &channel.Channel{Name: "web", Languages: []string{"de", "en"}}
		_, err := e.forChannel(never.ID, ch, false)
		wantErr(t, err, content.ErrNotFound)
		v, err := e.forChannel(never.ID, ch, true)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "locale", v.Locale, "en")
		eq(t, "draft", v.Draft, true)

		id := e.translated("B")
		e.update(id, "de", `{"title":"B de draft"}`)
		v, err = e.forChannel(id, ch, false)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "published view locale", v.Locale, "en")
		v, err = e.forChannel(id, ch, true)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "draft view locale", v.Locale, "de")
		eq(t, "draft view name", v.Name, "B de draft")
	})
}

func TestGetForChannelNilChannel(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		_, err := e.forChannel(e.article("x").ID, nil, false)
		wantErr(t, err, content.ErrBadRequest)
	})
}

const channelKindsSchema = `
types:
  note:
    fields:
      title: { type: string, translatable: true, required: true }
  landing:
    page: true
    fields:
      title: { type: string, translatable: true, required: true }
  settings:
    singleton: true
    fields:
      siteName: { type: string, translatable: true, required: true }
`

func TestGetForChannelSingletonAndPageType(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{schema: channelKindsSchema})
		set := e.publish(e.create("settings", "en", `{"siteName":"Acme"}`).ID)
		page := e.publish(e.create("landing", "en", `{"title":"Home"}`).ID)
		all := &channel.Channel{Name: "all", Languages: []string{"en"}}
		for _, id := range []ids.UUID{set.ID, page.ID} {
			if _, err := e.forChannel(id, all, false); err != nil {
				t.Fatal(err)
			}
		}
		only := &channel.Channel{Name: "only", Languages: []string{"en"}, Collections: []string{"settings", "landing"}}
		for _, id := range []ids.UUID{set.ID, page.ID} {
			if _, err := e.forChannel(id, only, false); err != nil {
				t.Fatal(err)
			}
		}
		notes := &channel.Channel{Name: "notes", Languages: []string{"en"}, Collections: []string{"note"}}
		for _, id := range []ids.UUID{set.ID, page.ID} {
			_, err := e.forChannel(id, notes, false)
			wantErr(t, err, content.ErrNotFound)
		}
	})
}

func TestGetForChannelMainLocaleOnlyAsDraft(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		e := newEnv(t, b.open(t, clk), clk, envConfig{schema: channelKindsSchema})
		v := e.create("note", "en", `{"title":"Main"}`)
		e.update(v.ID, "de", `{"title":"Haupt"}`)
		always := true
		e.tick()
		if _, err := e.svc.Update(ctx, v.ID, content.UpdateInput{AlwaysAvailable: &always}); err != nil {
			t.Fatal(err)
		}
		fr := &channel.Channel{Name: "fr", Languages: []string{"fr"}}
		_, err := e.forChannel(v.ID, fr, false)
		wantErr(t, err, content.ErrNotFound)
		got, err := e.forChannel(v.ID, fr, true)
		if err != nil {
			t.Fatal(err)
		}
		eq(t, "draft fallback locale", got.Locale, "en")
	})
}

func TestGetForChannelErrorsAreIndistinguishable(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		pub := e.article("Published")
		draft := e.create("article", "en", `{"title":"Draft"}`)
		per := e.person("Ann")
		ch := &channel.Channel{Name: "app", Languages: []string{"en"}, Collections: []string{"article"}}
		fr := &channel.Channel{Name: "app", Languages: []string{"fr"}, Collections: []string{"article"}}
		msg := func(id ids.UUID, c *channel.Channel) string {
			_, err := e.forChannel(id, c, false)
			wantErr(t, err, content.ErrNotFound)
			return strings.ReplaceAll(err.Error(), id.String(), "ID")
		}
		want := msg(per.ID, ch)
		for name, got := range map[string]string{"unpublished": msg(draft.ID, ch), "no locale": msg(pub.ID, fr)} {
			if got != want {
				t.Errorf("%s: %q differs from %q", name, got, want)
			}
		}
	})
}
