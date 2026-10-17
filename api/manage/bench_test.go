package manage

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/ids"
)

func BenchmarkListHandler(b *testing.B) {
	f := newFake(b)
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	items := make([]content.Summary, 50)
	for i := range items {
		items[i] = content.Summary{ID: ids.UUID{byte(i + 1)}, Type: "article", Key: fmt.Sprintf("k%d", i), Name: fmt.Sprintf("Article %d", i),
			Status: content.StatusPublished, Version: 3, Locales: []string{"en", "de"}, DraftParts: []string{}, CreatedAt: at, UpdatedAt: at}
	}
	f.list = &content.ListResult{Items: items, Total: 1234}
	h := newAPI(b, f)
	const target = "/collections/article/items?locale=en&status=published&q=news&sort=-rating,title&limit=50&offset=100" +
		"&where[rating][gte]=3&where[tags][contains]=go&where[author][eq]=abc"
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		r := httptest.NewRequest("GET", target, nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
