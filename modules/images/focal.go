package images

import (
	"context"
	"time"

	"github.com/hydrogencms/hydrogen/core/content"
	"github.com/hydrogencms/hydrogen/core/kernel"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const focalPage = 200

// focalSet is the focal point tokens of a file and when they were loaded.
type focalSet struct {
	tokens map[string]bool
	at     time.Time
}

// itemFocals collects the quantized focal point tokens of every item of an upload type whose file is sha, in
// its working document (drafts) and its published one. It reads as the system: URLs are public.
func itemFocals(ctx context.Context, s *kernel.Services, sha string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, t := range s.Schema.Types() {
		if t.Upload == nil {
			continue
		}
		for _, status := range []store.Status{store.StatusAny, store.StatusPublished} {
			for offset := 0; ; offset += focalPage {
				res, err := s.Content.List(ctx, content.ListOptions{Type: t.Name, Status: status, Limit: focalPage, Offset: offset,
					Where:  []content.Filter{{Field: schema.UploadFile, Op: store.OpEq, Value: sha}},
					Fields: []string{schema.UploadFile}})
				if err != nil {
					return nil, err
				}
				for _, it := range res.Items {
					file, _ := it.Fields[schema.UploadFile].(map[string]any)
					if f := focalOf(file["focal"]); f != nil {
						out[focalToken(f)] = true
					}
				}
				if len(res.Items) < focalPage {
					break
				}
			}
		}
	}
	return out, nil
}
