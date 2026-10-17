package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/api/webhook"
	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/content"
	"github.com/hydrogencms/hydrogen/kernel"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/store"
)

func TestDraftWritesMapToNoTags(t *testing.T) {
	run(t, func(t *testing.T, e *env) {
		tags := func(ev outbox.Event) []string {
			return cachetag.Tags([]cachetag.Mapper{cachetag.Items(func(string) bool { return false })}, ev.Topic, ev.Payload)
		}
		a := e.article("A")
		e.drain()

		// An autosave of a published item purges nothing.
		e.update(a.ID, "en", `{"summary":"draft text"}`)
		evs := e.drain()
		if len(evs) != 1 || evs[0].Topic != "item.updated" || tags(evs[0]) != nil {
			t.Fatalf("autosave: %+v %v", evs, tags(evs[0]))
		}
		var p struct{ Draft bool }
		_ = json.Unmarshal(evs[0].Payload, &p)
		if !p.Draft {
			t.Errorf("payload lacks draft: %s", evs[0].Payload)
		}
		// So does discarding it.
		if err := e.svc.Discard(ctx, a.ID, nil); err != nil {
			t.Fatal(err)
		}
		evs = e.drain()
		if len(evs) != 1 || evs[0].Topic != "item.updated" || tags(evs[0]) != nil {
			t.Fatalf("discard: %+v", evs)
		}
		// A new draft item purges nothing either.
		e.create("article", "en", `{"title":"New"}`)
		evs = e.drain()
		if len(evs) != 1 || evs[0].Topic != "item.created" || tags(evs[0]) != nil {
			t.Fatalf("create: %+v", evs)
		}
		// A key change of a published item applies at once, so it keeps its tags.
		k := "alpha"
		if _, err := e.svc.Update(ctx, a.ID, content.UpdateInput{Key: &k}); err != nil {
			t.Fatal(err)
		}
		evs = e.drain()
		if len(evs) != 1 || len(tags(evs[0])) != 2 {
			t.Fatalf("key change: %+v %v", evs, tags(evs[0]))
		}
		// And so does availability.
		on := true
		if _, err := e.svc.Update(ctx, a.ID, content.UpdateInput{AlwaysAvailable: &on}); err != nil {
			t.Fatal(err)
		}
		if evs = e.drain(); len(evs) != 1 || len(tags(evs[0])) != 2 {
			t.Fatalf("availability: %+v", evs)
		}
		// Publishing still purges.
		e.update(a.ID, "en", `{"summary":"x"}`)
		e.drain()
		e.publish(a.ID)
		if evs = e.drain(); len(evs) != 1 || len(tags(evs[0])) != 2 {
			t.Fatalf("publish: %+v", evs)
		}
	})
}

func TestSlowSubscriberGetsClearAll(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		lctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		const channel = "fix_overflow"
		ch, err := db.Notifier().Listen(lctx, channel)
		if err != nil {
			t.Fatal(err)
		}
		// The subscriber does not read while far more notifications than its buffer holds arrive.
		const sent = 300
		for i := range sent {
			err := db.Tx(ctx, func(tx store.Tx) error { return tx.Notify(channel, fmt.Sprintf("p%d", i)) })
			if err != nil {
				t.Fatal(err)
			}
		}
		time.Sleep(200 * time.Millisecond)
		got, sawEmpty := 0, false
		deadline := time.After(5 * time.Second)
		for !sawEmpty {
			select {
			case n := <-ch:
				got++
				sawEmpty = n.Payload == ""
			case <-deadline:
				t.Fatalf("no clear-all signal after %d notifications", got)
			}
		}
		if got >= sent {
			t.Errorf("expected drops covered by the signal, got %d of %d", got, sent)
		}
		// Afterwards notifications flow normally again.
		if err := db.Tx(ctx, func(tx store.Tx) error { return tx.Notify(channel, "after") }); err != nil {
			t.Fatal(err)
		}
		select {
		case n := <-ch:
			if n.Payload != "after" {
				t.Errorf("after recovery: %+v", n)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no notification after recovery")
		}
	})
}

func TestSlowConsumerDoesNotDelayOthers(t *testing.T) {
	forEachBackend(t, func(t *testing.T, b backend) {
		clk := newClock()
		db := b.open(t, clk)
		release := make(chan struct{})
		slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			<-release
		}))
		defer slow.Close()
		defer close(release)
		var fast atomic.Int32
		hook := webhook.Consumer("slow", webhook.Config{URL: slow.URL, Events: []string{"*"}, Secret: "s", Timeout: time.Minute},
			webhook.Options{Clock: clk})
		purge := outbox.Consumer{Name: kernel.PurgeConsumer, Topics: []string{"*"}, Handle: func(context.Context, outbox.Event) error {
			fast.Add(1)
			return nil
		}}
		ob, err := outbox.New(db, clk, nil, []outbox.Consumer{purge, hook}, outbox.Options{Poll: 20 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		rctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Go(func() { _ = ob.Run(rctx) })
		defer func() { cancel(); wg.Wait() }()
		time.Sleep(100 * time.Millisecond)
		for i := range 3 {
			err := db.Tx(ctx, func(tx store.Tx) error {
				_, err := ob.Publish(ctx, tx, "item.published", map[string]int{"n": i})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		deadline := time.Now().Add(3 * time.Second)
		for fast.Load() < 3 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if got := fast.Load(); got != 3 {
			t.Fatalf("cache.purge delivered %d of 3 while the webhook was stuck", got)
		}
	})
}

func TestJobsEnabledFalseDoesNotRunJobs(t *testing.T) {
	start := func(t *testing.T, appYAML string, runNow bool) (ran func() int32) {
		clk := newClock()
		db := openSQLite(t, clk)
		var n atomic.Int32
		mod := fnMod{"jobsmod", func(r *kernel.Registry) error {
			return r.Job("count", "@every 1h", func(context.Context, *kernel.Services) error { n.Add(1); return nil })
		}}
		app := newEventsApp(t, db, clk, "http: { addr: \"127.0.0.1:0\" }\n"+appYAML, 0, mod)
		rctx, cancel := context.WithCancel(context.Background())
		up := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = app.Run(rctx, func(string) { close(up) })
		}()
		t.Cleanup(func() { cancel(); <-done })
		select {
		case <-up:
		case <-time.After(5 * time.Second):
			t.Fatal("server did not start")
		}
		time.Sleep(50 * time.Millisecond)
		clk.Advance(2 * time.Hour)
		if runNow { // job:run works with the runner off
			if err := app.RunJob(ctx, "count"); err != nil {
				t.Fatal(err)
			}
		}
		return n.Load
	}
	t.Run("enabled", func(t *testing.T) {
		ran := start(t, "", false)
		deadline := time.Now().Add(4 * time.Second)
		for ran() < 1 && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if ran() < 1 {
			t.Fatalf("runner did not run the due job: %d", ran())
		}
	})
	t.Run("disabled", func(t *testing.T) {
		ran := start(t, "jobs: { enabled: false }\n", true)
		time.Sleep(1500 * time.Millisecond)
		if ran() != 1 {
			t.Fatalf("jobs ran on a node with jobs.enabled false: %d", ran())
		}
	})
}
