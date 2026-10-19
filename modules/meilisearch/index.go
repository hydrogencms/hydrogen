package meilisearch

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/search"
	"github.com/hydrogencms/hydrogen/core/store"
)

const (
	writeBatch   = 500
	deleteBatch  = 200
	cropWords    = 20
	maxTotalHits = 10000
	maxFacetVals = 100
	metaDocID    = "settings"
	markerID     = "rebuild_marker"
)

// Index is the Meilisearch engine: a search.Index over one index of a Meilisearch server.
type Index struct {
	cfg Config
	c   *client

	// wmu serialises Upsert and Delete, so one's stale-document cleanup cannot race another's writes.
	wmu sync.Mutex

	mu     sync.RWMutex
	fields map[string]search.FieldSetting // nil until Sync or Rebuild
}

var _ search.Index = (*Index)(nil)

// New returns the engine of a validated config. hc may be nil.
func New(cfg Config, hc *http.Client) *Index {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Index{cfg: cfg, c: &client{base: cfg.base(), key: cfg.Key, hc: hc, timeout: cfg.Timeout, taskTimeout: cfg.TaskTimeout}}
}

func (ix *Index) Name() string { return "meilisearch" }

func (ix *Index) metaUID() string { return ix.cfg.Index + "_meta" }
func (ix *Index) nextUID() string { return ix.cfg.Index + "_next" }

func (ix *Index) setFields(s search.Settings) {
	m := make(map[string]search.FieldSetting, len(s.Fields))
	for _, f := range s.Fields {
		m[f.Name] = f
	}
	ix.mu.Lock()
	ix.fields = m
	ix.mu.Unlock()
}

// settingsBody is the Meilisearch settings of an index for the search settings.
func settingsBody(s search.Settings) map[string]any {
	filterable := []string{"id", attrItem, attrType, attrLocale, attrMain, attrAlways, attrLocales, attrPublishedAt}
	sortable := []string{attrPublishedAt}
	for _, f := range s.Fields {
		filterable = append(filterable, fieldPrefix+f.Name)
		if f.Sortable && !f.List {
			sortable = append(sortable, fieldPrefix+f.Name)
		}
	}
	return map[string]any{
		"searchableAttributes": []string{attrName, attrText},
		"filterableAttributes": filterable,
		"sortableAttributes":   sortable,
		// A requested sort decides the order; without one the sort rule does nothing.
		"rankingRules": []string{"sort", "words", "typo", "proximity", "attribute", "exactness"},
		"pagination":   map[string]any{"maxTotalHits": maxTotalHits},
		"faceting":     map[string]any{"maxValuesPerFacet": maxFacetVals, "sortFacetValuesBy": map[string]string{"*": "count"}},
	}
}

// ensure creates the index when it does not exist and reports whether it did. It creates first and tolerates
// "already exists", so the key needs no permission to read indexes.
func (ix *Index) ensure(ctx context.Context, uid string) (created bool, err error) {
	err = ix.c.run(ctx, http.MethodPost, "/indexes", map[string]string{"uid": uid, "primaryKey": "id"})
	if isCode(err, "index_already_exists") {
		return false, nil
	}
	return err == nil, err
}

func (ix *Index) applySettings(ctx context.Context, uid string, s search.Settings) error {
	return ix.c.run(ctx, http.MethodPatch, idxPath(uid)+"/settings", settingsBody(s))
}

// storedFingerprint returns the fingerprint of the last completed rebuild, "" when there is none.
func (ix *Index) storedFingerprint(ctx context.Context) (string, error) {
	var doc struct {
		Fingerprint string `json:"fingerprint"`
	}
	err := ix.c.do(ctx, http.MethodGet, idxPath(ix.metaUID())+"/documents/"+metaDocID, nil, &doc)
	if isCode(err, "index_not_found") || isCode(err, "document_not_found") {
		return "", nil
	}
	return doc.Fingerprint, err
}

