package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/hydrogencms/hydrogen/cachetag"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/jobs"
	"github.com/hydrogencms/hydrogen/outbox"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
	"github.com/hydrogencms/hydrogen/webhook"
)

// PurgeConsumer is the name of the outbox consumer that broadcasts cache purges.
const PurgeConsumer = "cache.purge"

type job struct {
	name, schedule string
	fn             func(ctx context.Context, s *Services) error
}

// TagMapper adds a mapping from outbox events to the cache tags they invalidate. Core maps item.* events;
// tags from every mapper are merged for purges and webhooks.
func (r *Registry) TagMapper(m cachetag.Mapper) {
	r.mappers = append(r.mappers, m)
}

// Job schedules fn: schedule is a 5-field cron expression (UTC) or "@every <duration>". Across servers that
// share a database, each tick runs once. Job names are unique across modules.
func (r *Registry) Job(name, schedule string, fn func(ctx context.Context, s *Services) error) error {
	if err := jobs.ValidateName(name); err != nil {
		return err
	}
	if _, err := jobs.Parse(schedule); err != nil {
		return fmt.Errorf("job %q: %w", name, err)
	}
	if fn == nil {
		return fmt.Errorf("job %q: no function", name)
	}
	for _, j := range r.jobs {
		if j.value.name == name {
			return fmt.Errorf("job %q registered by both %s and %s", name, j.module, r.current)
		}
	}
	r.jobs = append(r.jobs, registered[job]{r.current, job{name, schedule, fn}})
	return nil
}

// tagMappers returns the core item mapper followed by the modules' mappers.
func tagMappers(s *schema.Schema, extra []cachetag.Mapper) []cachetag.Mapper {
	core := cachetag.Items(func(typ string) bool {
		t, ok := s.Type(typ)
		return ok && t.Singleton
	})
	return append([]cachetag.Mapper{core, importedMapper}, extra...)
}

// Tags returns the cache tags an outbox event invalidates, sorted.
func (s *Services) Tags(topic string, payload json.RawMessage) []string {
	return cachetag.Tags(s.mappers, topic, payload)
}

func purgeConsumer(db store.DB, mappers []cachetag.Mapper) outbox.Consumer {
	return outbox.Consumer{
		Name:   PurgeConsumer,
		Topics: []string{"*"},
		Handle: func(ctx context.Context, e outbox.Event) error {
			chunks := cachetag.Chunks(cachetag.Tags(mappers, e.Topic, e.Payload), cachetag.MaxPayload)
			if len(chunks) == 0 {
				return nil
			}
			return db.Tx(ctx, func(tx store.Tx) error {
				for _, c := range chunks {
					if err := tx.Notify(cachetag.PurgeChannel, c); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
}

func webhookConsumers(hooks map[string]webhook.Config, clk clock.Clock, tags func(string, json.RawMessage) []string) []outbox.Consumer {
	var out []outbox.Consumer
	for _, name := range slices.Sorted(maps.Keys(hooks)) {
		out = append(out, webhook.Consumer(name, hooks[name], webhook.Options{Clock: clk, Tags: tags}))
	}
	return out
}

func validateWebhooks(hooks map[string]webhook.Config, dev bool) error {
	for _, name := range slices.Sorted(maps.Keys(hooks)) {
		if err := hooks[name].Validate(name, dev); err != nil {
			return err
		}
	}
	return nil
}

// RunJob runs the named job now, under its lock (job:run). It fails if another server is running it.
func (a *App) RunJob(ctx context.Context, name string) error {
	return a.runner.RunNow(ctx, name)
}

// JobNames lists the registered jobs.
func (a *App) JobNames() []string { return a.runner.Names() }

// TickJobs starts the jobs that are due by the clock and waits for them, as App.Run's runner does on a timer.
// It exists for tests with a manual clock.
func (a *App) TickJobs(ctx context.Context) { a.runner.Tick(ctx) }
