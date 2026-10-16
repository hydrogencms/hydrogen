package release

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/manage"
	"github.com/hydrogencms/hydrogen/store"
)

// Get returns a release with its items. Needs content.read.
func (s *Service) Get(ctx context.Context, id ids.UUID) (*View, error) {
	if err := s.requireRead(ctx); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	var v *View
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		ctx := store.WithTx(ctx, tx)
		var err error
		v, err = s.detail(ctx, tx, id)
		return err
	})
	return v, err
}

func (s *Service) detail(ctx context.Context, tx store.Tx, id ids.UUID) (*View, error) {
	r, err := loadRelease(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	items, err := tx.Releases().ReleaseItems(ctx, id)
	if err != nil {
		return nil, err
	}
	names, err := s.actorNames(ctx, tx, r.CreatedBy)
	if err != nil {
		return nil, err
	}
	v := itemRelease(r, len(items), names)
	if v.Items, err = s.itemViews(ctx, tx, items); err != nil {
		return nil, err
	}
	return &v, nil
}

// List lists releases, newest first, with their item counts. Needs content.read.
func (s *Service) List(ctx context.Context, o ListOptions) ([]View, int, error) {
	if err := s.requireRead(ctx); err != nil {
		return nil, 0, err
	}
	var out []View
	var total int
	err := s.o.DB.View(ctx, func(tx store.Tx) error {
		var err error
		out, total, err = s.list(ctx, tx, o)
		return err
	})
	return out, total, err
}

func (s *Service) list(ctx context.Context, tx store.Tx, o ListOptions) ([]View, int, error) {
	limit := o.Limit
	if limit <= 0 {
		limit = 50
	}
	limit = min(limit, maxPage)
	rels, total, err := tx.Releases().ListReleases(ctx, store.ReleaseFilter{Statuses: o.Statuses, Kind: o.Kind, Item: o.Item},
		store.Page{Limit: limit, Offset: max(o.Offset, 0)})
	if err != nil {
		return nil, 0, err
	}
	actors := make([]string, len(rels))
	relIDs := make([]ids.UUID, len(rels))
	for i, r := range rels {
		actors[i], relIDs[i] = r.CreatedBy, r.ID
	}
	names, err := s.actorNames(ctx, tx, actors...)
	if err != nil {
		return nil, 0, err
	}
	counts, err := tx.Releases().ReleaseItemCounts(ctx, relIDs)
	if err != nil {
		return nil, 0, err
	}
	var mine map[ids.UUID]store.ReleaseItem
	if !o.Item.IsZero() {
		of, err := tx.Releases().ReleaseItemsOfItem(ctx, o.Item)
		if err != nil {
			return nil, 0, err
		}
		mine = make(map[ids.UUID]store.ReleaseItem, len(of))
		for _, it := range of {
			mine[it.ReleaseID] = it
		}
	}
	out := make([]View, 0, len(rels))
	for _, r := range rels {
		v := itemRelease(r, counts[r.ID], names)
		if it, ok := mine[r.ID]; ok {
			v.Action, v.Locales = it.Action, it.Locales
		}
		out = append(out, v)
	}
	return out, total, nil
}

// OfItem lists the open and scheduled releases that contain the item, with the item's action in each, for the
// editor. Needs content.read on the item; an item the caller cannot read is not found.
func (s *Service) OfItem(ctx context.Context, item ids.UUID) ([]View, error) {
	if err := s.requireRead(ctx); err != nil {
		return nil, err
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	ok, err := s.o.Content.CanRead(ctx, item)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, manage.NotFound("item %s not found", item)
	}
	out, _, err := s.List(ctx, ListOptions{Statuses: []string{StatusOpen, StatusScheduled}, Item: item, Limit: maxPage})
	return out, err
}

// itemViews describes release items: name and type from the content store, and whether the item still has
// something to do. Items the caller may not read keep their id and action only.
func (s *Service) itemViews(ctx context.Context, tx store.Tx, items []store.ReleaseItem) ([]ItemView, error) {
	c := tx.Content()
	idList := make([]ids.UUID, len(items))
	for i, it := range items {
		idList[i] = it.ItemID
	}
	rows, err := c.GetItems(ctx, idList)
	if err != nil {
		return nil, err
	}
	byID := make(map[ids.UUID]store.Item, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	drafts, err := c.DraftPartsOf(ctx, idList)
	if err != nil {
		return nil, err
	}
	nameOf, err := c.MainNamesOf(ctx, idList)
	if err != nil {
		return nil, err
	}
	out := make([]ItemView, 0, len(items))
	for _, ri := range items {
		v := ItemView{Item: ri.ItemID.String(), Action: ri.Action, Locales: ri.Locales, AddedBy: ri.AddedBy, AddedAt: ri.AddedAt}
		if v.Locales == nil {
			v.Locales = []string{}
		}
		it, ok := byID[ri.ItemID]
		if !ok {
			v.State = StateMissing
			out = append(out, v)
			continue
		}
		v.Type = it.Type
		if ok, err := s.o.Content.CanReadTx(ctx, tx, it); err != nil {
			return nil, err
		} else if !ok {
			v.Type, v.State = "", StateHidden
			out = append(out, v)
			continue
		}
		switch {
		case it.DeletedAt != nil:
			v.State = StateTrashed
		case ri.Action == ActionUnpublish && it.PublishedVersion > 0, ri.Action == ActionPublish && len(drafts[it.ID]) > 0:
			v.State = StateChanges
		default:
			v.State = StateCurrent
		}
		v.Name = nameOf[it.ID]
		out = append(out, v)
	}
	slices.SortStableFunc(out, func(a, b ItemView) int {
		return cmp.Or(a.AddedAt.Compare(b.AddedAt), strings.Compare(a.Item, b.Item))
	})
	return out, nil
}

// actorNames resolves display names of "kind:id" actors: a user's name (else login), a token's name; "" when
// unknown or gone. Each lookup finishes before the next starts.
func (s *Service) actorNames(ctx context.Context, tx store.Tx, actors ...string) (map[string]string, error) {
	names := map[string]string{}
	for _, a := range actors {
		if _, done := names[a]; done {
			continue
		}
		names[a] = ""
		kind, id, ok := strings.Cut(a, ":")
		uid, err := ids.Parse(id)
		if !ok || err != nil {
			continue
		}
		switch kind {
		case "user":
			u, err := tx.Auth().GetUser(ctx, uid)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			names[a] = cmp.Or(u.Name, u.Login)
		case "token", "agent":
			t, err := tx.Auth().GetToken(ctx, uid)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			names[a] = t.Name
		}
	}
	return names, nil
}
