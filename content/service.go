package content

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hydrogencms/hydrogen/audit"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/hooks"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

const (
	maxKeyLen      = 128
	fallbackLocale = "en"
)

var (
	localeRE = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	keyRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// Service is safe for concurrent use.
type Service struct {
	o          Options
	compiled   []byte                       // canonical JSON of the current schema
	slugs      map[string]map[string]string // type → slug field → source field
	migrations []Migration                  // sorted by name
	rev        atomic.Int64                 // schema revision recorded by EnsureSchemaRev
}

// New checks the options and returns the service. DB, Schema and Outbox are required; the other options
// have defaults (system clock, empty hook registry, "en").
func New(o Options) (*Service, error) {
	switch {
	case o.DB == nil:
		return nil, errors.New("content: Options.DB is required")
	case o.Schema == nil:
		return nil, errors.New("content: Options.Schema is required")
	case o.Outbox == nil:
		return nil, errors.New("content: Options.Outbox is required")
	}
	if o.Clock == nil {
		o.Clock = clock.System{}
	}
	if o.Hooks == nil {
		o.Hooks = &hooks.Registry{}
	}
	if o.Audit == nil {
		o.Audit = audit.New(o.Clock)
	}
	if o.IDs == nil {
		o.IDs = ids.NewGenerator(o.Clock, nil)
	}
	o.DefaultLocale = cmp.Or(o.DefaultLocale, fallbackLocale)
	if !localeRE.MatchString(o.DefaultLocale) {
		return nil, fmt.Errorf("content: invalid default locale %q", o.DefaultLocale)
	}
	migrations, err := sortMigrations(o.Migrations)
	if err != nil {
		return nil, err
	}
	compiled, err := o.Schema.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("content: encode schema: %w", err)
	}
	return &Service{o: o, compiled: compiled, slugs: slugSources(o.Schema), migrations: migrations}, nil
}

func (s *Service) Schema() *schema.Schema { return s.o.Schema }

// EnsureSchemaRev records the current schema as a revision (idempotent) and must run after database
// migrations, before writes.
func (s *Service) EnsureSchemaRev(ctx context.Context) (int, error) {
	var rev int
	err := s.o.DB.Tx(ctx, func(tx store.Tx) (err error) {
		rev, err = tx.Content().AddSchemaRev(ctx, s.o.Schema.Hash(), s.compiled, s.o.Clock.Now())
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("content: record schema revision: %w", err)
	}
	s.rev.Store(int64(rev))
	return rev, nil
}

func (s *Service) currentRev() (int, error) {
	rev := int(s.rev.Load())
	if rev == 0 {
		return 0, errors.New("content: schema revision not recorded; call EnsureSchemaRev after migrating the database")
	}
	return rev, nil
}

func (s *Service) typeOf(name string) (*schema.Type, error) {
	t, ok := s.o.Schema.Type(name)
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownType, name)
	}
	return t, nil
}

func badRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadRequest, fmt.Sprintf(format, args...))
}

func validateLocale(l string) error {
	if !localeRE.MatchString(l) {
		return badRequest("invalid locale %q", l)
	}
	return nil
}

func validateKey(k string) error {
	if len(k) > maxKeyLen || !keyRE.MatchString(k) {
		return badRequest("invalid key %q: use lowercase letters, digits, '-' and '_' (at most %d characters)", k, maxKeyLen)
	}
	return nil
}

// getItem reads an item that is not in the trash; a trashed item is ErrNotFound.
func getItem(ctx context.Context, tx store.Tx, id ids.UUID, forUpdate bool) (store.Item, error) {
	it, err := getItemAny(ctx, tx, id, forUpdate)
	if err == nil && it.DeletedAt != nil {
		return it, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return it, err
}

// getItemAny reads an item, trashed or not.
func getItemAny(ctx context.Context, tx store.Tx, id ids.UUID, forUpdate bool) (store.Item, error) {
	it, err := tx.Content().GetItem(ctx, id, forUpdate)
	return it, itemErr(id.String(), err)
}

func getItemByKey(ctx context.Context, tx store.Tx, key string) (store.Item, error) {
	it, err := tx.Content().GetItemByKey(ctx, key)
	return it, itemErr("key "+key, err)
}

func itemErr(what string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("%w: %s", ErrNotFound, what)
	case errors.Is(err, store.ErrDuplicateKey):
		return fmt.Errorf("%w: %s", ErrDuplicateKey, what)
	}
	return fmt.Errorf("content: %s: %w", what, err)
}

func (s *Service) now() time.Time { return s.o.Clock.Now() }

// eventPayload is the payload of item.* outbox events.
type eventPayload struct {
	ID      ids.UUID `json:"id"`
	Type    string   `json:"type"`
	Version int      `json:"version"`
	Locales []string `json:"locales"`
	// Draft marks writes that changed no published data (autosaves, discards): cache tags ignore them.
	Draft bool `json:"draft,omitempty"`
}

func (s *Service) emit(ctx context.Context, tx store.Tx, topic string, it store.Item, version int, locales []string) error {
	return s.emitEvent(ctx, tx, topic, eventPayload{ID: it.ID, Type: it.Type, Version: version, Locales: locales})
}

func (s *Service) emitEvent(ctx context.Context, tx store.Tx, topic string, p eventPayload) error {
	if p.Locales == nil {
		p.Locales = []string{}
	}
	_, err := s.o.Outbox.Publish(ctx, tx, topic, p)
	if err != nil {
		return fmt.Errorf("content: outbox %s: %w", topic, err)
	}
	return nil
}

func (s *Service) record(ctx context.Context, tx store.Tx, actor audit.Actor, action string, id ids.UUID, data any) error {
	err := s.o.Audit.Write(ctx, tx, audit.Entry{Actor: actor, Action: action, Subject: id.String(), Data: data})
	if err != nil {
		return fmt.Errorf("content: audit %s: %w", action, err)
	}
	return nil
}

func (s *Service) runHooks(ctx context.Context, tx store.Tx, it store.Item, ev hooks.Event, locale string, doc map[string]any) (*schema.Report, error) {
	hc := &hooks.Context{
		Context: ctx, Event: ev, Tx: tx, Locale: locale, Doc: doc, Errors: &schema.Report{},
		Item: hooks.Item{ID: it.ID, Type: it.Type, Key: it.Key, Version: it.PublishedVersion, MainLocale: it.MainLocale},
	}
	if err := s.o.Hooks.Run(hc, it.Type); err != nil {
		return nil, err
	}
	return hc.Errors, nil
}

// slugSources maps type → slug field → the field its value is generated from (the slug's `from` option).
func slugSources(sc *schema.Schema) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, t := range sc.Types() {
		for _, f := range t.Fields {
			if f.Type != "slug" {
				continue
			}
			if from, ok := sc.Option(f, "from"); ok {
				if out[t.Name] == nil {
					out[t.Name] = map[string]string{}
				}
				out[t.Name][f.Name] = from.(string)
			}
		}
	}
	return out
}

func sortMigrations(in []Migration) ([]Migration, error) {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b Migration) int { return strings.Compare(a.Name, b.Name) })
	for i, m := range out {
		switch {
		case m.Name == "" || m.Type == "" || m.Up == nil:
			return nil, fmt.Errorf("content: migration %q needs a name, a type and an Up function", m.Name)
		case i > 0 && out[i-1].Name == m.Name:
			return nil, fmt.Errorf("content: duplicate migration %q", m.Name)
		}
	}
	return out, nil
}

func isNotFound(err error) bool { return errors.Is(err, ErrNotFound) }
