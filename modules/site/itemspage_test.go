package site

import (
	"net/http"
	"net/url"
	"testing"
)

func TestItemsPageAndItemBlock(t *testing.T) {
	var last url.Values
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = r.URL.Query()
		switch r.URL.Path {
		case "/collections/post/items":
			w.Header().Set("Surrogate-Key", "c:post")
			if r.URL.Query().Get("total") == "true" {
				_, _ = w.Write([]byte(`{"items":[{"id":"1","type":"post","name":"One","fields":{}}],"hasMore":true,"total":7}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[{"id":"1","type":"post","name":"One","fields":{}}],"hasMore":true}`))
		case "/items/9":
			w.Header().Set("Surrogate-Key", "i:9")
			_, _ = w.Write([]byte(`{"item":{"id":"9","type":"page","name":"P","fields":{"title":"T",
				"body":[{"id":"b1","type":"hero","props":{"h":"Hi"}},
					{"id":"b2","type":"columns","props":{},"slots":{"left":[{"id":"b3","type":"text","props":{"t":"deep"},"slots":{"x":[{"id":"b4","type":"leaf","props":{}}]}}]}}],
				"aside":{"extra":[{"id":"b5","type":"note","props":{"n":1}}]}}}}`))
		default:
			w.WriteHeader(404)
		}
	})
	f := &fetcher{h: h, hdr: http.Header{}, channel: "web", tags: map[string]bool{}}
	p := &page{f: f, locale: "en", itemPath: func(it *Item) string { return "/p/" + it.ID }}

	pg, err := p.itemsPageFn("post", map[string]any{"limit": 1})
	if err != nil || len(pg.Items) != 1 || !pg.HasMore || pg.Total != -1 || pg.Items[0].Path != "/p/1" || last.Has("total") {
		t.Fatalf("page = %+v %v %v", pg, err, last)
	}
	pg, err = p.itemsPageFn("post", map[string]any{"limit": 1, "total": true})
	if err != nil || pg.Total != 7 || !pg.HasMore || last.Get("total") != "true" {
		t.Fatalf("page with total = %+v %v %v", pg, err, last)
	}
	if items, err := p.itemsFn("post", map[string]any{"limit": 1}); err != nil || len(items) != 1 {
		t.Errorf("items after itemsPage = %v %v", items, err)
	}
	{
		q := &page{}
		if pg, err := q.itemsPageFn("post"); err != nil || pg == nil || len(pg.Items) != 0 || pg.Total != -1 {
			t.Errorf("itemsPage without a fetcher = %+v %v", pg, err)
		}
		if b, err := q.itemBlockFn("9", "b1"); err != nil || b != nil {
			t.Errorf("itemBlock without a fetcher = %v %v", b, err)
		}
	}

	p.view = &View{p: p}
	b, err := p.itemBlockFn("9", "b1")
	if err != nil || b == nil || b.Type != "hero" || b.Props["h"] != "Hi" || b.Path != "" {
		t.Fatalf("block = %+v %v", b, err)
	}
	p.preview = true
	if b, err = p.itemBlockFn("9", "b4"); err != nil || b == nil || b.Type != "leaf" || b.Path != "" {
		t.Errorf("nested block = %+v %v", b, err)
	}
	b, err = p.itemBlockFn("9", "b2")
	if err != nil || b == nil || len(b.Slots["left"]) != 1 || b.Slots["left"][0].Slots["x"][0].ID != "b4" {
		t.Errorf("slots of a block = %+v %v", b, err)
	}
	if b, err = p.itemBlockFn("9", "b5"); err != nil || b == nil || b.Type != "note" {
		t.Errorf("block in another field = %+v %v", b, err)
	}
	for _, c := range [][2]string{{"9", "nope"}, {"9", ""}, {"404", "b1"}, {"", "b1"}} {
		if b, err := p.itemBlockFn(c[0], c[1]); err != nil || b != nil {
			t.Errorf("block %v = %+v %v", c, b, err)
		}
	}
	if got := f.tagList(); len(got) == 0 || got[len(got)-1] != "i:9" {
		t.Errorf("tags = %v", got)
	}
	vb, err := p.view.FindBlock("9", "b1")
	if err != nil || vb == nil || vb.ID != "b1" {
		t.Errorf("View.FindBlock = %+v %v", vb, err)
	}
	vp, err := p.view.ItemsPage("post")
	if err != nil || vp == nil || len(vp.Items) != 1 {
		t.Errorf("View.ItemsPage = %+v %v", vp, err)
	}
}

func TestItemsPageInTemplate(t *testing.T) {
	x := newExtensions()
	chain, _ := x.chain("starter")
	dir := t.TempDir()
	writeFile(t, dir, "templates/partials/t.html", `{{with itemsPage "post" (dict "total" true)}}{{len .Items}}/{{.HasMore}}/{{.Total}}{{end}}|{{with itemBlock "9" "b1"}}{{.Type}}{{end}}`)
	e, err := loadEngine(&sources{chain: chain, dir: dir}, true, nil)
	if err != nil {
		t.Fatalf("itemsPage and itemBlock do not parse: %v", err)
	}
	if len(e.names) == 0 {
		t.Fatal("no templates")
	}
}
