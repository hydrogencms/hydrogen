package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/hooks"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

// hasInheritance reports whether the language registry has a language that inherits from another. Only then
// does the module read resolved documents from the delivery snapshots: an overlay locale's stored document is
// sparse, and a locale without one is derived from its parent, so neither can be routed from the version.
func hasInheritance(s *channel.Set) bool {
	if s == nil {
		return false
	}
	for _, l := range s.Languages() {
		if l.Inherits != "" {
			return true
		}
	}
	return false
}

// snapshotDocs reads the resolved document of every locale an item is published in from its delivery
// snapshots, derived locales included. Names are the resolved names.
func snapshotDocs(ctx context.Context, q store.SQL, item ids.UUID) ([]publishedDoc, error) {
	var out []publishedDoc
	err := each(ctx, q, "SELECT locale, payload FROM published_snapshot WHERE item_id = ?", []any{item},
		func(r store.Rows) error {
			var locale, payload string
			if err := r.Scan(&locale, &payload); err != nil {
				return err
			}
			var p struct {
				Name   string          `json:"name"`
				Fields json.RawMessage `json:"fields"`
			}
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return fmt.Errorf("pages: snapshot of %s/%s: %w", item, locale, err)
			}
			doc := map[string]any{}
			if len(p.Fields) > 0 {
				var err error
				if doc, err = schema.DecodeDoc(p.Fields); err != nil {
					return fmt.Errorf("pages: snapshot of %s/%s: %w", item, locale, err)
				}
			}
			out = append(out, publishedDoc{locale, p.Name, doc})
			return nil
		})
	slices.SortFunc(out, func(a, b publishedDoc) int { return strings.Compare(a.Locale, b.Locale) })
	return out, err
}

// publishedDocs is the package function plus, with locale inheritance, the resolved documents of the item's
// snapshots: they replace the version's sparse documents and add the locales derived from a parent.
func (s *service) publishedDocs(ctx context.Context, tx store.Tx, it store.Item) ([]publishedDoc, error) {
	docs, err := publishedDocs(ctx, tx.Content(), it)
	if err != nil || !s.inherits || it.PublishedVersion == 0 {
		return docs, err
	}
	snaps, err := snapshotDocs(ctx, tx.SQL(), it.ID)
	if err != nil {
		return nil, err
	}
	for _, d := range snaps {
		if i := slices.IndexFunc(docs, func(x publishedDoc) bool { return x.Locale == d.Locale }); i >= 0 {
			docs[i] = d
		} else {
			docs = append(docs, d)
		}
	}
	slices.SortFunc(docs, func(a, b publishedDoc) int { return strings.Compare(a.Locale, b.Locale) })
	return docs, nil
}

// syncResolved brings the routes of an item in line with the resolved documents of its snapshots: the locale
// that was just published (whose hook document may be sparse), its stored descendants, and the locales derived
// from it. A page whose derived path is held by another owner keeps its old route (and a warning), instead of
// failing the publish of its parent locale.
func (s *service) syncResolved(hc *hooks.Context, tx store.Tx, class itemClass) ([]change, error) {
	docs, err := snapshotDocs(hc, tx.SQL(), hc.Item.ID)
	if err != nil {
		return nil, err
	}
	var out []change
	for _, d := range docs {
		var cs []change
		switch class {
		case classPage:
			cs, err = s.syncDerivedPage(hc, tx, hc.Item.ID, hc.Item.MainLocale, d.Locale, s.slugRowOfNamed(hc.Item.Type, d))
		case classRedirect:
			continue
		default:
			cs, err = s.syncMountedItem(hc, tx, hc.Item.ID, hc.Item.Type, d.Locale, d.Doc)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.Locale, err)
		}
		out = append(out, cs...)
	}
	return out, nil
}

// slugRowOfNamed is slugRowOf with the snapshot's name when the schema has no name field to compute.
func (s *service) slugRowOfNamed(typ string, d publishedDoc) slugRow {
	row := s.slugRowOf(typ, d.Doc)
	if row.Name == "" {
		row.Name = d.Name
	}
	return row
}

func (s *service) syncDerivedPage(ctx context.Context, tx store.Tx, item ids.UUID, mainLocale, locale string, row slugRow) ([]change, error) {
	q := tx.SQL()
	n, ok, err := s.lockedNodeByItem(ctx, tx, item)
	if err != nil || !ok {
		return nil, err
	}
	unchanged, err := slugUnchanged(ctx, q, n, locale, mainLocale, row)
	if err != nil {
		return nil, err
	}
	if !unchanged {
		plan, _, err := s.planPage(ctx, q, n, locale, mainLocale, row)
		if err != nil {
			return nil, err
		}
		if len(plan.clashes) > 0 {
			s.log.WarnContext(ctx, "pages: route of a derived locale skipped, its path is taken",
				"item", item, "locale", locale, "clash", plan.clashes[0].String())
			return nil, nil
		}
	}
	return s.syncPage(ctx, tx, item, mainLocale, locale, row)
}
