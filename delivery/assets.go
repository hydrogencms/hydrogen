package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
)

// Asset fields hold the id of an upload item. A response shows each as the asset itself, built when the
// response is made (snapshots keep the id):
//
//	{"id", "url", "name", "mime", "size", "width"?, "height"?, "focal"?, "fields": {alt, caption, …}}
//
// fields are the asset item's own fields in the response's locale (the file, folder and tags are left out; name
// to focal come from the file). An asset that is not published, not available in the channel or not an upload
// item is null. The asset is looked up like any item, so the response carries its cache tag and publishing
// the asset purges every cached response that shows it; for a draft, the asset's draft is shown.

// DefaultAssetPath is where the media module serves files: /assets/{sha}/{name}.
const DefaultAssetPath = "/assets"

func isAssetField(f *schema.Field) bool {
	return f.Shape.Base == schema.BaseRef && f.Shape.Format == "asset"
}

// assetTypes lists the types whose documents can hold asset fields.
func (a *API) assetTypes() map[string]bool {
	out := map[string]bool{}
	for _, t := range a.o.Schema.Types() {
		if a.o.Schema.HasFields(t, isAssetField) {
			out[t.Name] = true
		}
	}
	return out
}

// assetURL is the public URL of a file.
func (a *API) assetURL(sha, name string) string {
	if a.o.AssetURL != nil {
		return a.o.AssetURL(sha, name)
	}
	base := strings.TrimRight(a.o.AssetBase, "/")
	if base == "" {
		base = DefaultAssetPath
	}
	return base + "/" + sha + "/" + url.PathEscape(name)
}

// assetKey identifies an asset as one response shows it: the same item looks different per locale and for a
// draft.
type assetKey struct {
	id     ids.UUID
	locale string
	draft  bool
}

// assetWork is one item whose asset fields are being expanded.
type assetWork struct {
	r      *resolved
	t      *schema.Type
	fields map[string]any
	want   []ids.UUID
}

// expandAssets replaces the asset ids in the fields of the items with the assets. The assets of all the items
// are loaded together, once per locale and kind (published or draft), and remembered for the rest of the
// request: a list of cards that share an image costs one load, not one per card.
func (st *state) expandAssets(ctx context.Context, list []*resolved) error {
	sc := st.a.o.Schema
	var work []*assetWork
	need := map[assetKey]bool{}
	for _, r := range list {
		r.assetsDone = true
		t, ok := sc.Type(r.typ)
		if !ok {
			continue
		}
		var top map[string]json.RawMessage
		if err := json.Unmarshal(r.payload, &top); err != nil {
			return fmt.Errorf("delivery: decode payload of %s: %w", r.id, err)
		}
		fields, err := decodeObject(top["fields"])
		if err != nil {
			return fmt.Errorf("delivery: decode fields of %s: %w", r.id, err)
		}
		w := &assetWork{r: r, t: t, fields: fields}
		seen := map[string]bool{}
		sc.MapValues(t, fields, isAssetField, func(_ *schema.Field, _ string, v any) (any, bool) {
			if s, ok := v.(string); ok && !seen[s] {
				if id, err := ids.Parse(s); err == nil {
					seen[s] = true
					w.want = append(w.want, id)
					if k := (assetKey{id, r.locale, r.draft}); st.assets[k] == nil {
						need[k] = true
					}
				}
			}
			return v, false
		})
		if len(w.want) > 0 {
			work = append(work, w)
		}
	}
	if len(work) == 0 {
		return nil
	}
	if err := st.loadAssets(ctx, work, need); err != nil {
		return err
	}
	for _, w := range work {
		r := w.r
		byID := make(map[string]any, len(w.want))
		for _, id := range w.want {
			if a := st.assets[assetKey{id, r.locale, r.draft}]; a != nil && a.asset != nil {
				byID[id.String()] = a.asset
			}
		}
		sc.MapValues(w.t, w.fields, isAssetField, func(_ *schema.Field, _ string, v any) (any, bool) {
			s, _ := v.(string)
			if a, ok := byID[s]; ok {
				return a, true
			}
			return nil, true
		})
		raw, err := marshal(w.fields)
		if err != nil {
			return fmt.Errorf("delivery: encode fields of %s: %w", r.id, err)
		}
		payload, err := replaceKeys(r.payload, "fields", nil, map[string]json.RawMessage{"fields": raw})
		if err != nil {
			return err
		}
		r.payload, r.etag = payload, ""
	}
	return nil
}

// builtAsset is a remembered asset; asset is nil when the item is not available as one.
type builtAsset struct{ asset map[string]any }

// loadAssets loads the assets in need, one load per locale and kind, and remembers them.
func (st *state) loadAssets(ctx context.Context, work []*assetWork, need map[assetKey]bool) error {
	type group struct {
		rep  *resolved
		want []ids.UUID
	}
	groups := map[assetKey]*group{} // keyed with a zero id
	var order []assetKey
	for _, w := range work {
		for _, id := range w.want {
			k := assetKey{id, w.r.locale, w.r.draft}
			if !need[k] {
				continue
			}
			delete(need, k)
			gk := assetKey{locale: k.locale, draft: k.draft}
			g := groups[gk]
			if g == nil {
				g = &group{rep: w.r}
				groups[gk] = g
				order = append(order, gk)
			}
			g.want = append(g.want, id)
		}
	}
	if st.assets == nil {
		st.assets = map[assetKey]*builtAsset{}
	}
	for _, gk := range order {
		g := groups[gk]
		st.expanding = true
		got, err := st.loadFor(ctx, g.rep, g.want)
		st.expanding = false
		if err != nil {
			return err
		}
		for _, id := range g.want {
			st.assets[assetKey{id, gk.locale, gk.draft}] = &builtAsset{}
		}
		for _, x := range got {
			a, err := st.buildAsset(x)
			if err != nil {
				return err
			}
			st.assets[assetKey{x.id, gk.locale, gk.draft}] = &builtAsset{asset: a}
		}
	}
	return nil
}

func decodeObject(raw json.RawMessage) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// buildAsset shows an upload item as an asset; nil when x is not an upload item with a file.
func (st *state) buildAsset(x *resolved) (map[string]any, error) {
	t, ok := st.a.o.Schema.Type(x.typ)
	if !ok || t.Upload == nil {
		return nil, nil
	}
	var p struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(x.payload, &p); err != nil {
		return nil, fmt.Errorf("delivery: decode payload of %s: %w", x.id, err)
	}
	fields, err := decodeObject(p.Fields)
	if err != nil {
		return nil, fmt.Errorf("delivery: decode fields of %s: %w", x.id, err)
	}
	file, _ := fields[schema.UploadFile].(map[string]any)
	sha, _ := file["id"].(string)
	name, _ := file["name"].(string)
	if sha == "" {
		return nil, nil
	}
	out := map[string]any{"id": x.id.String(), "url": st.a.assetURL(sha, name), "name": name,
		"mime": file["mime"], "size": file["size"]}
	for _, k := range []string{"width", "height", "focal"} {
		if v, ok := file[k]; ok {
			out[k] = v
		}
	}
	for _, k := range []string{schema.UploadFile, schema.UploadFolder, schema.UploadTags} {
		delete(fields, k)
	}
	out["fields"] = fields
	return out, nil
}