func (ix *Index) storeFingerprint(ctx context.Context, fp string) error {
	if _, err := ix.ensure(ctx, ix.metaUID()); err != nil {
		return err
	}
	return ix.c.run(ctx, http.MethodPost, idxPath(ix.metaUID())+"/documents",
		[]map[string]string{{"id": metaDocID, "fingerprint": fp}})
}

// Sync creates the index when needed, applies the settings, and says whether the documents must be rebuilt:
// the index is new, or the last completed rebuild used another fingerprint.
func (ix *Index) Sync(ctx context.Context, s search.Settings) (bool, error) {
	created, err := ix.ensure(ctx, ix.cfg.Index)
	if err != nil {
		return false, err
	}
	if err := ix.applySettings(ctx, ix.cfg.Index, s); err != nil {
		return false, err
	}
	ix.setFields(s)
	fp, err := ix.storedFingerprint(ctx)
	if err != nil {
		return false, err
	}
	return created || fp != s.Fingerprint, nil
}

// Upsert replaces all documents of the items present in docs: the documents are added, then documents of
// those items with other ids (a locale that is no longer published) are deleted, so the item is never missing.
func (ix *Index) Upsert(ctx context.Context, docs []search.Doc) error {
	ix.wmu.Lock()
	defer ix.wmu.Unlock()
	for batch := range slices.Chunk(docs, writeBatch) {
		if err := ix.add(ctx, ix.cfg.Index, batch); err != nil {
			return err
		}
	}
	var items []ids.UUID
	byItem := map[ids.UUID][]string{}
	for _, d := range docs {
		if _, ok := byItem[d.Item]; !ok {
			items = append(items, d.Item)
		}
		byItem[d.Item] = append(byItem[d.Item], docID(d.Item, d.Locale))
	}
	for batch := range slices.Chunk(items, deleteBatch) {
		var itemLits, idLits []string
		for _, it := range batch {
			itemLits = append(itemLits, quote(it.String()))
			for _, id := range byItem[it] {
				idLits = append(idLits, quote(id))
			}
		}
		filter := attrItem + " IN [" + strings.Join(itemLits, ", ") + "] AND NOT id IN [" + strings.Join(idLits, ", ") + "]"
		if err := ix.c.run(ctx, http.MethodPost, idxPath(ix.cfg.Index)+"/documents/delete", map[string]string{"filter": filter}); err != nil {
			return err
		}
	}
	return nil
}

func (ix *Index) add(ctx context.Context, uid string, docs []search.Doc) error {
	body := make([]map[string]any, len(docs))
	for i, d := range docs {
		m, err := document(d)
		if err != nil {
			return err
		}
		body[i] = m
	}
	return ix.c.run(ctx, http.MethodPost, idxPath(uid)+"/documents", body)
}

// Delete removes every locale of the items with one delete-by-filter task per batch (`item IN [...]`;
// needs Meilisearch 1.2 or later). While a Rebuild is filling <index>_next the items are removed from it too,
// so a purged item cannot come back with the swap. <index>_next goes first: a swap between the two deletes then
// makes the second one hit the new live index, where the other order would leave the item in it.
func (ix *Index) Delete(ctx context.Context, items []ids.UUID) error {
	ix.wmu.Lock()
	defer ix.wmu.Unlock()
	for batch := range slices.Chunk(items, deleteBatch) {
		lits := make([]string, len(batch))
		for i, id := range batch {
			lits[i] = quote(id.String())
		}
		body := map[string]string{"filter": attrItem + " IN [" + strings.Join(lits, ", ") + "]"}
		for _, uid := range []string{ix.nextUID(), ix.cfg.Index} {
			err := ix.c.run(ctx, http.MethodPost, idxPath(uid)+"/documents/delete", body)
			if err != nil && !isCode(err, "index_not_found") {
				return err
			}
		}
	}
	return nil
}

