package client

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// RestoreVersion writes a stored version's shared part and the given locales (nil: every locale of the
// version) into the item's draft. Nothing is published.
func (m *Manage) RestoreVersion(ctx context.Context, id string, version int, locales []string) (*View, error) {
	body := map[string]any{}
	if len(locales) > 0 {
		body["locales"] = locales
	}
	return m.view(ctx, http.MethodPost, "/items/"+url.PathEscape(id)+"/versions/"+strconv.Itoa(version)+"/restore", "", body)
}
