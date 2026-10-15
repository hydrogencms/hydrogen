// Package cachetag names the cache tags shared by the delivery response cache, CDN purges and webhooks, and
// maps outbox events to the tags they invalidate.
package cachetag

import (
	"encoding/json"
	"slices"
	"strings"
)

// All purges everything.
const All = "*"

// PurgeChannel is the notifier channel every node listens on for purges. The payload is space-separated tags.
const PurgeChannel = "hydrogen_purge"

// MaxPayload is the longest purge payload sent in one notification (Postgres limits payloads to 8000 bytes).
const MaxPayload = 7000

// Item tags one item, and every response that includes or links it.
func Item(id string) string { return "i:" + id }

// Collection tags the listings of a collection type.
func Collection(t string) string { return "c:" + t }

// Singleton tags a singleton type's item.
func Singleton(t string) string { return "s:" + t }

// Routes tags the routes of a site.
func Routes(site string) string { return "r:" + site }

// Nav tags a site's navigation.
func Nav(site string) string { return "nav:" + site }

// Channel tags a channel's info.
func Channel(name string) string { return "ch:" + name }

// Mapper turns an outbox event into the tags it invalidates.
type Mapper func(topic string, payload json.RawMessage) []string

// Items maps the core item.* events: the item, plus its type's listing tag (singleton or collection as
// reported by singleton). Draft-only writes (payload "draft": true) change nothing delivery serves and map
// to no tags.
func Items(singleton func(typ string) bool) Mapper {
	return func(topic string, payload json.RawMessage) []string {
		switch topic {
		case "item.created", "item.updated", "item.published", "item.deleted":
		default:
			return nil
		}
		var p struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Draft bool   `json:"draft"`
		}
		if json.Unmarshal(payload, &p) != nil || p.Draft {
			return nil
		}
		var tags []string
		if p.ID != "" {
			tags = append(tags, Item(p.ID))
		}
		if p.Type != "" {
			if singleton != nil && singleton(p.Type) {
				tags = append(tags, Singleton(p.Type))
			} else {
				tags = append(tags, Collection(p.Type))
			}
		}
		return tags
	}
}

// Tags runs the mappers over an event and returns the tags sorted and deduplicated. Tags with whitespace or
// empty tags are dropped. If any mapper returns All, the result is just All.
func Tags(mappers []Mapper, topic string, payload json.RawMessage) []string {
	var out []string
	for _, m := range mappers {
		for _, t := range m(topic, payload) {
			if t == All {
				return []string{All}
			}
			if t == "" || strings.ContainsAny(t, " \t\r\n,") {
				continue
			}
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Chunks joins tags with spaces into payloads of at most limit bytes (a single longer tag gets its own). Tags
// are sorted and deduplicated first, so equal inputs give equal output; All wins over everything else.
func Chunks(tags []string, limit int) []string {
	tags = slices.Clone(tags)
	slices.Sort(tags)
	tags = slices.Compact(tags)
	if slices.Contains(tags, All) {
		return []string{All}
	}
	var out []string
	var b strings.Builder
	for _, t := range tags {
		if b.Len() > 0 && b.Len()+1+len(t) > limit {
			out = append(out, b.String())
			b.Reset()
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(t)
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}