// Rebuild fills <index>_next from next, swaps it with the index and deletes the old one, so searches always
// see a complete index. The fingerprint is stored last: an interrupted rebuild runs again at the next Sync.
func (ix *Index) Rebuild(ctx context.Context, s search.Settings, next func(ctx context.Context) ([]search.Doc, error)) error {
	nextUID := ix.nextUID()
	if err := ix.dropIndex(ctx, nextUID); err != nil {
		return err
	}
	if _, err := ix.ensure(ctx, nextUID); err != nil {
		return err
	}
	if err := ix.applySettings(ctx, nextUID, s); err != nil {
		return err
	}
	for {
		done, err := ix.fillBatch(ctx, nextUID, next)
		if err != nil {
			return err
		}
		if done {
			break
		}
	}
	// A swap needs both indexes; the first rebuild has no live one yet.
	if _, err := ix.ensure(ctx, ix.cfg.Index); err != nil {
		return err
	}
	if err := ix.swap(ctx, nextUID, s); err != nil {
		return err
	}
	ix.setFields(s)
	if err := ix.dropIndex(ctx, nextUID); err != nil {
		return err
	}
	return ix.storeFingerprint(ctx, s.Fingerprint)
}

// swap exchanges the live index with next. A key limited to some indexes cannot read the swap task (Meilisearch
// attaches it to no index), so then the swap is confirmed by a marker document that was added to next: tasks run
// in order, so after a later task of the index has finished, the marker is in the live index if the swap applied.
func (ix *Index) swap(ctx context.Context, nextUID string, s search.Settings) error {
	marker := map[string]string{"id": markerID, "token": s.Fingerprint + "/" + fmt.Sprint(time.Now().UnixNano())}
	if err := ix.c.run(ctx, http.MethodPost, idxPath(nextUID)+"/documents", []map[string]string{marker}); err != nil {
		return err
	}
	swap := []map[string]any{{"indexes": []string{ix.cfg.Index, nextUID}}}
	err := ix.c.run(ctx, http.MethodPost, "/swap-indexes", swap)
	if !isCode(err, "task_not_found") {
		return err
	}
	if err := ix.applySettings(ctx, ix.cfg.Index, s); err != nil {
		return err
	}
	var got map[string]string
	if err := ix.c.do(ctx, http.MethodGet, idxPath(ix.cfg.Index)+"/documents/"+markerID, nil, &got); err != nil || got["token"] != marker["token"] {
		return fmt.Errorf("meilisearch: the swap of %s and %s did not apply", ix.cfg.Index, nextUID)
	}
	return nil
}

// fillBatch fetches and adds one batch under the write lock: a Delete runs either before the batch is fetched
// (the item is gone from the source) or after it is added (and removes it from <index>_next).
func (ix *Index) fillBatch(ctx context.Context, nextUID string, next func(ctx context.Context) ([]search.Doc, error)) (bool, error) {
	ix.wmu.Lock()
	defer ix.wmu.Unlock()
	docs, err := next(ctx)
	if err != nil || len(docs) == 0 {
		return true, err
	}
	for batch := range slices.Chunk(docs, writeBatch) {
		if err := ix.add(ctx, nextUID, batch); err != nil {
			return false, err
		}
	}
	return false, nil
}

func (ix *Index) dropIndex(ctx context.Context, uid string) error {
	err := ix.c.run(ctx, http.MethodDelete, idxPath(uid), nil)
	if err != nil && !isCode(err, "index_not_found") {
		return err
	}
	return nil
}

