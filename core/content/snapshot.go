package content

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/hydrogencms/hydrogen/core/ids"
	"github.com/hydrogencms/hydrogen/core/schema"
	"github.com/hydrogencms/hydrogen/core/store"
)

const (
	snapshotTimeLayout = "2006-01-02T15:04:05.000Z"
)

// putSnapshots replaces the delivery snapshots of the item with one per locale of the version just written.
func (s *Service) putSnapshots(ctx context.Context, c store.ContentStore, st *itemState, outs []*localeOut, version int, at time.Time) error {
	snaps := make([]store.Snapshot, len(outs))
	var buf bytes.Buffer
	for i, o := range outs {
		doc, refs := o.doc, o.refs
		if !o.final {
			doc = cloneDoc(doc)
			s.o.Schema.Normalize(st.typ, doc)
			s.o.Schema.Prune(st.typ, doc)
			refs = s.o.Schema.Refs(st.typ, doc)
		}
		buf.Reset()
		if err := writeSnapshotPayload(&buf, st.item.ID.String(), st.typ, st.item.Key, o.code, version, at, o.name, doc); err != nil {
			return fmt.Errorf("content: snapshot of %s/%s: %w", st.item.ID, o.code, err)
		}
		payload := bytes.Clone(buf.Bytes())
		sum := sha256.Sum256(payload)
		snaps[i] = store.Snapshot{ItemID: st.item.ID, Locale: o.code, Version: version, Type: st.typ.Name,
			Key: st.item.Key, MainLocale: st.item.MainLocale, AlwaysAvailable: st.item.AlwaysAvailable,
			Payload: payload, ETag: hex.EncodeToString(sum[:]), Refs: snapshotRefs(refs), PublishedAt: at.UTC().Truncate(time.Millisecond)}
	}
	if err := c.PutSnapshots(ctx, st.item.ID, snaps); err != nil {
		return fmt.Errorf("content: store snapshots of %s: %w", st.item.ID, err)
	}
	return nil
}

// snapshotRefs lists the distinct referenced item ids, sorted.
func snapshotRefs(refs []schema.RefValue) []ids.UUID {
	var out []ids.UUID
	for _, r := range refs {
		if id, err := ids.Parse(r.To); err == nil {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b ids.UUID) int { return bytes.Compare(a[:], b[:]) })
	return slices.Compact(out)
}

// jsonWriter appends JSON values to a buffer without HTML escaping or a trailing newline.
type jsonWriter struct {
	buf *bytes.Buffer
	enc *json.Encoder
}

func (w jsonWriter) value(v any) error {
	if err := w.enc.Encode(v); err != nil {
		return err
	}
	w.buf.Truncate(w.buf.Len() - 1)
	return nil
}

func (w jsonWriter) raw(s string) { w.buf.WriteString(s) }

func (w jsonWriter) key(name string) error {
	if err := w.value(name); err != nil {
		return err
	}
	w.buf.WriteByte(':')
	return nil
}

// writeSnapshotPayload writes {"id","type","key"?,"locale","version","publishedAt","name","fields"}; fields
// follow the schema's order (groups too), and numbers keep the digits they were stored with.
func writeSnapshotPayload(buf *bytes.Buffer, id string, typ *schema.Type, key, locale string, version int, at time.Time, name string, doc map[string]any) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	w := jsonWriter{buf, enc}
	w.raw(`{"id":`)
	if err := w.value(id); err != nil {
		return err
	}
	w.raw(`,"type":`)
	if err := w.value(typ.Name); err != nil {
		return err
	}
	if key != "" {
		w.raw(`,"key":`)
		if err := w.value(key); err != nil {
			return err
		}
	}
	w.raw(`,"locale":`)
	if err := w.value(locale); err != nil {
		return err
	}
	fmt.Fprintf(buf, `,"version":%d,"publishedAt":"%s","name":`, version, at.UTC().Format(snapshotTimeLayout))
	if err := w.value(name); err != nil {
		return err
	}
	w.raw(`,"fields":`)
	if err := w.fields(typ.Fields, doc); err != nil {
		return err
	}
	w.raw("}")
	return nil
}

func (w jsonWriter) fields(fields []*schema.Field, doc map[string]any) error {
	w.raw("{")
	first := true
	for _, f := range fields {
		v, ok := doc[f.Name]
		if !ok {
			continue
		}
		if !first {
			w.raw(",")
		}
		first = false
		if err := w.key(f.Name); err != nil {
			return err
		}
		if err := w.field(f, v); err != nil {
			return err
		}
	}
	w.raw("}")
	return nil
}

func (w jsonWriter) field(f *schema.Field, v any) error {
	switch f.Shape.Base {
	case schema.BaseGroup:
		if m, ok := v.(map[string]any); ok {
			return w.fields(f.Fields, m)
		}
	case schema.BaseList:
		if list, ok := v.([]any); ok && f.Of != nil {
			w.raw("[")
			for i, e := range list {
				if i > 0 {
					w.raw(",")
				}
				if err := w.field(f.Of, e); err != nil {
					return err
				}
			}
			w.raw("]")
			return nil
		}
	}
	return w.value(v)
}
