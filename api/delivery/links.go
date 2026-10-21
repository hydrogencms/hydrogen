package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"github.com/hydrogencms/hydrogen/core/channel"
	"github.com/hydrogencms/hydrogen/core/ids"
)

// maxPatternRefs bounds the referenced items one response looks up to build pattern links.
const maxPatternRefs = 200

// patternURL is the URL of a path made from a channel pattern, relative to the channel and absolute.
func (a *API) patternURL(ch *channel.Channel, path string) (rel, abs string) {
	for _, p := range a.o.PatternURLs {
		if rel, abs, ok := p(ch, path); ok {
			return rel, abs
		}
	}
	return path, ch.Frontend + path
}

// fieldReader reads scalar fields of a payload for pattern placeholders; a dotted name walks into groups.
func fieldReader(payload []byte) func(string) (string, bool) {
	var p struct {
		Fields map[string]any `json:"fields"`
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if dec.Decode(&p) != nil {
		return func(string) (string, bool) { return "", false }
	}
	return func(name string) (string, bool) {
		var cur any = p.Fields
		for _, part := range strings.Split(name, ".") {
			m, ok := cur.(map[string]any)
			if !ok {
				return "", false
			}
			cur = m[part]
		}
		return plainText(cur)
	}
}

// patternLinks fills what no module routed from the channel's link patterns: an item's own url and the links of
// the items it references. The referenced items are looked up in the item's locale (their drafts for a draft),
// so the response carries their cache tags and a changed slug refreshes the pages that link to them.
func (st *state) patternLinks(ctx context.Context, all []*resolved, items []*Item) error {
	ch := st.m.ch
	if len(ch.Links) == 0 {
		return nil
	}
	loaded := 0
	for i, r := range all {
		it := items[i]
		if _, has := it.Extra["url"]; !has {
			if path, ok := ch.LinkPath(r.typ, r.id.String(), r.locale, fieldReader(r.source())); ok {
				_, abs := st.a.patternURL(ch, path)
				st.extra(it)["url"] = abs
			}
		}
		links, _ := it.Extra["links"].(map[string]string)
		var want []ids.UUID
		for _, ref := range r.refs {
			if _, done := links[ref.String()]; !done && loaded+len(want) < maxPatternRefs {
				want = append(want, ref)
			}
		}
		if len(want) == 0 {
			continue
		}
		loaded += len(want)
		got, err := st.loadFor(ctx, r, want)
		if err != nil {
			return err
		}
		merged := map[string]string{}
		for k, v := range links {
			merged[k] = v
		}
		for _, x := range got {
			if path, ok := ch.LinkPath(x.typ, x.id.String(), x.locale, fieldReader(x.source())); ok {
				rel, _ := st.a.patternURL(ch, path)
				merged[x.id.String()] = rel
			}
		}
		if len(merged) > 0 {
			st.extra(it)["links"] = merged
		}
	}
	return nil
}

func (st *state) extra(it *Item) map[string]any {
	if it.Extra == nil {
		it.Extra = map[string]any{}
	}
	return it.Extra
}
