package pages

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/core/access"
	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// UnplacedItem is a page item that is in no site's tree.
type UnplacedItem struct {
	ID        ids.UUID  `json:"id"`
	Type      string    `json:"type"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UnplacedList is one page of unplaced items, newest first, with the number of items the principal can read.
type UnplacedList struct {
	Items []UnplacedItem `json:"items"`
	Total int            `json:"total"`
}

// maxUnplacedScan bounds how many candidates a restricted reader's listing filters one by one.
const maxUnplacedScan = 5000

const unplacedScanBatch = 200

// Unplaced lists page-type items that are not trashed and have no node, newest update first. A reader limited
// by content grants only gets items it can read. item, when set, narrows the list to that one item.
func (s *Service) Unplaced(ctx context.Context, locale string, item *ids.UUID, limit, offset int) (*UnplacedList, error) {
	impl, err := s.get()
	if err != nil {
		return nil, err
	}
	return impl.unplaced(ctx, locale, item, limit, offset)
}

type unplacedRow struct {
	UnplacedItem
	item store.Item
}

func (s *service) pageTypeNames() []string {
	var out []string
	for _, t := range s.schema.Types() {
		if t.Page {
			out = append(out, t.Name)
		}
	}
	slices.Sort(out)
	return out
}

func (s *service) unplaced(ctx context.Context, locale string, item *ids.UUID, limit, offset int) (*UnplacedList, error) {
	types := s.pageTypeNames()
	out := &UnplacedList{Items: []UnplacedItem{}}
	if len(types) == 0 {
		return out, nil
	}
	if err := s.prepare(ctx); err != nil {
		return nil, err
	}
	q := s.db.Read()
	tw, args := inArgs(types)
	where := `n.id IS NULL AND i.deleted_at IS NULL AND i.type IN ` + tw
	if item != nil {
		where += " AND i.id = ?"
		args = append(args, *item)
	}
	if !s.restricted(ctx) {
		if err := q.QueryRow(ctx, "SELECT COUNT(*) FROM item i LEFT JOIN page_node n ON n.item_id = i.id WHERE "+where, args...).Scan(&out.Total); err != nil {
			return nil, err
		}
		rows, err := s.unplacedRows(ctx, q, locale, where, args, limit, offset)
		for _, r := range rows {
			out.Items = append(out.Items, r.UnplacedItem)
		}
		return out, err
	}
	var visible []unplacedRow
	for from := 0; from < maxUnplacedScan; from += unplacedScanBatch {
		rows, err := s.unplacedRows(ctx, q, locale, where, args, unplacedScanBatch, from)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			if s.svcs.Access.Can(ctx, access.PolicyContentRead, access.Target{Type: r.Type, Item: &r.item}) == nil {
				visible = append(visible, r)
			}
		}
		if len(rows) < unplacedScanBatch {
			break
		}
	}
	out.Total = len(visible)
	if offset < len(visible) {
		for _, r := range visible[offset:min(len(visible), offset+limit)] {
			out.Items = append(out.Items, r.UnplacedItem)
		}
	}
	return out, nil
}

// unplacedRows reads one page of candidates. Names come from the published version, else the draft, as the
// tree does; the order is total (update time, then id).
func (s *service) unplacedRows(ctx context.Context, q store.SQL, locale, where string, args []any, limit, offset int) ([]unplacedRow, error) {
	query := `SELECT i.id, i.type, i.main_locale, i.published_version, i.created_at, i.updated_at, i.created_by,
(SELECT l.name FROM item_version_locale l WHERE l.item_id = i.id AND l.version = i.published_version
    AND (l.locale = ? OR l.locale = i.main_locale) ORDER BY CASE WHEN l.locale = ? THEN 0 ELSE 1 END LIMIT 1),
(SELECT d.name FROM draft_part d WHERE d.item_id = i.id AND d.part <> '' AND d.name <> ''
    ORDER BY CASE WHEN d.part = ? THEN 0 WHEN d.part = i.main_locale THEN 1 ELSE 2 END, d.part LIMIT 1),
(SELECT COUNT(*) FROM draft_part d WHERE d.item_id = i.id)
FROM item i LEFT JOIN page_node n ON n.item_id = i.id
WHERE ` + where + ` ORDER BY i.updated_at DESC, i.id DESC LIMIT ? OFFSET ?`
	all := append([]any{locale, locale, locale}, args...)
	all = append(all, limit, offset)
	var out []unplacedRow
	err := each(ctx, q, query, all, func(r store.Rows) error {
		var it store.Item
		var published, drafts *string
		var nDrafts int
		if err := r.Scan(&it.ID, &it.Type, &it.MainLocale, &it.PublishedVersion, &it.CreatedAt, &it.UpdatedAt,
			&it.CreatedBy, &published, &drafts, &nDrafts); err != nil {
			return err
		}
		row := unplacedRow{item: it}
		row.ID, row.Type, row.UpdatedAt = it.ID, it.Type, it.UpdatedAt
		switch {
		case published != nil && *published != "":
			row.Name = *published
		case drafts != nil:
			row.Name = *drafts
		}
		row.Status = content.StatusDraft
		if it.PublishedVersion > 0 {
			row.Status = content.StatusPublished
			if nDrafts > 0 {
				row.Status = content.StatusChanged
			}
		}
		out = append(out, row)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("pages: list unplaced: %w", err)
	}
	return out, nil
}