type searchHit struct {
	Item      string  `json:"item"`
	Type      string  `json:"type"`
	Locale    string  `json:"locale"`
	Score     float64 `json:"_rankingScore"`
	Formatted struct {
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"_formatted"`
}

type searchResponse struct {
	Hits               []searchHit               `json:"hits"`
	EstimatedTotalHits int                       `json:"estimatedTotalHits"`
	TotalHits          int                       `json:"totalHits"`
	FacetDistribution  map[string]map[string]int `json:"facetDistribution"`
}

// Search answers the request from the index.
func (ix *Index) Search(ctx context.Context, r search.Request) (search.Result, error) {
	if err := r.Validate(); err != nil {
		return search.Result{}, err
	}
	filter, err := ix.filter(r)
	if err != nil {
		return search.Result{}, err
	}
	sort, err := ix.sortExprs(r.Sort)
	if err != nil {
		return search.Result{}, err
	}
	body := map[string]any{
		"q":                    r.Text,
		"filter":               filter,
		"attributesToRetrieve": []string{attrItem, attrType, attrLocale},
		"showRankingScore":     true,
	}
	if len(sort) > 0 {
		body["sort"] = sort
	}
	paged := r.Offset%r.Limit == 0
	if paged {
		body["page"], body["hitsPerPage"] = r.Offset/r.Limit+1, r.Limit
	} else {
		body["offset"], body["limit"] = r.Offset, r.Limit
	}
	facetAttr := make(map[string]string, len(r.Facets))
	if len(r.Facets) > 0 {
		attrs := make([]string, len(r.Facets))
		for i, name := range r.Facets {
			t, err := ix.resolve(name, func() store.Column { return store.ColText })
			if err != nil {
				return search.Result{}, err
			}
			if t.attr == attrPublishedAt {
				return search.Result{}, invalid("cannot facet by %q", name)
			}
			attrs[i] = t.attr
			facetAttr[t.attr] = name
		}
		body["facets"] = attrs
	}
	if r.Highlight {
		body["attributesToHighlight"] = []string{attrName, attrText}
		body["attributesToCrop"] = []string{attrText}
		body["attributesToRetrieve"] = []string{attrItem, attrType, attrLocale, attrName, attrText}
		body["cropLength"] = cropWords
		body["highlightPreTag"], body["highlightPostTag"] = search.MarkOpen, search.MarkClose
	}
	var resp searchResponse
	if err := ix.c.do(ctx, http.MethodPost, idxPath(ix.cfg.Index)+"/search", body, &resp); err != nil {
		var ae *apiError
		if errors.As(err, &ae) && ae.Status == http.StatusBadRequest && strings.HasPrefix(ae.Code, "invalid_search_") {
			return search.Result{}, invalid("%s", ae.Message)
		}
		return search.Result{}, err
	}
	res := search.Result{Hits: make([]search.Hit, 0, len(resp.Hits))}
	for _, h := range resp.Hits {
		id, err := ids.Parse(h.Item)
		if err != nil {
			return search.Result{}, fmt.Errorf("meilisearch: hit with bad item id %q", h.Item)
		}
		hit := search.Hit{Item: id, Type: h.Type, Locale: h.Locale}
		if len(r.Sort) == 0 {
			hit.Score = h.Score
		}
		if r.Highlight {
			hit.Highlights = map[string]string{}
			if h.Formatted.Name != "" {
				hit.Highlights["name"] = search.Marked(h.Formatted.Name)
			}
			if h.Formatted.Text != "" {
				hit.Highlights["text"] = search.Marked(h.Formatted.Text)
			}
		}
		res.Hits = append(res.Hits, hit)
	}
	if paged {
		res.Total = resp.TotalHits
		res.Estimated = res.Total >= maxTotalHits
	} else {
		res.Total, res.Estimated = resp.EstimatedTotalHits, true
	}
	if len(r.Facets) > 0 {
		res.Facets = make(map[string][]search.Facet, len(r.Facets))
		for attr, name := range facetAttr {
			facets := []search.Facet{}
			for v, n := range resp.FacetDistribution[attr] {
				facets = append(facets, search.Facet{Value: v, Count: n})
			}
			slices.SortFunc(facets, func(a, b search.Facet) int {
				if a.Count != b.Count {
					return b.Count - a.Count
				}
				return strings.Compare(a.Value, b.Value)
			})
			res.Facets[name] = facets
		}
	}
	return res, nil
}
