package content

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// Access is what the service asks of the authorizer; *access.Checker satisfies it. Checks apply only to a
// principal in the context (HTTP requests); without one the caller is the system and nothing is checked.
type Access interface {
	// Prepare loads the principal's grants, so checks inside a transaction need no query.
	Prepare(ctx context.Context) error
	Can(ctx context.Context, policy string, t access.Target) error
	// Screen is Can without matching Where rules, for refusing before the document is known.
	Screen(ctx context.Context, policy string, t access.Target) error
	ListScope(ctx context.Context, policy, typ string) (scoped bool, terms []store.ScopeTerm, err error)
	Allowed(ctx context.Context, t access.Target, policies ...string) (map[string]bool, error)
}

// guarded reports whether operations in ctx are checked: not for the system or a super principal.
func (s *Service) guarded(ctx context.Context) bool {
	if s.o.Access == nil {
		return false
	}
	p := access.PrincipalFrom(ctx)
	return p != nil && !p.Super && p.Kind != access.KindSystem
}

// prepare warms the principal's grants before a transaction opens.
func (s *Service) prepare(ctx context.Context) error {
	if !s.guarded(ctx) {
		return nil
	}
	return s.o.Access.Prepare(ctx)
}

// Prepare warms the grants of the principal in ctx. The Tx variants (PublishTx, UnpublishTx) do not, because
// they run inside the caller's transaction, where loading grants through the pool can starve a small one: call
// it once before opening the transaction. It is a no-op for the system.
func (s *Service) Prepare(ctx context.Context) error { return s.prepare(ctx) }

func (s *Service) targetOf(st *itemState, locales []string, doc map[string]any) access.Target {
	it := st.item
	return access.Target{Type: st.typ.Name, Item: &it, Locales: locales, Doc: doc}
}

// ruleDoc is the main-locale document that code access rules and module limitations (a page's mount filters)
// match: the working one, or the published one. It is already in memory wherever a principal is checked. A document that cannot be built is an error, never an empty document: a rule
// such as "kind ne video" would allow it.
func (s *Service) ruleDoc(st *itemState, working bool) (map[string]any, error) {
	code := st.item.MainLocale
	var shared, loc map[string]any
	var err error
	if working || st.pub == nil {
		shared, loc, _, err = st.workingParts(code)
	} else {
		shared, loc, _, err = st.publishedParts(code)
	}
	if err != nil {
		return nil, err
	}
	doc := mergeParts(cloneDoc(shared), cloneDoc(loc))
	s.o.Schema.Normalize(st.typ, doc)
	s.o.Schema.Prune(st.typ, doc)
	return doc, nil
}

// allLocales are the locales a change to the shared part reaches: every locale the working item has, plus
// those written now.
func allLocales(st *itemState, written ...string) []string {
	out := slices.Clone(st.locales(true))
	for _, l := range written {
		if !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	slices.Sort(out)
	return out
}

// notFound is what a principal gets for an item it may not read: the answer for an item that does not exist,
// so neither existence nor state can be probed with ids.
func notFound(err error, id ids.UUID) error {
	if errors.Is(err, access.ErrForbidden) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return err
}

// checkRead authorizes reading an item whose state is loaded; draft says the working version is shown. An item
// the principal may not read is not found.
func (s *Service) checkRead(ctx context.Context, st *itemState, draft bool) error {
	if !s.guarded(ctx) {
		return nil
	}
	doc, err := s.ruleDoc(st, draft)
	if err != nil {
		return err
	}
	return notFound(s.o.Access.Can(ctx, access.PolicyContentRead, s.targetOf(st, nil, doc)), st.item.ID)
}

// check authorizes an action on a loaded item against its working document. Reading comes first: an item the
// principal cannot read is not found, and one it can read but not act on is forbidden.
func (s *Service) check(ctx context.Context, policy string, st *itemState, locales []string) error {
	if !s.guarded(ctx) {
		return nil
	}
	doc, err := s.ruleDoc(st, true)
	if err != nil {
		return err
	}
	if err := notFound(s.o.Access.Can(ctx, access.PolicyContentRead, s.targetOf(st, nil, doc)), st.item.ID); err != nil {
		return err
	}
	return s.o.Access.Can(ctx, policy, s.targetOf(st, locales, doc))
}

// withActions tells the caller what it may do with the item (View.Actions); nil for the system.
func (s *Service) withActions(ctx context.Context, st *itemState, v *View) {
	if v == nil || s.o.Access == nil || access.PrincipalFrom(ctx) == nil {
		return
	}
	doc, err := s.ruleDoc(st, true)
	if err != nil {
		return
	}
	t := s.targetOf(st, []string{v.Locale}, doc)
	got, err := s.o.Access.Allowed(ctx, t, access.PolicyContentEdit, access.PolicyContentPublish, access.PolicyContentDelete)
	if err != nil {
		return
	}
	v.Actions = &Actions{Edit: got[access.PolicyContentEdit], Publish: got[access.PolicyContentPublish],
		Delete: got[access.PolicyContentDelete]}
}

// authorizeItem checks an action on an item that is not otherwise loaded (read first, see check), reading its
// state, which the checks match against. policy read checks reading alone.
func (s *Service) authorizeItem(ctx context.Context, c store.ContentStore, it store.Item, policy string, locales []string) error {
	if !s.guarded(ctx) {
		return nil
	}
	st, err := s.newState(it)
	if err != nil {
		return err
	}
	if err := st.load(ctx, c); err != nil {
		return err
	}
	if policy == access.PolicyContentRead {
		return s.checkRead(ctx, st, true)
	}
	return s.check(ctx, policy, st, locales)
}

// readable reports whether the principal in ctx may read the item.
func (s *Service) readable(ctx context.Context, c store.ContentStore, it store.Item) (bool, error) {
	err := s.authorizeItem(ctx, c, it, access.PolicyContentRead, nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, ErrNotFound):
		return false, nil
	}
	return false, err
}

