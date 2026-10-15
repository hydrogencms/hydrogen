package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hydrogencms/hydrogen/channel"
	"github.com/hydrogencms/hydrogen/clock"
	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/schema"
	"github.com/hydrogencms/hydrogen/store"
)

func TestSplice(t *testing.T) {
	extra, err := extraJSON(map[string]any{"url": "/a?b=1&c=<2>", "links": map[string]string{"b": "/b", "a": "/a"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	want := `"fallback":true,"links":{"a":"/a","b":"/b"},"url":"/a?b=1&c=<2>"`
	if string(extra) != want {
		t.Fatalf("extra = %s", extra)
	}
	payload := []byte(`{"id":"x","fields":{}}`)
	got := string(appendSpliced(nil, payload, extra))
	if got != `{`+want+`,"id":"x","fields":{}}` {
		t.Fatalf("spliced = %s", got)
	}
	if string(payload) != `{"id":"x","fields":{}}` {
		t.Fatal("payload modified")
	}
	if got := string(appendSpliced(nil, []byte(`{}`), extra)); got != `{`+want+`}` {
		t.Fatalf("empty payload = %s", got)
	}
	if got := string(appendSpliced(nil, payload, nil)); got != string(payload) {
		t.Fatalf("no extra = %s", got)
	}
	if e, _ := extraJSON(nil, false); e != nil {
		t.Fatal("nothing to add must be nil")
	}
	if _, err := extraJSON(map[string]any{"fields": 1}, false); err == nil {
		t.Fatal("reserved key accepted")
	}
	if _, err := extraJSON(map[string]any{"x": make(chan int)}, false); err == nil {
		t.Fatal("unencodable value accepted")
	}
}

func TestCanonicalQuery(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", ""}, {"b=2&a=1", "a=1&b=2"}, {"a=1&&b=2&", "a=1&b=2"},
		{"i=9&h=8&g=7&f=6&e=5&d=4&c=3&b=2&a=1", "a=1&b=2&c=3&d=4&e=5&f=6&g=7&h=8&i=9"},
		// Repeated values keep their order: it is part of the request's meaning.
		{"locale=de&locale=en", "locale=de&locale=en"}, {"locale=en&locale=de", "locale=en&locale=de"},
		{"b=1&a=2&b=0&a=1&c&a=0", "a=2&a=1&a=0&b=1&b=0&c"},
	} {
		if got := string(appendCanonQuery(nil, c.in)); got != c.want {
			t.Errorf("%q -> %q, want %q", c.in, got, c.want)
		}
	}
	if rawParam("a=1&channel=x%20y&b", "channel") != "x y" || rawParam("a=1", "channel") != "" || rawParam("channel=p+q", "channel") != "p q" {
		t.Error("rawParam")
	}
}

func TestRepeatedLocalesDoNotShareACacheKey(t *testing.T) {
	a, b := appendCanonQuery(nil, "locale=de&locale=en"), appendCanonQuery(nil, "locale=en&locale=de")
	if string(a) == string(b) {
		t.Fatalf("both canonicalise to %q", a)
	}
}

func TestRepeatedSingleValuedParametersAreRejected(t *testing.T) {
	ch := &channel.Channel{Name: "c", Languages: []string{"en", "de"}}
	for _, name := range singleValued {
		st := &state{m: &chanMeta{ch: ch}, q: mustQuery(name + "=en&" + name + "=de")}
		if err := st.narrow(); err == nil || err.Status != http.StatusBadRequest || !strings.Contains(err.Message, name) {
			t.Errorf("%s repeated: %v", name, err)
		}
		st = &state{m: &chanMeta{ch: ch}, q: mustQuery(name + "=en")}
		if name != "locale" && st.narrow() != nil {
			t.Errorf("%s once was rejected", name)
		}
	}
	// where[...] may repeat.
	st := &state{m: &chanMeta{ch: ch}, q: mustQuery("where[rank][gte]=1&where[rank][gte]=2")}
	if err := st.narrow(); err != nil {
		t.Errorf("repeated where: %v", err)
	}
}

func TestValidKey(t *testing.T) {
	for k, want := range map[string]bool{"a": true, "a-b_c9": true, "0x": true, "": false, "-a": false, "_a": false, "A": false,
		"a b": false, "a\x00": false, "a/b": false, "é": false, strings.Repeat("a", 128): true, strings.Repeat("a", 129): false} {
		if got := validKey(k); got != want {
			t.Errorf("validKey(%q) = %v", k, got)
		}
	}
}

func TestETagMatches(t *testing.T) {
	const e = `"abc"`
	for in, want := range map[string]bool{`"abc"`: true, `W/"abc"`: true, `"x", "abc"`: true, `*`: true, `"abd"`: false, ``: false} {
		if got := etagMatches([]string{in}, e); got != want {
			t.Errorf("%q = %v", in, got)
		}
	}
}

func newTestEntry(key string, size int, tags ...string) *entry {
	return &entry{key: key, body: make([]byte, size), tags: tags}
}

func TestCacheLRUEvictsLeastRecentlyUsed(t *testing.T) {
	c := newCache(4500)
	for i := range 10 {
		if !c.put(newTestEntry(fmt.Sprintf("k%d", i), 100), c.generation()) {
			t.Fatal("put refused")
		}
	}
	c.get([]byte("k0")) // k0 is now the most recently used; k1 the oldest
	c.put(newTestEntry("k10", 100), c.generation())
	if c.get([]byte("k1")) != nil {
		t.Error("k1 should have been evicted")
	}
	if c.get([]byte("k0")) == nil || c.get([]byte("k10")) == nil {
		t.Error("recent entries evicted")
	}
	n, used := c.stats()
	if n != 10 || used > c.max {
		t.Errorf("%d entries, %d bytes (max %d)", n, used, c.max)
	}
}

func TestCacheRefusesHugeEntries(t *testing.T) {
	c := newCache(1000)
	if c.put(newTestEntry("big", 5000), c.generation()) {
		t.Error("an entry above a quarter of the cache was stored")
	}
}

func TestCachePurgeByTag(t *testing.T) {
	c := newCache(1 << 20)
	c.put(newTestEntry("a", 10, "i:1", "c:article"), c.generation())
	c.put(newTestEntry("b", 10, "i:2", "c:article"), c.generation())
	c.put(newTestEntry("c", 10, "i:3"), c.generation())
	c.purge("i:1")
	if c.get([]byte("a")) != nil || c.get([]byte("b")) == nil || c.get([]byte("c")) == nil {
		t.Error("purge i:1")
	}
	c.purge("c:article", "unknown")
	if c.get([]byte("b")) != nil || c.get([]byte("c")) == nil {
		t.Error("purge c:article")
	}
	if len(c.byTag) != 1 {
		t.Errorf("tag index leaks: %v", c.byTag)
	}
	c.purge("*")
	if n, used := c.stats(); n != 0 || used != 0 || len(c.byTag) != 0 {
		t.Errorf("after * : %d entries, %d bytes", n, used)
	}
}

func TestCacheDropsResponsesComputedBeforeAPurge(t *testing.T) {
	c := newCache(1 << 20)
	gen := c.generation()
	c.purge("i:1")
	if c.put(newTestEntry("stale", 10, "i:1"), gen) {
		t.Error("a response computed before a purge was stored")
	}
	if !c.put(newTestEntry("fresh", 10, "i:1"), c.generation()) {
		t.Error("a fresh response was refused")
	}
}

func TestCacheReplacesKey(t *testing.T) {
	c := newCache(1 << 20)
	c.put(newTestEntry("k", 10, "t"), c.generation())
	c.put(newTestEntry("k", 20, "t"), c.generation())
	if n, _ := c.stats(); n != 1 || len(c.byTag["t"]) != 1 {
		t.Errorf("duplicate key kept both: %d entries, %d in index", n, len(c.byTag["t"]))
	}
}

func testSet(t testing.TB) *channel.Set {
	t.Helper()
	set, err := channel.Compile(nil, channel.CompileOptions{DefaultLocale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestPreviewTokens(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 10, 14, 9, 0, 0, 0, time.UTC))
	a := &API{o: Options{Clock: clk, PreviewSecret: []byte("secret-one"), Channels: testSet(t)}}
	item := ids.UUID{1, 2, 3}

	tok, exp, err := a.MintPreviewToken("default", ids.Nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := clk.Now().Add(DefaultPreviewTTL); !exp.Equal(want) {
		t.Errorf("default ttl: expires %v, want %v", exp, want)
	}
	cl, err := a.verifyPreview(tok)
	if err != nil || cl.Channel != "default" || cl.Item != "" {
		t.Fatalf("verify = %+v, %v", cl, err)
	}

	scoped, _, err := a.MintPreviewToken("default", item, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if cl, err = a.verifyPreview(scoped); err != nil || cl.Item != item.String() {
		t.Fatalf("scoped = %+v, %v", cl, err)
	}

	// Tampering: payload swapped for another, signature cut, bit flips, other secret, garbage.
	other, _, _ := a.MintPreviewToken("default", item, time.Minute)
	payload, sig, _ := strings.Cut(tok, ".")
	otherPayload, _, _ := strings.Cut(other, ".")
	b := &API{o: Options{Clock: clk, PreviewSecret: []byte("secret-two"), Channels: testSet(t)}}
	for name, bad := range map[string]string{
		"swapped payload": otherPayload + "." + sig,
		"no signature":    payload,
		"empty signature": payload + ".",
		"flipped":         payload + "." + flip(sig),
		"garbage":         "not a token",
		"empty":           ".",
		"bad base64":      "!!!." + sig,
	} {
		if _, err := a.verifyPreview(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := b.verifyPreview(tok); err == nil {
		t.Error("a token verified under another secret")
	}

	// Expiry.
	clk.Advance(DefaultPreviewTTL - time.Second)
	if _, err := a.verifyPreview(tok); err != nil {
		t.Errorf("one second before expiry: %v", err)
	}
	clk.Advance(time.Second)
	if _, err := a.verifyPreview(tok); err == nil {
		t.Error("expired token accepted")
	}

	for _, ttl := range []time.Duration{-time.Second, MaxPreviewTTL + time.Second} {
		if _, _, err := a.MintPreviewToken("default", ids.Nil, ttl); err == nil {
			t.Errorf("ttl %v accepted", ttl)
		}
	}
	if _, _, err := a.MintPreviewToken("default", ids.Nil, MaxPreviewTTL); err != nil {
		t.Errorf("max ttl refused: %v", err)
	}
	if _, _, err := a.MintPreviewToken("nope", ids.Nil, 0); err == nil {
		t.Error("unknown channel accepted")
	}
}

func flip(s string) string {
	b := []byte(s)
	if b[0] == 'A' {
		b[0] = 'B'
	} else {
		b[0] = 'A'
	}
	return string(b)
}

func TestPreviewTokenSources(t *testing.T) {
	r := httptest.NewRequest("GET", "/items/x?a=1&preview=q%2Edot", nil)
	if got := previewToken(r); got != "q.dot" {
		t.Errorf("query = %q", got)
	}
	r.Header.Set(previewHeader, "hdr")
	if got := previewToken(r); got != "hdr" {
		t.Errorf("header = %q", got)
	}
	if previewToken(httptest.NewRequest("GET", "/items/x", nil)) != "" {
		t.Error("token from nowhere")
	}
}

func TestParseTTL(t *testing.T) {
	for in, want := range map[string]time.Duration{``: 0, `null`: 0, `60`: time.Minute, `"15m"`: 15 * time.Minute, `3600`: time.Hour} {
		got, err := parseTTL([]byte(in))
		if err != nil || got != want {
			t.Errorf("%q = %v, %v", in, got, err)
		}
	}
	for _, in := range []string{`0`, `3601`, `"2h"`, `"x"`, `true`, `-5`} {
		if _, err := parseTTL([]byte(in)); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

// fakeReader serves snapshots from memory and counts calls.
type fakeReader struct {
	rows  []store.Snapshot
	calls int
}

func (f *fakeReader) Snapshots(_ context.Context, items []ids.UUID, locales []string) ([]store.Snapshot, error) {
	f.calls++
	var out []store.Snapshot
	for _, s := range f.rows {
		if slices.Contains(items, s.ItemID) && (len(locales) == 0 || slices.Contains(locales, s.Locale)) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (f *fakeReader) SnapshotsByKey(context.Context, string, []string) ([]store.Snapshot, error) {
	return nil, nil
}

func (f *fakeReader) Query(context.Context, store.Query) (store.QueryResult, error) {
	return store.QueryResult{}, nil
}

func uid(n byte) ids.UUID { return ids.UUID{15: n} }

func snap(n byte, locale, typ string, refs ...byte) store.Snapshot {
	s := store.Snapshot{ItemID: uid(n), Locale: locale, Type: typ, MainLocale: "en", Payload: []byte(`{"id":"` + uid(n).String() + `"}`)}
	for _, r := range refs {
		s.Refs = append(s.Refs, uid(r))
	}
	return s
}

func testState(rd store.Reader, ch *channel.Channel) *state {
	a := &API{o: Options{Reader: rd}}
	return &state{a: a, m: &chanMeta{ch: ch}, eff: ch}
}

func idsOf(list []*resolved) []byte {
	var out []byte
	for _, r := range list {
		out = append(out, r.id[15])
	}
	return out
}

func TestIncludeBreadthFirstAndCycleSafe(t *testing.T) {
	// 1 -> 2,3 ; 2 -> 1 (cycle back to the primary), 4 ; 3 -> 5 (not published) ; 4 -> 6 ; 6 -> 7
	rd := &fakeReader{rows: []store.Snapshot{
		snap(1, "en", "article", 2, 3), snap(2, "en", "article", 1, 4), snap(3, "en", "article", 5),
		snap(4, "en", "article", 6), snap(6, "en", "article", 7), snap(7, "en", "article"),
	}}
	ch := &channel.Channel{Name: "c", Languages: []string{"en"}}
	prim := []*resolved{{id: uid(1), refs: []ids.UUID{uid(2), uid(3)}}}

	for depth, want := range map[int][]byte{1: {2, 3}, 2: {2, 3, 4}, 3: {2, 3, 4, 6}} {
		got, err := testState(rd, ch).include(context.Background(), prim, depth)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(idsOf(got), want) {
			t.Errorf("depth %d: %v, want %v", depth, idsOf(got), want)
		}
	}
	rd.calls = 0
	if _, err := testState(rd, ch).include(context.Background(), prim, 3); err != nil {
		t.Fatal(err)
	}
	// One query per level, plus the all-locales retry at level 2, where item 5 has no row.
	if rd.calls != 4 {
		t.Errorf("%d reader calls", rd.calls)
	}
}

func TestIncludeSkipsDisallowedAndCapsAt100(t *testing.T) {
	rd := &fakeReader{}
	var refs []byte
	for i := 10; i < 160; i++ {
		rd.rows = append(rd.rows, snap(byte(i), "en", "article"))
		refs = append(refs, byte(i))
	}
	rd.rows = append(rd.rows, snap(9, "en", "person"))
	root := snap(1, "en", "article", append([]byte{9}, refs...)...)
	ch := &channel.Channel{Name: "c", Languages: []string{"en"}, Collections: []string{"article"}}
	got, err := testState(rd, ch).include(context.Background(), []*resolved{{id: uid(1), refs: root.Refs}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 100 {
		t.Errorf("%d included, want 100", len(got))
	}
	for _, r := range got {
		if r.typ == "person" {
			t.Error("a disallowed collection was included")
		}
	}
}

func TestLoadPicksLocaleAndAlwaysAvailable(t *testing.T) {
	de := snap(1, "de", "article")
	en := snap(1, "en", "article")
	only := snap(2, "en", "article")
	only.AlwaysAvailable = true
	never := snap(3, "en", "article")
	rd := &fakeReader{rows: []store.Snapshot{de, en, only, never}}

	ch := &channel.Channel{Name: "c", Languages: []string{"de", "en"}}
	got, err := testState(rd, ch).load(context.Background(), []ids.UUID{uid(1), uid(9)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].locale != "de" || got[0].fallback {
		t.Errorf("de first: %+v", got)
	}
	fr := &channel.Channel{Name: "c", Languages: []string{"fr", "de"}}
	got, _ = testState(rd, fr).load(context.Background(), []ids.UUID{uid(1)})
	if len(got) != 1 || got[0].locale != "de" || !got[0].fallback {
		t.Errorf("fr then de: %+v", got)
	}
	got, _ = testState(rd, &channel.Channel{Name: "c", Languages: []string{"fr"}}).load(context.Background(), []ids.UUID{uid(1), uid(2), uid(3)})
	if len(got) != 1 || got[0].id != uid(2) || got[0].locale != "en" || !got[0].fallback {
		t.Errorf("always available only: %+v", got)
	}
}

func TestNarrowLocale(t *testing.T) {
	ch := &channel.Channel{Name: "c", Languages: []string{"en", "de", "fr"}}
	for q, want := range map[string]string{"": "en,de,fr", "locale=en": "en,de,fr", "locale=fr": "fr,en,de"} {
		st := &state{m: &chanMeta{ch: ch}, q: mustQuery(q)}
		if err := st.narrow(); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(st.eff.Languages, ","); got != want {
			t.Errorf("%q -> %s, want %s", q, got, want)
		}
	}
	st := &state{m: &chanMeta{ch: ch}, q: mustQuery("locale=it")}
	if err := st.narrow(); err == nil || err.Status != http.StatusBadRequest {
		t.Errorf("unknown locale: %v", err)
	}
	if strings.Join(ch.Languages, ",") != "en,de,fr" {
		t.Error("the channel was modified")
	}
}

func TestCacheTTLSetting(t *testing.T) {
	for _, c := range []struct {
		v    any
		want int
	}{{nil, 60}, {300, 300}, {int64(5), 5}, {float64(7), 7}, {"x", 60}, {-3, 0}} {
		ch := &channel.Channel{}
		if c.v != nil {
			ch.Settings = map[string]any{"delivery": map[string]any{"cache_ttl": c.v}}
		}
		if got := cacheTTL(ch); got != c.want {
			t.Errorf("%v -> %d, want %d", c.v, got, c.want)
		}
	}
}

func mustQuery(raw string) url.Values {
	q, err := url.ParseQuery(raw)
	if err != nil {
		panic(err)
	}
	return q
}

func testSchema(t testing.TB) *schema.Schema {
	t.Helper()
	fsys := fstest.MapFS{"schema/s.yaml": {Data: []byte(`types:
  article: { fields: { title: { type: string } } }
  person: { fields: { fullName: { type: string } } }
  settings: { singleton: true, fields: { site: { type: string } } }
`)}}
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fsys, "schema"); err != nil {
		t.Fatal(err)
	}
	s, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func tagState(t testing.TB) *state {
	t.Helper()
	sch := testSchema(t)
	a := &API{o: Options{Schema: sch}}
	return &state{a: a, m: &chanMeta{ch: &channel.Channel{Name: "c", Languages: []string{"en"}}}}
}

func TestItemTagsCoverUnavailableItems(t *testing.T) {
	st := tagState(t)
	st.look(uid(1), "article")
	st.look(uid(2), "") // looked up, not available: type unknown
	st.tags = []string{"c:article"}
	got := st.responseTags()
	want := []string{"c:article", "i:" + uid(1).String(), "i:" + uid(2).String()}
	if !slices.Equal(got, want) {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

func TestItemTagsCollapseBeyondTheCap(t *testing.T) {
	st := tagState(t)
	for i := range MaxItemTags {
		st.look(ids.UUID{14: 1, 15: byte(i)}, "article")
	}
	if got := st.responseTags(); len(got) != MaxItemTags || !strings.HasPrefix(got[0], "i:") {
		t.Fatalf("at the cap: %d tags", len(got))
	}
	st.look(ids.UUID{14: 2}, "person")
	st.look(ids.UUID{14: 3}, "settings")
	if got := st.responseTags(); !slices.Equal(got, []string{"c:article", "c:person", "s:settings"}) {
		t.Errorf("collapsed = %v", got)
	}
	// An unavailable item's type is unknown, so every type the channel shows is tagged.
	st.look(ids.UUID{14: 4}, "")
	if got := st.responseTags(); !slices.Equal(got, []string{"c:article", "c:person", "s:settings"}) {
		t.Errorf("with unknown = %v", got)
	}
	st.m.ch = &channel.Channel{Name: "c", Languages: []string{"en"}, Collections: []string{"article"}}
	st.looked = map[ids.UUID]string{}
	for i := range MaxItemTags + 1 {
		st.look(ids.UUID{14: 1, 15: byte(i)}, "")
	}
	if got := st.responseTags(); !slices.Equal(got, []string{"c:article"}) {
		t.Errorf("unknown types on a one-collection channel = %v", got)
	}
}

func TestETagReusesTheStoredOne(t *testing.T) {
	st := &state{}
	payload := []byte(`{"id":"x"}`)
	st.whole = []stored{{payload, "feedface"}}
	if got := st.etagOf(append([]byte(nil), payload...)); got != `"feedface"` {
		t.Errorf("unchanged payload: %s", got)
	}
	sum := sha256.Sum256([]byte(`{"item":{"id":"x"}}`))
	if got := st.etagOf([]byte(`{"item":{"id":"x"}}`)); got != `"`+hex.EncodeToString(sum[:])+`"` {
		t.Errorf("a different body must be hashed: %s", got)
	}
	if got := st.etagOf([]byte(`{"id":"y"}`)); got == `"feedface"` {
		t.Error("same length, other bytes, reused the stored etag")
	}
}

func TestCachePurgeEvictsInSlices(t *testing.T) {
	c := newCache(1 << 24)
	n := 3*purgeSlice + 17
	for i := range n {
		c.put(newTestEntry(fmt.Sprintf("a%d", i), 10, "c:article", fmt.Sprintf("i:%d", i)), c.generation())
		c.put(newTestEntry(fmt.Sprintf("b%d", i), 10, "c:person"), c.generation())
	}
	c.purge("c:article")
	for i := range n {
		if c.get([]byte(fmt.Sprintf("a%d", i))) != nil {
			t.Fatalf("a%d survived", i)
		}
		if c.get([]byte(fmt.Sprintf("b%d", i))) == nil {
			t.Fatalf("b%d purged", i)
		}
	}
	if entries, _ := c.stats(); entries != n || len(c.byTag) != 1 {
		t.Errorf("%d entries, %d tags in the index", entries, len(c.byTag))
	}
	var want int64
	for e := c.head.next; e != &c.head; e = e.next {
		want += e.size
	}
	if _, used := c.stats(); used != want {
		t.Errorf("used %d, entries hold %d", used, want)
	}
}

func TestCacheDoesNotServeAPurgedEntryWhileItWaitsForEviction(t *testing.T) {
	c := newCache(1 << 20)
	e := newTestEntry("k", 10, "t")
	c.put(e, c.generation())
	c.mu.Lock()
	e.dead = true // what purge does under its first lock hold, before the slices are evicted
	c.mu.Unlock()
	if c.get([]byte("k")) != nil {
		t.Error("a dead entry was served")
	}
	if !c.put(newTestEntry("k", 10, "t"), c.generation()) || c.get([]byte("k")) == nil {
		t.Error("the key cannot be refilled")
	}
	c.purge("t") // must not evict the replacement twice or corrupt the accounting
	if n, used := c.stats(); n != 0 || used != 0 {
		t.Errorf("%d entries, %d bytes", n, used)
	}
}

func TestMatchesWhere(t *testing.T) {
	fsys := fstest.MapFS{"schema/s.yaml": {Data: []byte(`types:
  doc:
    fields:
      slug: { type: string, index: true }
      rating: { type: number, index: true }
      tags: { type: list, of: { type: string }, index: true }
      on: { type: boolean, index: true }
      day: { type: date, index: true }
      at: { type: datetime, index: true }
      author: { type: ref, to: [doc], index: true }
`)}}
	b := schema.NewBuilder(schema.NewRegistry())
	if err := b.LoadFS(fsys, "schema"); err != nil {
		t.Fatal(err)
	}
	sch, err := b.Compile()
	if err != nil {
		t.Fatal(err)
	}
	typ, _ := sch.Type("doc")
	fields := map[string]any{"slug": "hi", "rating": json.Number("3"), "tags": []any{"a", "b"}, "on": true,
		"day": "2026-03-01", "at": "2026-03-01T10:00:00.123456Z", "author": "01A13DF6-4D9F-75A3-A35D-4FDF076E1998"}
	empty := map[string]any{"slug": "", "author": ""}
	author, _ := ids.Parse("01a13df6-4d9f-75a3-a35d-4fdf076e1998")
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		c    store.Cond
		want bool
	}{
		{store.Cond{Field: "slug", Column: store.ColText, Op: store.OpEq, Value: "hi"}, true},
		{store.Cond{Field: "slug", Column: store.ColText, Op: store.OpEq, Value: "no"}, false},
		{store.Cond{Field: "slug", Column: store.ColText, Op: store.OpNe, Value: "no"}, true},
		{store.Cond{Field: "missing", Column: store.ColText, Op: store.OpNe, Value: "no"}, true},
		{store.Cond{Field: "slug", Column: store.ColText, Op: store.OpPrefix, Value: "h"}, true},
		{store.Cond{Field: "slug", Column: store.ColText, Op: store.OpPrefix, Value: "H"}, false},
		{store.Cond{Field: "slug", Column: store.ColText, Op: store.OpIn, Value: []string{"x", "hi"}}, true},
		{store.Cond{Field: "rating", Column: store.ColNum, Op: store.OpGte, Value: 3.0}, true},
		{store.Cond{Field: "rating", Column: store.ColNum, Op: store.OpLt, Value: 3.0}, false},
		{store.Cond{Field: "tags", Column: store.ColText, Op: store.OpContains, Value: "b"}, true},
		{store.Cond{Field: "on", Column: store.ColBool, Op: store.OpEq, Value: true}, true},
		{store.Cond{Field: "day", Column: store.ColTime, Op: store.OpEq, Value: day}, true},
		{store.Cond{Field: "day", Column: store.ColTime, Op: store.OpGt, Value: day.AddDate(0, -1, 0)}, true},
		{store.Cond{Field: "at", Column: store.ColTime, Op: store.OpEq, Value: time.Date(2026, 3, 1, 10, 0, 0, 123000000, time.UTC)}, true},
		{store.Cond{Field: "author", Column: store.ColRef, Op: store.OpEq, Value: author}, true},
		{store.Cond{Field: "slug", Op: store.OpExists, Value: true}, true},
		{store.Cond{Field: "missing", Op: store.OpExists, Value: true}, false},
		{store.Cond{Field: "missing", Column: store.ColText, Op: store.OpEq, Value: "x"}, false},
	}
	for _, c := range cases {
		if got := matchesWhere([]store.Cond{c.c}, sch.IndexValues(typ, fields)); got != c.want {
			t.Errorf("%+v: %v, want %v", c.c, got, c.want)
		}
	}
	// An empty string is not indexed, so it is absent.
	vals := sch.IndexValues(typ, empty)
	if matchesWhere([]store.Cond{{Field: "slug", Column: store.ColText, Op: store.OpEq, Value: ""}}, vals) ||
		matchesWhere([]store.Cond{{Field: "slug", Op: store.OpExists, Value: true}}, vals) ||
		!matchesWhere([]store.Cond{{Field: "author", Op: store.OpExists, Value: false}}, vals) {
		t.Error("empty strings must count as absent")
	}
}
