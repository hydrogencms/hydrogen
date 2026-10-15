package pages

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/exportfmt"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

// exporter writes the module's tables as typed records (modules/pages.ndjson): nodes first, parents before
// children, then slugs, mount items and routes. Import restores the rows as they are, so paths, sort keys and
// the redirect history are exactly the source's.
type exporter struct{}

// pageRecord is one line: T names the table and selects the fields in use.
type pageRecord struct {
	T string `json:"t"` // node, slug, mount, route

	// node
	ID         *ids.UUID       `json:"id,omitempty"`
	Parent     *ids.UUID       `json:"parent,omitempty"`
	Path       string          `json:"path,omitempty"`
	Depth      int             `json:"depth,omitempty"`
	SortKey    string          `json:"sortKey,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Item       *ids.UUID       `json:"item,omitempty"`
	Hidden     bool            `json:"hidden,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	MainLocale string          `json:"mainLocale,omitempty"`
	Collection string          `json:"collection,omitempty"`
	LinkItem   *ids.UUID       `json:"linkItem,omitempty"`
	LinkNode   *ids.UUID       `json:"linkNode,omitempty"`
	Created    *time.Time      `json:"createdAt,omitempty"`

	// node, slug, mount, route
	Site   string `json:"site,omitempty"`
	Locale string `json:"locale,omitempty"`

	// slug
	Node   *ids.UUID `json:"node,omitempty"`
	Slug   string    `json:"slug,omitempty"`
	Name   string    `json:"name,omitempty"`
	Vanity string    `json:"vanity,omitempty"`

	// mount (Node, Item, Locale)
	Rel string `json:"rel,omitempty"`

	// route (Site, Locale, Path; Item and Node as above)
	State  string  `json:"state,omitempty"`
	Target *string `json:"target,omitempty"`
	Status int     `json:"status,omitempty"`

	Updated *time.Time `json:"updatedAt,omitempty"`
}

func (exporter) Export(ctx context.Context, tx store.Tx, w *exportfmt.ModuleWriter) error {
	q := tx.SQL()
	if err := each(ctx, q, `SELECT id, site, parent_id, path, depth, sort_key, kind, item_id, hidden, data, main_locale,
			collection, link_item, link_node, created_at, updated_at FROM page_node ORDER BY depth, id`, nil, func(r store.Rows) error {
		var n pageRecord
		var data *json.RawMessage
		var id ids.UUID
		var created, updated time.Time
		if err := r.Scan(&id, &n.Site, &n.Parent, &n.Path, &n.Depth, &n.SortKey, &n.Kind, &n.Item, &n.Hidden, &data,
			&n.MainLocale, &n.Collection, &n.LinkItem, &n.LinkNode, &created, &updated); err != nil {
			return err
		}
		if data != nil {
			n.Data = bytesOf(*data)
		}
		n.T, n.ID, n.Created, n.Updated = "node", &id, &created, &updated
		return w.Write(n)
	}); err != nil {
		return fmt.Errorf("pages: export nodes: %w", err)
	}
	if err := each(ctx, q, `SELECT node_id, locale, slug, name, vanity FROM page_slug ORDER BY node_id, locale`, nil, func(r store.Rows) error {
		s := pageRecord{T: "slug"}
		var node ids.UUID
		if err := r.Scan(&node, &s.Locale, &s.Slug, &s.Name, &s.Vanity); err != nil {
			return err
		}
		s.Node = &node
		return w.Write(s)
	}); err != nil {
		return fmt.Errorf("pages: export slugs: %w", err)
	}
	if err := each(ctx, q, `SELECT node_id, item_id, locale, rel FROM page_mount_item ORDER BY node_id, item_id, locale`, nil, func(r store.Rows) error {
		m := pageRecord{T: "mount"}
		var node, item ids.UUID
		if err := r.Scan(&node, &item, &m.Locale, &m.Rel); err != nil {
			return err
		}
		m.Node, m.Item = &node, &item
		return w.Write(m)
	}); err != nil {
		return fmt.Errorf("pages: export mount items: %w", err)
	}
	if err := each(ctx, q, `SELECT site, locale, path, state, item_id, node_id, target, status, updated_at FROM route
			ORDER BY site, locale, path`, nil, func(r store.Rows) error {
		rt := pageRecord{T: "route"}
		var updated time.Time
		if err := r.Scan(&rt.Site, &rt.Locale, &rt.Path, &rt.State, &rt.Item, &rt.Node, &rt.Target, &rt.Status, &updated); err != nil {
			return err
		}
		rt.Updated = &updated
		return w.Write(rt)
	}); err != nil {
		return fmt.Errorf("pages: export routes: %w", err)
	}
	return nil
}

func bytesOf(m json.RawMessage) json.RawMessage { return append(json.RawMessage(nil), m...) }

func (exporter) Import(ctx context.Context, tx store.Tx, r *exportfmt.ModuleReader) error {
	q := tx.SQL()
	var n int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM page_node`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("pages: the page tree is not empty (%d nodes); import needs an empty one", n)
	}
	for {
		var rec pageRecord
		ok, err := r.Next(&rec)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := insertRecord(ctx, q, rec); err != nil {
			return fmt.Errorf("pages: import %s: %w", rec.T, err)
		}
	}
}

func insertRecord(ctx context.Context, q store.SQL, rec pageRecord) error {
	var err error
	switch rec.T {
	case "node":
		if rec.ID == nil || rec.Created == nil || rec.Updated == nil {
			return fmt.Errorf("node without id or times")
		}
		_, err = q.Exec(ctx, `INSERT INTO page_node (id, site, parent_id, path, depth, sort_key, kind, item_id, hidden, data,
				main_locale, collection, link_item, link_node, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, *rec.ID, rec.Site, rec.Parent, rec.Path, rec.Depth,
			rec.SortKey, rec.Kind, rec.Item, rec.Hidden, rec.Data, rec.MainLocale, rec.Collection, rec.LinkItem, rec.LinkNode,
			*rec.Created, *rec.Updated)
	case "slug":
		if rec.Node == nil {
			return fmt.Errorf("slug without node")
		}
		_, err = q.Exec(ctx, `INSERT INTO page_slug (node_id, locale, slug, name, vanity) VALUES (?, ?, ?, ?, ?)`,
			*rec.Node, rec.Locale, rec.Slug, rec.Name, rec.Vanity)
	case "mount":
		if rec.Node == nil || rec.Item == nil {
			return fmt.Errorf("mount item without node or item")
		}
		_, err = q.Exec(ctx, `INSERT INTO page_mount_item (node_id, item_id, locale, rel) VALUES (?, ?, ?, ?)`,
			*rec.Node, *rec.Item, rec.Locale, rec.Rel)
	case "route":
		if rec.Updated == nil {
			return fmt.Errorf("route without time")
		}
		_, err = q.Exec(ctx, `INSERT INTO route (site, locale, path, state, item_id, node_id, target, status, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, rec.Site, rec.Locale, rec.Path, rec.State, rec.Item, rec.Node, rec.Target,
			rec.Status, *rec.Updated)
	default:
		return fmt.Errorf("unknown record type %q", rec.T)
	}
	return err
}
