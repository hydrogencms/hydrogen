package storeutil

import (
	"strings"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/ids"
	"github.com/hydrogencms/hydrogen/store"
)

func TestValidField(t *testing.T) {
	for s, want := range map[string]bool{"title": true, "a.b1": true, "": false, "1a": false, "A": false, "a b": false, "a'": false} {
		if got := ValidField(s); got != want {
			t.Errorf("ValidField(%q) = %v", s, got)
		}
	}
}

func TestNormalize(t *testing.T) {
	if q := Normalize(store.Query{Limit: 0, Offset: -3}); q.Limit != DefaultLimit || q.Offset != 0 {
		t.Errorf("%+v", q)
	}
	if q := Normalize(store.Query{Limit: 9999}); q.Limit != MaxLimit {
		t.Errorf("%+v", q)
	}
}

func TestSearchTerms(t *testing.T) {
	if got := SearchTerms(`fox, "été" a:b!!`); strings.Join(got, "|") != "fox|été|a|b" {
		t.Errorf("%q", got)
	}
}

func TestScalarAndList(t *testing.T) {
	id := ids.UUID{1}
	if v, err := Scalar(store.ColRef, id.String()); err != nil || v != id {
		t.Errorf("%v %v", v, err)
	}
	if v, err := Scalar(store.ColNum, 3); err != nil || v != 3.0 {
		t.Errorf("%v %v", v, err)
	}
	if _, err := Scalar(store.ColText, 3); err == nil {
		t.Error("want error")
	}
	if _, err := Scalar(store.ColRef, "nope"); err == nil {
		t.Error("want error")
	}
	now := time.Now()
	if l, err := List(store.ColTime, []time.Time{now}); err != nil || len(l) != 1 {
		t.Errorf("%v %v", l, err)
	}
	if _, err := List(store.ColText, "x"); err == nil {
		t.Error("want error")
	}
}

func TestSearchTexts(t *testing.T) {
	loc, txt := SearchTexts([]store.SearchDoc{{Locale: "fr", Text: "a"}, {Locale: "en", Text: "b"}, {Locale: "en", Text: "c"}})
	if strings.Join(loc, ",") != "en,fr" || txt[0] != "b\nc" || txt[1] != "a" {
		t.Errorf("%v %q", loc, txt)
	}
	_, txt = SearchTexts([]store.SearchDoc{{Locale: "en", Text: strings.Repeat("é", MaxSearchChars+10)}})
	if n := len([]rune(txt[0])); n != MaxSearchChars {
		t.Errorf("capped to %d", n)
	}
}

func TestDecorator(t *testing.T) {
	a, b := ids.UUID{1}, ids.UUID{2}
	items := []store.ItemSummary{{Item: store.Item{ID: a, MainLocale: "en"}}, {Item: store.Item{ID: b, MainLocale: "en"}}}
	d := NewDecorator("fr", items)
	d.Published(a, "fr", "Bonjour")
	d.Published(a, "en", "Hello")
	d.Published(b, "en", "Only")
	d.Draft(b, "", "")
	d.Finish()
	if items[0].Name != "Bonjour" || items[0].Locales[0] != "en" || items[1].Name != "Only" || len(items[1].Drafts) != 1 {
		t.Errorf("%+v", items)
	}
}
