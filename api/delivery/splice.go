package delivery

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
)

// reserved are the top-level keys of a payload; decorators cannot set them.
var reserved = map[string]bool{"id": true, "type": true, "key": true, "locale": true, "version": true,
	"publishedAt": true, "name": true, "fields": true, "fallback": true}

// extraJSON encodes the keys to splice into a payload as `"k":v,"k":v` (no braces), sorted by key. fallback
// adds "fallback":true. It returns nil when there is nothing to add.
func extraJSON(extra map[string]any, fallback bool) ([]byte, error) {
	if len(extra) == 0 {
		return fallbackExtra(fallback), nil
	}
	keys := make([]string, 0, len(extra)+1)
	for k := range extra {
		if reserved[k] {
			return nil, fmt.Errorf("delivery: a decorator set the reserved key %q", k)
		}
		keys = append(keys, k)
	}
	if fallback {
		keys = append(keys, "fallback")
	}
	slices.Sort(keys)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := enc.Encode(k); err != nil {
			return nil, err
		}
		buf.Truncate(buf.Len() - 1)
		buf.WriteByte(':')
		if k == "fallback" && fallback {
			buf.WriteString("true")
			continue
		}
		if err := enc.Encode(extra[k]); err != nil {
			return nil, fmt.Errorf("delivery: encode decorator key %q: %w", k, err)
		}
		buf.Truncate(buf.Len() - 1)
	}
	return buf.Bytes(), nil
}

// appendSpliced appends payload with extra (from extraJSON) inserted after its opening brace; payload is not
// re-encoded.
func appendSpliced(dst, payload, extra []byte) []byte {
	if len(extra) == 0 || len(payload) < 2 || payload[0] != '{' {
		return append(dst, payload...)
	}
	dst = append(dst, '{')
	dst = append(dst, extra...)
	if payload[1] != '}' {
		dst = append(dst, ',')
	}
	return append(dst, payload[1:]...)
}
