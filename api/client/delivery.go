package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Delivery reads the delivery API (/api/deliver/v1). It is safe for concurrent use.
type Delivery struct{ c core }

// NewDelivery returns a client for the server at baseURL (e.g. "https://cms.acme.com"). Without WithChannel
// or WithToken the server uses its only channel, or answers 400 when there are several.
func NewDelivery(baseURL string, opts ...Option) *Delivery {
	return &Delivery{c: newCore(baseURL, deliverPrefix, opts)}
}

// With returns a copy with more options applied, e.g. d.With(client.WithPreview(tok)).
func (d *Delivery) With(opts ...Option) *Delivery {
	c := d.c
	c.s.header = c.s.header.Clone()
	for _, o := range opts {
		o(&c.s)
	}
	return &Delivery{c: c}
}

// ItemResult is the answer to Item, ItemByKey and Singleton.
type ItemResult struct {
	Meta
	Item     json.RawMessage            // the item payload; see DecodeItem
	Included map[string]json.RawMessage // referenced items by id when Include was asked
}

// ListResult is the answer to List.
type ListResult struct {
	Meta
	Items   []json.RawMessage
	HasMore bool // more items follow this page
	// Total is the number of all matches, or -1 when the answer has none: the delivery API counts only when
	// ListOptions.Total is set.
	Total    int
	Included map[string]json.RawMessage
	// Facets holds the value counts of ListOptions.Facets, most items first.
	Facets map[string][]FacetValue
}

// FacetValue is one value of a facet and the number of matching items that have it.
type FacetValue struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// RouteResult is the answer to Route (pages module).
type RouteResult struct {
	Meta
	Located Located
	Item    json.RawMessage // the located item's payload, when the URL leads to one
}

// Located says where a frontend URL leads.
type Located struct {
	Channel    string      `json:"channel"`
	Kind       string      `json:"kind"` // page, item, redirect, not_found, reserved
	Locale     string      `json:"locale"`
	Canonical  string      `json:"canonical"`
	Alternates []Alternate `json:"alternates"`
	Resolution *Resolution `json:"resolution"`
}

// Alternate is the same content in another channel or language (hreflang).
type Alternate struct {
	Channel string `json:"channel"`
	Locale  string `json:"locale"`
	URL     string `json:"url"`
}

// Resolution is the outcome of resolving a path.
type Resolution struct {
	Kind     string `json:"kind"`
	Item     string `json:"item"`
	Node     string `json:"node"`
	Path     string `json:"path"`
	Status   int    `json:"status"` // 200, 301/302/307/308 or 404
	Location string `json:"location"`
}

// TreeOptions selects navigation.
type TreeOptions struct {
	Site   string // default: the channel's site
	Locale string
	Depth  int // 0: the API's default
	// Fields adds the listed fields of each page's item to the nodes (TreeNode.Fields).
	Fields      []string
	IfNoneMatch string
}

// TreeResult is the navigation of a site.
type TreeResult struct {
	Meta   `json:"-"`
	Site   string     `json:"site"`
	Locale string     `json:"locale"`
	Nodes  []TreeNode `json:"nodes"`
}

// TreeNode is one navigation entry.
type TreeNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // page, mount, link
	Item string `json:"item"`
	Type string `json:"type"`
	Name string `json:"name"`
	Path string `json:"path"`
	URL  string `json:"url"`
	// Fields holds the selected fields of the page's item when TreeOptions.Fields was set.
	Fields map[string]json.RawMessage `json:"fields"`
	Nodes  []TreeNode                 `json:"nodes"`
}

// ChannelResult is the public description of a channel.
type ChannelResult struct {
	Meta        `json:"-"`
	Name        string                     `json:"name"`
	Languages   []string                   `json:"languages"`
	Collections []string                   `json:"collections"`
	Options     map[string]json.RawMessage `json:"options"`
}

func ifNoneMatch(etag string) http.Header {
	if etag == "" {
		return nil
	}
	return http.Header{"If-None-Match": {etag}}
}

func (d *Delivery) base() *query {
	q := &query{}
	if d.c.s.channel != "" {
		q.add("channel", d.c.s.channel)
	}
	return q
}

func (d *Delivery) get(ctx context.Context, path string, q *query, etag string, out any) (Meta, error) {
	resp, raw, err := d.c.call(ctx, http.MethodGet, path, q.String(), nil, ifNoneMatch(etag), etag != "")
	if err != nil {
		return Meta{}, err
	}
	m := metaOf(resp)
	if m.NotModified() {
		return m, nil
	}
	return m, decode(raw, out)
}

func (d *Delivery) one(ctx context.Context, path string, o *GetOptions) (*ItemResult, error) {
	q := d.base()
	if err := o.encode(q); err != nil {
		return nil, err
	}
	var body struct {
		Item     json.RawMessage            `json:"item"`
		Included map[string]json.RawMessage `json:"included"`
	}
	etag := ""
	if o != nil {
		etag = o.IfNoneMatch
	}
	m, err := d.get(ctx, path, q, etag, &body)
	if err != nil {
		return nil, err
	}
	return &ItemResult{Meta: m, Item: body.Item, Included: body.Included}, nil
}

