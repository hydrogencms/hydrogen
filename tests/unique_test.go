package tests

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/clock"
	"github.com/hydrogencms/hydrogen/core/content"
)

const uniqueSchema = `
types:
  page:
    name: "{title}"
    fields:
      title: { type: string, translatable: true }
      slug:  { type: slug, allow: ".,", unique: true, translatable: true }
      code:  { type: slug, unique: true }
  other:
    fields:
      slug: { type: slug, allow: ".,", unique: true }
`

func uniqueEnv(t *testing.T, b backend) *env {
	clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	return newEnv(t, b.open(t, clk), clk, envConfig{schema: uniqueSchema})
}

func notUnique(t *testing.T, what string, err error, path string) {
	t.Helper()
	var ve *content.ValidationError
	if !errors.As(err, &ve) || !errors.Is(err, content.ErrInvalid) {
		t.Fatalf("%s: %v, want a validation error", what, err)
	}
	for _, fe := range ve.Errors {
		if fe.Path == path && fe.Code == "not_unique" {
			return
		}
	}
	t.Fatalf("%s: %+v, want not_unique at %s", what, ve.Errors, path)
}

func TestUniqueFields(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := uniqueEnv(t, b)
		a := e.create("page", "en", `{"title":"A","slug":"acme,-inc","code":"c1"}`)
		e.publish(a.ID)

		// Dots and commas are stored as given.
		eq(t, "slug", e.get(a.ID, "en", false).Fields["slug"], any("acme,-inc"))

		_, err := e.svc.Create(ctx, content.CreateInput{Type: "page", Fields: doc(t, `{"slug":"acme,-inc"}`)})
		notUnique(t, "create duplicate", err, "slug")
		_, err = e.svc.Create(ctx, content.CreateInput{Type: "page", Fields: doc(t, `{"code":"c1"}`)})
		notUnique(t, "create duplicate shared", err, "code")

		// Another type, another locale and an empty value are no clash.
		e.create("other", "en", `{"slug":"acme,-inc"}`)
		e.create("page", "en", `{"title":"E1"}`)
		e.create("page", "en", `{"title":"E2"}`)
		e.update(a.ID, "de", `{"title":"A de","slug":"benelux-b.v"}`)
		other := e.create("page", "en", `{"title":"B","slug":"b"}`)
		e.update(other.ID, "de", `{"title":"B de","slug":"mine"}`)
		_, err = e.svc.Update(ctx, other.ID, content.UpdateInput{Locale: "de", Fields: doc(t, `{"slug":"benelux-b.v"}`)})
		notUnique(t, "update duplicate in locale", err, "slug")
		e.update(other.ID, "en", `{"slug":"benelux-b.v"}`)

		// Saving the item itself, or an unrelated field, is fine.
		e.update(a.ID, "en", `{"title":"A2","slug":"acme,-inc"}`)

		// A draft slug is claimed in the working projection.
		_, err = e.svc.Update(ctx, other.ID, content.UpdateInput{Locale: "en", Fields: doc(t, `{"slug":"acme,-inc"}`)})
		notUnique(t, "update to a published slug", err, "slug")

		// Trashing frees the slug; restoring fails while another item holds it.
		e.tick()
		if err := e.svc.Trash(ctx, a.ID); err != nil {
			t.Fatal(err)
		}
		c := e.create("page", "en", `{"title":"C","slug":"acme,-inc","code":"c2"}`)
		e.tick()
		_, err = e.svc.Restore(ctx, a.ID)
		notUnique(t, "restore", err, "slug")
		e.tick()
		if err := e.svc.Trash(ctx, c.ID); err != nil {
			t.Fatal(err)
		}
		e.tick()
		if _, err := e.svc.Restore(ctx, a.ID); err != nil {
			t.Fatalf("restore after the holder left: %v", err)
		}
	})
}

func TestUniqueChecksPublish(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := uniqueEnv(t, b)
		a := e.create("page", "en", `{"title":"A","slug":"x"}`)
		e.publish(a.ID)
		// B may draft "x" while A's draft moves away from it; discarding that draft brings the clash back, and
		// publishing B is refused.
		e.update(a.ID, "en", `{"slug":"y"}`)
		bv := e.create("page", "en", `{"title":"B","slug":"x"}`)
		if err := e.svc.Discard(ctx, a.ID, nil); err != nil {
			t.Fatal(err)
		}
		e.tick()
		_, err := e.svc.Publish(ctx, bv.ID, content.PublishOptions{})
		notUnique(t, "publish", err, "slug")
	})
}

func TestUniqueConcurrentCreates(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		e := uniqueEnv(t, b)
		const n = 8
		errs := make([]error, n)
		var wg sync.WaitGroup
		for i := range n {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = e.svc.Create(ctx, content.CreateInput{Type: "page", Fields: doc(t, `{"slug":"race"}`)})
			}()
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case !errors.Is(err, content.ErrInvalid):
				t.Errorf("unexpected error: %v", err)
			}
		}
		eq(t, "created", ok, 1)
	})
}
