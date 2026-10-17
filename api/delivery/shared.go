package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hydrogencms/hydrogen/core/ids"
)

// Shared blocks are inlined when a response is built, not stored in snapshots: a snapshot keeps the reference
// ({"type":"shared","props":{"item":id}}) and the response adds "blocks" with the shared item's blocks as the
// channel shows it in the page's locale. The shared item is looked up like any other item, so the response
// carries its cache tag and publishing it purges every cached page that uses it; for a draft page the shared
// item's draft is inlined. Responses are cached, so the work is paid on a cache miss only.

// sharedFields maps a type to the names of its blocks fields that can hold shared blocks.
func (a *API) sharedFields() map[string][]string {
	out := map[string][]string{}
	for _, t := range a.o.Schema.Types() {
		for _, f := range t.Fields {
			if a.o.Schema.MaySharedBlocks(f) {
				out[t.Name] = append(out[t.Name], f.Name)
			}
		}
	}
	return out
}

// block is a stored block with the key delivery adds for shared blocks, in the key order of a stored block.
type block struct {
	Blocks json.RawMessage     `json:"blocks,omitempty"`
	ID     string              `json:"id"`
	Props  json.RawMessage     `json:"props,omitempty"`
	Slots  map[string][]*block `json:"slots,omitempty"`
	Type   string              `json:"type"`
}

func (b *block) walk(fn func(*block)) {
	fn(b)
	for _, name := range sortedKeys(b.Slots) {
		for _, c := range b.Slots[name] {
			c.walk(fn)
		}
	}
}

func (b *block) item() (ids.UUID, bool) {
	var p struct {
		Item string `json:"item"`
	}
	if json.Unmarshal(b.Props, &p) != nil {
		return ids.UUID{}, false
	}
	id, err := ids.Parse(p.Item)
	return id, err == nil
}

// inline adds the blocks of shared blocks and the assets of asset fields to the resolved items. Items that were
// inlined already are skipped.
func (st *state) inline(ctx context.Context, list []*resolved) error {
	var assets []*resolved
	for _, r := range list {
		if !r.inlined {
			r.inlined = true
			if names := st.a.shared[r.typ]; len(names) > 0 {
				if err := st.inlineItem(ctx, r, names); err != nil {
					return err
				}
			}
		}
		if st.a.assets[r.typ] && !r.assetsDone && !st.expanding {
			assets = append(assets, r)
		}
	}
	if len(assets) == 0 {
		return nil
	}
	return st.expandAssets(ctx, assets)
}

func (st *state) inlineItem(ctx context.Context, r *resolved, names []string) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(r.payload, &top); err != nil {
		return fmt.Errorf("delivery: decode payload of %s: %w", r.id, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(top["fields"], &fields); err != nil {
		return fmt.Errorf("delivery: decode fields of %s: %w", r.id, err)
	}
	trees := map[string][]*block{}
	var want []ids.UUID
	for _, name := range names {
		raw, ok := fields[name]
		if !ok {
			continue
		}
		var tree []*block
		if err := json.Unmarshal(raw, &tree); err != nil {
			return fmt.Errorf("delivery: decode blocks %s of %s: %w", name, r.id, err)
		}
		for _, b := range tree {
			b.walk(func(b *block) {
				if id, ok := b.item(); ok && b.Type == "shared" && !slices.Contains(want, id) {
					want = append(want, id)
				}
			})
		}
		trees[name] = tree
	}
	if len(want) == 0 {
		return nil
	}
	inlined, more, err := st.sharedBlocks(ctx, r, want)
	if err != nil {
		return err
	}
	patched := map[string]json.RawMessage{}
	for name, tree := range trees {
		for _, b := range tree {
			b.walk(func(b *block) {
				if id, ok := b.item(); ok && b.Type == "shared" {
					b.Blocks = inlined[id]
				}
			})
		}
		raw, err := marshal(tree)
		if err != nil {
			return fmt.Errorf("delivery: encode blocks %s of %s: %w", name, r.id, err)
		}
		patched[name] = raw
	}
	payload, err := replaceKeys(r.payload, "fields", func(raw []byte) ([]byte, error) { return replaceKeys(raw, "", nil, patched) }, nil)
	if err != nil {
		return err
	}
	// The page now shows the shared content, so the items it links to count as the page's references.
	r.payload, r.etag = payload, ""
	r.refs = slices.Compact(slices.SortedFunc(slices.Values(slices.Concat(r.refs, more)), cmpUUID))
	return nil
}

// sharedBlocks loads the shared items and returns their blocks by item id (an unavailable item has no blocks)
// and the items they reference.
func (st *state) sharedBlocks(ctx context.Context, r *resolved, want []ids.UUID) (map[ids.UUID]json.RawMessage, []ids.UUID, error) {
	got, err := st.loadFor(ctx, r, want)
	if err != nil {
		return nil, nil, err
	}
	out := make(map[ids.UUID]json.RawMessage, len(want))
	for _, id := range want {
		out[id] = json.RawMessage("[]")
	}
	var more []ids.UUID
	for _, x := range got {
		more = append(more, x.refs...)
		t, ok := st.a.o.Schema.Type(x.typ)
		if !ok {
			continue
		}
		f, ok := t.BlocksField()
		if !ok || !t.Blockset {
			continue
		}
		var p struct {
			Fields map[string]json.RawMessage `json:"fields"`
		}
		if err := json.Unmarshal(x.payload, &p); err != nil {
			return nil, nil, fmt.Errorf("delivery: decode payload of %s: %w", x.id, err)
		}
		if raw, ok := p.Fields[f.Name]; ok {
			out[x.id] = raw
		}
	}
	return out, more, nil
}

// replaceKeys rewrites the members of a JSON object in place, keeping their order: the value of key goes
// through fn, and the members of set replace those of the same name.
func replaceKeys(obj []byte, key string, fn func([]byte) ([]byte, error), set map[string]json.RawMessage) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(obj))
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("delivery: rewrite payload: %w", err)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	out.WriteByte('{')
	for first := true; dec.More(); first = false {
		kt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("delivery: rewrite payload: %w", err)
		}
		k, _ := kt.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("delivery: rewrite payload: %w", err)
		}
		switch nv, replaced := set[k]; {
		case replaced:
			v = nv
		case fn != nil && k == key:
			if v, err = fn(v); err != nil {
				return nil, err
			}
		}
		if !first {
			out.WriteByte(',')
		}
		if err := enc.Encode(k); err != nil {
			return nil, err
		}
		out.Truncate(out.Len() - 1)
		out.WriteByte(':')
		out.Write(v)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// loadFor loads items an inlined part of r needs, in r's locale (the channel's other languages follow as
// fallbacks): from their drafts when r is a draft, else as the channel shows them. Every item is looked up, so
// the response is tagged with it.
func (st *state) loadFor(ctx context.Context, r *resolved, want []ids.UUID) ([]*resolved, error) {
	old := st.eff
	defer func() { st.eff = old }()
	st.eff = narrowTo(st.m.ch, r.locale)
	if !r.draft {
		return st.load(ctx, want)
	}
	var got []*resolved
	for _, id := range want {
		st.look(id, "")
		x, err := st.draft(ctx, id)
		if err != nil {
			return nil, err
		}
		if x != nil {
			st.look(id, x.typ)
			got = append(got, x)
		}
	}
	return got, nil
}