// Item reads an item by id. A nil o means defaults.
func (d *Delivery) Item(ctx context.Context, id string, o *GetOptions) (*ItemResult, error) {
	return d.one(ctx, "/items/"+url.PathEscape(id), o)
}

// ItemByKey reads an item by its key.
func (d *Delivery) ItemByKey(ctx context.Context, key string, o *GetOptions) (*ItemResult, error) {
	return d.one(ctx, "/items/by-key/"+url.PathEscape(key), o)
}

// Singleton reads the one item of a singleton type.
func (d *Delivery) Singleton(ctx context.Context, typ string, o *GetOptions) (*ItemResult, error) {
	return d.one(ctx, "/singletons/"+url.PathEscape(typ), o)
}

// List lists the items of a collection type. A nil o means the API's defaults.
func (d *Delivery) List(ctx context.Context, typ string, o *ListOptions) (*ListResult, error) {
	q := d.base()
	if err := o.encode(q); err != nil {
		return nil, err
	}
	var body struct {
		Items    []json.RawMessage          `json:"items"`
		HasMore  bool                       `json:"hasMore"`
		Total    *int                       `json:"total"`
		Included map[string]json.RawMessage `json:"included"`
		Facets   map[string][]FacetValue    `json:"facets"`
	}
	etag := ""
	if o != nil {
		etag = o.IfNoneMatch
	}
	m, err := d.get(ctx, "/collections/"+url.PathEscape(typ)+"/items", q, etag, &body)
	if err != nil {
		return nil, err
	}
	res := &ListResult{Meta: m, Items: body.Items, HasMore: body.HasMore, Total: -1, Included: body.Included, Facets: body.Facets}
	if body.Total != nil {
		res.Total = *body.Total
	}
	return res, nil
}

// Route asks where a frontend URL (absolute, as a visitor sees it) leads. Needs the pages module. Only o's
// Locale and IfNoneMatch apply.
func (d *Delivery) Route(ctx context.Context, rawURL string, o *GetOptions) (*RouteResult, error) {
	q := d.base()
	if o != nil && o.Locale != "" {
		q.add("locale", o.Locale)
	}
	q.add("url", rawURL)
	var body struct {
		Located Located         `json:"located"`
		Item    json.RawMessage `json:"item"`
	}
	etag := ""
	if o != nil {
		etag = o.IfNoneMatch
	}
	m, err := d.get(ctx, "/route", q, etag, &body)
	if err != nil {
		return nil, err
	}
	return &RouteResult{Meta: m, Located: body.Located, Item: body.Item}, nil
}

// Tree reads the navigation of a site. A nil o means the channel's site, default depth.
func (d *Delivery) Tree(ctx context.Context, o *TreeOptions) (*TreeResult, error) {
	q := d.base()
	etag := ""
	if o != nil {
		if o.Site != "" {
			q.add("site", o.Site)
		}
		if o.Locale != "" {
			q.add("locale", o.Locale)
		}
		if o.Depth > 0 {
			q.add("depth", strconv.Itoa(o.Depth))
		}
		if err := addFields(q, o.Fields); err != nil {
			return nil, err
		}
		etag = o.IfNoneMatch
	}
	var body TreeResult
	m, err := d.get(ctx, "/tree", q, etag, &body)
	if err != nil {
		return nil, err
	}
	body.Meta = m
	return &body, nil
}

// Channel reads a channel's public description. An empty name means the configured channel.
func (d *Delivery) Channel(ctx context.Context, name string) (*ChannelResult, error) {
	if name == "" {
		name = d.c.s.channel
	}
	if name == "" {
		return nil, fmt.Errorf("hydrogen: Channel needs a name or WithChannel")
	}
	var body ChannelResult
	m, err := d.get(ctx, "/channels/"+url.PathEscape(name), &query{}, "", &body)
	if err != nil {
		return nil, err
	}
	body.Meta = m
	return &body, nil
}

// Item is a delivery item with its fields decoded into T, typically a struct from gen/content.
type Item[T any] struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Key         string            `json:"key"`
	Locale      string            `json:"locale"`
	Version     int               `json:"version"`
	PublishedAt time.Time         `json:"publishedAt"`
	Name        string            `json:"name"`
	Fields      T                 `json:"fields"`
	Fallback    bool              `json:"fallback"` // shown in another locale than the one asked for
	URL         string            `json:"url"`      // pages module: the item's URL in the channel
	Links       map[string]string `json:"links"`    // pages module: URLs of referenced items by id
}

// DecodeItem decodes an item payload (ItemResult.Item, an element of ListResult.Items, an Included value).
func DecodeItem[T any](raw json.RawMessage) (Item[T], error) {
	var it Item[T]
	if len(raw) == 0 {
		return it, fmt.Errorf("hydrogen: no item to decode")
	}
	if err := json.Unmarshal(raw, &it); err != nil {
		return it, fmt.Errorf("hydrogen: decode item: %w", err)
	}
	return it, nil
}

// DecodeItems decodes a list of item payloads.
func DecodeItems[T any](raws []json.RawMessage) ([]Item[T], error) {
	out := make([]Item[T], len(raws))
	for i, r := range raws {
		var err error
		if out[i], err = DecodeItem[T](r); err != nil {
			return nil, err
		}
	}
	return out, nil
}
