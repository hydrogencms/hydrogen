// Package cachetag names the cache tags shared by the delivery response cache, CDN purges and webhooks, and
// maps outbox events to the tags they invalidate.
package cachetag

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
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
		case "item.created", "item.updated", "item.published", "item.unpublished", "item.trashed", "item.deleted":
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

// NoStore, as the ttl of SetHeaders, keeps every cache from storing the response.
const NoStore time.Duration = -1

// SetHeaders sets the cache headers of a response a shared cache may keep for ttl (whole seconds; 0 lets it
// revalidate every time): Cache-Control, and the tags as Surrogate-Key (space-separated) and Cache-Tag
// (comma-separated) so CDNs purge it with the rest. A negative ttl (NoStore) sets no-store. Tags are sorted and
// deduplicated; empty tags and those with whitespace or commas are dropped, and none set no tag headers.
func SetHeaders(h http.Header, ttl time.Duration, tags []string) {
	if ttl < 0 {
		h.Set("Cache-Control", "no-store")
	} else {
		n := strconv.FormatInt(int64(ttl/time.Second), 10)
		h.Set("Cache-Control", "public, max-age=0, s-maxage="+n+", stale-while-revalidate="+n)
	}
	h.Del("Surrogate-Key")
	h.Del("Cache-Tag")
	if !cleanTags(tags) {
		tags = slices.DeleteFunc(slices.Clone(tags), badTag)
		slices.Sort(tags)
		tags = slices.Compact(tags)
	}
	if len(tags) == 0 {
		return
	}
	h.Set("Surrogate-Key", strings.Join(tags, " "))
	h.Set("Cache-Tag", strings.Join(tags, ","))
}

func badTag(t string) bool { return t == "" || strings.ContainsAny(t, " \t\r\n,") }

// cleanTags reports tags that are already sorted, unique and valid (the site's and delivery's are), so the
// common case copies nothing.
func cleanTags(tags []string) bool {
	for i, t := range tags {
		if badTag(t) || (i > 0 && tags[i-1] >= t) {
			return false
		}
	}
	return true
}