// CanRead reports whether the principal in ctx may read the item, by the full read check (grants, limitations
// and access rules on its state). A missing item reads as false. Without a principal, or for the system, it
// is true for any item that exists.
func (s *Service) CanRead(ctx context.Context, id ids.UUID) (bool, error) {
	if err := s.prepare(ctx); err != nil {
		return false, err
	}
	var ok bool
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		it, err := tx.Content().GetItem(ctx, id, false)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		ok, err = s.CanReadTx(ctx, tx, it)
		return err
	})
	return ok, err
}

// CanReadTx is CanRead for an item already loaded, inside the caller's transaction. Like the other Tx methods
// it needs Prepare to have run before the transaction opened.
func (s *Service) CanReadTx(ctx context.Context, tx store.Tx, it store.Item) (bool, error) {
	return s.readable(store.WithTx(ctx, tx), tx.Content(), it)
}

// CanPreview refuses unless the principal in ctx may mint a preview token for the item in the channel: it must
// be readable (else not found), the channel must serve its collection, and the principal needs content.preview
// on it. Without a principal it allows. Tokens are not tied to the principal that minted them; they expire
// within an hour.
func (s *Service) CanPreview(ctx context.Context, ch *channel.Channel, id ids.UUID) error {
	return s.CanPreviewItem(ctx, ch, id, false)
}

// CanPreviewItem is CanPreview with a nil channel allowed (no collection check), and with share also requiring
// content.preview.share on the item: the check for creating and revoking share links.
func (s *Service) CanPreviewItem(ctx context.Context, ch *channel.Channel, id ids.UUID, share bool) error {
	if !s.guarded(ctx) {
		return nil
	}
	if err := s.prepare(ctx); err != nil {
		return err
	}
	return s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		it, err := getItem(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if err := s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentRead, nil); err != nil {
			return err
		}
		if ch != nil && !ch.AllowsCollection(it.Type) {
			return fmt.Errorf("%w: %s is not available on channel %s", ErrNotFound, id, ch.Name)
		}
		if err := s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentPreview, nil); err != nil {
			return err
		}
		if share {
			return s.authorizeItem(ctx, tx.Content(), it, access.PolicyContentPreviewShare, nil)
		}
		return nil
	})
}

// CanPreviewChannel refuses unless the principal in ctx may preview every item the channel serves: a
// channel-wide preview token shows all drafts of the channel, so the principal's content.read and
// content.preview grants must not be narrowed (by collection, owner or a Where rule) for any of its types.
func (s *Service) CanPreviewChannel(ctx context.Context, ch *channel.Channel) error {
	if !s.guarded(ctx) {
		return nil
	}
	if err := s.prepare(ctx); err != nil {
		return err
	}
	for _, t := range s.o.Schema.Types() {
		if !ch.AllowsCollection(t.Name) {
			continue
		}
		for _, policy := range []string{access.PolicyContentPreview, access.PolicyContentRead} {
			scoped, _, err := s.o.Access.ListScope(ctx, policy, t.Name)
			if err != nil {
				return err
			}
			if scoped {
				return fmt.Errorf("%w: %s", access.ErrForbidden, policy)
			}
		}
	}
	return nil
}
