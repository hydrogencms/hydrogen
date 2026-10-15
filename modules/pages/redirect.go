package pages

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// routesAtAnyLocale returns the rows at a path in every locale of a site.
func routesAtAnyLocale(ctx context.Context, q store.SQL, site, path string) ([]routeRow, error) {
	var out []routeRow
	err := each(ctx, q, "SELECT "+routeCols+" FROM route WHERE site = ? AND path = ?", []any{site, path},
		func(r store.Rows) error {
			x, err := scanRoute(r)
			out = append(out, x)
			return err
		})
	slices.SortFunc(out, func(a, b routeRow) int { return strings.Compare(a.Locale, b.Locale) })
	return out, err
}

// syncRedirect makes a published redirect item the manual route of its `from` path (replacing its earlier one).
func (s *service) syncRedirect(ctx context.Context, q store.SQL, item ids.UUID, doc map[string]any) ([]change, error) {
	r, problems := s.redirectOf(doc)
	if _, err := q.Exec(ctx, "DELETE FROM route WHERE item_id = ? AND state = 'manual'", item); err != nil {
		return nil, err
	}
	if len(problems) > 0 {
		s.log.WarnContext(ctx, "pages: invalid redirect skipped", "item", item, "field", problems[0].field, "problem", problems[0].msg)
		return nil, nil
	}
	row := []any{r.Site, r.Locale, r.From, StateManual, &item, nil, r.To, r.Status, s.clock.Now()}
	if err := s.upsertRoutes(ctx, q, [][]any{row}); err != nil {
		return nil, fmt.Errorf("redirect %s: %w", r.From, err)
	}
	return []change{{Site: r.Site}}, nil
}
