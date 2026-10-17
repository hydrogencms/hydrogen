package content

import (
	"bytes"
	"fmt"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/store"
)

// ListQuery validates list options and turns them into the store query List would run, for readers that run
// it themselves (the delivery API).
func (s *Service) ListQuery(o ListOptions) (store.Query, error) { return s.query(o) }

// PreviewPayload encodes a view (typically a draft view) as the delivery payload a snapshot of it would
// hold, and lists the items it references (sorted, unique). A never-published item shows its last change as
// publishedAt.
func (s *Service) PreviewPayload(v *View) (payload []byte, refs []ids.UUID, err error) {
	t, err := s.typeOf(v.Type)
	if err != nil {
		return nil, nil, err
	}
	doc := cloneDoc(v.Fields)
	s.o.Schema.Normalize(t, doc)
	s.o.Schema.Prune(t, doc)
	at := v.UpdatedAt
	if v.PublishedAt != nil && !v.Draft {
		at = *v.PublishedAt
	}
	var buf bytes.Buffer
	if err := writeSnapshotPayload(&buf, v.ID.String(), t, v.Key, v.Locale, v.Version, at.UTC().Truncate(time.Millisecond), v.Name, doc); err != nil {
		return nil, nil, fmt.Errorf("content: preview payload of %s: %w", v.ID, err)
	}
	return buf.Bytes(), snapshotRefs(s.o.Schema.Refs(t, doc)), nil
}
